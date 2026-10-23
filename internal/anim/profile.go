// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"math"
	"slices"
	"strconv"

	"github.com/go-text/typesetting/language"
)

// Day is one of the last days on the strip under the numbers.
type Day struct {
	No     int  // the day's number, #17
	Played bool // answered and scored
	Best   bool // the person's best day
}

// Tile is one number of a person's statistics with what it counts.
type Tile struct {
	Value, Label string
	Flame        bool // a streak: the flame goes before the number
	Gold         bool // a first place or a record
}

// Profile is the picture of a person's own statistics, shown above the text of
// the statistics panel: their pins of every day on the globe, their points,
// six numbers and the strip of their last days. The numbers stand still and
// the Earth turns under the pins, once a loop, so the loop has no seam.
// Every text shrinks to its room, since a label in German is twice as long
// as in Russian.
type Profile struct {
	Title     string // "statistics"
	Name      string
	Total     Tile    // the big number under the name
	Tiles     [6]Tile // two rows of three
	Days      []Day   // the last days, oldest first
	DaysLabel string
	Pins      []Guess // every day's answer; Medal 1 marks the best day
	Lang      language.Language
}

func NewProfile(lang string) *Profile { return &Profile{Lang: language.NewLanguage(lang)} }

func (s *Profile) Size() (int, int)  { return 1200, 630 }
func (s *Profile) Duration() float64 { return profileDur }

// One turn of the Earth, slow enough to read the numbers beside it.
const profileDur = 24

func (s *Profile) Draw(c *Canvas, t float64) {
	c.Lang = s.Lang
	c.Fill(field)

	// The globe of the person's pins, turned to where most of them are.
	const mx, my, mw, mh = 24, 24, 540, 582
	c.RoundRect(mx, my, mw, mh, 28, 28, deep, 1, nil)
	clip := c.clip
	clip.Clear()
	clip.RoundRect(mx, my, mw, mh, 28, 28)
	lat0, lon0 := 20.0, 15.0
	if len(s.Pins) > 0 {
		var x, y, z float64
		for _, p := range s.Pins {
			sl, cl := math.Sincos(p.Lat * rad)
			so, co := math.Sincos(p.Lon * rad)
			x, y, z = x+cl*co, y+cl*so, z+sl
		}
		lat0, lon0 = math.Atan2(z, math.Hypot(x, y))/rad, math.Atan2(y, x)/rad
		lat0 = max(-35, min(45, lat0))
	}
	g := newGlobeView(mx+mw/2, my+mh/2, 250, lat0*rad, (lon0-360*t/profileDur)*rad)
	g.x0, g.y0, g.x1, g.y1 = mx, my, mx+mw, my+mh
	c.Disc(g.cx, g.cy, g.R+2, field, 0.55, clip)
	c.mapDots(g, ripple{}, 0.5, clip)
	pins := slices.Clone(s.Pins)
	slices.SortStableFunc(pins, func(a, b Guess) int { return a.Medal - b.Medal })
	for _, p := range pins {
		x, y, z := g.at(p.Lat, p.Lon)
		if z < 0.05 {
			continue
		}
		a := smooth(0.05, 0.2, z)
		if p.Medal == 1 {
			c.Pin(x, y, 40, PinStyle{Fill: gold, Edge: deep, Hole: deep}, a, clip)
			continue
		}
		c.Disc(x, y, 7.5, deep, a, clip)
		c.Disc(x, y, 5.5, paper.Mix(gold, 1-smooth(200, 3000, p.Km)), a, clip)
	}

	// The texts on the right.
	const x0, x1 = 600, 1176
	w := c.Text(Display, 52, "maply", x0, 70, Left, white, 1, nil)
	nw := c.Text(Body, c.fit(Body, 26, 18, s.Name, 220), s.Name, x1, 68, Right, white, 1, nil)
	c.Text(Display, c.fit(Display, 30, 20, s.Title, x1-nw-24-(x0+w+12)), s.Title, x0+w+12, 70, Left, ink, 1, nil)

	vw := c.Text(Display, 104, s.Total.Value, x0, 182, Left, white, 1, nil)
	c.Text(BodyMedium, c.fit(BodyMedium, 22, 14, s.Total.Label, x1-x0-vw-16), s.Total.Label, x0+vw+16, 180, Left, ink, 1, nil)

	const tw, th, gap = 184, 104, 12
	for i, tile := range s.Tiles {
		x, y := x0+float64(i%3)*(tw+gap), 210+float64(i/3)*(th+gap)
		c.RoundRect(x, y, tw, th, 18, 18, deep, 1, nil)
		col := white
		if tile.Gold {
			col = gold
		}
		tx := x + 18
		if tile.Flame {
			c.flame(tx+11, y+58, 34, 1)
			tx += 32
		}
		c.Text(Display, c.fit(Display, 50, 30, tile.Value, x+tw-14-tx), tile.Value, tx, y+58, Left, col, 1, nil)
		c.Text(BodyMedium, c.fit(BodyMedium, 18, 12, tile.Label, tw-30), tile.Label, x+18, y+88, Left, ink, 1, nil)
	}

	// The last days: a full disc for a day played, a ring for a day missed,
	// gold for the best one, the day's number under each.
	if n := len(s.Days); n > 0 {
		const cy, r = 548, 14
		c.Text(BodyMedium, c.fit(BodyMedium, 18, 12, s.DaysLabel, x1-x0), s.DaysLabel, x0, 508, Left, ink, 1, nil)
		step := (x1 - x0 - 2*r) / float64(max(1, n-1))
		for i, d := range s.Days {
			x := x0 + r + float64(i)*step
			col := ink
			switch {
			case d.Best:
				c.Disc(x, cy, r, gold, 1, nil)
				col = gold
			case d.Played:
				c.Disc(x, cy, r, paper, 1, nil)
			default:
				c.Ring(x, cy, r-1.5, 3, tint, 1, nil)
			}
			c.Text(BodyMedium, 14, "#"+strconv.Itoa(d.No), x, cy+r+22, Center, col, 1, nil)
		}
	}
}

// fit is the size, from size down to least, at which s fits in width.
func (c *Canvas) fit(f *Font, size, least float64, s string, width float64) float64 {
	for size > least && c.Measure(f, size, s) > width {
		size--
	}
	return size
}
