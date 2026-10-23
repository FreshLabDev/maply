// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"testing"

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
	if ids, err := store.RecentPlayers(ctx, d); err != nil || len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("recent %v, %v", ids, err)
	}

	for _, c := range []struct{ set, ask, want string }{{"v1", "v1", "f1"}, {"v2", "v1", ""}} {
		if err := store.SetProfileCard(ctx, 7, "ru", c.set, "f1"); err != nil {
			t.Fatal(err)
		}
		if got, err := store.ProfileCard(ctx, 7, "ru", c.ask); err != nil || got != c.want {
			t.Fatalf("set %s, asked %s: %q, %v", c.set, c.ask, got, err)
		}
	}
}
