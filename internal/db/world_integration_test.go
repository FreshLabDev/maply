// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

const (
	sitePlayer      = "0b6f3f5e-8a3c-4d1e-9f2a-5c7d8e9f0a1b"
	otherSitePlayer = "6a1d2c3b-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
)

// Telegram and site players stand in one list: Telegram's best first, then
// the site's, so the site's pin nearest the place is fourth here. A Telegram
// answer is scored at once; a site answer still waiting for its deadline is
// counted but not ranked.
func TestWorldRanksEveryoneTogether(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	d := round.NewDate(2026, 11, 1)
	if err := store.Touch(ctx, tg.User{ID: 8, FirstName: "Ana"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Touch(ctx, tg.User{ID: 9, FirstName: "Late"}, nil); err != nil {
		t.Fatal(err)
	}
	early := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	closes := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	for _, g := range []NewGuess{
		{Date: d, UserID: 7, Context: "dm", Pin: geo.Point{Lat: 48, Lon: 2}, AnsweredAt: early, Deadline: closes},
		// Ana answers at the last minute: no speed bonus to beat the site's pin.
		{Date: d, UserID: 8, Context: "dm", Pin: geo.Point{Lat: 50, Lon: 30}, AnsweredAt: closes.Add(-time.Minute), Deadline: closes},
		// Late's window runs ten hours longer; the score is there all the same.
		{Date: d, UserID: 9, Context: "dm", Pin: geo.Point{Lat: 0, Lon: 0}, AnsweredAt: early, Deadline: closes.Add(10 * time.Hour)},
	} {
		if err := store.InsertGuess(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SitePlayer(ctx, sitePlayer, ptr("  Lu\u200bcía \n")); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: d, Player: sitePlayer, Pin: geo.Point{Lat: 50.4, Lon: 30.5}, AnsweredAt: early, Deadline: closes}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: d, Player: sitePlayer, Pin: geo.Point{}, AnsweredAt: early, Deadline: closes}); !errors.Is(err, ErrAlreadyAnswered) {
		t.Fatalf("second site answer: %v", err)
	}
	// A guest further west still has hours to go.
	if err := store.SitePlayer(ctx, otherSitePlayer, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: d, Player: otherSitePlayer, Pin: geo.Point{Lat: 40, Lon: -74}, AnsweredAt: early, Deadline: closes.Add(6 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	kyiv := scoring.Place{Point: geo.Point{Lat: 50.45, Lon: 30.52}, Country: "UA", Continent: "europe"}
	n, err := store.ScoreDue(ctx, d, closes, func(p geo.Point) scoring.Score { return scoring.Judge(p, kyiv, nil) })
	if err != nil || n != 4 {
		t.Fatalf("scored %d, %v", n, err)
	}
	w, err := store.World(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if w.Players() != 4 || w.Pending != 1 {
		t.Fatalf("players %d, pending %d", w.Players(), w.Pending)
	}
	if site := w.Entries[3]; !site.Site() || site.Name != "Lucía" || site.Rank != 4 {
		t.Fatalf("site = %+v", site)
	}
	if w.RankOf(8) != 1 || w.RankOf(7) != 2 || w.RankOf(9) != 3 || w.PlaceOf(sitePlayer) != 4 || w.PlaceOf(otherSitePlayer) != 0 {
		t.Fatalf("ranks = %+v", w.Entries)
	}
	if w.MedianKm == nil {
		t.Fatal("no median")
	}
	// The pins come in the ranking's order, the site's among them.
	pins, err := store.WorldPins(ctx, d)
	if err != nil || len(pins) != 4 {
		t.Fatalf("pins = %+v, %v", pins, err)
	}
	for i, p := range pins {
		if p.Km != w.Entries[i].Km {
			t.Errorf("pin %d at %.1f km, entry at %.1f km", i, p.Km, w.Entries[i].Km)
		}
	}
	if pins[3].Lat != 50.4 || !pins[3].AnsweredAt.Equal(early) {
		t.Errorf("site pin = %+v", pins[3])
	}

	// The world picture is kept per language, the last version winning.
	if _, ok, err := store.WorldCard(ctx, d, "en"); ok || err != nil {
		t.Fatalf("a card before any: %v, %v", ok, err)
	}
	for _, c := range []WorldCard{{Version: "v1", FileID: "f1"}, {Version: "v2", FileID: "f2", Final: true}} {
		if err := store.SetWorldCard(ctx, d, "en", c); err != nil {
			t.Fatal(err)
		}
	}
	if c, ok, _ := store.WorldCard(ctx, d, "en"); !ok || c != (WorldCard{Version: "v2", FileID: "f2", Final: true}) {
		t.Errorf("kept card = %+v", c)
	}
	if _, ok, _ := store.WorldCard(ctx, d, "ru"); ok {
		t.Error("a card in another language")
	}

	if day := dayRow(t, store, d, DaysQuery{UserID: 7}); day.Players != 4 || day.Pending != 1 || day.DayNo != 1 || day.Me == nil || day.Me.Rank != 2 {
		t.Fatalf("day = %+v, me %+v", day, day.Me)
	}
	if site := dayRow(t, store, d, DaysQuery{Player: sitePlayer}).Me; site == nil || site.Rank != 4 {
		t.Fatalf("site day = %+v", site)
	}
	if name, err := store.SiteName(ctx, sitePlayer); err != nil || name != "Lucía" {
		t.Fatalf("site name = %q, %v", name, err)
	}
	// No name keeps the name; a blank one goes back to "Guest".
	if err := store.SitePlayer(ctx, sitePlayer, nil); err != nil {
		t.Fatal(err)
	}
	if name, _ := store.SiteName(ctx, sitePlayer); name != "Lucía" {
		t.Fatalf("name lost: %q", name)
	}
	if err := store.SitePlayer(ctx, sitePlayer, ptr(" \t")); err != nil {
		t.Fatal(err)
	}
	if name, _ := store.SiteName(ctx, sitePlayer); name != "" {
		t.Fatalf("blank name kept as %q", name)
	}
}

func TestGroupsDayCountsMembers(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	d := round.NewDate(2026, 11, 1)
	group := &tg.Chat{ID: -1007001, Type: "supergroup"}
	for _, u := range []tg.User{{ID: 7, FirstName: "Pin"}, {ID: 8, FirstName: "Ana"}} {
		if err := store.Touch(ctx, u, group); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	if err := store.InsertGuess(ctx, NewGuess{Date: d, UserID: 8, Context: "group", ChatID: group.ID, Pin: geo.Point{Lat: 50, Lon: 30}, AnsweredAt: at, Deadline: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	place := scoring.Place{Point: geo.Point{Lat: 50.45, Lon: 30.52}}
	if _, err := store.ScoreDue(ctx, d, at.Add(time.Hour), func(p geo.Point) scoring.Score { return scoring.Judge(p, place, nil) }); err != nil {
		t.Fatal(err)
	}
	if c := dayRow(t, store, d, DaysQuery{ChatID: group.ID}).Group; c == nil || c.Members != 2 || c.Answered != 1 || c.BestName != "Ana" || c.BestKm == nil {
		t.Fatalf("chat day = %+v", c)
	}
}

func ptr(s string) *string { return &s }

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"  Lucía  ":                     "Lucía",
		"a\u202eb":                      "ab",
		"one\ttwo":                      "one two",
		"abcdefghijklmnopqrstuvwxyz123": "abcdefghijklmnopqrstuvwx",
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A Telegram player sees the place on answering and a new browser is a new
// player, so a site answer, in time or late, ranks after every Telegram one in
// every list that ranks the day, however well it scored.
func TestSiteAnswersRankAfterTelegram(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	d := round.NewDate(2026, 11, 1)
	if err := store.Touch(ctx, tg.User{ID: 8, FirstName: "Ana"}, nil); err != nil {
		t.Fatal(err)
	}
	early := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	closes := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	if err := store.InsertGuess(ctx, NewGuess{Date: d, UserID: 8, Context: "dm", Pin: geo.Point{Lat: 50, Lon: 30}, AnsweredAt: closes.Add(-time.Minute), Deadline: closes}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{sitePlayer, otherSitePlayer} {
		if err := store.SitePlayer(ctx, p, nil); err != nil {
			t.Fatal(err)
		}
	}
	// The exact spot, after Sydney's 20:00.
	if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: d, Player: sitePlayer, Pin: geo.Point{Lat: 50.45, Lon: 30.52}, AnsweredAt: early.Add(2 * time.Hour), Deadline: closes, Late: true}); err != nil {
		t.Fatal(err)
	}
	// The exact spot too, in the morning, before any reveal.
	if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: d, Player: otherSitePlayer, Pin: geo.Point{Lat: 50.45, Lon: 30.52}, AnsweredAt: early, Deadline: closes}); err != nil {
		t.Fatal(err)
	}
	kyiv := scoring.Place{Point: geo.Point{Lat: 50.45, Lon: 30.52}, Country: "UA", Continent: "europe"}
	if _, err := store.ScoreDue(ctx, d, closes, func(p geo.Point) scoring.Score { return scoring.Judge(p, kyiv, nil) }); err != nil {
		t.Fatal(err)
	}
	w, err := store.World(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if w.RankOf(8) != 1 || w.PlaceOf(otherSitePlayer) != 2 || w.PlaceOf(sitePlayer) != 3 {
		t.Fatalf("ranks = %+v", w.Entries)
	}
	if w.Entries[1].Points <= w.Entries[0].Points {
		t.Fatalf("the site answer did not score best: %+v", w.Entries)
	}
	pins, err := store.WorldPins(ctx, d)
	if err != nil || len(pins) != 3 || pins[0].Km != w.Entries[0].Km || pins[1].Km != w.Entries[1].Km {
		t.Fatalf("pins = %+v, %v", pins, err)
	}
	if mine := dayRow(t, store, d, DaysQuery{Player: sitePlayer}).Me; mine == nil || mine.Rank != 3 {
		t.Fatalf("site day = %+v", mine)
	}
	if mine := dayRow(t, store, d, DaysQuery{UserID: 8}).Me; mine == nil || mine.Rank != 1 {
		t.Fatalf("Ana's day = %+v", mine)
	}

	// With no Telegram answer, the site's best is first.
	exec(t, store, `DELETE FROM maply.guess`)
	exec(t, store, `DELETE FROM maply.site_guess WHERE player = $1`, otherSitePlayer)
	if w, err := store.World(ctx, d); err != nil || w.PlaceOf(sitePlayer) != 1 {
		t.Fatalf("alone = %+v, %v", w.Entries, err)
	}
}
