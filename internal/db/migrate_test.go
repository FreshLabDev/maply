// SPDX-License-Identifier: Apache-2.0
package db

import (
	"testing"
	"testing/fstest"

	"github.com/FreshLabDev/maply/migrations"
)

func TestListMigrationsOrdersByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"010_later.sql":  {},
		"002_second.sql": {},
		"001_first.sql":  {},
		"README.md":      {},
		"migrations.go":  {},
	}
	got, err := ListMigrations(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 10}
	if len(got) != len(want) {
		t.Fatalf("got %d migrations, want %d: %+v", len(got), len(want), got)
	}
	for i, m := range got {
		if m.Version != want[i] {
			t.Fatalf("migration %d has version %d, want %d", i, m.Version, want[i])
		}
	}
}

func TestListMigrationsRejectsDuplicateVersion(t *testing.T) {
	fsys := fstest.MapFS{"001_a.sql": {}, "1_b.sql": {}}
	if _, err := ListMigrations(fsys); err == nil {
		t.Fatal("duplicate version accepted")
	}
}

func TestEmbeddedMigrationsAreListed(t *testing.T) {
	got, err := ListMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Version != 1 {
		t.Fatalf("embedded migrations = %+v", got)
	}
}
