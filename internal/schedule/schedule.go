// SPDX-License-Identifier: Apache-2.0

// Package schedule sends the day's three messages to every chat: the morning
// post, the intrigue between 16:00 and 18:00 and the result at 20:00, each in
// the chat's own zone. Every send is a row in the job table keyed by kind,
// date and chat, so a restart re-reads the queue and nothing goes out twice.
// Between the post and the result it redraws the post's who-answered line, at
// most once a minute per chat.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
	"github.com/FreshLabDev/maply/internal/telegram"
)

type Store interface {
	ActiveChats(ctx context.Context) ([]db.Chat, error)
	EnsureChat(ctx context.Context, chatID int64) error
	EnsureRound(ctx context.Context, d round.Date) (db.Round, error)
	RoundOn(ctx context.Context, d round.Date) (db.Round, bool, error)
	PlanJob(ctx context.Context, kind string, chatID int64, d round.Date, runAt time.Time) error
	ClaimJobs(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]db.Job, error)
	FinishJob(ctx context.Context, id int64, status, lastErr string) error
	RetryJob(ctx context.Context, id int64, at time.Time, lastErr string) error
	PostponeJob(ctx context.Context, id int64, at time.Time) error
	ChatRound(ctx context.Context, chatID int64, d round.Date) (db.ChatRound, error)
	LastIntrigue(ctx context.Context, chatID int64, d round.Date) (string, error)
	SetPost(ctx context.Context, chatID int64, d round.Date, msgID int64, line, yesterday string, at time.Time) error
	SetPinned(ctx context.Context, chatID int64, d round.Date, pinned bool) error
	SetIntrigueSent(ctx context.Context, chatID int64, d round.Date, kind string, at time.Time) error
	SetResult(ctx context.Context, chatID int64, d round.Date, msgID int64, fileID string) error
	SetResultText(ctx context.Context, chatID int64, d round.Date, text, world string) error
	SetWorldResult(ctx context.Context, chatID int64, d round.Date, msgID int64, fileID, version string) error
	WorldVersion(ctx context.Context, chatID int64, d round.Date) (string, error)
	DMResults(ctx context.Context, d round.Date) ([]db.DMResult, error)
	GroupResults(ctx context.Context, d round.Date) ([]db.DMResult, error)
	SetAnswered(ctx context.Context, chatID int64, d round.Date, line string, at time.Time) error
	OpenPosts(ctx context.Context, since round.Date) ([]db.OpenPost, error)
	PhotoFileID(ctx context.Context, d round.Date) (string, error)
	SetPhotoFileID(ctx context.Context, d round.Date, id string) error
	PinWarned(ctx context.Context, chatID int64) (bool, error)
	SetPinWarned(ctx context.Context, chatID int64) error
	FirstPost(ctx context.Context, chatID int64, d round.Date) (bool, error)
	SetYesterday(ctx context.Context, chatID int64, d round.Date, text string) error
	Silent(ctx context.Context, chatID int64, d round.Date) ([]db.Member, error)
	Streaks(ctx context.Context, userIDs []int64, d round.Date) (map[int64]int, error)
	DayPins(ctx context.Context, d round.Date) ([][2]float64, error)
	InGroups(ctx context.Context, userID int64) (bool, error)
	GroupsOf(ctx context.Context, userID int64) ([]db.Group, error)
	ScoredBefore(ctx context.Context, userID int64, d round.Date) ([]db.Scored, error)
	ChatGuesses(ctx context.Context, d round.Date, chatID int64) ([]db.Guess, error)
	World(ctx context.Context, d round.Date) (db.World, error)
	GuessOf(ctx context.Context, d round.Date, userID int64) (db.Guess, bool, error)
	Puzzle(ctx context.Context, placeID int64) (db.Puzzle, error)
	Answer(ctx context.Context, placeID int64) (db.Answer, error)
	ScoreDue(ctx context.Context, d round.Date, now time.Time, judge func(geo.Point) scoring.Score) (int, error)
	LeaveChat(ctx context.Context, chatID int64) error
	MigrateChat(ctx context.Context, from, to int64) error
	FixZone(ctx context.Context, chatID int64, zone string) error
}

type Telegram interface {
	SendPhoto(ctx context.Context, chatID int64, photo telegram.Photo, caption string, markup telegram.Markup) (tg.Message, error)
	SendAnimation(ctx context.Context, chatID int64, a telegram.Animation, caption string, markup telegram.Markup) (tg.Message, error)
	EditCaption(ctx context.Context, chatID, messageID int64, caption string, markup telegram.Markup) error
	EditText(ctx context.Context, chatID, messageID int64, text string, markup telegram.Markup) error
	EditAnimation(ctx context.Context, chatID, messageID int64, a telegram.Animation, caption string, markup telegram.Markup) (tg.Message, error)
	SendText(ctx context.Context, chatID int64, text string, markup telegram.Markup) (tg.Message, error)
	Pin(ctx context.Context, chatID, messageID int64) error
	Unpin(ctx context.Context, chatID, messageID int64) error
}

// Avatars gives the profile photos of players, as square JPEGs, leaving out
// people who have none.
type Avatars interface {
	Fetch(ctx context.Context, ids []int64) map[int64][]byte
}

// Cards draws a chat's result as a picture. An error is never fatal: the
// result goes out with the place's photo instead.
type Cards interface {
	Result(ctx context.Context, chatID int64, d round.Date) ([]byte, error)
}

// Weather is what "it is now there" needs: the place's UTC offset and, when
// known, its temperature.
type Weather interface {
	Now(ctx context.Context, p geo.Point) (offset time.Duration, tempC *float64, err error)
}

type Scheduler struct {
	Store   Store
	TG      Telegram
	Weather Weather
	// Cards draws the result's map card; nil sends the photo.
	Cards Cards
	// Anim draws the posts as animations; nil sends photos and text.
	Anim *Animator
	// Avatars gives the players' photos for the pins; nil draws letters.
	Avatars Avatars
	// World draws the world animation a DM's result goes out as; nil sends
	// the person's own result.
	World     WorldCards
	Payloads  *auth.Signer
	Zones     round.Zones
	Countries *geo.Countries
	ImagesDir string
	// Username is the bot's, for the group link t.me/<Username>/play.
	Username string
	// AppURL opens the Mini App from a DM's button; empty uses the link.
	AppURL string
	Log    *slog.Logger
	Now    func() time.Time
	// Tick is how often the queue is read; a test makes it short.
	Tick time.Duration

	planned map[int64]string
	// planned is what was last planned per chat: the date and the settings
	// the times came from.
	// chats is the last ActiveChats, read once per step. The bot's Refresh
	// reads it too, so it is under chatsMu.
	chatsMu sync.RWMutex
	chats   map[int64]db.Chat
	// worldPlanned is the dates whose final world job is queued.
	worldPlanned map[round.Date]bool
	refreshing   inflight
	// redrawWait holds the chats whose last redraw failed, until when the
	// next one waits. Only the scheduler's own goroutine redraws.
	redrawWait map[int64]redrawRetry
	// redrawBroken is the error a chat's redraw last could not get past
	// before the edit, logged once and not every tick.
	redrawBroken map[int64]string
}

// redrawStuck logs what keeps a chat's post from being redrawn, once for
// each new error: a query that breaks stops every group's live line, and
// that must not go unseen, nor fill the log every tick.
func (s *Scheduler) redrawStuck(chatID int64, step string, err error) {
	msg := step + ": no round"
	if err != nil {
		msg = step + ": " + err.Error()
	}
	if s.redrawBroken[chatID] == msg {
		return
	}
	if s.redrawBroken == nil {
		s.redrawBroken = map[int64]string{}
	}
	s.redrawBroken[chatID] = msg
	s.Log.Error("post redraw stuck", "chat", chatID, "step", step, "error", err)
}

type redrawRetry struct {
	at time.Time
	n  int
}

// postGone tells whether an edit failed because the post is not there to
// edit any more: deleted, or too old for Telegram to change.
func postGone(err error) bool {
	var apiErr *tg.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	d := strings.ToLower(apiErr.Description)
	return strings.Contains(d, "message to edit not found") || strings.Contains(d, "message can't be edited") ||
		strings.Contains(d, "message_id_invalid")
}

const (
	jobPost     = "post"
	jobIntrigue = "intrigue"
	jobResult   = "result"
	jobWorld    = "world"

	lease       = 2 * time.Minute
	maxAttempts = 5
	// A redraw of the who-answered line waits at least this long after the
	// previous one, so a busy group does not hit Telegram's edit limits
	// (about 20 a minute in a group). The posts are looked at every
	// redrawTick, apart from the slower planning step.
	redrawEvery = 15 * time.Second
	redrawTick  = 5 * time.Second
)

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Run works until ctx ends.
func (s *Scheduler) Run(ctx context.Context) error {
	tick := s.Tick
	if tick == 0 {
		tick = 20 * time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	r := time.NewTicker(redrawTick)
	defer r.Stop()
	for {
		s.Step(ctx)
	wait:
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				break wait
			case <-r.C:
				if err := s.redraw(ctx); err != nil {
					s.Log.Error("post redraw failed", "error", err)
				}
			}
		}
	}
}

// Step plans, sends what is due and redraws the posts once.
func (s *Scheduler) Step(ctx context.Context) {
	if err := s.plan(ctx); err != nil {
		s.Log.Error("schedule planning failed", "error", err)
	}
	if err := s.work(ctx); err != nil {
		s.Log.Error("schedule work failed", "error", err)
	}
	if err := s.redraw(ctx); err != nil {
		s.Log.Error("post redraw failed", "error", err)
	}
}

func (s *Scheduler) location(c db.Chat) *time.Location { return s.Zones.For(c.Zone, c.Lang) }

// intrigueAt is a moment in 16:00–18:00 that is the same on every run for
// the chat and the date, so planning twice never moves it.
func intrigueAt(chatID int64, d round.Date, loc *time.Location) time.Time {
	h := fnv.New32a()
	fmt.Fprintf(h, "%d:%s", chatID, d)
	return d.At(16, 0, loc).Add(time.Duration(h.Sum32()%120) * time.Minute)
}

// plan queues the current date's three jobs for every chat. The queue's keys
// make this idempotent; the map only spares the database a repeat.
func (s *Scheduler) plan(ctx context.Context) error {
	chats, err := s.Store.ActiveChats(ctx)
	if err != nil {
		return err
	}
	if s.planned == nil {
		s.planned = map[int64]string{}
	}
	// A chat with no zone of its own follows its language. Fixed now, the
	// language can change without moving a reveal: a DM whose day is shown
	// could otherwise land in a zone where it is still open.
	for i, c := range chats {
		if c.Zone != "" {
			continue
		}
		name := round.DefaultName(c.Lang)
		if err := s.Store.FixZone(ctx, c.ID, name); err != nil {
			// The day is planned in the same zone; fixing it waits for the
			// next plan, and one chat does not hold up the rest.
			s.Log.Error("fix zone failed", "chat", c.ID, "error", err)
		}
		chats[i].Zone = name
	}
	byID := make(map[int64]db.Chat, len(chats))
	for _, c := range chats {
		byID[c.ID] = c
	}
	// A chat that left is planned afresh when it comes back, so a post
	// skipped while it was away goes out then.
	for id := range s.planned {
		if _, ok := byID[id]; !ok {
			delete(s.planned, id)
		}
	}
	s.chatsMu.Lock()
	s.chats = byID
	s.chatsMu.Unlock()
	now := s.now()
	if err := s.planWorld(ctx, chats, now); err != nil {
		return err
	}
	for _, c := range chats {
		loc := s.location(c)
		d := round.DateIn(now, loc)
		// The zone is in the mark: a chat that moves to another zone gets its
		// jobs moved the same minute, not after a restart.
		mark := fmt.Sprintf("%s %s %02d:%02d %t", d, loc, c.PostHour, c.PostMin, c.Intrigue)
		if s.planned[c.ID] == mark {
			continue
		}
		reveal := d.Reveal(loc)
		post := d.At(c.PostHour, c.PostMin, loc)
		if now.Before(reveal) {
			if err := s.Store.PlanJob(ctx, jobPost, c.ID, d, post); err != nil {
				return err
			}
			if at := intrigueAt(c.ID, d, loc); c.Intrigue && at.After(post.Add(time.Hour)) {
				if err := s.Store.PlanJob(ctx, jobIntrigue, c.ID, d, at); err != nil {
					return err
				}
			}
		}
		if err := s.Store.PlanJob(ctx, jobResult, c.ID, d, reveal); err != nil {
			return err
		}
		s.planned[c.ID] = mark
	}
	return nil
}

// errSkip ends a job without a send: there was nothing to say.
var errSkip = errors.New("nothing to send")

// laterError puts a job back in the queue: it ran before its time, because
// the chat moved to a zone further west after it was planned.
type laterError struct{ at time.Time }

func (e laterError) Error() string { return "not due until " + e.at.UTC().Format(time.RFC3339) }

// jobWait bounds one job. A job runs on a context of its own: a shutdown
// lets the send in flight finish and be recorded, where cutting it off after
// Telegram has it would make the next start send it again.
const jobWait = 4 * time.Minute

func (s *Scheduler) work(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		jobs, err := s.Store.ClaimJobs(ctx, s.now(), lease, 20)
		if err != nil || len(jobs) == 0 {
			return err
		}
		for i, j := range jobs {
			if ctx.Err() != nil {
				s.release(jobs[i:])
				return nil
			}
			s.runJob(ctx, j)
		}
	}
}

// release puts claimed jobs that never started back in the queue, so the
// next start runs them at once instead of after their lease.
func (s *Scheduler) release(jobs []db.Job) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, j := range jobs {
		if err := s.Store.PostponeJob(ctx, j.ID, j.RunAt); err != nil {
			s.Log.Warn("job not released", "key", j.Key, "error", err)
		}
	}
}

// do runs the job. A panic fails it like an error does, so one bad name or
// picture costs a job and its retries, not the process.
func (s *Scheduler) do(ctx context.Context, j db.Job) (err error) {
	defer func() {
		if p := recover(); p != nil {
			s.Log.Error("job panicked", "key", j.Key, "panic", p, "stack", string(debug.Stack()))
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	switch j.Kind {
	case jobPost:
		return s.post(ctx, j)
	case jobIntrigue:
		return s.intrigue(ctx, j)
	case jobResult:
		return s.result(ctx, j)
	case jobWorld:
		return s.worldFinal(ctx, j)
	}
	return errSkip
}

func (s *Scheduler) runJob(parent context.Context, j db.Job) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), jobWait)
	defer cancel()
	err := s.do(ctx, j)
	var apiErr *tg.APIError
	var later laterError
	switch {
	case errors.As(err, &later):
		s.Log.Info("job early, moved", "key", j.Key, "to", later.at)
		err = s.Store.PostponeJob(ctx, j.ID, later.at)
	case err == nil:
		err = s.Store.FinishJob(ctx, j.ID, "done", "")
	case errors.Is(err, errSkip):
		err = s.Store.FinishJob(ctx, j.ID, "skipped", "")
	case errors.As(err, &apiErr) && apiErr.MigrateToChatID != 0:
		// The group became a supergroup: the day goes on there from the
		// next planning pass, with the group's settings.
		s.Log.Info("group became a supergroup", "chat", j.ChatID, "to", apiErr.MigrateToChatID)
		if mErr := s.Store.MigrateChat(ctx, j.ChatID, apiErr.MigrateToChatID); mErr != nil {
			s.Log.Error("migrate chat failed", "chat", j.ChatID, "error", mErr)
		}
		err = s.Store.FinishJob(ctx, j.ID, "skipped", err.Error())
	case errors.As(err, &apiErr) && (apiErr.IsUnreachableDestination() || dmForbidden(j.ChatID, apiErr)):
		// Blocked, kicked or deleted: the chat stops getting the day.
		s.Log.Info("chat unreachable, leaving it", "chat", j.ChatID, "error", err)
		if leaveErr := s.Store.LeaveChat(ctx, j.ChatID); leaveErr != nil {
			s.Log.Error("leave chat failed", "chat", j.ChatID, "error", leaveErr)
		}
		err = s.Store.FinishJob(ctx, j.ID, "skipped", err.Error())
	case j.Attempts >= maxAttempts:
		s.Log.Error("job failed for good", "key", j.Key, "error", err)
		err = s.Store.FinishJob(ctx, j.ID, "failed", err.Error())
	default:
		s.Log.Warn("job failed, retrying", "key", j.Key, "attempt", j.Attempts, "error", err)
		err = s.Store.RetryJob(ctx, j.ID, s.now().Add(time.Duration(j.Attempts)*30*time.Second), err.Error())
	}
	if err != nil {
		s.Log.Error("job bookkeeping failed", "key", j.Key, "error", err)
	}
}

// dmForbidden is a person maply may not write to: blocked, deactivated, or
// never started, which is someone who opened the Mini App from a shared
// result and not the bot. Any message they send maply brings the DM back.
func dmForbidden(chatID int64, apiErr *tg.APIError) bool {
	return chatID > 0 && (apiErr.StatusCode == http.StatusForbidden || apiErr.ErrorCode == http.StatusForbidden)
}

// chatOf is the job's chat as the planner last saw it, with its zone and
// language. A chat that is gone since is not sent to.
func (s *Scheduler) chatOf(_ context.Context, id int64) (db.Chat, bool, error) {
	s.chatsMu.RLock()
	defer s.chatsMu.RUnlock()
	c, ok := s.chats[id]
	return c, ok, nil
}

func (s *Scheduler) post(ctx context.Context, j db.Job) error {
	c, ok, err := s.chatOf(ctx, j.ChatID)
	if err != nil {
		return err
	}
	loc := s.location(c)
	// A post planned before a long outage is no use after the reveal.
	if !ok || !s.now().Before(j.Date.Reveal(loc)) {
		return errSkip
	}
	if err := s.Store.EnsureChat(ctx, c.ID); err != nil {
		return err
	}
	rd, err := s.Store.EnsureRound(ctx, j.Date)
	if err != nil {
		return err
	}
	cr, err := s.Store.ChatRound(ctx, c.ID, j.Date)
	if err != nil {
		return err
	}
	if cr.PostMsgID != 0 {
		return nil
	}
	lang := i18n.Resolve(c.Lang)
	line := ""
	if c.Kind == "group" {
		if line, err = s.answeredLine(ctx, c.ID, j.Date, lang); err != nil {
			return err
		}
	}
	yesterday, err := s.yesterday(ctx, lang, j.Date, c)
	if err != nil {
		return err
	}
	// A group's first post says how the game goes, in place of a look back at
	// a day it did not play.
	if c.Kind == "group" {
		first, err := s.Store.FirstPost(ctx, c.ID, j.Date)
		if err != nil {
			return err
		}
		if first {
			yesterday = i18n.T(lang, "post.rules")
		}
	}
	markup, err := s.playMarkup(c, j.Date, lang, "btn.guess")
	if err != nil {
		return err
	}
	at := revealAt(lang, c)
	msg, err := s.sendPost(ctx, c.ID, rd, postCaption(lang, rd.DayNo, at, yesterday, line, c.Kind == "group"), markup)
	if err != nil {
		return err
	}
	if err := s.Store.SetPost(ctx, c.ID, j.Date, msg.MessageID, line, yesterday, s.now()); err != nil {
		return err
	}
	if c.Kind == "group" && s.pin(ctx, c, j.Date, msg.MessageID) {
		// No right to pin: the admins hear it once, in the post itself. A
		// message of its own would be the group's fourth that day.
		note := i18n.T(lang, "group.pin_rights")
		if yesterday != "" {
			note = yesterday + "\n\n" + note
		}
		// Stored first, so a failed edit is made by the redraw instead.
		if err := s.Store.SetYesterday(ctx, c.ID, j.Date, note); err != nil {
			s.Log.Warn("pin request not stored", "chat", c.ID, "error", err)
			return nil
		}
		caption := postCaption(lang, rd.DayNo, at, note, line, true)
		err := s.editCaption(ctx, c.ID, msg.MessageID, caption, markup)
		switch {
		case err == nil:
			if err := s.Store.SetPinWarned(ctx, c.ID); err != nil {
				s.Log.Warn("pin request not marked", "chat", c.ID, "error", err)
			}
		case tg.IsMessageNotModified(err):
			// No room for it today: tomorrow's post asks again.
			s.Log.Info("pin request did not fit", "chat", c.ID)
		default:
			// The line stored as stale makes the redraw put the request in.
			s.Log.Warn("pin request not added, left to the redraw", "chat", c.ID, "error", err)
			if err := s.Store.SetAnswered(ctx, c.ID, j.Date, "", s.now()); err != nil {
				s.Log.Warn("post not marked for a redraw", "chat", c.ID, "error", err)
			}
		}
	}
	return nil
}

// sendPhoto sends the day's photo, uploading it only the first time.
func (s *Scheduler) sendPhoto(ctx context.Context, chatID int64, rd db.Round, caption string, markup telegram.Markup) (tg.Message, error) {
	fileID, err := s.Store.PhotoFileID(ctx, rd.Date)
	if err != nil {
		return tg.Message{}, err
	}
	photo := telegram.Photo{FileID: fileID}
	if fileID == "" {
		puzzle, err := s.Store.Puzzle(ctx, rd.PlaceID)
		if err != nil {
			return tg.Message{}, err
		}
		photo.Path = filepath.Join(s.ImagesDir, puzzle.ImageKey)
	}
	msg, err := s.TG.SendPhoto(ctx, chatID, photo, caption, markup)
	if noRights(err) {
		// A group that lets maply write but not send pictures still gets
		// the game: the same caption and button, as a message of text.
		s.Log.Info("no right to send pictures, sending text", "chat", chatID, "error", err)
		return s.TG.SendText(ctx, chatID, caption, markup)
	}
	if err != nil {
		return msg, err
	}
	if fileID == "" && len(msg.Photo) > 0 {
		largest := msg.Photo[len(msg.Photo)-1].FileID
		if err := s.Store.SetPhotoFileID(ctx, rd.Date, largest); err != nil {
			s.Log.Warn("photo file id not stored", "date", rd.Date, "error", err)
		}
	}
	return msg, nil
}

// sendResult sends the result: the animation when it can be drawn, else the
// map card, else the place's photo. A renderer that is slow, down or wrong
// costs its picture and nothing else. Telegram refusing the picture itself
// (a 400 that is not about the chat) also falls back, since nothing was
// delivered; any other failure of the send is returned, because the picture
// may have gone out.
func (s *Scheduler) sendResult(ctx context.Context, c db.Chat, rd db.Round, scene anim.Scene, caption string, markup telegram.Markup) (tg.Message, error) {
	if msg, sent, err := s.sendScene(ctx, c.ID, scene, caption, markup); sent {
		return msg, err
	}
	if s.Cards != nil {
		png, err := s.Cards.Result(ctx, c.ID, rd.Date)
		if err == nil {
			var msg tg.Message
			if msg, err = s.TG.SendPhoto(ctx, c.ID, telegram.Photo{PNG: png}, caption, markup); err == nil || !refusedPicture(err) {
				return msg, err
			}
		}
		s.Log.Warn("result card not sent, sending the photo", "chat", c.ID, "date", rd.Date, "error", err)
	}
	return s.sendPhoto(ctx, c.ID, rd, caption, markup)
}

// editCaption replaces the text under a post or a result, which is a
// message of text when the chat did not allow pictures.
func (s *Scheduler) editCaption(ctx context.Context, chatID, msgID int64, caption string, markup telegram.Markup) error {
	err := s.TG.EditCaption(ctx, chatID, msgID, caption, markup)
	var apiErr *tg.APIError
	if errors.As(err, &apiErr) && strings.Contains(strings.ToLower(apiErr.Description), "no caption") {
		return s.TG.EditText(ctx, chatID, msgID, caption, markup)
	}
	return err
}

// noRights is Telegram refusing because maply is not allowed to.
func noRights(err error) bool {
	var apiErr *tg.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	d := strings.ToLower(apiErr.Description)
	return strings.Contains(d, "not enough rights") || strings.Contains(d, "admin_required") || strings.Contains(d, "have no rights")
}

func refusedPicture(err error) bool {
	var apiErr *tg.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && !apiErr.IsUnreachableDestination()
}

// pin pins the post without a sound. Without the right to pin the game goes
// on; pin reports whether the admins are to hear about it, which is when
// Telegram said maply has no right to and they have not heard it yet. A
// flood wait or a timeout is not theirs to fix.
func (s *Scheduler) pin(ctx context.Context, c db.Chat, d round.Date, msgID int64) (ask bool) {
	if err := s.TG.Pin(ctx, c.ID, msgID); err != nil {
		s.Log.Info("post not pinned", "chat", c.ID, "error", err)
		if !noRights(err) {
			return false
		}
		warned, werr := s.Store.PinWarned(ctx, c.ID)
		return werr == nil && !warned
	}
	if err := s.Store.SetPinned(ctx, c.ID, d, true); err != nil {
		s.Log.Warn("pin not stored", "chat", c.ID, "error", err)
	}
	return false
}

// playMarkup is the post's one button. A DM opens the Mini App directly; a
// group gets a link with a signed payload naming the chat and the date,
// because Telegram refuses web_app buttons in groups.
func (s *Scheduler) playMarkup(c db.Chat, d round.Date, lang, key string) (telegram.Markup, error) {
	text := i18n.T(lang, key)
	if c.Kind == "dm" && s.AppURL != "" {
		return telegram.Markup{{{Text: text, WebApp: s.AppURL, Style: tg.StylePrimary}}}, nil
	}
	link := "https://t.me/" + s.Username + "/play"
	if c.Kind == "group" {
		payload, err := s.Payloads.Sign(auth.Payload{Date: d, ChatID: c.ID})
		if err != nil {
			return nil, err
		}
		link += "?startapp=" + payload
	}
	return telegram.Markup{{{Text: text, URL: link, Style: tg.StylePrimary}}}, nil
}

// answeredLine is the post's block of who in the group has answered and the
// best distances so far.
func (s *Scheduler) answeredLine(ctx context.Context, chatID int64, d round.Date, lang string) (string, error) {
	guesses, err := s.Store.ChatGuesses(ctx, d, chatID)
	if err != nil {
		return "", err
	}
	return answeredBlock(lang, guesses), nil
}

// redraw rewrites each open group post whose who-answered line changed.
func (s *Scheduler) redraw(ctx context.Context) error {
	now := s.now()
	posts, err := s.Store.OpenPosts(ctx, round.DateIn(now, time.UTC).AddDays(-1))
	if err != nil {
		return err
	}
	for _, p := range posts {
		if now.Sub(p.AnsweredAt) < redrawEvery {
			continue
		}
		c, ok, err := s.chatOf(ctx, p.ChatID)
		if err != nil || !ok {
			continue
		}
		if !now.Before(p.Date.Reveal(s.location(c))) {
			continue
		}
		lang := i18n.Resolve(c.Lang)
		line, err := s.answeredLine(ctx, c.ID, p.Date, lang)
		if err != nil {
			s.redrawStuck(c.ID, "answered line", err)
			continue
		}
		if line == p.Answered {
			continue
		}
		rd, ok, err := s.Store.RoundOn(ctx, p.Date)
		if err != nil || !ok {
			s.redrawStuck(c.ID, "round", err)
			continue
		}
		if r, ok := s.redrawWait[c.ID]; ok && now.Before(r.at) {
			continue
		}
		markup, err := s.playMarkup(c, p.Date, lang, "btn.guess")
		if err != nil {
			s.redrawStuck(c.ID, "button", err)
			continue
		}
		delete(s.redrawBroken, c.ID)
		err = s.editCaption(ctx, c.ID, p.PostMsgID, postCaption(lang, rd.DayNo, revealAt(lang, c), p.Yesterday, line, true), markup)
		switch {
		case err == nil || tg.IsMessageNotModified(err):
			delete(s.redrawWait, c.ID)
		case postGone(err):
			// Deleted by an admin: nothing to keep up to date. The line is
			// stored as drawn, so only the next answer tries again.
			s.Log.Info("post gone, not redrawn", "chat", c.ID, "error", err)
			delete(s.redrawWait, c.ID)
		default:
			// Telegram says how long to wait when it is flooded; anything
			// else backs off, so a failing edit is not retried every tick
			// until the evening and the result has room to go out.
			if s.redrawWait == nil {
				s.redrawWait = map[int64]redrawRetry{}
			}
			r := s.redrawWait[c.ID]
			r.n++
			wait := tg.RetryAfter(err)
			if wait <= 0 {
				wait = min(30*time.Second<<min(r.n-1, 6), 30*time.Minute)
			}
			r.at = now.Add(wait)
			s.redrawWait[c.ID] = r
			s.Log.Warn("post redraw failed", "chat", c.ID, "attempt", r.n, "retry_in", wait, "error", err)
			continue
		}
		if err := s.Store.SetAnswered(ctx, c.ID, p.Date, line, now); err != nil {
			return err
		}
	}
	return nil
}
