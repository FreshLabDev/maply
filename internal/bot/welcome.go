// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/schedule"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// welcome greets a group maply has just been added to, so the group knows at
// once that maply works and when it speaks. It is the group's home panel with
// what comes next in it: the morning post when the group joins before it,
// today's post, which the scheduler sends within seconds, when it joins
// during the day, and a warm-up when the group's day is already over. One
// message, in answer to an admin adding maply: none of the day's three.
func (b *Bot) welcome(ctx context.Context, upd *tg.ChatMemberUpdated, chatLang, raw string) error {
	chatID := upd.Chat.ID
	lang := i18n.Resolve(raw)
	zoneLang := raw
	if chatLang != "" {
		lang, zoneLang = i18n.Resolve(chatLang), chatLang
	}
	zone, err := b.store.ChatZone(ctx, chatID)
	if err != nil {
		return err
	}
	st, err := b.store.ChatSettings(ctx, chatID)
	if err != nil {
		return err
	}
	loc := b.opts.Zones.For(zone, zoneLang)
	now := b.opts.Now()
	d := round.DateIn(now, loc)
	post := fmt.Sprintf("%02d:%02d", st.PostHour, st.PostMin)
	v := view{lang: lang, owner: upd.From.ID, sc: scopeGroup}

	var next string
	var warm string
	switch {
	case now.Before(d.At(st.PostHour, st.PostMin, loc)):
		next = v.t("welcome.morning", "post", post)
	case now.Before(d.Reveal(loc)):
		next = v.t("welcome.today")
	default:
		w, ok, err := b.warmup(ctx, chatID, d, now)
		if err != nil {
			return err
		}
		if ok {
			if warm, err = b.warmupLink(w); err != nil {
				return err
			}
			next = v.t("welcome.warmup", "time", w.ResultAt.In(loc).Format("15:04"))
		} else {
			next = v.t("welcome.tomorrow", "post", post)
		}
	}
	// A bot that is not an admin of a supergroup may not pin; in a basic
	// group every member may, unless the admins said otherwise, and the
	// morning post asks then.
	askPin := upd.Chat.Type == "supergroup" && upd.NewChatMember.Status != "administrator"
	text, markup := v.welcome(round.Name(zone, zoneLang), post, next, warm, askPin, b.pinLink())
	if _, err := b.tg.SendText(ctx, chatID, text, markup); err != nil {
		return fmt.Errorf("welcome %d: %w", chatID, err)
	}
	if askPin {
		// The admins have heard it: the morning post does not ask again.
		if err := b.store.SetPinWarned(ctx, chatID); err != nil {
			b.log.Warn("pin request not marked", "chat", chatID, "error", err)
		}
	}
	return nil
}

// warmup is the group's warm-up that still takes answers: the one it has, or
// a new one when the group has never played. A new one is the newest day of
// the archive, closed in every zone, and its result is planned for an hour
// on. date is the group's date now, which keys the result's job.
func (b *Bot) warmup(ctx context.Context, chatID int64, date round.Date, now time.Time) (db.Warmup, bool, error) {
	w, ok, err := b.store.WarmupOf(ctx, chatID)
	if err != nil {
		return db.Warmup{}, false, err
	}
	if ok {
		return w, w.Open(now), nil
	}
	closed, ok := closedBy(b.opts.Zones, now)
	if !ok {
		return db.Warmup{}, false, nil
	}
	if w, ok, err = b.store.StartWarmup(ctx, chatID, closed, now.Add(schedule.WarmupFor)); err != nil || !ok {
		return db.Warmup{}, false, err
	}
	if err := b.store.PlanJob(ctx, schedule.JobWarmup, chatID, date, w.ResultAt); err != nil {
		return db.Warmup{}, false, err
	}
	return w, true, nil
}

// closedBy is the newest date over in every zone by now: the archive's
// newest day, which no chat anywhere still plays.
func closedBy(z round.Zones, now time.Time) (round.Date, bool) {
	d := z.Newest(now)
	if z.LastReveal(d).IsZero() {
		return round.Date{}, false
	}
	for z.LastReveal(d).After(now) {
		d = d.AddDays(-1)
	}
	return d, true
}

// openWarmup is the link of the group's warm-up while it takes answers, else
// empty: the home panel of a group shows it as long as there is one to play.
func (b *Bot) openWarmup(ctx context.Context, chatID int64) string {
	w, ok, err := b.store.WarmupOf(ctx, chatID)
	if err != nil {
		b.log.Warn("warm-up not read", "chat", chatID, "error", err)
		return ""
	}
	if !ok || !w.Open(b.opts.Now()) {
		return ""
	}
	link, err := b.warmupLink(w)
	if err != nil {
		b.log.Warn("warm-up link not signed", "chat", chatID, "error", err)
		return ""
	}
	return link
}

// warmupLink opens the Mini App on the warm-up: "w" and a payload signed for
// the group and the day, so a link from another group, or edited by hand,
// opens nothing.
func (b *Bot) warmupLink(w db.Warmup) (string, error) {
	if b.opts.Payloads == nil {
		return "", nil
	}
	payload, err := b.opts.Payloads.SignWarmup(auth.Payload{Date: w.Day, ChatID: w.ChatID})
	if err != nil {
		return "", err
	}
	return "https://t.me/" + b.opts.Username + "/play?startapp=w" + payload, nil
}

// pinLink asks Telegram to make maply an admin with the right to pin: the
// admin picks the group, and Telegram offers that one right ticked.
func (b *Bot) pinLink() string {
	if b.opts.Username == "" {
		return ""
	}
	return "https://t.me/" + b.opts.Username + "?" + url.Values{"startgroup": {"pin"}, "admin": {"pin_messages"}}.Encode()
}

// welcome is the group's first panel: what maply is and when it speaks in
// the quote, then what comes next, and the admins' note when maply may not
// pin. Its buttons are the home panel's, after the warm-up and the right to
// pin when there are those.
func (v view) welcome(zone, post, next, warm string, askPin bool, pinLink string) (string, telegram.Markup) {
	body := quote(v.t("welcome.body", "post", post, "time", i18n.At(v.lang, revealTime(), zone)))
	text := panel(productName, "", v.t("welcome.hint"), body) + "\n\n" + next
	if askPin {
		text += "\n\n<i>" + v.t("welcome.pin") + "</i>"
	}
	var rows telegram.Markup
	if warm != "" {
		rows = append(rows, []telegram.Button{{Text: v.t("btn.warmup"), URL: warm, Style: tg.StylePrimary}})
	}
	if askPin && pinLink != "" {
		rows = append(rows, []telegram.Button{{Text: v.t("btn.pin_right"), URL: pinLink}})
	}
	rows = append(rows,
		[]telegram.Button{v.button("btn.settings", actSettings), v.button("btn.help", actHelp)},
		[]telegram.Button{v.closeButton()},
	)
	return text, rows
}
