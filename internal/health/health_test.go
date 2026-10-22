// SPDX-License-Identifier: Apache-2.0
package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeStore struct{ err error }

func (f fakeStore) Ping(context.Context) error { return f.err }

func serve(t *testing.T, store Store, lastPoll time.Time, initialized bool, startedAt time.Time) (int, response) {
	t.Helper()
	h := New(store,
		func() time.Time { return lastPoll },
		func() bool { return initialized },
		startedAt,
		Build{Version: "v1", Commit: "abc", Date: "today"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var got response
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return rr.Code, got
}

func TestHealthy(t *testing.T) {
	now := time.Now()
	code, got := serve(t, fakeStore{}, now, true, now.Add(-time.Hour))
	if code != http.StatusOK || !got.OK || !got.DB || got.Version != "v1" {
		t.Fatalf("code = %d, response = %+v", code, got)
	}
}

func TestUnhealthy(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name        string
		store       Store
		lastPoll    time.Time
		initialized bool
		startedAt   time.Time
	}{
		{"database down", fakeStore{err: errors.New("down")}, now, true, now},
		{"not initialized", fakeStore{}, now, false, now},
		{"polling stale", fakeStore{}, now.Add(-2 * pollMaxAge), true, now.Add(-time.Hour)},
		{"never polled after grace", fakeStore{}, time.Time{}, true, now.Add(-2 * pollMaxAge)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, got := serve(t, tc.store, tc.lastPoll, tc.initialized, tc.startedAt)
			if code != http.StatusServiceUnavailable || got.OK {
				t.Fatalf("code = %d, response = %+v", code, got)
			}
		})
	}
}

func TestStartingProcessIsFresh(t *testing.T) {
	now := time.Now()
	_, got := serve(t, fakeStore{}, time.Time{}, true, now)
	if !got.TelegramPollingFresh {
		t.Fatal("a process that just started is reported stale")
	}
}
