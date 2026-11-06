// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"image"
	"image/color"
	"testing"
)

func TestFacePaintsTheMiddleSquareAsADisc(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 30, 10))
	for x := range 30 {
		for y := range 10 {
			c := color.RGBA{0, 0, 0, 255}
			if x >= 10 && x < 20 {
				c = color.RGBA{255, 0, 0, 255}
			}
			img.Set(x, y, c)
		}
	}
	f := NewFace(img)
	if f == nil || f.N != 10 {
		t.Fatalf("face %+v", f)
	}
	c := NewCanvas(40, 40)
	c.Fill(RGB{0, 0, 1})
	c.Face(f, 20, 20, 10, 1, nil)
	at := func(x, y int) []float32 { i := 3 * (y*40 + x); return c.Pix[i : i+3] }
	if p := at(20, 20); p[0] < 0.99 || p[2] > 0.01 {
		t.Errorf("the middle is %v, want the photo's red", p)
	}
	if p := at(2, 2); p[2] < 0.99 {
		t.Errorf("outside the disc is %v, want the canvas", p)
	}
}
