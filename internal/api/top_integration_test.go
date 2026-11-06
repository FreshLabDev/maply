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

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

// freeGame writes one player's finished free game and its result: who is a
// Telegram id or a site player. A party gets a second seat nobody holds a
// result for.
func (hs *harness) freeGame(who any, kind, mode string, seconds, rounds, score int, at time.Time) {
	hs.t.Helper()
	ctx := context.Background()
	null := func(n int) any {
		if n == 0 {
			return nil
		}
		return n
	}
	var user, player any
	switch w := who.(type) {
	case int:
		user = int64(w)
	case int64:
		user = w
	case string:
		player = w
	}
	var code, started, place, players, beaten, streak any
	if kind == "party" {
		var n int
		if err := hs.pool.QueryRow(ctx, `SELECT count(*) FROM maply.free_game`).Scan(&n); err != nil {
			hs.t.Fatal(err)
		}
		code, started, place, players, beaten = fmt.Sprintf("PARTY%03d", n), at.Add(-time.Minute), 1, 2, 1
	}
	points, countries := score, 3
	switch mode {
	case "streak":
		streak, points = score, score*4000
	case "timed":
		countries, points = score, score*3000
	}
	var id int64
	if err := hs.pool.QueryRow(ctx, `
		INSERT INTO maply.free_game (code, kind, mode, seconds, rounds, status, created_at, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, 'done', $6, $7, $8) RETURNING id`,
		code, kind, mode, null(seconds), null(rounds), at.Add(-2*time.Minute), started, at).Scan(&id); err != nil {
		hs.t.Fatal(err)
	}
	if _, err := hs.pool.Exec(ctx, `
		INSERT INTO maply.free_member (game, m, user_id, player, name, joined_at, last_seen_at) VALUES ($1, 1, $2, $3, 'x', $4, $4)`,
		id, user, player, at.Add(-2*time.Minute)); err != nil {
		hs.t.Fatal(err)
	}
	if _, err := hs.pool.Exec(ctx, `
		INSERT INTO maply.free_result (game, m, user_id, player, kind, mode, seconds, rounds, points, points_base, km, km_rounds,
		                               time_ms, played, answered, countries, from_round, place, players, beaten, streak, finished_at)
		VALUES ($1, 1, $2, $3, $4, $5, $6, $7, $8, $8, 100, '{100}', 60000, 5, 5, $9, 1, $10, $11, $12, $13, $14)`,
		id, user, player, kind, mode, null(seconds), null(rounds), points, countries, place, players, beaten, streak, at); err != nil {
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
	Guest     bool   `json:"guest"`
	Rows      []struct {
		Rank  int    `json:"rank"`
		Name  string `json:"name"`
		Score int    `json:"score"`
		Who   string `json:"who"`
		Me    bool   `json:"me"`
		Anon  bool   `json:"anon"`
		Photo string `json:"photo"`
	} `json:"rows"`
}

// line is a board as "rank name score" lines, "*" after a handle, "?" on an
// anonymous row and "<" on the viewer's own.
func (b topBody) line() string {
	var out []string
	for _, r := range b.Rows {
		s := fmt.Sprintf("%d %s %d", r.Rank, r.Name, r.Score)
		if r.Who != "" {
			s += "*"
		}
		if r.Anon {
			s += "?"
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
// table, the last week or all time. A hidden player is "Anonymous" to
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
	// Without a timer a game is on no board, and an attempt at a challenge
	// is on none either.
	hs.freeGame(1, "solo", "classic", 0, 5, 24000, now.Add(-day))
	hs.freeGame(2, "challenge", "classic", 30, 5, 20000, now.Add(-day))
	hs.freeGame(1, "solo", "timed", 180, 0, 12, now.Add(-day))
	hs.freeGame(2, "solo", "timed", 180, 0, 14, now.Add(-day))

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
	// Lena hid herself: outside her groups she is "Anonymous" with no
	// handle; Dmitry shares one with her; she sees herself.
	check(get(oksana, "board=streak&seconds=60"), 1, "1 Anonymous 9?")
	check(get(dmitry, "board=streak&seconds=60"), 1, "1 Lena 9*")
	check(get(lena, "board=streak&seconds=60"), 1, "1 Lena 9*<")
	// A blitz is a classic at 30 s a place, each length its own table; a
	// party counts as a solo game does.
	blitz := get(oksana, "board=blitz")
	check(blitz, 3, "1 Anonymous 13000?, 2 Ivan 12000*, 3 Oksana 11000*<")
	if blitz.Seconds != 30 || blitz.Rounds != 5 || fmt.Sprint(blitz.RoundsOf) != "[5 3 10]" {
		t.Fatalf("the blitz's settings = %+v", blitz)
	}
	check(get(oksana, "board=blitz&rounds=3"), 1, "1 Oksana 9000*<")
	check(get(oksana, "board=blitz&period=week"), 2, "1 Ivan 12000*, 2 Oksana 11000*<")
	check(get(oksana, "board=classic"), 0, "")
	check(get(oksana, "board=timed"), 2, "1 Ivan 14*, 2 Oksana 12*<")

	// A guest on the site sees the names, and Lena as "Anonymous".
	req := httptest.NewRequest("GET", "/api/top?board=blitz&lang=en", nil)
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	if g := decode[topBody](t, rr.Body.String()); rr.Code != http.StatusOK || !g.Guest || g.line() != "1 Anonymous 13000?, 2 Ivan 12000*, 3 Oksana 11000*" {
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
	if got := fmt.Sprint(own.Top); got != "[{streak 30 0 1 3 5} {timed 180 0 2 2 12} {blitz 30 3 1 1 9000} {blitz 30 5 3 3 11000}]" {
		t.Fatalf("her places = %s", got)
	}
}

// photosAsked keeps a photo for everyone, at an address made of their id,
// and remembers whom it was asked for.
type photosAsked struct {
	calls     [][]int64
	refreshed [][]int64
	fetched   [][]int64
}

func faceKey(id int64) string { return fmt.Sprintf("%032x", id) }

func (p *photosAsked) Faces(_ context.Context, ids []int64) map[int64]string {
	p.calls = append(p.calls, append([]int64(nil), ids...))
	out := map[int64]string{}
	for _, id := range ids {
		out[id] = faceKey(id)
	}
	return out
}

func (p *photosAsked) Face(_ context.Context, key string) ([]byte, bool) {
	var id int64
	if _, err := fmt.Sscanf(key, "%x", &id); err != nil || faceKey(id) != key {
		return nil, false
	}
	return []byte{0xff, 0xd8, byte(id)}, true
}

func (p *photosAsked) Fetch(ctx context.Context, ids []int64) map[int64][]byte {
	p.fetched = append(p.fetched, append([]int64(nil), ids...))
	out := map[int64][]byte{}
	for _, id := range ids {
		out[id] = []byte{0xff, 0xd8, byte(id)}
	}
	return out
}

func (p *photosAsked) Refresh(ids []int64) {
	p.refreshed = append(p.refreshed, append([]int64(nil), ids...))
}

// A board's rows have the Telegram photos of everyone named on it, to
// anyone, in one ask, and never on an anonymous row. The same ids go to
// Telegram in the background.
func TestFreeLeaderboardPhotos(t *testing.T) {
	hs := setup(t)
	photos := &photosAsked{}
	hs.srv.Avatars = photos
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	hs.person(3, "Lena", minskGroup)
	hs.person(4, "Taras", minskGroup)
	hs.expect("POST", "/api/profile/settings", initData(3, "Lena", "en", ""), `{"hide_me":true}`, http.StatusOK)
	for i, id := range []int64{3, 2, 1, 4} {
		hs.freeGame(id, "solo", "streak", 30, 0, 9-i, hs.now.Add(-time.Hour))
	}
	ids := map[string]int64{"Oksana": 1, "Ivan": 2, "Lena": 3, "Taras": 4}
	faces := func(b topBody) string {
		var out []string
		for _, r := range b.Rows {
			face := r.Name + " -"
			if r.Photo != "" {
				face = r.Name + " photo"
				if r.Photo != "/face/"+faceKey(ids[r.Name])+".jpg" {
					t.Fatalf("photo of %s = %q", r.Name, r.Photo)
				}
			}
			out = append(out, face)
		}
		return strings.Join(out, ", ")
	}
	get := func(auth string) string {
		t.Helper()
		return faces(decode[topBody](t, hs.expect("GET", "/api/top?board=streak&seconds=30", auth, "", http.StatusOK)))
	}

	// From the DM: everyone named has a photo; Lena is "Anonymous".
	if got := get(initData(1, "Oksana", "en", "")); got != "Anonymous -, Ivan photo, Oksana photo, Taras photo" {
		t.Fatalf("from the DM: %s", got)
	}
	if fmt.Sprint(photos.calls) != "[[2 1 4]]" || fmt.Sprint(photos.refreshed) != "[[2 1 4]]" {
		t.Fatalf("photos asked: %v, refreshed: %v", photos.calls, photos.refreshed)
	}
	// From the Kyiv group the same.
	if got := get(initData(1, "Oksana", "en", hs.payload(kyivGroup))); got != "Anonymous -, Ivan photo, Oksana photo, Taras photo" {
		t.Fatalf("from the group: %s", got)
	}
	// A guest on the site too.
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/top?board=streak&seconds=30&lang=en", nil))
	if got := faces(decode[topBody](t, rr.Body.String())); got != "Anonymous -, Ivan photo, Oksana photo, Taras photo" {
		t.Fatalf("a guest: %s", got)
	}
	// Seen in the Kyiv group, Lena is named there and has her photo; to
	// someone who shares no group with her she is still "Anonymous", and
	// her photo is not even read.
	hs.person(3, "Lena", kyivGroup)
	if got := get(initData(1, "Oksana", "en", hs.payload(kyivGroup))); got != "Lena photo, Ivan photo, Oksana photo, Taras photo" {
		t.Fatalf("Lena in the group: %s", got)
	}
	hs.person(7, "Marta")
	photos.calls = nil
	if got := get(initData(7, "Marta", "en", "")); got != "Anonymous -, Ivan photo, Oksana photo, Taras photo" {
		t.Fatalf("Marta from the DM: %s", got)
	}
	if fmt.Sprint(photos.calls) != "[[2 1 4]]" {
		t.Fatalf("photos read for Marta: %v", photos.calls)
	}
}

// The app's own face is the person's Telegram photo, asked of Telegram on
// the spot; a guest has none and nothing is asked.
func TestOwnFace(t *testing.T) {
	hs := setup(t)
	photos := &photosAsked{}
	hs.srv.Avatars = photos
	hs.person(1, "Oksana", kyivGroup)
	face := decode[struct {
		Photo string `json:"photo"`
	}](t, hs.expect("GET", "/api/face", initData(1, "Oksana", "en", ""), "", http.StatusOK))
	if face.Photo != "/face/"+faceKey(1)+".jpg" || fmt.Sprint(photos.fetched) != "[[1]]" {
		t.Fatalf("own face %q, fetched %v", face.Photo, photos.fetched)
	}
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/face", nil))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "photo") || len(photos.fetched) != 1 {
		t.Fatalf("a guest's face: %d %s, fetched %v", rr.Code, rr.Body.String(), photos.fetched)
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

// An account without Telegram is on the boards as itself: every site player
// it holds in one row, under its name, and its profile has its places. An
// account with Telegram is its person, its site players' games before the
// link too. A site player no account holds is on no board.
func TestFreeLeaderboardsOfAccounts(t *testing.T) {
	hs := setup(t)
	const (
		home  = "11111111-1111-4111-8111-111111111111"
		work  = "22222222-2222-4222-8222-222222222222"
		vanya = "33333333-3333-4333-8333-333333333333"
		stray = "44444444-4444-4444-8444-444444444444"
	)
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	ctx := context.Background()
	for id, name := range map[string]string{home: "Marta", work: "Work", vanya: "Vanya", stray: "Stray"} {
		if _, err := hs.pool.Exec(ctx, `INSERT INTO maply.site_player (id, name) VALUES ($1, $2)`, id, name); err != nil {
			t.Fatal(err)
		}
	}
	marta, acc := hs.signInGoogle("g-marta", home)
	store := hs.srv.Store.(*db.Store)
	if _, err := store.AdoptGuest(ctx, acc, work, db.Rules{Open: func(round.Date) bool { return false }}); err != nil {
		t.Fatal(err)
	}
	ivan, _, err := store.SignIn(ctx, db.Identity{Kind: db.KindGoogle, GoogleSub: "g-ivan"}, vanya, db.Rules{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hs.pool.Exec(ctx, `UPDATE maply.account SET telegram_user_id = 2 WHERE id = $1`, ivan.ID); err != nil {
		t.Fatal(err)
	}

	day := 24 * time.Hour
	hs.freeGame(home, "solo", "streak", 30, 0, 6, hs.now.Add(-3*day))
	hs.freeGame(work, "solo", "streak", 30, 0, 7, hs.now.Add(-2*day))
	hs.freeGame(vanya, "solo", "streak", 30, 0, 8, hs.now.Add(-day))
	hs.freeGame(2, "solo", "streak", 30, 0, 3, hs.now.Add(-day))
	hs.freeGame(1, "solo", "streak", 30, 0, 5, hs.now.Add(-day))
	hs.freeGame(stray, "solo", "streak", 30, 0, 20, hs.now.Add(-day))

	oksana := initData(1, "Oksana", "en", "")
	b := decode[topBody](t, hs.expect("GET", "/api/top?board=streak", oksana, "", http.StatusOK))
	if b.Total != 3 || b.line() != "1 Ivan 8*, 2 Marta 7, 3 Oksana 5*<" {
		t.Fatalf("the streak seen in Telegram: %d [%s]", b.Total, b.line())
	}
	b = decode[topBody](t, hs.siteExpect("GET", "/api/top?board=streak&lang=en", marta, "", http.StatusOK))
	if b.Guest || b.line() != "1 Ivan 8*, 2 Marta 7<, 3 Oksana 5*" {
		t.Fatalf("the streak seen by Marta's account: %+v [%s]", b, b.line())
	}

	type place struct {
		Board string `json:"board"`
		Rank  int    `json:"rank"`
		Of    int    `json:"of"`
		Score int    `json:"score"`
	}
	// The profile's free play counts the same games: Marta's two site
	// players', Ivan's own and his site player's.
	type places struct {
		Top  []place `json:"top"`
		Free *struct {
			Games int `json:"games"`
		} `json:"free"`
	}
	if got := decode[places](t, hs.siteExpect("GET", "/api/profile?lang=en", marta, "", http.StatusOK)); fmt.Sprint(got.Top) != "[{streak 2 3 7}]" ||
		got.Free == nil || got.Free.Games != 2 {
		t.Fatalf("Marta's profile: places %+v, free %+v", got.Top, got.Free)
	}
	if got := decode[places](t, hs.expect("GET", "/api/profile", initData(2, "Ivan", "en", ""), "", http.StatusOK)); fmt.Sprint(got.Top) != "[{streak 1 3 8}]" ||
		got.Free == nil || got.Free.Games != 2 {
		t.Fatalf("Ivan's profile: places %+v, free %+v", got.Top, got.Free)
	}
}
