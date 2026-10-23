// SPDX-License-Identifier: Apache-2.0
package profile

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

func ptr[T any](v T) *T { return &v }

var today = round.NewDate(2026, 11, 10)

// Three days answered: two scored, today's still open. The world ranking of
// the 8th is final, the 9th's is not yet.
func sample() db.Profile {
	return db.Profile{
		Name: "Аня 🙂",
		Answers: []db.Answered{
			{Date: today.AddDays(-2), DayNo: 8, Lat: 50, Lon: 30, Km: ptr(1200.0), Points: ptr(2500)},
			{Date: today.AddDays(-1), DayNo: 9, Lat: 40, Lon: 10, Km: ptr(300.0), Points: ptr(4100)},
			{Date: today, DayNo: 10, Lat: 10, Lon: 10},
		},
		Rounds: []db.RoundDay{{Date: today, DayNo: 10}, {Date: today.AddDays(-1), DayNo: 9}, {Date: today.AddDays(-2), DayNo: 8}, {Date: today.AddDays(-3), DayNo: 7}},
		World:  []db.WorldPlace{{Date: today.AddDays(-2), Rank: 3, Of: 40}, {Date: today.AddDays(-1), Rank: 1, Of: 52}},
		Streak: 3,
	}
}

func closedBefore(d round.Date) func(round.Date) bool {
	return func(x round.Date) bool { return x.Time().Before(d.Time()) }
}

func TestBuildLaysOutTheNumbers(t *testing.T) {
	p := build(sample(), "ru", today, closedBefore(today.AddDays(-1)))
	if p.Name != "Аня" || p.Title != "статистика" || p.Total.Value != "6 600" {
		t.Fatalf("name %q, title %q, total %q", p.Name, p.Title, p.Total.Value)
	}
	want := []string{"2", "3", "750 км", "4 100", "3 / 40", "3"}
	for i, w := range want {
		if p.Tiles[i].Value != w {
			t.Errorf("tile %d = %q, want %q", i, p.Tiles[i].Value, w)
		}
	}
	if p.Tiles[3].Label != "лучший день, #9" || p.Tiles[4].Label != "место в мире, #8" || p.Tiles[5].Gold {
		t.Errorf("labels %q, %q, gold %v", p.Tiles[3].Label, p.Tiles[4].Label, p.Tiles[5].Gold)
	}
	// Today is on the strip, since it is answered; the 7th was missed.
	if len(p.Days) != 4 || p.Days[0].No != 7 || p.Days[0].Played || !p.Days[2].Best || !p.Days[3].Played {
		t.Fatalf("days %+v", p.Days)
	}
	if len(p.Pins) != 3 || p.Pins[1].Medal != 1 || p.Pins[2].Km != 4000 {
		t.Fatalf("pins %+v", p.Pins)
	}

	// Once the 9th is final, it is the place shown, and a first place is gold.
	p = build(sample(), "ru", today, closedBefore(today))
	if p.Tiles[4].Value != "1 / 52" || p.Tiles[5].Value != "1" || !p.Tiles[5].Gold {
		t.Fatalf("world %+v, best %+v", p.Tiles[4], p.Tiles[5])
	}
}

// A language the brand's faces do not write is drawn in English.
func TestBuildFallsBackToEnglish(t *testing.T) {
	p := build(sample(), "ja", today, closedBefore(today))
	if p.Title != "stats" || p.Tiles[2].Value != "750 km" || string(p.Lang) != "en" {
		t.Fatalf("title %q, median %q, lang %q", p.Title, p.Tiles[2].Value, p.Lang)
	}
}

func TestNothingToDrawBeforeAScoredDay(t *testing.T) {
	d := sample()
	d.Answers = d.Answers[2:]
	if p := build(d, "ru", today, closedBefore(today)); p != nil {
		t.Fatalf("drew %+v", p)
	}
}

type fakeStore struct {
	data  db.Profile
	cards map[string]string
}

func (f *fakeStore) ProfileOf(context.Context, int64, round.Date, int) (db.Profile, error) {
	return f.data, nil
}
func (f *fakeStore) ProfileCard(_ context.Context, _ int64, lang, version string) (string, error) {
	return f.cards[lang+version], nil
}
func (f *fakeStore) SetProfileCard(_ context.Context, _ int64, lang, version, id string) error {
	f.cards[lang+version] = id
	return nil
}
func (f *fakeStore) RecentPlayers(context.Context, round.Date) ([]int64, error) {
	return []int64{7}, nil
}
func (f *fakeStore) UserLang(context.Context, int64) (string, error) { return "ru", nil }

func TestTheFingerprintFollowsTheNumbers(t *testing.T) {
	st := &fakeStore{data: sample(), cards: map[string]string{}}
	s := &Service{Store: st, Now: func() time.Time { return today.Time().Add(12 * time.Hour) }}
	_, a, err := s.picture(context.Background(), 7, "ru")
	if err != nil {
		t.Fatal(err)
	}
	_, same, _ := s.picture(context.Background(), 7, "ru")
	st.data.Streak = 4
	_, other, _ := s.picture(context.Background(), 7, "ru")
	_, en, _ := s.picture(context.Background(), 7, "en")
	if a != same || a == other || a == en {
		t.Fatalf("versions %s %s %s %s", a, same, other, en)
	}
}

// One drawing serves every press until Telegram has the file; then its
// file_id does, and the file is gone.
func TestDrawOnceThenTheFileID(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil || raceEnabled {
		t.Skip("needs ffmpeg, and minutes under the race detector")
	}
	st := &fakeStore{data: sample(), cards: map[string]string{}}
	s := &Service{Store: st, FFmpeg: ffmpeg, Dir: t.TempDir(), Now: func() time.Time { return today.Time().Add(12 * time.Hour) }}
	ctx := context.Background()
	if _, ok, err := s.Ready(ctx, 7, "ru"); err != nil || ok {
		t.Fatalf("ready before drawing: %v, %v", ok, err)
	}
	c, err := s.Draw(ctx, 7, "ru", 0)
	if err != nil || len(c.MP4) < 1000 || c.FileID != "" {
		t.Fatalf("drew %d bytes, file %q, %v", len(c.MP4), c.FileID, err)
	}
	if again, ok, err := s.Ready(ctx, 7, "ru"); err != nil || !ok || len(again.MP4) != len(c.MP4) {
		t.Fatalf("not ready after drawing: %v, %v", ok, err)
	}
	if err := s.Keep(ctx, 7, "ru", c.Version, "file-1"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Ready(ctx, 7, "ru"); err != nil || !ok || got.FileID != "file-1" || got.MP4 != nil {
		t.Fatalf("after keeping: %+v, %v, %v", got, ok, err)
	}
	if c, err := s.Draw(ctx, 7, "ru", 0); err != nil || c.FileID != "file-1" {
		t.Fatalf("drew again: %+v, %v", c, err)
	}
}
