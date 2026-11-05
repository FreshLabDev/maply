// SPDX-License-Identifier: Apache-2.0
//go:build integration

package free_test

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/free"
)

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// googler makes an account with Google alone and its site player, as the
// site does on the account's first game, and gives the player as the free
// game knows it.
func (h *harness) googler(sub, name string) free.Who {
	h.t.Helper()
	acc, _, err := h.store.SignIn(h.ctx, db.Identity{Kind: db.KindGoogle, GoogleSub: sub}, "", db.Rules{})
	if err != nil {
		h.t.Fatal(err)
	}
	var n *string
	if name != "" {
		n = &name
	}
	p, err := h.store.AccountSitePlayer(h.ctx, acc.ID, newUUID(), n)
	if err != nil {
		h.t.Fatal(err)
	}
	return free.Who{Player: p, Account: true}
}

// guestOf is a site guest: a browser's player that no account has.
func (h *harness) guestOf() free.Who {
	h.t.Helper()
	w := free.Who{Player: newUUID()}
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO maply.site_player (id) VALUES ($1)`, w.Player); err != nil {
		h.t.Fatal(err)
	}
	return w
}

func (h *harness) jump(t time.Time) {
	h.mu.Lock()
	h.now = t
	h.mu.Unlock()
}

// playSolo answers every place of a solo game, at the place or in the sea.
func (h *harness) playSolo(w free.Who, id int64, places int, onTarget bool) free.SoloView {
	h.t.Helper()
	var v free.SoloView
	for n := 1; n <= places; n++ {
		if _, err := h.svc.StartSolo(h.ctx, w, id, n); err != nil {
			h.t.Fatal(err)
		}
		h.tick(5 * time.Second)
		pin := sea
		if onTarget {
			pin = at(h.spotOf(id, n))
		}
		var err error
		if v, err = h.svc.GuessSolo(h.ctx, w, id, free.GuessIn{N: n, Pin: pin}, "en"); err != nil {
			h.t.Fatal(err)
		}
	}
	return v
}

// playParty plays a started party to its end, everyone answering every
// place at the place, and gives the final view.
func (h *harness) playParty(code string, who ...free.Who) free.PartyView {
	h.t.Helper()
	for range 100 {
		v := h.view(h.svc, who[0], code, 0)
		if v.Status == free.StatusDone {
			return v
		}
		r := v.Rounds[len(v.Rounds)-1]
		if r.ClosedAt != nil {
			h.tick(2 * time.Second)
			continue
		}
		if h.clock().Before(r.StartsAt) {
			h.jump(r.StartsAt.Add(time.Second))
		}
		for _, w := range who {
			h.view(h.svc, w, code, 0)
		}
		for _, w := range who {
			if _, err := h.svc.GuessParty(h.ctx, w, code, "en", free.GuessIn{N: r.N, Pin: at(h.partySpot(code, r.N))}); err != nil {
				h.t.Fatal(err)
			}
		}
	}
	h.t.Fatal("the party never ended")
	return free.PartyView{}
}

// resultsOf is who a finished game's line is for: "user 101" or "player <id>".
func (h *harness) resultsOf(game int64) map[int]string {
	h.t.Helper()
	rows, err := h.pool.Query(h.ctx, `SELECT m, COALESCE('user ' || user_id::text, 'player ' || player::text) FROM maply.free_result WHERE game = $1`, game)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var m int
		var who string
		if err := rows.Scan(&m, &who); err != nil {
			h.t.Fatal(err)
		}
		out[m] = who
	}
	return out
}

// An account with Google alone keeps records as its site player; a guest
// keeps none, even when a caller hands the store a guest's line.
func TestAnAccountKeepsRecords(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	lena := h.googler("g-lena", "Lena")
	if !lena.Keeps() || lena.Telegram() {
		t.Fatalf("lena: %+v", lena)
	}
	set := free.Settings{Mode: free.Classic, Seconds: 30, Rounds: 3}
	v, err := h.svc.NewSolo(ctx, lena, "Lena", set)
	if err != nil {
		t.Fatal(err)
	}
	v = h.playSolo(lena, v.ID, 3, true)
	if v.Status != free.StatusDone || v.Final == nil || v.Final.Best != nil || v.Final.Record {
		t.Fatalf("lena's first game: %+v", v.Final)
	}
	if got := h.resultsOf(v.ID); len(got) != 1 || got[1] != "player "+lena.Player {
		t.Fatalf("lena's line: %+v", got)
	}
	home, err := h.svc.SoloHome(ctx, lena)
	if err != nil || home.Best[set.Key()] != v.Final.Points || home.Last != set {
		t.Fatalf("lena's records: %+v %v", home, err)
	}
	// A worse game reads against the record and leaves it be.
	w, err := h.svc.NewSolo(ctx, lena, "Lena", set)
	if err != nil {
		t.Fatal(err)
	}
	w = h.playSolo(lena, w.ID, 3, false)
	if w.Final == nil || w.Final.Best == nil || *w.Final.Best != v.Final.Points || w.Final.Record {
		t.Fatalf("lena's second game: %+v", w.Final)
	}

	// A guest plays the same and keeps nothing.
	guest := h.guestOf()
	if guest.Keeps() {
		t.Fatal("a guest keeps records")
	}
	g, err := h.svc.NewSolo(ctx, guest, "", set)
	if err != nil {
		t.Fatal(err)
	}
	if g = h.playSolo(guest, g.ID, 3, true); g.Status != free.StatusDone || g.Final.Best != nil {
		t.Fatalf("guest final: %+v", g.Final)
	}
	if got := h.resultsOf(g.ID); len(got) != 0 {
		t.Fatalf("guest lines: %+v", got)
	}
	// The store itself writes no line for a player no account has.
	g2, err := h.svc.NewSolo(ctx, guest, "", set)
	if err != nil {
		t.Fatal(err)
	}
	forged := free.Result{Line: free.Line{M: 1, Points: 9000, Played: 3, FromRound: 1, KmRounds: []float64{}},
		Player: guest.Player, Kind: free.Solo, Settings: set, At: h.clock()}
	if ok, err := h.store.FinishFree(ctx, g2.ID, free.StatusDone, h.clock(), []free.Result{forged}); err != nil || !ok {
		t.Fatalf("finish: %v %v", ok, err)
	}
	if got := h.resultsOf(g2.ID); len(got) != 0 {
		t.Fatalf("a forged guest line landed: %+v", got)
	}
	// An account's player that lost its account, deleted, keeps no more.
	ivy := h.googler("g-ivy", "")
	var acc string
	if err := h.pool.QueryRow(ctx, `SELECT account_id::text FROM maply.site_player WHERE id = $1`, ivy.Player).Scan(&acc); err != nil {
		t.Fatal(err)
	}
	i, err := h.svc.NewSolo(ctx, ivy, "Ivy", set)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.DeleteAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}
	h.playSolo(ivy, i.ID, 3, true)
	if got := h.resultsOf(i.ID); len(got) != 0 {
		t.Fatalf("a deleted account's line: %+v", got)
	}
}

// An account with Google alone plays with friends: it makes a lobby, a
// Telegram person and another account join, and every one of them gets a
// line. A guest is asked to sign in.
func TestAnAccountPlaysWithFriends(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	lena := h.googler("g-lena", "Lena")
	maks := h.googler("g-max", "")
	set := free.Settings{Mode: free.Classic, Seconds: 30, Rounds: 3}
	lobby, err := h.svc.NewParty(ctx, lena, "Lena", "en", set)
	if err != nil {
		t.Fatal(err)
	}
	code := lobby.Code
	if lobby.Me != 1 || lobby.Members[0].Name != "Lena" {
		t.Fatalf("lena's lobby: %+v", lobby)
	}
	if _, err := h.svc.Join(ctx, h.guestOf(), "", code, "en"); !is(err, free.ErrSignIn) {
		t.Fatalf("a guest joins: %v", err)
	}
	if _, err := h.svc.Poll(ctx, h.guestOf(), code, 0, "en"); !is(err, free.ErrSignIn) {
		t.Fatalf("a guest polls: %v", err)
	}
	// The same player without the mark of an account is a guest.
	if _, err := h.svc.Poll(ctx, free.Who{Player: lena.Player}, code, 0, "en"); !is(err, free.ErrSignIn) {
		t.Fatalf("lena's player as a guest: %v", err)
	}
	if _, err := h.svc.Join(ctx, ann, "Ann", code, "en"); err != nil {
		t.Fatal(err)
	}
	m, err := h.svc.Join(ctx, maks, "Guest", code, "en")
	if err != nil || m.Me != 3 || len(m.Members) != 3 {
		t.Fatalf("maks joins: %+v %v", m, err)
	}
	if again, err := h.svc.Join(ctx, maks, "Guest", code, "en"); err != nil || again.Me != 3 || len(again.Members) != 3 {
		t.Fatalf("maks opens the link again: %+v %v", again, err)
	}
	if home, err := h.svc.SoloHome(ctx, lena); err != nil || home.Party != code {
		t.Fatalf("lena's way back to the lobby: %+v %v", home, err)
	}
	if _, err := h.svc.StartParty(ctx, maks, code, "en", lobby.Version); !is(err, free.ErrNotHost) {
		t.Fatalf("maks starts: %v", err)
	}
	if _, err := h.svc.StartParty(ctx, lena, code, "en", lobby.Version); err != nil {
		t.Fatal(err)
	}
	final := h.playParty(code, lena, ann, maks)
	if len(final.Totals) != 3 {
		t.Fatalf("totals: %+v", final.Totals)
	}
	var game int64
	if err := h.pool.QueryRow(ctx, `SELECT id FROM maply.free_game WHERE code = $1`, code).Scan(&game); err != nil {
		t.Fatal(err)
	}
	want := map[int]string{1: "player " + lena.Player, 2: fmt.Sprint("user ", ann.UserID), 3: "player " + maks.Player}
	if got := h.resultsOf(game); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("lines %v, want %v", got, want)
	}
	if home, err := h.svc.SoloHome(ctx, maks); err != nil || home.Best[set.Key()] == 0 || home.Party != "" {
		t.Fatalf("maks's record: %+v %v", home, err)
	}
	// An account asks for the rematch, and the others come along.
	next, err := h.svc.Rematch(ctx, maks, "Guest", code)
	if err != nil || !free.ValidCode(next) {
		t.Fatalf("rematch: %q %v", next, err)
	}
	if v, err := h.svc.Join(ctx, lena, "Lena", next, "en"); err != nil || v.Host != 1 || v.Me != 2 {
		t.Fatalf("lena in the rematch: %+v %v", v, err)
	}
}

// A Telegram person and an account with Google alone take each other's
// challenges; a guest is asked to sign in, even to look.
func TestAChallengeBetweenTelegramAndAnAccount(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'Ann')`, ann.UserID); err != nil {
		t.Fatal(err)
	}
	lena := h.googler("g-lena", "Lena")
	set := free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3}
	a, err := h.svc.NewSolo(ctx, ann, "Ann", set)
	if err != nil {
		t.Fatal(err)
	}
	a = h.playSolo(ann, a.ID, 3, false)
	code, err := h.svc.MakeChallenge(ctx, ann, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	guest := h.guestOf()
	if _, err := h.svc.ChallengeOf(ctx, guest, code); !is(err, free.ErrSignIn) {
		t.Fatalf("a guest looks: %v", err)
	}
	if _, err := h.svc.TakeChallenge(ctx, guest, "", code, "en"); !is(err, free.ErrSignIn) {
		t.Fatalf("a guest takes it: %v", err)
	}
	// An account that has not played yet has no player, and may look.
	if cv, err := h.svc.ChallengeOf(ctx, free.Who{Account: true}, code); err != nil || cv.Mine != nil || cv.By != "Ann" {
		t.Fatalf("a new account looks: %+v %v", cv, err)
	}
	lv, err := h.svc.TakeChallenge(ctx, lena, "Lena", code, "en")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := h.svc.TakeChallenge(ctx, lena, "Lena", code, "en"); err != nil || again.ID != lv.ID {
		t.Fatalf("once: %+v %v", again, err)
	}
	var user *int64
	var player string
	if err := h.pool.QueryRow(ctx, `SELECT user_id, player::text FROM maply.free_attempt WHERE game = $1`, lv.ID).Scan(&user, &player); err != nil || user != nil || player != lena.Player {
		t.Fatalf("lena's attempt: %v %q %v", user, player, err)
	}
	lv = h.playSolo(lena, lv.ID, 3, true)
	if lv.Status != free.StatusDone || lv.Final.Best != nil || lv.Final.Record {
		t.Fatalf("lena's attempt is over, with no record: %+v", lv.Final)
	}
	cv, err := h.svc.ChallengeOf(ctx, lena, code)
	if err != nil || cv.Mine == nil || *cv.Mine != lv.ID || len(cv.Lines) != 2 || cv.Lines[0].Name != "Lena" || !cv.Lines[0].Me || cv.Lines[1].Me {
		t.Fatalf("lena sees it: %+v %v", cv, err)
	}
	if cv, err := h.svc.ChallengeOf(ctx, ann, code); err != nil || cv.Lines[0].Me || !cv.Lines[1].Me || !cv.Lines[1].Maker {
		t.Fatalf("ann sees it: %+v %v", cv, err)
	}
	if got := h.resultsOf(lv.ID); got[1] != "player "+lena.Player {
		t.Fatalf("lena's line: %+v", got)
	}
	if home, err := h.svc.SoloHome(ctx, lena); err != nil || len(home.Challenges) != 1 || home.Challenges[0].Code != code || home.Challenges[0].Maker {
		t.Fatalf("lena's screen: %+v %v", home.Challenges, err)
	}

	// Lena makes one of her own, and Ann takes it.
	l2, err := h.svc.NewSolo(ctx, lena, "Lena", set)
	if err != nil {
		t.Fatal(err)
	}
	h.playSolo(lena, l2.ID, 3, true)
	if _, err := h.svc.MakeChallenge(ctx, guest, l2.ID); !is(err, free.ErrSignIn) {
		t.Fatalf("a guest makes it: %v", err)
	}
	if _, err := h.svc.MakeChallenge(ctx, free.Who{Player: newUUID(), Account: true}, l2.ID); !is(err, free.ErrNotMember) {
		t.Fatalf("another account makes it: %v", err)
	}
	code2, err := h.svc.MakeChallenge(ctx, lena, l2.ID)
	if err != nil {
		t.Fatal(err)
	}
	av, err := h.svc.TakeChallenge(ctx, ann, "Ann", code2, "en")
	if err != nil {
		t.Fatal(err)
	}
	if r, err := h.svc.StartSolo(ctx, ann, av.ID, 1); err != nil || r.Repeat {
		t.Fatalf("ann's first place: %+v %v", r, err)
	}
	cv2, err := h.svc.ChallengeOf(ctx, ann, code2)
	if err != nil || cv2.By != "Lena" || len(cv2.Lines) != 2 || !cv2.Lines[1].Me || cv2.Lines[1].Status != free.StatusPlaying {
		t.Fatalf("ann in lena's challenge: %+v %v", cv2, err)
	}
	home, err := h.svc.SoloHome(ctx, lena)
	if err != nil || len(home.Challenges) != 2 || home.Challenges[0].Code != code2 || !home.Challenges[0].Maker {
		t.Fatalf("lena's challenges: %+v %v", home.Challenges, err)
	}
	// Lena knows the places of her own challenge: had in a free game.
	known, err := h.store.FreeKnown(ctx, lena, h.placeOf(l2.ID, 1))
	if err != nil || !known {
		t.Fatalf("lena knows her place: %v %v", known, err)
	}
	if known, err := h.store.FreeKnown(ctx, h.guestOf(), h.placeOf(l2.ID, 1)); err != nil || known {
		t.Fatalf("a new guest knows it: %v %v", known, err)
	}
}

func (h *harness) placeOf(game int64, n int) int64 {
	h.t.Helper()
	var id int64
	if err := h.pool.QueryRow(h.ctx, `SELECT place_id FROM maply.free_round WHERE game = $1 AND n = $2`, game, n).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}
