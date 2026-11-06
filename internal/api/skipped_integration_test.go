// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

// A day nobody has answered anywhere shows its place to nobody: the result
// and the card answer 403 "held" from the chat's reveal on, the days list and
// the day's page leave it out, and once the final job skips it the date is no
// round at all, whoever asks.
func TestADayNobodyAnsweredIsHeldThenGone(t *testing.T) {
	hs := setup(t)
	ctx := context.Background()
	store := hs.srv.Store.(*db.Store)
	oksana := initData(1, "Oksana", "uk", hs.payload(kyivGroup))
	ivan := initData(2, "Ivan", "ru", hs.payload(moscowGroup))
	hs.expect("GET", "/api/today", oksana, "", http.StatusOK)
	hs.expect("GET", "/api/today", ivan, "", http.StatusOK)

	// Kyiv's reveal: nothing to show yet, to the one who did not answer or to
	// the one who is not even in the chat.
	hs.now = time.Date(2026, time.December, 10, 18, 5, 0, 0, time.UTC)
	for _, who := range []string{oksana, ivan} {
		body := hs.expect("GET", "/api/result", who, "", http.StatusForbidden)
		if leaks(body) || !strings.Contains(body, "held") {
			t.Fatalf("result of a day nobody answered: %s", body)
		}
	}

	// Over everywhere, not skipped yet: the days list and the page keep it out.
	hs.now = time.Date(2026, time.December, 11, 4, 3, 0, 0, time.UTC)
	days := hs.expect("GET", "/api/days", oksana, "", http.StatusOK)
	var list struct {
		Days []dayView `json:"days"`
	}
	if err := json.Unmarshal([]byte(days), &list); err != nil || len(list.Days) != 0 || leaks(days) {
		t.Fatalf("days = %s (%v)", days, err)
	}
	hs.expect("GET", "/api/day?n=1", oksana, "", http.StatusNotFound)

	// The final job skips it.
	if ok, err := store.SkipDay(ctx, hs.date, round.DateIn(hs.now, time.UTC).AddDays(db.ReturnDays)); err != nil || !ok {
		t.Fatalf("skip = %v, %v", ok, err)
	}
	for _, path := range []string{"/api/today", "/api/result"} {
		if body := hs.expect("GET", path, oksana, "", http.StatusNotFound); !strings.Contains(body, "no_round") {
			t.Errorf("%s of a skipped date: %s", path, body)
		}
	}
	hs.expect("GET", "/api/site/result?date=2026-12-10", "", "", http.StatusNotFound)
	hs.expect("GET", "/api/day?n=1", oksana, "", http.StatusNotFound)
	hs.expect("GET", "/api/day?date=2026-12-10", oksana, "", http.StatusNotFound)
	hs.expect("GET", "/og/day/1-en.jpg", "", "", http.StatusNotFound)
	if body := hs.expect("GET", "/api/days", oksana, "", http.StatusOK); strings.Contains(body, "2026-12-10") {
		t.Errorf("days list shows the skipped date: %s", body)
	}
}

// An answer that comes after the first ones held back a result makes the day
// a day: the place may show, to everyone who is allowed to see it.
func TestAnAnswerEndsTheHold(t *testing.T) {
	hs := setup(t)
	oksana := initData(1, "Oksana", "uk", hs.payload(kyivGroup))
	ivan := initData(2, "Ivan", "ru", hs.payload(moscowGroup))
	hs.expect("GET", "/api/today", oksana, "", http.StatusOK)
	hs.expect("GET", "/api/today", ivan, "", http.StatusOK)
	hs.expect("POST", "/api/guess", ivan, `{"lat":50,"lon":30}`, http.StatusOK)
	hs.now = time.Date(2026, time.December, 10, 18, 5, 0, 0, time.UTC)
	if body := hs.expect("GET", "/api/result", oksana, "", http.StatusOK); !leaks(body) {
		t.Fatalf("result after an answer elsewhere: %s", body)
	}
}
