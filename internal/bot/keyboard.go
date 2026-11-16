// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// The DM's reply keyboard. In a DM the menu button opens the Mini App, which
// hides the command list, so these three buttons are what a newcomer has to
// press. A group never gets it: there it would sit under every member's
// input field. There is no location button: a location in a DM is the
// day's answer, and one press must not send it.
const (
	keyPlay   = "play"
	keyResult = "result"
	keyMenu   = "menu"
)

var keyLabels = map[string]string{keyPlay: "btn.play", keyResult: "kb.result", keyMenu: "kb.menu"}

// keyTexts maps each button's label in every language to the button. The
// client sends a text button back as a plain message in the language the
// keyboard was drawn in, which may no longer be the person's.
var keyTexts = func() map[string]string {
	m := map[string]string{}
	for _, code := range i18n.Codes() {
		for key, label := range keyLabels {
			m[i18n.T(code, label)] = key
		}
	}
	return m
}()

// keyOf is the keyboard button a DM message is, or "".
func keyOf(m *tg.Message) string {
	if m.Chat.Type != "private" || m.From == nil || m.From.IsBot {
		return ""
	}
	return keyTexts[strings.TrimSpace(m.Text)]
}

// keyboard is Play over Today's result and Menu. Play opens the day in the
// Mini App; without one it is a text button that sends the menu.
func (b *Bot) keyboard(lang string) telegram.Keyboard {
	play := telegram.KeyButton{Text: i18n.T(lang, "btn.play"), Style: tg.StylePrimary}
	if b.opts.AppURL != "" {
		play.WebApp = appPage(b.opts.AppURL, "/today")
	}
	return telegram.Keyboard{
		Rows: [][]telegram.KeyButton{
			{play},
			{{Text: i18n.T(lang, "kb.result")}, {Text: i18n.T(lang, "kb.menu")}},
		},
		Placeholder: i18n.T(lang, "kb.placeholder"),
	}
}

// sendKeyboard puts the keyboard under a DM's input field, in lang. Any
// other chat gets nothing.
func (b *Bot) sendKeyboard(ctx context.Context, chat tg.Chat, lang string) error {
	if chat.Type != "private" {
		return nil
	}
	if _, err := b.tg.SendKeyboard(ctx, chat.ID, i18n.T(lang, "kb.ready"), b.keyboard(lang)); err != nil {
		return fmt.Errorf("send keyboard to %d: %w", chat.ID, err)
	}
	return nil
}

// onStart answers /start: in a DM the keyboard first, then the panel, so
// the panel is what the person sees last.
func (b *Bot) onStart(ctx context.Context, m *tg.Message) error {
	if m.Chat.Type == "private" {
		raw, err := b.personLang(ctx, *m.From)
		if err != nil {
			return err
		}
		if err := b.sendKeyboard(ctx, m.Chat, i18n.Resolve(raw)); err != nil {
			// The panel still works without the keyboard.
			b.log.Warn("keyboard not sent", "error", err)
		}
	}
	return b.sendHome(ctx, m)
}

func (b *Bot) onKey(ctx context.Context, m *tg.Message, key string) error {
	if key == keyResult {
		return b.sendToday(ctx, m)
	}
	return b.sendHome(ctx, m)
}

// sendToday is the person's day, read in their DM's zone. Before the DM's
// reveal it says whether they have answered and how long until the result.
// From the reveal it gives their score and their place in the world, the
// same rank the Mini App shows without names, with the result one tap away.
func (b *Bot) sendToday(ctx context.Context, m *tg.Message) error {
	raw, err := b.personLang(ctx, *m.From)
	if err != nil {
		return err
	}
	lang := i18n.Resolve(raw)
	zone, err := b.store.ChatZone(ctx, m.From.ID)
	if err != nil {
		return err
	}
	loc := b.opts.Zones.For(zone, raw)
	now := b.opts.Now()
	d := round.DateIn(now, loc)
	say := func(text string, markup telegram.Markup) error {
		_, err := b.tg.SendText(ctx, m.Chat.ID, text, markup)
		return err
	}
	if _, ok, err := b.store.RoundOn(ctx, d); err != nil {
		return err
	} else if !ok {
		return say(i18n.T(lang, "today.none"), nil)
	}
	guess, answered, err := b.store.GuessOf(ctx, d, m.From.ID)
	if err != nil {
		return err
	}
	reveal := d.Reveal(loc)
	if now.Before(reveal) {
		key := "today.answered"
		if !answered {
			// An earlier reveal in one of their groups may have closed
			// the day for them already.
			deadline, err := b.deadline(ctx, m.From.ID, d, loc)
			if err != nil {
				return err
			}
			if !now.Before(deadline) {
				return say(i18n.T(lang, "loc.closed"), nil)
			}
			key = "today.waiting"
		}
		return say(i18n.T(lang, key,
			"time", i18n.At(lang, revealTime(), round.Name(zone, raw)),
			"left", until(lang, reveal.Sub(now))), nil)
	}
	if !answered {
		return say(i18n.T(lang, "result.missed"), nil)
	}
	world, err := b.store.World(ctx, d)
	if err != nil {
		return err
	}
	lines := []string{"<b>" + i18n.T(lang, "kb.result") + "</b>"}
	if guess.Km != nil {
		score := i18n.T(lang, "app.km_points",
			"points", i18n.FormatNumber(lang, guess.Total()), "km", i18n.FormatDistance(lang, *guess.Km))
		lines = append(lines, i18n.T(lang, "today.score", "score", score))
	}
	if rank := world.RankOf(m.From.ID); rank > 0 {
		// Zones still before their reveal are still answering.
		key := "today.world"
		if now.Before(b.opts.Zones.LastReveal(d)) {
			key = "result.world_now"
		}
		lines = append(lines, i18n.T(lang, key,
			"rank", i18n.FormatNumber(lang, rank), "n", i18n.FormatNumber(lang, world.Players())))
	}
	var markup telegram.Markup
	if b.opts.AppURL != "" {
		markup = telegram.Markup{{{Text: i18n.T(lang, "btn.result"), WebApp: appPage(b.opts.AppURL, "/today"), Style: tg.StylePrimary}}}
	}
	return say(strings.Join(lines, "\n"), markup)
}

// until is how long is left, rounded up to the minute so it never reads 0.
func until(lang string, d time.Duration) string {
	mins := int(math.Ceil(d.Minutes()))
	if mins < 1 {
		mins = 1
	}
	if h := mins / 60; h > 0 {
		return i18n.T(lang, "unit.hm", "h", strconv.Itoa(h), "m", strconv.Itoa(mins%60))
	}
	return i18n.T(lang, "unit.min", "m", strconv.Itoa(mins))
}
