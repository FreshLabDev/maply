// SPDX-License-Identifier: Apache-2.0
//go:build integration

// The SQL in this package needs a real PostgreSQL:
//
//	MAPLY_TEST_DATABASE_URL=postgres://… go test -tags=integration ./internal/db/
package db

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FreshLabDev/tg"

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
			`TRUNCATE core.person, core.chat CASCADE`,
			`TRUNCATE core.user_language`,
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
