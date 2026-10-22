// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/round"
)

func approvePlace(t *testing.T, s *Store, sourceID string, difficulty int) int64 {
	t.Helper()
	var id int64
	err := s.pool.QueryRow(context.Background(), `
		INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon, image_key,
		                         country_iso, continent, difficulty, reviewed_at)
		VALUES ('approved', 'mapillary', $1, 'CC-BY-SA-4.0', 'photo', 50, 30, 'key-'||$1, 'UA', 'europe', $2, now())
		RETURNING id`, sourceID, difficulty).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEnsureRoundSpendsOnePlacePerDate(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	easy := approvePlace(t, store, "easy", 1)
	hard := approvePlace(t, store, "hard", 5)

	monday := round.NewDate(2026, time.November, 2)
	friday := monday.AddDays(4)

	// Many callers at once still produce one round and spend one place.
	var wg sync.WaitGroup
	got := make([]Round, 8)
	errs := make([]error, 8)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], errs[i] = store.EnsureRound(ctx, monday)
		}()
	}
	wg.Wait()
	for i := range got {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if got[i].PlaceID != easy || got[i].DayNo != 1 {
			t.Fatalf("caller %d got %+v, want place %d as day 1", i, got[i], easy)
		}
	}

	fri, err := store.EnsureRound(ctx, friday)
	if err != nil {
		t.Fatal(err)
	}
	if fri.PlaceID != hard || fri.DayNo != 5 {
		t.Fatalf("Friday = %+v, want the hard place as day 5", fri)
	}
	if _, err := store.EnsureRound(ctx, friday.AddDays(1)); !errors.Is(err, ErrNoPlace) {
		t.Fatalf("empty queue: err = %v, want ErrNoPlace", err)
	}
	if _, err := store.EnsureRound(ctx, monday.AddDays(-1)); err == nil {
		t.Fatal("a round before the first one was created")
	}
	var used int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM maply.place WHERE status = 'used'`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 2 {
		t.Fatalf("%d places used, want 2", used)
	}
}

// A person who plays in a Kyiv group and a Moscow group must answer before the
// Moscow reveal. Leaving the Moscow group gives the hour back.
func TestDeadlineAcrossChats(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	person := tg.User{ID: 51, FirstName: "Oksana", LanguageCode: "uk"}
	kyivGroup := &tg.Chat{ID: -1005101, Type: "supergroup", Title: "Kyiv"}
	moscowGroup := &tg.Chat{ID: -1005102, Type: "supergroup", Title: "Moscow"}
	for _, chat := range []*tg.Chat{nil, kyivGroup, moscowGroup} {
		if err := store.Touch(ctx, person, chat); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, store, `INSERT INTO maply.chat_settings (chat_id, kind, timezone) VALUES
		($1, 'group', 'Europe/Kyiv'), ($2, 'group', 'Europe/Moscow')`, kyivGroup.ID, moscowGroup.ID)

	contexts, err := store.Contexts(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The groups name their zones; the DM has none and follows Oksana's language.
	want := []Context{{-1005102, "Europe/Moscow", ""}, {-1005101, "Europe/Kyiv", ""}, {0, "", "uk"}}
	if len(contexts) != len(want) {
		t.Fatalf("contexts = %+v, want %+v", contexts, want)
	}
	for i := range want {
		if contexts[i] != want[i] {
			t.Fatalf("contexts = %+v, want %+v", contexts, want)
		}
	}

	winter := round.NewDate(2026, time.December, 10)
	got, err := store.Deadline(ctx, zones, person.ID, winter)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, time.December, 10, 17, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("deadline = %s, want %s (20:00 Moscow)", got.UTC(), want)
	}

	exec(t, store, `UPDATE maply.chat_settings SET active = false WHERE chat_id = $1`, moscowGroup.ID)
	got, err = store.Deadline(ctx, zones, person.ID, winter)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, time.December, 10, 18, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("deadline after leaving Moscow = %s, want %s (20:00 Kyiv)", got.UTC(), want)
	}

	// Someone maply has never seen still gets a window: the default zone's.
	got, err = store.Deadline(ctx, zones, 99999, winter)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, time.December, 10, 18, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("deadline for a stranger = %s, want %s (20:00 Kyiv)", got.UTC(), want)
	}
}
