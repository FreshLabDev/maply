// SPDX-License-Identifier: Apache-2.0

// Package avatar keeps the players' profile photos for the results: the pins
// of the animation, the card and the Mini App's map show the person instead
// of a letter. A photo is asked of Telegram at most once a day per person and
// kept small; a person with none the bot may see keeps their letter.
package avatar

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	_ "image/png"
	"log/slog"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"
	"golang.org/x/image/draw"

	"github.com/FreshLabDev/maply/internal/db"
)

// Side is the kept photo's side in pixels: twice the largest place it is
// drawn, the podium's.
const Side = 128

// fresh is how long a kept answer from Telegram stands, a photo or none.
const fresh = 24 * time.Hour

// maxBytes bounds a download; Telegram's 320 px photo is some 20 KB.
const maxBytes = 2 << 20

// workers is how many people are asked of Telegram at once.
const workers = 4

type Store interface {
	AvatarOf(ctx context.Context, userID int64) (db.Avatar, bool, error)
	SetAvatar(ctx context.Context, userID int64, a db.Avatar) error
}

type Telegram interface {
	ProfilePhoto(ctx context.Context, userID int64, minSide int) (tg.Photo, bool, error)
	FileBytes(ctx context.Context, fileID string, max int64) ([]byte, error)
}

type Service struct {
	Store Store
	TG    Telegram
	Log   *slog.Logger
	Now   func() time.Time
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Kept is the photos already kept for ids, without asking Telegram: for a
// page that must answer at once. People without one are left out.
func (s *Service) Kept(ctx context.Context, ids []int64) map[int64][]byte {
	out := map[int64][]byte{}
	if s == nil {
		return out
	}
	for _, id := range ids {
		a, ok, err := s.Store.AvatarOf(ctx, id)
		if err != nil {
			s.log().Warn("avatar not read", "user", id, "error", err)
			return out
		}
		if ok && a.JPEG != nil {
			out[id] = a.JPEG
		}
	}
	return out
}

// Fetch is the photos of ids, asking Telegram for the ones not asked today.
// Whatever fails keeps the photo kept before, or none: a letter instead of a
// face is never worth a missing result.
func (s *Service) Fetch(ctx context.Context, ids []int64) map[int64][]byte {
	out := map[int64][]byte{}
	if s == nil {
		return out
	}
	var mu sync.Mutex
	work := make(chan int64)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for id := range work {
				if b := s.one(ctx, id); b != nil {
					mu.Lock()
					out[id] = b
					mu.Unlock()
				}
			}
		})
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		work <- id
	}
	close(work)
	wg.Wait()
	return out
}

func (s *Service) one(ctx context.Context, id int64) []byte {
	kept, ok, err := s.Store.AvatarOf(ctx, id)
	if err != nil {
		s.log().Warn("avatar not read", "user", id, "error", err)
		return nil
	}
	now := s.now()
	if ok && now.Sub(kept.CheckedAt) < fresh {
		return kept.JPEG
	}
	photo, has, err := s.TG.ProfilePhoto(ctx, id, 2*Side)
	if err != nil {
		s.log().Info("profile photo not asked", "user", id, "error", err)
		return kept.JPEG
	}
	next := db.Avatar{CheckedAt: now}
	switch {
	case !has:
	case ok && photo.FileUniqueID == kept.UniqueID && kept.JPEG != nil:
		next.UniqueID, next.JPEG = kept.UniqueID, kept.JPEG
	default:
		raw, err := s.TG.FileBytes(ctx, photo.FileID, maxBytes)
		if err != nil {
			s.log().Info("profile photo not downloaded", "user", id, "error", err)
			return kept.JPEG
		}
		small, err := Square(raw)
		if err != nil {
			s.log().Info("profile photo not read", "user", id, "error", err)
			return kept.JPEG
		}
		next.UniqueID, next.JPEG = photo.FileUniqueID, small
	}
	if err := s.Store.SetAvatar(ctx, id, next); err != nil {
		s.log().Warn("avatar not kept", "user", id, "error", err)
	}
	return next.JPEG
}

// Square is the middle square of a photo, Side pixels, as a JPEG.
func Square(raw []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	n := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-n)/2, b.Min.Y+(b.Dy()-n)/2
	src := image.Rect(x0, y0, x0+n, y0+n)
	dst := image.NewRGBA(image.Rect(0, 0, Side, Side))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, src, draw.Src, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
