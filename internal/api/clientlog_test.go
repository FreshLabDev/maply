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
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/log", strings.NewReader(body)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	out := logs.String()
	if !strings.Contains(out, "x is undefined") || strings.Contains(out, "tgWebAppData") || strings.Contains(out, strings.Repeat("a", 2001)) {
		t.Fatalf("log = %s", out)
	}
	for i := 0; i < 20; i++ {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/log", strings.NewReader(`{"message":"again"}`)))
	}
	if n := strings.Count(logs.String(), "again"); n != 9 {
		t.Fatalf("logged %d of 20 repeats, want the limit of 10 in all", n)
	}
}
