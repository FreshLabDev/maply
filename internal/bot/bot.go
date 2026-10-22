// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

type Store interface {
	Offset(context.Context) (int64, error)
	AdvanceOffset(context.Context, int64) error
	Touch(context.Context, tg.User, *tg.Chat) error

	UserLang(ctx context.Context, userID int64) (string, error)
	SetUserLang(ctx context.Context, userID int64, lang string) error
	ClearUserLang(ctx context.Context, userID int64) error
	ChatLang(ctx context.Context, chatID int64) (string, error)
	SetChatLang(ctx context.Context, chatID int64, lang string) error
	ClearChatLang(ctx context.Context, chatID int64) error
	ChatZone(ctx context.Context, chatID int64) (string, error)
	SetChatZone(ctx context.Context, chatID int64, zone string) error
	AdoptChatZone(ctx context.Context, chatID int64, zone string) error
	LeaveChat(ctx context.Context, chatID int64) error
	ReturnChat(ctx context.Context, chatID int64) error
	Stats(ctx context.Context, userID int64) (db.Stats, error)

	ChatSettings(ctx context.Context, chatID int64) (db.Settings, error)
	MovePost(ctx context.Context, chatID int64, minutes int) error
	SetSwitch(ctx context.Context, chatID int64, name string, on bool) error

	RoundOn(ctx context.Context, d round.Date) (db.Round, bool, error)
	Contexts(ctx context.Context, userID int64) ([]db.Context, error)
	InsertGuess(ctx context.Context, g db.NewGuess) error
}

type Telegram interface {
	DeleteWebhook(context.Context) error
	GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]tg.Update, error)
	SetMyCommandsForScope(ctx context.Context, commands []tg.BotCommand, scope *tg.BotCommandScope) error
	SendText(ctx context.Context, chatID int64, text string, markup telegram.Markup) (tg.Message, error)
	EditText(ctx context.Context, chatID, messageID int64, text string, markup telegram.Markup) error
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
	AnswerCallbackQuery(ctx context.Context, callbackID, text string) error
	AnswerCallbackQueryAlert(ctx context.Context, callbackID, text string) error
	GetChatMember(ctx context.Context, chatID, userID int64) (tg.ChatMember, error)
	SetDescriptions(ctx context.Context, lang, description, short string) error
}

// Options are what the panels show that the bot cannot learn from an update.
type Options struct {
	// Username is the bot's, without "@": "/start@<Username>" is ours in a
	// group, "/start@<another>" is not.
	Username string
	Version  string
	// AppURL opens the Mini App from the Play button. Empty leaves it out.
	AppURL string
	Zones  round.Zones
	// Now is time.Now unless a test sets it.
	Now func() time.Time
}

type Bot struct {
	store       Store
	tg          Telegram
	log         *slog.Logger
	opts        Options
	lastPoll    atomic.Int64
	initialized atomic.Bool
}

func New(store Store, client Telegram, log *slog.Logger, opts Options) *Bot {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Bot{store: store, tg: client, log: log, opts: opts}
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
	b.publishCommands(ctx)
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
	// Whoever writes to maply gets the day again, after a block too.
	if u.Message != nil && !user.IsBot {
		if err := b.store.ReturnChat(ctx, chat.ID); err != nil {
			b.log.Error("return chat failed", "update_id", u.UpdateID, "error", err)
		}
	}
	var err error
	switch {
	case u.Message != nil && b.isStart(u.Message):
		err = b.sendHome(ctx, u.Message)
	case u.Message != nil && u.Message.Chat.Type == "private":
		if p, ok := locationOf(u.Message); ok {
			err = b.onLocation(ctx, u.Message, p)
		}
	case u.Callback != nil:
		err = b.onCallback(ctx, u.Callback)
	case u.MyChatMember != nil:
		err = b.onMembership(ctx, u.MyChatMember)
	}
	if err != nil {
		b.log.Error("update failed", "update_id", u.UpdateID, "error", err)
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
