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
	namesRoom    = 420
	// The post lists every distance so far, up to this many characters.
	kmRoom = 240
)

func revealText() string { return fmt.Sprintf("%02d:00", round.RevealHour) }

// revealAt is the reveal with the chat's zone, "20:00 (Kyiv)": a reader in
// another zone does not take it for their own 20:00.
func revealAt(lang string, c db.Chat) string {
	return i18n.At(lang, revealText(), round.Name(c.Zone, c.Lang))
}

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
		kind, text = "dm", i18n.T(lang, "intrigue.dm", "time", revealAt(lang, c))
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
	// One answer is not "everybody": a spread of 0 km says nothing.
	if len(silent) == 0 && len(guesses) >= 2 {
		return "all", i18n.T(lang, "intrigue.all", "km", i18n.FormatKm(lang, spread), "time", revealAt(lang, c)), nil, nil
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
		if len(guesses) == 0 {
			return "waiting", i18n.T(lang, "intrigue.waiting_none", "time", revealAt(lang, c)), nil, nil
		}
		return "waiting", i18n.T(lang, "intrigue.waiting", "n", i18n.FormatNumber(lang, len(guesses)), "time", revealAt(lang, c)), nil, nil
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
	shown := now.weather
	if lang == "ar" {
		// A left-to-right mark keeps the minus in front of the number in a
		// right-to-left line: "-5 °C", not "5 °C-". The picture draws its
		// own line and does not need it.
		shown = "\u200e" + shown
	}
	return i18n.T(lang, "intrigue.now", "time", at, "temp", shown), now, true
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
	// A chat that moved west after the job was planned still answers until
	// its new reveal: the place waits for it.
	if reveal := j.Date.Reveal(s.location(c)); s.now().Before(reveal) {
		return laterError{reveal}
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return err
	}
	if err := s.scoreDue(ctx, j.Date, answer); err != nil {
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

// hold keeps a chat's result back while the day has no answer anywhere: a
// result shows the place, and a place nobody answered is not shown. The job
// ends without a message; the final world job sends the result once another
// zone has answered, or skips the day.
func (s *Scheduler) hold(ctx context.Context, c db.Chat, d round.Date) (bool, error) {
	n, err := s.Store.DayAnswers(ctx, d)
	if err != nil || n > 0 {
		return false, err
	}
	if err := s.Store.SetHeld(ctx, c.ID, d); err != nil {
		return false, err
	}
	s.Log.Info("result held, nobody has answered yet", "chat", c.ID, "date", d)
	return true, nil
}

func (s *Scheduler) groupResult(ctx context.Context, c db.Chat, rd db.Round, cr db.ChatRound, answer db.Answer, lang string, markup telegram.Markup) error {
	// A group that joined after the reveal never saw the place.
	if cr.PostMsgID == 0 {
		return errSkip
	}
	if held, err := s.hold(ctx, c, rd.Date); held || err != nil {
		return err
	}
	if cr.Pinned {
		if err := s.TG.Unpin(ctx, c.ID, cr.PostMsgID); err != nil {
			s.Log.Info("post not unpinned", "chat", c.ID, "error", err)
		}
	}
	// A result that goes out late, after the day has closed everywhere,
	// would miss the world job's edit: it carries the final line itself.
	final := !s.now().Before(s.Zones.LastReveal(rd.Date))
	gr, err := s.groupCaption(ctx, c, rd, cr, answer, lang, final)
	if err != nil {
		return err
	}
	scene := resultScene(lang, rd.DayNo, answer, gr.guesses, gr.streaks, s.faces(ctx, gr.guesses))
	msg, err := s.sendResult(ctx, c, rd, scene, gr.caption, markup)
	if err != nil {
		return err
	}
	if err := s.Store.SetResult(ctx, c.ID, rd.Date, msg.MessageID, animationID(msg)); err != nil {
		return err
	}
	if final {
		return nil
	}
	return s.Store.SetResultText(ctx, c.ID, rd.Date, gr.caption, gr.world)
}

// groupDay is a group's result: its caption and what it was drawn from.
type groupDay struct {
	caption string
	// world is the world line in the caption, empty when it has none.
	world   string
	guesses []db.Guess
	streaks map[int64]int
}

// groupCaption is the result's caption: the place, the table, who answered,
// and where the group stands in the world. At 20:00 the world is still
// answering further west, so the line says "now"; once the day closes
// everywhere, worldFinal draws it again with final set.
func (s *Scheduler) groupCaption(ctx context.Context, c db.Chat, rd db.Round, cr db.ChatRound, answer db.Answer, lang string, final bool) (groupDay, error) {
	guesses, err := s.Store.ChatGuesses(ctx, rd.Date, c.ID)
	if err != nil {
		return groupDay{}, err
	}
	ids := make([]int64, len(guesses))
	for i, g := range guesses {
		ids[i] = g.UserID
	}
	streaks, err := s.Store.Streaks(ctx, ids, rd.Date)
	if err != nil {
		return groupDay{}, err
	}
	silent, err := s.Store.Silent(ctx, c.ID, rd.Date)
	if err != nil {
		return groupDay{}, err
	}
	lines := table(lang, guesses, streaks)
	world := ""
	if len(guesses) > 0 {
		lines = append(lines, "", i18n.T(lang, "result.answered_of", "n", strconv.Itoa(len(guesses)), "total", strconv.Itoa(len(guesses)+len(silent))))
		w, err := s.Store.World(ctx, rd.Date)
		if err == nil {
			w, err = s.anonFirst(ctx, lang, c, w)
		}
		if err != nil {
			return groupDay{}, err
		}
		if world = worldGroupLine(lang, w, guesses, final); world != "" {
			lines = append(lines, "", world)
		}
	}
	back, err := s.lateLookBack(ctx, lang, rd.Date, c, cr)
	if err != nil {
		return groupDay{}, err
	}
	if back != "" {
		lines = append(lines, "", "<blockquote>"+back+"</blockquote>")
	}
	caption := fitCaption(card.Quoted(lang, rd.DayNo, answer), lines)
	if !strings.Contains(caption, world) {
		world = ""
	}
	return groupDay{caption, world, guesses, streaks}, nil
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
	if held, err := s.hold(ctx, c, rd.Date); held || err != nil {
		return err
	}
	if s.World != nil {
		if sent, err := s.dmWorld(ctx, c, rd, cr, answer, lang); sent || err != nil {
			return err
		}
	}
	own := []string{i18n.T(lang, "result.missed")}
	if answered && g.Km != nil {
		if own, err = s.ownLines(ctx, lang, rd.Date, g); err != nil {
			return err
		}
	}
	back, err := s.lateLookBack(ctx, lang, rd.Date, c, cr)
	if err != nil {
		return err
	}
	if back != "" {
		own = append(own, "", back)
	}
	inGroups, err := s.Store.InGroups(ctx, c.ID)
	if err != nil {
		return err
	}
	if inGroups {
		// The groups show the place and the fact; the DM names the day and
		// the place in a line and keeps the rest for the person's own day.
		text := fmt.Sprintf("<b>maply #%d</b>", rd.DayNo)
		if place := card.Place(lang, answer); place != "" {
			text += ": " + html.EscapeString(place)
		}
		text += "\n" + strings.Join(own, "\n")
		msg, err := s.TG.SendText(ctx, c.ID, text, markup)
		if err != nil {
			return err
		}
		return s.Store.SetResult(ctx, c.ID, rd.Date, msg.MessageID, "")
	}
	var scene anim.Scene
	if answered {
		scene = resultScene(lang, rd.DayNo, answer, []db.Guess{g}, nil, s.faces(ctx, []db.Guess{g}))
	}
	msg, err := s.sendResult(ctx, c, rd, scene, fitCaption(card.Quoted(lang, rd.DayNo, answer), own), markup)
	if err != nil {
		return err
	}
	return s.Store.SetResult(ctx, c.ID, rd.Date, msg.MessageID, animationID(msg))
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
		score := "…"
		if g.Km != nil {
			score = i18n.FormatDistance(lang, *g.Km) + " · <b>" + i18n.FormatNumber(lang, g.Total()) + "</b>"
		}
		place := fmt.Sprintf("%d.", i+1)
		if i < len(medals) && g.Km != nil {
			place = medals[i]
		}
		line := fmt.Sprintf("%s %s · %s", place, displayName(g.FirstName), score)
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

var medals = []string{"🥇", "🥈", "🥉"}

// postCaption is the morning post: the day and the question, in a group who
// has answered and the best distances so far, and the look back at yesterday
// when it is final, set apart in a quote.
func postCaption(lang string, dayNo int, at, yesterday, block string, group bool) string {
	text := fmt.Sprintf("<b>maply #%d</b> · %s\n%s", dayNo, i18n.T(lang, "post.hint"), i18n.T(lang, "post.how", "time", at))
	if group {
		text += "\n\n" + block
	}
	return withLookBack(lang, text, yesterday)
}

// skippedCaption is the morning post of a day nobody answered, edited: it
// says so, and keeps the quote it carried, which is said once and nowhere
// else (the rules, the pin request, the month's champion).
func skippedCaption(lang string, dayNo int, yesterday string) string {
	return withLookBack(lang, fmt.Sprintf("<b>maply #%d</b>\n%s", dayNo, i18n.T(lang, "post.skipped")), yesterday)
}

// withLookBack puts the quote under a post's text when it fits.
func withLookBack(lang, text, yesterday string) string {
	// Yesterday is the first to go when the caption would not fit; a request
	// for the right to pin goes last, since it is said once. A month's season
	// outlasts the day's lines: it is said once a month, and nowhere else.
	if yesterday != "" {
		if with := text + "\n\n<blockquote>" + yesterday + "</blockquote>"; visibleLen(with) <= captionLimit {
			return with
		}
		if at := strings.Index(yesterday, "<b>"+i18n.T(lang, "season.title")+"</b>"); at > 0 {
			if with := text + "\n\n<blockquote>" + yesterday[at:] + "</blockquote>"; visibleLen(with) <= captionLimit {
				return with
			}
		}
		if ask := i18n.T(lang, "group.pin_rights"); yesterday != ask && strings.HasSuffix(yesterday, ask) {
			if with := text + "\n\n<blockquote>" + ask + "</blockquote>"; visibleLen(with) <= captionLimit {
				return with
			}
		}
	}
	return text
}

// answeredBlock is the group post's live part: who has answered, in the
// alphabet's order rather than the answers', and every distance so far,
// closest first and without names. The Mini App shows the same answers unnamed; listing names
// in the order they came would tell who is who.
func answeredBlock(lang string, guesses []db.Guess) string {
	if len(guesses) == 0 {
		return i18n.T(lang, "post.nobody")
	}
	names := make([]string, 0, len(guesses))
	var kms []float64
	for _, g := range guesses {
		names = append(names, strings.TrimSpace(g.FirstName))
		if g.Km != nil {
			kms = append(kms, *g.Km)
		}
	}
	sort.SliceStable(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	for i, n := range names {
		names[i] = displayName(n)
	}
	// At most nameLimit names and namesRoom characters of them: long names
	// would otherwise push the caption past Telegram's limit.
	shown, room := 0, 0
	for shown < len(names) && shown < nameLimit {
		room += visibleLen(names[shown]) + 2
		if room > namesRoom && shown > 0 {
			break
		}
		shown++
	}
	if shown < len(names) {
		names = append(names[:shown:shown], fmt.Sprintf("+%d", len(names)-shown))
	}
	text := "<b>" + i18n.T(lang, "post.answered", "n", strconv.Itoa(len(guesses))) + "</b>\n" + strings.Join(names, ", ")
	if len(kms) > 0 {
		sort.Float64s(kms)
		var best []string
		room := 0
		for i, km := range kms {
			d := i18n.FormatDistance(lang, km)
			if room += visibleLen(d) + 3; room > kmRoom && i > 0 {
				best = append(best, fmt.Sprintf("+%d", len(kms)-i))
				break
			}
			best = append(best, d)
		}
		text += "\n<b>" + i18n.T(lang, "post.best") + "</b>\n" + strings.Join(best, " · ")
	}
	return text
}

// worldGroupLine is where the group's players stand among everyone who has
// answered today, best first. final says the day has closed in every zone.
func worldGroupLine(lang string, w db.World, guesses []db.Guess, final bool) string {
	line, first := worldGroupParts(lang, w, guesses, final, groupPlaces)
	if first != "" {
		return line + "\n" + first
	}
	return line
}

// worldGroupParts is worldGroupLine in two: the group's places, at most
// names of them, and on a closed day the world's first when they are not in
// the group, which is what gives way when the caption has no room.
func worldGroupParts(lang string, w db.World, guesses []db.Guess, final bool, names int) (line, first string) {
	type placed struct {
		name string
		rank int
	}
	var list []placed
	for _, g := range guesses {
		if rank := w.RankOf(g.UserID); rank > 0 {
			list = append(list, placed{displayName(g.FirstName), rank})
		}
	}
	if len(list) == 0 {
		return "", ""
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].rank < list[j].rank })
	places := make([]string, 0, min(len(list), names)+1)
	for i, p := range list {
		if i == names {
			places = append(places, "…")
			break
		}
		places = append(places, fmt.Sprintf("%s #%d", p.name, p.rank))
	}
	n := i18n.FormatNumber(lang, w.Players())
	if !final {
		return i18n.T(lang, "result.world_group_now", "places", strings.Join(places, " · "), "n", n), ""
	}
	line = i18n.T(lang, "result.world_group_final", "places", strings.Join(places, " · "), "n", n)
	// The world's first, unless they are in this group and named already.
	if list[0].rank != 1 {
		e := w.Entries[0]
		first = i18n.T(lang, "result.world_first", "name", entryName(lang, e), "km", kmText(lang, e.Km))
	}
	return line, first
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
