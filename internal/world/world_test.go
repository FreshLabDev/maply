// SPDX-License-Identifier: Apache-2.0
package world

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
)

type fakeStore struct {
	world db.World
	pins  []db.WorldPin
	kept  map[string]db.WorldCard
}

func (f *fakeStore) World(context.Context, round.Date) (db.World, error) { return f.world, nil }
func (f *fakeStore) WorldPins(context.Context, round.Date) ([]db.WorldPin, error) {
	return f.pins, nil
}
func (f *fakeStore) RoundOn(_ context.Context, d round.Date) (db.Round, bool, error) {
	return db.Round{Date: d, DayNo: 3, PlaceID: 1}, true, nil
}
func (f *fakeStore) Answer(context.Context, int64) (db.Answer, error) {
	return db.Answer{Point: geo.Point{Lat: 38.72, Lon: -9.14}, CountryISO: "PT", City: map[string]string{"en": "Lisbon"}}, nil
}
func (f *fakeStore) WorldCard(_ context.Context, _ round.Date, lang string) (db.WorldCard, bool, error) {
	c, ok := f.kept[lang]
	return c, ok, nil
}
func (f *fakeStore) SetWorldCard(_ context.Context, _ round.Date, lang string, c db.WorldCard) error {
	f.kept[lang] = c
	return nil
}

func (f *fakeStore) answer(user int64, name string, km float64, at time.Time) {
	f.world.Entries = append(f.world.Entries, db.Entry{Rank: len(f.world.Entries) + 1, UserID: user, Name: name, Km: km, Points: 5000 - int(km)})
	f.pins = append(f.pins, db.WorldPin{Lat: 40, Lon: float64(len(f.pins)), Km: km, AnsweredAt: at})
}

func TestCardIsDrawnOncePerRanking(t *testing.T) {
	ctx := context.Background()
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	d := round.NewDate(2026, 11, 2)
	clock := d.At(20, 0, time.UTC)
	store := &fakeStore{kept: map[string]db.WorldCard{}}
	drawn := 0
	s := &Service{Store: store, Zones: zones, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:  func() time.Time { return clock },
		Draw: func(context.Context, anim.Scene) ([]byte, error) { drawn++; return []byte("mp4"), nil }}

	if _, err := s.Card(ctx, d, "en", 0); err != ErrEmpty {
		t.Fatalf("empty day: %v", err)
	}
	store.answer(1, "Ana", 3, clock.Add(-time.Hour))
	c, err := s.Card(ctx, d, "en", 1)
	if err != nil || drawn != 1 || c.MP4 == nil || c.Final {
		t.Fatalf("first card = %+v, %v, drawn %d", c, err, drawn)
	}
	s.Keep(ctx, d, "en", c, "file-1")
	if again, _ := s.Card(ctx, d, "en", 1); drawn != 1 || again.FileID != "file-1" || again.MP4 != nil {
		t.Errorf("same ranking drawn again or not by file id: %+v", again)
	}
	if fresh, _ := s.Fresh(ctx, d, "en", c.Version, 1); !fresh {
		t.Error("the picture just drawn is not fresh")
	}

	// A new answer a minute later: the last picture stands for it, unless
	// the person asking is not on it.
	clock = clock.Add(time.Minute)
	store.answer(2, "Bo", 30, clock)
	if fresh, _ := s.Fresh(ctx, d, "en", c.Version, 1); !fresh {
		t.Error("redrawn within the minute for somebody already on it")
	}
	if c2, _ := s.Card(ctx, d, "en", 1); drawn != 1 || c2.Version != c.Version {
		t.Errorf("redrawn too soon: drawn %d", drawn)
	}
	c2, _ := s.Card(ctx, d, "en", 2)
	if drawn != 2 || c2.World.RankOf(2) != 2 {
		t.Errorf("a new player's own result lacks them: drawn %d, rank %d", drawn, c2.World.RankOf(2))
	}

	// The day closes everywhere: one more picture, final.
	clock = zones.LastReveal(d)
	if fresh, _ := s.Fresh(ctx, d, "en", c2.Version, 0); fresh {
		t.Error("an open day's picture is fresh after the close")
	}
	final, _ := s.Card(ctx, d, "en", 0)
	if drawn != 3 || !final.Final {
		t.Errorf("final = %+v, drawn %d", final, drawn)
	}
}

func TestScenePinsRainInTheOrderTheyCame(t *testing.T) {
	store := &fakeStore{}
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	store.answer(1, "Ana", 3, at.Add(2*time.Hour)) // best, answered last
	store.answer(2, "Bo", 30, at)
	store.answer(3, "Cy", 300, at.Add(time.Hour))
	store.answer(4, "Di", 3000, at.Add(30*time.Minute))
	a, _ := store.Answer(context.Background(), 1)
	w := Scene("en", 3, a, store.world, store.pins, nil, [3]bool{true, true, true}).(*anim.World)
	if w.Guesses[0].At != 1 || w.Guesses[1].At != 0 {
		t.Errorf("times = %v %v", w.Guesses[0].At, w.Guesses[1].At)
	}
	for i, want := range []int{1, 2, 3, 0} {
		if w.Guesses[i].Medal != want {
			t.Errorf("medal of %d = %d, want %d", i, w.Guesses[i].Medal, want)
		}
	}
	if w.Top[0] == nil || w.Top[2] == nil || w.UnitM != "m" {
		t.Errorf("podium %v, metres %q", w.Top, w.UnitM)
	}
}

// A site winner is a guest on the podium, in English where the faces don't
// write the language's word for it.
func TestSiteWinnerIsAGuestInEveryLanguage(t *testing.T) {
	store := &fakeStore{}
	store.answer(1, "Ana", 3, time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC))
	a, _ := store.Answer(context.Background(), 1)
	w := store.world
	w.Entries = append([]db.Entry{{Rank: 1, Player: "p1", Name: "anything typed", Km: 1, Points: 5000}}, w.Entries...)
	for lang, want := range map[string]string{"zh": "Guest", "ja": "Guest", "ar": "Guest", "en": "Guest"} {
		sc := Scene(lang, 3, a, w, store.pins, nil, [3]bool{true, true, true}).(*anim.World)
		if got := sc.Top[0].Name; got != want {
			t.Errorf("%s: site winner named %q, want %q", lang, got, want)
		}
	}
	if got := Scene("ru", 3, a, w, store.pins, nil, [3]bool{true, true, true}).(*anim.World).Top[0].Name; got == "?" || got == "anything typed" {
		t.Errorf("ru: site winner named %q", got)
	}
}

// hidingStore is a store where some players asked to be hidden.
type hidingStore struct {
	*fakeStore
	hidden map[int64]bool
}

func (h hidingStore) Hidden(_ context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	for _, id := range ids {
		if h.hidden[id] {
			out[id] = true
		}
	}
	return out, nil
}

// A player who hid themselves is "Anonymous" with the plain face on the
// final podium, in every DM alike; hiding draws the picture again.
func TestPodiumNamesNobodyWhoHid(t *testing.T) {
	ctx := context.Background()
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	d := round.NewDate(2026, 11, 2)
	clock := zones.LastReveal(d)
	store := &fakeStore{kept: map[string]db.WorldCard{}}
	store.answer(1, "Ana", 3, clock.Add(-3*time.Hour))
	store.answer(2, "Bo", 30, clock.Add(-2*time.Hour))
	hs := hidingStore{fakeStore: store, hidden: map[int64]bool{}}
	var scene *anim.World
	s := &Service{Store: hs, Zones: zones, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return clock },
		Draw: func(_ context.Context, sc anim.Scene) ([]byte, error) {
			scene = sc.(*anim.World)
			return []byte("mp4"), nil
		}}

	shown, err := s.Card(ctx, d, "ru", 0)
	if err != nil || scene.Top[0].Name != "Ana" || scene.Top[0].Face != nil {
		t.Fatalf("before hiding: %v, %+v", err, scene.Top[0])
	}
	hs.hidden[1] = true
	if fresh, _ := s.Fresh(ctx, d, "ru", shown.Version, 0); fresh {
		t.Error("the picture with her name is fresh after she hid")
	}
	c, err := s.Card(ctx, d, "ru", 0)
	if err != nil || c.Version == shown.Version {
		t.Fatalf("hidden card %v, same version %t", err, c.Version == shown.Version)
	}
	if scene.Top[0].Name != "Аноним" || scene.Top[0].Face != nobody || scene.Top[1].Name != "Bo" {
		t.Fatalf("podium = %+v, %+v", scene.Top[0], scene.Top[1])
	}
	// The kept ranking keeps her: her own DM still finds her place.
	if c.World.RankOf(1) != 1 || c.World.Entries[0].Name != "Ana" {
		t.Fatalf("the card's ranking lost her: %+v", c.World.Entries[0])
	}
	// A language the faces don't write gets English.
	if _, err := s.Card(ctx, d, "ja", 0); err != nil || scene.Top[0].Name != "Anonymous" {
		t.Fatalf("ja podium = %v, %+v", err, scene.Top[0])
	}
}

// namingStore is a store that tells which players' groups have had their
// reveal.
type namingStore struct {
	*fakeStore
	open map[int64]bool
}

func (n namingStore) NamedOn(_ context.Context, _ round.Zones, _ round.Date, ids []int64, _ time.Time) (map[int64]bool, error) {
	out := map[int64]bool{}
	for _, id := range ids {
		out[id] = n.open[id]
	}
	return out, nil
}

// Before the day closes everywhere the podium names a player whose groups
// have all had their reveal, and keeps the others' names back; a name
// opening draws the picture again at once, however recent the last one.
func TestPodiumNamesWhoseGroupsHaveRevealed(t *testing.T) {
	ctx := context.Background()
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	d := round.NewDate(2026, 11, 2)
	clock := d.At(19, 0, time.UTC)
	store := &fakeStore{kept: map[string]db.WorldCard{}}
	store.answer(1, "Ana", 3, clock.Add(-3*time.Hour))
	store.answer(2, "Bo", 30, clock.Add(-2*time.Hour))
	ns := namingStore{fakeStore: store, open: map[int64]bool{1: true}}
	var scene *anim.World
	s := &Service{Store: ns, Zones: zones, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return clock },
		Draw: func(_ context.Context, sc anim.Scene) ([]byte, error) {
			scene = sc.(*anim.World)
			return []byte("mp4"), nil
		}}

	c, err := s.Card(ctx, d, "en", 0)
	if err != nil || c.Final || c.Named != [3]bool{true, false, false} {
		t.Fatalf("card %+v, %v", c, err)
	}
	if scene.Top[0].Name != "Ana" || scene.Top[1].Name != "•••" {
		t.Fatalf("podium = %q, %q", scene.Top[0].Name, scene.Top[1].Name)
	}
	clock = clock.Add(time.Minute)
	ns.open[2] = true
	if fresh, _ := s.Fresh(ctx, d, "en", c.Version, 0); fresh {
		t.Error("the picture without his name is fresh after his group's reveal")
	}
	if c2, err := s.Card(ctx, d, "en", 0); err != nil || c2.Version == c.Version || scene.Top[1].Name != "Bo" {
		t.Fatalf("after his reveal: %v, same version %t, %q", err, c2.Version == c.Version, scene.Top[1].Name)
	}
}
