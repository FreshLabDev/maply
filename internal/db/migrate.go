// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

type Migration struct {
	Version int
	Name    string
}

var migrationFilePattern = regexp.MustCompile(`^(\d+)_.+\.sql$`)

// migrationLockKey serializes migrations across instances. Any constant works
// as long as it is stable across builds and differs from the other bots on the
// same server.
const migrationLockKey int64 = 0x6d61706c79 // "maply"

// Migrate applies every migration in fsys that schema_migrations has not seen,
// in version order, each in its own transaction.
func (s *Store) Migrate(ctx context.Context, fsys fs.FS) error {
	migrations, err := ListMigrations(fsys)
	if err != nil {
		return err
	}

	// A session-level lock on a dedicated connection: during a rolling restart
	// the second process waits for the first to finish, then finds every
	// migration applied.
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration lock connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		// Releasing the connection drops the lock anyway; the background
		// context lets the unlock run when ctx is already canceled.
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version integer PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	for _, m := range migrations {
		var applied bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, m.Version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", m.Name, err)
		}
		if applied {
			continue
		}
		sql, err := fs.ReadFile(fsys, m.Name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.Name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.Name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", m.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version,name) VALUES($1,$2)`, m.Version, m.Name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", m.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.Name, err)
		}
	}
	return nil
}

// ListMigrations returns the migrations in fsys in version order. Two files
// with the same version are an error: only one of them would ever be applied.
func ListMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var out []Migration
	seen := map[int]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationFilePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("parse migration version %s: %w", entry.Name(), err)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations %s and %s share version %d", prev, entry.Name(), version)
		}
		seen[version] = entry.Name()
		out = append(out, Migration{Version: version, Name: entry.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}
