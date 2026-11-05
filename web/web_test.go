// SPDX-License-Identifier: Apache-2.0
package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
)

func serve(t *testing.T, target string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIndex(t *testing.T) {
	rec := serve(t, "/", nil)
	if rec.Code != http.StatusOK || !regexp.MustCompile(`<script defer src="/app\.js\?v=[0-9a-f]{24}">`).MatchString(rec.Body.String()) {
		t.Fatalf("GET / = %d", rec.Code)
	}
	h := rec.Header()
	if h.Get("Content-Security-Policy") != CSP || h.Get("Cache-Control") != "no-cache" || h.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("headers = %v", h)
	}
	if !strings.Contains(CSP, "frame-ancestors 'self' https://web.telegram.org") || !strings.HasPrefix(h.Get("Strict-Transport-Security"), "max-age=") {
		t.Fatalf("framing and HSTS: %v", h)
	}
	again := serve(t, "/", http.Header{"If-None-Match": {h.Get("ETag")}})
	if again.Code != http.StatusNotModified {
		t.Fatalf("revalidation = %d", again.Code)
	}
}

func TestVendorIsImmutable(t *testing.T) {
	rec := serve(t, "/vendor/sofia-sans-5.3.0/sofia-sans-cyrillic-400-normal.woff2", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("font = %d %v", rec.Code, rec.Header())
	}
}

// Every page address is the app itself, with the same headers and the same
// hashed scripts as "/".
func TestPages(t *testing.T) {
	index := serve(t, "/", nil)
	for _, p := range []string{"/days", "/days/", "/days/2026-11-04", "/days/14", "/me", "/play", "/party/K7QF", "/p/a1b2c3"} {
		rec := serve(t, p, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != index.Body.String() || rec.Header().Get("Content-Security-Policy") != CSP ||
			rec.Header().Get("ETag") != index.Header().Get("ETag") {
			t.Errorf("GET %s = %d", p, rec.Code)
		}
	}
}

func TestUnknownPaths(t *testing.T) {
	for _, p := range []string{"/nope.js", "/../web.go", "/app/index.html", "/vendor/", "/days/2026/11", "/me/x", "/party", "/p", "/play/x",
		"/party/" + strings.Repeat("a", 65), "/days/a.b", "/days/app.js", "/dayss"} {
		if rec := serve(t, p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d", p, rec.Code)
		}
	}
}

// Every file index.html loads from the service is embedded, and our own code
// reaches no host but Telegram and the tile server: the CSP would block the
// rest in the browser, and this catches it before.
func TestNoThirdPartyHosts(t *testing.T) {
	index, err := fs.ReadFile(embedded, "app/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(string(index), -1) {
		ref := m[1]
		if strings.HasPrefix(ref, "https://telegram.org/") {
			continue
		}
		// The page is served on /days/14 too: a relative path would load /days/app.js.
		if !strings.HasPrefix(ref, "/") {
			t.Errorf("index.html loads %s by a relative path", ref)
		}
		if _, err := fs.Stat(embedded, "app"+ref); err != nil {
			t.Errorf("index.html loads %s, which is not embedded", ref)
		}
	}
	url := regexp.MustCompile(`https?://[a-z0-9.-]+`)
	allowed := map[string]bool{"https://telegram.org": true, TileHost: true}
	// Links people tap (attribution, the share sheet) open in Telegram, not from the page.
	links := map[string]bool{"https://t.me": true, "https://www.mapillary.com": true, "https://panoramax.fr": true, "https://www.openstreetmap.org": true, "https://openfreemap.org": true}
	for _, name := range appFiles(t) {
		data, err := fs.ReadFile(embedded, "app/"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range url.FindAllString(string(data), -1) {
			if !allowed[u] && !links[u] {
				t.Errorf("%s reaches %s", name, u)
			}
		}
	}
}

func TestTextsPerLanguage(t *testing.T) {
	for _, lang := range i18n.Codes() {
		rec := serve(t, "/texts/"+lang+".json", nil)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
			rec.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("/texts/%s.json = %d %v", lang, rec.Code, rec.Header())
		}
		var got struct {
			Lang string            `json:"lang"`
			T    map[string]string `json:"t"`
			Zone map[string]string `json:"zone"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Lang != lang || got.T["where"] == "" || got.T["unit.m"] == "" || len(got.Zone) != len(round.ZoneChoices) {
			t.Fatalf("%s: lang %q, where %q, %d zones", lang, got.Lang, got.T["where"], len(got.Zone))
		}
		if _, ok := got.T["btn.play"]; ok {
			t.Fatalf("%s: bot strings reached the app", lang)
		}
	}
	if rec := serve(t, "/texts/xx.json", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown language = %d", rec.Code)
	}
}

// The app holds no words of its own (AGENTS.md): every key it asks for is in
// internal/i18n, so a typo shows up here and not as a raw key on a phone.
func TestAppKeysExist(t *testing.T) {
	known := map[string]bool{}
	for key := range i18n.WithPrefix("en", "app.") {
		known[strings.TrimPrefix(key, "app.")] = true
	}
	for key := range i18n.WithPrefix("en", "unit.") {
		known[key] = true
	}
	uses := regexp.MustCompile(`\b(?:TN?|lines)\("([a-z_.]*[a-z_])"|data-(?:t|aria)="([a-z_.]+)"|LOOK = \{([^}]*)\}`)
	quoted := regexp.MustCompile(`"([a-z_]+)"`)
	for _, name := range appFiles(t) {
		if strings.HasSuffix(name, ".css") {
			continue
		}
		data, err := fs.ReadFile(embedded, "app/"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range uses.FindAllStringSubmatch(string(data), -1) {
			keys := []string{m[1] + m[2]}
			if m[3] != "" {
				keys = nil
				for _, q := range quoted.FindAllStringSubmatch(m[3], -1) {
					keys = append(keys, q[1])
				}
			}
			for _, key := range keys {
				if !known[key] {
					t.Errorf("%s asks for %q, which internal/i18n does not have", name, key)
				}
			}
		}
		if cyr := regexp.MustCompile(`[\p{Cyrillic}]`).FindString(string(data)); cyr != "" {
			t.Errorf("%s has Cyrillic text in it; it belongs in internal/i18n", name)
		}
	}
	for _, key := range []string{"compass_n", "compass_ne", "compass_e", "compass_se", "compass_s", "compass_sw", "compass_w", "compass_nw",
		"loading", "on_water", "on_land", "moved", "error.network", "error.left", "error.unavailable", "error.bad_request", "error.internal"} {
		if !known[key] {
			t.Errorf("the app builds %q, which internal/i18n does not have", key)
		}
	}
}

// The days page draws a flag only for a code it knows a file for, and its
// letters otherwise: the list in days.js is the vendored files, no more.
func TestFlagsAreTheVendoredFiles(t *testing.T) {
	data, err := fs.ReadFile(embedded, "app/days.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)const FLAGS = new Set\(\((.*?)\)\.split`).FindSubmatch(data)
	if m == nil {
		t.Fatal("no FLAGS in days.js")
	}
	listed := map[string]bool{}
	for _, code := range regexp.MustCompile(`[A-Z]{2}`).FindAllString(string(m[1]), -1) {
		listed[code] = true
	}
	files, err := fs.Glob(embedded, "app/vendor/country-flag-icons-1.6.20/3x2/*.svg")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(listed) {
		t.Errorf("%d flag files, %d codes in days.js", len(files), len(listed))
	}
	for _, f := range files {
		if code := strings.TrimSuffix(path.Base(f), ".svg"); !listed[code] {
			t.Errorf("%s is served but days.js does not know it", code)
		}
	}
}

// appFiles are the app's own files, every screen's included: all but the
// vendored libraries and the generated coastline.
func appFiles(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(embedded, "app")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "land.js" {
			names = append(names, e.Name())
		}
	}
	return names
}
