// SPDX-License-Identifier: Apache-2.0

// Package thumb draws the strip each closed day shows on the days page: the
// band of the day's panorama from 8° below the horizon to 22° above it, as a
// camera facing the place's thumb_yaw would see it. Below the band are the
// road, the car's hood and the head of whoever carried the camera; above it
// mostly sky. A photo is cropped to the same shape instead.
//
// A strip is drawn once, when the day is over in every zone, and kept in the
// database under a random key (see migrations/017_day_thumb.sql).
package thumb

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"math"

	xdraw "golang.org/x/image/draw"
)

// Recipe changes with the drawing, so a strip of an older one can be told
// apart and drawn again.
const Recipe = 1

// The band, in degrees of pitch.
const (
	Low  = -8.0
	High = 22.0
)

// Size is a strip's pixels and the JPEG quality it is kept at.
type Size struct{ W, H, Quality int }

var (
	// Small is a row of the list, 88×56 in the Mini App and 132×84 on a wide
	// screen: 4–8 KB on the panoramas of the curation, 6 KB in the middle.
	Small = Size{220, 140, 65}
	// Wide is the top of the day's page, 25–50 KB.
	Wide = Size{800, 300, 62}
	// Card is the picture of a link to a day's page, under its texts:
	// Open Graph's 1.91:1, with more of the street than the strips have.
	Card = Size{1200, 630, 82}
)

// The card's band: the street as one stands in it, about 75° across.
const (
	CardLow  = -14.0
	CardHigh = 30.0
)

// Pano reports whether img is an equirectangular panorama, 2:1.
func Pano(img image.Image) bool {
	b := img.Bounds()
	return b.Dx() == 2*b.Dy()
}

// Frame draws the band of img facing yaw, in degrees clockwise from the
// centre of the image. A panorama is projected as a camera with that view
// sees it, straight lines staying straight; anything else is a photo and is
// cropped, its bottom first, where the hood is.
func Frame(img image.Image, yaw float64, size Size) *image.RGBA {
	return View(img, yaw, Low, High, size)
}

// View is Frame with another band, low to high degrees of pitch.
func View(img image.Image, yaw, low, high float64, size Size) *image.RGBA {
	src := rgba(img)
	if !Pano(src) {
		return crop(src, size)
	}
	// Drawn at twice the size and brought down: the sampling is bilinear,
	// and a strip seen whole wants the panorama's detail averaged, not
	// picked.
	big := project(src, yaw, low, high, Size{W: 2 * size.W, H: 2 * size.H})
	out := image.NewRGBA(image.Rect(0, 0, size.W, size.H))
	xdraw.CatmullRom.Scale(out, out.Rect, big, big.Rect, xdraw.Src, nil)
	return out
}

// JPEG encodes a strip at its size's quality. Encoding from pixels writes no
// EXIF.
func JPEG(img image.Image, size Size) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: size.Quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func rgba(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Rect, img, b.Min, draw.Src)
	return out
}

// project renders a rectilinear view of the panorama src: the vertical
// field is the band from low to high, and the horizontal one follows from
// the size.
func project(src *image.RGBA, yaw, low, high float64, size Size) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size.W, size.H))
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	t := math.Tan((high - low) / 2 * math.Pi / 180)
	aspect := float64(size.W) / float64(size.H)
	pitch := (high + low) / 2 * math.Pi / 180
	sp, cp := math.Sincos(pitch)
	yawR := yaw * math.Pi / 180
	for j := 0; j < size.H; j++ {
		y := (1 - 2*(float64(j)+0.5)/float64(size.H)) * t
		for i := 0; i < size.W; i++ {
			x := (2*(float64(i)+0.5)/float64(size.W) - 1) * aspect * t
			// The camera looks along z; tilting it up by pitch turns y and z.
			yy := y*cp + sp
			zz := -y*sp + cp
			lon := math.Atan2(x, zz) + yawR
			lat := math.Atan2(yy, math.Hypot(x, zz))
			u := 0.5 + lon/(2*math.Pi)
			v := 0.5 - lat/math.Pi
			r, g, b := bilinear(src, u*float64(sw), v*float64(sh))
			o := out.PixOffset(i, j)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = r, g, b, 255
		}
	}
	return out
}

// bilinear samples src at pixel coordinates x, y: x wraps round the
// panorama, y stops at the poles.
func bilinear(src *image.RGBA, x, y float64) (r, g, b uint8) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	x -= 0.5
	y -= 0.5
	fx, fy := math.Floor(x), math.Floor(y)
	tx, ty := x-fx, y-fy
	x0 := int(fx) % w
	if x0 < 0 {
		x0 += w
	}
	x1 := (x0 + 1) % w
	y0, y1 := min(max(int(fy), 0), h-1), min(max(int(fy)+1, 0), h-1)
	a, c := src.PixOffset(x0, y0), src.PixOffset(x1, y0)
	d, e := src.PixOffset(x0, y1), src.PixOffset(x1, y1)
	p := src.Pix
	mix := func(k int) uint8 {
		top := float64(p[a+k]) + (float64(p[c+k])-float64(p[a+k]))*tx
		bot := float64(p[d+k]) + (float64(p[e+k])-float64(p[d+k]))*tx
		return uint8(top + (bot-top)*ty + 0.5)
	}
	return mix(0), mix(1), mix(2)
}

// crop cuts a photo to the strip's shape: the full width when it is taller
// than the strip, with more taken from the bottom than the top.
func crop(src *image.RGBA, size Size) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	aspect := float64(size.W) / float64(size.H)
	r := src.Rect
	if float64(w)/float64(h) > aspect {
		cw := int(math.Round(float64(h) * aspect))
		x0 := (w - cw) / 2
		r = image.Rect(x0, 0, x0+cw, h)
	} else {
		ch := int(math.Round(float64(w) / aspect))
		y0 := int(float64(h-ch) * 0.35)
		r = image.Rect(0, y0, w, y0+ch)
	}
	out := image.NewRGBA(image.Rect(0, 0, size.W, size.H))
	xdraw.CatmullRom.Scale(out, out.Rect, src, r, xdraw.Src, nil)
	return out
}

// pickStep is how far apart the directions Pick tries are.
const pickStep = 15

// Pick is the direction a panorama's strip looks best in, in degrees
// clockwise from the centre of the image, a multiple of pickStep. The best
// view has detail and some sky: a street seen along, not a wall seen close.
// Each direction counts with its neighbours, so a narrow gap between two
// walls does not win. A photo has one direction, 0.
func Pick(img image.Image) float64 {
	src := rgba(img)
	if !Pano(src) {
		return 0
	}
	n := 360 / pickStep
	scores := make([]float64, n)
	for k := range n {
		scores[k] = score(project(src, float64(k*pickStep), Low, High, Size{W: 132, H: 84}))
	}
	best, bestAt := -1.0, 0
	for k := range n {
		s := 0.5*scores[k] + 0.25*scores[(k+n-1)%n] + 0.25*scores[(k+1)%n]
		if s > best {
			best, bestAt = s, k
		}
	}
	return float64(bestAt * pickStep)
}

// score rates one view: its detail, the share of sky in its top third, its
// colour, and how dark it is.
func score(img *image.RGBA) float64 {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	lum := make([]float64, w*h)
	var rg, yb, rg2, yb2, sum float64
	sky, top := 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := img.PixOffset(x, y)
			r, g, b := float64(img.Pix[o])/255, float64(img.Pix[o+1])/255, float64(img.Pix[o+2])/255
			l := 0.2126*r + 0.7152*g + 0.0722*b
			lum[y*w+x] = l
			sum += l
			a, c := r-g, 0.5*(r+g)-b
			rg, yb, rg2, yb2 = rg+a, yb+c, rg2+a*a, yb2+c*c
			if y < h/3 {
				top++
				hi, lo := max(r, g, b), min(r, g, b)
				sat := 0.0
				if hi > 0 {
					sat = (hi - lo) / hi
				}
				if (l > 0.62 && sat < 0.22) || (b > r+0.04 && b >= g && l > 0.38) {
					sky++
				}
			}
		}
	}
	n := float64(w * h)
	var detail float64
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			gx := lum[y*w+x+1] - lum[y*w+x-1]
			gy := lum[(y+1)*w+x] - lum[(y-1)*w+x]
			detail += math.Hypot(gx, gy)
		}
	}
	detail /= float64((w - 2) * (h - 2))
	mrg, myb := rg/n, yb/n
	colour := math.Sqrt(max(0, rg2/n-mrg*mrg)+max(0, yb2/n-myb*myb)) + 0.3*math.Hypot(mrg, myb)
	skyShare := float64(sky) / float64(top)
	open := 0.55 + 0.9*min(skyShare, 0.45)
	if skyShare > 0.9 {
		// Nothing but sky: the camera looks over a fence or a cliff.
		open *= 0.6
	}
	dark := 1.0
	if m := sum / n; m < 0.18 {
		dark = 0.4 + 3*m
	}
	return detail * open * (0.75 + colour) * dark
}
