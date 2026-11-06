// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

// finalStore records what the final world job does with the store.
type finalStore struct {
	Store
	read      []round.Date
	kept      []round.Date
	postponed []time.Time
	finished  []string
}

func (f *finalStore) RoundOn(_ context.Context, d round.Date) (db.Round, bool, error) {
	f.read = append(f.read, d)
	return db.Round{}, false, nil
}

func (f *finalStore) KeepDay(_ context.Context, d round.Date) error {
	f.kept = append(f.kept, d)
	return nil
}

func (f *finalStore) PostponeJob(_ context.Context, _ int64, at time.Time) error {
	f.postponed = append(f.postponed, at)
	return nil
}

func (f *finalStore) FinishJob(_ context.Context, _ int64, status, _ string) error {
	f.finished = append(f.finished, status)
	return nil
}

// A final world job that runs before the day is over in every zone, moved
// by hand or by a clock that jumped, reads and keeps nothing: it goes back
// to the queue for its time, and only then does the day go to day_closed.
func TestFinalWorldWaitsForTheLastReveal(t *testing.T) {
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	d := round.NewDate(2026, time.December, 10)
	last := zones.LastReveal(d)
	now := last.Add(-time.Minute)
	store := &finalStore{}
	s := &Scheduler{Store: store, Zones: zones, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return now }}
	job := db.Job{ID: 1, Key: "world:" + d.String(), Kind: jobWorld, Date: d, RunAt: now}

	s.runJob(context.Background(), job)
	if len(store.read) != 0 || len(store.kept) != 0 || len(store.finished) != 0 {
		t.Fatalf("early job read %v, kept %v, finished %v", store.read, store.kept, store.finished)
	}
	if len(store.postponed) != 1 || !store.postponed[0].Equal(last.Add(worldAfter)) {
		t.Fatalf("early job moved to %v, want %v", store.postponed, last.Add(worldAfter))
	}

	// At the last reveal it runs: here the date has no round, so it ends
	// without a send.
	now = last
	s.runJob(context.Background(), job)
	if len(store.read) != 1 || len(store.finished) != 1 || store.finished[0] != "skipped" {
		t.Fatalf("job on time read %v, finished %v", store.read, store.finished)
	}
}
