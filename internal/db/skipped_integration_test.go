// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/migrations"
)

// A day nobody answered keeps its number and its round row, but is no round:
// the place goes back to the queue, not to be drawn before back_after, and the
// date can never be drawn again.
func TestSkipDayReturnsThePlaceForAMonth(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	easy := approvePlace(t, store, "easy", 1)
	mid := approvePlace(t, store, "mid", 2)
	monday := round.NewDate(2026, time.November, 2)
	back := monday.AddDays(ReturnDays)

	if r, err := store.EnsureRound(ctx, monday); err != nil || r.PlaceID != easy {
		t.Fatalf("Monday = %+v, %v", r, err)
	}
	if n, err := store.DayAnswers(ctx, monday); err != nil || n != 0 {
		t.Fatalf("answers of an empty day = %d, %v", n, err)
	}
	if ok, err := store.SkipDay(ctx, monday, back); err != nil || !ok {
		t.Fatalf("skip = %v, %v", ok, err)
	}
	if ok, err := store.SkipDay(ctx, monday, back); err != nil || ok {
		t.Fatalf("a second skip = %v, %v", ok, err)
	}
	if _, ok, err := store.RoundOn(ctx, monday); err != nil || ok {
		t.Fatalf("a skipped day is a round: %v, %v", ok, err)
	}
	if _, err := store.EnsureRound(ctx, monday); !errors.Is(err, ErrDaySkipped) {
		t.Fatalf("a skipped date drew a place: %v", err)
	}
	var status string
	var after *time.Time
	if err := store.pool.QueryRow(ctx, `SELECT status, back_after FROM maply.place WHERE id = $1`, easy).Scan(&status, &after); err != nil {
		t.Fatal(err)
	}
	if status != "approved" || after == nil || round.DateIn(*after, time.UTC) != back {
		t.Fatalf("the place is %q, back after %v, want approved and %v", status, after, back)
	}

	// The next days do not draw it before its time.
	if r, err := store.EnsureRound(ctx, monday.AddDays(1)); err != nil || r.PlaceID != mid || r.DayNo != 2 {
		t.Fatalf("Tuesday = %+v, %v", r, err)
	}
	if _, err := store.EnsureRound(ctx, monday.AddDays(2)); !errors.Is(err, ErrNoPlace) {
		t.Fatalf("Wednesday drew the returning place: %v", err)
	}
	if _, err := store.EnsureRound(ctx, back.AddDays(-1)); !errors.Is(err, ErrNoPlace) {
		t.Fatalf("the day before back_after drew the place: %v", err)
	}
	r, err := store.EnsureRound(ctx, back)
	if err != nil || r.PlaceID != easy || r.DayNo != 31 {
		t.Fatalf("on back_after = %+v, %v", r, err)
	}
	var rounds int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM maply.round WHERE place_id = $1`, easy).Scan(&rounds); err != nil || rounds != 2 {
		t.Fatalf("%d rounds name the place, want the skipped one and the new one (%v)", rounds, err)
	}

	// A skipped round is never kept, and is not asked about again.
	if err := store.KeepDay(ctx, monday); err != nil {
		t.Fatal(err)
	}
	var kept int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM maply.day_closed WHERE date = $1`, monday.Time()).Scan(&kept); err != nil || kept != 0 {
		t.Fatalf("a skipped day was kept: %d, %v", kept, err)
	}
	open, err := store.Unclosed(ctx, back)
	if err != nil || len(open) != 2 || open[0] != monday.AddDays(1) || open[1] != back {
		t.Fatalf("unclosed = %v, %v", open, err)
	}
	skipped, err := store.SkippedDates(ctx, back)
	if err != nil || len(skipped) != 1 || skipped[0] != monday {
		t.Fatalf("skipped = %v, %v", skipped, err)
	}
}

// Any answer keeps the day, a late site one too, and the check and the mark
// are one statement.
func TestSkipDayRefusesADayWithAnAnswer(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	approvePlace(t, store, "a", 1)
	approvePlace(t, store, "b", 2)
	monday := round.NewDate(2026, time.November, 2)
	for _, d := range []round.Date{monday, monday.AddDays(1)} {
		if _, err := store.EnsureRound(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	// A Telegram answer on Monday.
	if err := store.Touch(ctx, tg.User{ID: 7, FirstName: "Pin"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertGuess(ctx, NewGuess{Date: monday, UserID: 7, Context: "dm", Pin: geo.Point{Lat: 1, Lon: 1}, AnsweredAt: at, Deadline: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// A site answer on Tuesday, taken in after every zone was over.
	if err := store.SitePlayer(ctx, sitePlayer, ptr("Lu")); err != nil {
		t.Fatal(err)
	}
	late := monday.AddDays(1)
	if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: late, Player: sitePlayer, Pin: geo.Point{Lat: 1, Lon: 1}, AnsweredAt: at.Add(48 * time.Hour), Deadline: at.Add(48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []round.Date{monday, late} {
		if n, err := store.DayAnswers(ctx, d); err != nil || n != 1 {
			t.Fatalf("answers of %v = %d, %v", d, n, err)
		}
		if ok, err := store.SkipDay(ctx, d, d.AddDays(ReturnDays)); err != nil || ok {
			t.Fatalf("skip of %v with an answer = %v, %v", d, ok, err)
		}
		if _, ok, err := store.RoundOn(ctx, d); err != nil || !ok {
			t.Fatalf("%v is no round after a refused skip: %v, %v", d, ok, err)
		}
	}
	var approved int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM maply.place WHERE status = 'approved'`).Scan(&approved); err != nil || approved != 0 {
		t.Fatalf("a refused skip returned %d places (%v)", approved, err)
	}
}

// A day nobody answered does not break a streak: the run goes over it, and it
// is not one of the days counted.
func TestStreaksStepOverASkippedDay(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	for i, d := range []string{"a", "b", "c", "d"} {
		approvePlace(t, store, d, 1+i)
	}
	monday := round.NewDate(2026, time.November, 2)
	if err := store.Touch(ctx, tg.User{ID: 7, FirstName: "Pin"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Touch(ctx, tg.User{ID: 8, FirstName: "Ana"}, nil); err != nil {
		t.Fatal(err)
	}
	for n := range 4 {
		d := monday.AddDays(n)
		if _, err := store.EnsureRound(ctx, d); err != nil {
			t.Fatal(err)
		}
		at := d.At(9, 0, time.UTC)
		// Pin answers Monday, Wednesday and Thursday, Ana Wednesday and
		// Thursday; nobody answers Tuesday.
		who := map[int][]int64{0: {7}, 2: {7, 8}, 3: {7, 8}}[n]
		for _, u := range who {
			if err := store.InsertGuess(ctx, NewGuess{Date: d, UserID: u, Context: "dm", Pin: geo.Point{Lat: 1, Lon: 1}, AnsweredAt: at, Deadline: at.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	thursday := monday.AddDays(3)
	got, err := store.Streaks(ctx, []int64{7, 8}, thursday)
	if err != nil || got[7] != 2 || got[8] != 2 {
		t.Fatalf("before the skip: %v, %v", got, err)
	}
	// Tuesday had no answer and is skipped: Pin's run goes over it to Monday.
	if ok, err := store.SkipDay(ctx, monday.AddDays(1), thursday.AddDays(ReturnDays)); err != nil || !ok {
		t.Fatalf("skip Tuesday: %v, %v", ok, err)
	}
	got, err = store.Streaks(ctx, []int64{7, 8}, thursday)
	if err != nil || got[7] != 3 || got[8] != 2 {
		t.Fatalf("streaks over a skipped day = %v, %v, want Pin 3, Ana 2", got, err)
	}
	// Asked on the day after the last answer, the run still ends there.
	if got, err := store.Streaks(ctx, []int64{7}, thursday.AddDays(1)); err != nil || got[7] != 3 {
		t.Fatalf("a day later = %v, %v", got, err)
	}
}

// The days list shows no date that is over everywhere with nobody's answer,
// whether the final world job has skipped it yet or not; a kept day or one
// with a late answer stays.
func TestDaysPageHidesADayNobodyAnswered(t *testing.T) {
	store := testStore(t)
	history(t, store)
	ctx := context.Background()
	// The 8th is over everywhere and not kept: its answers hold it. Take them
	// away and it is a day nobody answered that the final job has not reached.
	exec(t, store, `DELETE FROM maply.guess WHERE date = '2026-11-08'`)
	exec(t, store, `DELETE FROM maply.site_guess WHERE date = '2026-11-08'`)
	q := DaysQuery{Newest: round.NewDate(2026, 11, 10), N: 30, Final: round.NewDate(2026, 11, 8), Seen: round.NewDate(2026, 11, 9), UserID: 7}
	p, err := store.DaysPage(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.Days {
		switch day := d.Date.Time().Day(); day {
		case 4, 8:
			t.Errorf("the %dth, a day nobody answered, is on the list", day)
		}
	}
	// A late answer makes it a day again.
	if err := store.SitePlayer(ctx, sitePlayer, ptr("Lu")); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: round.NewDate(2026, 11, 8), Player: sitePlayer, Pin: geo.Point{Lat: 0, Lon: 0}, AnsweredAt: at, Deadline: at}); err != nil {
		t.Fatal(err)
	}
	p, err = store.DaysPage(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range p.Days {
		found = found || d.Date.Time().Day() == 8
	}
	if !found {
		t.Error("the 8th is gone after an answer came")
	}
}

// A chat whose result waits is marked, listed once and not after the result
// went out; the morning posts of a date are listed with their pin.
func TestHeldResultsAndPosts(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	approvePlace(t, store, "a", 1)
	monday := round.NewDate(2026, time.November, 2)
	if _, err := store.EnsureRound(ctx, monday); err != nil {
		t.Fatal(err)
	}
	for _, c := range []int64{-10, -20} {
		exec(t, store, `INSERT INTO maply.chat_settings (chat_id, kind, timezone) VALUES ($1, 'group', 'Europe/Kyiv')`, c)
		if _, err := store.ChatRound(ctx, c, monday); err != nil {
			t.Fatal(err)
		}
		if err := store.SetPost(ctx, c, monday, 100-c, "line", "", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetPinned(ctx, -10, monday, true); err != nil {
		t.Fatal(err)
	}
	posts, err := store.Posts(ctx, monday)
	if err != nil || len(posts) != 2 || posts[0] != (Post{ChatID: -20, MsgID: 120, Pinned: false}) || posts[1] != (Post{ChatID: -10, MsgID: 110, Pinned: true}) {
		t.Fatalf("posts = %+v, %v", posts, err)
	}
	if held, err := store.HeldChats(ctx, monday); err != nil || len(held) != 0 {
		t.Fatalf("held before any = %v, %v", held, err)
	}
	for _, c := range []int64{-10, -20} {
		if err := store.SetHeld(ctx, c, monday); err != nil {
			t.Fatal(err)
		}
	}
	if held, err := store.HeldChats(ctx, monday); err != nil || len(held) != 2 {
		t.Fatalf("held = %v, %v", held, err)
	}
	exec(t, store, `UPDATE maply.chat_round SET result_msg_id = 5 WHERE chat_id = -10`)
	if held, err := store.HeldChats(ctx, monday); err != nil || len(held) != 1 || held[0] != -20 {
		t.Fatalf("held after one went out = %v, %v", held, err)
	}
}

// Migration 028 skips, for good, the days the final job kept with nobody's
// answer, and leaves a day anyone answered, and a day not over yet.
func TestMigrationSkipsDaysKeptWithNobody(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	today := round.DateIn(time.Now(), time.UTC)
	d := func(back int) round.Date { return today.AddDays(-back) }
	// 10 and 9 days back: nobody, kept. 8 back: a site answer, kept. Yesterday
	// and today: nobody, but not over everywhere, and not kept.
	for i, back := range []int{10, 9, 8, 1, 0} {
		id := int64(i + 1)
		exec(t, store, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key,
		                                         country_iso, continent, difficulty, reviewed_at)
		                OVERRIDING SYSTEM VALUE
		                VALUES ($1, 'used', 'mapillary', $2, 'CC-BY-SA-4.0', 'panorama', 50, 30, $3, 'UA', 'europe', 2, now())`,
			id, fmt.Sprintf("m%d", id), fmt.Sprintf("img%d", id))
		exec(t, store, `INSERT INTO maply.round (date, day_no, place_id) VALUES ($1, $2, $3)`, d(back).Time(), 100-back, id)
		if back >= 8 {
			exec(t, store, `INSERT INTO maply.day_closed (date) VALUES ($1)`, d(back).Time())
		}
	}
	if err := store.SitePlayer(ctx, sitePlayer, ptr("Lu")); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: d(8), Player: sitePlayer, Pin: geo.Point{Lat: 0, Lon: 0}, AnsweredAt: time.Now(), Deadline: time.Now()}); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.FS.ReadFile("028_skipped_days.sql")
	if err != nil {
		t.Fatal(err)
	}
	// The same file twice, as a restart in the middle would run it.
	for range 2 {
		exec(t, store, string(body))
	}

	skipped, err := store.SkippedDates(ctx, today)
	if err != nil || len(skipped) != 2 || skipped[0] != d(10) || skipped[1] != d(9) {
		t.Fatalf("skipped = %v, %v, want the two oldest", skipped, err)
	}
	var kept int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM maply.day_closed WHERE date IN ($1, $2)`, d(10).Time(), d(9).Time()).Scan(&kept); err != nil || kept != 0 {
		t.Fatalf("%d skipped days are still kept (%v)", kept, err)
	}
	var back, live int
	if err := store.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'approved' AND back_after = $1::date + 30),
		       count(*) FILTER (WHERE status = 'used' AND back_after IS NULL)
		  FROM maply.place`, today.Time()).Scan(&back, &live); err != nil || back != 2 || live != 3 {
		t.Fatalf("%d places returned and %d still used, want 2 and 3 (%v)", back, live, err)
	}
	// The kept day with an answer is still kept.
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM maply.day_closed WHERE date = $1`, d(8).Time()).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("the answered day lost its keeping: %d, %v", kept, err)
	}
}
