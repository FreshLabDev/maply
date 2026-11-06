// SPDX-License-Identifier: Apache-2.0

// Package avatar keeps the players' profile photos for the results: the pins
// of the animation, the card and the Mini App's map show the person instead
// of a letter. A photo is asked of Telegram at most once a day per person and
// kept small; a person with none the bot may see keeps their letter.
package avatar

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"log/slog"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

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

// Refresh's queue: how many people wait at most, how long one is not queued
// again, how many it remembers, and how long one ask may take.
const (
	queueSize = 512
	requeue   = time.Hour
	remember  = 20000
	askWait   = 20 * time.Second
)

type Store interface {
	AvatarOf(ctx context.Context, userID int64) (db.Avatar, bool, error)
	SetAvatar(ctx context.Context, userID int64, a db.Avatar) error
}

type Telegram interface {
	ProfilePhoto(ctx context.Context, userID int64, minSide int) (tg.Photo, bool, error)
	FileBytes(ctx context.Context, fileID string, max int64) ([]byte, error)
}

// batch is a Store that reads many kept photos in one query.
type batch interface {
	AvatarsOf(ctx context.Context, ids []int64) (map[int64][]byte, error)
}

// owned is a Store that keeps the photos people chose themselves, which
// maply shows over Telegram's.
type owned interface {
	OwnPhotos(ctx context.Context, ids []int64) (map[int64][]byte, error)
}

// addressed is a Store that gives each kept photo an address of its own.
type addressed interface {
	FacesOf(ctx context.Context, ids []int64) (map[int64]string, error)
	FaceJPEG(ctx context.Context, face string) ([]byte, bool, error)
}

type Service struct {
	Store Store
	// TG asks Telegram; nil keeps only what is kept.
	TG  Telegram
	Log *slog.Logger
	Now func() time.Time

	start  sync.Once
	queue  chan int64
	mu     sync.Mutex
	queued map[int64]time.Time
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
	if s == nil || len(ids) == 0 {
		return out
	}
	if b, ok := s.Store.(batch); ok {
		m, err := b.AvatarsOf(ctx, ids)
		if err != nil {
			s.log().Warn("avatars not read", "users", len(ids), "error", err)
			return out
		}
		return m
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

// Faces is the addresses of the photos kept for ids, without asking
// Telegram: /face/<face>.jpg names each. People without one are left out, and
// so is everyone when the store gives photos no address.
func (s *Service) Faces(ctx context.Context, ids []int64) map[int64]string {
	out := map[int64]string{}
	a, ok := s.addressed()
	if !ok || len(ids) == 0 {
		return out
	}
	m, err := a.FacesOf(ctx, ids)
	if err != nil {
		s.log().Warn("faces not read", "users", len(ids), "error", err)
		return out
	}
	return m
}

// Face is the photo at the address face, or nothing.
func (s *Service) Face(ctx context.Context, face string) ([]byte, bool) {
	a, ok := s.addressed()
	if !ok {
		return nil, false
	}
	b, ok, err := a.FaceJPEG(ctx, face)
	if err != nil {
		s.log().Warn("face not read", "error", err)
		return nil, false
	}
	return b, ok
}

func (s *Service) addressed() (addressed, bool) {
	if s == nil {
		return nil, false
	}
	a, ok := s.Store.(addressed)
	return a, ok
}

// Fetch is the photos of ids, asking Telegram for the ones not asked today;
// a photo a person chose themselves comes over Telegram's.
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
	if o, ok := s.Store.(owned); ok && len(ids) > 0 {
		own, err := o.OwnPhotos(ctx, ids)
		if err != nil {
			s.log().Warn("own photos not read", "users", len(ids), "error", err)
		}
		for id, b := range own {
			out[id] = b
		}
	}
	return out
}

func (s *Service) one(ctx context.Context, id int64) []byte {
	kept, ok, err := s.Store.AvatarOf(ctx, id)
	if err != nil {
		s.log().Warn("avatar not read", "user", id, "error", err)
		return nil
	}
	now := s.now()
	if ok && now.Sub(kept.CheckedAt) < fresh || s.TG == nil {
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
		next.UniqueID, next.JPEG, next.Face = kept.UniqueID, kept.JPEG, kept.Face
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

// Refresh asks Telegram, in the background, for the photos of ids not asked
// today, so that a page shows the face a person has now and a person never
// seen in a result gets one. It never waits: ids queued within the hour, or
// more than the queue holds, are left for a later page.
func (s *Service) Refresh(ids []int64) {
	if s == nil || s.TG == nil || len(ids) == 0 {
		return
	}
	s.start.Do(func() {
		s.queue = make(chan int64, queueSize)
		go s.drain()
	})
	now := s.now()
	var add []int64
	s.mu.Lock()
	if s.queued == nil || len(s.queued) > remember {
		s.queued = map[int64]time.Time{}
	}
	for _, id := range ids {
		if at, ok := s.queued[id]; id == 0 || ok && now.Sub(at) < requeue {
			continue
		}
		s.queued[id] = now
		add = append(add, id)
	}
	s.mu.Unlock()
	for i, id := range add {
		select {
		case s.queue <- id:
		default:
			s.mu.Lock()
			for _, left := range add[i:] {
				delete(s.queued, left)
			}
			s.mu.Unlock()
			return
		}
	}
}

// drain asks for the queued people one at a time, for as long as the
// process runs.
func (s *Service) drain() {
	for id := range s.queue {
		ctx, cancel := context.WithTimeout(context.Background(), askWait)
		s.one(ctx, id)
		cancel()
	}
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

// OwnSide is the side of a photo a person chose themselves: the profile's
// head draws it larger than any result does.
const OwnSide = 256

// OwnLimit bounds the side of a photo a person sends, before it is read
// whole: a small file may claim a huge picture.
const OwnLimit = 4096

// ErrPhoto is a file that is not a JPEG, PNG or WebP picture maply takes.
var ErrPhoto = errors.New("avatar: not a photo maply takes")

// Own is a photo a person sent as their own, the middle square of it,
// OwnSide pixels, as a JPEG without anything the file carried besides the
// picture. A see-through part becomes white.
func Own(raw []byte) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrPhoto
	}
	switch format {
	case "jpeg", "png", "webp":
	default:
		return nil, ErrPhoto
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > OwnLimit || cfg.Height > OwnLimit {
		return nil, ErrPhoto
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrPhoto
	}
	b := img.Bounds()
	n := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-n)/2, b.Min.Y+(b.Dy()-n)/2
	src := image.Rect(x0, y0, x0+n, y0+n)
	dst := image.NewRGBA(image.Rect(0, 0, OwnSide, OwnSide))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, src, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 86}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
