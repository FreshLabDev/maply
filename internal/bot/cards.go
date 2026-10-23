// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/profile"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// Profiles draws a person's statistics picture; see package profile.
type Profiles interface {
	Ready(ctx context.Context, userID int64, lang string) (profile.Card, bool, error)
	Draw(ctx context.Context, userID int64, lang string, workers int) (profile.Card, error)
	Keep(ctx context.Context, userID int64, lang, version, fileID string) error
}

// drawWait is how long a press waits in the background for a picture that
// was not drawn ahead.
const drawWait = 3 * time.Minute

// screens remembers what each panel shows. A picture drawn after the press
// goes in only if the panel still shows the statistics it was drawn for, and
// a button under a statistics picture acts only on a picture the bot put
// there: the morning post and the result are animations too, and a forged
// Close must not delete them.
type screens struct {
	mu   sync.Mutex
	seq  uint64
	byID map[[2]int64]screen
}

type screen struct {
	action string
	seq    uint64
	card   bool
	at     time.Time
}

const screenLife = 48 * time.Hour

func (s *screens) set(chatID, msgID int64, action string, card bool, now time.Time) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = map[[2]int64]screen{}
	}
	if len(s.byID) > 1000 {
		for k, v := range s.byID {
			if now.Sub(v.at) > screenLife {
				delete(s.byID, k)
			}
		}
	}
	s.seq++
	s.byID[[2]int64{chatID, msgID}] = screen{action: action, seq: s.seq, card: card, at: now}
	return s.seq
}

func (s *screens) get(chatID, msgID int64) screen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[[2]int64{chatID, msgID}]
}

// isCard reports whether m is a statistics picture this process put there.
func (b *Bot) isCard(m tg.Message) bool {
	return m.MessageID != 0 && m.Text == "" && len(m.Photo) == 0 && b.screens.get(m.Chat.ID, m.MessageID).card
}

// showStats puts the statistics on the panel: as the picture when it is
// drawn, else as text at once, with the picture following when it is. show
// puts text on the panel and returns the message the panel is then.
func (b *Bot) showStats(ctx context.Context, chatID, msgID int64, userID int64, lang, text string, markup telegram.Markup, show func(string, telegram.Markup) (int64, error)) error {
	card, ok, err := b.opts.Profiles.Ready(ctx, userID, lang)
	switch {
	case errors.Is(err, profile.ErrNothing):
		_, err := show(text, markup)
		return err
	case err != nil:
		b.log.Warn("profile not read", "user", userID, "error", err)
		_, err := show(text, markup)
		return err
	case ok:
		if err := b.showCard(ctx, chatID, msgID, userID, lang, card, text, markup); err == nil {
			return nil
		} else {
			b.log.Warn("profile not shown", "user", userID, "error", err)
		}
	}
	id, err := show(text, markup)
	if err != nil {
		return err
	}
	seq := b.screens.get(chatID, id).seq
	go func() {
		ctx, cancel := context.WithTimeout(ctx, drawWait)
		defer cancel()
		card, err := b.opts.Profiles.Draw(ctx, userID, lang, 0)
		if err != nil {
			if !errors.Is(err, profile.ErrNothing) && ctx.Err() == nil {
				b.log.Warn("profile not drawn", "user", userID, "error", err)
			}
			return
		}
		if b.screens.get(chatID, id).seq != seq {
			return
		}
		if err := b.showCard(ctx, chatID, id, userID, lang, card, text, markup); err != nil {
			b.log.Warn("profile not shown", "user", userID, "error", err)
		}
	}()
	return nil
}

func (b *Bot) showCard(ctx context.Context, chatID, msgID, userID int64, lang string, card profile.Card, text string, markup telegram.Markup) error {
	msg, err := b.tg.EditAnimation(ctx, chatID, msgID, card.Animation(), text, markup)
	if err != nil && !tg.IsMessageNotModified(err) {
		return err
	}
	b.screens.set(chatID, msgID, actStats, true, b.opts.Now())
	if card.FileID == "" && msg.Document != nil {
		if err := b.opts.Profiles.Keep(ctx, userID, lang, card.Version, msg.Document.FileID); err != nil {
			b.log.Warn("profile file id not kept", "user", userID, "error", err)
		}
	}
	return nil
}
