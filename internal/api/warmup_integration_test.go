// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/bot"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/schedule"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// chatLog is every message maply sends, from the bot and from the
// scheduler, as a group would see them.
type chatLog struct {
	msgs   []chatMsg
	cancel context.CancelFunc
	batch  []tg.Update
	polled int
}

type chatMsg struct {
	kind   string
	chatID int64
	text   string
	markup telegram.Markup
}

func (c *chatLog) add(kind string, chatID int64, text string, m telegram.Markup) (tg.Message, error) {
	c.msgs = append(c.msgs, chatMsg{kind, chatID, text, m})
	msg := tg.Message{MessageID: int64(len(c.msgs)), Chat: tg.Chat{ID: chatID}, Text: text}
	if kind == "photo" {
		msg.Photo = []tg.Photo{{FileID: "warm-photo"}}
	}
	return msg, nil
}

func (c *chatLog) in(chatID int64) []chatMsg {
	var out []chatMsg
	for _, m := range c.msgs {
		if m.chatID == chatID {
			out = append(out, m)
		}
	}
	return out
}

// The bot's side.
func (c *chatLog) DeleteWebhook(context.Context) error { return nil }
func (c *chatLog) GetUpdates(ctx context.Context, _ int64, _ int) ([]tg.Update, error) {
	c.polled++
	if c.polled == 1 {
		return c.batch, nil
	}
	c.cancel()
	return nil, ctx.Err()
}
func (c *chatLog) SetMyCommandsForScope(context.Context, []tg.BotCommand, *tg.BotCommandScope) error {
	return nil
}
func (c *chatLog) SendText(_ context.Context, chatID int64, text string, m telegram.Markup) (tg.Message, error) {
	return c.add("text", chatID, text, m)
}
func (c *chatLog) SendTextIn(_ context.Context, chatID int64, _ int, text string, m telegram.Markup) (tg.Message, error) {
	return c.add("text", chatID, text, m)
}
func (c *chatLog) SendKeyboard(_ context.Context, chatID int64, text string, _ telegram.Keyboard) (tg.Message, error) {
	return c.add("keyboard", chatID, text, nil)
}
func (c *chatLog) EditText(context.Context, int64, int64, string, telegram.Markup) error { return nil }
func (c *chatLog) EditAnimation(context.Context, int64, int64, telegram.Animation, string, telegram.Markup) (tg.Message, error) {
	return tg.Message{}, nil
}
func (c *chatLog) DeleteMessage(context.Context, int64, int64) error              { return nil }
func (c *chatLog) AnswerCallbackQuery(context.Context, string, string) error      { return nil }
func (c *chatLog) AnswerCallbackQueryAlert(context.Context, string, string) error { return nil }
func (c *chatLog) GetChatMember(_ context.Context, _, userID int64) (tg.ChatMember, error) {
	return tg.ChatMember{Status: "administrator", User: tg.User{ID: userID}}, nil
}
func (c *chatLog) SetDescriptions(context.Context, string, string, string) error { return nil }

// The scheduler's side.
func (c *chatLog) SendPhoto(_ context.Context, chatID int64, _ telegram.Photo, caption string, m telegram.Markup) (tg.Message, error) {
	return c.add("photo", chatID, caption, m)
}
func (c *chatLog) SendAnimation(_ context.Context, chatID int64, _ telegram.Animation, caption string, m telegram.Markup) (tg.Message, error) {
	return c.add("animation", chatID, caption, m)
}
func (c *chatLog) EditCaption(context.Context, int64, int64, string, telegram.Markup) error {
	return nil
}
func (c *chatLog) Pin(_ context.Context, chatID, _ int64) error {
	_, err := c.add("pin", chatID, "", nil)
	return err
}
func (c *chatLog) Unpin(context.Context, int64, int64) error { return nil }

// TestAGroupAddedInTheEveningWarmsUp adds maply to a group at 21:00 in Kyiv,
// after the group's reveal: the group gets its welcome with a warm-up on the
// archive's newest day, plays it from the Mini App, and an hour later gets
// the warm-up's result. The warm-up is a day of the archive: it is in no
// day's table, no world, no profile.
func TestAGroupAddedInTheEveningWarmsUp(t *testing.T) {
	hs := freeSetup(t)
	ctx := context.Background()
	store := hs.srv.Store.(*db.Store)
	const newGroup = int64(-1001900005599)
	hs.person(1, "Oksana")
	hs.person(2, "Ivan")
	// Yesterday Ivan answered from his DM; the day is over everywhere.
	yesterday := hs.seedPast(1, "JP", "asia", pastAnswer{user: 2, km: 2000, points: 900})
	worldBefore, err := store.World(ctx, yesterday)
	if err != nil {
		t.Fatal(err)
	}
	// 21:00 in Kyiv on 10 December: the group's 20:00 has passed.
	hs.now = time.Date(2026, time.December, 10, 19, 0, 0, 0, time.UTC)
	zones := hs.srv.Zones
	hs.srv.Free.Zones = zones
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	group := tg.Chat{ID: newGroup, Type: "supergroup", Title: "Atlas"}
	oksana := tg.User{ID: 1, FirstName: "Oksana", LanguageCode: "uk"}

	// The bot is added.
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	chats := &chatLog{cancel: cancel, batch: []tg.Update{{UpdateID: 1, MyChatMember: &tg.ChatMemberUpdated{
		Chat: group, From: oksana, OldChatMember: tg.ChatMember{Status: "left"}, NewChatMember: tg.ChatMember{Status: "administrator"}}}}}
	b := bot.New(store, chats, log, bot.Options{Username: "maplyin_bot", Zones: zones, Payloads: hs.signer,
		Now: func() time.Time { return hs.now }})
	if err := b.Run(runCtx); err != nil {
		t.Fatal(err)
	}
	welcome := chats.in(newGroup)
	if len(welcome) != 1 || welcome[0].kind != "text" || !strings.Contains(welcome[0].text, "22:00") {
		t.Fatalf("welcome = %+v", welcome)
	}
	var link string
	for _, row := range welcome[0].markup {
		for _, btn := range row {
			if strings.Contains(btn.URL, "startapp=w") {
				link = btn.URL
			}
		}
	}
	u, err := url.Parse(link)
	if err != nil || link == "" {
		t.Fatalf("no warm-up button: %+v", welcome[0].markup)
	}
	start := u.Query().Get("startapp")

	// Oksana plays it from the welcome; Ivan answered that day and cannot;
	// a stranger with a forwarded link is not in the group.
	members := hs.srv.Members.(members)
	members[[2]int64{newGroup, 1}] = true
	members[[2]int64{newGroup, 2}] = true
	inGroup := initData(1, "Oksana", "uk", start)
	g := decode[free.SoloView](t, hs.expect("POST", "/api/free/archive", inGroup, `{"warmup":true}`, http.StatusOK))
	if g.Day == nil || g.Day.N != 99 || g.Current == nil {
		t.Fatalf("warm-up game = %+v", g)
	}
	hs.expect("POST", fmt.Sprintf("/api/free/solo/%d/guess", g.ID), inGroup, `{"n":1,"lat":10.5,"lon":20.5}`, http.StatusOK)
	if body := hs.expect("POST", "/api/free/archive", initData(2, "Ivan", "ru", start), `{"warmup":true}`, http.StatusConflict); !strings.Contains(body, "day_answered") {
		t.Fatalf("Ivan took a day he answered: %s", body)
	}
	hs.expect("POST", "/api/free/archive", initData(3, "Alesia", "be", start), `{"warmup":true}`, http.StatusForbidden)
	// The day's post payload is not a warm-up's, and a warm-up needs one.
	day, err := hs.signer.Sign(auth.Payload{Date: yesterday, ChatID: newGroup})
	if err != nil {
		t.Fatal(err)
	}
	hs.expect("POST", "/api/free/archive", initData(1, "Oksana", "uk", "w"+day), `{"warmup":true}`, http.StatusUnauthorized)
	hs.expect("POST", "/api/free/archive", initData(1, "Oksana", "uk", ""), `{"warmup":true}`, http.StatusBadRequest)

	// The scheduler plans the group's day, which is over: no post and no
	// result. An hour after the welcome the warm-up's result comes.
	sched := &schedule.Scheduler{Store: store, TG: chats, Payloads: hs.signer, Zones: zones, Countries: hs.srv.Countries,
		ImagesDir: hs.srv.ImagesDir, Username: "maplyin_bot", Log: log, Now: func() time.Time { return hs.now }}
	sched.Step(ctx)
	if got := chats.in(newGroup); len(got) != 1 {
		t.Fatalf("before the warm-up's result the group got %+v", got[1:])
	}
	hs.now = hs.now.Add(30 * time.Minute)
	sched.Step(ctx)
	if got := chats.in(newGroup); len(got) != 1 {
		t.Fatalf("the warm-up's result came early: %+v", got[1:])
	}
	// Too late for the warm-up: its result is about to come.
	hs.now = hs.now.Add(30 * time.Minute)
	hs.expect("POST", "/api/free/archive", initData(2, "Ivan", "ru", start), `{"warmup":true}`, http.StatusGone)
	sched.Step(ctx)
	sched.Step(ctx)
	got := chats.in(newGroup)
	if len(got) != 2 || got[1].kind != "photo" {
		t.Fatalf("group messages = %+v", got)
	}
	result := got[1].text
	for _, want := range []string{"maply #99", "Oksana", "Past", "07:00", "20:00"} {
		if !strings.Contains(result, want) {
			t.Fatalf("warm-up result = %q, missing %q", result, want)
		}
	}
	if strings.Contains(result, "Ivan") {
		t.Fatalf("warm-up result names Ivan, who did not play it: %q", result)
	}

	// Nowhere else: no answer of the day, the day's world as it was, no day
	// on Oksana's profile, nothing in the day's ranking of the group.
	var guesses int
	if err := hs.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM maply.guess WHERE user_id = 1) + (SELECT count(*) FROM maply.site_guess)`).Scan(&guesses); err != nil {
		t.Fatal(err)
	}
	if guesses != 0 {
		t.Fatalf("the warm-up wrote %d answers of the day", guesses)
	}
	worldAfter, err := store.World(ctx, yesterday)
	if err != nil {
		t.Fatal(err)
	}
	median := func(w db.World) string {
		if w.MedianKm == nil {
			return "-"
		}
		return fmt.Sprint(*w.MedianKm)
	}
	if fmt.Sprint(worldAfter.Entries) != fmt.Sprint(worldBefore.Entries) || median(worldAfter) != median(worldBefore) {
		t.Fatalf("the warm-up moved the world:\n%+v\n%+v", worldBefore, worldAfter)
	}
	if v := decode[ownView](t, hs.expect("GET", "/api/profile", initData(1, "Oksana", "uk", ""), "", http.StatusOK)); v.Ranked.Days != 0 {
		t.Fatalf("Oksana's profile counts %d days", v.Ranked.Days)
	}
	if gs, err := store.ChatGuesses(ctx, round.DateIn(hs.now, time.UTC), newGroup); err != nil || len(gs) != 0 {
		t.Fatalf("the group's day = %+v, %v", gs, err)
	}
	// One warm-up a group: added back, it gets a welcome without one.
	chats.polled, chats.batch = 0, chats.batch[:1]
	chats.batch[0].UpdateID = 2
	runCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	chats.cancel = cancel
	if err := b.Run(runCtx); err != nil {
		t.Fatal(err)
	}
	if again := chats.in(newGroup); len(again) != 3 || strings.Contains(fmt.Sprint(again[2].markup), "startapp=w") {
		t.Fatalf("added back: %+v", again[2:])
	}
}
