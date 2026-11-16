// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
)

// A label that meant two buttons in two languages would do the wrong one.
func TestKeyLabelsAreUnambiguous(t *testing.T) {
	seen := map[string]string{}
	for _, code := range i18n.Codes() {
		for key, label := range keyLabels {
			text := i18n.T(code, label)
			if other, ok := seen[text]; ok && other != key {
				t.Fatalf("%q is both %s and %s", text, other, key)
			}
			seen[text] = key
		}
	}
}

// Every language's label is its button in a DM, whatever the person's
// language is now; in a group, or from a bot, it is just text.
func TestKeyOfMatchesEveryLanguageInDMsOnly(t *testing.T) {
	for _, code := range i18n.Codes() {
		for key, label := range keyLabels {
			text := i18n.T(code, label)
			m := command(private(member), member, text).Message
			if got := keyOf(m); got != key {
				t.Fatalf("%s %q = %q, want %q", code, text, got, key)
			}
			if got := keyOf(command(group, member, text).Message); got != "" {
				t.Fatalf("%s %q in a group = %q", code, text, got)
			}
			bot := tg.User{ID: 7, IsBot: true}
			if got := keyOf(command(private(bot), bot, text).Message); got != "" {
				t.Fatalf("a bot's %q = %q", text, got)
			}
		}
	}
	for _, text := range []string{"", "hello", "/start", "Меню please"} {
		if got := keyOf(command(private(member), member, text).Message); got != "" {
			t.Fatalf("%q = %q", text, got)
		}
	}
}

func TestStartSendsTheKeyboardInDMsOnly(t *testing.T) {
	b, _, client := testBot(t)
	ctx := context.Background()
	b.handle(ctx, command(private(member), member, "/start"))
	if len(client.keyboards) != 1 || len(client.sends) != 1 {
		t.Fatalf("keyboards = %d, sends = %d, want the keyboard and the panel", len(client.keyboards), len(client.sends))
	}
	k := client.keyboards[0]
	if k.chatID != userID || k.text != i18n.T("ru", "kb.ready") || k.kb.Placeholder != i18n.T("ru", "kb.placeholder") {
		t.Fatalf("keyboard = %+v", k)
	}
	rows := k.kb.Rows
	if len(rows) != 2 || len(rows[0]) != 1 || len(rows[1]) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	play := rows[0][0]
	if play.Text != "Играть" || play.WebApp != "https://maply.example/today" || play.Style != tg.StylePrimary {
		t.Fatalf("play = %+v", play)
	}
	if rows[1][0].Text != "Итог дня" || rows[1][1].Text != "Меню" || rows[1][0].WebApp != "" || rows[1][1].WebApp != "" {
		t.Fatalf("second row = %+v", rows[1])
	}

	// A group gets its panel and never the keyboard, nor an answer to a
	// label typed there.
	b.handle(ctx, command(group, admin, "/start"))
	b.handle(ctx, command(group, member, "Меню"))
	b.handle(ctx, command(group, member, "Итог дня"))
	if len(client.keyboards) != 1 || len(client.sends) != 2 {
		t.Fatalf("a group got keyboards %d, sends %d", len(client.keyboards), len(client.sends))
	}
	if err := b.sendKeyboard(ctx, group, "ru"); err != nil || len(client.keyboards) != 1 {
		t.Fatalf("sendKeyboard to a group: err %v, keyboards %d", err, len(client.keyboards))
	}
}

// Menu sends the home panel, in any language's label; a label is never
// taken for anything else.
func TestMenuButtonSendsTheHome(t *testing.T) {
	b, store, client := testBot(t)
	ctx := context.Background()
	b.handle(ctx, command(private(member), member, "Меню"))
	b.handle(ctx, command(private(member), member, "Menu"))
	if len(client.sends) != 2 || len(client.keyboards) != 0 {
		t.Fatalf("sends = %d, keyboards = %d", len(client.sends), len(client.keyboards))
	}
	for _, s := range client.sends {
		if !strings.Contains(s.text, i18n.T("ru", "home.hint")) {
			t.Fatalf("menu = %q", s.text)
		}
	}
	if len(store.guesses) != 0 {
		t.Fatal("a button counted as an answer")
	}
}

func TestTodaysResult(t *testing.T) {
	b, store, client := testBot(t)
	ctx := context.Background()
	today := func() string {
		b.handle(ctx, command(private(member), member, "Итог дня"))
		return client.sends[len(client.sends)-1].text
	}
	if got := today(); got != i18n.T("ru", "today.none") {
		t.Fatalf("no round: %q", got)
	}
	d := round.DateIn(winterDay, b.opts.Zones.For("", "ru"))
	store.rounds[d.String()] = true
	// 14:00 in Kyiv, six hours before the result.
	if got, want := today(), i18n.T("ru", "today.waiting", "time", "20:00 (Киев)", "left", "6 ч 0 мин"); got != want {
		t.Fatalf("before answering: %q, want %q", got, want)
	}
	b.handle(ctx, location(member, 48.85, 2.35))
	b.opts.Now = func() time.Time { return winterDay.Add(5*time.Hour + 30*time.Second) }
	if got, want := today(), i18n.T("ru", "today.answered", "time", "20:00 (Киев)", "left", "1 ч 0 мин"); got != want {
		t.Fatalf("after answering: %q, want %q", got, want)
	}

	// 21:00 in Kyiv: the score and the place in the world, which the zones
	// to the west are still adding to.
	b.opts.Now = func() time.Time { return winterDay.Add(7 * time.Hour) }
	km, ptsKm, bonus := 312.0, 3000, 500
	store.scored = map[int64]db.Guess{userID: {UserID: userID, Km: &km, PtsKm: &ptsKm, PtsBonus: &bonus}}
	store.world = db.World{Entries: []db.Entry{{Rank: 1, UserID: 99}, {Rank: 2, UserID: userID}, {Rank: 3, UserID: 98}}}
	got := today()
	for _, want := range []string{"3 500 за 312 км", i18n.T("ru", "result.world_now", "rank", "2", "n", "3")} {
		if !strings.Contains(strings.ReplaceAll(got, " ", " "), want) {
			t.Fatalf("after the reveal: %q, want %q in it", got, want)
		}
	}
	m := client.sends[len(client.sends)-1].markup
	if len(m) != 1 || m[0][0].Text != i18n.T("ru", "btn.result") || m[0][0].WebApp != "https://maply.example/today" {
		t.Fatalf("result button = %+v", m)
	}

	// Someone who did not answer is told so, and gets no button.
	b.handle(ctx, command(private(admin), admin, "Підсумок дня"))
	last := client.sends[len(client.sends)-1]
	if last.text != i18n.T("uk", "result.missed") || last.markup != nil {
		t.Fatalf("missed = %+v", last)
	}
}

// Before the DM's reveal, an earlier reveal in a group may have closed the
// day; the button says so instead of counting down.
func TestTodaysResultAfterTheDeadline(t *testing.T) {
	b, store, client := testBot(t)
	b.store = deadlineStore{store, winterDay.Add(-time.Hour)}
	d := round.DateIn(winterDay, b.opts.Zones.For("", "ru"))
	store.rounds[d.String()] = true
	b.handle(context.Background(), command(private(member), member, "Итог дня"))
	if got := client.sends[0].text; got != i18n.T("ru", "loc.closed") {
		t.Fatalf("closed = %q", got)
	}
}

type deadlineStore struct {
	*fakeStore
	at time.Time
}

func (s deadlineStore) Deadline(context.Context, round.Zones, int64, round.Date, *time.Location) (time.Time, error) {
	return s.at, nil
}

// A new language brings the keyboard again in it; the same one does not.
func TestLanguageChangeResendsTheKeyboard(t *testing.T) {
	b, _, client := testBot(t)
	ctx := context.Background()
	b.handle(ctx, press(private(member), member, cb(userID, "lang:en")))
	if len(client.keyboards) != 1 || client.keyboards[0].kb.Rows[1][1].Text != "Menu" {
		t.Fatalf("keyboards = %+v", client.keyboards)
	}
	b.handle(ctx, press(private(member), member, cb(userID, "lang:en")))
	if len(client.keyboards) != 1 {
		t.Fatalf("the same language sent the keyboard again: %d", len(client.keyboards))
	}
	// Following Telegram goes back to the client's Russian.
	b.handle(ctx, press(private(member), member, cb(userID, actLangAuto)))
	if len(client.keyboards) != 2 || client.keyboards[1].kb.Rows[1][1].Text != "Меню" {
		t.Fatalf("keyboards = %+v", client.keyboards)
	}
	// A forged language press in a group changes no keyboard.
	b.handle(ctx, press(group, member, cb(userID, "lang:de")))
	b.handle(ctx, press(group, member, cb(userID, actLangAuto)))
	if len(client.keyboards) != 2 {
		t.Fatalf("a group got a keyboard: %d", len(client.keyboards))
	}
}
