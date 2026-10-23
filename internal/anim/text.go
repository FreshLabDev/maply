// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"math"
	"sync"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// Sofia Sans and Sofia Sans Extra Condensed, the brand's faces, as variable
// fonts under the SIL Open Font License (fonts/OFL.txt).
var (
	//go:embed fonts/SofiaSans.ttf
	sofiaSans []byte
	//go:embed fonts/SofiaSansExtraCondensed.ttf
	sofiaSansExtraCondensed []byte
)

// The faces of the posts: the display face of the logo and the body face in
// two weights.
var (
	Display    = mustFont(sofiaSansExtraCondensed, 700)
	Body       = mustFont(sofiaSans, 700)
	BodyMedium = mustFont(sofiaSans, 500)
)

// Font is one face at one weight. It shapes with HarfBuzz rules, so the
// language picks the local forms: Sofia Sans draws Bulgarian Cyrillic unless
// told the text is Russian.
type Font struct {
	mu        sync.Mutex
	face      *font.Face
	shaper    shaping.HarfbuzzShaper
	upem      float64
	asc, desc float64 // font units; desc is negative
	runs      map[runKey]*Run
}

type runKey struct {
	s    string
	size float64
	lang language.Language
}

func mustFont(ttf []byte, weight float32) *Font {
	face, err := font.ParseTTF(bytes.NewReader(ttf))
	if err != nil {
		panic("anim: " + err.Error())
	}
	face.SetVariations([]font.Variation{{Tag: ot.MustNewTag("wght"), Value: weight}})
	f := &Font{face: face, upem: float64(face.Upem()), runs: map[runKey]*Run{}}
	if ext, ok := face.FontHExtents(); ok {
		f.asc, f.desc = float64(ext.Ascender), float64(ext.Descender)
	}
	return f
}

// Run is a shaped line: its outline in pixels with the pen at (0, 0) on the
// baseline, y down, and how far the pen moved.
type Run struct {
	segs   []glyphSeg
	Width  float64
	middle float64 // from the baseline up to the middle of the em box
}

type glyphSeg struct {
	op  ot.SegmentOp
	pts [3][2]float64
}

// Layout shapes s at size pixels. Runs are cached, so a frame that draws the
// same words as the last costs only their rasterising.
func (f *Font) Layout(s string, size float64, lang language.Language) *Run {
	key := runKey{s, size, lang}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.runs[key]; ok {
		return r
	}
	text := []rune(s)
	out := f.shaper.Shape(shaping.Input{
		Text: text, RunStart: 0, RunEnd: len(text), Direction: di.DirectionLTR,
		Face: f.face, Size: fixed.Int26_6(math.Round(size * 64)), Script: scriptOf(text), Language: lang,
	})
	k := size / f.upem
	r := &Run{middle: (f.asc + f.desc) / 2 * k}
	pen := 0.0
	for _, g := range out.Glyphs {
		ox, oy := pen+fx(g.XOffset), -fx(g.YOffset)
		if outline, ok := f.face.GlyphData(g.GlyphID).(font.GlyphOutline); ok {
			for _, s := range outline.Segments {
				var gs glyphSeg
				gs.op = s.Op
				for i, p := range s.ArgsSlice() {
					gs.pts[i] = [2]float64{ox + float64(p.X)*k, oy - float64(p.Y)*k}
				}
				r.segs = append(r.segs, gs)
			}
		}
		pen += fx(g.Advance)
	}
	r.Width = pen
	f.runs[key] = r
	return r
}

func fx(v fixed.Int26_6) float64 { return float64(v) / 64 }

// scriptOf is the script of the first letter that has one: the digits and
// signs of "20:00" or "1 234 км" go with the letters around them.
func scriptOf(text []rune) language.Script {
	for _, r := range text {
		if s := language.LookupScript(r); s != language.Common && s != language.Inherited && s != language.Unknown {
			return s
		}
	}
	return language.Latin
}

// Align says which end of a line its x is.
type Align int

const (
	Left Align = iota
	Center
	Right
)

type textScratch struct {
	z   vector.Rasterizer
	pix []byte
}

// Text draws s with its baseline at y and x at the given end, and returns its
// width.
func (c *Canvas) Text(f *Font, size float64, s string, x, y float64, align Align, col RGB, alpha float64, clip *Mask) float64 {
	w := c.textMask(f, size, s, x, y, align)
	c.Paint(c.mask, col, alpha, clip)
	return w
}

// textMask adds s to the canvas's mask without painting it.
func (c *Canvas) textMask(f *Font, size float64, s string, x, y float64, align Align) float64 {
	r := f.Layout(s, size, c.Lang)
	switch align {
	case Center:
		x -= r.Width / 2
	case Right:
		x -= r.Width
	}
	c.mask.Run(r, &c.text, func(p [2]float64) [2]float64 { return [2]float64{x + p[0], y + p[1]} })
	return r.Width
}

// Measure is the width of s.
func (c *Canvas) Measure(f *Font, size float64, s string) float64 {
	return f.Layout(s, size, c.Lang).Width
}

// Run adds a shaped line to the mask, each point of its outline moved by at.
func (m *Mask) Run(r *Run, s *textScratch, at func([2]float64) [2]float64) {
	if len(r.segs) == 0 {
		return
	}
	segs := make([]glyphSeg, len(r.segs))
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i, g := range r.segs {
		segs[i].op = g.op
		for j, p := range g.pts {
			q := at(p)
			segs[i].pts[j] = q
			if j < segPoints(g.op) {
				x0, y0, x1, y1 = min(x0, q[0]), min(y0, q[1]), max(x1, q[0]), max(y1, q[1])
			}
		}
	}
	b := image.Rect(int(math.Floor(x0)), int(math.Floor(y0)), int(math.Ceil(x1))+1, int(math.Ceil(y1))+1).
		Intersect(image.Rect(0, 0, m.W, m.H))
	if b.Empty() {
		return
	}
	w, h := b.Dx(), b.Dy()
	if cap(s.pix) < w*h {
		s.pix = make([]byte, w*h)
	}
	a := &image.Alpha{Pix: s.pix[:w*h], Stride: w, Rect: image.Rect(0, 0, w, h)}
	clear(a.Pix)
	z := &s.z
	z.Reset(w, h)
	z.DrawOp = draw.Src
	ox, oy := float64(b.Min.X), float64(b.Min.Y)
	pt := func(p [2]float64) (float32, float32) { return float32(p[0] - ox), float32(p[1] - oy) }
	open := false
	for _, g := range segs {
		switch g.op {
		case ot.SegmentOpMoveTo:
			if open {
				z.ClosePath()
			}
			z.MoveTo(pt(g.pts[0]))
			open = true
		case ot.SegmentOpLineTo:
			z.LineTo(pt(g.pts[0]))
		case ot.SegmentOpQuadTo:
			bx, by := pt(g.pts[0])
			cx, cy := pt(g.pts[1])
			z.QuadTo(bx, by, cx, cy)
		case ot.SegmentOpCubeTo:
			bx, by := pt(g.pts[0])
			cx, cy := pt(g.pts[1])
			dx, dy := pt(g.pts[2])
			z.CubeTo(bx, by, cx, cy, dx, dy)
		}
	}
	if open {
		z.ClosePath()
	}
	z.Draw(a, a.Rect, image.Opaque, image.Point{})
	m.box = m.box.Union(b)
	for y := 0; y < h; y++ {
		row := (b.Min.Y+y)*m.W + b.Min.X
		for x, v := range a.Pix[y*w : y*w+w] {
			if v != 0 {
				m.put(row+x, float32(v)/255)
			}
		}
	}
}

func segPoints(op ot.SegmentOp) int {
	switch op {
	case ot.SegmentOpQuadTo:
		return 2
	case ot.SegmentOpCubeTo:
		return 3
	}
	return 1
}
