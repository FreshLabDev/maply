// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FreshLabDev/maply/internal/geo"
)

func testPlace(pool string) *place {
	h := 12.5
	return &place{
		c: candidate{Source: "mapillary", SourceID: "1234567890", Lat: 48.8566, Lon: 2.3522, IsPano: true,
			Author: "o'brien, ltd", License: "CC-BY-SA-4.0"},
		key: "k", country: geo.Country{ISO: "FR", Continent: "europe"}, heading: &h, difficulty: 2,
		city: map[string]string{"en": "Paris (l'Île)"}, poly: json.RawMessage(`{"type":"Polygon","coordinates":[]}`),
		pool: pool,
	}
}

func TestInsertPool(t *testing.T) {
	daily := testPlace("daily").insert()
	if strings.Contains(daily, "pool") {
		t.Errorf("a place of the day names the pool column, so its SQL needs the new schema:\n%s", daily)
	}
	free := testPlace("free").insert()
	if !strings.Contains(free, "INSERT INTO maply.place (pool, status,") || !strings.Contains(free, "VALUES ('free', 'approved',") {
		t.Errorf("a free place is not inserted into the free pool:\n%s", free)
	}
}

func TestAvoid(t *testing.T) {
	dir := t.TempDir()
	sqlPath := filepath.Join(dir, "places.sql")
	sql := "BEGIN;\n" + testPlace("daily").insert() + testPlace("free").insert() + "COMMIT;\n"
	if err := os.WriteFile(sqlPath, []byte(sql), 0o644); err != nil {
		t.Fatal(err)
	}
	candPath := filepath.Join(dir, "candidates.json")
	if err := os.WriteFile(candPath, []byte(`[{"n":1,"source":"mapillary","source_id":"777","lat":-33.87,"lon":151.21}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := readAvoid([]string{sqlPath, candPath}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.points) != 3 {
		t.Fatalf("read %d places, want 3", len(a.points))
	}
	for _, tc := range []struct {
		c    candidate
		want bool
	}{
		{candidate{SourceID: "1234567890", Lat: 0, Lon: 0}, true},    // the same frame
		{candidate{SourceID: "1", Lat: 48.90, Lon: 2.40}, true},      // 6 km from it
		{candidate{SourceID: "2", Lat: 49.00, Lon: 2.35}, false},     // 16 km from it
		{candidate{SourceID: "3", Lat: -33.90, Lon: 151.20}, true},   // near a spare frame of a batch
		{candidate{SourceID: "777", Lat: 10, Lon: 10}, true},         // that frame itself
		{candidate{SourceID: "4", Lat: -33.87, Lon: -151.21}, false}, // across the world
	} {
		if got := a.why(tc.c) != ""; got != tc.want {
			t.Errorf("why(%+v) = %q, want refused %v", tc.c, a.why(tc.c), tc.want)
		}
	}
	var none *avoidList
	if none.why(candidate{}) != "" {
		t.Error("no -avoid refuses a candidate")
	}
}

func TestSQLTuple(t *testing.T) {
	got, err := sqlTuple(`'a,b', 'it''s', 1.5, NULL, '{"x":"(y)"}'::jsonb, now())
ON CONFLICT`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a,b", "it's", "1.5", "NULL", `{"x":"(y)"}::jsonb`, "now()"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := sqlTuple(`'never closed`); err == nil {
		t.Error("an unterminated tuple parsed")
	}
}

func TestOneName(t *testing.T) {
	if got := oneName(map[string]string{"en": "Kyiv", "uk": "Київ", "ru": "Киев"}); len(got) != 1 || got["en"] != "Kyiv" {
		t.Errorf("oneName = %v", got)
	}
	if got := oneName(map[string]string{"ru": "Киев"}); got != nil {
		t.Errorf("oneName without an English or local name = %v, want nil", got)
	}
}
