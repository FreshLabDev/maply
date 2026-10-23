// SPDX-License-Identifier: Apache-2.0
//go:build integration

// A whole day on a fast clock against a real PostgreSQL:
//
//	MAPLY_TEST_DATABASE_URL=postgres://… go test -tags=integration ./internal/schedule/
package schedule

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
	"github.com/FreshLabDev/maply/migrations"
)

type sent struct {
	kind    string // photo, text, edit, pin, unpin
	chatID  int64
	text    string
	fileID  string
	upload  bool
	buttons telegram.Markup
}

type fakeTG struct {
	mu   sync.Mutex
	next int64
	log  []sent
	// fail makes the next send to the chat fail with this error.
	fail map[int64]error
}

func (f *fakeTG) add(s sent) (tg.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[s.chatID]; err != nil {
		delete(f.fail, s.chatID)
		return tg.Message{}, err
	}
	f.next++
	f.log = append(f.log, s)
	msg := tg.Message{MessageID: f.next}
	if s.kind == "photo" {
		msg.Photo = []tg.Photo{{FileID: "small"}, {FileID: "file-1"}}
	}
	return msg, nil
}

func (f *fakeTG) SendPhoto(_ context.Context, chatID int64, p telegram.Photo, caption string, m telegram.Markup) (tg.Message, error) {
	return f.add(sent{kind: "photo", chatID: chatID, text: caption, fileID: p.FileID, upload: p.Path != "", buttons: m})
}

func (f *fakeTG) SendAnimation(_ context.Context, chatID int64, a telegram.Animation, caption string, m telegram.Markup) (tg.Message, error) {
	return f.add(sent{kind: "animation", chatID: chatID, text: caption, fileID: a.FileID, upload: a.MP4 != nil, buttons: m})
}

func (f *fakeTG) EditCaption(_ context.Context, chatID, _ int64, caption string, m telegram.Markup) error {
	_, err := f.add(sent{kind: "edit", chatID: chatID, text: caption, buttons: m})
	return err
}

func (f *fakeTG) SendText(_ context.Context, chatID int64, text string, m telegram.Markup) (tg.Message, error) {
	return f.add(sent{kind: "text", chatID: chatID, text: text, buttons: m})
}

func (f *fakeTG) Pin(_ context.Context, chatID, _ int64) error {
	_, err := f.add(sent{kind: "pin", chatID: chatID})
	return err
}

func (f *fakeTG) Unpin(_ context.Context, chatID, _ int64) error {
	_, err := f.add(sent{kind: "unpin", chatID: chatID})
	return err
}

func (f *fakeTG) count(kind string, chatID int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.log {
		if s.kind == kind && s.chatID == chatID {
			n++
		}
	}
	return n
}

func (f *fakeTG) last(kind string, chatID int64) sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.log) - 1; i >= 0; i-- {
		if s := f.log[i]; s.kind == kind && s.chatID == chatID {
			return s
		}
	}
	return sent{}
}

type fakeWeather struct{}

func (fakeWeather) Now(context.Context, geo.Point) (time.Duration, *float64, error) {
	t := -3.4
	return 2 * time.Hour, &t, nil
}

func testDB(t *testing.T) (*db.Store, *pgxpool.Pool) {
	t.Helper()
	rawURL := os.Getenv("MAPLY_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set MAPLY_TEST_DATABASE_URL to run the database tests")
	}
	if u, err := url.Parse(rawURL); err != nil || !strings.Contains(u.Path, "test") {
		t.Fatalf("MAPLY_TEST_DATABASE_URL must name a disposable database containing \"test\"")
	}
	ctx := context.Background()
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
	store, err := db.Connect(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		for _, stmt := range []string{
			`TRUNCATE maply.job, maply.chat_round, maply.guess, maply.site_guess, maply.site_player, maply.chat_settings, maply.round, maply.place`,
			`TRUNCATE core.person, core.chat CASCADE`,
		} {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
	}
	reset()
	t.Cleanup(reset)
	return store, pool
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

const (
	group  = int64(-1005001)
	player = int64(7) // plays in the group and in a DM
	quiet  = int64(8) // in the group, never answers
	alone  = int64(9) // only a DM
)

// TestOneDay runs 05:00 to 21:00 Kyiv time in ten-minute steps, restarts the
// scheduler at noon, and checks that every chat got each message once.
func TestOneDay(t *testing.T) {
	store, pool := testDB(t)
	ctx := context.Background()
	images := t.TempDir()
	if err := os.WriteFile(filepath.Join(images, "k1"), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon, image_key,
	                                       country_iso, continent, city, fact, difficulty, reviewed_at)
	               VALUES ('approved', 'mapillary', 'm1', 'CC-BY-SA-4.0', 'photo', 38.72, -9.14, 'k1', 'PT', 'europe',
	                       '{"en":"Lisbon","ru":"Лиссабон"}', '{"en":"Built on seven hills."}', 2, now())`)
	chat := &tg.Chat{ID: group, Type: "supergroup", Title: "Maps"}
	for _, u := range []tg.User{{ID: player, FirstName: "Pin"}, {ID: quiet, FirstName: "Q<b>"}} {
		if err := store.Touch(ctx, u, chat); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []tg.User{{ID: player, FirstName: "Pin"}, {ID: alone, FirstName: "Solo"}} {
		if err := store.Touch(ctx, u, nil); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, pool, `INSERT INTO maply.chat_settings (chat_id, kind, timezone) VALUES ($1, 'group', 'Europe/Kyiv')`, group)
	exec(t, pool, `INSERT INTO maply.chat_settings (chat_id, kind, timezone) VALUES ($1, 'dm', 'Europe/Kyiv'), ($2, 'dm', 'Europe/Kyiv')`, player, alone)

	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	countries, err := geo.World()
	if err != nil {
		t.Fatal(err)
	}
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	clock := time.Date(2026, 11, 2, 5, 0, 0, 0, kyiv)
	fake := &fakeTG{fail: map[int64]error{}}
	newScheduler := func() *Scheduler {
		return &Scheduler{
			Store: store, TG: fake, Weather: fakeWeather{}, Payloads: auth.NewSigner("123:abc"),
			Zones: zones, Countries: countries, ImagesDir: images, Username: "maplyin_bot",
			AppURL: "https://maply.example/", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Now: func() time.Time { return clock },
		}
	}
	s := newScheduler()
	d := round.DateIn(clock, kyiv)
	// The first post to the lone player fails once; the retry sends it.
	fake.fail[alone] = fmt.Errorf("telegram: 502 Bad Gateway")

	for ; clock.Hour() < 21; clock = clock.Add(10 * time.Minute) {
		if clock.Hour() == 10 && clock.Minute() == 0 {
			exec(t, pool, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
			               VALUES ($1, $2, 'dm', 40.4, -3.7, $3, $4)`, d.Time(), player, clock, d.Reveal(kyiv))
		}
		if clock.Hour() == 12 && clock.Minute() == 0 {
			s = newScheduler() // a restart forgets what it planned
		}
		s.Step(ctx)
		// A second instance would also step; the queue keeps it from repeating.
		if clock.Minute() == 30 {
			newScheduler().Step(ctx)
		}
	}

	for _, c := range []struct {
		name   string
		kind   string
		chatID int64
		want   int
	}{
		{"group post", "photo", group, 2}, // the post and the result
		{"group pin", "pin", group, 1},
		{"group unpin", "unpin", group, 1},
		{"group intrigue", "text", group, 1},
		{"group redraw", "edit", group, 1},
		{"player post", "photo", player, 1},
		{"player intrigue and own result", "text", player, 1},
		{"lone post and result", "photo", alone, 2},
		{"lone intrigue", "text", alone, 1},
		{"quiet gets no DM", "photo", quiet, 0},
	} {
		if got := fake.count(c.kind, c.chatID); got != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, got, c.kind, c.want)
		}
	}
	uploads := 0
	for _, m := range fake.log {
		if m.upload {
			uploads++
		}
	}
	if uploads != 1 {
		t.Errorf("photo uploaded %d times, want once and then by file id", uploads)
	}
	post := fake.log[0]
	if !strings.Contains(post.text, "maply #1") {
		t.Errorf("post caption = %q", post.text)
	}
	if edit := fake.last("edit", group); !strings.Contains(edit.text, "Pin") {
		t.Errorf("redraw = %q, want the player named", edit.text)
	}
	if b := fake.last("photo", group).buttons; len(b) != 1 || !strings.HasPrefix(b[0][0].URL, "https://t.me/maplyin_bot/play?startapp=") {
		t.Errorf("group button = %+v, want the signed link", b)
	}
	if b := fake.last("photo", alone).buttons; len(b) != 1 || b[0][0].WebApp != "https://maply.example/" {
		t.Errorf("DM button = %+v, want the Mini App", b)
	}
	result := fake.last("photo", group).text
	for _, want := range []string{"Lisbon", "Portugal", "Built on seven hills.", "1. Pin"} {
		if !strings.Contains(result, want) {
			t.Errorf("group result %q lacks %q", result, want)
		}
	}
	if own := fake.last("text", player).text; !strings.Contains(own, "km") {
		t.Errorf("player's own result = %q", own)
	}
	if intr := fake.last("text", group).text; !strings.Contains(intr, "−3") && !strings.Contains(intr, "-3") {
		t.Errorf("group intrigue = %q, want the weather", intr)
	}
	var open int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM maply.job WHERE status NOT IN ('done', 'skipped')`).Scan(&open); err != nil || open != 0 {
		t.Errorf("%d jobs left open (%v)", open, err)
	}
}

// A chat that blocked maply is left, not retried.
func TestBlockedChatIsLeft(t *testing.T) {
	store, pool := testDB(t)
	ctx := context.Background()
	images := t.TempDir()
	if err := os.WriteFile(filepath.Join(images, "k1"), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon, image_key,
	                                       country_iso, continent, difficulty, reviewed_at)
	               VALUES ('approved', 'mapillary', 'm1', 'CC-BY-SA-4.0', 'photo', 38.72, -9.14, 'k1', 'PT', 'europe', 2, now())`)
	if err := store.Touch(ctx, tg.User{ID: alone, FirstName: "Solo"}, nil); err != nil {
		t.Fatal(err)
	}
	zones, _ := round.LoadZones()
	countries, _ := geo.World()
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	clock := time.Date(2026, 11, 2, 9, 0, 0, 0, kyiv)
	fake := &fakeTG{fail: map[int64]error{alone: &tg.APIError{ErrorCode: 403, Description: "Forbidden: bot was blocked by the user"}}}
	s := &Scheduler{Store: store, TG: fake, Payloads: auth.NewSigner("123:abc"), Zones: zones, Countries: countries,
		ImagesDir: images, Username: "maplyin_bot", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return clock }}
	s.Step(ctx)
	clock = clock.Add(time.Minute)
	s.Step(ctx)
	if n := len(fake.log); n != 0 {
		t.Fatalf("sent %d messages to a chat that blocked maply", n)
	}
	chats, err := store.ActiveChats(ctx)
	if err != nil || len(chats) != 0 {
		t.Fatalf("active chats after the block = %+v, %v", chats, err)
	}
}
