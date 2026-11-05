// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/round"
)

func TestProfileOfReadsAnswersRoundsAndPlaces(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	exec(t, store, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus, pts_speed)
	                VALUES ('2026-11-01', 7, 'dm', 48.85, 2.35, '2026-11-01 10:00+03', '2026-11-01 20:00+03', 2000, 1000, 300, 50)`)
	d := round.NewDate(2026, 11, 1)
	p, err := store.ProfileOf(ctx, 7, d.AddDays(1), 15)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Pin" || len(p.Answers) != 1 || p.Answers[0].DayNo != 1 || *p.Answers[0].Points != 1350 || *p.Answers[0].Km != 2000 {
		t.Fatalf("profile %+v", p)
	}
	if len(p.Rounds) != 1 || p.Rounds[0].DayNo != 1 || len(p.World) != 1 || p.World[0].Rank != 1 || p.World[0].Of != 1 || p.Streak != 1 {
		t.Fatalf("rounds %+v, world %+v, streak %d", p.Rounds, p.World, p.Streak)
	}
	// Drawn ahead only for someone whose statistics went out lately.
	if ids, err := store.RecentPlayers(ctx, d); err != nil || len(ids) != 0 {
		t.Fatalf("recent before any statistics = %v, %v", ids, err)
	}

	for _, c := range []struct{ set, ask, want string }{{"v1", "v1", "f1"}, {"v2", "v1", ""}} {
		if err := store.SetProfileCard(ctx, 7, "ru", c.set, "f1"); err != nil {
			t.Fatal(err)
		}
		if got, err := store.ProfileCard(ctx, 7, "ru", c.ask); err != nil || got != c.want {
			t.Fatalf("set %s, asked %s: %q, %v", c.set, c.ask, got, err)
		}
	}
	if ids, err := store.RecentPlayers(ctx, d); err != nil || len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("recent %v, %v", ids, err)
	}
}

// A day kept reads as it was kept, in the profile and on the days page; a
// day not kept is ranked when asked.
func TestKeptDayIsReadAsKept(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	if err := store.Touch(ctx, tg.User{ID: 8, FirstName: "Ana"}, nil); err != nil {
		t.Fatal(err)
	}
	exec(t, store, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus, pts_speed)
	                VALUES ('2026-11-01', 7, 'dm', 48.85, 2.35, '2026-11-01 10:00+03', '2026-11-01 20:00+03', 2000, 1000, 300, 50),
	                       ('2026-11-01', 8, 'dm', 50.40, 30.50, '2026-11-01 11:00+03', '2026-11-01 20:00+03', 10, 3500, 1500, 0)`)
	d := round.NewDate(2026, 11, 1)
	if err := store.KeepDay(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := store.KeepDay(ctx, d); err != nil {
		t.Fatal(err)
	}
	var kept int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM maply.day_place WHERE date = '2026-11-01'`).Scan(&kept); err != nil || kept != 2 {
		t.Fatalf("kept %d places, %v", kept, err)
	}
	// A change after the day was kept does not move the kept places.
	exec(t, store, `UPDATE maply.guess SET km = 1, pts_km = 3500, pts_bonus = 1500, pts_speed = 100 WHERE user_id = 7`)
	p, err := store.ProfileOf(ctx, 7, d.AddDays(1), 15)
	if err != nil || len(p.World) != 1 || p.World[0].Rank != 2 || p.World[0].Of != 2 {
		t.Fatalf("kept world = %+v, %v", p.World, err)
	}
	if mine := dayRow(t, store, d, DaysQuery{UserID: 7}).Me; mine == nil || mine.Rank != 2 || mine.Km != 2000 {
		t.Fatalf("kept day = %+v", mine)
	}
	// Unkept, the day is ranked anew.
	exec(t, store, `DELETE FROM maply.day_closed WHERE date = '2026-11-01'`)
	p, err = store.ProfileOf(ctx, 7, d.AddDays(1), 15)
	if err != nil || len(p.World) != 1 || p.World[0].Rank != 1 {
		t.Fatalf("open world = %+v, %v", p.World, err)
	}
	if mine := dayRow(t, store, d, DaysQuery{UserID: 7}).Me; mine == nil || mine.Rank != 1 {
		t.Fatalf("open day = %+v", mine)
	}
}
