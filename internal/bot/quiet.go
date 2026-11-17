// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"fmt"

	"github.com/asterfield-dev/tg"

	"github.com/asterfield-dev/maply/internal/db"
	"github.com/asterfield-dev/maply/internal/i18n"
	"github.com/asterfield-dev/maply/internal/schedule"
	"github.com/asterfield-dev/maply/internal/telegram"
)

func isQuietCB(data string) bool { return data == schedule.QuietKeep || data == schedule.QuietPause }

// onQuiet is a press under the question a quiet group gets (see
// schedule.askQuiet). Any member may keep the game going; only an admin may
// pause it, and the check runs on every press.
func (b *Bot) onQuiet(ctx context.Context, cq *tg.CallbackQuery) error {
	m := cq.Message
	if scopeOf(m.Chat) != scopeGroup || !isPanel(*m) || !pressedHere(*m, cq.Data) {
		return b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
	}
	raw, err := b.personLang(ctx, cq.From)
	if err != nil {
		_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
		return err
	}
	_, lang, err := b.groupLang(ctx, m.Chat.ID, raw)
	if err != nil {
		_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
		return err
	}
	text := ""
	switch cq.Data {
	case schedule.QuietKeep:
		kept, err := b.store.KeepPlaying(ctx, m.Chat.ID)
		if err != nil {
			_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
			return err
		}
		text = i18n.T(lang, "quiet.kept")
		if kept {
			b.log.Info("quiet group kept", "chat", m.Chat.ID)
		} else if paused, err := b.store.Paused(ctx, m.Chat.ID); err != nil {
			_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
			return err
		} else if paused {
			// Too late: the group paused itself. /start turns it back on.
			text = i18n.T(lang, "quiet.paused")
		}
	case schedule.QuietPause:
		admin, err := b.isAdmin(ctx, m.Chat.ID, cq.From.ID)
		if err != nil {
			_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
			return err
		}
		if !admin {
			return b.tg.AnswerCallbackQueryAlert(ctx, cq.ID, i18n.T(i18n.Resolve(raw), "msg.admin_only"))
		}
		if _, ok, err := b.store.PauseGroup(ctx, m.Chat.ID, db.PausedByAdmin); err != nil {
			_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
			return err
		} else if ok {
			b.log.Info("group paused", "chat", m.Chat.ID, "by", db.PausedByAdmin)
		}
		text = i18n.T(lang, "quiet.paused")
	}
	err = b.tg.EditText(ctx, m.Chat.ID, m.MessageID, text, nil)
	if err != nil && !tg.IsMessageNotModified(err) {
		_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
		return fmt.Errorf("edit quiet question in %d: %w", m.Chat.ID, err)
	}
	return b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
}

// paused is the home of a paused group: what happened, and Resume for an
// admin, who is the only one who can press it, over Settings.
func (v view) paused(admin bool) (string, telegram.Markup) {
	text := panel(productName, "", v.t("home.hint"), quote(v.t("home.paused")))
	var rows telegram.Markup
	if admin {
		b := v.button("btn.resume", actResume)
		b.Style = tg.StylePrimary
		rows = append(rows, []telegram.Button{b}, []telegram.Button{v.button("btn.settings", actSettings)})
	}
	rows = append(rows,
		[]telegram.Button{v.button("btn.help", actHelp), v.button("btn.about", actAbout)},
		[]telegram.Button{v.closeButton()},
	)
	return text, rows
}

// groupPaused reads whether the group is paused for its home. A failed read
// shows the usual home: Resume is only offered when the pause is known.
func (b *Bot) groupPaused(ctx context.Context, chatID int64) bool {
	paused, err := b.store.Paused(ctx, chatID)
	if err != nil {
		b.log.Warn("pause not read", "chat", chatID, "error", err)
		return false
	}
	return paused
}
