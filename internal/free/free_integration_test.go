// SPDX-License-Identifier: Apache-2.0
//go:build integration

// The free game against the real store, with the clock moved by hand:
//
//	MAPLY_TEST_DATABASE_URL=postgres://… go test -tags=integration ./internal/free/
package free_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/migrations"
)

// spot is a place of the test pool, at a capital, so the judge finds its
// country.
type spot struct {
	iso, continent string
	lat, lon       float64
}

var spots = []spot{
	{"FR", "europe", 48.8566, 2.3522},
	{"DE", "europe", 52.52, 13.405},
	{"ES", "europe", 40.4168, -3.7038},
	{"IT", "europe", 41.9028, 12.4964},
	{"PL", "europe", 52.2297, 21.0122},
	{"JP", "asia", 35.6762, 139.6503},
	{"PE", "south_america", -12.0464, -77.0428},
	{"KE", "africa", -1.2921, 36.8219},
	{"CA", "north_america", 45.4215, -75.6972},
	{"AU", "oceania", -35.2809, 149.13},
	{"FR", "europe", 43.2965, 5.3698},
	{"DE", "europe", 48.1351, 11.582},
}

var countries = sync.OnceValues(geo.World)

type harness struct {
	t     *testing.T
	ctx   context.Context
	store *db.Store
	pool  *pgxpool.Pool
	svc   *free.Service
	mu    sync.Mutex
	now   time.Time
	// places maps a place id to its spot.
	places map[int64]spot
	draws  int
}

func (h *harness) clock() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.now
}

func (h *harness) tick(d time.Duration) {
	h.mu.Lock()
	h.now = h.now.Add(d)
	h.mu.Unlock()
}

func setup(t *testing.T) *harness {
	t.Helper()
	rawURL := os.Getenv("MAPLY_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set MAPLY_TEST_DATABASE_URL to run the free game tests")
	}
	if u, err := url.Parse(rawURL); err != nil || !strings.Contains(strings.TrimPrefix(u.Path, "/"), "test") {
		t.Fatalf("MAPLY_TEST_DATABASE_URL must name a disposable database containing \"test\"")
	}
	ctx := context.Background()
	store, err := db.Connect(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	pool, err := pgxpool.New(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	bootstrap, err := os.ReadFile(filepath.Join("..", "..", "deploy", "core-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(bootstrap)); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE maply.free_game, maply.guess, maply.site_guess, maply.site_player, maply.round, maply.place, maply.day_closed, core.person CASCADE`); err != nil {
		t.Fatal(err)
	}
	c, err := countries()
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, ctx: ctx, store: store, pool: pool, now: time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC), places: map[int64]spot{}}
	for i, s := range spots {
		h.addPlace(fmt.Sprintf("free%02d", i), "free", s)
	}
	h.svc = h.service(c)
	return h
}

func (h *harness) service(c *geo.Countries) *free.Service {
	return &free.Service{Store: h.store, Countries: c, ImageKey: []byte("test"), Now: h.clock,
		Intn: func(n int) int { h.draws++; return h.draws % n }}
}

func (h *harness) addPlace(src, pool string, s spot) int64 {
	h.t.Helper()
	var id int64
	key := fmt.Sprintf("K%sxxxxxxxxxxxxxxx", src)
	status := "approved"
	if pool == "used" {
		pool, status = "daily", "used"
	}
	err := h.pool.QueryRow(h.ctx, `
		INSERT INTO maply.place (status, pool, source, source_id, license, kind, lat, lon, image_key,
		                         country_iso, continent, city, difficulty, reviewed_at)
		VALUES ($1, $2, 'mapillary', $3, 'CC-BY-SA-4.0', 'panorama', $4, $5, $6, $7, $8, '{"en":"Somewhere"}', 3, now())
		RETURNING id`, status, pool, src, s.lat, s.lon, key, s.iso, s.continent).Scan(&id)
	if err != nil {
		h.t.Fatal(err)
	}
	h.places[id] = s
	return id
}

var (
	ann  = free.Who{UserID: 101}
	bob  = free.Who{UserID: 102}
	cleo = free.Who{UserID: 103}
)

// spotOf is where place n of a solo game is, read the way a cheat could not:
// from the database.
func (h *harness) spotOf(game int64, n int) spot {
	h.t.Helper()
	var id int64
	if err := h.pool.QueryRow(h.ctx, `SELECT place_id FROM maply.free_round WHERE game = $1 AND n = $2`, game, n).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return h.places[id]
}

func at(s spot) *geo.Point { return &geo.Point{Lat: s.lat, Lon: s.lon} }

// noPlace fails when a response says where a place of the pool is.
func noPlace(t *testing.T, what string, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	body := string(b)
	for _, s := range spots {
		for _, needle := range []string{fmt.Sprint(s.lat), fmt.Sprint(s.lon), `"` + s.iso + `"`} {
			if strings.Contains(body, needle) {
				t.Fatalf("%s says where the place is (%s): %s", what, needle, body)
			}
		}
	}
	for _, needle := range []string{"Somewhere", "mapillary", "Kfree", `"lat"`, `"country"`} {
		if strings.Contains(body, needle) {
			t.Fatalf("%s leaks %q: %s", what, needle, body)
		}
	}
}

func is(err, want error) bool { return errors.Is(err, want) }

// flip changes one character of a token.
func flip(s string, i int) string {
	c := byte('A')
	if s[i] == 'A' {
		c = 'B'
	}
	return s[:i] + string(c) + s[i+1:]
}

func TestSoloTimerAndGrace(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	v, err := h.svc.NewSolo(ctx, ann, "Ann", free.Settings{Mode: free.Classic, Seconds: 30, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if v.Next != 1 || v.Current != nil {
		t.Fatalf("new game: %+v", v)
	}
	noPlace(t, "a new game", v)
	// The second place cannot start before the first.
	if _, err := h.svc.StartSolo(ctx, ann, v.ID, 2); !is(err, free.ErrOrder) {
		t.Fatalf("start 2 first: %v", err)
	}
	r1, err := h.svc.StartSolo(ctx, ann, v.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	noPlace(t, "a started place", r1)
	if r1.EndsAt == nil || r1.EndsAt.Sub(r1.StartsAt) != 30*time.Second {
		t.Fatalf("times: %+v", r1)
	}
	// A reload ten seconds on gives the same picture and the same end.
	h.tick(10 * time.Second)
	again, err := h.svc.StartSolo(ctx, ann, v.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if again.Image != r1.Image || !again.EndsAt.Equal(*r1.EndsAt) || !again.StartsAt.Equal(r1.StartsAt) {
		t.Fatalf("restart moved the place: %+v vs %+v", again, r1)
	}
	if left := again.EndsAt.Sub(again.Now); left != 20*time.Second {
		t.Fatalf("left after the reload: %v", left)
	}
	// The picture is there, and never under the place's own key.
	if strings.Contains(r1.Image, "Kfree") || !strings.HasPrefix(r1.Image, "/api/free/img/") {
		t.Fatalf("image path %q", r1.Image)
	}
	token := strings.TrimSuffix(strings.TrimPrefix(r1.Image, "/api/free/img/"), ".jpg")
	if key, err := h.svc.Image(ctx, token); err != nil || !strings.HasPrefix(key, "Kfree") {
		t.Fatalf("image: %q %v", key, err)
	}
	for _, forged := range []string{flip(token, 3), flip(token, 12), flip(token, len(token)-1), token + "A", token[1:]} {
		if _, err := h.svc.Image(ctx, forged); !is(err, free.ErrNotFound) {
			t.Fatalf("forged image token %q: %v", forged, err)
		}
	}
	// Giving up is not for a timed place.
	if _, err := h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 1, Skip: true}, "en"); !is(err, free.ErrBadRequest) {
		t.Fatalf("give up on a timer: %v", err)
	}
	// One and a half seconds past the end is in the grace: taken, no speed.
	h.tick(21500 * time.Millisecond)
	s1 := h.spotOf(v.ID, 1)
	got, err := h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 1, Pin: at(s1)}, "en")
	if err != nil {
		t.Fatal(err)
	}
	d := got.Done[0]
	if d.Late || d.Guess.Speed != 0 || d.Guess.Distance != 3500 || d.Guess.Bonus != 800 || !d.Guess.Country {
		t.Fatalf("pin in the grace: %+v", d)
	}
	if d.Place.Lat != s1.lat || d.Place.Country != s1.iso || d.Place.City != "Somewhere" {
		t.Fatalf("the answer comes with the answer: %+v", d.Place)
	}
	if d.Guess.TimeMs != 30_000 {
		t.Errorf("time is capped at the place's: %d", d.Guess.TimeMs)
	}
	if _, err := h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 1, Pin: at(s1)}, "en"); !is(err, free.ErrAnswered) {
		t.Fatalf("second answer: %v", err)
	}
	// Place 2, answered at once with the right spot: the whole speed bonus.
	r2, err := h.svc.StartSolo(ctx, ann, v.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	h.tick(time.Second)
	got, err = h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 2, Pin: at(h.spotOf(v.ID, 2))}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if g := got.Done[1].Guess; g.Speed != free.MaxSpeed || g.Points != 3500+800+200 {
		t.Fatalf("quick answer: %+v", g)
	}
	// Place 3: a pin two and a half seconds after the end is a miss.
	r3, err := h.svc.StartSolo(ctx, ann, v.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if r3.Image == r2.Image {
		t.Fatal("each place has its own address")
	}
	h.tick(32500 * time.Millisecond)
	got, err = h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 3, Pin: at(h.spotOf(v.ID, 3))}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if d := got.Done[2]; !d.Late || d.Guess.Points != 0 || d.Guess.Lat != nil {
		t.Fatalf("late pin: %+v", d)
	}
	if got.Status != free.StatusDone || got.Final == nil || got.Final.Points != 4300+4500 || got.Final.Best != nil {
		t.Fatalf("final: %+v %+v", got.Status, got.Final)
	}
	// Three different countries.
	if a, b, c := h.spotOf(v.ID, 1).iso, h.spotOf(v.ID, 2).iso, h.spotOf(v.ID, 3).iso; a == b || b == c || a == c {
		t.Fatalf("one country twice: %s %s %s", a, b, c)
	}
}

func TestSoloWithoutTimerAndRecords(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	set := free.Settings{Mode: free.Classic, Seconds: 0, Rounds: 3}
	play := func(give int) free.SoloView {
		v, err := h.svc.NewSolo(ctx, ann, "Ann", set)
		if err != nil {
			t.Fatal(err)
		}
		var last free.SoloView
		for n := 1; n <= 3; n++ {
			r, err := h.svc.StartSolo(ctx, ann, v.ID, n)
			if err != nil {
				t.Fatal(err)
			}
			if r.EndsAt != nil {
				t.Fatal("no timer has no end")
			}
			h.tick(time.Hour) // no hurry, no bonus
			in := free.GuessIn{N: n, Pin: at(h.spotOf(v.ID, n))}
			if n <= give {
				in = free.GuessIn{N: n, Skip: true}
			}
			if last, err = h.svc.GuessSolo(ctx, ann, v.ID, in, "en"); err != nil {
				t.Fatal(err)
			}
		}
		return last
	}
	first := play(1)
	if first.Final == nil || first.Final.Points != 2*4300 || first.Final.Best != nil || first.Final.Record {
		t.Fatalf("first game: %+v", first.Final)
	}
	if g := first.Done[0].Guess; g.Points != 0 || g.Lat != nil || first.Done[0].Place.Country == "" {
		t.Fatalf("given up: %+v", first.Done[0])
	}
	second := play(0)
	if second.Final.Best == nil || *second.Final.Best != 2*4300 || !second.Final.Record || second.Final.Points != 3*4300 {
		t.Fatalf("second game beats the first: %+v", second.Final)
	}
	// The best of an unfinished game does not count: one started and left.
	v, err := h.svc.NewSolo(ctx, ann, "Ann", set)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartSolo(ctx, ann, v.ID, 1); err != nil {
		t.Fatal(err)
	}
	home, err := h.svc.SoloHome(ctx, ann)
	if err != nil {
		t.Fatal(err)
	}
	if home.Last != set || home.Open == nil || *home.Open != v.ID || home.Best[set.Key()] != 3*4300 {
		t.Fatalf("home: %+v", home)
	}
	// A day later the open game is abandoned and stays out of the records.
	h.tick(25 * time.Hour)
	if err := h.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	home, _ = h.svc.SoloHome(ctx, ann)
	if home.Open != nil {
		t.Fatalf("abandoned game still open: %+v", home)
	}
	var status string
	if err := h.pool.QueryRow(ctx, `SELECT status FROM maply.free_game WHERE id = $1`, v.ID).Scan(&status); err != nil || status != free.StatusAbandoned {
		t.Fatalf("status %q %v", status, err)
	}
	var n int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM maply.free_result WHERE user_id = $1`, ann.UserID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("results: %d %v", n, err)
	}
}

func TestStreakEnds(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	set := free.Settings{Mode: free.Streak, Seconds: 30}
	if _, err := h.svc.NewSolo(ctx, ann, "Ann", free.Settings{Mode: free.Streak, Seconds: 0}); !is(err, free.ErrBadRequest) {
		t.Fatalf("a streak needs a timer: %v", err)
	}
	v, err := h.svc.NewSolo(ctx, ann, "Ann", set)
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
	if v.Streak != 2 || v.Next != 3 {
		t.Fatalf("streak of 2: %+v", v)
	}
	// A pin in the sea ends it.
	if _, err := h.svc.StartSolo(ctx, ann, v.ID, 3); err != nil {
		t.Fatal(err)
	}
	v, err = h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: 3, Pin: &geo.Point{Lat: 0, Lon: -30}}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != free.StatusDone || v.Final == nil || v.Final.Streak != 2 || v.Next != 0 {
		t.Fatalf("sea ends the streak: %+v %+v", v, v.Final)
	}
	if _, err := h.svc.StartSolo(ctx, ann, v.ID, 4); !is(err, free.ErrOver) {
		t.Fatalf("no place after the end: %v", err)
	}
	// A second streak ends when the time runs out, without a request: the
	// next read finds it over.
	v, err = h.svc.NewSolo(ctx, ann, "Ann", set)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartSolo(ctx, ann, v.ID, 1); err != nil {
		t.Fatal(err)
	}
	h.tick(33 * time.Second)
	if v, err = h.svc.SoloGame(ctx, ann, v.ID, "en"); err != nil {
		t.Fatal(err)
	}
	if v.Status != free.StatusDone || v.Final.Streak != 0 || len(v.Done) != 1 || v.Done[0].Place.Country == "" {
		t.Fatalf("time out ends the streak: %+v", v)
	}
	var best int
	if err := h.pool.QueryRow(ctx, `SELECT max(streak) FROM maply.free_result WHERE user_id = $1 AND mode = 'streak' AND seconds = 30`, ann.UserID).Scan(&best); err != nil || best != 2 {
		t.Fatalf("streak record %d %v", best, err)
	}
}

func TestPlacesComeFromThePoolAndUnplayedDays(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	// A future day's place stays in the queue; a closed day Ann played is
	// not for her, one she did not play is.
	future := h.addPlace("future", "daily", spot{"NO", "europe", 59.9139, 10.7522})
	played := h.addPlace("played", "used", spot{"SE", "europe", 59.3293, 18.0686})
	unplayed := h.addPlace("unplayed", "used", spot{"FI", "europe", 60.1699, 24.9384})
	// A closed day nobody played whose picture is a flat photo: maply shows
	// panoramas only, so neither free play nor the archive has it.
	photo := h.addPlace("photo", "used", spot{"EE", "europe", 59.4370, 24.7536})
	if _, err := h.pool.Exec(ctx, `UPDATE maply.place SET kind = 'photo' WHERE id = $1`, photo); err != nil {
		t.Fatal(err)
	}
	for i, id := range []int64{played, unplayed, photo} {
		d := round.NewDate(2026, time.October, 20+i)
		if _, err := h.pool.Exec(ctx, `INSERT INTO maply.round (date, day_no, place_id) VALUES ($1, $2, $3)`, d.Time(), i+1, id); err != nil {
			t.Fatal(err)
		}
		if _, err := h.pool.Exec(ctx, `INSERT INTO maply.day_closed (date) VALUES ($1)`, d.Time()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'Ann')`, ann.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
		VALUES ('2026-10-20', $1, 'dm', 0, 0, '2026-10-20 10:00Z', '2026-10-20 18:00Z')`, ann.UserID); err != nil {
		t.Fatal(err)
	}
	// Ann plays every place she can get: 12 of the pool and the day she did
	// not play, each once, before any comes again.
	seen := map[int64]bool{}
	set := free.Settings{Mode: free.Classic, Seconds: 0, Rounds: 10}
	for g := 0; g < 2; g++ {
		v, err := h.svc.NewSolo(ctx, ann, "Ann", set)
		if err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 10; n++ {
			r, err := h.svc.StartSolo(ctx, ann, v.ID, n)
			if err != nil {
				t.Fatal(err)
			}
			var id int64
			if err := h.pool.QueryRow(ctx, `SELECT place_id FROM maply.free_round WHERE game = $1 AND n = $2`, v.ID, n).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if id == future || id == played || id == photo {
				t.Fatalf("place %d must never come", id)
			}
			if wantRepeat := len(seen) >= 13; r.Repeat != wantRepeat || seen[id] != wantRepeat {
				t.Fatalf("game %d place %d: repeat %v after %d seen", g, n, r.Repeat, len(seen))
			}
			seen[id] = true
			h.tick(time.Second)
			if _, err := h.svc.GuessSolo(ctx, ann, v.ID, free.GuessIn{N: n, Skip: true}, "en"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !seen[unplayed] {
		t.Fatal("the closed day Ann did not play is in the game")
	}
	days, err := h.svc.ArchiveOf(ctx, ann)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].N != 2 || days[1].N != 1 {
		t.Fatalf("the archive has the panorama days only: %+v", days)
	}
	if _, err := h.svc.NewArchive(ctx, ann, "Ann", 3, "en"); !errors.Is(err, free.ErrNotFound) {
		t.Fatalf("the photo's day taken: %v", err)
	}
	// The daily queue never gives a free place.
	d := round.NewDate(2026, time.November, 6)
	rd, err := h.store.EnsureRound(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if rd.PlaceID != future {
		t.Fatalf("the day got place %d, want the daily one %d", rd.PlaceID, future)
	}
	if _, err := h.store.EnsureRound(ctx, d.AddDays(1)); !errors.Is(err, db.ErrNoPlace) {
		t.Fatalf("with the daily queue empty the free pool does not fill in: %v", err)
	}
	// A free place cannot be moved to the days, nor become a round.
	for id := range h.places {
		if h.places[id].iso == "FR" {
			if _, err := h.pool.Exec(ctx, `UPDATE maply.place SET pool = 'daily' WHERE id = $1`, id); err == nil {
				t.Fatal("a free place became a day's place")
			}
			if _, err := h.pool.Exec(ctx, `INSERT INTO maply.round (date, day_no, place_id) VALUES ('2026-12-01', 99, $1)`, id); err == nil {
				t.Fatal("a round on a free place")
			}
			break
		}
	}
}

func TestOneSoloAtATime(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	a, _ := h.svc.NewSolo(ctx, ann, "Ann", free.DefaultSolo)
	if _, err := h.svc.StartSolo(ctx, ann, a.ID, 1); err != nil {
		t.Fatal(err)
	}
	b, _ := h.svc.NewSolo(ctx, ann, "Ann", free.DefaultSolo)
	if _, err := h.svc.StartSolo(ctx, ann, a.ID, 2); !is(err, free.ErrOver) && !is(err, free.ErrOrder) {
		t.Fatalf("the old game goes on: %v", err)
	}
	// The place seen in the abandoned game is not drawn again.
	first := h.spotOf(a.ID, 1)
	if _, err := h.svc.StartSolo(ctx, ann, b.ID, 1); err != nil {
		t.Fatal(err)
	}
	var same bool
	if err := h.pool.QueryRow(ctx, `SELECT (SELECT place_id FROM maply.free_round WHERE game = $1 AND n = 1) = (SELECT place_id FROM maply.free_round WHERE game = $2 AND n = 1)`, a.ID, b.ID).Scan(&same); err != nil || same {
		t.Fatalf("same place again %v %v (%s)", same, err, first.iso)
	}
	// Someone else cannot read or play it.
	if _, err := h.svc.SoloGame(ctx, bob, b.ID, "en"); !is(err, free.ErrNotMember) {
		t.Fatalf("other player: %v", err)
	}
	// A site guest plays alone, and gets no record.
	guest := free.Who{Player: "6f1c1f43-3c39-4b8c-9a5c-2f6b1d0b7e11"}
	if _, err := h.pool.Exec(ctx, `INSERT INTO maply.site_player (id) VALUES ($1)`, guest.Player); err != nil {
		t.Fatal(err)
	}
	g, err := h.svc.NewSolo(ctx, guest, "", free.Settings{Mode: free.Classic, Seconds: 0, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 3; n++ {
		if _, err := h.svc.StartSolo(ctx, guest, g.ID, n); err != nil {
			t.Fatal(err)
		}
		if g, err = h.svc.GuessSolo(ctx, guest, g.ID, free.GuessIn{N: n, Skip: true}, "en"); err != nil {
			t.Fatal(err)
		}
	}
	if g.Status != free.StatusDone || g.Final == nil || g.Final.Best != nil || g.Final.Record {
		t.Fatalf("guest final: %+v", g.Final)
	}
	if _, err := h.svc.NewParty(ctx, guest, "", "en", free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 5}); !is(err, free.ErrSignIn) {
		t.Fatalf("a guest makes a party: %v", err)
	}
	var n int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM maply.free_result WHERE game = $1`, g.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("guest results: %d %v", n, err)
	}
}

// view is a poll's full answer; it fails on a short one.
func (h *harness) view(svc *free.Service, w free.Who, code string, since int64) free.PartyView {
	h.t.Helper()
	got, err := svc.Poll(h.ctx, w, code, since, "en")
	if err != nil {
		h.t.Fatal(err)
	}
	v, ok := got.(free.PartyView)
	if !ok {
		h.t.Fatalf("poll since %d: %+v", since, got)
	}
	return v
}

// wait moves the clock two seconds at a time to until, with everyone in
// who polling as the app does.
func (h *harness) wait(until time.Time, who ...free.Who) {
	h.t.Helper()
	for h.clock().Before(until) {
		h.tick(2 * time.Second)
		for _, w := range who {
			code := ""
			if err := h.pool.QueryRow(h.ctx, `SELECT g.code FROM maply.free_game g JOIN maply.free_member m ON m.game = g.id WHERE m.user_id = $1 AND g.status IN ('lobby', 'playing') ORDER BY g.id DESC LIMIT 1`, w.UserID).Scan(&code); err != nil {
				h.t.Fatal(err)
			}
			h.view(h.svc, w, code, 0)
		}
	}
}

// partySpot is where place n of a party is.
func (h *harness) partySpot(code string, n int) spot {
	h.t.Helper()
	var id int64
	if err := h.pool.QueryRow(h.ctx, `SELECT r.place_id FROM maply.free_round r JOIN maply.free_game g ON g.id = r.game WHERE g.code = $1 AND r.n = $2`, code, n).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return h.places[id]
}

func TestPartyFromLobbyToRematch(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	if _, err := h.svc.NewParty(ctx, ann, "Ann", "en", free.Settings{Mode: free.Classic, Seconds: 0, Rounds: 3}); !is(err, free.ErrBadRequest) {
		t.Fatalf("a party without a timer: %v", err)
	}
	lobby, err := h.svc.NewParty(ctx, ann, "Ann", "en", free.Settings{Mode: free.Classic, Seconds: 30, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	code := lobby.Code
	if !free.ValidCode(code) || lobby.Host != 1 || lobby.Me != 1 || lobby.Status != free.StatusLobby {
		t.Fatalf("lobby: %+v", lobby)
	}
	if _, err := h.svc.StartParty(ctx, ann, code, "en", lobby.Version); !is(err, free.ErrAlone) {
		t.Fatalf("a party of one: %v", err)
	}
	if _, err := h.svc.Poll(ctx, bob, code, 0, "en"); !is(err, free.ErrNotMember) {
		t.Fatalf("poll before joining: %v", err)
	}
	if _, err := h.svc.Join(ctx, free.Who{Player: "6f1c1f43-3c39-4b8c-9a5c-2f6b1d0b7e11"}, "", code, "en"); !is(err, free.ErrSignIn) {
		t.Fatalf("a site guest joins: %v", err)
	}
	if _, err := h.svc.Join(ctx, bob, "Bob", "ZZZZZZZZ", "en"); !is(err, free.ErrNotFound) {
		t.Fatalf("a made-up code: %v", err)
	}
	b, err := h.svc.Join(ctx, bob, "Bob", code, "en")
	if err != nil {
		t.Fatal(err)
	}
	if b.Me != 2 || len(b.Members) != 2 {
		t.Fatalf("bob joined: %+v", b)
	}
	if _, err := h.svc.Join(ctx, cleo, "Cleo", code, "en"); err != nil {
		t.Fatal(err)
	}
	// Only the host changes the settings; a start on the old ones is refused.
	sixty := free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3}
	if _, err := h.svc.SetParty(ctx, bob, code, "en", sixty); !is(err, free.ErrNotHost) {
		t.Fatalf("bob changes the settings: %v", err)
	}
	if _, err := h.svc.StartParty(ctx, bob, code, "en", lobby.Version); !is(err, free.ErrNotHost) {
		t.Fatalf("bob starts: %v", err)
	}
	set, err := h.svc.SetParty(ctx, ann, code, "en", sixty)
	if err != nil {
		t.Fatal(err)
	}
	if set.Version != lobby.Version+1 || set.Settings != sixty || set.Length != 3*(60+2+10+3) {
		t.Fatalf("new settings: %+v", set)
	}
	if _, err := h.svc.StartParty(ctx, ann, code, "en", lobby.Version); !is(err, free.ErrChanged) || err.(*free.Error).Status != 409 {
		t.Fatalf("start on settings nobody saw: %v", err)
	}
	// A poll that knows the version gets the version and the clock only.
	short, err := h.svc.Poll(ctx, bob, code, set.V, "en")
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := short.(free.Short); !ok || s.V != set.V {
		t.Fatalf("short poll: %+v", short)
	} else if body, _ := json.Marshal(s); len(body) > 60 {
		t.Errorf("a short poll is %d bytes: %s", len(body), body)
	}
	started, err := h.svc.StartParty(ctx, ann, code, "en", set.Version)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := h.svc.StartParty(ctx, ann, code, "en", set.Version); err != nil || again.StartedAt == nil || !again.StartedAt.Equal(*started.StartedAt) {
		t.Fatalf("a second start: %v", err)
	}
	if started.Kind != free.Party || len(started.Rounds) != 1 {
		t.Fatalf("started: %+v", started)
	}
	r1 := started.Rounds[0]
	if r1.StartsAt.Sub(h.clock()) != free.Countdown || r1.EndsAt.Sub(r1.StartsAt) != 60*time.Second {
		t.Fatalf("round 1 times: %+v", r1)
	}
	noPlace(t, "a party's first place", started)
	token := strings.TrimSuffix(strings.TrimPrefix(r1.Image, "/api/free/img/"), ".jpg")
	if _, err := h.svc.Image(ctx, token); !is(err, free.ErrNotYet) {
		t.Fatalf("the picture before the countdown: %v", err)
	}
	s1 := h.partySpot(code, 1)
	if _, err := h.svc.GuessParty(ctx, ann, code, "en", free.GuessIn{N: 1, Pin: at(s1)}); !is(err, free.ErrNotYet) {
		t.Fatalf("a pin during the countdown: %v", err)
	}
	// Place 1: everyone answers, and it closes before its time.
	h.tick(free.Countdown + 5*time.Second)
	if _, err := h.svc.Image(ctx, token); err != nil {
		t.Fatal(err)
	}
	a1, err := h.svc.GuessParty(ctx, ann, code, "en", free.GuessIn{N: 1, Pin: at(s1)})
	if err != nil {
		t.Fatal(err)
	}
	noPlace(t, "the party after one answer", a1)
	if a1.Rounds[0].ClosedAt != nil || len(a1.Rounds[0].Answered) != 1 || a1.Rounds[0].Answered[0] != 1 {
		t.Fatalf("after ann: %+v", a1.Rounds[0])
	}
	bv := h.view(h.svc, bob, code, set.V)
	noPlace(t, "bob's view after ann answered", bv)
	if _, err := h.svc.GuessParty(ctx, bob, code, "en", free.GuessIn{N: 1, Pin: &geo.Point{Lat: 50, Lon: 10}}); err != nil {
		t.Fatal(err)
	}
	h.tick(time.Second)
	h.view(h.svc, cleo, code, 0)
	c1, err := h.svc.GuessParty(ctx, cleo, code, "en", free.GuessIn{N: 1, Pin: &geo.Point{Lat: 0, Lon: -30}})
	if err != nil {
		t.Fatal(err)
	}
	closed := c1.Rounds[0]
	if closed.ClosedAt == nil || !closed.ClosedAt.Equal(h.clock()) || closed.Place == nil || closed.Place.Country != s1.iso || len(closed.Guesses) != 3 {
		t.Fatalf("all answered, closed: %+v", closed)
	}
	if len(c1.Rounds) != 2 || !c1.Rounds[1].StartsAt.Equal(closed.ClosedAt.Add(10*time.Second+free.Countdown)) {
		t.Fatalf("next place after the reveal and the countdown: %+v", c1.Rounds)
	}
	if c1.Best == nil || c1.Best.M != 1 || c1.Totals[0].M != 1 || c1.Totals[0].Rank != 1 {
		t.Fatalf("ann leads: %+v %+v", c1.Best, c1.Totals)
	}
	if _, err := h.svc.GuessParty(ctx, bob, code, "en", free.GuessIn{N: 1, Pin: at(s1)}); !is(err, free.ErrAnswered) {
		t.Fatalf("a second pin: %v", err)
	}
	// Everyone watches the reveal, then the service restarts: the new one
	// reads the same game and goes on, and still waits for Bob.
	h.wait(c1.Rounds[1].StartsAt.Add(-time.Second), ann, bob)
	h.svc = h.service(h.svc.Countries)
	h.tick(time.Second)
	r2 := h.view(h.svc, ann, code, c1.V)
	if r2.V == c1.V || len(r2.Rounds) != 2 || r2.Rounds[1].ClosedAt != nil {
		t.Fatalf("after the restart: %+v", r2)
	}
	noPlace(t, "place 2 after the restart", r2.Rounds[1])
	// Dave comes during place 2: he plays from place 3.
	dave := free.Who{UserID: 104}
	d, err := h.svc.Join(ctx, dave, "Dave", code, "en")
	if err != nil {
		t.Fatal(err)
	}
	if d.Me != 4 || d.Members[3].From != 3 {
		t.Fatalf("dave: %+v", d.Members)
	}
	if _, err := h.svc.GuessParty(ctx, dave, code, "en", free.GuessIn{N: 2, Pin: at(s1)}); !is(err, free.ErrTooLate) {
		t.Fatalf("dave on place 2: %v", err)
	}
	// Place 2: ann answers; bob keeps polling and never answers, cleo is
	// gone. It closes at its end and the grace, not before.
	if _, err := h.svc.GuessParty(ctx, ann, code, "en", free.GuessIn{N: 2, Pin: at(h.partySpot(code, 2))}); err != nil {
		t.Fatal(err)
	}
	ends := *r2.Rounds[1].EndsAt
	for h.clock().Before(ends) {
		h.tick(5 * time.Second)
		if v := h.view(h.svc, bob, code, 0); v.Rounds[1].ClosedAt != nil {
			t.Fatalf("closed at %v while bob is there", h.clock())
		}
	}
	h.mu.Lock()
	h.now = ends.Add(1500 * time.Millisecond)
	h.mu.Unlock()
	late, err := h.svc.GuessParty(ctx, bob, code, "en", free.GuessIn{N: 2, Pin: at(h.partySpot(code, 2))})
	if err != nil {
		t.Fatalf("a pin in the grace: %v", err)
	}
	if late.Rounds[1].ClosedAt == nil {
		t.Fatal("bob was the last one waited for")
	}
	for _, g := range late.Rounds[1].Guesses {
		if g.M == 2 && (g.Speed != 0 || g.Distance != 3500) {
			t.Fatalf("a pin in the grace has no speed: %+v", g)
		}
	}
	if _, err := h.svc.GuessParty(ctx, cleo, code, "en", free.GuessIn{N: 2, Pin: at(s1)}); !is(err, free.ErrClosed) {
		t.Fatalf("after the close: %v", err)
	}
	// Gus had place 3, drawn before he came, in a game of his own: it is not
	// his to play, and it is the last one.
	gus := free.Who{UserID: 107}
	if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'Gus')`, gus.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `
		WITH g AS (INSERT INTO maply.free_game (kind, mode, rounds, status, created_at)
		           VALUES ('solo', 'classic', 3, 'done', now()) RETURNING id),
		     m AS (INSERT INTO maply.free_member (game, m, user_id, name, joined_at, last_seen_at)
		           SELECT id, 1, $2, 'Gus', now(), now() FROM g)
		INSERT INTO maply.free_round (game, n, place_id, starts_at)
		SELECT g.id, 1, r.place_id, now() FROM g, maply.free_round r JOIN maply.free_game p ON p.id = r.game
		 WHERE p.code = $1 AND r.n = 3`, code, gus.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Join(ctx, gus, "Gus", code, "en"); !is(err, free.ErrTooLate) {
		t.Fatalf("gus knows the place drawn before he came: %v", err)
	}
	// Erin comes in the reveal of place 2 and plays place 3.
	erin := free.Who{UserID: 105}
	if e, err := h.svc.Join(ctx, erin, "Erin", code, "en"); err != nil || e.Members[len(e.Members)-1].From != 3 {
		t.Fatalf("erin: %v", err)
	}
	// Place 3 closes on its time with nobody answering; the game ends after
	// its reveal.
	r3 := late.Rounds[2]
	h.mu.Lock()
	h.now = r3.EndsAt.Add(free.Grace)
	h.mu.Unlock()
	v3 := h.view(h.svc, ann, code, 0)
	if v3.Rounds[2].ClosedAt == nil || !v3.Rounds[2].ClosedAt.Equal(r3.EndsAt.Add(free.Grace)) || v3.Status != free.StatusPlaying {
		t.Fatalf("place 3 closed on its time: %+v", v3.Rounds[2])
	}
	if _, err := h.svc.Join(ctx, free.Who{UserID: 106}, "Finn", code, "en"); !is(err, free.ErrTooLate) {
		t.Fatalf("a join after the last place began: %v", err)
	}
	if _, err := h.svc.Rematch(ctx, ann, "Ann", code); !is(err, free.ErrOrder) {
		t.Fatalf("a rematch before the end: %v", err)
	}
	h.tick(10 * time.Second)
	done := h.view(h.svc, bob, code, 0)
	if done.Status != free.StatusDone || len(done.Totals) != 5 {
		t.Fatalf("done: %+v", done)
	}
	if done.Totals[0].M != 1 || done.Totals[1].M != 2 {
		t.Fatalf("standing: %+v", done.Totals)
	}
	rows, err := h.pool.Query(ctx, `SELECT r.user_id, r.kind, r.place, r.players, r.beaten, r.played, r.answered FROM maply.free_result r JOIN maply.free_game g ON g.id = r.game WHERE g.code = $1 ORDER BY place, user_id`, code)
	if err != nil {
		t.Fatal(err)
	}
	type res struct {
		user                                     int64
		kind                                     string
		place, players, beaten, played, answered int
	}
	var results []res
	for rows.Next() {
		var r res
		if err := rows.Scan(&r.user, &r.kind, &r.place, &r.players, &r.beaten, &r.played, &r.answered); err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	if len(results) != 5 || results[0] != (res{101, "party", 1, 5, 4, 3, 2}) || results[1] != (res{102, "party", 2, 5, 3, 3, 2}) {
		t.Fatalf("results: %+v", results)
	}
	// A rematch: the first asks, the second lands in the same lobby.
	next, err := h.svc.Rematch(ctx, bob, "Bob", code)
	if err != nil {
		t.Fatal(err)
	}
	same, err := h.svc.Rematch(ctx, ann, "Ann", code)
	if err != nil || same != next || next == code {
		t.Fatalf("rematch %q %q %v", next, same, err)
	}
	nv := h.view(h.svc, ann, next, 0)
	if nv.Status != free.StatusLobby || nv.Host != 1 || nv.Members[0].Name != "Bob" || len(nv.Members) != 2 || nv.Settings != sixty {
		t.Fatalf("rematch lobby: %+v", nv)
	}
	if h.view(h.svc, cleo, code, 0).Next != next {
		t.Fatal("the old party names the rematch")
	}
}

func TestPartyHostAwayAndLobbies(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	set := free.Settings{Mode: free.Classic, Seconds: 30, Rounds: 5}
	lobby, err := h.svc.NewParty(ctx, ann, "Ann", "en", set)
	if err != nil {
		t.Fatal(err)
	}
	code := lobby.Code
	if _, err := h.svc.Join(ctx, bob, "Bob", code, "en"); err != nil {
		t.Fatal(err)
	}
	// Ann goes quiet; after 30 s Bob, who keeps polling, is the host.
	for range 7 {
		h.tick(5 * time.Second)
		h.view(h.svc, bob, code, 0)
	}
	v := h.view(h.svc, bob, code, 0)
	if v.Host != 2 {
		t.Fatalf("host after ann went quiet: %d", v.Host)
	}
	// Nobody asks for 16 minutes and the party leaves memory. Read again, it
	// has a version no app has seen: an app still on the last one gets the
	// whole party, where Bob has gone quiet too and Ann is the host again.
	h.tick(16 * time.Minute)
	if err := h.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if back := h.view(h.svc, ann, code, v.V); back.V <= v.V || back.Host != 1 {
		t.Fatalf("after the eviction: v %d after %d, host %d", back.V, v.V, back.Host)
	}
	h.view(h.svc, bob, code, 0)
	// Opening the invitation again changes nothing for the others.
	again, err := h.svc.Join(ctx, bob, "Bob", code, "en")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := h.svc.Poll(ctx, ann, code, again.V, "en"); err != nil {
		t.Fatal(err)
	} else if _, short := got.(free.Short); !short {
		t.Fatalf("a second open moved the version: %+v", got)
	}
	// The lobby is full at eight.
	for i := 3; i <= 8; i++ {
		if _, err := h.svc.Join(ctx, free.Who{UserID: int64(200 + i)}, fmt.Sprint("P", i), code, "en"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.Join(ctx, free.Who{UserID: 300}, "Nine", code, "en"); !is(err, free.ErrFull) {
		t.Fatalf("a ninth: %v", err)
	}
	// Someone leaves, a seat is free, and the numbers are not reused.
	if err := h.svc.LeaveParty(ctx, free.Who{UserID: 208}, code); err != nil {
		t.Fatal(err)
	}
	nine, err := h.svc.Join(ctx, free.Who{UserID: 300}, "Nine", code, "en")
	if err != nil || nine.Me != 9 || len(nine.Members) != 8 {
		t.Fatalf("nine: %v %+v", err, nine.Members)
	}
	// Everyone leaves: the lobby is given up.
	for _, m := range nine.Members {
		w := free.Who{UserID: int64(200 + m.M)}
		switch m.M {
		case 1:
			w = ann
		case 2:
			w = bob
		case 9:
			w = free.Who{UserID: 300}
		}
		if err := h.svc.LeaveParty(ctx, w, code); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	if err := h.pool.QueryRow(ctx, `SELECT status FROM maply.free_game WHERE code = $1`, code).Scan(&status); err != nil || status != free.StatusAbandoned {
		t.Fatalf("empty lobby: %q %v", status, err)
	}
	// A lobby nobody touches for half an hour is given up by the sweep.
	quiet, err := h.svc.NewParty(ctx, ann, "Ann", "en", set)
	if err != nil {
		t.Fatal(err)
	}
	h.tick(31 * time.Minute)
	if err := h.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT status FROM maply.free_game WHERE code = $1`, quiet.Code).Scan(&status); err != nil || status != free.StatusAbandoned {
		t.Fatalf("quiet lobby: %q %v", status, err)
	}
	// A duel where Bob stops answering: after two missed places he is away
	// and the places close as soon as Ann answers.
	duel, err := h.svc.NewParty(ctx, ann, "Ann", "en", set)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Join(ctx, bob, "Bob", duel.Code, "en"); err != nil {
		t.Fatal(err)
	}
	st, err := h.svc.StartParty(ctx, ann, duel.Code, "en", duel.Version)
	if err != nil || st.Kind != free.Duel {
		t.Fatalf("duel: %v %+v", err, st.Kind)
	}
	for n := 1; n <= 3; n++ {
		v := h.view(h.svc, ann, duel.Code, 0)
		r := v.Rounds[n-1]
		h.mu.Lock()
		h.now = r.StartsAt.Add(time.Second)
		h.mu.Unlock()
		h.view(h.svc, bob, duel.Code, 0) // bob is there, but does not answer
		g, err := h.svc.GuessParty(ctx, ann, duel.Code, "en", free.GuessIn{N: n, Pin: at(h.partySpot(duel.Code, n))})
		if err != nil {
			t.Fatal(err)
		}
		closed := g.Rounds[n-1].ClosedAt != nil
		if want := n == 3; closed != want {
			t.Fatalf("place %d closed at ann's answer: %v", n, closed)
		}
		if !closed {
			h.mu.Lock()
			h.now = r.EndsAt.Add(free.Grace)
			h.mu.Unlock()
			if v := h.view(h.svc, ann, duel.Code, 0); v.Rounds[n-1].ClosedAt == nil {
				t.Fatalf("place %d after its time", n)
			}
		}
	}
	if v := h.view(h.svc, ann, duel.Code, 0); !v.Members[1].Away {
		t.Fatalf("bob is away: %+v", v.Members)
	}
}
