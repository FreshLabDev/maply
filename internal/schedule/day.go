// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"fmt"
	"hash/fnv"
	"html"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/card"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
	"github.com/FreshLabDev/maply/internal/telegram"
)

const (
	// A streak is worth a mention from this length on.
	mentionStreak = 5
	// "The world says" needs this many answers worldwide to mean anything.
	worldPins = 5
	// Pins closer than this are not "spread"; the line would be no tease.
	spreadKm = 50
	// Telegram cuts a caption at 1024 characters; the table stops before.
	captionLimit = 1024
	tableRows    = 10
	nameLimit    = 30
)

func revealText() string { return fmt.Sprintf("%02d:00", round.RevealHour) }

func (s *Scheduler) intrigue(ctx context.Context, j db.Job) error {
	c, ok, err := s.chatOf(ctx, j.ChatID)
	if err != nil {
		return err
	}
	if !ok || !c.Intrigue || !s.now().Before(j.Date.Reveal(s.location(c))) {
		return errSkip
	}
	cr, err := s.Store.ChatRound(ctx, c.ID, j.Date)
	if err != nil {
		return err
	}
	if cr.IntrigueSent {
		return nil
	}
	if cr.PostMsgID == 0 {
		return errSkip
	}
	rd, ok, err := s.Store.RoundOn(ctx, j.Date)
	if err != nil || !ok {
		return orSkip(err)
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return err
	}
	lang := i18n.Resolve(c.Lang)
	var (
		kind, text string
		there      *localNow
	)
	if c.Kind == "dm" {
		if _, answered, err := s.Store.GuessOf(ctx, j.Date, c.ID); err != nil || answered {
			return orSkip(err)
		}
		kind, text = "dm", i18n.T(lang, "intrigue.dm", "time", revealText())
		if now, at, ok := s.nowThere(ctx, answer.Point, lang); ok {
			text, there = now+"\n"+text, &at
		}
	} else if kind, text, there, err = s.groupIntrigue(ctx, c, j.Date, answer, lang); err != nil {
		return err
	}
	markup, err := s.playMarkup(c, j.Date, lang, "btn.guess")
	if err != nil {
		return err
	}
	// The tease of the time there is also drawn: the hour on a dial of the
	// day there, and how much of the chat's day is left.
	var scene anim.Scene
	if there != nil {
		here := s.now().In(s.location(c))
		scene = anim.NewIntrigue(rd.DayNo, there.hour, there.minute, there.weather,
			float64(here.Hour())+float64(here.Minute())/60, revealText(), lang)
	}
	if _, sent, err := s.sendScene(ctx, c.ID, scene, text, markup); err != nil {
		return err
	} else if !sent {
		if _, err := s.TG.SendText(ctx, c.ID, text, markup); err != nil {
			return err
		}
	}
	return s.Store.SetIntrigueSent(ctx, c.ID, j.Date, kind, s.now())
}

func orSkip(err error) error {
	if err != nil {
		return err
	}
	return errSkip
}

// groupIntrigue picks one tease for the group. When everybody has answered
// it says so; otherwise it is one of the kinds that has something to say
// today, never yesterday's, and the plain count of answers when none has.
func (s *Scheduler) groupIntrigue(ctx context.Context, c db.Chat, d round.Date, answer db.Answer, lang string) (string, string, *localNow, error) {
	guesses, err := s.Store.ChatGuesses(ctx, d, c.ID)
	if err != nil {
		return "", "", nil, err
	}
	silent, err := s.Store.Silent(ctx, c.ID, d)
	if err != nil {
		return "", "", nil, err
	}
	spread := spreadOf(guesses)
	if len(silent) == 0 && len(guesses) > 0 {
		return "all", i18n.T(lang, "intrigue.all", "km", i18n.FormatKm(lang, spread), "time", revealText()), nil, nil
	}
	last, err := s.Store.LastIntrigue(ctx, c.ID, d)
	if err != nil {
		return "", "", nil, err
	}
	type option struct{ kind, text string }
	var options []option
	add := func(kind, text string) {
		if kind != last && text != "" {
			options = append(options, option{kind, text})
		}
	}
	var there *localNow
	if now, at, ok := s.nowThere(ctx, answer.Point, lang); ok {
		add("now", now)
		there = &at
	}
	if len(guesses) >= 2 && spread >= spreadKm {
		add("spread", i18n.T(lang, "intrigue.spread", "km", i18n.FormatKm(lang, spread)))
	}
	if text, err := s.worldLine(ctx, d, lang); err != nil {
		return "", "", nil, err
	} else {
		add("world", text)
	}
	if c.Mentions {
		text, err := s.streakLine(ctx, silent, d, lang)
		if err != nil {
			return "", "", nil, err
		}
		add("streak", text)
	}
	if len(options) == 0 {
		return "waiting", i18n.T(lang, "intrigue.waiting", "n", strconv.Itoa(len(guesses)), "time", revealText()), nil, nil
	}
	h := fnv.New32a()
	fmt.Fprintf(h, "intrigue:%d:%s", c.ID, d)
	pick := options[h.Sum32()%uint32(len(options))]
	if pick.kind != "now" {
		there = nil
	}
	return pick.kind, pick.text, there, nil
}

// localNow is the time and the weather at the place.
type localNow struct {
	hour, minute int
	weather      string
}

// nowThere is "it is 03:12 there and 4 °C". Without the weather service the
// tease is skipped rather than guessed.
func (s *Scheduler) nowThere(ctx context.Context, p geo.Point, lang string) (string, localNow, bool) {
	if s.Weather == nil {
		return "", localNow{}, false
	}
	offset, temp, err := s.Weather.Now(ctx, p)
	if err != nil {
		s.Log.Warn("weather unavailable", "error", err)
		return "", localNow{}, false
	}
	there := s.now().UTC().Add(offset)
	now := localNow{hour: there.Hour(), minute: there.Minute()}
	at := there.Format("15:04")
	if temp == nil {
		return i18n.T(lang, "intrigue.now_time", "time", at), now, true
	}
	now.weather = strconv.Itoa(int(math.Round(*temp))) + " °C"
	return i18n.T(lang, "intrigue.now", "time", at, "temp", now.weather), now, true
}

// worldLine is the continent most of today's players chose, worldwide.
func (s *Scheduler) worldLine(ctx context.Context, d round.Date, lang string) (string, error) {
	pins, err := s.Store.DayPins(ctx, d)
	if err != nil || len(pins) < worldPins || s.Countries == nil {
		return "", err
	}
	counts := map[string]int{}
	for _, p := range pins {
		if c, ok := s.Countries.Locate(geo.Point{Lat: p[0], Lon: p[1]}); ok {
			counts[c.Continent]++
		}
	}
	best, n := "", 0
	for cont, k := range counts {
		if k > n || (k == n && cont < best) {
			best, n = cont, k
		}
	}
	if best == "" {
		return "", nil
	}
	pct := strconv.Itoa(int(math.Round(float64(n) * 100 / float64(len(pins)))))
	return i18n.T(lang, "intrigue.world", "pct", pct, "continent", i18n.T(lang, "continent."+best)), nil
}

// streakLine calls out the silent member with the longest streak at risk.
func (s *Scheduler) streakLine(ctx context.Context, silent []db.Member, d round.Date, lang string) (string, error) {
	if len(silent) == 0 {
		return "", nil
	}
	ids := make([]int64, len(silent))
	for i, m := range silent {
		ids[i] = m.ID
	}
	streaks, err := s.Store.Streaks(ctx, ids, d)
	if err != nil {
		return "", err
	}
	var who db.Member
	best := 0
	for _, m := range silent {
		if n := streaks[m.ID]; n > best {
			who, best = m, n
		}
	}
	if best < mentionStreak {
		return "", nil
	}
	return i18n.T(lang, "intrigue.streak", "name", mention(who), "n", strconv.Itoa(best)), nil
}

func (s *Scheduler) result(ctx context.Context, j db.Job) error {
	c, ok, err := s.chatOf(ctx, j.ChatID)
	if err != nil || !ok {
		return orSkip(err)
	}
	rd, ok, err := s.Store.RoundOn(ctx, j.Date)
	if err != nil || !ok {
		return orSkip(err)
	}
	cr, err := s.Store.ChatRound(ctx, c.ID, j.Date)
	if err != nil {
		return err
	}
	if cr.ResultMsgID != 0 {
		return nil
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return err
	}
	place := scoring.Place{Point: answer.Point, Country: answer.CountryISO, Continent: answer.Continent, City: answer.CityShape}
	if _, err := s.Store.ScoreDue(ctx, j.Date, s.now(), func(p geo.Point) scoring.Score {
		return scoring.Judge(p, place, s.Countries)
	}); err != nil {
		return err
	}
	lang := i18n.Resolve(c.Lang)
	markup, err := s.playMarkup(c, j.Date, lang, "btn.result")
	if err != nil {
		return err
	}
	if c.Kind == "group" {
		return s.groupResult(ctx, c, rd, cr, answer, lang, markup)
	}
	return s.dmResult(ctx, c, rd, cr, answer, lang, markup)
}

func (s *Scheduler) groupResult(ctx context.Context, c db.Chat, rd db.Round, cr db.ChatRound, answer db.Answer, lang string, markup telegram.Markup) error {
	// A group that joined after the reveal never saw the place.
	if cr.PostMsgID == 0 {
		return errSkip
	}
	if cr.Pinned {
		if err := s.TG.Unpin(ctx, c.ID, cr.PostMsgID); err != nil {
			s.Log.Info("post not unpinned", "chat", c.ID, "error", err)
		}
	}
	guesses, err := s.Store.ChatGuesses(ctx, rd.Date, c.ID)
	if err != nil {
		return err
	}
	ids := make([]int64, len(guesses))
	for i, g := range guesses {
		ids[i] = g.UserID
	}
	streaks, err := s.Store.Streaks(ctx, ids, rd.Date)
	if err != nil {
		return err
	}
	silent, err := s.Store.Silent(ctx, c.ID, rd.Date)
	if err != nil {
		return err
	}
	lines := table(lang, guesses, streaks)
	if len(guesses) > 0 {
		lines = append(lines, "", i18n.T(lang, "result.answered_of", "n", strconv.Itoa(len(guesses)), "total", strconv.Itoa(len(guesses)+len(silent))))
	}
	back, err := s.lateLookBack(ctx, lang, rd.Date, c, cr)
	if err != nil {
		return err
	}
	if back != "" {
		lines = append(lines, "", back)
	}
	scene := resultScene(lang, rd.DayNo, answer, guesses, streaks)
	msg, err := s.sendResult(ctx, c, rd, scene, fitCaption(card.Quoted(lang, rd.DayNo, answer), lines), markup)
	if err != nil {
		return err
	}
	return s.Store.SetResult(ctx, c.ID, rd.Date, msg.MessageID)
}

// dmResult is the whole result for somebody who plays alone, and only their
// own line for somebody whose groups already show the place.
func (s *Scheduler) dmResult(ctx context.Context, c db.Chat, rd db.Round, cr db.ChatRound, answer db.Answer, lang string, markup telegram.Markup) error {
	g, answered, err := s.Store.GuessOf(ctx, rd.Date, c.ID)
	if err != nil {
		return err
	}
	if cr.PostMsgID == 0 && !answered {
		return errSkip
	}
	own := i18n.T(lang, "result.missed")
	if answered && g.Km != nil {
		own = i18n.T(lang, "result.your", "km", i18n.FormatKm(lang, *g.Km), "points", i18n.FormatNumber(lang, g.Total()))
	}
	back, err := s.lateLookBack(ctx, lang, rd.Date, c, cr)
	if err != nil {
		return err
	}
	if back != "" {
		own += "\n\n" + back
	}
	inGroups, err := s.Store.InGroups(ctx, c.ID)
	if err != nil {
		return err
	}
	if inGroups {
		msg, err := s.TG.SendText(ctx, c.ID, own, markup)
		if err != nil {
			return err
		}
		return s.Store.SetResult(ctx, c.ID, rd.Date, msg.MessageID)
	}
	var scene anim.Scene
	if answered {
		scene = resultScene(lang, rd.DayNo, answer, []db.Guess{g}, nil)
	}
	msg, err := s.sendResult(ctx, c, rd, scene, fitCaption(card.Quoted(lang, rd.DayNo, answer), []string{own}), markup)
	if err != nil {
		return err
	}
	return s.Store.SetResult(ctx, c.ID, rd.Date, msg.MessageID)
}

func table(lang string, guesses []db.Guess, streaks map[int64]int) []string {
	if len(guesses) == 0 {
		return []string{i18n.T(lang, "result.nobody")}
	}
	var lines []string
	for i, g := range guesses {
		if i == tableRows {
			lines = append(lines, fmt.Sprintf("+%d", len(guesses)-tableRows))
			break
		}
		score := "—"
		if g.Km != nil {
			score = i18n.T(lang, "unit.km", "km", i18n.FormatKm(lang, *g.Km)) + " · " + i18n.FormatNumber(lang, g.Total())
		}
		line := fmt.Sprintf("%d. %s — %s", i+1, displayName(g.FirstName), score)
		if n := streaks[g.UserID]; n >= 2 {
			line += fmt.Sprintf(" 🔥%d", n)
		}
		lines = append(lines, line)
	}
	return lines
}

// fitCaption joins the head and the lines, dropping lines from the end until
// Telegram takes it.
func fitCaption(head string, lines []string) string {
	for n := len(lines); n >= 0; n-- {
		text := head
		if n > 0 {
			text += "\n\n" + strings.Join(lines[:n], "\n")
		}
		if visibleLen(text) <= captionLimit {
			return text
		}
	}
	return head
}

// visibleLen counts what Telegram counts: the text without its HTML tags,
// in UTF-16 units.
func visibleLen(s string) int {
	n, inTag := 0, false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>' && inTag:
			inTag = false
		case !inTag:
			n++
			if r > 0xFFFF {
				n++
			}
		}
	}
	return n
}

// postCaption is the morning post: the day, the question, the look back at
// yesterday when it is final, and in a group the who-answered line.
func postCaption(lang string, dayNo int, yesterday, line string, group bool) string {
	text := fmt.Sprintf("<b>maply #%d</b>\n%s", dayNo, i18n.T(lang, "post.hint", "time", revealText()))
	if yesterday != "" {
		text += "\n\n" + yesterday
	}
	if group {
		text += "\n\n" + line
	}
	return text
}

func answeredText(lang string, names []string) string {
	if len(names) == 0 {
		return i18n.T(lang, "post.nobody")
	}
	if len(names) > nameLimit {
		names = append(names[:nameLimit:nameLimit], fmt.Sprintf("+%d", len(names)-nameLimit))
	}
	return i18n.T(lang, "post.answered", "names", strings.Join(names, ", "))
}

func sortByAnswer(gs []db.Guess) {
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].AnsweredAt.Before(gs[j].AnsweredAt) })
}

func displayName(first string) string {
	first = strings.TrimSpace(first)
	if first == "" {
		return "?"
	}
	if r := []rune(first); len(r) > 32 {
		first = string(r[:32]) + "…"
	}
	return html.EscapeString(first)
}

func mention(m db.Member) string {
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, m.ID, displayName(m.FirstName))
}

// spreadOf is the longest distance between two pins.
func spreadOf(gs []db.Guess) float64 {
	best := 0.0
	for i := range gs {
		for j := i + 1; j < len(gs); j++ {
			best = math.Max(best, scoring.DistanceKm(gs[i].Pin, gs[j].Pin))
		}
	}
	return best
}
