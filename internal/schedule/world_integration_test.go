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
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// A chat hears about the world once, the day after: in the morning post
// when yesterday has closed in every zone, else with that evening's result.
func TestLookBackComesOnceTheDayAfter(t *testing.T) {
	store, pool := testDB(t)
	ctx := context.Background()
	exec(t, pool, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key,
	                                       country_iso, continent, difficulty, reviewed_at)
	               OVERRIDING SYSTEM VALUE
	               VALUES (1, 'used', 'mapillary', 'm1', 'CC-BY-SA-4.0', 'photo', 38.72, -9.14, 'k1', 'PT', 'europe', 2, now())`)
	y, d := round.NewDate(2026, 11, 1), round.NewDate(2026, 11, 2)
	exec(t, pool, `INSERT INTO maply.round (date, day_no, place_id) VALUES ($1, 1, 1)`, y.Time())
	chat := &tg.Chat{ID: group, Type: "supergroup", Title: "Café <3"}
	for _, u := range []tg.User{{ID: player, FirstName: "Pin"}, {ID: quiet, FirstName: "Ana"}} {
		if err := store.Touch(ctx, u, chat); err != nil {
			t.Fatal(err)
		}
	}
	answered := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	closes := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	for id, pin := range map[int64]geo.Point{player: {Lat: 40.4, Lon: -3.7}, quiet: {Lat: 38.7, Lon: -9.1}} {
		if err := store.InsertGuess(ctx, db.NewGuess{Date: y, UserID: id, Context: "dm", Pin: pin, AnsweredAt: answered, Deadline: closes}); err != nil {
			t.Fatal(err)
		}
	}
	lisbon := scoring.Place{Point: geo.Point{Lat: 38.72, Lon: -9.14}, Country: "PT", Continent: "europe"}
	if _, err := store.ScoreDue(ctx, y, closes, func(p geo.Point) scoring.Score { return scoring.Judge(p, lisbon, nil) }); err != nil {
		t.Fatal(err)
	}
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	var clock time.Time
	s := &Scheduler{Store: store, Zones: zones, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return clock }}

	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	dm := db.Chat{ID: player, Kind: "dm"}
	grp := db.Chat{ID: group, Kind: "group"}

	clock = d.At(7, 0, kyiv)
	back, err := s.yesterday(ctx, "en", d, dm)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Yesterday’s first place worldwide: Ana", "Your place: 2 of 2."} {
		if !strings.Contains(back, want) {
			t.Errorf("DM look back %q lacks %q", back, want)
		}
	}
	// Two players make no typical miss and no share beaten.
	if strings.Contains(back, "Typical") || strings.Contains(back, "ahead of") {
		t.Errorf("DM look back %q has statistics of two players", back)
	}
	if back, _ := s.yesterday(ctx, "en", d, grp); !strings.Contains(back, "of 2: Ana #1, Pin #2") {
		t.Errorf("group look back = %q", back)
	}
	// Carried by the morning post, it is not repeated at 20:00.
	if late, _ := s.lateLookBack(ctx, "en", d, dm, db.ChatRound{Yesterday: back}); late != "" {
		t.Errorf("repeated at the result: %q", late)
	}

	// At 07:00 in Tokyo yesterday is still open in the Americas.
	clock = d.At(7, 0, tokyo)
	if back, _ := s.yesterday(ctx, "en", d, dm); back != "" {
		t.Errorf("Tokyo morning got a half-counted day: %q", back)
	}
	clock = d.At(20, 0, tokyo)
	if late, _ := s.lateLookBack(ctx, "en", d, dm, db.ChatRound{}); !strings.Contains(late, "Your place: 2 of 2") {
		t.Errorf("Tokyo result look back = %q", late)
	}
}

// A DM result tells the person about their own day: where the points came
// from and their place in each group they play in.
func TestOwnLinesTellThePersonTheirDay(t *testing.T) {
	store, pool := testDB(t)
	ctx := context.Background()
	exec(t, pool, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key,
	                                       country_iso, continent, difficulty, reviewed_at)
	               OVERRIDING SYSTEM VALUE
	               VALUES (1, 'used', 'mapillary', 'm1', 'CC-BY-SA-4.0', 'photo', 38.72, -9.14, 'k1', 'PT', 'europe', 2, now())`)
	d := round.NewDate(2026, 11, 1)
	exec(t, pool, `INSERT INTO maply.round (date, day_no, place_id) VALUES ($1, 1, 1)`, d.Time())
	if err := store.EnsureChat(ctx, group); err != nil {
		t.Fatal(err)
	}
	chat := &tg.Chat{ID: group, Type: "supergroup", Title: "Café <3"}
	for _, u := range []tg.User{{ID: player, FirstName: "Pin"}, {ID: quiet, FirstName: "Ana"}} {
		if err := store.Touch(ctx, u, chat); err != nil {
			t.Fatal(err)
		}
	}
	answered := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	closes := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	for id, pin := range map[int64]geo.Point{player: {Lat: 38.7201, Lon: -9.1401}, quiet: {Lat: 40.4, Lon: -3.7}} {
		if err := store.InsertGuess(ctx, db.NewGuess{Date: d, UserID: id, Context: "group", ChatID: group, Pin: pin, AnsweredAt: answered, Deadline: closes}); err != nil {
			t.Fatal(err)
		}
	}
	lisbon := scoring.Place{Point: geo.Point{Lat: 38.72, Lon: -9.14}, Country: "PT", Continent: "europe"}
	if _, err := store.ScoreDue(ctx, d, closes, func(p geo.Point) scoring.Score { return scoring.Judge(p, lisbon, nil) }); err != nil {
		t.Fatal(err)
	}
	g, ok, err := store.GuessOf(ctx, d, player)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	s := &Scheduler{Store: store, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	lines, err := s.ownLines(ctx, "en", d, g)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{"Your result: 14 m · ", "3,500 for the distance", "In Café &lt;3: 1 of 2."} {
		if !strings.Contains(text, want) {
			t.Errorf("own lines %q lack %q", text, want)
		}
	}
}
