// SPDX-License-Identifier: Apache-2.0
//go:build integration

// The SQL in this package needs a real PostgreSQL:
//
//	MAPLY_TEST_DATABASE_URL=postgres://… go test -tags=integration ./internal/db/
package db

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FreshLabDev/tg"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/FreshLabDev/maply/migrations"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	rawURL := os.Getenv("MAPLY_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set MAPLY_TEST_DATABASE_URL to run the database tests")
	}
	// These tests truncate what they touch. The production database is shared
	// with the other bots, so refuse anything that is not visibly scratch.
	if name := databaseName(rawURL); !strings.Contains(name, "test") {
		t.Fatalf("MAPLY_TEST_DATABASE_URL must point at a disposable database whose name contains \"test\", got %q", name)
	}
	ctx := context.Background()
	bootstrap, err := os.ReadFile(filepath.Join("..", "..", "deploy", "core-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Connect(ctx, rawURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(store.Close)
	if _, err := store.pool.Exec(ctx, string(bootstrap)); err != nil {
		t.Fatalf("core bootstrap: %v", err)
	}
	if err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reset := func() {
		for _, stmt := range []string{
			`UPDATE maply.runtime_state SET telegram_offset=0`,
			`TRUNCATE maply.job, maply.chat_round, maply.guess, maply.site_guess, maply.site_player, maply.chat_settings, maply.round, maply.place`,
			`TRUNCATE core.person, core.chat CASCADE`,
			`TRUNCATE core.language_observation`,
		} {
			if _, err := store.pool.Exec(ctx, stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
	}
	reset()
	t.Cleanup(reset)
	return store
}

func databaseName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(parsed.Path, "/")
}

func TestMigrateIsRepeatable(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	want, err := ListMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	var applied int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM maply.schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != len(want) {
		t.Fatalf("schema_migrations has %d rows, want %d", applied, len(want))
	}
}

func TestOffsetOnlyMovesForward(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	for _, off := range []int64{10, 7} {
		if err := store.AdvanceOffset(ctx, off); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Offset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10 {
		t.Fatalf("offset = %d, want 10", got)
	}
}

func TestTouchRecordsPersonAndGroup(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	user := tg.User{ID: 42, FirstName: "Ann", LanguageCode: "uk"}
	if err := store.Touch(ctx, user, &tg.Chat{ID: -1001, Type: "supergroup", Title: "Maps"}); err != nil {
		t.Fatal(err)
	}
	// A DM is not a chat in core: the person is the subject.
	if err := store.Touch(ctx, user, &tg.Chat{ID: 42, Type: "private"}); err != nil {
		t.Fatal(err)
	}
	var people, chats int
	if err := store.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.person), (SELECT count(*) FROM core.chat)`).Scan(&people, &chats); err != nil {
		t.Fatal(err)
	}
	if people != 1 || chats != 1 {
		t.Fatalf("core has %d people and %d chats, want 1 and 1", people, chats)
	}
}

// rejects runs stmt and requires PostgreSQL to refuse it with code.
func rejects(t *testing.T, s *Store, code, stmt string, args ...any) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), stmt, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("%s: got %v, want SQLSTATE %s", stmt, err, code)
	}
}

func exec(t *testing.T, s *Store, stmt string, args ...any) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), stmt, args...); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

const (
	uniqueViolation = "23505"
	checkViolation  = "23514"
)

// seedRound makes one used place, the round for 2026-11-01 and a player.
func seedRound(t *testing.T, s *Store) {
	t.Helper()
	if err := s.Touch(context.Background(), tg.User{ID: 7, FirstName: "Pin"}, nil); err != nil {
		t.Fatal(err)
	}
	exec(t, s, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key,
	                                    country_iso, continent, difficulty, reviewed_at)
	            OVERRIDING SYSTEM VALUE
	            VALUES (1, 'used', 'mapillary', 'm1', 'CC-BY-SA-4.0', 'photo', 50.45, 30.52, 'k1', 'UA', 'europe', 2, now())`)
	exec(t, s, `INSERT INTO maply.round (date, day_no, place_id) VALUES ('2026-11-01', 1, 1)`)
}

func TestOneGuessPerPersonPerDay(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	const insert = `INSERT INTO maply.guess (date, user_id, context, chat_id, lat, lon, answered_at, deadline_at)
	                VALUES ('2026-11-01', 7, $1, $2, 48.85, 2.35, '2026-11-01 10:00+03', '2026-11-01 20:00+03')`
	exec(t, store, insert, "dm", nil)
	// The same person from a group is still the same answer.
	rejects(t, store, uniqueViolation, insert, "group", int64(-1001))
}

func TestGuessAfterDeadlineIsRefused(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	rejects(t, store, checkViolation,
		`INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
		 VALUES ('2026-11-01', 7, 'dm', 0, 0, '2026-11-01 20:00:01+03', '2026-11-01 20:00+03')`)
}

func TestApprovedPlaceNeedsWhatARoundUses(t *testing.T) {
	store := testStore(t)
	rejects(t, store, checkViolation,
		`INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon)
		 VALUES ('approved', 'panoramax', 'p1', 'CC-BY-SA-4.0', 'panorama', 1, 1)`)
	exec(t, store,
		`INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon)
		 VALUES ('pending', 'panoramax', 'p1', 'CC-BY-SA-4.0', 'panorama', 1, 1)`)
	rejects(t, store, uniqueViolation,
		`INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon)
		 VALUES ('pending', 'panoramax', 'p1', 'CC-BY-SA-4.0', 'panorama', 2, 2)`)
}

func TestPostCannotMovePastTheReveal(t *testing.T) {
	store := testStore(t)
	rejects(t, store, checkViolation,
		`INSERT INTO maply.chat_settings (chat_id, kind, post_time) VALUES (-1001, 'group', '20:00')`)
	rejects(t, store, checkViolation,
		`INSERT INTO maply.chat_settings (chat_id, kind) VALUES (-1001, 'dm')`)
	exec(t, store, `INSERT INTO maply.chat_settings (chat_id, kind, post_time) VALUES (-1001, 'group', '09:30')`)
}

func TestJobKeyIsIdempotent(t *testing.T) {
	store := testStore(t)
	const insert = `INSERT INTO maply.job (key, kind, chat_id, date, run_at)
	                VALUES ('post:2026-11-01:-1001', 'post', -1001, '2026-11-01', '2026-11-01 07:00+03')`
	exec(t, store, insert)
	rejects(t, store, uniqueViolation, insert)
}
