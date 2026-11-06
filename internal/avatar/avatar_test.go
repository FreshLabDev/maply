// SPDX-License-Identifier: Apache-2.0
package avatar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
)

type memStore struct {
	mu    sync.Mutex
	m     map[int64]db.Avatar
	faces int
}

func (s *memStore) AvatarOf(_ context.Context, id int64) (db.Avatar, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.m[id]
	return a, ok, nil
}

func (s *memStore) SetAvatar(_ context.Context, id int64, a db.Avatar) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// As the database does: a photo without an address gets a new one.
	if a.JPEG == nil {
		a.Face = ""
	} else if a.Face == "" {
		s.faces++
		a.Face = fmt.Sprintf("face%d", s.faces)
	}
	s.m[id] = a
	return nil
}

type fakeTG struct {
	mu        sync.Mutex
	photos    map[int64]tg.Photo // missing: no photo
	asked     int
	downloads int
	fail      bool
}

func (f *fakeTG) ProfilePhoto(_ context.Context, id int64, _ int) (tg.Photo, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	if f.fail {
		return tg.Photo{}, false, errors.New("telegram is down")
	}
	p, ok := f.photos[id]
	return p, ok, nil
}

func (f *fakeTG) FileBytes(context.Context, string, int64) ([]byte, error) {
	f.mu.Lock()
	f.downloads++
	f.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, nil)
	return b.Bytes(), err
}

func TestFetchAsksTelegramOnceADay(t *testing.T) {
	now := time.Date(2026, 10, 26, 18, 0, 0, 0, time.UTC)
	store := &memStore{m: map[int64]db.Avatar{}}
	fake := &fakeTG{photos: map[int64]tg.Photo{1: {FileID: "f1", FileUniqueID: "u1"}}}
	s := &Service{Store: store, TG: fake, Now: func() time.Time { return now }}

	got := s.Fetch(context.Background(), []int64{1, 2})
	if len(got) != 1 || got[1] == nil {
		t.Fatalf("Fetch = %v, want the photo of 1 only", got)
	}
	img, err := jpeg.Decode(bytes.NewReader(got[1]))
	if err != nil || img.Bounds().Dx() != Side || img.Bounds().Dy() != Side {
		t.Fatalf("kept photo is not a %d px square: %v %v", Side, img.Bounds(), err)
	}
	if fake.asked != 2 || fake.downloads != 1 {
		t.Fatalf("asked %d, downloaded %d; want 2 and 1", fake.asked, fake.downloads)
	}
	// The same day nobody is asked again, the one without a photo included.
	s.Fetch(context.Background(), []int64{1, 2})
	if fake.asked != 2 {
		t.Errorf("asked again the same day: %d", fake.asked)
	}
	// A day on, the same photo is not downloaded again and keeps its
	// address.
	first := store.m[1].Face
	now = now.Add(25 * time.Hour)
	s.Fetch(context.Background(), []int64{1})
	if fake.asked != 3 || fake.downloads != 1 || store.m[1].Face != first {
		t.Errorf("a day on: asked %d, downloaded %d, face %q then %q; want 3, 1 and the same face", fake.asked, fake.downloads, first, store.m[1].Face)
	}
	// A new photo is, under a new address.
	now = now.Add(25 * time.Hour)
	fake.photos[1] = tg.Photo{FileID: "f2", FileUniqueID: "u2"}
	s.Fetch(context.Background(), []int64{1})
	if fake.downloads != 2 || store.m[1].Face == first {
		t.Errorf("a new photo: downloaded %d, face %q", fake.downloads, store.m[1].Face)
	}
	// Telegram failing keeps the photo there was.
	now = now.Add(25 * time.Hour)
	fake.fail = true
	if got := s.Fetch(context.Background(), []int64{1}); got[1] == nil {
		t.Errorf("a failing Telegram lost the kept photo")
	}
	if k := s.Kept(context.Background(), []int64{1, 2}); len(k) != 1 || k[1] == nil {
		t.Errorf("Kept = %v", k)
	}
}

// Refresh asks in the background, once an hour at most per person, and
// leaves a service without Telegram alone.
func TestRefreshAsksInTheBackground(t *testing.T) {
	now := time.Date(2026, 11, 6, 18, 0, 0, 0, time.UTC)
	store := &memStore{m: map[int64]db.Avatar{}}
	fake := &fakeTG{photos: map[int64]tg.Photo{1: {FileID: "f1", FileUniqueID: "u1"}}}
	s := &Service{Store: store, TG: fake, Now: func() time.Time { return now }}

	s.Refresh([]int64{1, 2, 0})
	deadline := time.Now().Add(5 * time.Second)
	for len(s.Kept(context.Background(), []int64{1})) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the photo of 1 was not kept")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		store.mu.Lock()
		_, two := store.m[2]
		store.mu.Unlock()
		if two {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("2 was not asked")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Refresh([]int64{1, 2})
	time.Sleep(50 * time.Millisecond)
	fake.mu.Lock()
	asked := fake.asked
	fake.mu.Unlock()
	if asked != 2 {
		t.Errorf("asked %d times, want 2: the same hour asks nobody again", asked)
	}

	(&Service{Store: store}).Refresh([]int64{3})
	if got := (&Service{Store: store}).Fetch(context.Background(), []int64{1, 3}); len(got) != 1 || got[1] == nil {
		t.Errorf("Fetch without Telegram = %v, want the kept photo of 1", got)
	}
}

func TestSquareTakesTheMiddle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 100))
	for x := range 300 {
		for y := range 100 {
			c := color.RGBA{0, 0, 0, 255}
			if x >= 100 && x < 200 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	out, err := Square(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sq, _ := jpeg.Decode(bytes.NewReader(out))
	if r, _, _, _ := sq.At(Side/2, Side/2).RGBA(); r < 0xe000 {
		t.Errorf("the middle of the square is not the middle of the photo: %x", r)
	}
}
