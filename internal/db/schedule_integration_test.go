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

// The result keeps the file_id of the animation it went out as, for sharing;
// a result sent as a picture keeps none.
func TestResultKeepsItsAnimation(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	d := round.NewDate(2026, 11, 1)
	if err := store.EnsureChat(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ChatRound(ctx, 7, d); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ set, want string }{{"anim-1", "anim-1"}, {"", ""}} {
		if err := store.SetResult(ctx, 7, d, 42, c.set); err != nil {
			t.Fatal(err)
		}
		if got, err := store.ResultAnimation(ctx, 7, d); err != nil || got != c.want {
			t.Fatalf("after %q: %q, %v", c.set, got, err)
		}
	}
	if got, err := store.ResultAnimation(ctx, 7, d.AddDays(1)); err != nil || got != "" {
		t.Fatalf("a day without a result: %q, %v", got, err)
	}
}

// A DM result showing the world picture keeps its version, and the day's
// close finds it among the DM results to put the final picture in.
func TestWorldResultKeepsItsVersion(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	d := round.NewDate(2026, 11, 1)
	for _, id := range []int64{7, 8, -1007001} {
		if err := store.EnsureChat(ctx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ChatRound(ctx, id, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetWorldResult(ctx, 7, d, 42, "anim-1", "v1"); err != nil {
		t.Fatal(err)
	}
	// An edit Telegram found unchanged gives no file id: the old one stays.
	if err := store.SetWorldResult(ctx, 7, d, 42, "", "v2"); err != nil {
		t.Fatal(err)
	}
	if v, err := store.WorldVersion(ctx, 7, d); err != nil || v != "v2" {
		t.Fatalf("version = %q, %v", v, err)
	}
	if id, _ := store.ResultAnimation(ctx, 7, d); id != "anim-1" {
		t.Fatalf("file id = %q", id)
	}
	if v, _ := store.WorldVersion(ctx, 8, d); v != "" {
		t.Fatalf("a result without the world picture has version %q", v)
	}
	// The group's result and a DM's text result are not DM animations.
	if err := store.SetResult(ctx, -1007001, d, 50, "anim-2"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetResult(ctx, 8, d, 51, ""); err != nil {
		t.Fatal(err)
	}
	got, err := store.DMResults(ctx, d)
	if err != nil || len(got) != 1 || got[0] != (DMResult{ChatID: 7, MsgID: 42}) {
		t.Fatalf("dm results = %+v, %v", got, err)
	}
}
