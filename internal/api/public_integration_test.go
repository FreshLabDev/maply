// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type stillsSeen struct {
	users []int64
	langs []string
}

func (s *stillsSeen) Still(_ context.Context, userID int64, lang string) ([]byte, error) {
	s.users = append(s.users, userID)
	s.langs = append(s.langs, lang)
	return []byte("\x89PNG"), nil
}

// Kept is a picture drawn before, as profile.Service keeps them.
func (s *stillsSeen) Kept(userID int64, lang string) []byte {
	for i, u := range s.users {
		if u == userID && s.langs[i] == lang {
			return []byte("\x89PNG")
		}
	}
	return nil
}

type publicView struct {
	Name   string `json:"name"`
	Ranked struct {
		Days       int  `json:"days"`
		Points     int  `json:"points"`
		Percentile *int `json:"percentile"`
	} `json:"ranked"`
	Error string `json:"error"`
}

// A public page opens for anyone with its link, holds only the days over
// everywhere and nothing that finds the person, and a new link or turning
// it off ends the old one.
func TestPublicPage(t *testing.T) {
	hs := setup(t)
	stills := &stillsSeen{}
	hs.srv.Stills = stills
	hs.person(1, "Oksana", kyivGroup, moscowGroup)
	hs.person(2, "Ivan", moscowGroup)
	hs.seedPast(2, "EG", "africa", pastAnswer{user: 1, km: 400, points: 2500, bonus: 300}, pastAnswer{user: 2, km: 100, points: 3800, bonus: 800})
	hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500}, pastAnswer{user: 2, km: 2000, points: 900})
	oksana := initData(1, "Oksana", "uk", "")
	hs.expect("GET", "/api/today", oksana, "", http.StatusOK)
	hs.expect("POST", "/api/guess", oksana, `{"lat":40,"lon":-3}`, http.StatusOK)

	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		return rr
	}
	if rr := get("/api/p/AAAAAAAAAAAAAAAAAAAAAA"); rr.Code != http.StatusNotFound || rr.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("a page nobody turned on = %d %v", rr.Code, rr.Header())
	}
	// Only a Telegram player has a page to turn on, and the body says what.
	hs.expect("POST", "/api/profile/public", "", `{"on":true}`, http.StatusUnauthorized)
	hs.expect("POST", "/api/profile/public", oksana, `{}`, http.StatusBadRequest)
	hs.expect("POST", "/api/profile/public", oksana, `{"on":"yes"}`, http.StatusBadRequest)

	on := decode[ownView](t, `{"settings":`+hs.expect("POST", "/api/profile/public", oksana, `{"on":true}`, http.StatusOK)+`}`)
	slug, ok := strings.CutPrefix(on.Settings.Public, "/p/")
	if !ok || !slugForm.MatchString(slug) {
		t.Fatalf("public = %q", on.Settings.Public)
	}
	if own := decode[ownView](t, hs.expect("GET", "/api/profile", oksana, "", http.StatusOK)); own.Settings.Public != on.Settings.Public {
		t.Fatalf("the profile's settings say %q", own.Settings.Public)
	}

	rr := get("/api/p/" + slug)
	page := decode[publicView](t, rr.Body.String())
	// Her answer of today is not there: the page counts the days over
	// everywhere, as a stranger's view of a profile does.
	if rr.Code != http.StatusOK || rr.Header().Get("X-Robots-Tag") != "noindex, nofollow" ||
		page.Name != "Oksana" || page.Ranked.Days != 2 || page.Ranked.Points != 2500+4950 || page.Ranked.Percentile != nil {
		t.Fatalf("page = %d %+v", rr.Code, page)
	}
	for _, leak := range []string{`"history"`, `"lat"`, hs.date.String(), `"who"`, `"id"`, "telegram", "username", "photo", "answered_at"} {
		if strings.Contains(rr.Body.String(), leak) {
			t.Fatalf("the public page carries %s: %s", leak, rr.Body.String())
		}
	}
	// The picture is in the owner's language, whatever the fetch asks: one
	// picture per player for the edge to keep. A query would be a new key
	// at the edge, so it goes back to the address without one.
	if rr := get("/api/p/" + slug + "/card.png?lang=en"); rr.Code != http.StatusMovedPermanently ||
		rr.Header().Get("Location") != "/api/p/"+slug+"/card.png" || len(stills.users) != 0 {
		t.Fatalf("card with a query = %d %v", rr.Code, rr.Header())
	}
	card := get("/api/p/" + slug + "/card.png")
	if card.Code != http.StatusOK || card.Header().Get("Content-Type") != "image/png" || card.Header().Get("X-Robots-Tag") != "noindex, nofollow" ||
		len(stills.users) != 1 || stills.users[0] != 1 || stills.langs[0] != "uk" {
		t.Fatalf("card = %d %v, drawn for %v in %v", card.Code, card.Header(), stills.users, stills.langs)
	}
	// Once drawn it is served to any number of fetches: only a drawing
	// counts against the limit from everyone.
	for i := range 70 {
		req := httptest.NewRequest("GET", "/api/p/"+slug+"/card.png", nil)
		req.Header.Set("CF-Connecting-IP", "203.0.113."+strconv.Itoa(i+1))
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("fetch %d of a drawn card = %d", i+1, rr.Code)
		}
	}
	if len(stills.users) != 1 {
		t.Fatalf("a drawn card was drawn %d times", len(stills.users))
	}
	// The slug is the page's key: it stays out of the log.
	if strings.Contains(hs.logs.String(), slug) {
		t.Fatalf("the log names the slug: %s", hs.logs.String())
	}

	// A new link ends the old one.
	renewed := decode[ownView](t, `{"settings":`+hs.expect("POST", "/api/profile/public", oksana, `{"on":true}`, http.StatusOK)+`}`)
	next := strings.TrimPrefix(renewed.Settings.Public, "/p/")
	if next == slug || !slugForm.MatchString(next) {
		t.Fatalf("renewed %q after %q", next, slug)
	}
	if rr := get("/api/p/" + slug); rr.Code != http.StatusNotFound || decode[publicView](t, rr.Body.String()).Error != "not_found" {
		t.Fatalf("the old link = %d %s", rr.Code, rr.Body.String())
	}
	if rr := get("/api/p/" + slug + "/card.png"); rr.Code != http.StatusNotFound || len(stills.users) != 1 {
		t.Fatalf("the old link's card = %d", rr.Code)
	}
	if rr := get("/api/p/" + next); rr.Code != http.StatusOK {
		t.Fatalf("the new link = %d", rr.Code)
	}
	// "Hide me" closes the page, and showing again opens it at the same link.
	hs.expect("POST", "/api/profile/settings", oksana, `{"hide_me":true}`, http.StatusOK)
	if rr := get("/api/p/" + next); rr.Code != http.StatusNotFound || decode[publicView](t, rr.Body.String()).Error != "not_found" {
		t.Fatalf("a hidden player's page = %d %s", rr.Code, rr.Body.String())
	}
	if rr := get("/api/p/" + next + "/card.png"); rr.Code != http.StatusNotFound || len(stills.users) != 1 {
		t.Fatalf("a hidden player's card = %d", rr.Code)
	}
	hs.expect("POST", "/api/profile/settings", oksana, `{"hide_me":false}`, http.StatusOK)
	if rr := get("/api/p/" + next); rr.Code != http.StatusOK || decode[publicView](t, rr.Body.String()).Name != "Oksana" {
		t.Fatalf("the page after showing again = %d", rr.Code)
	}
	// Off, and nothing answers.
	off := decode[ownView](t, `{"settings":`+hs.expect("POST", "/api/profile/public", oksana, `{"on":false}`, http.StatusOK)+`}`)
	if off.Settings.Public != "" {
		t.Fatalf("off = %+v", off.Settings)
	}
	if rr := get("/api/p/" + next); rr.Code != http.StatusNotFound {
		t.Fatalf("a link turned off = %d", rr.Code)
	}
}

// The season in the profile is the month's days over everywhere: the
// world's, and the group's the profile was opened from.
func TestProfileSeason(t *testing.T) {
	hs := setup(t)
	hs.person(1, "Oksana", kyivGroup, moscowGroup)
	hs.person(2, "Ivan", moscowGroup)
	hs.person(3, "Alesia", minskGroup)
	hs.seedPast(2, "EG", "africa", pastAnswer{user: 1, km: 400, points: 2500, bonus: 300}, pastAnswer{user: 2, km: 100, points: 3800, bonus: 800},
		pastAnswer{user: 3, km: 10, points: 4900, bonus: 1500})
	hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500}, pastAnswer{user: 2, km: 2000, points: 900},
		pastAnswer{user: 3, km: 10, points: 4900, bonus: 1500})
	type place struct {
		Kind   string `json:"kind"`
		Rank   int    `json:"rank"`
		Of     int    `json:"of"`
		Points int    `json:"points"`
	}
	type withSeason struct {
		Season []place `json:"season"`
	}
	oksanaMoscow := initData(1, "Oksana", "uk", hs.payload(moscowGroup))
	// Her answer of today is in no season yet.
	hs.expect("GET", "/api/today", oksanaMoscow, "", http.StatusOK)
	hs.expect("POST", "/api/guess", oksanaMoscow, `{"lat":40,"lon":-3}`, http.StatusOK)

	got := decode[withSeason](t, hs.expect("GET", "/api/profile", oksanaMoscow, "", http.StatusOK)).Season
	want := []place{{"world", 2, 3, 2500 + 4950}, {"group", 1, 2, 2500 + 4950}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("season from the Moscow group = %+v, want %+v", got, want)
	}
	// From the DM there is no group's.
	got = decode[withSeason](t, hs.expect("GET", "/api/profile", initData(1, "Oksana", "uk", ""), "", http.StatusOK)).Season
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("season from the DM = %+v", got)
	}
}
