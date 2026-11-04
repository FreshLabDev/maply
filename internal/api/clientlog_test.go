// SPDX-License-Identifier: Apache-2.0
package api

import (
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
	if n := strings.Count(logs.String(), "again"); n != 9 {
		t.Fatalf("logged %d of 20 repeats, want the limit of 10 in all", n)
	}
}
