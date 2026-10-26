// SPDX-License-Identifier: Apache-2.0
package world

import (
	"context"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/anim"
)

// MAPLY_WORLD_PREVIEW=out.mp4 go test -run TestPreview ./internal/world/
func TestPreview(t *testing.T) {
	out := os.Getenv("MAPLY_WORLD_PREVIEW")
	if out == "" {
		t.Skip("set MAPLY_WORLD_PREVIEW to draw a sample")
	}
	store := &fakeStore{}
	at := time.Date(2026, 11, 2, 6, 0, 0, 0, time.UTC)
	store.answer(1, "_amti", 0.002, at.Add(9*time.Hour))
	store.answer(2, "Tochka 🍂", 2.3, at.Add(time.Hour))
	store.answer(3, "Ana", 183, at.Add(3*time.Hour))
	store.answer(4, "Bo", 950, at.Add(5*time.Hour))
	store.pins[0].Lat, store.pins[0].Lon = 38.72, -9.14
	store.pins[1].Lat, store.pins[1].Lon = 38.70, -9.10
	store.pins[2].Lat, store.pins[2].Lon = 40.4, -3.7
	store.pins[3].Lat, store.pins[3].Lon = 48.8, 2.3
	faces := map[int64]*anim.Face{}
	for id, path := range map[int64]string{1: os.Getenv("MAPLY_WORLD_FACE1"), 3: os.Getenv("MAPLY_WORLD_FACE2")} {
		if f, err := os.Open(path); err == nil {
			if img, err := jpeg.Decode(f); err == nil {
				faces[id] = anim.NewFace(img)
			}
			f.Close()
		}
	}
	a, _ := store.Answer(context.Background(), 1)
	if _, err := anim.Encode(context.Background(), Scene("ru", 4, a, store.world, store.pins, faces), out, anim.Options{FFmpeg: "ffmpeg"}); err != nil {
		t.Fatal(err)
	}
}
