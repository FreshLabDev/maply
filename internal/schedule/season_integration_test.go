// SPDX-License-Identifier: Apache-2.0
//go:build integration

package schedule

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
)

// The month's champion comes inside the look back at its last day, which the
// morning post of the 1st carries: no fourth message. A group hears its own
// champion, a DM the world's and the person's place.
func TestSeasonEndsInTheLookBackOfTheMonth(t *testing.T) {
	store, pool := testDB(t)
	ctx := context.Background()
	exec(t, pool, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key,
	                                       country_iso, continent, difficulty, reviewed_at)
	               OVERRIDING SYSTEM VALUE
	               VALUES (1, 'used', 'mapillary', 'm1', 'CC-BY-SA-4.0', 'panorama', 38.72, -9.14, 'k1', 'PT', 'europe', 2, now()),
	                      (2, 'used', 'mapillary', 'm2', 'CC-BY-SA-4.0', 'panorama', 38.72, -9.14, 'k2', 'PT', 'europe', 2, now())`)
	exec(t, pool, `INSERT INTO maply.round (date, day_no, place_id) VALUES ('2026-10-30', 1, 1), ('2026-10-31', 2, 2)`)
	chat := &tg.Chat{ID: group, Type: "supergroup", Title: "Maps"}
	for _, u := range []tg.User{{ID: player, FirstName: "Pin"}, {ID: quiet, FirstName: "Ana"}} {
		if err := store.Touch(ctx, u, chat); err != nil {
			t.Fatal(err)
		}
	}
	// Pin plays both days for 2,000; Ana only the last, for 5,000.
	exec(t, pool, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus, pts_speed)
	               VALUES ('2026-10-30', $1, 'dm', 40, -3, '2026-10-30 10:00Z', '2026-10-30 18:00Z', 500, 1000, 0, 0),
	                      ('2026-10-31', $1, 'dm', 40, -3, '2026-10-31 10:00Z', '2026-10-31 18:00Z', 500, 1000, 0, 0),
	                      ('2026-10-31', $2, 'dm', 38.7, -9.1, '2026-10-31 10:00Z', '2026-10-31 18:00Z', 1, 3500, 1500, 0)`, player, quiet)
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	first := round.NewDate(2026, 11, 1)
	clock := first.At(12, 0, kyiv)
	s := &Scheduler{Store: store, Zones: zones, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return clock }}
	dm := db.Chat{ID: player, Kind: "dm"}
	grp := db.Chat{ID: group, Kind: "group"}

	back, err := s.yesterday(ctx, "en", first, dm)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<b>Yesterday worldwide</b>",
		"\n\n<b>" + i18n.T("en", "season.title") + "</b>\n🏆 " + i18n.T("en", "season.world", "name", "Ana", "points", "5,000"),
		i18n.T("en", "season.you", "rank", "2", "n", "2", "points", "2,000"),
	} {
		if !strings.Contains(back, want) {
			t.Errorf("DM look back %q lacks %q", back, want)
		}
	}
	back, err = s.yesterday(ctx, "en", first, grp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(back, "🏆 "+i18n.T("en", "season.group", "name", "Ana", "points", "5,000")) || strings.Contains(back, "season: ") {
		t.Errorf("group look back = %q", back)
	}
	// A day inside the month closes nothing.
	clock = first.AddDays(-1).At(12, 0, kyiv)
	if back, _ := s.yesterday(ctx, "en", first.AddDays(-1), dm); strings.Contains(back, "🏆") {
		t.Errorf("look back at the 30th = %q", back)
	}
}
