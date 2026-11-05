// SPDX-License-Identifier: Apache-2.0
package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A page's error reaches the log without Telegram's launch data, cut short.
func TestClientErrorsAreLoggedWithoutTheLaunchData(t *testing.T) {
	var logs strings.Builder
	s := &Server{Log: slog.New(slog.NewTextHandler(&logs, nil))}
	h := s.Handler()
	body := `{"message":"TypeError: x is undefined","source":"https://maply.example/app/#tgWebAppData=user%3D1%26hash%3Dabc","line":12,"stack":"` + strings.Repeat("a", 5000) + `"}`
	post := func(body, kind string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/log", strings.NewReader(body))
		req.Header.Set("Content-Type", kind)
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	// A plain-text post, which any page can send without asking, is refused.
	if code := post(body, "text/plain"); code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain: status %d", code)
	}
	if code := post(body, "application/json"); code != http.StatusNoContent {
		t.Fatalf("status %d", code)
	}
	out := logs.String()
	if !strings.Contains(out, "x is undefined") || strings.Contains(out, "tgWebAppData") || strings.Contains(out, strings.Repeat("a", 2001)) {
		t.Fatalf("log = %s", out)
	}
	for i := 0; i < 20; i++ {
		post(`{"message":"again"}`, "application/json; charset=utf-8")
	}
	if n := strings.Count(logs.String(), "again"); n != 4 {
		t.Fatalf("logged %d of 20 repeats, want the limit of 5 in all", n)
	}
}

// A page address loses its query, where the Mini App's signed start
// parameter is.
func TestClientLogDropsTheQuery(t *testing.T) {
	got := clean("at https://maply.example/app/?tgWebAppStartParam=AQEz_x-y&lang=ru:12:5 (app.js:3)", 300)
	if strings.Contains(got, "tgWebAppStartParam") || strings.Contains(got, "AQEz") || !strings.Contains(got, "https://maply.example/app/") || !strings.Contains(got, "(app.js:3)") {
		t.Fatalf("clean = %q", got)
	}
}

// Another site's page cannot post as a site player: not as a form, and not
// at all when the browser says it is cross-site.
func TestSitePostsComeFromMaplysPages(t *testing.T) {
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := s.Handler()
	for _, c := range []struct{ path, kind, site string }{
		{"/api/site/guess", "text/plain", ""},
		{"/api/site/guess", "application/x-www-form-urlencoded", ""},
		{"/api/site/guess", "application/json", "cross-site"},
		{"/api/site/name", "text/plain", ""},
		{"/api/site/name", "application/json", "cross-site"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(`{"lat":1,"lon":2,"name":"x="}`))
		req.Header.Set("Content-Type", c.kind)
		if c.site != "" {
			req.Header.Set("Sec-Fetch-Site", c.site)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || rec.Result().Header.Get("Set-Cookie") != "" {
			t.Errorf("%s as %s from %q: %d, cookie %q", c.path, c.kind, c.site, rec.Code, rec.Result().Header.Get("Set-Cookie"))
		}
	}
}

// A public profile's address is its key: the log keeps only that it was one.
func TestClientLogDropsPublicSlugs(t *testing.T) {
	got := clean("TypeError at https://maply.example/p/Ab_9-xYz0123456789abcd (profile.js:3:1)", 300)
	if strings.Contains(got, "Ab_9") || !strings.Contains(got, "/p/…") || !strings.Contains(got, "profile.js:3:1") {
		t.Fatalf("clean = %q", got)
	}
}

// A point in a map library's message stays out of the log.
func TestClientLogDropsCoordinates(t *testing.T) {
	got := clean("Invalid LngLat object: (30.523412, -50.4501) at app.js:128:15 v8.0", 300)
	if strings.Contains(got, "30.52") || strings.Contains(got, "50.45") || !strings.Contains(got, "app.js:128:15") || !strings.Contains(got, "v8.0") {
		t.Fatalf("clean = %q", got)
	}
}
