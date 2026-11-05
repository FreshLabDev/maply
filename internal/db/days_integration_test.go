// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// queries counts what a pool sends to the database.
type queries struct{ n atomic.Int32 }

func (q *queries) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.n.Add(1)
	return ctx
}

func (q *queries) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// counted is a second Store on the test database whose queries are counted.
func counted(t *testing.T) (*Store, *queries) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv("MAPLY_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "maply"
	q := &queries{}
	cfg.ConnConfig.Tracer = q
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &Store{pool: pool}, q
}

// dayRow is d's line of the days list for the viewer of q, d being over in
// every zone.
func dayRow(t *testing.T, s *Store, d round.Date, q DaysQuery) DayRow {
	t.Helper()
	q.Newest, q.Final, q.Seen, q.N = d, d, d, 1
	p, err := s.DaysPage(context.Background(), q)
	if err != nil || len(p.Days) != 1 || p.Days[0].Date != d {
		t.Fatalf("days page for %v = %+v, %v", d, p, err)
	}
	return p.Days[0]
}

const historyGroup = int64(-1001900007001)

// history is ten days from 1 November: Pin (7) and Ana (8) of a group answer
// most of them, a site player some, and nobody the 4th. The 1st to the 7th
// are kept closed, the 1st to the 6th with their strips; the 8th is over
// everywhere but not kept yet; the 9th and the 10th are still open, the 10th
// kept and drawn by mistake, as a hand-made row would be.
func history(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	group := &tg.Chat{ID: historyGroup, Type: "supergroup"}
	for _, u := range []tg.User{{ID: 7, FirstName: "Pin"}, {ID: 8, FirstName: "Ana"}} {
		if err := store.Touch(ctx, u, group); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SitePlayer(ctx, sitePlayer, ptr("Lu")); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 10; n++ {
		d := round.NewDate(2026, 11, n)
		exec(t, store, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key,
		                                    country_iso, continent, city, difficulty, reviewed_at)
		            OVERRIDING SYSTEM VALUE
		            VALUES ($1, 'used', 'mapillary', $2, 'CC-BY-SA-4.0', 'panorama', 50.45, 30.52, $3, 'UA', 'europe',
		                    '{"en":"Kyiv"}', 2, now())`, n, fmt.Sprintf("m%d", n), fmt.Sprintf("image%02dkey", n))
		exec(t, store, `INSERT INTO maply.round (date, day_no, place_id) VALUES ($1, $2, $3)`, d.Time(), n, n)
		at := time.Date(2026, 11, n, 8, 0, 0, 0, time.UTC)
		// Pin is closer on even days, Ana on odd ones; on the 6th Pin is
		// on the spot.
		pin, ana := geo.Point{Lat: 50, Lon: 30}, geo.Point{Lat: 49, Lon: 25}
		if n%2 == 1 {
			pin, ana = ana, pin
		}
		if n == 6 {
			pin = geo.Point{Lat: 50.45, Lon: 30.52}
		}
		for _, g := range []NewGuess{
			{Date: d, UserID: 7, Context: "group", ChatID: historyGroup, Pin: pin, AnsweredAt: at, Deadline: at.Add(time.Hour)},
			{Date: d, UserID: 8, Context: "group", ChatID: historyGroup, Pin: ana, AnsweredAt: at, Deadline: at.Add(time.Hour)},
		} {
			if n == 4 {
				break
			}
			if err := store.InsertGuess(ctx, g); err != nil {
				t.Fatal(err)
			}
		}
		if n%3 == 0 {
			if err := store.InsertSiteGuess(ctx, NewSiteGuess{Date: d, Player: sitePlayer, Pin: geo.Point{Lat: 0, Lon: 0}, AnsweredAt: at, Deadline: at.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
		}
		kyiv := scoring.Place{Point: geo.Point{Lat: 50.45, Lon: 30.52}, Country: "UA", Continent: "europe"}
		if _, err := store.ScoreDue(ctx, d, at.Add(time.Hour), func(p geo.Point) scoring.Score { return scoring.Judge(p, kyiv, nil) }); err != nil {
			t.Fatal(err)
		}
		if n <= 7 {
			if err := store.KeepDay(ctx, d); err != nil {
				t.Fatal(err)
			}
		}
		if n <= 6 {
			if err := store.PutThumb(ctx, Thumb{Date: d, Key: fmt.Sprintf("thumb%02dAAAAAAAAAAAAAA", n)[:20], Recipe: 1, Small: []byte("s"), Wide: []byte("w")}); err != nil {
				t.Fatal(err)
			}
		}
	}
	exec(t, store, `INSERT INTO maply.day_closed (date) VALUES ('2026-11-10')`)
	if err := store.PutThumb(ctx, Thumb{Date: round.NewDate(2026, 11, 10), Key: "thumb10AAAAAAAAAAAAA", Recipe: 1, Small: []byte("s"), Wide: []byte("w")}); err != nil {
		t.Fatal(err)
	}
}

func TestDaysPage(t *testing.T) {
	store := testStore(t)
	history(t, store)
	ctx := context.Background()
	reader, sent := counted(t)
	q := DaysQuery{
		Newest: round.NewDate(2026, 11, 10), N: 4,
		Final: round.NewDate(2026, 11, 8), Seen: round.NewDate(2026, 11, 9),
		UserID: 7, ChatID: historyGroup,
	}
	var dates []round.Date
	var all []DayRow
	for page := 0; ; page++ {
		sent.n.Store(0)
		p, err := reader.DaysPage(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if n := sent.n.Load(); n != 1 {
			t.Fatalf("page %d took %d queries, want 1", page, n)
		}
		for _, d := range p.Days {
			dates = append(dates, d.Date)
		}
		all = append(all, p.Days...)
		if p.Best != round.NewDate(2026, 11, 6) {
			t.Errorf("best = %v, want the 6th, Pin's spot-on day", p.Best)
		}
		if !p.More {
			break
		}
		q.Before = p.Days[len(p.Days)-1].Date
	}
	if len(dates) != 10 || dates[0] != round.NewDate(2026, 11, 10) || dates[9] != round.NewDate(2026, 11, 1) {
		t.Fatalf("dates = %v", dates)
	}
	day := func(n int) DayRow { return all[10-n] }

	// Open: how many answered, whether Pin did, and the picture up to the
	// date begun where he is. Nothing scored, nothing about the place, no
	// strip even where one was drawn by mistake.
	for n, answered := range map[int]int{9: 3, 10: 2} {
		d := day(n)
		if d.Players+d.Pending != answered || !d.Played {
			t.Errorf("open %d: %+v", n, d)
		}
		if d.MedianKm != nil || d.BestKm != nil || d.CountryPct != 0 || d.Country != "" || len(d.City) != 0 || d.Thumb != "" || d.Me != nil || d.Group != nil {
			t.Errorf("open %d says what it must not: %+v", n, d)
		}
	}
	if day(9).Image != "image09key" || day(9).Kind != "panorama" || day(10).Image != "" || day(10).Kind != "" {
		t.Errorf("open pictures: 9 %q %q, 10 %q %q", day(9).Image, day(9).Kind, day(10).Image, day(10).Kind)
	}

	// Final: the place and the numbers; the strip once kept and drawn; the
	// viewer's line from the kept places or, on the 8th, ranked live.
	for n := 1; n <= 8; n++ {
		d := day(n)
		if d.Image != "" || d.Kind != "" {
			t.Errorf("final %d carries the puzzle: %+v", n, d)
		}
		if n == 4 {
			if d.Thumb == "" || d.Country != "UA" {
				t.Errorf("nobody's day has no strip or place: %+v", d)
			}
			if d.Players != 0 || d.Played || d.Me != nil || d.MedianKm != nil || d.Group == nil || d.Group.Answered != 0 || d.Group.Members != 2 {
				t.Errorf("nobody's day: %+v %+v", d, d.Group)
			}
			continue
		}
		if d.Country != "UA" || d.City["en"] != "Kyiv" || d.MedianKm == nil || d.BestKm == nil || !d.Played || d.Me == nil {
			t.Errorf("final %d: %+v", n, d)
			continue
		}
		wantThumb := n <= 6
		if (d.Thumb != "") != wantThumb {
			t.Errorf("final %d thumb %q", n, d.Thumb)
		}
		wantRank := 2
		if n%2 == 0 {
			wantRank = 1
		}
		if d.Me.Rank != wantRank {
			t.Errorf("final %d: Pin's rank %d, want %d", n, d.Me.Rank, wantRank)
		}
		if d.Group == nil || d.Group.Answered != 2 || d.Group.Members != 2 || d.Group.BestKm == nil {
			t.Errorf("final %d group: %+v", n, d.Group)
		} else if want := map[bool]string{true: "Pin", false: "Ana"}[n%2 == 0]; d.Group.BestName != want {
			t.Errorf("final %d: group's best %q, want %q", n, d.Group.BestName, want)
		}
		wantPlayers := 2
		if n%3 == 0 {
			wantPlayers = 3
		}
		if d.Players != wantPlayers || d.Pending != 0 {
			t.Errorf("final %d: %d players, %d pending", n, d.Players, d.Pending)
		}
	}

	// The site player: their own line where they answered, no group.
	sent.n.Store(0)
	p, err := reader.DaysPage(ctx, DaysQuery{Newest: round.NewDate(2026, 11, 10), N: 30, Final: round.NewDate(2026, 11, 8), Seen: round.NewDate(2026, 11, 9), Player: sitePlayer})
	if err != nil || sent.n.Load() != 1 {
		t.Fatalf("site page: %v, %d queries", err, sent.n.Load())
	}
	for _, d := range p.Days {
		n := d.Date.Time().Day()
		if d.Group != nil || d.Played != (n%3 == 0) || (d.Me != nil) != (n%3 == 0 && n <= 8) {
			t.Errorf("site %d: %+v", n, d)
		}
		if d.Me != nil && d.Me.Rank != 3 {
			t.Errorf("site %d ranks %d, want after both Telegram players", n, d.Me.Rank)
		}
	}
	if p.More || p.Best.IsZero() {
		t.Errorf("site page more %v, best %v", p.More, p.Best)
	}

	// Nobody: the world's numbers alone.
	p, err = reader.DaysPage(ctx, DaysQuery{Newest: round.NewDate(2026, 11, 10), N: 30, Final: round.NewDate(2026, 11, 8), Seen: round.NewDate(2026, 11, 9)})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.Days {
		if d.Played || d.Me != nil || d.Group != nil {
			t.Errorf("nobody's view of %v: %+v", d.Date, d)
		}
	}
	if !p.Best.IsZero() {
		t.Errorf("nobody's best = %v", p.Best)
	}
}

func TestDayStrips(t *testing.T) {
	store := testStore(t)
	history(t, store)
	ctx := context.Background()
	// The 7th is kept without a strip; the 8th is not kept and is not drawn.
	undrawn, err := store.Undrawn(ctx, 1)
	if err != nil || len(undrawn) != 1 || undrawn[0].Date != round.NewDate(2026, 11, 7) || undrawn[0].ImageKey != "image07key" || undrawn[0].Yaw != nil {
		t.Fatalf("undrawn = %+v, %v", undrawn, err)
	}
	exec(t, store, `UPDATE maply.place SET thumb_yaw = 105 WHERE id = 7`)
	if undrawn, _ = store.Undrawn(ctx, 1); len(undrawn) != 1 || undrawn[0].Yaw == nil || *undrawn[0].Yaw != 105 {
		t.Fatalf("undrawn with a yaw = %+v", undrawn)
	}
	// A newer recipe draws every kept day again.
	if undrawn, _ = store.Undrawn(ctx, 2); len(undrawn) != 8 {
		t.Fatalf("undrawn for recipe 2 = %d days", len(undrawn))
	}
	if err := store.PutThumb(ctx, Thumb{Date: round.NewDate(2026, 11, 8), Key: "notkeptAAAAAAAAAAAAA", Recipe: 1, Small: []byte("s"), Wide: []byte("w")}); !errors.Is(err, ErrNotKept) {
		t.Fatalf("a strip for a day not kept: %v", err)
	}
	// An older recipe does not replace a newer strip; a newer one does,
	// under its own key.
	d := round.NewDate(2026, 11, 1)
	if err := store.PutThumb(ctx, Thumb{Date: d, Key: "olderAAAAAAAAAAAAAAA", Recipe: 0, Small: []byte("o"), Wide: []byte("o")}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.ThumbByKey(ctx, "olderAAAAAAAAAAAAAAA"); ok {
		t.Fatal("an older recipe replaced the strip")
	}
	if err := store.PutThumb(ctx, Thumb{Date: d, Key: "newerAAAAAAAAAAAAAAA", Recipe: 2, Small: []byte("n"), Wide: []byte("N")}); err != nil {
		t.Fatal(err)
	}
	if th, ok, err := store.ThumbByKey(ctx, "newerAAAAAAAAAAAAAAA"); err != nil || !ok || th.Date != d || string(th.Wide) != "N" {
		t.Fatalf("newer strip = %+v, %v, %v", th, ok, err)
	}
	if _, ok, _ := store.ThumbByKey(ctx, "thumb01AAAAAAAAAAAAA"); ok {
		t.Fatal("the old key still serves")
	}
	// A day taken out of day_closed by hand loses its strip with its places.
	exec(t, store, `DELETE FROM maply.day_closed WHERE date = '2026-11-02'`)
	if _, ok, _ := store.ThumbByKey(ctx, "thumb02AAAAAAAAAAAAA"); ok {
		t.Fatal("a strip outlived its day")
	}

	c, err := store.ClosedDay(ctx, round.NewDate(2026, 11, 5), round.NewDate(2026, 11, 8))
	if err != nil || c.Thumb != "thumb05AAAAAAAAAAAAA" || c.Image != "image05key" || c.Kind != "panorama" || len(c.Pins) != 2 || c.Prev != 4 || c.Next != 6 {
		t.Fatalf("closed day = %+v, %v", c, err)
	}
	// The next day stops at the newest begun; the first has no day before.
	if c, _ := store.ClosedDay(ctx, round.NewDate(2026, 11, 8), round.NewDate(2026, 11, 8)); c.Next != 0 || c.Prev != 7 {
		t.Fatalf("the newest day's neighbours = %d, %d", c.Prev, c.Next)
	}
	if c, _ := store.ClosedDay(ctx, round.NewDate(2026, 11, 1), round.NewDate(2026, 11, 8)); c.Prev != 0 || c.Next != 2 {
		t.Fatalf("the first day's neighbours = %d, %d", c.Prev, c.Next)
	}
	if r, ok, err := store.RoundNo(ctx, 5); err != nil || !ok || r.Date != round.NewDate(2026, 11, 5) {
		t.Fatalf("round #5 = %+v, %v, %v", r, ok, err)
	}
	if _, ok, err := store.RoundNo(ctx, 99); err != nil || ok {
		t.Fatalf("round #99: %v, %v", ok, err)
	}
}
