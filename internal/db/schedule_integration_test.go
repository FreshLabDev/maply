// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/round"
)

// A post that has not gone out follows the admin's new time; one already
// taken does not move.
func TestPlanJobMovesOnlyAnUntouchedJob(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	d := round.NewDate(2026, 11, 2)
	at := time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC)
	runAt := func() time.Time {
		var got time.Time
		if err := store.pool.QueryRow(ctx, `SELECT run_at FROM job WHERE key = 'post:2026-11-02:-1001'`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, t0 := range []time.Time{at, at.Add(time.Hour)} {
		if err := store.PlanJob(ctx, "post", -1001, d, t0); err != nil {
			t.Fatal(err)
		}
	}
	if got := runAt(); !got.Equal(at.Add(time.Hour)) {
		t.Fatalf("run_at = %v, want the moved time", got)
	}
	if jobs, err := store.ClaimJobs(ctx, at.Add(2*time.Hour), time.Minute, 10); err != nil || len(jobs) != 1 {
		t.Fatalf("claimed %v, %v", jobs, err)
	}
	if err := store.PlanJob(ctx, "post", -1001, d, at.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := runAt(); !got.Equal(at.Add(time.Hour)) {
		t.Fatalf("a claimed job moved to %v", got)
	}
}

func TestReturnChatAfterABlock(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if err := store.LeaveChat(ctx, 77); err != nil {
		t.Fatal(err)
	}
	var active bool
	read := func() bool {
		if err := store.pool.QueryRow(ctx, `SELECT active FROM chat_settings WHERE chat_id = 77`).Scan(&active); err != nil {
			t.Fatal(err)
		}
		return active
	}
	if read() {
		t.Fatal("still active after the block")
	}
	if err := store.ReturnChat(ctx, 77); err != nil {
		t.Fatal(err)
	}
	if !read() {
		t.Fatal("not active after coming back")
	}
}
