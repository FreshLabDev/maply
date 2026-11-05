// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
)

const stripKey = "Qw7rT2yU9iO4pA6sD1fG"

// A day open in any zone says on the days page how many have answered and
// when it closes, and nothing about its place: no name, no numbers, no
// strip, even one kept by hand. Its page answers 403. Once the day is over
// everywhere, both show it, and the strip once it is drawn.
func TestDaysShowAPlaceOnlyOnceItIsOverEverywhere(t *testing.T) {
	hs := setup(t)
	ctx := context.Background()
	store := hs.srv.Store.(*db.Store)
	oksana := initData(1, "Oksana", "uk", "")
	hs.expect("GET", "/api/today", oksana, "", http.StatusOK)
	hs.expect("POST", "/api/guess", oksana, `{"lat":50.45,"lon":30.52}`, http.StatusOK)

	type daysBody struct {
		Days []dayView `json:"days"`
		Next string    `json:"next"`
		Best string    `json:"best"`
	}
	list := func(path, auth string) (daysBody, string) {
		t.Helper()
		body := hs.expect("GET", path, auth, "", http.StatusOK)
		var out daysBody
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
		return out, body
	}
	open := func() {
		t.Helper()
		got, body := list("/api/days", oksana)
		if leaks(body) || strings.Contains(body, stripKey) || strings.Contains(body, "Oksana") {
			t.Fatalf("an open day leaks: %s", body)
		}
		if len(got.Days) != 1 || got.Next != "" || got.Best != "" {
			t.Fatalf("days = %s", body)
		}
		d := got.Days[0]
		latest := time.Date(2026, time.December, 11, 4, 0, 0, 0, time.UTC).Format(time.RFC3339)
		if d.Final || d.Day != 1 || d.OpensAt != latest || d.Pending != 1 || !d.Played || d.Image != "/img/"+testImageKey+".jpg" || d.Kind != "panorama" {
			t.Fatalf("open day = %+v", d)
		}
		for _, path := range []string{"/api/day?n=1", "/api/day?date=2026-12-10"} {
			body := hs.expect("GET", path, oksana, "", http.StatusForbidden)
			if leaks(body) || strings.Contains(body, stripKey) || !strings.Contains(body, `"opens_at"`) {
				t.Fatalf("%s before the close = %s", path, body)
			}
		}
	}
	open()
	// Kept and drawn by hand before its time, the day still shows nothing.
	if _, err := hs.pool.Exec(ctx, `INSERT INTO maply.day_closed (date) VALUES ('2026-12-10')`); err != nil {
		t.Fatal(err)
	}
	if err := store.PutThumb(ctx, db.Thumb{Date: hs.date, Key: stripKey, Recipe: 1, Small: []byte("small"), Wide: []byte("wide")}); err != nil {
		t.Fatal(err)
	}
	open()
	if _, err := hs.pool.Exec(ctx, `DELETE FROM maply.day_closed`); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/days?n=0", "/api/days?n=61", "/api/days?before=yesterday", "/api/day?n=0", "/api/day?n=3000000000", "/api/day?n=1000000", "/api/day?date=soon"} {
		hs.expect("GET", path, oksana, "", http.StatusBadRequest)
	}
	hs.expect("GET", "/api/day?n=2", oksana, "", http.StatusNotFound)

	// Over everywhere, not kept yet: the place and the numbers, no strip.
	hs.now = time.Date(2026, time.December, 11, 4, 0, 0, 0, time.UTC)
	got, body := list("/api/days", oksana)
	if d := got.Days[0]; !d.Final || d.City != "Львів" || d.Country != "UA" || d.Thumb != "" || d.Image != "" || d.Me == nil || d.Me.Rank != 1 || d.Players != 1 {
		t.Fatalf("closed day = %s", body)
	}
	var page dayResponse
	if err := json.Unmarshal([]byte(hs.expect("GET", "/api/day?n=1", oksana, "", http.StatusOK)), &page); err != nil {
		t.Fatal(err)
	}
	if page.Day != 1 || page.Date != "2026-12-10" || page.Place.City != "Львів" || page.Image != "/img/"+testImageKey+".jpg" || page.Kind != "panorama" ||
		page.Heading == nil || *page.Heading != 164.5 || page.Thumb != "" || len(page.Pins) != 1 || len(page.Misses) != 1 ||
		page.BestKm == nil || page.Prev != 0 || page.Next != 0 {
		t.Fatalf("day page = %+v", page)
	}
	if page.Me == nil || page.Me.Rank != 1 || page.Me.Pin == nil || page.Me.Pin.Lat != 50.45 || len(page.Ranking) != 1 || !page.Ranking[0].Me {
		t.Fatalf("Oksana's part = %+v, %+v", page.Me, page.Ranking)
	}

	// Kept and drawn: the strip's paths, and the bytes under them.
	if err := store.KeepDay(ctx, hs.date); err != nil {
		t.Fatal(err)
	}
	if err := store.PutThumb(ctx, db.Thumb{Date: hs.date, Key: stripKey, Recipe: 1, Small: []byte("small"), Wide: []byte("wide")}); err != nil {
		t.Fatal(err)
	}
	if got, body = list("/api/days", oksana); got.Days[0].Thumb != "/thumb/"+stripKey+".jpg" || got.Best != "2026-12-10" {
		t.Fatalf("kept day = %s", body)
	}
	// A page read before the strip was drawn is kept for closedSoon only, so
	// the strip shows soon after.
	if err := json.Unmarshal([]byte(hs.expect("GET", "/api/day?date=2026-12-10", oksana, "", http.StatusOK)), &page); err != nil || page.Thumb != "" {
		t.Fatalf("day page within closedSoon = %q, %v", page.Thumb, err)
	}
	hs.now = hs.now.Add(closedSoon)
	if err := json.Unmarshal([]byte(hs.expect("GET", "/api/day?date=2026-12-10", oksana, "", http.StatusOK)), &page); err != nil || page.Thumb != "/thumb/"+stripKey+".w.jpg" {
		t.Fatalf("day page thumb = %q, %v", page.Thumb, err)
	}
	for path, want := range map[string]string{"/thumb/" + stripKey + ".jpg": "small", "/thumb/" + stripKey + ".w.jpg": "wide"} {
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != http.StatusOK || rr.Body.String() != want || rr.Header().Get("Content-Type") != "image/jpeg" ||
			!strings.Contains(rr.Header().Get("Cache-Control"), "immutable") || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s = %d %q %v", path, rr.Code, rr.Body, rr.Header())
		}
	}
	for _, path := range []string{"/thumb/AAAAAAAAAAAAAAAAAAAA.jpg", "/thumb/" + stripKey, "/thumb/" + stripKey + ".png", "/thumb/" + stripKey + ".jpg.jpg", "/thumb/short.jpg", "/thumb/..%2F" + stripKey + ".jpg", "/thumb/" + testImageKey + "AAAA.jpg"} {
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, rr.Code)
		}
	}
}

// The days page shows an open day's puzzle only where the day has begun: on
// the site that is the browser's zone, so Kyiv does not see the picture
// Kiritimati plays already.
func TestDaysShowAPuzzleOnlyWhereItsDayBegan(t *testing.T) {
	hs := setup(t)
	if _, err := hs.pool.Exec(context.Background(), `
		INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon, image_key,
		                         country_iso, continent, difficulty, reviewed_at)
		VALUES ('used', 'mapillary', '5550001', 'CC-BY-SA-4.0', 'panorama', 1, 2, 'Nx8bV3cZ5mQ1wE7rT9yU', 'UA', 'europe', 2, now());
		INSERT INTO maply.round (date, day_no, place_id)
		SELECT '2026-12-10'::date, 1, id FROM maply.place WHERE source_id = '918273645'
		UNION ALL SELECT '2026-12-11'::date, 2, id FROM maply.place WHERE source_id = '5550001'`); err != nil {
		t.Fatal(err)
	}
	type daysBody struct {
		Days []dayView `json:"days"`
	}
	list := func(tz string) daysBody {
		t.Helper()
		var got daysBody
		body := hs.expect("GET", "/api/days?tz="+tz, "", "", http.StatusOK)
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	// 11:00 UTC: the 11th on Kiritimati, but no zone maply offers has begun
	// it, so it is not listed at all.
	hs.now = time.Date(2026, time.December, 10, 11, 0, 0, 0, time.UTC)
	if got := list("Pacific/Kiritimati"); len(got.Days) != 1 || got.Days[0].Date != "2026-12-10" {
		t.Fatalf("Kiritimati at 11:00 = %+v", got.Days)
	}
	// 14:00 UTC: the 11th in Sydney, the 10th in Kyiv.
	hs.now = time.Date(2026, time.December, 10, 14, 0, 0, 0, time.UTC)
	for tz, want := range map[string]string{"Europe/Kyiv": "", "Australia/Sydney": "/img/Nx8bV3cZ5mQ1wE7rT9yU.jpg", "Pacific/Kiritimati": "/img/Nx8bV3cZ5mQ1wE7rT9yU.jpg"} {
		got := list(tz)
		if len(got.Days) != 2 || got.Days[0].Final || got.Days[1].Final || got.Days[0].Image != want || got.Days[1].Image != "/img/"+testImageKey+".jpg" {
			t.Fatalf("%s at 14:00 = %+v", tz, got.Days)
		}
	}
}
