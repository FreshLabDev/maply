// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// isStart reports whether a message is /start for maply. In a group a bare
// /start is for every bot there, and /start@another is not ours.
func (b *Bot) isStart(m *tg.Message) bool {
	if m.From == nil || m.From.IsBot {
		return false
	}
	head, _, _ := strings.Cut(strings.TrimSpace(m.Text), " ")
	cmd, at, addressed := strings.Cut(head, "@")
	if cmd != "/start" {
		return false
	}
	return !addressed || strings.EqualFold(at, b.opts.Username)
}

// publishCommands sets /start's description in every language. A failure is
// logged and polling starts anyway: the command works without a description.
func (b *Bot) publishCommands(ctx context.Context) {
	for _, code := range append([]string{""}, i18n.Codes()...) {
		lang := code
		if lang == "" {
			lang = i18n.DefaultLang
		}
		cmds := []tg.BotCommand{{Command: "start", Description: i18n.T(lang, "cmd.start")}}
		if err := b.tg.SetMyCommandsForScope(ctx, cmds, &tg.BotCommandScope{Type: "default", LanguageCode: code}); err != nil {
			b.log.Warn("bot commands not published", "language", code, "error", err)
			return
		}
	}
}

// personLang is what core resolves for the person, or their client's
// language. It is the raw code: a zone follows it before the interface
// narrows it to one of the sixteen.
func (b *Bot) personLang(ctx context.Context, u tg.User) (string, error) {
	lang, err := b.store.UserLang(ctx, u.ID)
	if err != nil {
		return "", err
	}
	if lang == "" {
		lang = u.LanguageCode
	}
	return lang, nil
}

// groupLang is the group's language in core, or the reader's while the group
// has none, so a panel is never drawn in a language nobody asked for.
func (b *Bot) groupLang(ctx context.Context, chatID int64, reader string) (chat string, panel string, err error) {
	chat, err = b.store.ChatLang(ctx, chatID)
	if err != nil {
		return "", "", err
	}
	if chat != "" {
		return chat, i18n.Resolve(chat), nil
	}
	return "", i18n.Resolve(reader), nil
}

// isAdmin asks Telegram on every use: rights change, and nothing is cached.
func (b *Bot) isAdmin(ctx context.Context, chatID, userID int64) (bool, error) {
	member, err := b.tg.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return false, err
	}
	return member.Status == "administrator" || member.Status == "creator", nil
}

// adopt fixes a group's zone from its language, or the language of whoever
// set maply up while the group has none. A zone already there stays.
func (b *Bot) adopt(ctx context.Context, chatID int64, chatLang, personLang string) error {
	lang := chatLang
	if lang == "" {
		lang = personLang
	}
	return b.store.AdoptChatZone(ctx, chatID, round.DefaultName(lang))
}

func (b *Bot) sendHome(ctx context.Context, m *tg.Message) error {
	raw, err := b.personLang(ctx, *m.From)
	if err != nil {
		return err
	}
	v := view{lang: i18n.Resolve(raw), owner: m.From.ID, sc: scopeOf(m.Chat)}
	admin := false
	if v.sc == scopeGroup {
		chatLang, panelLang, err := b.groupLang(ctx, m.Chat.ID, raw)
		if err != nil {
			return err
		}
		v.lang = panelLang
		// A group maply joined before it set zones up gets one now.
		if zone, err := b.store.ChatZone(ctx, m.Chat.ID); err != nil {
			return err
		} else if zone == "" {
			if err := b.adopt(ctx, m.Chat.ID, chatLang, raw); err != nil {
				return err
			}
		}
		if admin, err = b.isAdmin(ctx, m.Chat.ID, m.From.ID); err != nil {
			// The member's panel is the safe one to show.
			b.log.Warn("admin check failed", "chat", m.Chat.ID, "error", err)
		}
	}
	text, markup := v.home(b.opts.AppURL, admin)
	if _, err := b.tg.SendText(ctx, m.Chat.ID, text, markup); err != nil {
		return fmt.Errorf("send home to %d: %w", m.Chat.ID, err)
	}
	return nil
}

// isPanel reports whether a callback may act on the message it came from.
// Telegram lets a client send any callback data against any message it can
// see, and the owner id stops only one person driving another's panel. A
// panel is a text message; the morning post and the result are photos, and
// a forged Close must not delete them.
func isPanel(m tg.Message) bool {
	return m.MessageID != 0 && m.Chat.ID != 0 && m.Text != "" && len(m.Photo) == 0
}

func (b *Bot) onCallback(ctx context.Context, cq *tg.CallbackQuery) error {
	owner, action, ok := parseMenuCB(cq.Data)
	if !ok {
		return b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
	}
	raw, err := b.personLang(ctx, cq.From)
	if err != nil {
		_ = b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
		return err
	}
	reader := i18n.Resolve(raw)
	if cq.From.ID != owner {
		return b.tg.AnswerCallbackQueryAlert(ctx, cq.ID, i18n.T(reader, "msg.not_yours"))
	}
	if !isPanel(cq.Message) {
		return b.tg.AnswerCallbackQuery(ctx, cq.ID, "")
	}
	alert, err := b.runAction(ctx, cq, action, raw)
	if alert != "" {
		return b.tg.AnswerCallbackQueryAlert(ctx, cq.ID, alert)
	}
	// Also after a failure: a button that spins for half a minute is worse
	// than one that does nothing.
	if answerErr := b.tg.AnswerCallbackQuery(ctx, cq.ID, ""); err == nil {
		err = answerErr
	}
	return err
}

// runAction does what the button says and redraws the panel. It returns an
// alert to show instead when the press is refused.
func (b *Bot) runAction(ctx context.Context, cq *tg.CallbackQuery, action, raw string) (string, error) {
	chat := cq.Message.Chat
	v := view{lang: i18n.Resolve(raw), owner: cq.From.ID, sc: scopeOf(chat)}
	if action == actClose {
		if v.sc != scopeGroup {
			return "", nil
		}
		return "", b.tg.DeleteMessage(ctx, chat.ID, cq.Message.MessageID)
	}

	var chatLang string
	admin := false
	if v.sc == scopeGroup {
		var err error
		if chatLang, v.lang, err = b.groupLang(ctx, chat.ID, raw); err != nil {
			return "", err
		}
		if adminOnly(action) || action == actHome {
			if admin, err = b.isAdmin(ctx, chat.ID, cq.From.ID); err != nil {
				return "", err
			}
		}
		if adminOnly(action) && !admin {
			return i18n.T(i18n.Resolve(raw), "msg.admin_only"), nil
		}
	}
	// A DM's zone is keyed by the person and follows their language until
	// they pick one; a group's follows the group's.
	zoneLang := raw
	if v.sc == scopeGroup {
		zoneLang = chatLang
	}

	edit := func(text string, markup telegram.Markup) (string, error) {
		return "", b.tg.EditText(ctx, chat.ID, cq.Message.MessageID, text, markup)
	}
	settings := func() (string, error) {
		zone, err := b.store.ChatZone(ctx, chat.ID)
		if err != nil {
			return "", err
		}
		return edit(v.settings(round.Name(zone, zoneLang), chatLang))
	}
	zonePanel := func() (string, error) {
		zone, err := b.store.ChatZone(ctx, chat.ID)
		if err != nil {
			return "", err
		}
		return edit(v.zone(round.Name(zone, zoneLang), b.opts.Zones, b.opts.Now()))
	}

	switch action {
	case actHome:
		return edit(v.home(b.opts.AppURL, admin))
	case actHelp:
		return edit(v.help())
	case actAbout:
		return edit(v.about(b.opts.Version))
	case actStats:
		st, err := b.store.Stats(ctx, cq.From.ID)
		if err != nil {
			return "", err
		}
		return edit(v.stats(st))
	case actLang:
		return edit(v.language())
	case actLangAuto:
		if err := b.store.ClearUserLang(ctx, cq.From.ID); err != nil {
			return "", err
		}
		// Ask core again rather than assume the client's code: another
		// automatic source may still answer for this person.
		raw, err := b.personLang(ctx, cq.From)
		if err != nil {
			return "", err
		}
		v.lang = i18n.Resolve(raw)
		return edit(v.language())
	case actSettings:
		return settings()
	case actZone:
		return zonePanel()
	case actGroupLang:
		return edit(v.groupLanguage(chatLang))
	case actGroupAuto:
		if err := b.store.ClearChatLang(ctx, chat.ID); err != nil {
			return "", err
		}
		// Core may still hold an automatic language for the group.
		var err error
		if chatLang, v.lang, err = b.groupLang(ctx, chat.ID, raw); err != nil {
			return "", err
		}
		return edit(v.groupLanguage(chatLang))
	}

	if code, ok := strings.CutPrefix(action, actLang+":"); ok {
		// The person's own language is set from their direct chat, where it
		// is offered; in a group the language there is the group's.
		if !i18n.IsSupported(code) || v.sc != scopePrivate {
			return "", nil
		}
		if err := b.store.SetUserLang(ctx, cq.From.ID, code); err != nil {
			return "", err
		}
		v.lang = code
		return edit(v.language())
	}
	if code, ok := strings.CutPrefix(action, actGroupLang+":"); ok {
		if !i18n.IsSupported(code) || v.sc != scopeGroup {
			return "", nil
		}
		if err := b.store.SetChatLang(ctx, chat.ID, code); err != nil {
			return "", err
		}
		v.lang = code
		return edit(v.groupLanguage(code))
	}
	if name, ok := strings.CutPrefix(action, actZone+":"); ok {
		if !round.IsChoice(name) {
			return "", nil
		}
		if err := b.store.SetChatZone(ctx, chat.ID, name); err != nil {
			return "", err
		}
		return zonePanel()
	}
	return "", nil
}

// onMembership sets a group up when maply is added and marks it when maply
// is removed.
func (b *Bot) onMembership(ctx context.Context, upd *tg.ChatMemberUpdated) error {
	if upd.Chat.Type != "group" && upd.Chat.Type != "supergroup" {
		return nil
	}
	in := func(status string) bool { return status == "member" || status == "administrator" }
	switch {
	case in(upd.NewChatMember.Status) && !in(upd.OldChatMember.Status):
		chatLang, err := b.store.ChatLang(ctx, upd.Chat.ID)
		if err != nil {
			return err
		}
		raw, err := b.personLang(ctx, upd.From)
		if err != nil {
			return err
		}
		return b.adopt(ctx, upd.Chat.ID, chatLang, raw)
	case !in(upd.NewChatMember.Status) && in(upd.OldChatMember.Status):
		return b.store.LeaveChat(ctx, upd.Chat.ID)
	}
	return nil
}
