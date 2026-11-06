//go:build integration

// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The world's table names a player at the viewer's reveal once every group
// the player is in has had its own: a DM on Moscow time sees Ivan of the
// Moscow group at 17:00 UTC, and Taras of the Kyiv group only at 18:00, when
// the Kyiv group has seen its own result.
func TestWorldNamesOpenWithTheirGroups(t *testing.T) {
	hs := setup(t)
	hs.now = time.Date(2026, time.December, 10, 10, 0, 0, 0, time.UTC)
	hs.srv.Members.(members)[[2]int64{moscowGroup, 2}] = true
	hs.srv.Members.(members)[[2]int64{kyivGroup, 8}] = true
	ivan, taras := initData(2, "Ivan", "ru", hs.payload(moscowGroup)), initData(8, "Taras", "uk", hs.payload(kyivGroup))
	dmitry := initData(5, "Dmitry", "ru", "")
	for _, who := range []string{ivan, taras, dmitry} {
		hs.expect("GET", "/api/today", who, "", http.StatusOK)
	}
	hs.expect("POST", "/api/guess", ivan, `{"lat":55.75,"lon":37.6}`, http.StatusOK)
	hs.expect("POST", "/api/guess", taras, `{"lat":50.45,"lon":30.5}`, http.StatusOK)
	hs.expect("POST", "/api/guess", dmitry, `{"lat":49,"lon":24}`, http.StatusOK)

	board := func() (string, map[string]bool) {
		t.Helper()
		var res resultResponse
		if err := json.Unmarshal([]byte(hs.expect("GET", "/api/result", dmitry, "", http.StatusOK)), &res); err != nil {
			t.Fatal(err)
		}
		named := map[string]bool{}
		hidden := 0
		for _, l := range res.Board {
			if l.Hidden {
				hidden++
			} else {
				named[l.Name] = true
			}
		}
		if len(named)+hidden != 3 {
			t.Fatalf("board = %+v", res.Board)
		}
		return res.Version, named
	}

	// 16:30 UTC: before his own reveal, nobody else has a name.
	hs.now = time.Date(2026, time.December, 10, 16, 30, 0, 0, time.UTC)
	if _, named := board(); named["Ivan"] || named["Taras"] || !named["Dmitry"] {
		t.Fatalf("before his reveal: %v", named)
	}
	// 17:30 UTC: past 20:00 in Moscow, Ivan is named; Taras's Kyiv group
	// is still guessing.
	hs.now = time.Date(2026, time.December, 10, 17, 30, 0, 0, time.UTC)
	v1, named := board()
	if !named["Ivan"] || named["Taras"] {
		t.Fatalf("after Moscow's reveal: %v", named)
	}
	// 18:00 UTC: the Kyiv group's reveal names Taras, and the version moves
	// so the open app draws the table again.
	hs.now = time.Date(2026, time.December, 10, 18, 0, 0, 0, time.UTC)
	v2, named := board()
	if !named["Ivan"] || !named["Taras"] || v2 == v1 {
		t.Fatalf("after Kyiv's reveal: %v, version %s then %s", named, v1, v2)
	}
	open, err := hs.srv.Store.NamedOn(context.Background(), hs.srv.Zones, hs.date, []int64{2, 8, 5}, time.Date(2026, time.December, 10, 17, 30, 0, 0, time.UTC))
	if err != nil || !open[2] || open[8] || !open[5] {
		t.Fatalf("NamedOn at 17:30 = %v, %v", open, err)
	}
}

// The own zone is the DM's, shown and set in the profile's settings, with
// the bot's picker's refusals.
func TestOwnZoneInTheProfile(t *testing.T) {
	hs := setup(t)
	oksana := initData(1, "Oksana", "uk", "")
	hs.now = time.Date(2026, time.December, 10, 10, 0, 0, 0, time.UTC)
	settings := func(body string) profileSetting {
		t.Helper()
		var set profileSetting
		if err := json.Unmarshal([]byte(body), &set); err != nil {
			t.Fatal(err)
		}
		return set
	}
	var own ownProfile
	if err := json.Unmarshal([]byte(hs.expect("GET", "/api/profile", oksana, "", http.StatusOK)), &own); err != nil {
		t.Fatal(err)
	}
	if own.Settings == nil || own.Settings.Zone != "Europe/Kyiv" || len(own.Settings.Zones) != 16 {
		t.Fatalf("settings = %+v", own.Settings)
	}
	if set := settings(hs.expect("POST", "/api/profile/settings", oksana, `{"zone":"Europe/Berlin"}`, http.StatusOK)); set.Zone != "Europe/Berlin" {
		t.Fatalf("after Berlin = %+v", set)
	}
	if zone, err := hs.srv.Store.ChatZone(context.Background(), 1); err != nil || zone != "Europe/Berlin" {
		t.Fatalf("DM zone = %q, %v", zone, err)
	}
	hs.expect("POST", "/api/profile/settings", oksana, `{"zone":"Pacific/Honolulu"}`, http.StatusBadRequest)
	hs.expect("POST", "/api/profile/settings", oksana, `{"zone":"Europe/Kyiv","hide_me":true}`, http.StatusBadRequest)
	// 19:30 UTC: past 20:00 in Berlin; New York is still on the day she has
	// seen, Tokyo on tomorrow before her result.
	hs.now = time.Date(2026, time.December, 10, 19, 30, 0, 0, time.UTC)
	if body := hs.expect("POST", "/api/profile/settings", oksana, `{"zone":"America/New_York"}`, http.StatusConflict); body != `{"error":"zone_tomorrow"}`+"\n" {
		t.Fatalf("west after the reveal = %s", body)
	}
	hs.now = time.Date(2026, time.December, 10, 16, 0, 0, 0, time.UTC)
	if body := hs.expect("POST", "/api/profile/settings", oksana, `{"zone":"Asia/Tokyo"}`, http.StatusConflict); body != `{"error":"zone_after_result"}`+"\n" {
		t.Fatalf("a date ahead before the result = %s", body)
	}
}
