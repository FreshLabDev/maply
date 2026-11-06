// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/schedule"
)

// onRefreshWorld is a press of Refresh under a DM's result: the world
// picture is redrawn with the answers scored since, and a toast says what
// happens. Only the person whose DM it is can press it.
func (b *Bot) onRefreshWorld(ctx context.Context, cq *tg.CallbackQuery, d round.Date) error {
	m := cq.Message
	if b.opts.World == nil || m.Chat.Type != "private" || m.Chat.ID != cq.From.ID || m.MessageID == 0 {
		return b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
	}
	raw, err := b.personLang(ctx, cq.From)
	if err != nil {
		_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
		return err
	}
	lang := i18n.Resolve(raw)
	r, err := b.opts.World.RefreshWorld(ctx, m.Chat.ID, m.MessageID, d)
	text := ""
	switch r {
	case schedule.RefreshSame:
		text = i18n.T(lang, "world.refresh_same")
	case schedule.RefreshFinal:
		text = i18n.T(lang, "world.refresh_final")
	case schedule.RefreshDrawing:
		text = i18n.T(lang, "world.refresh_drawing")
	}
	if answerErr := b.tg.AnswerCallbackQuery(ctx, cq.ID, text); err == nil {
		err = answerErr
	}
	return err
}
