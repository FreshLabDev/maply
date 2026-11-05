// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/round"
)

// A season is the best twenty days of a month: missed days cost nothing
// while twenty remain, a day outside the month and a site answer count for
// nobody, and a chat's season holds only the people seen in it.
func TestSeasonSumsTheBestTwentyDays(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	for _, u := range []tg.User{{ID: 8, FirstName: "Ana"}, {ID: 9, FirstName: "Oleh"}} {
		if err := store.Touch(ctx, u, &tg.Chat{ID: -1001, Type: "supergroup", Title: "Maps"}); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, store, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key,
	                                        country_iso, continent, difficulty, reviewed_at)
	                OVERRIDING SYSTEM VALUE
	                SELECT g, 'used', 'mapillary', 'm' || g, 'CC-BY-SA-4.0', 'panorama', 50, 30, 'k' || g, 'UA', 'europe', 2, now()
	                  FROM generate_series(2, 31) g`)
	exec(t, store, `INSERT INTO maply.round (date, day_no, place_id)
	                SELECT DATE '2026-11-01' + (g - 1), g, g FROM generate_series(2, 30) g`)
	exec(t, store, `INSERT INTO maply.round (date, day_no, place_id) VALUES ('2026-10-31', 31, 31)`)
	const guesses = `INSERT INTO maply.guess (date, user_id, context, chat_id, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus, pts_speed)
	                 SELECT d, $1::bigint, 'dm', NULL, 50, 30, d + time '10:00', d + time '20:00', 100, $2::int, $3::int, 0
	                   FROM generate_series($4::date, $5::date, interval '1 day') d`
	// Pin: 25 days of 1000 and one of 5000, plus 5000 the day before the
	// month began. Twenty best: 5000 + 19 · 1000.
	exec(t, store, guesses, 7, 1000, 0, "2026-11-01", "2026-11-25")
	exec(t, store, `UPDATE maply.guess SET pts_km = 3500, pts_bonus = 1500 WHERE user_id = 7 AND date = '2026-11-03'`)
	exec(t, store, guesses, 7, 3500, 1500, "2026-10-31", "2026-10-31")
	// Ana: exactly twenty days of 1200, the same sum; Pin's best day is better.
	exec(t, store, guesses, 8, 1200, 0, "2026-11-01", "2026-11-20")
	// Oleh: fifteen days of 1500, short of twenty and still ranked.
	exec(t, store, guesses, 9, 1500, 0, "2026-11-10", "2026-11-24")
	// A site player with better days than anyone has no season.
	exec(t, store, `INSERT INTO maply.site_player (id, name) VALUES ('0d5e0000-0000-4000-8000-000000000001', 'Site')`)
	exec(t, store, `INSERT INTO maply.site_guess (date, player, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus)
	                SELECT d, '0d5e0000-0000-4000-8000-000000000001', 50, 30, d + time '19:00', d + time '20:00', 1, 3500, 1500
	                  FROM generate_series(DATE '2026-11-01', DATE '2026-11-30', interval '1 day') d`)

	first, last := round.NewDate(2026, 11, 1), round.NewDate(2026, 11, 30)
	world, err := store.Season(ctx, first, last, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []SeasonEntry{{7, "Pin", 24000, 20}, {8, "Ana", 24000, 20}, {9, "Oleh", 22500, 15}}
	if len(world) != len(want) {
		t.Fatalf("world season %+v, want %+v", world, want)
	}
	for i := range want {
		if world[i] != want[i] {
			t.Fatalf("world season %+v, want %+v", world, want)
		}
	}
	group, err := store.Season(ctx, first, last, -1001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(group) != 2 || group[0] != want[1] || group[1] != want[2] {
		t.Fatalf("group season %+v, want Ana then Oleh", group)
	}
	if top, err := store.Season(ctx, first, last, 0, 1); err != nil || len(top) != 1 || top[0].UserID != 7 {
		t.Fatalf("champion %+v, %v", top, err)
	}
	for _, c := range []struct {
		chat, user int64
		rank, of   int
	}{{0, 8, 2, 3}, {-1001, 8, 1, 2}, {-1001, 9, 2, 2}, {-1001, 7, 0, 0}} {
		r, err := store.SeasonOf(ctx, first, last, c.chat, c.user)
		if err != nil || r.Rank != c.rank || r.Of != c.of || r.UserID != c.user {
			t.Fatalf("season of %d in %d = %+v, %v; want %d of %d", c.user, c.chat, r, err, c.rank, c.of)
		}
	}
	// The month so far: on the 10th Oleh has not played yet.
	if r, err := store.SeasonOf(ctx, first, round.NewDate(2026, 11, 9), 0, 9); err != nil || r.Rank != 0 {
		t.Fatalf("Oleh before he played = %+v, %v", r, err)
	}
}
