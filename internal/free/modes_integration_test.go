// SPDX-License-Identifier: Apache-2.0
//go:build integration

package free_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
)

// started is a party on set that who[0] hosts and the others join, begun.
func (h *harness) started(set free.Settings, who ...free.Who) (string, free.PartyView) {
	h.t.Helper()
	lobby, err := h.svc.NewParty(h.ctx, who[0], "Host", "en", set)
	if err != nil {
		h.t.Fatal(err)
	}
	for i, w := range who[1:] {
		if _, err := h.svc.Join(h.ctx, w, string(rune('B'+i)), lobby.Code, "en"); err != nil {
			h.t.Fatal(err)
		}
	}
	v := h.view(h.svc, who[0], lobby.Code, 0)
	st, err := h.svc.StartParty(h.ctx, who[0], lobby.Code, "en", v.Version)
	if err != nil {
		h.t.Fatal(err)
	}
	return lobby.Code, st
}

// into moves the clock to a second into place n of a party, with everyone
// polling, and gives the view.
func (h *harness) into(code string, n int, who ...free.Who) free.PartyView {
	h.t.Helper()
	var r free.PartyRound
	for range 20 {
		v := h.view(h.svc, who[0], code, 0)
		for _, x := range v.Rounds {
			if x.N == n {
				r = x
			}
		}
		if r.N == n {
			break
		}
		h.tick(2 * time.Second)
	}
	if r.N != n {
		h.t.Fatalf("place %d never came", n)
	}
	if h.clock().Before(r.StartsAt) {
		h.jump(r.StartsAt.Add(time.Second))
	}
	var v free.PartyView
	for _, w := range who {
		v = h.view(h.svc, w, code, 0)
	}
	return v
}

func (h *harness) guessAt(code string, w free.Who, n int, pin *geo.Point) (free.PartyView, error) {
	h.t.Helper()
	return h.svc.GuessParty(h.ctx, w, code, "en", free.GuessIn{N: n, Pin: pin})
}

func (h *harness) mustGuess(code string, w free.Who, n int, pin *geo.Point) free.PartyView {
	h.t.Helper()
	v, err := h.guessAt(code, w, n, pin)
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

// roundN is place n in the view, or the zero round.
func roundN(v free.PartyView, n int) free.PartyRound {
	for _, r := range v.Rounds {
		if r.N == n {
			return r
		}
	}
	return free.PartyRound{}
}

func lineOf(v free.PartyView, m int) free.Line {
	for _, l := range v.Totals {
		if l.M == m {
			return l
		}
	}
	return free.Line{}
}

func TestLobbySwitchesModes(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	lobby, err := h.svc.NewParty(ctx, ann, "Ann", "en", free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 5})
	if err != nil {
		t.Fatal(err)
	}
	code := lobby.Code
	if _, err := h.svc.Join(ctx, bob, "Bob", code, "en"); err != nil {
		t.Fatal(err)
	}
	version := lobby.Version
	for _, set := range []free.Settings{
		{Mode: free.Streak, Seconds: 30},
		{Mode: free.Streak, Seconds: 60},
		{Mode: free.Timed, Seconds: free.TimedSeconds},
		{Mode: free.Classic, Seconds: 30, Rounds: 3},
	} {
		v, err := h.svc.SetParty(ctx, ann, code, "en", set)
		if err != nil {
			t.Fatalf("%+v: %v", set, err)
		}
		if v.Settings != set || v.Version != version+1 {
			t.Fatalf("lobby after %+v: %+v version %d", set, v.Settings, v.Version)
		}
		version = v.Version
		if b := h.view(h.svc, bob, code, 0); b.Settings != set {
			t.Fatalf("bob sees %+v, not %+v", b.Settings, set)
		}
	}
	if _, err := h.svc.SetParty(ctx, bob, code, "en", free.Settings{Mode: free.Streak, Seconds: 30}); !is(err, free.ErrNotHost) {
		t.Fatalf("bob switches the mode: %v", err)
	}
	// A day is chosen before the lobby, not in it, and a lobby on a day
	// keeps to it.
	for _, bad := range []free.Settings{
		{Mode: free.Archive, Seconds: 60, Rounds: 1}, {Mode: free.Streak, Seconds: 90}, {Mode: free.Timed, Seconds: 60},
		{Mode: free.Classic, Seconds: 0, Rounds: 3},
	} {
		if _, err := h.svc.SetParty(ctx, ann, code, "en", bad); !is(err, free.ErrBadRequest) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	// A streak lobby starts as a streak.
	if _, err := h.svc.SetParty(ctx, ann, code, "en", free.Settings{Mode: free.Streak, Seconds: 60}); err != nil {
		t.Fatal(err)
	}
	v := h.view(h.svc, ann, code, 0)
	st, err := h.svc.StartParty(ctx, ann, code, "en", v.Version)
	if err != nil || st.Settings.Mode != free.Streak || len(st.Rounds) != 1 || st.Rounds[0].EndsAt.Sub(st.Rounds[0].StartsAt) != 60*time.Second {
		t.Fatalf("started: %+v %v", st.Settings, err)
	}
	var mode string
	if err := h.pool.QueryRow(ctx, `SELECT mode FROM maply.free_game WHERE code = $1`, code).Scan(&mode); err != nil || mode != free.Streak {
		t.Fatalf("stored mode %q %v", mode, err)
	}
	// The lobby cannot be switched once the game has begun.
	if _, err := h.svc.SetParty(ctx, ann, code, "en", free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 5}); !is(err, free.ErrOver) {
		t.Fatalf("switching a started game: %v", err)
	}
}

func TestStreakParty(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	code, st := h.started(free.Settings{Mode: free.Streak, Seconds: 30}, ann, bob, cleo)
	if st.Kind != free.Party || len(st.Rounds) != 1 || st.Length != 0 {
		t.Fatalf("started: %+v", st)
	}
	// Place 1: ann and bob find the country, cleo's pin is in the sea.
	h.into(code, 1, ann, bob, cleo)
	h.mustGuess(code, ann, 1, at(h.partySpot(code, 1)))
	h.mustGuess(code, bob, 1, at(h.partySpot(code, 1)))
	v := h.mustGuess(code, cleo, 1, sea)
	if r := roundN(v, 1); r.ClosedAt == nil || len(r.Guesses) != 3 {
		t.Fatalf("everyone answered, the place closes: %+v", r)
	}
	// The reveal has everyone's answer, with the country.
	oks := map[int]bool{}
	for _, g := range roundN(v, 1).Guesses {
		oks[g.M] = g.Country
	}
	if !oks[1] || !oks[2] || oks[3] {
		t.Fatalf("countries: %v", oks)
	}
	if !v.Members[2].Out || v.Members[0].Out || v.Members[1].Out {
		t.Fatalf("cleo is out: %+v", v.Members)
	}
	// Out, she watches: the next place is drawn, and her pin is refused.
	v = h.into(code, 2, ann, bob, cleo)
	if len(v.Rounds) != 2 || roundN(v, 2).ClosedAt != nil {
		t.Fatalf("place 2 begins: %+v", v.Rounds)
	}
	// Who stood for each place: all three for the first, ann and bob for the second.
	if p1, p2 := roundN(v, 1).Playing, roundN(v, 2).Playing; len(p1) != 3 || len(p2) != 2 || p2[0] != 1 || p2[1] != 2 {
		t.Fatalf("playing: %v %v", p1, p2)
	}
	if _, err := h.guessAt(code, cleo, 2, at(h.partySpot(code, 2))); !is(err, free.ErrOut) {
		t.Fatalf("cleo answers when out: %v", err)
	}
	// Place 2 waits for ann and bob and not for cleo; bob misses the country.
	h.mustGuess(code, ann, 2, at(h.partySpot(code, 2)))
	if p := h.view(h.svc, cleo, code, 0); roundN(p, 2).ClosedAt != nil {
		t.Fatalf("place 2 still waits for bob")
	}
	v = h.mustGuess(code, bob, 2, sea)
	if roundN(v, 2).ClosedAt == nil {
		t.Fatalf("place 2 closes on the last standing answer")
	}
	// Ann is the one left, and the game goes on for her alone.
	for n := 3; n <= 4; n++ {
		v = h.into(code, n, ann)
		if roundN(v, n).N != n {
			t.Fatalf("place %d never came: %+v", n, v.Rounds)
		}
		v = h.mustGuess(code, ann, n, at(h.partySpot(code, n)))
		if roundN(v, n).ClosedAt == nil {
			t.Fatalf("place %d closes on ann's answer alone", n)
		}
	}
	// A long streak does not make a long poll: the place in play and the one
	// before it.
	if v = h.into(code, 5, ann); len(v.Rounds) != 2 || v.Rounds[0].N != 4 || v.Rounds[1].N != 5 {
		t.Fatalf("rounds in a poll: %d", len(v.Rounds))
	}
	// Ann's time runs out on place 5 without a pin: nobody stands, the game
	// ends after the reveal.
	h.jump(v.Rounds[1].EndsAt.Add(free.Grace))
	v = h.view(h.svc, ann, code, 0)
	if roundN(v, 5).ClosedAt == nil || v.Status != free.StatusPlaying {
		t.Fatalf("place 5 missed: %s %+v", v.Status, v.Rounds)
	}
	if _, err := h.guessAt(code, ann, 6, sea); !is(err, free.ErrOrder) {
		t.Fatalf("no place 6: %v", err)
	}
	h.tick(free.RevealFor(30))
	v = h.view(h.svc, ann, code, 0)
	if v.Status != free.StatusDone || len(v.Rounds) != 5 {
		t.Fatalf("over: %s with %d places", v.Status, len(v.Rounds))
	}
	// Ranked by the longest streak, then points: ann 4, bob 1, cleo 0.
	a, b, c := lineOf(v, 1), lineOf(v, 2), lineOf(v, 3)
	if a.Streak != 4 || b.Streak != 1 || c.Streak != 0 || a.Rank != 1 || b.Rank != 2 || c.Rank != 3 {
		t.Fatalf("totals: %+v %+v %+v", a, b, c)
	}
	// Bob's and cleo's lines stop where their streaks did.
	if b.Played != 2 || c.Played != 1 || a.Played != 5 {
		t.Fatalf("played: %d %d %d", a.Played, b.Played, c.Played)
	}
	rows, err := h.pool.Query(ctx, `SELECT m, streak, place FROM maply.free_result WHERE game = (SELECT id FROM maply.free_game WHERE code = $1) ORDER BY m`, code)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := [][3]int{{1, 4, 1}, {2, 1, 2}, {3, 0, 3}}
	for i := 0; rows.Next(); i++ {
		var got [3]int
		if err := rows.Scan(&got[0], &got[1], &got[2]); err != nil || got != want[i] {
			t.Fatalf("result row %d: %v %v", i, got, err)
		}
	}
	// The party's streak is on the same board as a solo one, and the player's
	// best.
	best, err := h.store.FreeBest(ctx, ann, 0)
	if err != nil || best["streak/30"] != 4 {
		t.Fatalf("ann's best: %v %v", best, err)
	}
}

func TestStreakPartyEndsWhenNobodyStands(t *testing.T) {
	h := setup(t)
	code, _ := h.started(free.Settings{Mode: free.Streak, Seconds: 60}, ann, bob)
	h.into(code, 1, ann, bob)
	h.mustGuess(code, ann, 1, sea)
	v := h.mustGuess(code, bob, 1, sea)
	if roundN(v, 1).ClosedAt == nil || len(v.Rounds) != 1 {
		t.Fatalf("both missed: %+v", v.Rounds)
	}
	h.tick(free.RevealFor(60) + time.Second)
	v = h.view(h.svc, ann, code, 0)
	if v.Status != free.StatusDone || len(v.Rounds) != 1 {
		t.Fatalf("no place is drawn for nobody: %s %d", v.Status, len(v.Rounds))
	}
	if l := lineOf(v, 1); l.Streak != 0 || l.Rank != 1 {
		t.Fatalf("a tie on nothing: %+v", l)
	}
	if _, err := h.svc.Join(h.ctx, cleo, "Cleo", code, "en"); !is(err, free.ErrOver) {
		t.Fatalf("joining a finished game: %v", err)
	}
}

func TestStreakPartyEndsWithThePool(t *testing.T) {
	h := setup(t)
	code, _ := h.started(free.Settings{Mode: free.Streak, Seconds: 30}, ann, bob)
	// Bob is out at once; ann goes on until the twelve places of the pool
	// are used, and the game ends without a thirteenth.
	h.into(code, 1, ann, bob)
	h.mustGuess(code, ann, 1, at(h.partySpot(code, 1)))
	h.mustGuess(code, bob, 1, sea)
	for n := 2; n <= len(spots); n++ {
		h.into(code, n, ann)
		h.mustGuess(code, ann, n, at(h.partySpot(code, n)))
	}
	h.tick(free.RevealFor(30) + free.Countdown)
	v := h.view(h.svc, ann, code, 0)
	if v.Status != free.StatusDone || len(v.Rounds) != len(spots) {
		t.Fatalf("the pool ran dry: %s with %d places", v.Status, len(v.Rounds))
	}
	if l := lineOf(v, 1); l.Streak != len(spots) {
		t.Fatalf("ann's streak: %+v", l)
	}
}

func TestStreakPartyLateJoiner(t *testing.T) {
	h := setup(t)
	code, _ := h.started(free.Settings{Mode: free.Streak, Seconds: 30}, ann, bob)
	h.into(code, 1, ann, bob)
	h.mustGuess(code, ann, 1, sea)
	// Cleo comes in during place 1, plays from place 2, and is standing for it
	// when the others are out.
	c, err := h.svc.Join(h.ctx, cleo, "Cleo", code, "en")
	if err != nil || c.Members[2].From != 2 {
		t.Fatalf("cleo joins: %v %+v", err, c.Members)
	}
	h.mustGuess(code, bob, 1, sea)
	v := h.into(code, 2, ann, bob, cleo)
	if len(v.Rounds) != 2 {
		t.Fatalf("the game goes on for cleo: %+v", v.Rounds)
	}
	if _, err := h.guessAt(code, ann, 2, at(h.partySpot(code, 2))); !is(err, free.ErrOut) {
		t.Fatalf("ann is out: %v", err)
	}
	h.mustGuess(code, cleo, 2, at(h.partySpot(code, 2)))
}

func TestTimedParty(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	set := free.Settings{Mode: free.Timed, Seconds: free.TimedSeconds}
	code, st := h.started(set, ann, bob)
	if st.Length != int((free.Countdown+free.TimedSeconds*time.Second+free.Grace)/time.Second) || st.Kind != free.Duel {
		t.Fatalf("started: length %d kind %s", st.Length, st.Kind)
	}
	r1 := st.Rounds[0]
	if r1.EndsAt.Sub(r1.StartsAt) != 3*time.Minute || st.EndsAt == nil || !st.EndsAt.Equal(*r1.EndsAt) {
		t.Fatalf("one shared three minutes: %+v", r1)
	}
	if _, err := h.svc.Join(ctx, cleo, "Cleo", code, "en"); !is(err, free.ErrTooLate) {
		t.Fatalf("joining a game against the clock under way: %v", err)
	}
	if _, err := h.guessAt(code, ann, 1, at(h.partySpot(code, 1))); !is(err, free.ErrNotYet) {
		t.Fatalf("a pin in the countdown: %v", err)
	}
	h.into(code, 1, ann, bob)
	end := *r1.EndsAt
	// Ann is quick: place 1 right, place 2 passed, place 3 right.
	v := h.mustGuess(code, ann, 1, at(h.partySpot(code, 1)))
	if len(v.Rounds) != 2 || v.Rounds[0].Place == nil || v.Rounds[1].Place != nil || v.Rounds[1].ClosedAt != nil {
		t.Fatalf("ann sees her place and the next: %+v", v.Rounds)
	}
	if len(v.Totals) != 0 || v.Best != nil {
		t.Fatalf("no standings while it is played: %+v", v.Totals)
	}
	h.tick(5 * time.Second)
	if _, err := h.guessAt(code, ann, 2, nil); err == nil {
		t.Fatal("a pin of nothing is not a pass")
	}
	v, err := h.svc.GuessParty(ctx, ann, code, "en", free.GuessIn{N: 2, Skip: true})
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	h.tick(5 * time.Second)
	v = h.mustGuess(code, ann, 3, at(h.partySpot(code, 3)))
	if _, err := h.guessAt(code, ann, 3, sea); !is(err, free.ErrAnswered) {
		t.Fatalf("second answer to place 3: %v", err)
	}
	if _, err := h.guessAt(code, ann, 5, sea); !is(err, free.ErrOrder) {
		t.Fatalf("a place ann has not reached: %v", err)
	}
	// Bob is at place 1 still. He sees nothing of ann's game but her count.
	b := h.view(h.svc, bob, code, 0)
	body, _ := json.Marshal(b)
	if len(b.Rounds) != 1 || b.Rounds[0].N != 1 || b.Rounds[0].Place != nil || b.Rounds[0].ClosedAt != nil {
		t.Fatalf("bob has place 1 alone: %+v", b.Rounds)
	}
	noPlace(t, "bob's view of a game against the clock", b)
	if strings.Contains(string(body), v.Rounds[len(v.Rounds)-1].Image) {
		t.Fatalf("bob has the address of ann's place")
	}
	if a := b.Members[0]; a.Done != 3 || a.Countries != 2 || a.Points == 0 || b.Members[1].Done != 0 {
		t.Fatalf("the ribbon: %+v", b.Members)
	}
	if _, err := h.guessAt(code, bob, 3, sea); !is(err, free.ErrOrder) {
		t.Fatalf("bob skips ahead: %v", err)
	}
	// Bob answers 1 and 2; place 3 is the one ann had, drawn once.
	h.tick(10 * time.Second)
	b = h.mustGuess(code, bob, 1, sea)
	if len(b.Rounds) != 2 || b.Rounds[0].Guesses[0].Country || len(b.Rounds[0].Guesses) != 1 {
		t.Fatalf("bob sees his own pin: %+v", b.Rounds)
	}
	h.mustGuess(code, bob, 2, at(h.partySpot(code, 2)))
	b = h.mustGuess(code, bob, 3, at(h.partySpot(code, 3)))
	var rounds int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM maply.free_round WHERE game = (SELECT id FROM maply.free_game WHERE code = $1)`, code).Scan(&rounds); err != nil || rounds != 4 {
		t.Fatalf("places drawn: %d %v", rounds, err)
	}
	if b.Rounds[len(b.Rounds)-1].N != 4 {
		t.Fatalf("bob is at place 4: %+v", b.Rounds)
	}
	// The grace after the three minutes still takes a pin; the game is over
	// for everyone after it.
	h.jump(end.Add(time.Second))
	h.mustGuess(code, ann, 4, at(h.partySpot(code, 4)))
	if v = h.view(h.svc, bob, code, 0); v.Status != free.StatusPlaying {
		t.Fatalf("in the grace: %s", v.Status)
	}
	h.jump(end.Add(free.Grace))
	v = h.view(h.svc, bob, code, 0)
	if v.Status != free.StatusDone {
		t.Fatalf("over at the end and the grace: %s", v.Status)
	}
	if _, err := h.guessAt(code, bob, 4, sea); !is(err, free.ErrOver) {
		t.Fatalf("a pin after the end: %v", err)
	}
	// Now the places and everyone's pins are open.
	if len(v.Rounds) != 4 {
		t.Fatalf("every place reached: %d", len(v.Rounds))
	}
	for _, r := range v.Rounds {
		if r.Place == nil {
			t.Fatalf("place %d is closed at the end", r.N)
		}
	}
	if len(v.Rounds[0].Guesses) != 2 || len(v.Rounds[3].Guesses) != 2 {
		t.Fatalf("both answers to places 1 and 4: %d %d", len(v.Rounds[0].Guesses), len(v.Rounds[3].Guesses))
	}
	// Ann's answer to 4 came in the grace, so no place 5 was drawn; bob was
	// on 4 when the time ran out: a miss for him.
	for _, g := range v.Rounds[3].Guesses {
		if g.M == 2 && g.Lat != nil {
			t.Fatalf("bob's miss on place 4: %+v", g)
		}
	}
	// Ann found 3 countries (1, 3, 4), bob 2 (2, 3): countries decide.
	a, bb := lineOf(v, 1), lineOf(v, 2)
	if a.Countries != 3 || bb.Countries != 2 || a.Rank != 1 || bb.Rank != 2 {
		t.Fatalf("standings: %+v %+v", a, bb)
	}
	if a.Played != 4 || bb.Played != 4 {
		t.Fatalf("played: %d %d", a.Played, bb.Played)
	}
	// The result is the solo game's, in the board of countries.
	best, err := h.store.FreeBest(ctx, ann, 0)
	if err != nil || best["timed/180"] != 3 {
		t.Fatalf("ann's best: %v %v", best, err)
	}
	var kind string
	if err := h.pool.QueryRow(ctx, `SELECT kind FROM maply.free_result WHERE game = (SELECT id FROM maply.free_game WHERE code = $1) AND m = 2`, code).Scan(&kind); err != nil || kind != free.Duel {
		t.Fatalf("bob's result: %q %v", kind, err)
	}
}

// A game against the clock whose players go quiet is finished by the sweep.
func TestTimedPartyIsSweptAtItsEnd(t *testing.T) {
	h := setup(t)
	code, st := h.started(free.Settings{Mode: free.Timed, Seconds: free.TimedSeconds}, ann, bob)
	h.into(code, 1, ann, bob)
	h.mustGuess(code, ann, 1, at(h.partySpot(code, 1)))
	h.jump(st.EndsAt.Add(time.Minute))
	if err := h.svc.Sweep(h.ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	var n int
	if err := h.pool.QueryRow(h.ctx, `SELECT g.status, (SELECT count(*) FROM maply.free_result r WHERE r.game = g.id) FROM maply.free_game g WHERE g.code = $1`, code).Scan(&status, &n); err != nil || status != free.StatusDone || n != 2 {
		t.Fatalf("swept: %q %d %v", status, n, err)
	}
	v := h.view(h.svc, bob, code, 0)
	// Bob never answered: place 2 was ann's, and his place 1 a miss.
	if l := lineOf(v, 2); l.Answered != 0 || l.Played != 1 || l.Rank != 2 {
		t.Fatalf("bob: %+v", l)
	}
}

func TestPartyPassIsForTheClockOnly(t *testing.T) {
	h := setup(t)
	code, _ := h.started(free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3}, ann, bob)
	h.into(code, 1, ann, bob)
	if _, err := h.svc.GuessParty(h.ctx, ann, code, "en", free.GuessIn{N: 1, Skip: true}); !is(err, free.ErrBadRequest) {
		t.Fatalf("passing a classic place: %v", err)
	}
}

func TestArchiveParty(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	answered := h.addPlace("answered", "used", spot{"SE", "europe", 59.3293, 18.0686})
	open := h.addPlace("open", "used", spot{"FI", "europe", 60.1699, 24.9384})
	live := h.addPlace("live", "used", spot{"DK", "europe", 55.6761, 12.5683})
	h.day(1, round.NewDate(2026, time.October, 20), answered, true)
	h.day(2, round.NewDate(2026, time.October, 21), open, true)
	h.day(3, round.NewDate(2026, time.October, 22), live, false)
	dave := free.Who{UserID: 104}
	for _, w := range []free.Who{ann, bob, cleo, dave} {
		if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'P')`, w.UserID); err != nil {
			t.Fatal(err)
		}
	}
	// Cleo answered day 2 on the day; dave had its place in a free game.
	if _, err := h.pool.Exec(ctx, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
		VALUES ('2026-10-21', $1, 'dm', 0, 0, '2026-10-21 10:00Z', '2026-10-21 18:00Z')`, cleo.UserID); err != nil {
		t.Fatal(err)
	}
	h.seenBy(dave, "Dave", open)
	// Ann answered day 1, so she cannot host it; a day still in play is no
	// day for the archive; the settings and the day go together.
	set := free.Settings{Mode: free.Archive, Seconds: 90, Rounds: 1}
	if _, err := h.pool.Exec(ctx, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
		VALUES ('2026-10-20', $1, 'dm', 0, 0, '2026-10-20 10:00Z', '2026-10-20 18:00Z')`, ann.UserID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		set free.Settings
		day int
		err error
	}{
		{set, 1, free.ErrDayAnswered}, {set, 3, free.ErrNotFound}, {set, 9, free.ErrNotFound}, {set, 0, free.ErrBadRequest},
		{free.Settings{Mode: free.Archive, Seconds: 0, Rounds: 1}, 2, free.ErrBadRequest},
		{free.Settings{Mode: free.Archive, Seconds: 30, Rounds: 1}, 2, free.ErrBadRequest},
		{free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3}, 2, free.ErrBadRequest},
	} {
		if _, err := h.svc.NewArchiveParty(ctx, ann, "Ann", "en", c.set, c.day); !is(err, c.err) {
			t.Fatalf("%+v day %d: %v, want %v", c.set, c.day, err, c.err)
		}
	}
	if _, err := h.svc.NewParty(ctx, ann, "Ann", "en", set); !is(err, free.ErrBadRequest) {
		t.Fatalf("a lobby on a day without a day: %v", err)
	}
	lobby, err := h.svc.NewArchiveParty(ctx, ann, "Ann", "en", set, 2)
	if err != nil {
		t.Fatal(err)
	}
	if lobby.Day == nil || lobby.Day.N != 2 || lobby.Day.Date != "2026-10-21" || lobby.Settings != set || lobby.Length != 90+2+10+3 {
		t.Fatalf("lobby: %+v %+v", lobby.Day, lobby.Settings)
	}
	noPlace(t, "a lobby on a day", lobby)
	code := lobby.Code
	// Whoever answered the day or had its place cannot join.
	if _, err := h.svc.Join(ctx, cleo, "Cleo", code, "en"); !is(err, free.ErrDayAnswered) {
		t.Fatalf("cleo answered it: %v", err)
	}
	if _, err := h.svc.Join(ctx, dave, "Dave", code, "en"); !is(err, free.ErrDaySeen) {
		t.Fatalf("dave had it: %v", err)
	}
	if _, err := h.svc.Join(ctx, bob, "Bob", code, "en"); err != nil {
		t.Fatal(err)
	}
	// The host picks a timer of the three, and nothing else changes.
	if v, err := h.svc.SetParty(ctx, ann, code, "en", free.Settings{Mode: free.Archive, Seconds: 120, Rounds: 1}); err != nil || v.Settings.Seconds != 120 {
		t.Fatalf("a longer timer: %v", err)
	}
	for _, bad := range []free.Settings{{Mode: free.Archive, Seconds: 30, Rounds: 1}, {Mode: free.Classic, Seconds: 60, Rounds: 3}, {Mode: free.Streak, Seconds: 60}} {
		if _, err := h.svc.SetParty(ctx, ann, code, "en", bad); !is(err, free.ErrBadRequest) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	set.Seconds = 60
	lobbyNow, err := h.svc.SetParty(ctx, ann, code, "en", set)
	if err != nil {
		t.Fatal(err)
	}
	// Bob answers the day before the start: the start refuses.
	if _, err := h.pool.Exec(ctx, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
		VALUES ('2026-10-21', $1, 'dm', 0, 0, '2026-10-21 11:00Z', '2026-10-21 18:00Z')`, bob.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartParty(ctx, ann, code, "en", lobbyNow.Version); !is(err, free.ErrDayAnswered) {
		t.Fatalf("a start with a member who answered: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM maply.guess WHERE user_id = $1`, bob.UserID); err != nil {
		t.Fatal(err)
	}
	st, err := h.svc.StartParty(ctx, ann, code, "en", lobbyNow.Version)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != free.Duel || len(st.Rounds) != 1 || st.Rounds[0].EndsAt.Sub(st.Rounds[0].StartsAt) != 60*time.Second {
		t.Fatalf("started: %+v", st)
	}
	noPlace(t, "a party on a day, started", st)
	if h.partySpot(code, 1).iso != "FI" {
		t.Fatalf("the day's place")
	}
	if _, err := h.svc.Join(ctx, dave, "Dave", code, "en"); !is(err, free.ErrDaySeen) && !is(err, free.ErrTooLate) {
		t.Fatalf("joining the game: %v", err)
	}
	// One place, everyone's pin, a table.
	h.into(code, 1, ann, bob)
	h.mustGuess(code, ann, 1, at(h.partySpot(code, 1)))
	v := h.mustGuess(code, bob, 1, sea)
	if len(v.Rounds) != 1 || v.Rounds[0].ClosedAt == nil || len(v.Rounds[0].Guesses) != 2 || v.Rounds[0].Place.Country != "FI" {
		t.Fatalf("reveal: %+v", v.Rounds)
	}
	if v.Totals[0].M != 1 || v.Totals[1].M != 2 {
		t.Fatalf("table: %+v", v.Totals)
	}
	h.tick(free.RevealFor(60))
	v = h.view(h.svc, bob, code, 0)
	if v.Status != free.StatusDone || v.Day == nil || v.Day.N != 2 {
		t.Fatalf("done: %s %+v", v.Status, v.Day)
	}
	if _, err := h.svc.Rematch(ctx, ann, "Ann", code); !is(err, free.ErrBadRequest) {
		t.Fatalf("a rematch of a day: %v", err)
	}
	// No record, no row in the day's table, and the place is theirs to have
	// seen: the archive says so.
	var mode string
	if err := h.pool.QueryRow(ctx, `SELECT mode FROM maply.free_result WHERE game = (SELECT id FROM maply.free_game WHERE code = $1) AND m = 1`, code).Scan(&mode); err != nil || mode != free.Archive {
		t.Fatalf("result: %q %v", mode, err)
	}
	if best, err := h.store.FreeBest(ctx, ann, 0); err != nil || len(best) != 0 {
		t.Fatalf("no record of a day: %v %v", best, err)
	}
	var dayRows int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM maply.day_place`).Scan(&dayRows); err != nil || dayRows != 0 {
		t.Fatalf("the day's table: %d %v", dayRows, err)
	}
	if _, err := h.svc.NewArchive(ctx, bob, "Bob", 2, "en"); !is(err, free.ErrDaySeen) {
		t.Fatalf("bob plays the day alone after: %v", err)
	}
	days, err := h.svc.ArchiveOf(ctx, bob)
	if err != nil || days[0].N != 2 || days[0].State != free.DaySeen {
		t.Fatalf("bob's archive: %+v %v", days, err)
	}
}
