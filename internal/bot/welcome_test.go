// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/schedule"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// welcomeBot is a bot whose clock the test moves, with a signer for the
// warm-up's link.
func welcomeBot(t *testing.T, now *time.Time) (*Bot, *fakeStore, *fakeTelegram, *auth.Signer) {
	t.Helper()
	store := newStore()
	client := &fakeTelegram{members: map[int64]string{adminID: "administrator", userID: "member"}}
	signer := auth.NewSigner("test-token")
	b := New(store, client, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		Username: "maplyin_bot", Version: "v0.1.0", AppURL: "https://maply.example/",
		Zones: loadZones(t), Now: func() time.Time { return *now }, Payloads: signer,
	})
	return b, store, client, signer
}

func added(chat tg.Chat, by tg.User, status string) tg.Update {
	return tg.Update{MyChatMember: &tg.ChatMemberUpdated{Chat: chat, From: by,
		OldChatMember: tg.ChatMember{Status: "left"}, NewChatMember: tg.ChatMember{Status: status}}}
}

func urlOf(m telegram.Markup, text string) string {
	for _, b := range buttons(m) {
		if b.Text == text {
			return b.URL
		}
	}
	return ""
}

// A group hears from maply the moment it is added, and what it hears
// depends on where its day stands: before the morning post, during the day,
// and after the reveal, when it gets a warm-up instead.
func TestWelcomeFollowsTheGroupsDay(t *testing.T) {
	ctx := context.Background()
	// Kyiv is UTC+2 in December.
	at := func(h, m int) time.Time { return time.Date(2026, time.December, 10, h, m, 0, 0, time.UTC) }
	cases := []struct {
		name string
		now  time.Time
		next string
	}{
		{"before the post", at(4, 30), i18n.T("uk", "welcome.morning", "post", "07:00")},
		{"during the day", at(12, 0), i18n.T("uk", "welcome.today")},
		{"after the reveal, nothing to play", at(19, 0), i18n.T("uk", "welcome.tomorrow", "post", "07:00")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := c.now
			b, store, client, _ := welcomeBot(t, &now)
			b.handle(ctx, added(group, admin, "administrator"))
			if len(client.sends) != 1 || client.sends[0].chatID != groupID {
				t.Fatalf("sends = %+v", client.sends)
			}
			w := client.sends[0]
			if !strings.Contains(w.text, c.next) || !strings.Contains(w.text, "20:00") {
				t.Fatalf("welcome = %q, want %q", w.text, c.next)
			}
			if urlOf(w.markup, i18n.T("uk", "btn.warmup")) != "" || len(store.jobs) != 0 {
				t.Fatalf("a warm-up with nothing to warm up on: %+v, jobs %v", w.markup, store.jobs)
			}
			if !has(w.markup, actSettings) || !has(w.markup, actClose) {
				t.Fatalf("no settings in the welcome: %+v", w.markup)
			}
		})
	}
}

// After its reveal a new group gets a warm-up on the archive's newest day,
// with a link signed for the group and the day, and its result an hour on.
func TestWelcomeAfterTheRevealWarmsUp(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.December, 10, 19, 0, 0, 0, time.UTC) // 21:00 in Kyiv
	b, store, client, signer := welcomeBot(t, &now)
	store.warmDay = round.NewDate(2026, time.December, 9)
	b.handle(ctx, added(group, admin, "administrator"))

	w := client.sends[0]
	if !strings.Contains(w.text, i18n.T("uk", "welcome.warmup", "time", "22:00")) {
		t.Fatalf("welcome = %q", w.text)
	}
	link := urlOf(w.markup, i18n.T("uk", "btn.warmup"))
	u, err := url.Parse(link)
	if err != nil || !strings.HasPrefix(link, "https://t.me/maplyin_bot/play?") {
		t.Fatalf("warm-up link = %q", link)
	}
	raw, ok := strings.CutPrefix(u.Query().Get("startapp"), "w")
	if !ok {
		t.Fatalf("startapp = %q", u.Query().Get("startapp"))
	}
	p, err := signer.VerifyWarmup(raw)
	if err != nil || p.ChatID != groupID || p.Date.String() != "2026-12-09" {
		t.Fatalf("payload = %+v, %v", p, err)
	}
	// The day's post payload does not open a warm-up, nor the other way.
	if _, err := signer.Verify(raw); err == nil {
		t.Fatal("a warm-up payload opens a day")
	}
	want := schedule.JobWarmup + ":2026-12-10:-1001@2026-12-10T20:00:00Z"
	if len(store.jobs) != 1 || store.jobs[0] != want {
		t.Fatalf("jobs = %v, want %s", store.jobs, want)
	}

	// /start in the group offers it while it takes answers.
	b.handle(ctx, command(group, member, "/start"))
	// The member reads it in their language: the group has none yet.
	if urlOf(client.sends[1].markup, i18n.T("ru", "btn.warmup")) != link {
		t.Fatalf("home = %+v", client.sends[1].markup)
	}
	// Removed and added back within the hour: the same warm-up, planned once.
	b.handle(ctx, added(group, admin, "administrator"))
	if urlOf(client.sends[2].markup, i18n.T("uk", "btn.warmup")) != link || len(store.jobs) != 1 {
		t.Fatalf("second welcome = %+v, jobs %v", client.sends[2], store.jobs)
	}
	// Once it is over, home has no warm-up and a new welcome says tomorrow.
	now = now.Add(time.Hour)
	b.handle(ctx, command(group, member, "/start"))
	if urlOf(client.sends[3].markup, i18n.T("ru", "btn.warmup")) != "" {
		t.Fatalf("home after the warm-up = %+v", client.sends[3].markup)
	}
	b.handle(ctx, added(group, admin, "administrator"))
	if !strings.Contains(client.sends[4].text, i18n.T("uk", "welcome.tomorrow", "post", "07:00")) {
		t.Fatalf("welcome after the warm-up = %q", client.sends[4].text)
	}
}

// A supergroup where maply is not an admin hears once that maply would pin
// the post, with a link that asks for that one right; the morning post does
// not ask again.
func TestWelcomeAsksForThePinOnce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.December, 10, 12, 0, 0, 0, time.UTC)
	b, store, client, _ := welcomeBot(t, &now)
	b.handle(ctx, added(group, admin, "member"))
	w := client.sends[0]
	if !strings.Contains(w.text, i18n.T("uk", "welcome.pin")) || !store.pinWarned[groupID] {
		t.Fatalf("welcome = %q, warned = %v", w.text, store.pinWarned[groupID])
	}
	link := urlOf(w.markup, i18n.T("uk", "btn.pin_right"))
	if link != "https://t.me/maplyin_bot?admin=pin_messages&startgroup=pin" {
		t.Fatalf("pin link = %q", link)
	}
	// An admin, or a basic group where members pin, is not asked.
	for _, u := range []tg.Update{added(group, admin, "administrator"), added(tg.Chat{ID: -7, Type: "group"}, admin, "member")} {
		b.handle(ctx, u)
		if last := client.sends[len(client.sends)-1]; strings.Contains(last.text, i18n.T("uk", "welcome.pin")) {
			t.Fatalf("asked again: %q", last.text)
		}
	}
}
