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
	"time"

	"github.com/FreshLabDev/tg"

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
	ChatRound(ctx context.Context, chatID int64, d round.Date) (db.ChatRound, error)
	LastIntrigue(ctx context.Context, chatID int64, d round.Date) (string, error)
	SetPost(ctx context.Context, chatID int64, d round.Date, msgID int64, line, yesterday string, at time.Time) error
	SetPinned(ctx context.Context, chatID int64, d round.Date, pinned bool) error
	SetIntrigueSent(ctx context.Context, chatID int64, d round.Date, kind string, at time.Time) error
	SetResult(ctx context.Context, chatID int64, d round.Date, msgID int64) error
	SetAnswered(ctx context.Context, chatID int64, d round.Date, line string, at time.Time) error
	OpenPosts(ctx context.Context, since round.Date) ([]db.OpenPost, error)
	PhotoFileID(ctx context.Context, d round.Date) (string, error)
	SetPhotoFileID(ctx context.Context, d round.Date, id string) error
	WarnPin(ctx context.Context, chatID int64) (bool, error)
	Silent(ctx context.Context, chatID int64, d round.Date) ([]db.Member, error)
	Streaks(ctx context.Context, userIDs []int64, d round.Date) (map[int64]int, error)
	DayPins(ctx context.Context, d round.Date) ([][2]float64, error)
	InGroups(ctx context.Context, userID int64) (bool, error)
	ChatGuesses(ctx context.Context, d round.Date, chatID int64) ([]db.Guess, error)
	World(ctx context.Context, d round.Date) (db.World, error)
	GuessOf(ctx context.Context, d round.Date, userID int64) (db.Guess, bool, error)
	Puzzle(ctx context.Context, placeID int64) (db.Puzzle, error)
	Answer(ctx context.Context, placeID int64) (db.Answer, error)
	ScoreDue(ctx context.Context, d round.Date, now time.Time, judge func(geo.Point) scoring.Score) (int, error)
	LeaveChat(ctx context.Context, chatID int64) error
}

type Telegram interface {
	SendPhoto(ctx context.Context, chatID int64, photo telegram.Photo, caption string, markup telegram.Markup) (tg.Message, error)
	EditCaption(ctx context.Context, chatID, messageID int64, caption string, markup telegram.Markup) error
	SendText(ctx context.Context, chatID int64, text string, markup telegram.Markup) (tg.Message, error)
	Pin(ctx context.Context, chatID, messageID int64) error
	Unpin(ctx context.Context, chatID, messageID int64) error
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
	Cards     Cards
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
	// chats is the last ActiveChats, read once per step.
	chats map[int64]db.Chat
}

const (
	jobPost     = "post"
	jobIntrigue = "intrigue"
	jobResult   = "result"

	lease       = 2 * time.Minute
	maxAttempts = 5
	// A redraw of the who-answered line waits at least this long after the
	// previous one, so a busy group does not hit Telegram's edit limits.
	redrawEvery = time.Minute
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
	for {
		s.Step(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
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
	s.chats = make(map[int64]db.Chat, len(chats))
	for _, c := range chats {
		s.chats[c.ID] = c
	}
	now := s.now()
	for _, c := range chats {
		loc := s.location(c)
		d := round.DateIn(now, loc)
		mark := fmt.Sprintf("%s %02d:%02d %t", d, c.PostHour, c.PostMin, c.Intrigue)
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

func (s *Scheduler) work(ctx context.Context) error {
	for {
		jobs, err := s.Store.ClaimJobs(ctx, s.now(), lease, 20)
		if err != nil || len(jobs) == 0 {
			return err
		}
		for _, j := range jobs {
			s.runJob(ctx, j)
		}
	}
}

func (s *Scheduler) runJob(ctx context.Context, j db.Job) {
	var err error
	switch j.Kind {
	case jobPost:
		err = s.post(ctx, j)
	case jobIntrigue:
		err = s.intrigue(ctx, j)
	case jobResult:
		err = s.result(ctx, j)
	default:
		err = errSkip
	}
	var apiErr *tg.APIError
	switch {
	case err == nil:
		err = s.Store.FinishJob(ctx, j.ID, "done", "")
	case errors.Is(err, errSkip):
		err = s.Store.FinishJob(ctx, j.ID, "skipped", "")
	case errors.As(err, &apiErr) && apiErr.IsUnreachableDestination():
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

// chatOf is the job's chat as the planner last saw it, with its zone and
// language. A chat that is gone since is not sent to.
func (s *Scheduler) chatOf(_ context.Context, id int64) (db.Chat, bool, error) {
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
	markup, err := s.playMarkup(c, j.Date, lang, "btn.guess")
	if err != nil {
		return err
	}
	msg, err := s.sendPhoto(ctx, c.ID, rd, postCaption(lang, rd.DayNo, yesterday, line, c.Kind == "group"), markup)
	if err != nil {
		return err
	}
	if err := s.Store.SetPost(ctx, c.ID, j.Date, msg.MessageID, line, yesterday, s.now()); err != nil {
		return err
	}
	if c.Kind == "group" {
		s.pin(ctx, c, j.Date, msg.MessageID, lang)
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

// sendResult sends the result: the map card when it can be drawn, else the
// place's photo. A renderer that is slow, down or wrong costs the card and
// nothing else. Telegram refusing the picture itself (a 400 that is not about
// the chat) also falls back, since nothing was delivered; any other failure
// of the send is returned, because the card may have gone out.
func (s *Scheduler) sendResult(ctx context.Context, c db.Chat, rd db.Round, caption string, markup telegram.Markup) (tg.Message, error) {
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

func refusedPicture(err error) bool {
	var apiErr *tg.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && !apiErr.IsUnreachableDestination()
}

// pin pins the post without a sound. Without the right to pin the game goes
// on, and the admins hear about it once.
func (s *Scheduler) pin(ctx context.Context, c db.Chat, d round.Date, msgID int64, lang string) {
	if err := s.TG.Pin(ctx, c.ID, msgID); err != nil {
		s.Log.Info("post not pinned", "chat", c.ID, "error", err)
		warn, werr := s.Store.WarnPin(ctx, c.ID)
		if werr != nil || !warn {
			return
		}
		if _, err := s.TG.SendText(ctx, c.ID, i18n.T(lang, "group.pin_rights"), nil); err != nil {
			s.Log.Warn("pin warning not sent", "chat", c.ID, "error", err)
		}
		return
	}
	if err := s.Store.SetPinned(ctx, c.ID, d, true); err != nil {
		s.Log.Warn("pin not stored", "chat", c.ID, "error", err)
	}
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

// answeredLine names who in the group has answered, in the order they did.
func (s *Scheduler) answeredLine(ctx context.Context, chatID int64, d round.Date, lang string) (string, error) {
	guesses, err := s.Store.ChatGuesses(ctx, d, chatID)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(guesses))
	byTime := append([]db.Guess(nil), guesses...)
	sortByAnswer(byTime)
	for _, g := range byTime {
		names = append(names, displayName(g.FirstName))
	}
	return answeredText(lang, names), nil
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
		if err != nil || line == p.Answered {
			continue
		}
		rd, ok, err := s.Store.RoundOn(ctx, p.Date)
		if err != nil || !ok {
			continue
		}
		markup, err := s.playMarkup(c, p.Date, lang, "btn.guess")
		if err != nil {
			continue
		}
		err = s.TG.EditCaption(ctx, c.ID, p.PostMsgID, postCaption(lang, rd.DayNo, p.Yesterday, line, true), markup)
		if err != nil && !tg.IsMessageNotModified(err) {
			s.Log.Warn("post redraw failed", "chat", c.ID, "error", err)
			continue
		}
		if err := s.Store.SetAnswered(ctx, c.ID, p.Date, line, now); err != nil {
			return err
		}
	}
	return nil
}
