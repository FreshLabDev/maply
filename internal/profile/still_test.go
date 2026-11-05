// SPDX-License-Identifier: Apache-2.0
package profile

import (
	"bytes"
	"context"
	"image/png"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

type upToStore struct {
	fakeStore
	asked []round.Date
}

func (f *upToStore) ProfileOf(_ context.Context, _ int64, upTo round.Date, _ int) (db.Profile, error) {
	f.asked = append(f.asked, upTo)
	return f.data, nil
}

// The public page's picture reads only the days over everywhere, and one
// drawing serves every preview of the link until another day closes.
func TestStillCountsOnlyClosedDays(t *testing.T) {
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	st := &upToStore{fakeStore: fakeStore{data: sample()}}
	now := today.Time().Add(12 * time.Hour)
	s := &Service{Store: st, Zones: zones, Now: func() time.Time { return now }}
	b, err := s.Still(context.Background(), 7, "ru")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil || img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
		t.Fatalf("still %v, %v", img.Bounds(), err)
	}
	if _, err := s.Still(context.Background(), 7, "ru"); err != nil {
		t.Fatal(err)
	}
	if want := LatestClosed(zones, now); len(st.asked) != 1 || st.asked[0] != want || !want.Before(today) {
		t.Fatalf("asked for %v, want once up to %v", st.asked, want)
	}
	st.data.Answers = st.data.Answers[2:]
	now = now.Add(24 * time.Hour)
	if _, err := s.Still(context.Background(), 7, "ru"); err != ErrNothing {
		t.Fatalf("nothing scored: %v", err)
	}
}

// The picture goes to strangers, who see no pins.
func TestStillHasNoPins(t *testing.T) {
	if p := build(sample(), "ru", today, func(round.Date) bool { return true }); p == nil || len(p.Pins) == 0 {
		t.Fatalf("the owner's picture has no pins to leave out: %+v", p)
	}
	if p := publicScene(sample(), "ru", today); p == nil || p.Pins != nil {
		t.Fatalf("public picture %+v", p)
	}
}
