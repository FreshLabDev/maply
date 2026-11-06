// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
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

// catchUpAfter is how long after the last reveal a day still open counts as
// missed by its job, which runs worldAfter after it and tries a few times.
const catchUpAfter = time.Hour

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
		defer func() {
			if p := recover(); p != nil {
				s.Log.Error("world refresh panicked", "chat", chatID, "panic", p, "stack", string(debug.Stack()))
			}
		}()
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
	return i18n.T(lang, key, "time", i18n.At(lang, at.Format("15:04"), round.Name(c.Zone, c.Lang)))
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

// worldFinal runs once the job's date has closed in every zone: see closeDay.
func (s *Scheduler) worldFinal(ctx context.Context, j db.Job) error {
	// The job's own time is not trusted: a job planned or moved by hand, or a
	// clock that jumped, would keep the day while it is still played
	// somewhere, and the archive, the days and their strips read the place
	// from day_closed.
	if last := s.Zones.LastReveal(j.Date); s.now().Before(last) {
		return laterError{last.Add(worldAfter)}
	}
	return s.closeDay(ctx, j.Date)
}

// closeDay ends a day that is over in every zone, for the final world job
// and for the catch-up. A day nobody answered anywhere is skipped. Any other
// is kept as it stands: results held back for want of an answer go out, the
// final picture goes into every DM result and the final world line into every
// group result. All but the held results are edits, so a group still gets
// three messages a day.
func (s *Scheduler) closeDay(ctx context.Context, d round.Date) error {
	rd, ok, err := s.Store.RoundOn(ctx, d)
	if err != nil || !ok {
		return orSkip(err)
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return err
	}
	if err := s.scoreDue(ctx, d, answer); err != nil {
		return err
	}
	n, err := s.Store.DayAnswers(ctx, d)
	if err != nil {
		return err
	}
	if n == 0 {
		if skipped, err := s.skipDay(ctx, d, rd); err != nil || skipped {
			return err
		}
	}
	// Over everywhere and scored, the day's places are kept as they are. A
	// failure is the job's: it runs again, as the day is not kept.
	if err := s.Store.KeepDay(ctx, d); err != nil {
		return err
	}
	held := s.sendHeld(ctx, d)
	if err := s.finalGroups(ctx, d, rd, answer); err != nil {
		return err
	}
	if s.World != nil {
		if err := s.finalDMs(ctx, d, rd, answer); err != nil {
			return err
		}
	}
	return held
}

// sendHeld sends the results the chats held at their reveal, now that the day
// has an answer. A result that goes out after the day closed carries the
// final world line itself.
func (s *Scheduler) sendHeld(ctx context.Context, d round.Date) error {
	chats, err := s.Store.HeldChats(ctx, d)
	if err != nil {
		return err
	}
	failed := 0
	for _, id := range chats {
		err := s.result(ctx, db.Job{ChatID: id, Date: d})
		var apiErr *tg.APIError
		switch {
		case err == nil || errors.Is(err, errSkip):
		case errors.As(err, &apiErr) && (apiErr.IsUnreachableDestination() || dmForbidden(id, apiErr)):
			s.Log.Info("chat unreachable, leaving it", "chat", id, "error", err)
			if leaveErr := s.Store.LeaveChat(ctx, id); leaveErr != nil {
				s.Log.Error("leave chat failed", "chat", id, "error", leaveErr)
			}
		default:
			s.Log.Warn("held result not sent", "chat", id, "date", d, "error", err)
			failed++
		}
	}
	if failed > 0 {
		// Another try sends only the ones still held.
		return fmt.Errorf("held results: %d of %d not sent", failed, len(chats))
	}
	return nil
}

// skipDay skips a day nobody answered: the morning posts that went out say
// so and are unpinned, as edits, and the round and its place are marked. The
// posts come first, so a failed edit runs the day again. It reports false
// when an answer turned up and the day is kept after all.
func (s *Scheduler) skipDay(ctx context.Context, d round.Date, rd db.Round) (bool, error) {
	posts, err := s.Store.Posts(ctx, d)
	if err != nil {
		return false, err
	}
	failed := 0
	for _, p := range posts {
		c, ok, _ := s.chatOf(ctx, p.ChatID)
		if !ok {
			continue
		}
		if p.Pinned {
			if err := s.TG.Unpin(ctx, c.ID, p.MsgID); err != nil {
				s.Log.Info("post not unpinned", "chat", c.ID, "error", err)
			} else if err := s.Store.SetPinned(ctx, c.ID, d, false); err != nil {
				s.Log.Warn("unpin not stored", "chat", c.ID, "error", err)
			}
		}
		cr, err := s.Store.ChatRound(ctx, c.ID, d)
		if err != nil {
			return false, err
		}
		caption := skippedCaption(i18n.Resolve(c.Lang), rd.DayNo, cr.Yesterday)
		if err := s.editCaption(ctx, c.ID, p.MsgID, caption, nil); err != nil && !tg.IsMessageNotModified(err) {
			s.Log.Warn("skipped day's post not edited", "chat", c.ID, "date", d, "error", err)
			if retryEdit(err) {
				failed++
			}
		}
	}
	if failed > 0 {
		return false, fmt.Errorf("skipped day's posts: %d of %d not edited", failed, len(posts))
	}
	back := round.DateIn(s.now(), time.UTC).AddDays(db.ReturnDays)
	skipped, err := s.Store.SkipDay(ctx, d, back)
	if err != nil {
		return false, err
	}
	if skipped {
		s.Log.Info("day skipped, nobody answered", "date", d, "place", rd.PlaceID, "back_after", back)
	}
	return skipped, nil
}

// catchUp closes the days whose final world job did not: it ran before days
// were kept, or failed for good. The job's key is done by then, so nothing
// else would try again. A day is looked at an hour after its last reveal, when
// the job has had its tries.
func (s *Scheduler) catchUp(ctx context.Context) {
	if err := s.CatchUp(ctx); err != nil {
		s.Log.Error("catch-up failed", "error", err)
	}
}

// CatchUp closes every day that is over in every zone and neither kept nor
// skipped. A failed day is left for the next pass.
func (s *Scheduler) CatchUp(ctx context.Context) error {
	chats, err := s.Store.ActiveChats(ctx)
	if err != nil {
		return err
	}
	s.setChats(chats)
	now := s.now()
	days, err := s.Store.Unclosed(ctx, round.DateIn(now, time.UTC))
	if err != nil {
		return err
	}
	var first error
	for _, d := range days {
		if now.Before(s.Zones.LastReveal(d).Add(catchUpAfter)) {
			continue
		}
		s.Log.Info("catching up a day nobody closed", "date", d)
		err := func() error {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobWait)
			defer cancel()
			return s.closeDay(ctx, d)
		}()
		if err != nil && !errors.Is(err, errSkip) {
			s.Log.Error("day not closed", "date", d, "error", err)
			if first == nil {
				first = fmt.Errorf("close %s: %w", d, err)
			}
		}
	}
	return first
}

// finalGroups redraws each group result's caption with the closed day's
// world line.
func (s *Scheduler) finalGroups(ctx context.Context, d round.Date, rd db.Round, answer db.Answer) error {
	results, err := s.Store.GroupResults(ctx, d)
	if err != nil {
		return err
	}
	done, failed := 0, 0
	for _, r := range results {
		c, ok, _ := s.chatOf(ctx, r.ChatID)
		if !ok || c.Kind != "group" {
			continue
		}
		lang := i18n.Resolve(c.Lang)
		cr, err := s.Store.ChatRound(ctx, c.ID, d)
		if err != nil {
			return err
		}
		caption, err := s.finalCaption(ctx, c, rd, cr, answer, lang)
		if err != nil {
			return err
		}
		if caption == "" {
			continue
		}
		markup, err := s.playMarkup(c, d, lang, "btn.result")
		if err != nil {
			return err
		}
		if err := s.editCaption(ctx, c.ID, r.MsgID, caption, markup); err != nil && !tg.IsMessageNotModified(err) {
			s.Log.Warn("final world line not put", "chat", c.ID, "date", d, "error", err)
			if retryEdit(err) {
				failed++
			}
			continue
		}
		done++
	}
	if len(results) > 0 {
		s.Log.Info("final world lines put", "date", d, "results", len(results), "updated", done)
	}
	if failed > 0 {
		// Another try edits only these: the rest answer "not modified".
		return fmt.Errorf("final world lines: %d of %d not put", failed, len(results))
	}
	return nil
}

// retryEdit is a failed edit that may go through on another try: Telegram
// flooded, down or slow. A 400 or a 403 is the message or the chat, and
// the same edit would fail again.
func retryEdit(err error) bool {
	var apiErr *tg.APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	code := apiErr.StatusCode
	if code == 0 {
		code = apiErr.ErrorCode
	}
	return code != http.StatusBadRequest && code != http.StatusForbidden
}

// finalCaption is a group's result with the closed day's world line. The
// caption that went out at 20:00 keeps everything else as it was: the table
// and the count are the evening's, whoever has left the group since. A result
// kept from before that was stored is built again. Empty when there is no
// world line to make final.
func (s *Scheduler) finalCaption(ctx context.Context, c db.Chat, rd db.Round, cr db.ChatRound, answer db.Answer, lang string) (string, error) {
	if cr.ResultText == "" {
		gr, err := s.groupCaption(ctx, c, rd, cr, answer, lang, true)
		return gr.caption, err
	}
	if cr.ResultWorld == "" || !strings.Contains(cr.ResultText, cr.ResultWorld) {
		return "", nil
	}
	guesses, err := s.Store.ChatGuesses(ctx, rd.Date, c.ID)
	if err != nil {
		return "", err
	}
	w, err := s.Store.World(ctx, rd.Date)
	if err == nil {
		w, err = s.anonFirst(ctx, lang, c, w)
	}
	if err != nil {
		return "", err
	}
	// The final line is longer than the evening's: more players, higher
	// places, a longer label in some languages. It names fewer people until
	// it fits; the world's first goes in only when there is room for it,
	// since the group's places are what the line is for.
	for names := groupPlaces; names > 0; names-- {
		line, first := worldGroupParts(lang, w, guesses, true, names)
		if line == "" {
			return "", nil
		}
		if first != "" {
			if full := strings.Replace(cr.ResultText, cr.ResultWorld, line+"\n"+first, 1); visibleLen(full) <= captionLimit {
				return full, nil
			}
		}
		if caption := strings.Replace(cr.ResultText, cr.ResultWorld, line, 1); visibleLen(caption) <= captionLimit {
			return caption, nil
		}
	}
	s.Log.Warn("final world line does not fit", "chat", c.ID, "date", rd.Date)
	return "", nil
}

// finalDMs puts the final world picture in every DM result.
func (s *Scheduler) finalDMs(ctx context.Context, d round.Date, rd db.Round, answer db.Answer) error {
	j := db.Job{Date: d}
	results, err := s.Store.DMResults(ctx, j.Date)
	if err != nil || len(results) == 0 {
		return err
	}
	done, failed := 0, 0
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
			if retryEdit(err) {
				failed++
			}
			continue
		}
		done++
	}
	s.Log.Info("final world results put", "date", j.Date, "results", len(results), "updated", done)
	if failed > 0 {
		// Another try skips the results that already show the final picture.
		return fmt.Errorf("final world results: %d of %d not put", failed, len(results))
	}
	return nil
}

// planWorld queues the final world job of every date a chat is on, while
// that date is still open somewhere.
func (s *Scheduler) planWorld(ctx context.Context, chats []db.Chat, now time.Time) error {
	if s.worldPlanned == nil {
		s.worldPlanned = map[round.Date]bool{}
	}
	for _, c := range chats {
		if c.Kind == "dm" && s.World == nil {
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
