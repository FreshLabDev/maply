// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"image"
	"math"
	"strconv"

	"github.com/go-text/typesetting/language"
)

// RGB is a colour in sRGB, each channel 0..1. Colours blend in sRGB, as the
// browser canvas the posts were designed in does.
type RGB struct{ R, G, B float32 }

// Hex parses "#RRGGBB".
func Hex(s string) RGB {
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil || len(s) != 7 {
		panic("anim: bad colour " + s)
	}
	return RGB{float32(n>>16) / 255, float32(n>>8&255) / 255, float32(n&255) / 255}
}

// Mix goes from a to b as t goes from 0 to 1.
func (a RGB) Mix(b RGB, t float64) RGB {
	f := float32(t)
	return RGB{a.R + (b.R-a.R)*f, a.G + (b.G-a.G)*f, a.B + (b.B-a.B)*f}
}

// Canvas is one frame being drawn: opaque sRGB pixels and the scratch space
// its shapes are rasterised in. A canvas belongs to one goroutine.
type Canvas struct {
	W, H  int
	Pix   []float32         // r, g, b per pixel, rows top to bottom
	Lang  language.Language // of the text drawn on it
	mask  *Mask             // scratch for one shape or layer at a time
	layer *Mask             // a second one, for two layers drawn together
	clip  *Mask             // the clip of the frame's window, kept while it draws
	text  textScratch
	bg    []float32
	bgKey string
	pano  panoScratch
}

func NewCanvas(w, h int) *Canvas {
	return &Canvas{W: w, H: h, Pix: make([]float32, 3*w*h), mask: NewMask(w, h), layer: NewMask(w, h), clip: NewMask(w, h)}
}

// Fill paints the whole canvas one colour.
func (c *Canvas) Fill(col RGB) {
	for i := 0; i < len(c.Pix); i += 3 {
		c.Pix[i], c.Pix[i+1], c.Pix[i+2] = col.R, col.G, col.B
	}
}

// Background draws what stays the same from frame to frame once and copies
// it in for the frames after, as long as key is the same.
func (c *Canvas) Background(key string, draw func()) {
	if c.bgKey == key && len(c.bg) == len(c.Pix) {
		copy(c.Pix, c.bg)
		return
	}
	draw()
	c.bg = append(c.bg[:0], c.Pix...)
	c.bgKey = key
}

// Paint blends col over the canvas by the mask's coverage times alpha, and
// by the clip's coverage when there is one, then clears the mask.
func (c *Canvas) Paint(m *Mask, col RGB, alpha float64, clip *Mask) {
	box := m.box
	if clip != nil {
		box = box.Intersect(clip.box)
	}
	a := float32(alpha)
	if a > 0 {
		for y := box.Min.Y; y < box.Max.Y; y++ {
			row := y * m.W
			for x := box.Min.X; x < box.Max.X; x++ {
				k := m.A[row+x] * a
				if clip != nil {
					k *= clip.A[row+x]
				}
				if k <= 0 {
					continue
				}
				p := c.Pix[3*(row+x) : 3*(row+x)+3 : 3*(row+x)+3]
				p[0] += (col.R - p[0]) * k
				p[1] += (col.G - p[1]) * k
				p[2] += (col.B - p[2]) * k
			}
		}
	}
	m.Clear()
}

// Disc paints a filled circle straight away.
func (c *Canvas) Disc(cx, cy, r float64, col RGB, alpha float64, clip *Mask) {
	c.mask.Disc(cx, cy, r)
	c.Paint(c.mask, col, alpha, clip)
}

// Ring paints the stroke of a circle.
func (c *Canvas) Ring(cx, cy, r, width float64, col RGB, alpha float64, clip *Mask) {
	c.mask.Ring(cx, cy, r, width)
	c.Paint(c.mask, col, alpha, clip)
}

// RoundRect paints a rectangle whose top corners have radius top and bottom
// corners radius bottom.
func (c *Canvas) RoundRect(x, y, w, h, top, bottom float64, col RGB, alpha float64, clip *Mask) {
	c.mask.RoundRect(x, y, w, h, top, bottom)
	c.Paint(c.mask, col, alpha, clip)
}

// RGBA writes the frame as 8-bit RGBA, rounding to the nearest level.
func (c *Canvas) RGBA(dst []byte) {
	for i, j := 0, 0; i < len(c.Pix); i, j = i+3, j+4 {
		dst[j] = level(c.Pix[i])
		dst[j+1] = level(c.Pix[i+1])
		dst[j+2] = level(c.Pix[i+2])
		dst[j+3] = 255
	}
}

// RGB24 writes the frame as 8-bit RGB, three bytes a pixel.
func (c *Canvas) RGB24(dst []byte) {
	for i, v := range c.Pix {
		dst[i] = level(v)
	}
}

// Image returns the frame as an image, for previews and tests.
func (c *Canvas) Image() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	c.RGBA(img.Pix)
	return img
}

func level(v float32) byte {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	}
	return byte(v*255 + 0.5)
}

// Mask is coverage, 0..1 per pixel. Shapes added to one mask merge by taking
// the larger coverage, so the dots of one layer blend once where they touch,
// like one canvas path with many subpaths.
type Mask struct {
	W, H int
	A    []float32
	box  image.Rectangle // where A may be non-zero
}

func NewMask(w, h int) *Mask { return &Mask{W: w, H: h, A: make([]float32, w*h)} }

// Clear zeroes the mask where it was drawn on.
func (m *Mask) Clear() {
	for y := m.box.Min.Y; y < m.box.Max.Y; y++ {
		clear(m.A[y*m.W+m.box.Min.X : y*m.W+m.box.Max.X])
	}
	m.box = image.Rectangle{}
}

// Empty tells whether nothing was drawn since the last Clear.
func (m *Mask) Empty() bool { return m.box.Empty() }

// span clips the pixel range a shape touches, x0..x1 and y0..y1 in canvas
// units, to the mask and grows the drawn box by it.
func (m *Mask) span(x0, y0, x1, y1 float64) image.Rectangle {
	r := image.Rect(int(math.Floor(x0)), int(math.Floor(y0)), int(math.Ceil(x1)), int(math.Ceil(y1))).
		Intersect(image.Rect(0, 0, m.W, m.H))
	if !r.Empty() {
		m.box = m.box.Union(r)
	}
	return r
}

func (m *Mask) put(i int, k float32) {
	if k > m.A[i] {
		m.A[i] = k
	}
}

// cover turns a signed distance in pixels, negative inside, into coverage.
func cover(d float64) float32 {
	switch {
	case d <= -0.5:
		return 1
	case d >= 0.5:
		return 0
	}
	return float32(0.5 - d)
}

func (m *Mask) Disc(cx, cy, r float64) {
	if r <= 0 {
		return
	}
	if r < 1.5 {
		m.Ellipse(cx, cy, r, r, 0)
		return
	}
	b := m.span(cx-r-1, cy-r-1, cx+r+1, cy+r+1)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		dy := float64(y) + 0.5 - cy
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			m.put(y*m.W+x, cover(math.Sqrt(dx*dx+dy*dy)-r))
		}
	}
}

// Ring is the stroke of a circle of radius r, width wide.
func (m *Mask) Ring(cx, cy, r, width float64) {
	o := r + width/2 + 1
	b := m.span(cx-o, cy-o, cx+o, cy+o)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		dy := float64(y) + 0.5 - cy
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			m.put(y*m.W+x, cover(math.Abs(math.Sqrt(dx*dx+dy*dy)-r)-width/2))
		}
	}
}

// Ellipse adds an ellipse with radius rx along the direction rot (radians
// from the x axis) and ry across it. Dots smaller than a few pixels are
// sampled 4×4 per pixel; larger ones use the distance to the edge.
func (m *Mask) Ellipse(cx, cy, rx, ry, rot float64) {
	if rx <= 0 || ry <= 0 {
		return
	}
	cs, sn := math.Cos(rot), math.Sin(rot)
	e := max(rx, ry) + 1
	b := m.span(cx-e, cy-e, cx+e, cy+e)
	irx, iry := 1/rx, 1/ry
	if min(rx, ry) < 2 {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				n := 0
				for sy := 0; sy < 4; sy++ {
					dy := float64(y) + (float64(sy)+0.5)/4 - cy
					for sx := 0; sx < 4; sx++ {
						dx := float64(x) + (float64(sx)+0.5)/4 - cx
						u, v := (dx*cs+dy*sn)*irx, (-dx*sn+dy*cs)*iry
						if u*u+v*v <= 1 {
							n++
						}
					}
				}
				if n > 0 {
					m.put(y*m.W+x, float32(n)/16)
				}
			}
		}
		return
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		dy := float64(y) + 0.5 - cy
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			u, v := (dx*cs+dy*sn)*irx, (-dx*sn+dy*cs)*iry
			f := u*u + v*v - 1
			g := 2 * math.Sqrt(u*u*irx*irx+v*v*iry*iry)
			if g == 0 {
				m.put(y*m.W+x, 1)
				continue
			}
			m.put(y*m.W+x, cover(f/g))
		}
	}
}

// RoundRect adds the rectangle x, y, w, h with radius top on its upper corners
// and bottom on its lower ones; radii are capped at half the shorter side.
func (m *Mask) RoundRect(x, y, w, h, top, bottom float64) {
	if w <= 0 || h <= 0 {
		return
	}
	lim := min(w, h) / 2
	top, bottom = min(top, lim), min(bottom, lim)
	cx, cy, hw, hh := x+w/2, y+h/2, w/2, h/2
	b := m.span(x-1, y-1, x+w+1, y+h+1)
	for py := b.Min.Y; py < b.Max.Y; py++ {
		dy := float64(py) + 0.5 - cy
		r := bottom
		if dy < 0 {
			r = top
		}
		qy := math.Abs(dy) - hh + r
		for px := b.Min.X; px < b.Max.X; px++ {
			qx := math.Abs(float64(px)+0.5-cx) - hw + r
			m.put(py*m.W+px, cover(boxDist(qx, qy)-r))
		}
	}
}

func boxDist(qx, qy float64) float64 {
	return math.Hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0)
}

// Field adds the coverage f gives each pixel centre in x0..x1, y0..y1.
func (m *Mask) Field(x0, y0, x1, y1 float64, f func(x, y float64) float32) {
	b := m.span(x0, y0, x1, y1)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if k := f(float64(x)+0.5, float64(y)+0.5); k > 0 {
				m.put(y*m.W+x, k)
			}
		}
	}
}

// ClipRect keeps the mask inside x0..x1, y0..y1, with soft edges.
func (m *Mask) ClipRect(x0, y0, x1, y1 float64) {
	for y := m.box.Min.Y; y < m.box.Max.Y; y++ {
		ky := overlap(float64(y), y0, y1)
		for x := m.box.Min.X; x < m.box.Max.X; x++ {
			m.A[y*m.W+x] *= ky * overlap(float64(x), x0, x1)
		}
	}
}

// overlap is how much of the pixel p..p+1 lies in a..b.
func overlap(p, a, b float64) float32 {
	return float32(max(0, min(p+1, b)-max(p, a)))
}

// Capsule adds a segment from a to b with round ends of radius r: a dash of a
// dashed line with round caps.
func (m *Mask) Capsule(ax, ay, bx, by, r float64) {
	b := m.span(min(ax, bx)-r-1, min(ay, by)-r-1, max(ax, bx)+r+1, max(ay, by)+r+1)
	ex, ey := bx-ax, by-ay
	l2 := ex*ex + ey*ey
	for y := b.Min.Y; y < b.Max.Y; y++ {
		py := float64(y) + 0.5 - ay
		for x := b.Min.X; x < b.Max.X; x++ {
			px := float64(x) + 0.5 - ax
			h := 0.0
			if l2 > 0 {
				h = min(1, max(0, (px*ex+py*ey)/l2))
			}
			m.put(y*m.W+x, cover(math.Hypot(px-ex*h, py-ey*h)-r))
		}
	}
}

// Dashed adds a dashed line along the points: dashes dash long with round
// caps width wide, starting every period pixels.
func (m *Mask) Dashed(pts [][2]float64, dash, period, width float64) {
	if len(pts) < 2 {
		return
	}
	lens := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		lens[i] = lens[i-1] + math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
	}
	total := lens[len(lens)-1]
	at := func(s float64) (float64, float64) {
		i := 1
		for i < len(pts)-1 && lens[i] < s {
			i++
		}
		f := 0.0
		if seg := lens[i] - lens[i-1]; seg > 0 {
			f = (s - lens[i-1]) / seg
		}
		return pts[i-1][0] + (pts[i][0]-pts[i-1][0])*f, pts[i-1][1] + (pts[i][1]-pts[i-1][1])*f
	}
	for s := 0.0; s <= total; s += period {
		ax, ay := at(s)
		bx, by := at(min(s+dash, total))
		m.Capsule(ax, ay, bx, by, width/2)
	}
}
