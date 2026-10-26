// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

type touch struct {
	userID int64
	chatID int64
}

type fakeStore struct {
	mu       sync.Mutex
	offset   int64
	touches  []touch
	userLang map[int64]string
	chatLang map[int64]string
	zones    map[int64]string
	left     []int64
	returned []int64
	stats    db.Stats
	settings map[int64]db.Settings
	rounds   map[string]bool
	guesses  []db.NewGuess
}

func newStore() *fakeStore {
	return &fakeStore{userLang: map[int64]string{}, chatLang: map[int64]string{}, zones: map[int64]string{},
		settings: map[int64]db.Settings{}, rounds: map[string]bool{}}
}

func (f *fakeStore) Offset(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.offset, nil
}

func (f *fakeStore) AdvanceOffset(_ context.Context, offset int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offset = max(f.offset, offset)
	return nil
}

func (f *fakeStore) Touch(_ context.Context, user tg.User, chat *tg.Chat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var chatID int64
	if chat != nil {
		chatID = chat.ID
	}
	f.touches = append(f.touches, touch{user.ID, chatID})
	return nil
}

func (f *fakeStore) UserLang(_ context.Context, id int64) (string, error) { return f.userLang[id], nil }
func (f *fakeStore) SetUserLang(_ context.Context, id int64, l string) error {
	f.userLang[id] = l
	return nil
}
func (f *fakeStore) ClearUserLang(_ context.Context, id int64) error {
	delete(f.userLang, id)
	return nil
}
func (f *fakeStore) ChatLang(_ context.Context, id int64) (string, error) { return f.chatLang[id], nil }
func (f *fakeStore) SetChatLang(_ context.Context, id int64, l string) error {
	f.chatLang[id] = l
	return nil
}
func (f *fakeStore) ClearChatLang(_ context.Context, id int64) error {
	delete(f.chatLang, id)
	return nil
}
func (f *fakeStore) ChatZone(_ context.Context, id int64) (string, error) { return f.zones[id], nil }
func (f *fakeStore) SetChatZone(_ context.Context, id int64, z string) error {
	f.zones[id] = z
	return nil
}
func (f *fakeStore) AdoptChatZone(_ context.Context, id int64, z string) error {
	if f.zones[id] == "" {
		f.zones[id] = z
	}
	return nil
}
func (f *fakeStore) LeaveChat(_ context.Context, id int64) error {
	f.left = append(f.left, id)
	return nil
}
func (f *fakeStore) ReturnChat(_ context.Context, id int64) error {
	f.returned = append(f.returned, id)
	return nil
}
func (f *fakeStore) Stats(context.Context, int64) (db.Stats, error) { return f.stats, nil }
func (f *fakeStore) ChatSettings(_ context.Context, id int64) (db.Settings, error) {
	st, ok := f.settings[id]
	if !ok {
		st = db.Settings{PostHour: 7, Intrigue: true, Mentions: true}
	}
	return st, nil
}
func (f *fakeStore) MovePost(ctx context.Context, id int64, minutes int) error {
	st, _ := f.ChatSettings(ctx, id)
	m := min(max(st.PostHour*60+st.PostMin+minutes, 5*60), 19*60)
	st.PostHour, st.PostMin = m/60, m%60
	f.settings[id] = st
	return nil
}
func (f *fakeStore) SetSwitch(ctx context.Context, id int64, name string, on bool) error {
	st, _ := f.ChatSettings(ctx, id)
	if name == "intrigue" {
		st.Intrigue = on
	} else {
		st.Mentions = on
	}
	f.settings[id] = st
	return nil
}
func (f *fakeStore) RoundOn(_ context.Context, d round.Date) (db.Round, bool, error) {
	return db.Round{Date: d, DayNo: 1, PlaceID: 1}, f.rounds[d.String()], nil
}
func (f *fakeStore) Contexts(context.Context, int64) ([]db.Context, error) { return nil, nil }
func (f *fakeStore) InsertGuess(_ context.Context, g db.NewGuess) error {
	for _, old := range f.guesses {
		if old.UserID == g.UserID && old.Date.String() == g.Date.String() {
			return db.ErrAlreadyAnswered
		}
	}
	f.guesses = append(f.guesses, g)
	return nil
}

type sent struct {
	chatID, messageID int64
	text              string
	markup            telegram.Markup
}

// fakeTelegram hands out one batch, then cancels the run on the next poll. It
// records what the bot said and answers getChatMember from members.
type fakeTelegram struct {
	batch    []tg.Update
	offsets  []int64
	cancel   context.CancelFunc
	commands int
	// descriptions counts the languages the profile texts were written in.
	descriptions int
	sends        []sent
	edits        []sent
	deleted      []int64
	alerts       []string
	answers      int
	toasts       []string
	members      map[int64]string
	anims        []sent
	animated     chan struct{} // told after each animation, when set
}

func (f *fakeTelegram) EditAnimation(_ context.Context, chatID, messageID int64, a telegram.Animation, caption string, markup telegram.Markup) (tg.Message, error) {
	f.anims = append(f.anims, sent{chatID, messageID, caption, markup})
	if f.animated != nil {
		f.animated <- struct{}{}
	}
	id := a.FileID
	if id == "" {
		id = "uploaded"
	}
	return tg.Message{MessageID: messageID, Chat: tg.Chat{ID: chatID}, Caption: caption, Document: &tg.Document{FileID: id}}, nil
}

func (f *fakeTelegram) DeleteWebhook(context.Context) error { return nil }

func (f *fakeTelegram) SetDescriptions(context.Context, string, string, string) error {
	f.descriptions++
	return nil
}

func (f *fakeTelegram) GetUpdates(ctx context.Context, offset int64, _ int) ([]tg.Update, error) {
	f.offsets = append(f.offsets, offset)
	if len(f.offsets) == 1 {
		return f.batch, nil
	}
	f.cancel()
	return nil, ctx.Err()
}

func (f *fakeTelegram) SetMyCommandsForScope(context.Context, []tg.BotCommand, *tg.BotCommandScope) error {
	f.commands++
	return nil
}

func (f *fakeTelegram) SendText(_ context.Context, chatID int64, text string, markup telegram.Markup) (tg.Message, error) {
	f.sends = append(f.sends, sent{chatID: chatID, text: text, markup: markup})
	return tg.Message{MessageID: 100, Chat: tg.Chat{ID: chatID}, Text: text}, nil
}

func (f *fakeTelegram) EditText(_ context.Context, chatID, messageID int64, text string, markup telegram.Markup) error {
	f.edits = append(f.edits, sent{chatID, messageID, text, markup})
	return nil
}

func (f *fakeTelegram) DeleteMessage(_ context.Context, _, messageID int64) error {
	f.deleted = append(f.deleted, messageID)
	return nil
}

func (f *fakeTelegram) AnswerCallbackQuery(_ context.Context, _, text string) error {
	f.answers++
	f.toasts = append(f.toasts, text)
	return nil
}

func (f *fakeTelegram) AnswerCallbackQueryAlert(_ context.Context, _, text string) error {
	f.alerts = append(f.alerts, text)
	return nil
}

func (f *fakeTelegram) GetChatMember(_ context.Context, _, userID int64) (tg.ChatMember, error) {
	return tg.ChatMember{Status: f.members[userID], User: tg.User{ID: userID}}, nil
}

func TestRunTouchesSendersAndAdvancesOffset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store := newStore()
	store.offset = 40
	client := &fakeTelegram{cancel: cancel, batch: []tg.Update{
		{UpdateID: 40, Message: &tg.Message{From: &tg.User{ID: 1}, Chat: tg.Chat{ID: -100, Type: "supergroup"}}},
		{UpdateID: 41, Message: &tg.Message{Chat: tg.Chat{ID: -200, Type: "channel"}}},
		{UpdateID: 42, MyChatMember: &tg.ChatMemberUpdated{From: tg.User{ID: 2}, Chat: tg.Chat{ID: -300, Type: "group"}}},
	}}
	b := New(store, client, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{})

	if err := b.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !b.Initialized() || b.LastPoll().IsZero() {
		t.Fatalf("initialized = %v, last poll = %v", b.Initialized(), b.LastPoll())
	}
	if len(client.offsets) != 2 || client.offsets[0] != 40 || client.offsets[1] != 43 {
		t.Fatalf("polled offsets = %v, want [40 43]", client.offsets)
	}
	if client.commands != 17 {
		t.Fatalf("published commands %d times, want the default and 16 languages", client.commands)
	}
	if client.descriptions != 17 {
		t.Fatalf("published descriptions %d times, want 17", client.descriptions)
	}
	if store.offset != 43 {
		t.Fatalf("stored offset = %d, want 43", store.offset)
	}
	want := []touch{{1, -100}, {2, -300}}
	if len(store.touches) != len(want) {
		t.Fatalf("touches = %+v, want %+v", store.touches, want)
	}
	for i := range want {
		if store.touches[i] != want[i] {
			t.Fatalf("touches = %+v, want %+v", store.touches, want)
		}
	}
}
