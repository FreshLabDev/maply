// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/web"
)

// A link to a day's page has a preview only once the day is over everywhere
// and kept with its strip: before that the page is the plain app and the
// picture is a 404, the same as for a day that never was. The picture is
// drawn in the reader's language when the faces have its letters, and in
// English otherwise.
func TestADayLinkHasAPreviewOnlyOnceTheDayIsKept(t *testing.T) {
	hs := setup(t)
	ctx := context.Background()
	store := hs.srv.Store.(*db.Store)
	hs.srv.AppURL = "https://maply.test/"
	photo := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := range 480 {
		for x := range 640 {
			photo.Set(x, y, color.RGBA{uint8(x / 3), uint8(y / 2), 160, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, photo, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hs.srv.ImagesDir, testImageKey), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := web.HandlerWithMeta(hs.srv.PageMeta)
	if err != nil {
		t.Fatal(err)
	}
	oksana := initData(1, "Oksana", "uk", "")
	hs.expect("GET", "/api/today", oksana, "", http.StatusOK)
	hs.expect("POST", "/api/guess", oksana, `{"lat":50.45,"lon":30.52}`, http.StatusOK)

	page := func(target, lang string) string {
		t.Helper()
		req := httptest.NewRequest("GET", target, nil)
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		rr := httptest.NewRecorder()
		app.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s = %d", target, rr.Code)
		}
		return rr.Body.String()
	}
	picture := func(method, name string) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, httptest.NewRequest(method, "/og/day/"+name, nil))
		return rr
	}
	none := func(when string) {
		t.Helper()
		if body := page("/days/1", "uk"); strings.Contains(body, "og:") || leaks(body) {
			t.Fatalf("%s: the page has a preview", when)
		}
		for _, name := range []string{"1-uk.jpg", "1-en.jpg"} {
			if rr := picture("GET", name); rr.Code != http.StatusNotFound || leaks(rr.Body.String()) {
				t.Fatalf("%s: %s = %d %q", when, name, rr.Code, rr.Body)
			}
		}
	}
	none("open")
	// Kept and drawn by hand before its time, the day still shows nothing.
	if _, err := hs.pool.Exec(ctx, `INSERT INTO maply.day_closed (date) VALUES ('2026-12-10')`); err != nil {
		t.Fatal(err)
	}
	if err := store.PutThumb(ctx, db.Thumb{Date: hs.date, Key: stripKey, Recipe: 1, Small: []byte("small"), Wide: []byte("wide")}); err != nil {
		t.Fatal(err)
	}
	none("kept by hand")
	if _, err := hs.pool.Exec(ctx, `DELETE FROM maply.day_closed`); err != nil {
		t.Fatal(err)
	}
	hs.now = time.Date(2026, time.December, 11, 4, 0, 0, 0, time.UTC)
	none("over, not kept")

	if err := store.KeepDay(ctx, hs.date); err != nil {
		t.Fatal(err)
	}
	if err := store.PutThumb(ctx, db.Thumb{Date: hs.date, Key: stripKey, Recipe: 1, Small: []byte("small"), Wide: []byte("wide")}); err != nil {
		t.Fatal(err)
	}
	// The day's page read without its strip is kept for closedSoon.
	if strings.Contains(page("/days/1", "uk"), "og:") {
		t.Fatal("a preview within closedSoon")
	}
	hs.now = hs.now.Add(closedSoon)
	body := page("/days/1", "uk-UA,uk;q=0.9")
	for _, want := range []string{
		`<meta property="og:title" content="maply #1 · Львів">`,
		`<meta property="og:url" content="https://maply.test/days/1">`,
		`<meta property="og:image" content="https://maply.test/og/day/1-uk.jpg">`,
		`<meta property="og:image:height" content="630">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("no %s in %s", want, body[:min(len(body), 2000)])
		}
	}
	// ?lang= is the language a link shared from the app names; one whose
	// letters the faces lack gets the English picture.
	if body := page("/days/1?lang=zh", "uk"); !strings.Contains(body, "/og/day/1-en.jpg") {
		t.Fatal("a Chinese preview does not fall back to the English picture")
	}
	if body := page("/days/1", "xx, ru;q=0.5"); !strings.Contains(body, "/og/day/1-ru.jpg") {
		t.Fatal("Accept-Language is not followed")
	}
	// The other pages, and the date form of a day, stay plain.
	for _, p := range []string{"/days", "/days/2026-12-10", "/days/2", "/"} {
		if strings.Contains(page(p, "uk"), "og:") {
			t.Fatalf("%s has a preview", p)
		}
	}

	rr := picture("GET", "1-uk.jpg")
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "image/jpeg" || rr.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(rr.Header().Get("Cache-Control"), "max-age=") {
		t.Fatalf("picture = %d %v", rr.Code, rr.Header())
	}
	if bytes.Contains(rr.Body.Bytes()[:min(rr.Body.Len(), 512)], []byte("Exif")) {
		t.Fatal("the picture carries EXIF")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(rr.Body.Bytes()))
	if err != nil || cfg.Width != 1200 || cfg.Height != 630 {
		t.Fatalf("picture = %+v, %v", cfg, err)
	}
	if again := picture("GET", "1-uk.jpg"); !bytes.Equal(again.Body.Bytes(), rr.Body.Bytes()) {
		t.Fatal("the kept picture changed")
	}
	if head := picture("HEAD", "1-uk.jpg"); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD = %d, %d bytes", head.Code, head.Body.Len())
	}
	for _, name := range []string{"1-zh.jpg", "1-xx.jpg", "01-en.jpg", "2-en.jpg", "1-en.png", "1-en", "1-EN.jpg", "..%2F1-en.jpg", "0-en.jpg"} {
		if rr := picture("GET", name); rr.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", name, rr.Code)
		}
	}
	// A picture that cannot be read is a 404 that names no path.
	if err := os.Remove(filepath.Join(hs.srv.ImagesDir, testImageKey)); err != nil {
		t.Fatal(err)
	}
	if rr := picture("GET", "1-ru.jpg"); rr.Code != http.StatusNotFound || strings.Contains(rr.Body.String(), hs.srv.ImagesDir) ||
		strings.Contains(hs.logs.String(), hs.srv.ImagesDir) {
		t.Fatalf("missing picture = %d %q", rr.Code, rr.Body)
	}
}
