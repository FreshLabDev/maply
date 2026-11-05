// SPDX-License-Identifier: Apache-2.0
package thumb

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

type Store interface {
	Undrawn(ctx context.Context, recipe int) ([]db.Undrawn, error)
	PutThumb(ctx context.Context, t db.Thumb) error
}

// Service draws the strips of the days kept closed. The final world job
// keeps a day once it is over in every zone; the service finds it on its
// next pass and draws it, and draws the older days that have none.
type Service struct {
	Store     Store
	ImagesDir string
	Log       *slog.Logger

	// failed holds when a day's picture would not draw, so a file missing
	// is logged every failedRetry and not on every pass.
	failed map[round.Date]time.Time
}

// failedRetry is how long a day whose picture would not draw waits before
// it is tried again: a file put back by hand is drawn the same day.
const failedRetry = 6 * time.Hour

// Run draws what is missing now and then every so often, until ctx ends.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	for {
		if _, err := s.DrawAll(ctx); err != nil && ctx.Err() == nil {
			s.Log.Error("day strips not drawn", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// DrawAll draws the strip of every kept day without one, one day at a time:
// a panorama decoded takes 32 MB.
func (s *Service) DrawAll(ctx context.Context) (int, error) {
	days, err := s.Store.Undrawn(ctx, Recipe)
	if err != nil {
		return 0, err
	}
	drawn := 0
	for _, d := range days {
		if ctx.Err() != nil {
			return drawn, ctx.Err()
		}
		if at, ok := s.failed[d.Date]; ok && time.Since(at) < failedRetry {
			continue
		}
		t, err := s.Draw(d)
		if err != nil {
			if s.failed == nil {
				s.failed = map[round.Date]time.Time{}
			}
			s.failed[d.Date] = time.Now()
			// The error names the image by its random key at most: nothing
			// about the place, which is out anyway.
			s.Log.Warn("day strip not drawn", "date", d.Date, "error", err)
			continue
		}
		if err := s.Store.PutThumb(ctx, t); errors.Is(err, db.ErrNotKept) {
			continue
		} else if err != nil {
			return drawn, err
		}
		drawn++
	}
	if drawn > 0 {
		s.Log.Info("day strips drawn", "n", drawn)
	}
	return drawn, nil
}

// Draw makes both sizes of a day's strip under a new random key.
func (s *Service) Draw(d db.Undrawn) (db.Thumb, error) {
	if s.ImagesDir == "" {
		return db.Thumb{}, errors.New("no images directory")
	}
	name := filepath.Base(d.ImageKey)
	f, err := os.Open(filepath.Join(s.ImagesDir, name))
	if err != nil {
		// The key finds the file; the server's paths stay out of the log.
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return db.Thumb{}, fmt.Errorf("image %s: %w", name, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return db.Thumb{}, fmt.Errorf("decode: %w", err)
	}
	src := rgba(img)
	yaw := 0.0
	if d.Kind == "panorama" && Pano(src) {
		if d.Yaw != nil {
			yaw = *d.Yaw
		} else {
			yaw = Pick(src)
		}
	}
	small, err := JPEG(Frame(src, yaw, Small), Small)
	if err != nil {
		return db.Thumb{}, err
	}
	wide, err := JPEG(Frame(src, yaw, Wide), Wide)
	if err != nil {
		return db.Thumb{}, err
	}
	key, err := NewKey()
	if err != nil {
		return db.Thumb{}, err
	}
	return db.Thumb{Date: d.Date, Key: key, Recipe: Recipe, Small: small, Wide: wide}, nil
}

// NewKey is a random name for a strip, 20 letters and digits as the place
// images have: it says nothing about the day.
func NewKey() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 20)
	for i := range b {
		k, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[k.Int64()]
	}
	return string(b), nil
}
