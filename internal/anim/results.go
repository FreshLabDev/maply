// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"math"
	"slices"
	"strconv"

	"github.com/go-text/typesetting/language"
)

// Place is the answer of the day.
type Place struct {
	Lat, Lon      float64
	Name, Country string
}

// Player is one pin on the map of the results.
type Player struct {
	Name       string
	Lat, Lon   float64
	Km, Points float64
	Rank       int // 1 is the closest
	Streak     int // days in a row, shown on the podium when > 0
}

// Results is the post of the evening in a group: the camera flies to where
// everybody's pins are, the place comes out, lines run to every pin, and the
// three closest stand on the podium.
type Results struct {
	Day     int
	Place   Place
	Players []Player // in the order they answered
	Unit    string   // of the distances, "km"
	Lang    language.Language
	fitLat  float64
	fitLon  float64
}

const resultsDur = 12

// NewResults ranks nobody: the players come with their distance, points and
// rank, as the bot scored them.
func NewResults(day int, place Place, players []Player, unit, lang string) *Results {
	r := &Results{Day: day, Place: place, Players: players, Unit: unit, Lang: language.NewLanguage(lang)}
	lats, lons := []float64{place.Lat}, []float64{place.Lon}
	for _, p := range players {
		lats, lons = append(lats, p.Lat), append(lons, p.Lon)
	}
	r.fitLat = (slices.Min(lats) + slices.Max(lats)) / 2
	r.fitLon = (slices.Min(lons) + slices.Max(lons)) / 2
	return r
}

func (r *Results) Size() (int, int)  { return 720, 900 }
func (r *Results) Duration() float64 { return resultsDur }

// fitR is the radius of the globe that holds every pin and the place in a
// window w×h, with room above each pin for its head and to the right for its
// distance.
func (r *Results) fitR(w, h float64) float64 {
	R := 4000.0
	fit := func(lat, lon float64) {
		a, b, _ := Ortho(lat*rad, lon*rad, r.fitLat*rad, r.fitLon*rad)
		if a > 0 {
			R = min(R, (w/2-110)/a)
		}
		if a < 0 {
			R = min(R, (w/2-40)/-a)
		}
		if b > 0 {
			R = min(R, (h/2-72)/b)
		}
		if b < 0 {
			R = min(R, (h/2-34)/-b)
		}
	}
	fit(r.Place.Lat, r.Place.Lon)
	for _, p := range r.Players {
		fit(p.Lat, p.Lon)
	}
	return R
}

func (r *Results) Draw(c *Canvas, t float64) {
	c.Lang = r.Lang
	c.Background("results"+strconv.Itoa(r.Day), func() {
		c.Fill(field)
		c.header(r.Day, 64, 84)
	})
	shrink := inOut(lin(5.9, 6.8, t)) - inOut(lin(9.7, 10.5, t))
	const mx, my, mw = 32, 112, 656
	mh := mix(756, 480, shrink)
	cx, cy := 360.0, my+mh/2
	// At the end everything leaves the way it came and the camera pulls back
	// to the globe of the first frame: the loop has no seam.
	gone := smooth(9.9, 10.5, t)
	keep := 1 - gone
	// The place, once it is out.
	if pa := smooth(4.3, 4.9, t) * (1 - smooth(9.7, 10.3, t)); pa > 0 {
		cw := c.Text(Display, 30, r.Place.Country, 688, 82, Right, ink, pa, nil)
		c.Text(Display, 44, r.Place.Name, 688-cw-12, 82, Right, white, pa, nil)
	}
	c.RoundRect(mx, my, mw, mh, 28, 28, deep, 1, nil)
	clip := c.clip
	clip.Clear()
	clip.RoundRect(mx, my, mw, mh, 28, 28)

	// The camera flies from a turning globe to the part of the world the
	// pins are in.
	fly := inOut(lin(0.2, 2.4, t)) - inOut(lin(10.2, 11.8, t))
	lon0, lat0 := mix(-30, r.fitLon, fly)*rad, mix(22, r.fitLat, fly)*rad
	R := mix(250, r.fitR(mw, mh), inOut(lin(0.5, 2.5, t))-inOut(lin(10.1, 11.8, t)))
	s0, c0 := math.Sincos(lat0)
	P := func(lat, lon float64) (x, y float64) {
		a, b, _ := orthoDot(math.Sin(lat*rad), math.Cos(lat*rad), lon*rad, s0, c0, lon0)
		return cx + a*R, cy - b*R
	}
	ax, ay := P(r.Place.Lat, r.Place.Lon)
	since := t - 4.3
	c.mapDots(cx, cy, R, s0, c0, lon0, mx, my, mw, mh, ax, ay, since, clip)

	// The lines from the place to every pin, along the great circle.
	for i, p := range r.Players {
		pr := easeOut(lin(4.7+float64(i)*0.12, 5.6+float64(i)*0.12, t)) * (1 - inOut(lin(9.6, 10.3, t)))
		if pr <= 0 {
			continue
		}
		pts := make([][2]float64, 41)
		for s := range pts {
			la, lo := GreatCircle(r.Place.Lat, r.Place.Lon, p.Lat, p.Lon, pr*float64(s)/40)
			pts[s][0], pts[s][1] = P(la, lo)
		}
		c.mask.Dashed(pts, 2, 11, 4)
		c.Paint(c.mask, white, 0.95*keep, clip)
	}
	// The place: a target that lands and keeps a slow pulse.
	if since > 0 {
		s := back(lin(0, 0.5, since))
		for k := range 2 {
			ph := math.Mod(since+float64(k)*1.1, 2.2) / 2.2
			c.Ring(ax, ay, 12+ph*40, 3, white, 0.7*(1-ph)*keep, clip)
		}
		c.Disc(ax, ay, 13*s, white, keep, clip)
		c.Disc(ax, ay, 5.5*s, field, keep, clip)
	}
	// The pins drop in the order people answered, then take the colour of
	// their place.
	rc := smooth(5.7, 6.2, t)
	for i, p := range r.Players {
		t0 := 2.3 + float64(i)*0.32
		f := lin(t0, t0+0.42, t)
		if f <= 0 {
			continue
		}
		qx, qy := P(p.Lat, p.Lon)
		drop := (1-easeIn(f))*120 + easeIn(gone)*90
		sq := 0.0
		if t > t0+0.42 {
			g := lin(t0+0.42, t0+0.8, t)
			sq = math.Sin(g*math.Pi) * 0.18 * (1 - g)
		}
		to := ink
		if p.Rank >= 1 && p.Rank <= 3 {
			to = medal[p.Rank-1]
		}
		col := paper.Mix(to, rc)
		if a := smooth(t0, t0+0.1, t) * keep; a > 0 {
			c.Pin(qx, qy-drop, 44, PinStyle{Fill: col, Edge: deep, Hole: deep, Label: initial(p.Name), LabelColor: col, Squash: sq}, a, clip)
		}
		// The distance, beside the pin.
		if ka := smooth(5.3+float64(i)*0.12, 5.7+float64(i)*0.12, t) * keep; ka > 0 {
			s := Thousands(p.Km) + " " + r.Unit
			w := c.Measure(Body, 19, s) + 16
			bx, by := qx+18, qy-46
			c.RoundRect(bx, by, w, 26, 13, 13, deep, ka, clip)
			c.Text(Body, 19, s, bx+8, by+19, Left, white, ka, clip)
		}
	}
	r.podium(c, t)
}

// mapDots draws the 1° halftone land seen from above lat0, lon0, with the
// ripple that runs out from the place when it is revealed.
func (c *Canvas) mapDots(cx, cy, R, s0, c0, lon0, mx, my, mw, mh, ax, ay, since float64, clip *Mask) {
	sp := rad * R
	sea, land := c.mask, c.layer
	for _, d := range fine {
		a, b, z := orthoDot(d.sinLat, d.cosLat, d.lon, s0, c0, lon0)
		if z < 0.02 {
			continue
		}
		px, py := cx+a*R, cy-b*R
		if px < mx-10 || px > mx+mw+10 || py < my-10 || py > my+mh+10 {
			continue
		}
		sx, sy := a, -b
		lit := smooth(-0.15, 0.95, sx*light[0]+sy*light[1]+z*light[2])
		w := 0.0
		if since > 0 {
			ring, dist := since*340, math.Hypot(px-ax, py-ay)
			w = max(0, 1-math.Abs(dist-ring)/46) * max(0, 1-since/1.5)
		}
		k := 0.14 + 0.26*lit + 0.4*w
		if d.land {
			k = 0.3 + 0.62*lit + 0.5*w
		}
		rd := k * sp / 2
		if rd < 0.4 {
			continue
		}
		m := sea
		if d.land || w > 0.5 {
			m = land
		}
		m.Ellipse(px, py, max(0.4, rd*z), rd, math.Atan2(sy, sx))
	}
	c.Paint(sea, tint, 1, clip)
	c.Paint(land, white, 0.92, clip)
}

// podium puts the three closest on their steps: third, second, first, and
// dots fly out of the first place when it lands.
func (r *Results) podium(c *Canvas, t float64) {
	const base, cw = 888, 196
	cols := [3]struct{ x, h float64 }{{262, 172}, {52, 132}, {472, 102}}
	for _, ri := range []int{2, 1, 0} {
		i := slices.IndexFunc(r.Players, func(p Player) bool { return p.Rank == ri+1 })
		if i < 0 {
			continue
		}
		p, col := r.Players[i], cols[ri]
		t0 := 6.2 + float64(2-ri)*0.42
		down := 1 - easeIn(lin(9.5+float64(ri)*0.12, 10.1+float64(ri)*0.12, t))
		g := back(lin(t0, t0+0.6, t)) * down
		if g <= 0 {
			continue
		}
		h := col.h * g
		top := base - h
		c.RoundRect(col.x, top, cw, h+40, 18, 0, medal[ri], 1, nil)
		c.textMask(Display, 84, strconv.Itoa(ri+1), col.x+cw/2, top+76, Center)
		c.mask.ClipRect(col.x, top, col.x+cw, top+h)
		c.Paint(c.mask, field, 1, nil)
		a := smooth(t0+0.25, t0+0.6, t) * down
		if a <= 0 {
			continue
		}
		c.streakName(p.Name, p.Streak, col.x+cw/2, top-44, a)
		c.Text(BodyMedium, 21, Thousands(p.Km)+" "+r.Unit+" · "+Thousands(p.Points), col.x+cw/2, top-14, Center, paper, a, nil)
	}
	burst := t - (6.2 + 0.84 + 0.45)
	if burst <= 0 || burst >= 2.4 {
		return
	}
	rnd, top := rng(7), base-cols[0].h
	colors := []RGB{white, gold, paper, bronze, white}
	for i := range 90 {
		ang := -math.Pi/2 + (rnd()-0.5)*2.4
		v := 260 + rnd()*420
		size := 3 + rnd()*5
		life := 1.4 + rnd()*0.9
		if burst > life {
			continue
		}
		px := 360 + math.Cos(ang)*v*burst
		py := top + math.Sin(ang)*v*burst + 520*burst*burst
		c.Disc(px, py, size, colors[i%5], 1-smooth(life-0.5, life, burst), nil)
	}
}

// streakName writes a name centred on x, and after it a flame and the
// streak when there is one: the 🔥 of the messages, drawn, since the brand's
// faces have no emoji.
func (c *Canvas) streakName(name string, streak int, x, y, alpha float64) {
	if streak <= 0 {
		c.Text(Body, 28, name, x, y, Center, white, alpha, nil)
		return
	}
	n := strconv.Itoa(streak)
	const gap, flameW = 10, 15
	total := c.Measure(Body, 28, name) + gap + flameW + 3 + c.Measure(Body, 28, n)
	x0 := x - total/2
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

// initial is the first letter of a name, for its pin.
func initial(name string) string {
	for _, r := range name {
		return string(r)
	}
	return ""
}
