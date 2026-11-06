// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"math"
	"strings"
	"sync"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// Sofia Sans and Sofia Sans Extra Condensed, the brand's faces, and Noto
// Emoji, the one-colour emoji face they fall back to, as variable fonts under
// the SIL Open Font License (fonts/OFL.txt).
var (
	//go:embed fonts/SofiaSans.ttf
	sofiaSans []byte
	//go:embed fonts/SofiaSansExtraCondensed.ttf
	sofiaSansExtraCondensed []byte
	//go:embed fonts/NotoEmoji.ttf
	notoEmoji []byte
)

// The faces of the posts: the display face of the logo and the body face in
// two weights. An emoji in a name is drawn from Noto Emoji in the text's own
// colour.
var (
	Display    = mustFont(sofiaSansExtraCondensed, 700).with(mustFont(notoEmoji, 700))
	Body       = mustFont(sofiaSans, 700).with(mustFont(notoEmoji, 700))
	BodyMedium = mustFont(sofiaSans, 500).with(mustFont(notoEmoji, 500))
)

// ShapingLang is the language the faces are told for text in the language
// code. Sofia Sans has local forms for Russian only: told Ukrainian or
// Belarusian, it keeps its Bulgarian ones, a в like a ϐ and a т like an m.
// Those two are shaped as Russian, whose forms are theirs too.
func ShapingLang(code string) language.Language {
	switch code {
	case "uk", "be":
		code = "ru"
	}
	return language.NewLanguage(code)
}

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
	// fallback draws what face has no glyph for: emoji.
	fallback *Font
}

func (f *Font) with(fallback *Font) *Font {
	f.fallback = fallback
	return f
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
	r := &Run{middle: (f.asc + f.desc) / 2 * (size / f.upem)}
	pen := 0.0
	for _, part := range f.split(text) {
		if part.face == f {
			pen = f.shape(r, text, part.start, part.end, size, lang, pen)
			continue
		}
		pen = part.face.shapeLocked(r, text, part.start, part.end, size, lang, pen)
	}
	r.Width = pen
	// Every day brings new names and numbers, so the cache is emptied now
	// and then rather than kept for the life of the process.
	if len(f.runs) >= maxRuns {
		clear(f.runs)
	}
	f.runs[key] = r
	return r
}

// maxRuns bounds a face's cache: a whole animation draws far fewer strings.
const maxRuns = 1024

// shapeLocked is shape on the fallback, which belongs to one face but is
// read by Covered too. The lock is released even if shaping panics: a held
// one would stop all drawing for good.
func (f *Font) shapeLocked(r *Run, text []rune, start, end int, size float64, lang language.Language, pen float64) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shape(r, text, start, end, size, lang, pen)
}

// shape adds text[start:end] to r with the pen at pen, and returns where the
// pen ends.
func (f *Font) shape(r *Run, text []rune, start, end int, size float64, lang language.Language, pen float64) float64 {
	out := f.shaper.Shape(shaping.Input{
		Text: text, RunStart: start, RunEnd: end, Direction: di.DirectionLTR,
		Face: f.face, Size: fixed.Int26_6(math.Round(size * 64)), Script: scriptOf(text[start:end]), Language: lang,
	})
	k := size / f.upem
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
	return pen
}

// part is a stretch of text drawn from one face.
type part struct {
	face       *Font
	start, end int
}

// split cuts text where it passes between the face and its fallback. The
// caller holds f.mu. What glues an emoji together (a joiner, a variation
// selector, a skin tone, a keycap, a flag's tag) stays with the letter before
// it, so a sequence like a family or a flag is shaped whole. One with no
// letter before it is drawn from whichever face has it.
func (f *Font) split(text []rune) []part {
	var parts []part
	for i, r := range text {
		face := f
		switch {
		case sticks(r) && len(parts) > 0:
			face = parts[len(parts)-1].face
			// A skin tone or a keycap after a letter or a space: the
			// letter's face has no glyph for it and would draw a box.
			if face == f && f.fallback != nil && !f.hasLocked(r) && f.fallback.has(r) {
				face = f.fallback
			}
		case f.fallback != nil && !f.hasLocked(r) && f.fallback.has(r):
			face = f.fallback
		}
		if n := len(parts); n > 0 && parts[n-1].face == face {
			parts[n-1].end = i + 1
			continue
		}
		parts = append(parts, part{face, i, i + 1})
	}
	return parts
}

// sticks tells whether r only modifies the character before it.
func sticks(r rune) bool {
	return r == 0x200D || r == 0x20E3 || (r >= 0xFE00 && r <= 0xFE0F) ||
		(r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F)
}

// has tells whether the face itself has a glyph for r.
func (f *Font) has(r rune) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasLocked(r)
}

func (f *Font) hasLocked(r rune) bool {
	_, ok := f.face.NominalGlyph(r)
	return ok
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
	z    vector.Rasterizer
	pix  []byte
	segs []glyphSeg
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
	if cap(s.segs) < len(r.segs) {
		s.segs = make([]glyphSeg, len(r.segs))
	}
	segs := s.segs[:len(r.segs)]
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

// Covers tells whether the faces have a glyph for every letter of s. They
// cover Latin, Cyrillic and emoji; a name or a place in another script is
// written some other way before it goes on a post.
func Covers(s string) bool {
	for _, r := range s {
		if !Covered(r) {
			return false
		}
	}
	return true
}

// Covered tells whether the faces, with their emoji fallback, have a glyph
// for r. What glues an emoji together counts as covered.
func Covered(r rune) bool {
	if sticks(r) {
		return true
	}
	for _, f := range []*Font{Display, Body} {
		if !f.has(r) && (f.fallback == nil || !f.fallback.has(r)) {
			return false
		}
	}
	return true
}

// Name is a person's first name as a post writes it: the letters the faces
// have, sixteen at most, or "?" when none is left.
func Name(first string) string {
	name := strings.Map(func(r rune) rune {
		if Covered(r) {
			return r
		}
		return -1
	}, strings.TrimSpace(first))
	name = strings.Join(strings.Fields(name), " ")
	// A joiner or a skin tone left at the front, with the letter it modified
	// dropped, has nothing to stick to.
	name = strings.TrimLeftFunc(name, sticks)
	if r := []rune(name); len(r) > 16 {
		name = string(r[:15]) + "…"
	}
	if name == "" || !Covers(name) {
		return "?"
	}
	return name
}
