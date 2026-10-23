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

const sitePlayer = "0b6f3f5e-8a3c-4d1e-9f2a-5c7d8e9f0a1b"

// Telegram and site players stand in one list, best first; an answer still
// waiting for its deadline is counted but not ranked.
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
	kyiv := scoring.Place{Point: geo.Point{Lat: 50.45, Lon: 30.52}, Country: "UA", Continent: "europe"}
	n, err := store.ScoreDue(ctx, d, closes, func(p geo.Point) scoring.Score { return scoring.Judge(p, kyiv, nil) })
	if err != nil || n != 3 {
		t.Fatalf("scored %d, %v", n, err)
	}
	w, err := store.World(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if w.Players() != 3 || w.Pending != 1 {
		t.Fatalf("players %d, pending %d", w.Players(), w.Pending)
	}
	if first := w.Entries[0]; !first.Site() || first.Name != "Lucía" || first.Rank != 1 {
		t.Fatalf("first = %+v", first)
	}
	if w.RankOf(8) != 2 || w.RankOf(7) != 3 || w.RankOf(9) != 0 || w.PlaceOf(sitePlayer) != 1 {
		t.Fatalf("ranks = %+v", w.Entries)
	}
	if w.MedianKm == nil {
		t.Fatal("no median")
	}

	days, err := store.Days(ctx, d, 7)
	if err != nil || len(days) != 1 || days[0].Players != 3 || days[0].Pending != 1 || days[0].DayNo != 1 {
		t.Fatalf("days = %+v, %v", days, err)
	}
	mine, err := store.MyDays(ctx, 7, days)
	if err != nil || mine[d].Rank != 3 {
		t.Fatalf("my days = %+v, %v", mine, err)
	}
	site, err := store.SiteDays(ctx, sitePlayer, days)
	if err != nil || site[d].Rank != 1 {
		t.Fatalf("site days = %+v, %v", site, err)
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

func TestChatDaysCountMembers(t *testing.T) {
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
	days, err := store.Days(ctx, d, 1)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := store.ChatDays(ctx, group.ID, days)
	if err != nil {
		t.Fatal(err)
	}
	if c := chat[d]; c.Members != 2 || c.Answered != 1 || c.BestName != "Ana" || c.BestKm == nil {
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
