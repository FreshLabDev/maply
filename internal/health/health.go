// SPDX-License-Identifier: Apache-2.0
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type Store interface {
	Ping(context.Context) error
}

type Build struct {
	Version string
	Commit  string
	Date    string
}

type Handler struct {
	store       Store
	lastPoll    func() time.Time
	initialized func() bool
	startedAt   time.Time
	build       Build
	log         *slog.Logger
}

// pollMaxAge is how long polling may stay silent. getUpdates holds a request for
// 25 seconds, so anything past three of those is a stuck poller.
const pollMaxAge = 90 * time.Second

func New(store Store, lastPoll func() time.Time, initialized func() bool, startedAt time.Time, build Build, log *slog.Logger) *Handler {
	return &Handler{store: store, lastPoll: lastPoll, initialized: initialized, startedAt: startedAt, build: build, log: log}
}

type response struct {
	OK                   bool       `json:"ok"`
	Version              string     `json:"version"`
	Commit               string     `json:"commit"`
	BuiltAt              string     `json:"built_at"`
	DB                   bool       `json:"db"`
	TelegramInitialized  bool       `json:"telegram_initialized"`
	TelegramPollingFresh bool       `json:"telegram_polling_fresh"`
	TelegramLastPollAt   *time.Time `json:"telegram_last_poll_at,omitempty"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	result := response{Version: h.build.Version, Commit: h.build.Commit, BuiltAt: h.build.Date}
	if err := h.store.Ping(ctx); err != nil {
		h.log.Error("health database check failed", "error", err)
	} else {
		result.DB = true
	}
	lastPoll := h.lastPoll()
	if !lastPoll.IsZero() {
		result.TelegramLastPollAt = &lastPoll
	}
	result.TelegramPollingFresh = fresh(h.startedAt, lastPoll, pollMaxAge)
	result.TelegramInitialized = h.initialized != nil && h.initialized()
	result.OK = result.DB && result.TelegramInitialized && result.TelegramPollingFresh
	w.Header().Set("Content-Type", "application/json")
	if !result.OK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(result)
}

// fresh gives a process that has not polled yet the same grace period from its
// start, so a container is not reported unhealthy while it connects.
func fresh(startedAt, lastSeen time.Time, maxAge time.Duration) bool {
	if lastSeen.IsZero() {
		return time.Since(startedAt) < maxAge
	}
	return time.Since(lastSeen) < maxAge
}
