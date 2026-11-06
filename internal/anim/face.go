// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"image"
	"math"
)

// Face is a person's profile photo, cut square and kept as sRGB floats so a
// frame samples it without converting.
type Face struct {
	N   int
	Pix []float32 // r, g, b per pixel, N×N, rows top to bottom
}

// NewFace takes the middle square of img. Nil for an empty image.
func NewFace(img image.Image) *Face {
	b := img.Bounds()
	n := min(b.Dx(), b.Dy())
	if n <= 0 {
		return nil
	}
	x0, y0 := b.Min.X+(b.Dx()-n)/2, b.Min.Y+(b.Dy()-n)/2
	f := &Face{N: n, Pix: make([]float32, 3*n*n)}
	for y := range n {
		for x := range n {
			r, g, bl, _ := img.At(x0+x, y0+y).RGBA()
			i := 3 * (y*n + x)
			f.Pix[i], f.Pix[i+1], f.Pix[i+2] = float32(r)/0xffff, float32(g)/0xffff, float32(bl)/0xffff
		}
	}
	return f
}

// sample is the face at u, v in 0..1, bilinear.
func (f *Face) sample(u, v float64) (float32, float32, float32) {
	x := min(max(u*float64(f.N)-0.5, 0), float64(f.N-1))
	y := min(max(v*float64(f.N)-0.5, 0), float64(f.N-1))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, f.N-1), min(y0+1, f.N-1)
	fx, fy := float32(x-float64(x0)), float32(y-float64(y0))
	at := func(x, y int) []float32 { i := 3 * (y*f.N + x); return f.Pix[i : i+3 : i+3] }
	a, b, c, d := at(x0, y0), at(x1, y0), at(x0, y1), at(x1, y1)
	var out [3]float32
	for k := range 3 {
		top := a[k] + (b[k]-a[k])*fx
		bot := c[k] + (d[k]-c[k])*fx
		out[k] = top + (bot-top)*fy
	}
	return out[0], out[1], out[2]
}

// Face paints f as a disc of radius r around cx, cy.
func (c *Canvas) Face(f *Face, cx, cy, r, alpha float64, clip *Mask) {
	if f == nil || r <= 0 || alpha <= 0 {
		return
	}
	x0, y0 := max(int(math.Floor(cx-r-1)), 0), max(int(math.Floor(cy-r-1)), 0)
	x1, y1 := min(int(math.Ceil(cx+r+1)), c.W), min(int(math.Ceil(cy+r+1)), c.H)
	a := float32(alpha)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			k := cover(math.Hypot(px-cx, py-cy)-r) * a
			if clip != nil {
				k *= clip.A[y*c.W+x]
			}
			if k <= 0 {
				continue
			}
			sr, sg, sb := f.sample((px-cx+r)/(2*r), (py-cy+r)/(2*r))
			p := c.Pix[3*(y*c.W+x) : 3*(y*c.W+x)+3 : 3*(y*c.W+x)+3]
			p[0] += (sr - p[0]) * k
			p[1] += (sg - p[1]) * k
			p[2] += (sb - p[2]) * k
		}
	}
}
