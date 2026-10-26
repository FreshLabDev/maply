// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/card"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
	"github.com/FreshLabDev/maply/internal/telegram"
	"github.com/FreshLabDev/maply/internal/world"
)

// A DM's result shows the whole world's day: every answer scored so far on
// one globe. Until the last zone's reveal more answers come in, so the
// result carries a Refresh button and a note saying why the ranking is not
// final; once the day closes everywhere a job puts the final picture in
// every DM result and takes the button away.

// WorldCards draws the world animation; see package world.
type WorldCards interface {
	Card(ctx context.Context, d round.Date, lang string, need int64) (world.Card, error)
	Fresh(ctx context.Context, d round.Date, lang, shown string, need int64) (bool, error)
	Keep(ctx context.Context, d round.Date, lang string, c world.Card, fileID string)
	Final(d round.Date) bool
}

// worldCB is the Refresh button's data: "wr:<date>".
const worldCB = "wr:"

// worldAfter is how long after the last zone's reveal the final picture is
// drawn, for the last answers to be scored.
const worldAfter = 2 * time.Minute

// worldWait bounds a refresh drawn in the background.
const worldWait = 3 * time.Minute

// WorldCallback is the date in a Refresh button's data.
func WorldCallback(data string) (round.Date, bool) {
	rest, ok := strings.CutPrefix(data, worldCB)
	if !ok {
		return round.Date{}, false
	}
	d, err := round.ParseDate(rest)
	return d, err == nil
}

// Refresh is what a press of Refresh does.
type Refresh int

const (
	// RefreshGone: the message is not a result this process can refresh.
	RefreshGone Refresh = iota
	// RefreshSame: nothing new since the picture the message shows.
	RefreshSame
	// RefreshFinal: the day has closed and the message shows it.
	RefreshFinal
	// RefreshDrawing: a newer picture is on its way.
	RefreshDrawing
)

type inflight struct {
	mu  sync.Mutex
	set map[int64]bool
}

func (f *inflight) take(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.set == nil {
		f.set = map[int64]bool{}
	}
	if f.set[id] {
		return false
	}
	f.set[id] = true
	return true
}

func (f *inflight) done(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.set, id)
}

// RefreshWorld answers a press of Refresh under the DM result msgID of d. A
// newer picture is drawn in the background, since a drawing takes seconds
// and the bot reads its updates one by one.
func (s *Scheduler) RefreshWorld(ctx context.Context, chatID, msgID int64, d round.Date) (Refresh, error) {
	if s.World == nil {
		return RefreshGone, nil
	}
	c, ok, _ := s.chatOf(ctx, chatID)
	if !ok || c.Kind != "dm" {
		return RefreshGone, nil
	}
	cr, err := s.Store.ChatRound(ctx, chatID, d)
	if err != nil || cr.ResultMsgID != msgID {
		return RefreshGone, err
	}
	rd, ok, err := s.Store.RoundOn(ctx, d)
	if err != nil || !ok {
		return RefreshGone, err
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return RefreshGone, err
	}
	if err := s.scoreDue(ctx, d, answer); err != nil {
		return RefreshGone, err
	}
	lang := i18n.Resolve(c.Lang)
	shown, err := s.Store.WorldVersion(ctx, chatID, d)
	if err != nil {
		return RefreshGone, err
	}
	fresh, err := s.World.Fresh(ctx, d, lang, shown, chatID)
	switch {
	case errors.Is(err, world.ErrEmpty):
		return RefreshSame, nil
	case err != nil:
		return RefreshGone, err
	case fresh && s.World.Final(d):
		return RefreshFinal, nil
	case fresh:
		return RefreshSame, nil
	}
	if !s.refreshing.take(chatID) {
		return RefreshDrawing, nil
	}
	go func() {
		defer s.refreshing.done(chatID)
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), worldWait)
		defer cancel()
		if err := s.putWorld(ctx, c, rd, cr, answer, msgID); err != nil {
			s.Log.Warn("world result not refreshed", "chat", chatID, "date", d, "error", err)
		}
	}()
	return RefreshDrawing, nil
}

// scoreDue scores d's answers whose time is up.
func (s *Scheduler) scoreDue(ctx context.Context, d round.Date, answer db.Answer) error {
	place := scoring.Place{Point: answer.Point, Country: answer.CountryISO, Continent: answer.Continent, City: answer.CityShape}
	_, err := s.Store.ScoreDue(ctx, d, s.now(), func(p geo.Point) scoring.Score {
		return scoring.Judge(p, place, s.Countries)
	})
	return err
}

// putWorld puts d's world picture as it is now in the DM result msgID.
func (s *Scheduler) putWorld(ctx context.Context, c db.Chat, rd db.Round, cr db.ChatRound, answer db.Answer, msgID int64) error {
	lang := i18n.Resolve(c.Lang)
	wc, err := s.World.Card(ctx, rd.Date, lang, c.ID)
	if err != nil {
		return err
	}
	caption, err := s.worldCaption(ctx, c, rd, cr, answer, lang, wc)
	if err != nil {
		return err
	}
	markup, err := s.worldMarkup(c, rd.Date, lang, wc.Final)
	if err != nil {
		return err
	}
	msg, err := s.TG.EditAnimation(ctx, c.ID, msgID, wc.Animation(), caption, markup)
	if err != nil && !tg.IsMessageNotModified(err) {
		return err
	}
	// Not modified leaves no message: the result already shows it.
	id := animationID(msg)
	s.World.Keep(ctx, rd.Date, lang, wc, id)
	if id == "" {
		id = wc.FileID
	}
	return s.Store.SetWorldResult(ctx, c.ID, rd.Date, msgID, id, wc.Version)
}

// dmWorld sends the DM result as the world picture. sent is false when
// nothing went out and the result is to be sent the old way.
func (s *Scheduler) dmWorld(ctx context.Context, c db.Chat, rd db.Round, cr db.ChatRound, answer db.Answer, lang string) (sent bool, err error) {
	wc, err := s.World.Card(ctx, rd.Date, lang, c.ID)
	if err != nil {
		if !errors.Is(err, world.ErrEmpty) {
			s.Log.Warn("world animation not drawn, sending the result alone", "chat", c.ID, "date", rd.Date, "error", err)
		}
		return false, nil
	}
	caption, err := s.worldCaption(ctx, c, rd, cr, answer, lang, wc)
	if err != nil {
		return false, err
	}
	markup, err := s.worldMarkup(c, rd.Date, lang, wc.Final)
	if err != nil {
		return false, err
	}
	msg, err := s.TG.SendAnimation(ctx, c.ID, wc.Animation(), caption, markup)
	if err != nil {
		if refusedPicture(err) {
			s.Log.Warn("world animation refused, sending the result alone", "chat", c.ID, "error", err)
			return false, nil
		}
		return true, err
	}
	id := animationID(msg)
	s.World.Keep(ctx, rd.Date, lang, wc, id)
	return true, s.Store.SetWorldResult(ctx, c.ID, rd.Date, msg.MessageID, id, wc.Version)
}

// worldCaption is the DM result over the world picture: the place, the
// person's own day, their place in the world so far, and until the day
// closes everywhere a note that the ranking still grows.
func (s *Scheduler) worldCaption(ctx context.Context, c db.Chat, rd db.Round, cr db.ChatRound, answer db.Answer, lang string, wc world.Card) (string, error) {
	g, answered, err := s.Store.GuessOf(ctx, rd.Date, c.ID)
	if err != nil {
		return "", err
	}
	lines := []string{i18n.T(lang, "result.missed")}
	if answered && g.Km != nil {
		if lines, err = s.ownLines(ctx, lang, rd.Date, g); err != nil {
			return "", err
		}
	}
	if line := worldLine(lang, wc, c.ID); line != "" {
		lines = append(lines, "", line)
	}
	if !wc.Final {
		lines = append(lines, "", "<i>"+s.worldWait(lang, rd.Date, c)+"</i>")
	}
	back, err := s.lateLookBack(ctx, lang, rd.Date, c, cr)
	if err != nil {
		return "", err
	}
	if back != "" {
		lines = append(lines, "", back)
	}
	return fitCaption(card.Quoted(lang, rd.DayNo, answer), lines), nil
}

// worldLine is the person's place in the world: "so far" until the day is
// final. Empty when they are not ranked.
func worldLine(lang string, wc world.Card, userID int64) string {
	rank := wc.World.RankOf(userID)
	if rank == 0 {
		return ""
	}
	n := wc.World.Players()
	r, all := strconv.Itoa(rank), i18n.FormatNumber(lang, n)
	if !wc.Final {
		return i18n.T(lang, "result.world_now", "rank", r, "n", all)
	}
	if pct := betterPct(rank, n); n >= worldAheadFrom && pct > 0 {
		return i18n.T(lang, "world.you_ahead", "rank", r, "n", all, "pct", strconv.Itoa(pct))
	}
	return i18n.T(lang, "world.you", "rank", r, "n", all)
}

// worldWait says when the day's ranking will be final, in the chat's time.
func (s *Scheduler) worldWait(lang string, d round.Date, c db.Chat) string {
	loc := s.location(c)
	at := s.Zones.LastReveal(d).In(loc)
	key := "result.world_wait"
	if round.DateIn(at, loc) != d {
		key = "result.world_wait_tomorrow"
	}
	return i18n.T(lang, key, "time", at.Format("15:04"))
}

// worldMarkup is the result's button and, until the day is final, Refresh.
func (s *Scheduler) worldMarkup(c db.Chat, d round.Date, lang string, final bool) (telegram.Markup, error) {
	markup, err := s.playMarkup(c, d, lang, "btn.result")
	if err != nil || final {
		return markup, err
	}
	refresh := telegram.Button{Text: i18n.T(lang, "btn.refresh"), CallbackData: worldCB + d.String()}
	markup[0] = append([]telegram.Button{refresh}, markup[0]...)
	return markup, nil
}

// worldFinal puts the final picture in every DM result of the job's date.
func (s *Scheduler) worldFinal(ctx context.Context, j db.Job) error {
	if s.World == nil {
		return errSkip
	}
	results, err := s.Store.DMResults(ctx, j.Date)
	if err != nil || len(results) == 0 {
		return orSkip(err)
	}
	rd, ok, err := s.Store.RoundOn(ctx, j.Date)
	if err != nil || !ok {
		return orSkip(err)
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return err
	}
	if err := s.scoreDue(ctx, j.Date, answer); err != nil {
		return err
	}
	done := 0
	for _, r := range results {
		c, ok, _ := s.chatOf(ctx, r.ChatID)
		if !ok || c.Kind != "dm" {
			continue
		}
		lang := i18n.Resolve(c.Lang)
		shown, err := s.Store.WorldVersion(ctx, c.ID, j.Date)
		if err != nil {
			return err
		}
		if fresh, err := s.World.Fresh(ctx, j.Date, lang, shown, 0); err != nil {
			return err
		} else if fresh {
			continue
		}
		cr, err := s.Store.ChatRound(ctx, c.ID, j.Date)
		if err != nil {
			return err
		}
		if err := s.putWorld(ctx, c, rd, cr, answer, r.MsgID); err != nil {
			s.Log.Warn("final world result not put", "chat", c.ID, "date", j.Date, "error", err)
			continue
		}
		done++
	}
	s.Log.Info("final world results put", "date", j.Date, "results", len(results), "updated", done)
	return nil
}

// planWorld queues the final world job of every date a chat is on, while
// that date is still open somewhere.
func (s *Scheduler) planWorld(ctx context.Context, chats []db.Chat, now time.Time) error {
	if s.World == nil {
		return nil
	}
	if s.worldPlanned == nil {
		s.worldPlanned = map[round.Date]bool{}
	}
	for _, c := range chats {
		if c.Kind != "dm" {
			continue
		}
		d := round.DateIn(now, s.location(c))
		last := s.Zones.LastReveal(d)
		if s.worldPlanned[d] || !now.Before(last) {
			continue
		}
		if err := s.Store.PlanJob(ctx, jobWorld, 0, d, last.Add(worldAfter)); err != nil {
			return err
		}
		s.worldPlanned[d] = true
	}
	for d := range s.worldPlanned {
		if d.DaysSince(round.DateIn(now, time.UTC)) < -3 {
			delete(s.worldPlanned, d)
		}
	}
	return nil
}
