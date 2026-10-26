// SPDX-License-Identifier: Apache-2.0

// Package anim renders the animated posts of the game: seamless loops drawn
// frame by frame on the CPU and encoded to H.264 by ffmpeg. The scenes were
// designed in a browser canvas; this package draws the same frames in a
// fraction of the time, without a browser or a GPU.
package anim

import (
	"math"
	"strconv"
	"strings"
)

// FPS is the frame rate of every post.
const FPS = 30

// The brand's colours.
var (
	field  = Hex("#245A3C")
	deep   = Hex("#1A412B")
	tint   = Hex("#59826B")
	paper  = Hex("#DDE5E0")
	ink    = Hex("#B5C7BD")
	white  = Hex("#FFFFFF")
	gold   = Hex("#F2C14E")
	silver = Hex("#DDE5E0")
	bronze = Hex("#D98E52")
	medal  = [3]RGB{gold, silver, bronze}
)

// Scene is one looping post. Draw paints the frame at t seconds; the frame at
// Duration looks the same as the frame at 0, so the loop has no seam.
type Scene interface {
	Size() (w, h int)
	Duration() float64
	Draw(c *Canvas, t float64)
}

// Frames is how many frames one loop of the scene has.
func Frames(s Scene) int { return int(math.Round(s.Duration() * FPS)) }

func clamp01(x float64) float64 { return min(1, max(0, x)) }

// lin goes from 0 to 1 as t goes from a to b.
func lin(a, b, t float64) float64 { return clamp01((t - a) / (b - a)) }

func smooth(a, b, x float64) float64 {
	t := lin(a, b, x)
	return t * t * (3 - 2*t)
}

func mix(a, b, t float64) float64 { return a + (b-a)*t }

func inOut(t float64) float64 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	return 1 - math.Pow(-2*t+2, 3)/2
}

func easeOut(t float64) float64 { return 1 - math.Pow(1-t, 3) }

func easeIn(t float64) float64 { return t * t * t }

func sine(t float64) float64 { return -(math.Cos(math.Pi*t) - 1) / 2 }

func back(t float64) float64 {
	const c1 = 1.5
	return 1 + (c1+1)*math.Pow(t-1, 3) + c1*math.Pow(t-1, 2)
}

// rng is mulberry32, as in the design, so the confetti falls the same way.
func rng(seed uint32) func() float64 {
	return func() float64 {
		seed += 0x6D2B79F5
		t := (seed ^ seed>>15) * (1 | seed)
		t = (t + (t^t>>7)*(61|t)) ^ t
		return float64(t^t>>14) / 4294967296
	}
}

// header draws "maply #N" at the top left.
func (c *Canvas) header(day int, size, y float64) {
	w := c.Text(Display, size, "maply", 32, y, Left, white, 1, nil)
	c.Text(Display, math.Round(size*0.47), "#"+strconv.Itoa(day), 32+w+size*0.19, y, Left, ink, 1, nil)
}

// timeline is a row of dots from now to the reveal, with a glint running
// along it towards the time.
func (c *Canvas) timeline(x0, x1, y, t, period, filled float64) {
	n := int(math.Round((x1-x0)/17)) + 1
	for i := range n {
		f := float64(i) / float64(n-1)
		ph := math.Mod(t/period, 1)*1.4 - 0.2
		g := max(0, 1-math.Abs(f-ph)/0.12)
		if f <= filled {
			c.Disc(x0+f*(x1-x0), y, 4.2+2.2*g, white, 1, nil)
		} else {
			c.Disc(x0+f*(x1-x0), y, 3.2+2.2*g, tint.Mix(white, g*0.8), 1, nil)
		}
	}
}

// Thousands writes n rounded, its thousands apart by a no-break space, the
// narrowest space the brand's faces have: 1 234.
// Distance is km with its unit, in metres under a kilometre when unitM is
// given: a pin a few steps off says "2 m", not "0 km".
func Distance(km float64, unit, unitM string) string {
	if km < 1 && unitM != "" {
		return Thousands(km*1000) + " " + unitM
	}
	return Thousands(km) + " " + unit
}

func Thousands(n float64) string {
	s := strconv.Itoa(int(math.Round(n)))
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune('\u00a0')
		}
		b.WriteRune(r)
	}
	return b.String()
}
