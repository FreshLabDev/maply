// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"math"
	"strconv"

	"github.com/go-text/typesetting/language"
)

// Guess is one answer of the day on the world's globe. The world post names
// nobody but the three first, so a guess is only a point and when it came.
type Guess struct {
	Lat, Lon float64
	Km       float64
	At       float64 // when in the day it came, 0 to 1
	Medal    int     // 1, 2 or 3 for the first three, else 0
}

// World is the post of the day's end for everyone: all the pins rain onto one
// globe, take the colour of how close they came, and the world's first three
// stand on the podium. It is the same picture in every chat; a person's own
// place goes in the caption.
type World struct {
	Day     int
	Place   Place
	Guesses []Guess
	Top     [3]*Winner // the first three; nil where there are fewer
	Unit    string
	Lang    language.Language
}

const worldDur = 12

var worldPodium = podium{h: [3]float64{112, 84, 64}, rise: 5.7, step: 0.38, down: 9.3, num: 64, numY: 62, nameY: -42, lineY: -13}

func NewWorld(day int, place Place, guesses []Guess, top [3]*Winner, unit, lang string) *World {
	return &World{Day: day, Place: place, Guesses: guesses, Top: top, Unit: unit, Lang: language.NewLanguage(lang)}
}

func (w *World) Size() (int, int)  { return 720, 900 }
func (w *World) Duration() float64 { return worldDur }

func (w *World) Draw(c *Canvas, t float64) {
	const mx, my, mw, mh = 32, 112, 656, 560
	c.Lang = w.Lang
	c.Background("world"+strconv.Itoa(w.Day), func() {
		c.Fill(field)
		c.header(w.Day, 64, 84)
	})
	const reveal = 4.8
	gone := smooth(9.6, 10.6, t)
	c.placeLabel(w.Place, smooth(reveal, reveal+0.6, t)*(1-smooth(9.8, 10.4, t)))
	c.RoundRect(mx, my, mw, mh, 28, 28, deep, 1, nil)
	clip := c.clip
	clip.Clear()
	clip.RoundRect(mx, my, mw, mh, 28, 28)

	// The globe sways a little around the place, once a loop.
	g := newGlobeView(360, my+mh/2, 300, 4*rad, (w.Place.Lon+10*math.Sin(2*math.Pi*t/worldDur))*rad)
	g.x0, g.y0, g.x1, g.y1 = mx, my, mx+mw, my+mh
	ax, ay, _ := g.at(w.Place.Lat, w.Place.Lon)
	since := t - reveal
	wave := ripple{x: ax, y: ay, since: since, speed: 300, life: 1.8}
	c.mapDots(g, wave, 0.5, clip)

	// Everyone's pins rain in through the day, then take the colour of how
	// close they came.
	rc := smooth(reveal+0.2, reveal+1.0, t)
	for _, p := range w.Guesses {
		t0 := 0.5 + p.At*4
		f := lin(t0, t0+0.35, t)
		out := 1 - smooth(9.4+p.At*0.8, 9.7+p.At*0.8, t)
		if f <= 0 || out <= 0 {
			continue
		}
		x, y, z := g.at(p.Lat, p.Lon)
		if z < 0.05 {
			continue
		}
		col := paper.Mix(gold.Mix(ink, smooth(0, 3500, p.Km)), rc)
		r := 3.6
		if p.Medal >= 1 && p.Medal <= 3 {
			col = paper.Mix(medal[p.Medal-1], rc)
			r += 3 * rc
		}
		r = r*(0.6+0.4*back(f)) + 3*wave.at(x, y)
		y -= (1 - easeIn(f)) * 30
		a := smooth(0, 0.3, f) * out
		c.Disc(x, y, r+1.5, deep, a, clip)
		c.Disc(x, y, r, col, a, clip)
	}
	c.target(ax, ay, since, 14, 44, 11, 4.5, 1-gone, clip)

	// How many played: counts up as the pins come in, back down at the end.
	if n := int(math.Round(float64(len(w.Guesses)) * easeOut(lin(0.5, 4.6, t)) * (1 - inOut(lin(9.8, 11.0, t))))); n > 0 {
		s := strconv.Itoa(n)
		cw := c.Measure(Display, 40, s) + 64
		c.RoundRect(52, 132, cw, 52, 26, 26, deep, 0.85, clip)
		c.Pin(78, 172, 30, PinStyle{Fill: white, Hole: deep, NoEdge: true, EdgeWidth: 1}, 1, clip)
		c.Text(Display, 40, s, 98, 173, Left, white, 1, clip)
	}
	c.podium(worldPodium, w.Top, w.Unit, t)
}
