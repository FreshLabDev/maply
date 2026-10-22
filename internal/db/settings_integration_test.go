// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/FreshLabDev/tg"
)

// A group's language is the group's own: a member's choice never stands in for
// it, so every member reads the same zone off it.
func TestChatLanguageIsNotAMembers(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	admin := tg.User{ID: 61, FirstName: "Taras", LanguageCode: "uk"}
	group := &tg.Chat{ID: -1006101, Type: "supergroup"}
	if err := store.Touch(ctx, admin, group); err != nil {
		t.Fatal(err)
	}
	if lang, err := store.ChatLang(ctx, group.ID); err != nil || lang != "" {
		t.Fatalf("ChatLang before a choice = %q, %v", lang, err)
	}
	if err := store.SetChatLang(ctx, group.ID, "be"); err != nil {
		t.Fatal(err)
	}
	if lang, err := store.ChatLang(ctx, group.ID); err != nil || lang != "be" {
		t.Fatalf("ChatLang = %q, %v", lang, err)
	}
	// The admin's own language is untouched by the group's.
	if lang, err := store.UserLang(ctx, admin.ID); err != nil || lang != "uk" {
		t.Fatalf("UserLang = %q, %v", lang, err)
	}
	if err := store.ClearChatLang(ctx, group.ID); err != nil {
		t.Fatal(err)
	}
	if lang, err := store.ChatLang(ctx, group.ID); err != nil || lang != "" {
		t.Fatalf("ChatLang after clearing = %q, %v", lang, err)
	}

	// Picking by hand outranks the client; following Telegram brings it back.
	if err := store.SetUserLang(ctx, admin.ID, "de"); err != nil {
		t.Fatal(err)
	}
	if lang, _ := store.UserLang(ctx, admin.ID); lang != "de" {
		t.Fatalf("UserLang after a manual pick = %q", lang)
	}
	if err := store.Touch(ctx, admin, nil); err != nil {
		t.Fatal(err)
	}
	if lang, _ := store.UserLang(ctx, admin.ID); lang != "de" {
		t.Fatalf("a touch overrode the manual pick: %q", lang)
	}
	if err := store.ClearUserLang(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	if lang, _ := store.UserLang(ctx, admin.ID); lang != "uk" {
		t.Fatalf("UserLang after following Telegram = %q", lang)
	}
}

// The zone maply adopts on setup stays when the group's language changes, and
// a zone an admin picked is never overwritten by a later setup.
func TestChatZone(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const group, person = int64(-1006201), int64(62)
	if zone, err := store.ChatZone(ctx, group); err != nil || zone != "" {
		t.Fatalf("ChatZone of an unknown chat = %q, %v", zone, err)
	}
	if err := store.AdoptChatZone(ctx, group, "Europe/Kyiv"); err != nil {
		t.Fatal(err)
	}
	if err := store.AdoptChatZone(ctx, group, "Europe/Moscow"); err != nil {
		t.Fatal(err)
	}
	if zone, _ := store.ChatZone(ctx, group); zone != "Europe/Kyiv" {
		t.Fatalf("a second setup moved the zone to %q", zone)
	}
	if err := store.SetChatZone(ctx, group, "Asia/Tokyo"); err != nil {
		t.Fatal(err)
	}
	if zone, _ := store.ChatZone(ctx, group); zone != "Asia/Tokyo" {
		t.Fatalf("ChatZone after a pick = %q", zone)
	}
	// A DM is keyed by the person.
	if err := store.SetChatZone(ctx, person, "Europe/Minsk"); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := store.pool.QueryRow(ctx, `SELECT kind FROM chat_settings WHERE chat_id = $1`, person).Scan(&kind); err != nil || kind != "dm" {
		t.Fatalf("DM row kind = %q, %v", kind, err)
	}
}

// Stats counts only the days that have a result.
func TestStatsCountScoredDays(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	seedRound(t, store)
	if st, err := store.Stats(ctx, 7); err != nil || st.Days != 0 || st.MedianKm != nil {
		t.Fatalf("stats before any answer = %+v, %v", st, err)
	}
	exec(t, store, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key,
	                                        country_iso, continent, difficulty, reviewed_at)
	                OVERRIDING SYSTEM VALUE VALUES
	                (2, 'used', 'mapillary', 'm2', 'CC-BY-SA-4.0', 'photo', 1, 1, 'k2', 'UA', 'europe', 2, now()),
	                (3, 'used', 'mapillary', 'm3', 'CC-BY-SA-4.0', 'photo', 1, 1, 'k3', 'UA', 'europe', 2, now())`)
	exec(t, store, `INSERT INTO maply.round (date, day_no, place_id) VALUES ('2026-11-02', 2, 2), ('2026-11-03', 3, 3)`)
	exec(t, store, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus) VALUES
		('2026-11-01', 7, 'dm', 50, 30, '2026-11-01 10:00+03', '2026-11-01 20:00+03', 100, 3000, 800),
		('2026-11-02', 7, 'dm', 50, 30, '2026-11-02 10:00+03', '2026-11-02 20:00+03', 300, 2500, 0)`)
	// Today's answer has no distance yet.
	exec(t, store, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at) VALUES
		('2026-11-03', 7, 'dm', 50, 30, '2026-11-03 10:00+03', '2026-11-03 20:00+03')`)
	st, err := store.Stats(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if st.Days != 2 || st.Points != 6300 || st.Best != 3800 || st.MedianKm == nil || *st.MedianKm != 200 {
		t.Fatalf("stats = %+v (median %v)", st, st.MedianKm)
	}
}

func TestLeaveChatKeepsTheZone(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const group = int64(-1006301)
	if err := store.AdoptChatZone(ctx, group, "Europe/Minsk"); err != nil {
		t.Fatal(err)
	}
	if err := store.LeaveChat(ctx, group); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err := store.pool.QueryRow(ctx, `SELECT active FROM maply.chat_settings WHERE chat_id = $1`, group).Scan(&active); err != nil || active {
		t.Fatalf("active = %v, %v", active, err)
	}
	if err := store.AdoptChatZone(ctx, group, "Europe/Kyiv"); err != nil {
		t.Fatal(err)
	}
	if zone, _ := store.ChatZone(ctx, group); zone != "Europe/Minsk" {
		t.Fatalf("zone after coming back = %q", zone)
	}
}
