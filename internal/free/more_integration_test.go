// SPDX-License-Identifier: Apache-2.0
//go:build integration

package free_test

import (
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
)

var sea = &geo.Point{Lat: 0, Lon: -30}

// seenBy writes a finished free game of who's with one place, as if they had
// played it.
func (h *harness) seenBy(who free.Who, name string, place int64) {
	h.t.Helper()
	if _, err := h.pool.Exec(h.ctx, `
		WITH g AS (INSERT INTO maply.free_game (kind, mode, rounds, status, created_at)
		           VALUES ('solo', 'classic', 3, 'done', '2026-01-01') RETURNING id),
		     m AS (INSERT INTO maply.free_member (game, m, user_id, name, joined_at, last_seen_at)
		           SELECT id, 1, $1, $2, '2026-01-01', '2026-01-01' FROM g)
		INSERT INTO maply.free_round (game, n, place_id, starts_at) SELECT g.id, 1, $3, '2026-01-01' FROM g`,
		who.UserID, name, place); err != nil {
		h.t.Fatal(err)
	}
}

// day writes day n on date at place, closed in every zone or not.
func (h *harness) day(n int, d round.Date, place int64, closed bool) {
	h.t.Helper()
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO maply.round (date, day_no, place_id) VALUES ($1, $2, $3)`, d.Time(), n, place); err != nil {
		h.t.Fatal(err)
	}
	if closed {
		if _, err := h.pool.Exec(h.ctx, `INSERT INTO maply.day_closed (date) VALUES ($1)`, d.Time()); err != nil {
			h.t.Fatal(err)
		}
	}
}

func TestTimedGame(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	set := free.Settings{Mode: free.Timed, Seconds: free.TimedSeconds}
	for _, bad := range []free.Settings{{Mode: free.Timed, Seconds: 60}, {Mode: free.Timed, Seconds: 180, Rounds: 5}, free.ArchiveSettings} {
		if _, err := h.svc.NewSolo(ctx, ann, "Ann", bad); !is(err, free.ErrBadRequest) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	v, err := h.svc.NewSolo(ctx, ann, "Ann", set)
	if err != nil {
		t.Fatal(err)
	}
	if v.EndsAt != nil {
		t.Fatal("the clock starts with the first place")
	}
	t0 := h.clock()
	r, err := h.svc.StartSolo(ctx, ann, v.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	end := t0.Add(3 * time.Minute)
	if r.EndsAt == nil || !r.EndsAt.Equal(end) {
		t.Fatalf("place 1 ends with the game: %v", r.EndsAt)
	}
	h.tick(10 * time.Second)
	v, err = h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 1, Pin: at(h.spotOf(v.ID, 1))}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if g := v.Done[0].Guess; g.Speed != 0 || !g.Country || v.Countries != 1 || v.Next != 2 || v.EndsAt == nil || !v.EndsAt.Equal(end) {
		t.Fatalf("no speed bonus against the clock: %+v %+v", g, v)
	}
	// Every place ends with the game; one may be passed while the time runs,
	// and a wrong country does not end it.
	if r, err = h.svc.StartSolo(ctx, ann, v.ID, 2); err != nil || !r.EndsAt.Equal(end) {
		t.Fatalf("place 2: %+v %v", r, err)
	}
	if v, err = h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 2, Skip: true}, "en"); err != nil || v.Next != 3 {
		t.Fatalf("pass: %+v %v", v, err)
	}
	if _, err = h.svc.StartSolo(ctx, ann, v.ID, 3); err != nil {
		t.Fatal(err)
	}
	h.tick(100 * time.Second)
	if v, err = h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 3, Pin: sea}, "en"); err != nil || v.Status != free.StatusPlaying || v.Next != 4 {
		t.Fatalf("a miss goes on: %+v %v", v, err)
	}
	if _, err = h.svc.StartSolo(ctx, ann, v.ID, 4); err != nil {
		t.Fatal(err)
	}
	// The time runs out on place 4: the game is over, the place a miss.
	h.tick(73 * time.Second)
	if v, err = h.svc.SoloGame(ctx, ann, v.ID, "en"); err != nil {
		t.Fatal(err)
	}
	if v.Status != free.StatusDone || v.Final == nil || v.Final.Countries != 1 || v.Final.Played != 4 || len(v.Done) != 4 || v.Next != 0 {
		t.Fatalf("over: %+v %+v", v, v.Final)
	}
	if v.Done[3].Guess.TimeMs != (70 * time.Second).Milliseconds() {
		t.Fatalf("the last place's time is what was left of the game: %d", v.Done[3].Guess.TimeMs)
	}
	if _, err := h.svc.StartSolo(ctx, ann, v.ID, 5); !is(err, free.ErrOver) {
		t.Fatalf("no place after the time: %v", err)
	}
	// A second game with two countries beats it; the record is the count.
	v, err = h.svc.NewSolo(ctx, ann, "Ann", set)
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		if _, err := h.svc.StartSolo(ctx, ann, v.ID, n); err != nil {
			t.Fatal(err)
		}
		h.tick(5 * time.Second)
		if v, err = h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: n, Pin: at(h.spotOf(v.ID, n))}, "en"); err != nil {
			t.Fatal(err)
		}
	}
	// Nobody comes back: the sweep finishes it once the time is up.
	h.tick(3 * time.Minute)
	if err := h.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	var count int
	if err := h.pool.QueryRow(ctx, `SELECT g.status, r.countries FROM maply.free_game g JOIN maply.free_result r ON r.game = g.id
		WHERE g.id = $1 AND r.mode = 'timed' AND r.seconds = 180 AND r.rounds IS NULL`, v.ID).Scan(&status, &count); err != nil || status != free.StatusDone || count != 2 {
		t.Fatalf("swept: %q %d %v", status, count, err)
	}
	if v, err = h.svc.SoloGame(ctx, ann, v.ID, "en"); err != nil || v.Final.Best == nil || *v.Final.Best != 1 || !v.Final.Record {
		t.Fatalf("record: %+v %v", v.Final, err)
	}
	home, err := h.svc.SoloHome(ctx, ann)
	if err != nil || home.Best["timed/180"] != 2 || home.Last != set {
		t.Fatalf("home: %+v %v", home, err)
	}
}

func TestArchive(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	answered := h.addPlace("answered", "used", spot{"SE", "europe", 59.3293, 18.0686})
	open := h.addPlace("open", "used", spot{"FI", "europe", 60.1699, 24.9384})
	seen := h.addPlace("seen", "used", spot{"NO", "europe", 59.9139, 10.7522})
	live := h.addPlace("live", "used", spot{"DK", "europe", 55.6761, 12.5683})
	h.day(1, round.NewDate(2026, time.October, 20), answered, true)
	h.day(2, round.NewDate(2026, time.October, 21), open, true)
	h.day(3, round.NewDate(2026, time.October, 22), seen, true)
	h.day(4, round.NewDate(2026, time.October, 23), live, false)
	for _, w := range []free.Who{ann, bob} {
		if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'P')`, w.UserID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.pool.Exec(ctx, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
		VALUES ('2026-10-20', $1, 'dm', 0, 0, '2026-10-20 10:00Z', '2026-10-20 18:00Z')`, ann.UserID); err != nil {
		t.Fatal(err)
	}
	h.seenBy(ann, "Ann", seen)
	// Day 2 kept three answers: 4000, 2000 and 500 points.
	for i, p := range []int{4000, 2000, 500} {
		if _, err := h.pool.Exec(ctx, `INSERT INTO maply.day_place (date, user_id, km, points, rank, n)
			VALUES ('2026-10-21', $1, $2, $3, $4, 3)`, 900+i, float64(10+i*1000), p, i+1); err != nil {
			t.Fatal(err)
		}
	}

	days, err := h.svc.ArchiveOf(ctx, ann)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, d := range days {
		got = append(got, d.Date+" "+d.State)
	}
	if want := "2026-10-22 seen,2026-10-21 open,2026-10-20 answered"; strings.Join(got, ",") != want {
		t.Fatalf("archive %v, want %s: a day still in play is never in it", got, want)
	}
	noPlace(t, "the archive", days)
	for n, want := range map[int]error{1: free.ErrDayAnswered, 3: free.ErrDaySeen, 4: free.ErrNotFound, 9: free.ErrNotFound} {
		if _, err := h.svc.NewArchive(ctx, ann, "Ann", n, "en"); !is(err, want) {
			t.Fatalf("day %d: %v, want %v", n, err, want)
		}
	}
	v, err := h.svc.NewArchive(ctx, ann, "Ann", 2, "en")
	if err != nil {
		t.Fatal(err)
	}
	if v.Current == nil || v.Current.EndsAt != nil || v.Day == nil || v.Day.N != 2 || v.Day.Date != "2026-10-21" || v.Next != 0 || v.Settings != free.ArchiveSettings {
		t.Fatalf("a day from the archive begins at once, with no timer: %+v", v)
	}
	noPlace(t, "a day taken", v)
	if _, err := h.svc.Image(ctx, strings.TrimSuffix(strings.TrimPrefix(v.Current.Image, "/api/free/img/"), ".jpg")); err != nil {
		t.Fatalf("its picture: %v", err)
	}
	if again, err := h.svc.NewArchive(ctx, ann, "Ann", 2, "en"); err != nil || again.ID != v.ID {
		t.Fatalf("taken again gives the same game: %+v %v", again, err)
	}
	// A new solo game leaves the day be, and the screen does not take it for
	// a game to continue nor for the last settings.
	other, err := h.svc.NewSolo(ctx, ann, "Ann", free.DefaultSolo)
	if err != nil {
		t.Fatal(err)
	}
	home, err := h.svc.SoloHome(ctx, ann)
	if err != nil || home.Open == nil || *home.Open != other.ID || home.Last != free.DefaultSolo || home.Archive != 0 {
		t.Fatalf("home: %+v %v", home, err)
	}
	h.tick(time.Hour)
	v, err = h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 1, Pin: &geo.Point{Lat: 60.17, Lon: 24.94}}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != free.StatusDone || v.Final == nil || v.Final.DayRank != 1 || v.Final.DayOf != 4 || v.Final.Best != nil || v.Done[0].Place.Country != "FI" {
		t.Fatalf("would have been first of four: %+v %+v", v, v.Final)
	}
	var kind, mode string
	if err := h.pool.QueryRow(ctx, `SELECT kind, mode FROM maply.free_result WHERE game = $1`, v.ID).Scan(&kind, &mode); err != nil || kind != free.Solo || mode != free.Archive {
		t.Fatalf("the result goes to free play: %s %s %v", kind, mode, err)
	}
	days, _ = h.svc.ArchiveOf(ctx, ann)
	if d := days[1]; d.State != free.DayPlayed || d.Game != v.ID || d.Points == nil || *d.Points != v.Final.Points {
		t.Fatalf("played: %+v", d)
	}
	if home, _ = h.svc.SoloHome(ctx, ann); home.Best["archive/0/1"] != 0 || len(home.Best) != 0 {
		t.Fatalf("no record of the archive: %+v", home.Best)
	}
	// Bob's pin in the sea stands behind the day's answers; with the speed
	// bonus out of it, a tie in points goes to the day's answer.
	b, err := h.svc.NewArchive(ctx, bob, "Bob", 2, "en")
	if err != nil {
		t.Fatal(err)
	}
	if b, err = h.svc.GuessSolo(ctx, bob, b.ID, free.GuessIn{N: 1, Pin: sea}, "en"); err != nil || b.Final.DayRank != 4 || b.Final.DayOf != 4 {
		t.Fatalf("bob: %+v %v", b.Final, err)
	}
	// Given up, a day has no rank.
	if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'Cleo')`, cleo.UserID); err != nil {
		t.Fatal(err)
	}
	c, err := h.svc.NewArchive(ctx, cleo, "Cleo", 1, "en")
	if err != nil {
		t.Fatal(err)
	}
	if c, err = h.svc.GuessSolo(ctx, cleo, c.ID, free.GuessIn{N: 1, Skip: true}, "en"); err != nil || c.Final.DayRank != 0 || c.Final.Points != 0 {
		t.Fatalf("cleo gives up: %+v %v", c.Final, err)
	}
	// A site guest has an archive too, by the browser's player.
	guest := free.Who{Player: "6f1c6d2e-6b0e-4c3a-9a51-0e1d2c3b4a59"}
	if days, err = h.svc.ArchiveOf(ctx, guest); err != nil || len(days) != 3 || days[0].State != free.DayOpen {
		t.Fatalf("guest: %+v %v", days, err)
	}
	g, err := h.svc.NewArchive(ctx, guest, "", 1, "en")
	if err != nil || g.Current == nil {
		t.Fatalf("guest takes a day: %+v %v", g, err)
	}
	if days, _ = h.svc.ArchiveOf(ctx, guest); days[2].State != free.DayPlayed || days[2].Points != nil {
		t.Fatalf("guest's day in play: %+v", days[2])
	}
	if days, _ = h.svc.ArchiveOf(ctx, free.Who{}); len(days) != 3 || days[2].State != free.DayOpen {
		t.Fatalf("a new browser: %+v", days)
	}
}

func TestChallenge(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	for _, w := range []free.Who{ann, bob, cleo} {
		if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'P')`, w.UserID); err != nil {
			t.Fatal(err)
		}
	}
	set := free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3}
	// play answers every place of a game at the place, after wait, and gives
	// the places it had.
	play := func(w free.Who, id int64, wait time.Duration, upTo int) (free.SoloView, []spot) {
		t.Helper()
		var v free.SoloView
		var got []spot
		for n := 1; n <= upTo; n++ {
			r, err := h.svc.StartSolo(ctx, w, id, n)
			if err != nil {
				t.Fatal(err)
			}
			noPlace(t, "a place begun", r)
			got = append(got, h.spotOf(id, n))
			h.tick(wait)
			if v, err = h.svc.GuessSolo(ctx, w, id, free.GuessIn{N: n, Pin: at(got[n-1])}, "en"); err != nil {
				t.Fatal(err)
			}
		}
		return v, got
	}
	v, err := h.svc.NewSolo(ctx, ann, "Ann", set)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.MakeChallenge(ctx, ann, v.ID); !is(err, free.ErrNotDone) {
		t.Fatalf("a game still going: %v", err)
	}
	annV, places := play(ann, v.ID, 20*time.Second, 3)
	if _, err := h.svc.MakeChallenge(ctx, bob, v.ID); !is(err, free.ErrNotMember) {
		t.Fatalf("someone else's game: %v", err)
	}
	if _, err := h.svc.MakeChallenge(ctx, free.Who{Player: "6f1c6d2e-6b0e-4c3a-9a51-0e1d2c3b4a59"}, v.ID); !is(err, free.ErrTelegram) {
		t.Fatalf("a guest: %v", err)
	}
	code, err := h.svc.MakeChallenge(ctx, ann, v.ID)
	if err != nil || !free.ValidCode(code) {
		t.Fatalf("challenge %q %v", code, err)
	}
	if again, _ := h.svc.MakeChallenge(ctx, ann, v.ID); again != code {
		t.Fatalf("one challenge a game: %s %s", again, code)
	}
	if v, _ = h.svc.SoloGame(ctx, ann, v.ID, "en"); v.Challenge != code {
		t.Fatalf("the game names its challenge: %+v", v)
	}
	// Without a timer there is no challenge.
	nt, err := h.svc.NewSolo(ctx, ann, "Ann", free.Settings{Mode: free.Classic, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.MakeChallenge(ctx, ann, nt.ID); !is(err, free.ErrBadRequest) {
		t.Fatalf("no timer: %v", err)
	}

	cv, err := h.svc.ChallengeOf(ctx, bob, code)
	if err != nil {
		t.Fatal(err)
	}
	if cv.By != "Ann" || cv.Mine != nil || cv.Settings != set || len(cv.Lines) != 1 || cv.Lines[0].Rank != 1 || !cv.Lines[0].Maker || cv.Lines[0].Points != annV.Final.Points {
		t.Fatalf("bob sees the challenge: %+v", cv)
	}
	noPlace(t, "a challenge", cv)
	if _, err := h.svc.TakeChallenge(ctx, free.Who{Player: "6f1c6d2e-6b0e-4c3a-9a51-0e1d2c3b4a59"}, "", code, "en"); !is(err, free.ErrTelegram) {
		t.Fatalf("a guest takes it: %v", err)
	}
	// Bob had place 2 before: he plays it all the same, marked.
	var p2 int64
	if err := h.pool.QueryRow(ctx, `SELECT place_id FROM maply.free_round WHERE game = $1 AND n = 2`, v.ID).Scan(&p2); err != nil {
		t.Fatal(err)
	}
	h.seenBy(bob, "Bob", p2)
	bv, err := h.svc.TakeChallenge(ctx, bob, "Bob", code, "en")
	if err != nil {
		t.Fatal(err)
	}
	if bv.Settings != set || bv.Next != 1 || bv.Challenge != code || bv.ID == v.ID {
		t.Fatalf("bob's attempt: %+v", bv)
	}
	noPlace(t, "an attempt", bv)
	// A new solo game of Bob's leaves the attempt be.
	if _, err := h.svc.NewSolo(ctx, bob, "Bob", free.DefaultSolo); err != nil {
		t.Fatal(err)
	}
	r, err := h.svc.StartSolo(ctx, bob, bv.ID, 1)
	if err != nil || r.Repeat {
		t.Fatalf("place 1: %+v %v", r, err)
	}
	if h.spotOf(bv.ID, 1) != places[0] {
		t.Fatal("the attempt plays the challenge's first place")
	}
	cv, _ = h.svc.ChallengeOf(ctx, cleo, code)
	if len(cv.Lines) != 2 || cv.Lines[1].Status != free.StatusPlaying || cv.Lines[1].Rank != 0 {
		t.Fatalf("bob playing: %+v", cv.Lines)
	}
	h.tick(10 * time.Second)
	if _, err := h.svc.GuessSolo(ctx, bob, bv.ID, free.GuessIn{N: 1, Pin: at(places[0])}, "en"); err != nil {
		t.Fatal(err)
	}
	if r, err = h.svc.StartSolo(ctx, bob, bv.ID, 2); err != nil || !r.Repeat {
		t.Fatalf("place 2 is marked: %+v %v", r, err)
	}
	h.tick(10 * time.Second)
	if _, err := h.svc.GuessSolo(ctx, bob, bv.ID, free.GuessIn{N: 2, Pin: at(places[1])}, "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartSolo(ctx, bob, bv.ID, 3); err != nil {
		t.Fatal(err)
	}
	h.tick(10 * time.Second)
	if bv, err = h.svc.GuessSolo(ctx, bob, bv.ID, free.GuessIn{N: 3, Pin: at(places[2])}, "en"); err != nil || bv.Status != free.StatusDone {
		t.Fatalf("bob done: %+v %v", bv, err)
	}
	if h.spotOf(bv.ID, 2) != places[1] || h.spotOf(bv.ID, 3) != places[2] {
		t.Fatal("the attempt plays the challenge's places in order")
	}
	// Quicker on the same places, Bob is first.
	cv, _ = h.svc.ChallengeOf(ctx, bob, code)
	if cv.Mine == nil || *cv.Mine != bv.ID || cv.Status != free.StatusDone || len(cv.Lines) != 2 ||
		cv.Lines[0].Name != "Bob" || !cv.Lines[0].Me || cv.Lines[0].Rank != 1 || cv.Lines[0].Seen != 1 ||
		cv.Lines[1].Name != "Ann" || cv.Lines[1].Rank != 2 || cv.Lines[0].Points <= cv.Lines[1].Points {
		t.Fatalf("standings: %+v", cv.Lines)
	}
	if again, err := h.svc.TakeChallenge(ctx, bob, "Bob", code, "en"); err != nil || again.ID != bv.ID {
		t.Fatalf("once: %+v %v", again, err)
	}
	if mine, err := h.svc.TakeChallenge(ctx, ann, "Ann", code, "en"); err != nil || mine.ID != v.ID {
		t.Fatalf("the maker's attempt is her game: %+v %v", mine, err)
	}
	var kind string
	var place *int
	if err := h.pool.QueryRow(ctx, `SELECT kind, place FROM maply.free_result WHERE game = $1`, bv.ID).Scan(&kind, &place); err != nil || kind != free.Challenge || place != nil {
		t.Fatalf("bob's result: %s %v %v", kind, place, err)
	}
	home, err := h.svc.SoloHome(ctx, bob)
	if err != nil || len(home.Challenges) != 1 || home.Challenges[0].Code != code || home.Challenges[0].Players != 2 ||
		home.Challenges[0].By != "Ann" || home.Challenges[0].Maker || home.Challenges[0].Status != free.StatusDone {
		t.Fatalf("bob's screen: %+v %v", home.Challenges, err)
	}
	// Cleo starts and walks away: a day later her attempt is abandoned and
	// stays unranked.
	c, err := h.svc.TakeChallenge(ctx, cleo, "Cleo", code, "en")
	if err != nil {
		t.Fatal(err)
	}
	play(cleo, c.ID, time.Second, 1)
	h.tick(25 * time.Hour)
	if err := h.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	cv, _ = h.svc.ChallengeOf(ctx, ann, code)
	if len(cv.Lines) != 3 || cv.Lines[2].Name != "Cleo" || cv.Lines[2].Status != free.StatusAbandoned || cv.Lines[2].Played != 1 || cv.Lines[2].Rank != 0 {
		t.Fatalf("cleo walked away: %+v", cv.Lines)
	}
	if _, err := h.svc.ChallengeOf(ctx, ann, "ZZZZZZZZ"); !is(err, free.ErrNotFound) {
		t.Fatalf("no such challenge: %v", err)
	}
	if !free.Invitation("c"+code) || !free.Invitation("p"+code) || free.Invitation("x"+code) || free.Invitation("c") {
		t.Fatal("invitations")
	}
}
