// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/FreshLabDev/tg"
)

type Store interface {
	Offset(context.Context) (int64, error)
	AdvanceOffset(context.Context, int64) error
	Touch(context.Context, tg.User, *tg.Chat) error
}

type Telegram interface {
	DeleteWebhook(context.Context) error
	GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]tg.Update, error)
}

type Bot struct {
	store       Store
	tg          Telegram
	log         *slog.Logger
	lastPoll    atomic.Int64
	initialized atomic.Bool
}

func New(store Store, client Telegram, log *slog.Logger) *Bot {
	return &Bot{store: store, tg: client, log: log}
}

// LastPoll is when getUpdates last answered, for /healthz.
func (b *Bot) LastPoll() time.Time {
	if unix := b.lastPoll.Load(); unix != 0 {
		return time.Unix(unix, 0)
	}
	return time.Time{}
}

func (b *Bot) Initialized() bool {
	return b.initialized.Load()
}

func (b *Bot) Run(ctx context.Context) error {
	if !b.initialize(ctx) {
		return nil
	}
	offset, err := b.store.Offset(ctx)
	if err != nil {
		// Starting from 0 replays at most the updates Telegram still holds.
		// Refusing to start would be the worse failure.
		b.log.Error("telegram offset load failed; starting from the oldest pending update", "error", err)
		offset = 0
	}
	var pollFailures int
	for ctx.Err() == nil {
		updates, err := b.tg.GetUpdates(ctx, offset, 25)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			pollFailures++
			delay := retryDelay(pollFailures)
			if pollFailures <= 3 || pollFailures%10 == 0 {
				b.log.Error("telegram polling failed", "error", err, "retry_in", delay)
			}
			if !wait(ctx, delay) {
				return nil
			}
			continue
		}
		if pollFailures > 0 {
			b.log.Info("telegram polling recovered", "after_failures", pollFailures)
			pollFailures = 0
		}
		b.lastPoll.Store(time.Now().Unix())
		if len(updates) == 0 {
			continue
		}
		for _, u := range updates {
			b.handle(ctx, u)
		}
		if ctx.Err() != nil {
			return nil
		}
		offset = updates[len(updates)-1].UpdateID + 1
		if err := b.store.AdvanceOffset(ctx, offset); err != nil {
			// The in-memory offset still moves; a restart before the next
			// successful write replays the batch.
			b.log.Error("telegram offset persist failed", "offset", offset, "error", err)
		}
	}
	return nil
}

// initialize drops any webhook, since getUpdates fails while one is set. It
// retries until it succeeds or ctx ends, and reports which of the two happened.
func (b *Bot) initialize(ctx context.Context) bool {
	for attempt := 1; ctx.Err() == nil; attempt++ {
		if err := b.tg.DeleteWebhook(ctx); err != nil {
			b.log.Warn("telegram initialization failed", "step", "deleteWebhook", "attempt", attempt, "error", err)
			if !wait(ctx, retryDelay(attempt)) {
				return false
			}
			continue
		}
		b.initialized.Store(true)
		return true
	}
	return false
}

func (b *Bot) handle(ctx context.Context, u tg.Update) {
	var (
		user *tg.User
		chat *tg.Chat
	)
	switch {
	case u.Message != nil:
		// Channel posts carry no sender.
		user, chat = u.Message.From, &u.Message.Chat
	case u.Callback != nil:
		user, chat = &u.Callback.From, &u.Callback.Message.Chat
	case u.MyChatMember != nil:
		user, chat = &u.MyChatMember.From, &u.MyChatMember.Chat
	}
	if user == nil || user.ID == 0 {
		return
	}
	if err := b.store.Touch(ctx, *user, chat); err != nil {
		b.log.Error("core touch failed", "update_id", u.UpdateID, "error", err)
	}
}

func retryDelay(attempt int) time.Duration {
	delay := time.Duration(attempt) * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}

func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
