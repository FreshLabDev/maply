// SPDX-License-Identifier: Apache-2.0
//go:build integration

package schedule

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
)

// standing is a Kyiv group on 2 November 2026 with one place, a scheduler on
// a clock the test moves, and the fake Telegram.
type standing struct {
	store *db.Store
	pool  *pgxpool.Pool
	fake  *fakeTG
	s     *Scheduler
	zones round.Zones
	clock time.Time
	d     round.Date
	kyiv  *time.Location
}

func newStanding(t *testing.T, places int) *standing {
	t.Helper()
	store, pool := testDB(t)
	ctx := context.Background()
	images := t.TempDir()
	for i := 1; i <= places; i++ {
		key := fmt.Sprintf("k%d", i)
		if err := os.WriteFile(filepath.Join(images, key), []byte("jpeg"), 0o644); err != nil {
			t.Fatal(err)
		}
		exec(t, pool, `INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon, image_key,
		                                       country_iso, continent, city, fact, difficulty, reviewed_at)
		               VALUES ('approved', 'mapillary', $1, 'CC-BY-SA-4.0', 'panorama', 38.72, -9.14, $2, 'PT', 'europe',
		                       '{"en":"Lisbon"}', '{"en":"Built on seven hills."}', 2, now())`, key, key)
	}
	for _, u := range []tg.User{{ID: player, FirstName: "Pin", LanguageCode: "en"}, {ID: quiet, FirstName: "Quiet", LanguageCode: "en"}} {
		if err := store.Touch(ctx, u, &tg.Chat{ID: group, Type: "supergroup", Title: "Maps"}); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, pool, `INSERT INTO maply.chat_settings (chat_id, kind, timezone) VALUES ($1, 'group', 'Europe/Kyiv')`, group)
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	countries, err := geo.World()
	if err != nil {
		t.Fatal(err)
	}
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	st := &standing{store: store, pool: pool, fake: &fakeTG{fail: map[int64]error{}, failKind: map[string]error{}}, zones: zones, kyiv: kyiv,
		clock: time.Date(2026, 11, 2, 5, 0, 0, 0, kyiv)}
	st.d = round.DateIn(st.clock, kyiv)
	st.s = &Scheduler{Store: store, TG: st.fake, Weather: fakeWeather{}, Payloads: auth.NewSigner("123:abc"), Zones: zones, Countries: countries,
		ImagesDir: images, Username: "maplyin_bot", AppURL: "https://maply.example/",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return st.clock }}
	return st
}

// run steps the scheduler every ten minutes until the clock reaches until.
func (st *standing) run(t *testing.T, until time.Time) {
	t.Helper()
	for ; st.clock.Before(until); st.clock = st.clock.Add(10 * time.Minute) {
		st.s.Step(context.Background())
	}
	st.clock = until
	st.s.Step(context.Background())
}

func (st *standing) held(t *testing.T) bool {
	t.Helper()
	var held bool
	if err := st.pool.QueryRow(context.Background(), `SELECT held FROM maply.chat_round WHERE chat_id = $1 AND date = $2`, group, st.d.Time()).Scan(&held); err != nil {
		t.Fatal(err)
	}
	return held
}

func (st *standing) skipped(t *testing.T, d round.Date) (skipped bool, status string, back *time.Time) {
	t.Helper()
	err := st.pool.QueryRow(context.Background(), `
		SELECT r.skipped_at IS NOT NULL, p.status, p.back_after FROM maply.round r JOIN maply.place p ON p.id = r.place_id
		 WHERE r.date = $1`, d.Time()).Scan(&skipped, &status, &back)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func (st *standing) kept(t *testing.T, d round.Date) bool {
	t.Helper()
	var n int
	if err := st.pool.QueryRow(context.Background(), `SELECT count(*) FROM maply.day_closed WHERE date = $1`, d.Time()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// A day nobody answered anywhere: the chat's result is held at its reveal, no
// place is shown, and once every zone is over the morning post says nobody
// answered and is unpinned, and the place goes back for a month.
func TestNobodyAnsweredDayIsSkippedAtTheFinal(t *testing.T) {
	st := newStanding(t, 1)
	ctx := context.Background()
	st.run(t, st.d.At(20, 30, st.kyiv))

	// The post went out and was pinned; at Kyiv's reveal nothing else did: no
	// result, and the place was not shown.
	if got := st.fake.count("photo", group) + st.fake.count("text", group); got != 2 {
		t.Fatalf("the group got %d messages by the evening, want the post and the intrigue: %+v", got, st.fake.log)
	}
	for _, m := range st.fake.log {
		if strings.Contains(m.text, "Lisbon") || strings.Contains(m.text, "seven hills") {
			t.Fatalf("a message shows the place of a day nobody answered: %q", m.text)
		}
	}
	if !st.held(t) {
		t.Fatal("the chat's result is not held")
	}
	if skipped, _, _ := st.skipped(t, st.d); skipped {
		t.Fatal("the day was skipped before it was over everywhere")
	}
	if st.fake.count("pin", group) != 1 || st.fake.count("unpin", group) != 0 {
		t.Fatalf("pins %d, unpins %d before the final", st.fake.count("pin", group), st.fake.count("unpin", group))
	}

	// The final world job.
	st.run(t, st.zones.LastReveal(st.d).Add(worldAfter+time.Minute))
	skipped, status, back := st.skipped(t, st.d)
	wantBack := round.DateIn(st.clock, time.UTC).AddDays(db.ReturnDays)
	if !skipped || status != "approved" || back == nil || round.DateIn(*back, time.UTC) != wantBack {
		t.Fatalf("skipped %v, place %q, back after %v, want skipped, approved, %v", skipped, status, back, wantBack)
	}
	if st.kept(t, st.d) {
		t.Fatal("a skipped day was kept")
	}
	edit := st.fake.last("edit", group)
	if !strings.Contains(edit.text, i18n.T("en", "post.skipped")) || !strings.Contains(edit.text, "maply #1") {
		t.Fatalf("the post became %q", edit.text)
	}
	if len(edit.buttons) != 0 {
		t.Fatalf("the skipped post kept its button: %+v", edit.buttons)
	}
	if st.fake.count("unpin", group) != 1 {
		t.Fatalf("%d unpins, want 1", st.fake.count("unpin", group))
	}
	if got := st.fake.count("photo", group) + st.fake.count("text", group); got != 2 {
		t.Fatalf("the skip sent a message: %d in all", got)
	}

	// Nothing more happens on another pass or on the catch-up.
	calls := len(st.fake.log)
	st.clock = st.clock.Add(2 * time.Hour)
	st.s.Step(ctx)
	if err := st.s.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	if len(st.fake.log) != calls {
		t.Fatalf("a skipped day was worked on again: %+v", st.fake.log[calls:])
	}
	// The date has no round now, and the place waits.
	if _, ok, err := st.store.RoundOn(ctx, st.d); err != nil || ok {
		t.Fatalf("RoundOn = %v, %v", ok, err)
	}
	if _, err := st.store.EnsureRound(ctx, st.d); err == nil {
		t.Fatal("a skipped date got a place")
	}
}

// Held, then another zone answers before the day closes: the final job sends
// the chat's result, late, and keeps the day. The chat itself had nobody, and
// says so next to the place.
func TestHeldResultGoesOutWhenAnotherZoneAnswers(t *testing.T) {
	st := newStanding(t, 1)
	ctx := context.Background()
	if err := st.store.Touch(ctx, tg.User{ID: alone, FirstName: "West", LanguageCode: "en"}, nil); err != nil {
		t.Fatal(err)
	}
	st.run(t, st.d.At(20, 30, st.kyiv))
	if !st.held(t) {
		t.Fatal("the chat's result is not held")
	}
	messages := st.fake.count("photo", group) + st.fake.count("text", group)

	// Someone in Los Angeles answers in their evening, before their reveal.
	last := st.zones.LastReveal(st.d)
	exec(t, st.pool, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
	                  VALUES ($1, $2, 'dm', 40.4, -3.7, $3, $4)`, st.d.Time(), alone, last.Add(-time.Hour), last)
	st.run(t, last.Add(worldAfter+time.Minute))

	if skipped, _, _ := st.skipped(t, st.d); skipped {
		t.Fatal("a day with an answer was skipped")
	}
	if !st.kept(t, st.d) {
		t.Fatal("the day was not kept")
	}
	cr, err := st.store.ChatRound(ctx, group, st.d)
	if err != nil || cr.ResultMsgID == 0 {
		t.Fatalf("the held result was not sent: %+v, %v", cr, err)
	}
	if got := st.fake.count("photo", group) + st.fake.count("text", group); got != messages+1 {
		t.Fatalf("the group got %d messages, want %d", got, messages+1)
	}
	result := st.fake.last("photo", group).text
	for _, want := range []string{"Lisbon", "Built on seven hills.", i18n.T("en", "result.nobody")} {
		if !strings.Contains(result, want) {
			t.Errorf("the late result %q lacks %q", result, want)
		}
	}
	if st.fake.count("unpin", group) != 1 {
		t.Errorf("%d unpins, want the post unpinned with the result", st.fake.count("unpin", group))
	}
	if held, err := st.store.HeldChats(ctx, st.d); err != nil || len(held) != 0 {
		t.Errorf("still held: %v, %v", held, err)
	}
}

// A day whose final job never ran is closed by the catch-up, an hour after
// the last reveal; one that nobody answered is skipped by it.
func TestCatchUpClosesTheDaysTheFinalJobMissed(t *testing.T) {
	st := newStanding(t, 2)
	ctx := context.Background()
	answered, nobody := st.d, st.d.AddDays(1)
	for _, d := range []round.Date{answered, nobody} {
		if _, err := st.store.EnsureRound(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.store.Touch(ctx, tg.User{ID: alone, FirstName: "Solo", LanguageCode: "en"}, nil); err != nil {
		t.Fatal(err)
	}
	exec(t, st.pool, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
	                  VALUES ($1, $2, 'dm', 40.4, -3.7, $3, $4)`, answered.Time(), alone, answered.At(10, 0, st.kyiv), answered.Reveal(st.kyiv))

	// Nothing before the hour is up: the job may still be about to run.
	st.clock = st.zones.LastReveal(answered).Add(catchUpAfter - time.Minute)
	if err := st.s.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	if st.kept(t, answered) {
		t.Fatal("the day was caught up before its job had its tries")
	}
	// Tomorrow's is not over yet, so it is left alone.
	st.clock = st.zones.LastReveal(answered).Add(catchUpAfter + time.Minute)
	if err := st.s.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	if !st.kept(t, answered) {
		t.Fatal("the missed day was not kept")
	}
	if kept := st.kept(t, nobody); kept {
		t.Fatal("a day still open was kept")
	}
	if skipped, _, _ := st.skipped(t, nobody); skipped {
		t.Fatal("a day still open was skipped")
	}

	// A day over everywhere with nobody's answer is skipped by it.
	st.clock = st.zones.LastReveal(nobody).Add(catchUpAfter + time.Minute)
	if err := st.s.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	skipped, status, _ := st.skipped(t, nobody)
	if !skipped || status != "approved" {
		t.Fatalf("skipped %v, place %q", skipped, status)
	}
	unclosed, err := st.store.Unclosed(ctx, nobody)
	if err != nil || len(unclosed) != 0 {
		t.Fatalf("unclosed after the catch-up: %v, %v", unclosed, err)
	}
}

// The final job returns a failed keeping, so its queue tries again; and a
// failed edit of a skipped day's post leaves the day not skipped, for the
// next try.
func TestFinalJobRetriesWhatFailed(t *testing.T) {
	st := newStanding(t, 2)
	ctx := context.Background()
	if err := st.store.Touch(ctx, tg.User{ID: alone, FirstName: "Solo", LanguageCode: "en"}, nil); err != nil {
		t.Fatal(err)
	}
	answered, nobody := st.d, st.d.AddDays(1)
	st.run(t, st.d.At(9, 30, st.kyiv))
	for _, d := range []round.Date{answered, nobody} {
		if _, err := st.store.EnsureRound(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, st.pool, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
	                  VALUES ($1, $2, 'dm', 40.4, -3.7, $3, $4)`, answered.Time(), alone, answered.At(10, 0, st.kyiv), answered.Reveal(st.kyiv))

	// Keeping fails: the job's error says so, and the day is not kept.
	failing := &failingKeep{Store: st.s.Store}
	real := st.s.Store
	st.s.Store = failing
	st.clock = st.zones.LastReveal(answered).Add(worldAfter + time.Minute)
	if err := st.s.worldFinal(ctx, db.Job{Kind: "world", Date: answered}); err == nil {
		t.Fatal("a day that could not be kept ended its job")
	}
	st.s.Store = real
	if st.kept(t, answered) {
		t.Fatal("the day was kept")
	}
	if err := st.s.worldFinal(ctx, db.Job{Kind: "world", Date: answered}); err != nil {
		t.Fatal(err)
	}
	if !st.kept(t, answered) {
		t.Fatal("the retry did not keep the day")
	}

	// The skipped day's post cannot be edited: Telegram is down. The day is
	// still a day, and the retry skips it.
	st.run(t, st.d.AddDays(1).At(9, 30, st.kyiv))
	st.fake.failKind["edit"] = fmt.Errorf("telegram: 502 Bad Gateway")
	st.clock = st.zones.LastReveal(nobody).Add(worldAfter + time.Minute)
	if err := st.s.worldFinal(ctx, db.Job{Kind: "world", Date: nobody}); err == nil {
		t.Fatal("a failed edit ended the job")
	}
	if skipped, _, _ := st.skipped(t, nobody); skipped {
		t.Fatal("the day was skipped with its post unedited")
	}
	delete(st.fake.failKind, "edit")
	if err := st.s.worldFinal(ctx, db.Job{Kind: "world", Date: nobody}); err != nil {
		t.Fatal(err)
	}
	if skipped, _, _ := st.skipped(t, nobody); !skipped {
		t.Fatal("the retry did not skip the day")
	}
}

// failingKeep is a store that cannot keep a day.
type failingKeep struct{ Store }

func (failingKeep) KeepDay(context.Context, round.Date) error { return fmt.Errorf("database is down") }
