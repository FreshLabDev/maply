// SPDX-License-Identifier: Apache-2.0
package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
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
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<script src="app.js">`) {
		t.Fatalf("GET / = %d", rec.Code)
	}
	h := rec.Header()
	if h.Get("Content-Security-Policy") != CSP || h.Get("Cache-Control") != "no-cache" || h.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("headers = %v", h)
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

func TestUnknownPaths(t *testing.T) {
	for _, p := range []string{"/nope.js", "/../web.go", "/app/index.html", "/vendor/"} {
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
		if _, err := fs.Stat(embedded, "app/"+ref); err != nil {
			t.Errorf("index.html loads %s, which is not embedded", ref)
		}
	}
	url := regexp.MustCompile(`https?://[a-z0-9.-]+`)
	allowed := map[string]bool{"https://telegram.org": true, TileHost: true}
	// Links people tap (attribution, the share sheet) open in Telegram, not from the page.
	links := map[string]bool{"https://t.me": true, "https://www.mapillary.com": true, "https://panoramax.fr": true, "https://www.openstreetmap.org": true, "https://openfreemap.org": true}
	for _, name := range []string{"index.html", "app.css", "app.js", "tg.js", "map.js"} {
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
