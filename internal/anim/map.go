// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"math"
	"strconv"
)

// Place is the answer of the day.
type Place struct {
	Lat, Lon      float64
	Name, Country string
}

// placeLabel writes the place and its country at the top right.
func (c *Canvas) placeLabel(p Place, alpha float64) {
	if alpha <= 0 {
		return
	}
	cw := c.Text(Display, 30, p.Country, 688, 82, Right, ink, alpha, nil)
	c.Text(Display, 44, p.Name, 688-cw-12, 82, Right, white, alpha, nil)
}

// globeView is the halftone map seen from above lat0, lon0: a globe of radius
// R centred on cx, cy.
type globeView struct {
	cx, cy, R      float64
	s0, c0, lon0   float64 // sine and cosine of lat0, and lon0, in radians
	x0, y0, x1, y1 float64 // the window it shows
}

func newGlobeView(cx, cy, R, lat0, lon0 float64) globeView {
	s0, c0 := math.Sincos(lat0)
	return globeView{cx: cx, cy: cy, R: R, s0: s0, c0: c0, lon0: lon0}
}

// at is where lat, lon in degrees falls on the screen, and z < 0 behind.
func (g globeView) at(lat, lon float64) (x, y, z float64) {
	a, b, z := orthoDot(math.Sin(lat*rad), math.Cos(lat*rad), lon*rad, g.s0, g.c0, g.lon0)
	return g.cx + a*g.R, g.cy - b*g.R, z
}

// ripple is a ring running out from x, y at speed pixels a second, fading
// over life seconds.
type ripple struct {
	x, y, since, speed, life float64
}

func (r ripple) at(px, py float64) float64 {
	if r.since <= 0 {
		return 0
	}
	return max(0, 1-math.Abs(math.Hypot(px-r.x, py-r.y)-r.since*r.speed)/46) * max(0, 1-r.since/r.life)
}

// mapDots draws the 1° halftone land: dots swell where the ripple passes,
// and the sea's dots it lifts turn white with the land.
func (c *Canvas) mapDots(g globeView, w ripple, landAlpha float64, clip *Mask) {
	sp := rad * g.R
	sea, land := c.mask, c.layer
	for _, d := range fine {
		a, b, z := orthoDot(d.sinLat, d.cosLat, d.lon, g.s0, g.c0, g.lon0)
		if z < 0.02 {
			continue
		}
		px, py := g.cx+a*g.R, g.cy-b*g.R
		if px < g.x0-10 || px > g.x1+10 || py < g.y0-10 || py > g.y1+10 {
			continue
		}
		sx, sy := a, -b
		lit := smooth(-0.15, 0.95, sx*light[0]+sy*light[1]+z*light[2])
		k := w.at(px, py)
		size := 0.14 + 0.26*lit + 0.4*k
		if d.land {
			size = 0.3 + 0.62*lit + 0.5*k
		}
		rd := size * sp / 2
		if rd < 0.4 {
			continue
		}
		m := sea
		if d.land || k > 0.5 {
			m = land
		}
		m.Ellipse(px, py, max(0.4, rd*z), rd, math.Atan2(sy, sx))
	}
	c.Paint(sea, tint, 1, clip)
	c.Paint(land, white, landAlpha, clip)
}

// target marks the place: it lands and keeps a slow pulse of two rings.
func (c *Canvas) target(x, y, since, ring, grow, outer, inner, alpha float64, clip *Mask) {
	if since <= 0 || alpha <= 0 {
		return
	}
	s := back(lin(0, 0.5, since))
	for k := range 2 {
		ph := math.Mod(since+float64(k)*1.1, 2.2) / 2.2
		c.Ring(x, y, ring+ph*grow, 3, white, 0.7*(1-ph)*alpha, clip)
	}
	c.Disc(x, y, outer*s, white, alpha, clip)
	c.Disc(x, y, inner*s, field, alpha, clip)
}

// Winner is one of the three on a podium.
type Winner struct {
	Name       string
	Km, Points float64
	Streak     int   // days in a row, shown when > 0
	Face       *Face // the profile photo, beside the name; nil for none
}

// podium is how the steps of a post rise: their heights for first, second
// and third, when the first rises and how far apart, when they go down, and
// where the texts sit.
type podium struct {
	h                [3]float64
	rise, step, down float64
	num, numY        float64
	nameY, lineY     float64
}

var podiumX = [3]float64{262, 52, 472}

const podiumBase, podiumW = 888, 196

// podium puts the winners on their steps: third, second, first. A nil
// winner leaves the step out.
func (c *Canvas) podium(p podium, win [3]*Winner, unit, unitM string, t float64) {
	for _, ri := range []int{2, 1, 0} {
		w := win[ri]
		if w == nil {
			continue
		}
		t0 := p.rise + float64(2-ri)*p.step
		down := 1 - easeIn(lin(p.down+float64(ri)*0.12, p.down+0.6+float64(ri)*0.12, t))
		g := back(lin(t0, t0+0.6, t)) * down
		if g <= 0 {
			continue
		}
		h := p.h[ri] * g
		x, top := podiumX[ri], podiumBase-h
		c.RoundRect(x, top, podiumW, h+40, 18, 0, medal[ri], 1, nil)
		c.textMask(Display, p.num, strconv.Itoa(ri+1), x+podiumW/2, top+p.numY, Center)
		c.mask.ClipRect(x, top, x+podiumW, top+h)
		c.Paint(c.mask, field, 1, nil)
		a := smooth(t0+0.25, t0+0.6, t) * down
		if a <= 0 {
			continue
		}
		c.streakName(w.Name, w.Streak, w.Face, x+podiumW/2, top+p.nameY, a)
		c.Text(BodyMedium, 21, Distance(w.Km, unit, unitM)+" · "+Thousands(w.Points), x+podiumW/2, top+p.lineY, Center, paper, a, nil)
	}
}

// streakName writes a name centred on x, and after it a flame and the
// streak when there is one: the 🔥 of the messages, drawn, since the brand's
// faces have no emoji.
func (c *Canvas) streakName(name string, streak int, face *Face, x, y, alpha float64) {
	// The photo sits before the name, its middle on the letters' middle.
	const faceD, faceGap = 34, 8
	lead := 0.0
	if face != nil {
		lead = faceD + faceGap
	}
	if streak <= 0 {
		x0 := x - (lead+c.Measure(Body, 28, name))/2
		c.Face(face, x0+faceD/2, y-10, faceD/2, alpha, nil)
		c.Text(Body, 28, name, x0+lead, y, Left, white, alpha, nil)
		return
	}
	n := strconv.Itoa(streak)
	const gap, flameW = 10, 15
	total := lead + c.Measure(Body, 28, name) + gap + flameW + 3 + c.Measure(Body, 28, n)
	x0 := x - total/2
	c.Face(face, x0+faceD/2, y-10, faceD/2, alpha, nil)
	x0 += lead
	w := c.Text(Body, 28, name, x0, y, Left, white, alpha, nil)
	c.flame(x0+w+gap+flameW/2, y, 24, alpha)
	c.Text(Body, 28, n, x0+w+gap+flameW+3, y, Left, white, alpha, nil)
}

// flame is the pin upside down, bronze, with a gold heart: its base at y,
// h tall, centred on x.
func (c *Canvas) flame(x, y, h, alpha float64) {
	for _, l := range []struct {
		h   float64
		col RGB
	}{{h, bronze}, {h * 0.55, gold}} {
		k := l.h / pinHeight
		top := y - l.h
		c.mask.Field(x-pinR*k-1, top-1, x+pinR*k+1, y+1, func(px, py float64) float32 {
			u, v := (px-x)/k+pinCX, pinTip-(py-top)/k
			return cover(pinDist(u, v) * k)
		})
		c.Paint(c.mask, l.col, alpha, nil)
	}
}
