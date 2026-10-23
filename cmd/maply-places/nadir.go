// SPDX-License-Identifier: Apache-2.0

package main

import (
	"image"
	"image/color"
	"image/draw"
)

// nadirPitch is the pitch below which a panorama is painted over. A 360
// camera sees its own mount and the car roof there, and capture projects
// stick their logo over it: a government's crest or a city council's name
// gives the place away to anyone who looks down or downloads the picture.
const nadirPitch = -45.0

// maskNadir paints everything below nadirPitch of an equirectangular frame
// with the colours just above it, smoothed along the horizon, fading into
// their mean towards the bottom. The pixels below are gone from the file, not
// only hidden in the viewer.
func maskNadir(src image.Image) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	cut := int(float64(h) * (90 - nadirPitch) / 180)
	if w == 0 || cut <= 0 || cut >= h {
		return dst
	}

	// The colour of each column just above the cut.
	band := max(1, h/256)
	col := make([][3]float64, w)
	for x := 0; x < w; x++ {
		for y := cut - band; y < cut; y++ {
			c := dst.RGBAAt(x, max(y, 0))
			col[x][0] += float64(c.R)
			col[x][1] += float64(c.G)
			col[x][2] += float64(c.B)
		}
		for i := range col[x] {
			col[x][i] /= float64(band)
		}
	}
	// A wide box blur around the horizon, which wraps: no detail of the
	// border survives into the fill.
	r := max(1, w/24)
	soft := make([][3]float64, w)
	var mean [3]float64
	for x := 0; x < w; x++ {
		for d := -r; d <= r; d++ {
			c := col[((x+d)%w+w)%w]
			for i := range c {
				soft[x][i] += c[i]
			}
		}
		for i := range soft[x] {
			soft[x][i] /= float64(2*r + 1)
			mean[i] += soft[x][i] / float64(w)
		}
	}

	feather := max(1, h/128)
	fade := max(1, h/16)
	for y := cut - feather; y < h; y++ {
		if y < 0 {
			continue
		}
		for x := 0; x < w; x++ {
			fill := soft[x]
			if y >= cut {
				t := min(1, float64(y-cut)/float64(fade))
				for i := range fill {
					fill[i] = fill[i]*(1-t) + mean[i]*t
				}
			}
			a := 1.0
			if y < cut {
				a = float64(y-(cut-feather)+1) / float64(feather+1)
			}
			o := dst.RGBAAt(x, y)
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(float64(o.R)*(1-a) + fill[0]*a + 0.5),
				G: uint8(float64(o.G)*(1-a) + fill[1]*a + 0.5),
				B: uint8(float64(o.B)*(1-a) + fill[2]*a + 0.5),
				A: 255,
			})
		}
	}
	return dst
}
