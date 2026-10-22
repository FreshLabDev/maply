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
)

type touch struct {
	userID int64
	chatID int64
}

type fakeStore struct {
	mu      sync.Mutex
	offset  int64
	touches []touch
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

// fakeTelegram hands out one batch, then cancels the run on the next poll.
type fakeTelegram struct {
	batch   []tg.Update
	offsets []int64
	cancel  context.CancelFunc
}

func (f *fakeTelegram) DeleteWebhook(context.Context) error { return nil }

func (f *fakeTelegram) GetUpdates(ctx context.Context, offset int64, _ int) ([]tg.Update, error) {
	f.offsets = append(f.offsets, offset)
	if len(f.offsets) == 1 {
		return f.batch, nil
	}
	f.cancel()
	return nil, ctx.Err()
}

func TestRunTouchesSendersAndAdvancesOffset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store := &fakeStore{offset: 40}
	client := &fakeTelegram{cancel: cancel, batch: []tg.Update{
		{UpdateID: 40, Message: &tg.Message{From: &tg.User{ID: 1}, Chat: tg.Chat{ID: -100, Type: "supergroup"}}},
		{UpdateID: 41, Message: &tg.Message{Chat: tg.Chat{ID: -200, Type: "channel"}}},
		{UpdateID: 42, MyChatMember: &tg.ChatMemberUpdated{From: tg.User{ID: 2}, Chat: tg.Chat{ID: -300, Type: "group"}}},
	}}
	b := New(store, client, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := b.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !b.Initialized() || b.LastPoll().IsZero() {
		t.Fatalf("initialized = %v, last poll = %v", b.Initialized(), b.LastPoll())
	}
	if len(client.offsets) != 2 || client.offsets[0] != 40 || client.offsets[1] != 43 {
		t.Fatalf("polled offsets = %v, want [40 43]", client.offsets)
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
