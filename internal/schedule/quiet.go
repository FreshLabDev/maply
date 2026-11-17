// SPDX-License-Identifier: Apache-2.0
package schedule

import (
	"context"
	"time"

	"github.com/asterfield-dev/tg"

	"github.com/asterfield-dev/maply/internal/db"
	"github.com/asterfield-dev/maply/internal/i18n"
	"github.com/asterfield-dev/maply/internal/round"
	"github.com/asterfield-dev/maply/internal/telegram"
)

// A group where nobody plays is asked, in place of an intrigue, whether to
// go on, and paused when nobody answers the question. See docs/design.md →
// "Groups".
const (
	// QuietDays is how many quiet days in a row bring the question.
	QuietDays = 7
	// QuietWait is how many days the question waits for a press; the
	// morning after, the group is paused.
	QuietWait = 3

	// The question's buttons. Any member may keep the game going; only an
	// admin may pause it.
	QuietKeep  = "quiet:keep"
	QuietPause = "quiet:pause"
)

// QuietMarkup is the question's two buttons.
func QuietMarkup(lang string) telegram.Markup {
	return telegram.Markup{{
		{Text: i18n.T(lang, "btn.quiet_keep"), CallbackData: QuietKeep, Style: tg.StylePrimary},
		{Text: i18n.T(lang, "btn.quiet_pause"), CallbackData: QuietPause},
	}}
}

// resting is a resumed group's date that started before the resume: its
// day begins with the first morning post after it.
func resting(c db.Chat, d round.Date, loc *time.Location) bool {
	return c.Kind == "group" && !c.ResumedAt.IsZero() && d.At(c.PostHour, c.PostMin, loc).Before(c.ResumedAt)
}

// askQuiet sends the question in place of the day's intrigue when the group
// has had QuietDays quiet days in a row and has not been asked since. It
// reports whether it did; the question then is the day's second message.
func (s *Scheduler) askQuiet(ctx context.Context, c db.Chat, d round.Date) (bool, error) {
	q, err := s.Store.QuietOf(ctx, c.ID, d)
	if err != nil {
		return false, err
	}
	if q.Answered {
		return false, s.Store.ClearQuiet(ctx, c.ID)
	}
	if q.Asked || q.Days < QuietDays {
		return false, nil
	}
	lang := i18n.Resolve(c.Lang)
	msg, err := s.TG.SendText(ctx, c.ID, i18n.T(lang, "quiet.ask"), QuietMarkup(lang))
	if err != nil {
		return false, err
	}
	s.Log.Info("quiet group asked", "chat", c.ID, "date", d, "days", q.Days)
	if err := s.Store.AskQuiet(ctx, c.ID, d, msg.MessageID); err != nil {
		return true, err
	}
	return true, s.Store.SetIntrigueSent(ctx, c.ID, d, "quiet", s.now())
}

// pauseIfIgnored pauses the group on the morning QuietWait days after the
// question when nobody pressed a button and nobody answered from the group
// since. It reports whether it paused; the morning post is then not sent.
func (s *Scheduler) pauseIfIgnored(ctx context.Context, c db.Chat, d round.Date) (bool, error) {
	q, err := s.Store.QuietOf(ctx, c.ID, d)
	if err != nil {
		return false, err
	}
	if q.Answered {
		return false, s.Store.ClearQuiet(ctx, c.ID)
	}
	if !q.Asked || d.Before(q.AskedOn.AddDays(QuietWait)) {
		return false, nil
	}
	msgID, ok, err := s.Store.PauseGroup(ctx, c.ID, db.PausedBySilence)
	if err != nil || !ok {
		return ok, err
	}
	s.Log.Info("group paused", "chat", c.ID, "by", db.PausedBySilence, "asked_on", q.AskedOn)
	s.chatsMu.Lock()
	delete(s.chats, c.ID)
	s.chatsMu.Unlock()
	if msgID != 0 {
		lang := i18n.Resolve(c.Lang)
		if err := s.TG.EditText(ctx, c.ID, msgID, i18n.T(lang, "quiet.paused"), nil); err != nil && !tg.IsMessageNotModified(err) {
			// The pause holds; the question just keeps its buttons, and a
			// press on them finds the group paused.
			s.Log.Info("quiet question not edited", "chat", c.ID, "error", err)
		}
	}
	return true, nil
}
