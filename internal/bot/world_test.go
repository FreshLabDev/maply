// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/schedule"
)

type fakeRefresher struct {
	r     schedule.Refresh
	calls []round.Date
}

func (f *fakeRefresher) RefreshWorld(_ context.Context, _, _ int64, d round.Date) (schedule.Refresh, error) {
	f.calls = append(f.calls, d)
	return f.r, nil
}

// Refresh works in the person's own DM only and says what it does.
func TestRefreshWorld(t *testing.T) {
	ctx := context.Background()
	d := round.NewDate(2026, 11, 2)
	press := func(chat tg.Chat, from int64) *tg.CallbackQuery {
		return &tg.CallbackQuery{ID: "q", From: tg.User{ID: from, LanguageCode: "en"}, Data: "wr:" + d.String(),
			Message: tg.Message{MessageID: 5, Chat: chat}}
	}
	for _, c := range []struct {
		name  string
		r     schedule.Refresh
		cq    *tg.CallbackQuery
		calls int
		toast string
	}{
		{"drawing", schedule.RefreshDrawing, press(tg.Chat{ID: 9, Type: "private"}, 9), 1, "Drawing the map with the new answers, this takes up to a minute."},
		{"nothing new", schedule.RefreshSame, press(tg.Chat{ID: 9, Type: "private"}, 9), 1, "No new answers yet."},
		{"final", schedule.RefreshFinal, press(tg.Chat{ID: 9, Type: "private"}, 9), 1, "The day's ranking is final."},
		{"gone", schedule.RefreshGone, press(tg.Chat{ID: 9, Type: "private"}, 9), 1, ""},
		{"in a group", schedule.RefreshDrawing, press(tg.Chat{ID: -100, Type: "supergroup"}, 9), 0, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			client := &fakeTelegram{}
			world := &fakeRefresher{r: c.r}
			b := New(newStore(), client, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{World: world})
			if err := b.onCallback(ctx, c.cq); err != nil {
				t.Fatal(err)
			}
			if len(world.calls) != c.calls || (c.calls > 0 && world.calls[0] != d) {
				t.Errorf("refreshes = %v", world.calls)
			}
			if len(client.toasts) != 1 || client.toasts[0] != c.toast {
				t.Errorf("toasts = %q, want %q", client.toasts, c.toast)
			}
		})
	}
}
