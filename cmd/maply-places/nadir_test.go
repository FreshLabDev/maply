// SPDX-License-Identifier: Apache-2.0

package main

import (
	"image"
	"image/color"
	"testing"
)

func TestMaskNadirPaintsOverTheLogo(t *testing.T) {
	const w, h = 256, 128
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	ground := color.RGBA{R: 90, G: 90, B: 90, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.SetRGBA(x, y, ground)
		}
	}
	// A logo straight down, where the camera's mount is.
	logo := color.RGBA{R: 255, G: 220, B: 0, A: 255}
	for y := h - 20; y < h; y++ {
		for x := 0; x < w; x++ {
			src.SetRGBA(x, y, logo)
		}
	}

	got := maskNadir(src)
	cut := h * 3 / 4
	for y := cut; y < h; y++ {
		for x := 0; x < w; x += 7 {
			if c := got.RGBAAt(x, y); c != ground {
				t.Fatalf("(%d, %d) = %v below the cut, want the ground %v", x, y, c, ground)
			}
		}
	}
	if c := got.RGBAAt(10, cut/2); c != ground {
		t.Fatalf("above the cut = %v, want it untouched", c)
	}
}
