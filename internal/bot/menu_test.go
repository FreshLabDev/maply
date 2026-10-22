// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// A Telegram user id is up to 52 bits; the longest owner keeps every callback
// honest about Telegram's 64-byte limit.
const longOwner = int64(4503599627370495)

var winterDay = time.Date(2026, time.December, 10, 12, 0, 0, 0, time.UTC)

func loadZones(t *testing.T) round.Zones {
	t.Helper()
	z, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	return z
}

type drawn struct {
	name   string
	text   string
	markup telegram.Markup
}

func everyPanel(t *testing.T, v view) []drawn {
	t.Helper()
	median := 1234.4
	var out []drawn
	add := func(name string, text string, m telegram.Markup) { out = append(out, drawn{name, text, m}) }
	text, m := v.home("https://maply.example/", true)
	add("home", text, m)
	text, m = v.help()
	add("help", text, m)
	text, m = v.about("v0.1.0")
	add("about", text, m)
	text, m = v.language()
	add("language", text, m)
	text, m = v.groupLanguage("uk")
	add("group language", text, m)
	text, m = v.settings("America/Los_Angeles", "")
	add("settings", text, m)
	text, m = v.zone("America/Los_Angeles", loadZones(t), winterDay)
	add("zone", text, m)
	text, m = v.stats(db.Stats{Days: 12, Points: 41234, Best: 4999, MedianKm: &median})
	add("stats", text, m)
	text, m = v.stats(db.Stats{})
	add("empty stats", text, m)
	return out
}

func TestEveryPanelIsOneShape(t *testing.T) {
	for _, lang := range i18n.Codes() {
		for _, sc := range []scope{scopePrivate, scopeGroup} {
			for _, p := range everyPanel(t, view{lang: lang, owner: longOwner, sc: sc}) {
				lines := strings.SplitN(p.text, "\n", 3)
				if !strings.HasPrefix(lines[0], "<b>") || len(lines) < 2 ||
					!strings.HasPrefix(lines[1], "<i>") || !strings.HasSuffix(lines[1], "</i>") {
					t.Errorf("%s/%s: not title, hint: %q", lang, p.name, p.text)
				}
				if len(lines) == 3 && !strings.HasPrefix(lines[2], "\n<blockquote>") {
					t.Errorf("%s/%s: the body is not quoted: %q", lang, p.name, p.text)
				}
				if strings.Contains(p.text, "[") || strings.Contains(p.text, "{") {
					t.Errorf("%s/%s: a key or placeholder leaked: %q", lang, p.name, p.text)
				}
				for _, row := range p.markup {
					for _, b := range row {
						if len(b.CallbackData) > 64 {
							t.Errorf("%s/%s: callback %q is %d bytes", lang, p.name, b.CallbackData, len(b.CallbackData))
						}
						if strings.Contains(b.Text, "[") || strings.Contains(b.Text, "{") {
							t.Errorf("%s/%s: button %q", lang, p.name, b.Text)
						}
					}
				}
			}
		}
	}
}

func buttons(m telegram.Markup) []telegram.Button {
	var all []telegram.Button
	for _, row := range m {
		all = append(all, row...)
	}
	return all
}

func has(m telegram.Markup, action string) bool {
	for _, b := range buttons(m) {
		if strings.HasSuffix(b.CallbackData, ":"+action) {
			return true
		}
	}
	return false
}

// Close exists only where the panel is a message in somebody else's feed.
func TestCloseOnlyInGroups(t *testing.T) {
	for _, sc := range []scope{scopePrivate, scopeGroup} {
		for _, p := range everyPanel(t, view{lang: "en", owner: 1, sc: sc}) {
			if got := has(p.markup, actClose); got != (sc == scopeGroup) {
				t.Errorf("scope %d, %s: close = %v", sc, p.name, got)
			}
		}
	}
}

func TestHome(t *testing.T) {
	dm := view{lang: "en", owner: 1, sc: scopePrivate}
	_, m := dm.home("https://maply.example/", false)
	play := m[0][0]
	if play.WebApp != "https://maply.example/" || play.Style != tg.StylePrimary {
		t.Fatalf("play = %+v", play)
	}
	if _, m := dm.home("", false); m[0][0].WebApp != "" {
		t.Fatal("a home without APP_URL offered Play")
	}

	group := view{lang: "en", owner: 1, sc: scopeGroup}
	_, member := group.home("https://maply.example/", false)
	_, admin := group.home("https://maply.example/", true)
	if has(member, actSettings) || !has(admin, actSettings) {
		t.Fatal("Settings must be on an admin's group home and only there")
	}
	for _, b := range buttons(admin) {
		if b.WebApp != "" {
			t.Fatal("Telegram refuses a web_app button in a group")
		}
	}
}

// One option of a set is green, and it is the current one.
func TestLanguageGridMarksTheCurrentOne(t *testing.T) {
	_, m := view{lang: "be", owner: 1, sc: scopePrivate}.language()
	var green []string
	options := 0
	for _, b := range buttons(m) {
		if strings.Contains(b.CallbackData, ":lang:") {
			options++
		}
		if b.Style == tg.StyleSuccess {
			green = append(green, b.CallbackData)
		}
	}
	if options != len(i18n.LANGUAGE_OPTIONS) || len(green) != 1 || green[0] != "m:1:lang:be" {
		t.Fatalf("options = %d, green = %v", options, green)
	}
	if !has(m, actLangAuto) {
		t.Fatal("no way back to following Telegram")
	}
}

func TestZoneButtonsShowTodaysOffset(t *testing.T) {
	_, m := view{lang: "en", owner: 1, sc: scopeGroup}.zone("Asia/Kolkata", loadZones(t), winterDay)
	labels := map[string]telegram.Button{}
	for _, b := range buttons(m) {
		if name, ok := strings.CutPrefix(b.CallbackData, "m:1:zone:"); ok {
			labels[name] = b
		}
	}
	if len(labels) != len(round.ZoneChoices) {
		t.Fatalf("%d zone buttons", len(labels))
	}
	for name, want := range map[string]string{
		"Asia/Kolkata":      "UTC+5:30",
		"America/Sao_Paulo": "UTC−3",
		"Europe/Kyiv":       "UTC+2",
		"Australia/Sydney":  "UTC+11",
		"Europe/London":     "· UTC",
	} {
		if !strings.HasSuffix(labels[name].Text, want) {
			t.Errorf("%s: %q, want …%s", name, labels[name].Text, want)
		}
	}
	if labels["Asia/Kolkata"].Style != tg.StyleSuccess {
		t.Fatal("the current zone is not marked")
	}
}

func TestStatsFormatNumbersPerLanguage(t *testing.T) {
	median := 1234.6
	st := db.Stats{Days: 3, Points: 41234, Best: 4999, MedianKm: &median}
	en, _ := view{lang: "en", sc: scopePrivate}.stats(st)
	de, _ := view{lang: "de", sc: scopePrivate}.stats(st)
	if !strings.Contains(en, "41,234") || !strings.Contains(en, "1,235 km") {
		t.Fatalf("en = %q", en)
	}
	if !strings.Contains(de, "41.234") {
		t.Fatalf("de = %q", de)
	}
}

// --- presses ------------------------------------------------------------

const (
	groupID = int64(-1001)
	adminID = int64(11)
	userID  = int64(22)
)

func testBot(t *testing.T) (*Bot, *fakeStore, *fakeTelegram) {
	t.Helper()
	store := newStore()
	client := &fakeTelegram{members: map[int64]string{adminID: "administrator", userID: "member"}}
	b := New(store, client, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		Username: "maplyin_bot", Version: "v0.1.0", AppURL: "https://maply.example/",
		Zones: loadZones(t), Now: func() time.Time { return winterDay },
	})
	return b, store, client
}

func command(chat tg.Chat, from tg.User, text string) tg.Update {
	return tg.Update{Message: &tg.Message{MessageID: 5, From: &from, Chat: chat, Text: text}}
}

func press(chat tg.Chat, from tg.User, data string) tg.Update {
	return tg.Update{Callback: &tg.CallbackQuery{ID: "q", From: from, Data: data,
		Message: tg.Message{MessageID: 100, Chat: chat, Text: "maply"}}}
}

var (
	group   = tg.Chat{ID: groupID, Type: "supergroup"}
	admin   = tg.User{ID: adminID, FirstName: "Olena", LanguageCode: "uk"}
	member  = tg.User{ID: userID, FirstName: "Pavel", LanguageCode: "ru"}
	private = func(u tg.User) tg.Chat { return tg.Chat{ID: u.ID, Type: "private"} }
)

func TestStartIsOursOnly(t *testing.T) {
	b, _, client := testBot(t)
	ctx := context.Background()
	for _, text := range []string{"/start", "/start@MaplyIn_Bot", "/start payload", "/start@voicybot", "/help", "start"} {
		b.handle(ctx, command(group, member, text))
	}
	if len(client.sends) != 3 {
		t.Fatalf("answered %d of the commands, want 3", len(client.sends))
	}
}

// A group's panel speaks the group's language, and a group that never set a
// zone gets the one its language implies.
func TestGroupStartAdoptsAZone(t *testing.T) {
	b, store, client := testBot(t)
	store.chatLang[groupID] = "be"
	b.handle(context.Background(), command(group, member, "/start"))
	if store.zones[groupID] != "Europe/Minsk" {
		t.Fatalf("zone = %q", store.zones[groupID])
	}
	if got := client.sends[0].text; !strings.Contains(got, i18n.T("be", "home.hint")) {
		t.Fatalf("panel = %q", got)
	}
	if has(client.sends[0].markup, actSettings) {
		t.Fatal("a member was offered Settings")
	}
}

func TestOnlyTheOwnerPresses(t *testing.T) {
	b, store, client := testBot(t)
	b.handle(context.Background(), press(group, member, cb(adminID, "zone:Europe/Berlin")))
	if len(client.alerts) != 1 || client.alerts[0] != i18n.T("ru", "msg.not_yours") {
		t.Fatalf("alerts = %v", client.alerts)
	}
	if store.zones[groupID] != "" || len(client.edits) != 0 {
		t.Fatal("somebody else's press changed the panel")
	}
}

// The admin check runs on the press itself: a member forging their own id
// into a Settings callback is refused.
func TestGroupSettingsAreAdminOnly(t *testing.T) {
	b, store, client := testBot(t)
	ctx := context.Background()
	b.handle(ctx, press(group, member, cb(userID, "zone:Europe/Berlin")))
	if len(client.alerts) != 1 || client.alerts[0] != i18n.T("ru", "msg.admin_only") || store.zones[groupID] != "" {
		t.Fatalf("alerts = %v, zone = %q", client.alerts, store.zones[groupID])
	}

	b.handle(ctx, press(group, admin, cb(adminID, "zone:Europe/Berlin")))
	if store.zones[groupID] != "Europe/Berlin" || len(client.edits) != 1 {
		t.Fatalf("zone = %q, edits = %d", store.zones[groupID], len(client.edits))
	}
	// Only a listed zone is a choice.
	b.handle(ctx, press(group, admin, cb(adminID, "zone:Pacific/Kiritimati")))
	if store.zones[groupID] != "Europe/Berlin" {
		t.Fatalf("zone = %q", store.zones[groupID])
	}

	b.handle(ctx, press(group, admin, cb(adminID, "glang:pl")))
	if store.chatLang[groupID] != "pl" || !strings.Contains(client.edits[len(client.edits)-1].text, i18n.T("pl", "lang.group_hint")) {
		t.Fatalf("group language = %q", store.chatLang[groupID])
	}
	// The admin's own language is not the group's.
	if store.userLang[adminID] != "" {
		t.Fatal("the group's language was written to the admin")
	}
}

func TestPersonalLanguageAndZone(t *testing.T) {
	b, store, client := testBot(t)
	ctx := context.Background()
	b.handle(ctx, press(private(member), member, cb(userID, "lang:de")))
	if store.userLang[userID] != "de" || !strings.Contains(client.edits[0].text, i18n.T("de", "lang.hint")) {
		t.Fatalf("lang = %q, panel = %q", store.userLang[userID], client.edits[0].text)
	}
	// A person's zone follows their language until they pick one.
	b.handle(ctx, press(private(member), member, cb(userID, "set")))
	if got := client.edits[1].text; !strings.Contains(got, i18n.T("de", "zone.Europe/Berlin")) {
		t.Fatalf("settings = %q", got)
	}
	b.handle(ctx, press(private(member), member, cb(userID, "zone:Asia/Tokyo")))
	if store.zones[userID] != "Asia/Tokyo" {
		t.Fatalf("zone = %q", store.zones[userID])
	}
	b.handle(ctx, press(private(member), member, cb(userID, "langauto")))
	if store.userLang[userID] != "" || !strings.Contains(client.edits[3].text, i18n.T("ru", "lang.hint")) {
		t.Fatalf("after following Telegram: %q", client.edits[3].text)
	}
	if len(client.alerts) != 0 {
		t.Fatalf("alerts = %v", client.alerts)
	}
}

func TestCloseDeletesOnlyAGroupPanel(t *testing.T) {
	b, _, client := testBot(t)
	ctx := context.Background()
	b.handle(ctx, press(private(member), member, cb(userID, "close")))
	if len(client.deleted) != 0 {
		t.Fatal("closed a direct chat's panel")
	}
	// The morning post is a photo; a forged Close against it is ignored.
	post := press(group, member, cb(userID, "close"))
	post.Callback.Message.Text, post.Callback.Message.Photo = "", []tg.Photo{{FileID: "F"}}
	b.handle(ctx, post)
	if len(client.deleted) != 0 {
		t.Fatal("a forged Close deleted the morning post")
	}
	b.handle(ctx, press(group, member, cb(userID, "close")))
	if len(client.deleted) != 1 || client.deleted[0] != 100 {
		t.Fatalf("deleted = %v", client.deleted)
	}
}

func TestJoiningAndLeaving(t *testing.T) {
	b, store, _ := testBot(t)
	ctx := context.Background()
	membership := func(from tg.User, old, new string) tg.Update {
		return tg.Update{MyChatMember: &tg.ChatMemberUpdated{Chat: group, From: from,
			OldChatMember: tg.ChatMember{Status: old}, NewChatMember: tg.ChatMember{Status: new}}}
	}
	// The group has no language yet, so the person who added maply decides.
	b.handle(ctx, membership(member, "left", "member"))
	if store.zones[groupID] != "Europe/Kyiv" {
		t.Fatalf("zone = %q", store.zones[groupID])
	}
	// A promotion is not a second setup.
	b.handle(ctx, membership(admin, "member", "administrator"))
	b.handle(ctx, membership(admin, "administrator", "kicked"))
	if len(store.left) != 1 || store.left[0] != groupID {
		t.Fatalf("left = %v", store.left)
	}
}
