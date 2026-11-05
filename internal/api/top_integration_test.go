// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// freeGame writes one player's finished free game and its result. A party
// gets a second seat nobody holds a result for.
func (hs *harness) freeGame(user int64, kind, mode string, seconds, rounds, score int, at time.Time) {
	hs.t.Helper()
	ctx := context.Background()
	null := func(n int) any {
		if n == 0 {
			return nil
		}
		return n
	}
	var code, started, place, players, beaten, streak any
	if kind != "solo" {
		var n int
		if err := hs.pool.QueryRow(ctx, `SELECT count(*) FROM maply.free_game`).Scan(&n); err != nil {
			hs.t.Fatal(err)
		}
		code, started, place, players, beaten = fmt.Sprintf("PARTY%03d", n), at.Add(-time.Minute), 1, 2, 1
	}
	points := score
	if mode == "streak" {
		streak, points = score, score*4000
	}
	var id int64
	if err := hs.pool.QueryRow(ctx, `
		INSERT INTO maply.free_game (code, kind, mode, seconds, rounds, status, created_at, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, 'done', $6, $7, $8) RETURNING id`,
		code, kind, mode, null(seconds), null(rounds), at.Add(-2*time.Minute), started, at).Scan(&id); err != nil {
		hs.t.Fatal(err)
	}
	if _, err := hs.pool.Exec(ctx, `
		INSERT INTO maply.free_member (game, m, user_id, name, joined_at, last_seen_at) VALUES ($1, 1, $2, 'x', $3, $3)`,
		id, user, at.Add(-2*time.Minute)); err != nil {
		hs.t.Fatal(err)
	}
	if _, err := hs.pool.Exec(ctx, `
		INSERT INTO maply.free_result (game, m, user_id, kind, mode, seconds, rounds, points, points_base, km, km_rounds,
		                               time_ms, played, answered, countries, from_round, place, players, beaten, streak, finished_at)
		VALUES ($1, 1, $2, $3, $4, $5, $6, $7, $7, 100, '{100}', 60000, 5, 5, 3, 1, $8, $9, $10, $11, $12)`,
		id, user, kind, mode, null(seconds), null(rounds), points, place, players, beaten, streak, at); err != nil {
		hs.t.Fatal(err)
	}
}

type topBody struct {
	Board     string `json:"board"`
	Seconds   int    `json:"seconds"`
	Rounds    int    `json:"rounds"`
	Period    string `json:"period"`
	SecondsOf []int  `json:"seconds_of"`
	RoundsOf  []int  `json:"rounds_of"`
	Total     int    `json:"total"`
	Rows      []struct {
		Rank  int    `json:"rank"`
		Name  string `json:"name"`
		Score int    `json:"score"`
		Who   string `json:"who"`
		Me    bool   `json:"me"`
	} `json:"rows"`
}

// line is a board as "rank name score" lines, "*" after a handle and "<"
// on the viewer's own.
func (b topBody) line() string {
	var out []string
	for _, r := range b.Rows {
		s := fmt.Sprintf("%d %s %d", r.Rank, r.Name, r.Score)
		if r.Who != "" {
			s += "*"
		}
		if r.Me {
			s += "<"
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

// The free play's boards: one row a player with their best, equal bests by
// who reached theirs first, only games on a timer, each setting its own
// table, the last week or all time. A hidden player is a Telegram player to
// anyone outside their groups, and the profile has its places.
func TestFreeLeaderboards(t *testing.T) {
	hs := setup(t)
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	hs.person(3, "Lena", minskGroup)
	hs.person(5, "Dmitry", minskGroup)
	oksana, lena, dmitry := initData(1, "Oksana", "en", ""), initData(3, "Lena", "en", ""), initData(5, "Dmitry", "en", "")
	hs.expect("POST", "/api/profile/settings", lena, `{"hide_me":true}`, http.StatusOK)
	day := 24 * time.Hour
	now := hs.now

	hs.freeGame(1, "solo", "streak", 30, 0, 5, now.Add(-10*day))
	hs.freeGame(1, "solo", "streak", 30, 0, 3, now.Add(-day))
	hs.freeGame(2, "solo", "streak", 30, 0, 5, now.Add(-2*day))
	hs.freeGame(5, "solo", "streak", 30, 0, 4, now.Add(-3*time.Hour))
	hs.freeGame(3, "solo", "streak", 60, 0, 9, now.Add(-time.Hour))
	hs.freeGame(2, "party", "classic", 30, 5, 12000, now.Add(-day))
	hs.freeGame(1, "solo", "classic", 30, 5, 11000, now.Add(-day))
	hs.freeGame(3, "solo", "classic", 30, 5, 13000, now.Add(-20*day))
	hs.freeGame(1, "solo", "classic", 30, 3, 9000, now.Add(-day))
	// Without a timer a game is on no board.
	hs.freeGame(1, "solo", "classic", 0, 5, 24000, now.Add(-day))

	get := func(auth, query string) topBody {
		t.Helper()
		return decode[topBody](t, hs.expect("GET", "/api/top?"+query, auth, "", http.StatusOK))
	}
	check := func(b topBody, total int, want string) {
		t.Helper()
		if b.Total != total || b.line() != want {
			t.Fatalf("%s %d/%d %s: %d [%s], want %d [%s]", b.Board, b.Seconds, b.Rounds, b.Period, b.Total, b.line(), total, want)
		}
	}

	// A streak of 5 twice: Oksana had hers first. Her later 3 is not a row.
	streak := get(oksana, "board=streak&seconds=30")
	check(streak, 3, "1 Oksana 5*<, 2 Ivan 5*, 3 Dmitry 4*")
	if streak.Period != "all" || fmt.Sprint(streak.SecondsOf) != "[30 60]" || streak.RoundsOf != nil {
		t.Fatalf("the streak's settings = %+v", streak)
	}
	check(get(oksana, "board=streak&period=week"), 3, "1 Ivan 5*, 2 Dmitry 4*, 3 Oksana 3*<")
	// Lena hid herself: outside her groups she is a Telegram player with no
	// handle; Dmitry shares one with her; she sees herself.
	check(get(oksana, "board=streak&seconds=60"), 1, "1 Telegram player 9")
	check(get(dmitry, "board=streak&seconds=60"), 1, "1 Lena 9*")
	check(get(lena, "board=streak&seconds=60"), 1, "1 Lena 9*<")
	// A blitz is a classic at 30 s a place, each length its own table; a
	// party counts as a solo game does.
	blitz := get(oksana, "board=blitz")
	check(blitz, 3, "1 Telegram player 13000, 2 Ivan 12000*, 3 Oksana 11000*<")
	if blitz.Seconds != 30 || blitz.Rounds != 5 || fmt.Sprint(blitz.RoundsOf) != "[5 3 10]" {
		t.Fatalf("the blitz's settings = %+v", blitz)
	}
	check(get(oksana, "board=blitz&rounds=3"), 1, "1 Oksana 9000*<")
	check(get(oksana, "board=blitz&period=week"), 2, "1 Ivan 12000*, 2 Oksana 11000*<")
	check(get(oksana, "board=classic"), 0, "")

	// A guest on the site sees the names, and Lena as a Telegram player.
	req := httptest.NewRequest("GET", "/api/top?board=blitz&lang=en", nil)
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	if g := decode[topBody](t, rr.Body.String()); rr.Code != http.StatusOK || g.line() != "1 Telegram player 13000, 2 Ivan 12000*, 3 Oksana 11000*" {
		t.Fatalf("a guest sees %d %s", rr.Code, rr.Body.String())
	}

	// A name opens the mini profile; the handle is the board's own.
	ivan := blitz.Rows[1].Who
	if mini := decode[miniView](t, hs.expect("GET", "/api/profile/who?id="+ivan, oksana, "", http.StatusOK)); mini.Name != "Ivan" {
		t.Fatalf("Ivan's mini profile = %+v", mini)
	}
	if ivan == streak.Rows[1].Who {
		t.Fatal("one handle for Ivan on two boards")
	}

	for _, bad := range []string{"board=nope", "board=classic&seconds=30", "board=blitz&rounds=7", "board=streak&rounds=5",
		"board=timed&seconds=60", "board=streak&period=month"} {
		hs.expect("GET", "/api/top?"+bad, oksana, "", http.StatusBadRequest)
	}

	// The profile has her place on each board she is on, all time.
	type place struct {
		Board   string `json:"board"`
		Seconds int    `json:"seconds"`
		Rounds  int    `json:"rounds"`
		Rank    int    `json:"rank"`
		Of      int    `json:"of"`
		Score   int    `json:"score"`
	}
	own := decode[struct {
		Top []place `json:"top"`
	}](t, hs.expect("GET", "/api/profile", oksana, "", http.StatusOK))
	if got := fmt.Sprint(own.Top); got != "[{streak 30 0 1 3 5} {blitz 30 3 1 1 9000} {blitz 30 5 3 3 11000}]" {
		t.Fatalf("her places = %s", got)
	}
}

// A board shows its first hundred and the viewer's own row wherever it is.
func TestFreeLeaderboardKeepsTheViewersRow(t *testing.T) {
	hs := setup(t)
	hs.person(1, "Oksana")
	for i := range 100 {
		hs.freeGame(int64(1000+i), "solo", "streak", 60, 0, 10, hs.now.Add(-time.Duration(i+1)*time.Minute))
	}
	hs.freeGame(1, "solo", "streak", 60, 0, 9, hs.now.Add(-time.Minute))
	b := decode[topBody](t, hs.expect("GET", "/api/top?board=streak&seconds=60", initData(1, "Oksana", "uk", ""), "", http.StatusOK))
	last := b.Rows[len(b.Rows)-1]
	if b.Total != 101 || len(b.Rows) != 101 || last.Rank != 101 || !last.Me || last.Name != "Oksana" || b.Rows[99].Rank != 100 {
		t.Fatalf("total %d, %d rows, last %+v", b.Total, len(b.Rows), last)
	}
	// Equal streaks rank by who had theirs first: the oldest is first.
	if b.Rows[0].Rank != 1 || b.Rows[0].Score != 10 {
		t.Fatalf("first %+v", b.Rows[0])
	}
}
