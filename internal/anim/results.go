// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"math"
	"slices"
	"strconv"

	"github.com/go-text/typesetting/language"
)

// Player is one pin on the map of the results.
type Player struct {
	Name       string
	Lat, Lon   float64
	Km, Points float64
	Rank       int   // 1 is the closest
	Streak     int   // days in a row, shown on the podium when > 0
	Face       *Face // the profile photo; nil draws the initial
}

// Results is the post of the evening in a group: the camera flies to where
// everybody's pins are, the place comes out, lines run to every pin, and the
// three closest stand on the podium.
type Results struct {
	Day     int
	Place   Place
	Players []Player // in the order they answered
	Unit    string   // of the distances, "km"
	UnitM   string   // of the distances under a kilometre, "m"
	Lang    language.Language
	fitLat  float64
	fitLon  float64
}

const resultsDur = 12

var resultsPodium = podium{h: [3]float64{172, 132, 102}, rise: 6.2, step: 0.42, down: 9.5, num: 84, numY: 76, nameY: -44, lineY: -14}

// NewResults ranks nobody: the players come with their distance, points and
// rank, as the bot scored them.
func NewResults(day int, place Place, players []Player, unit, unitM, lang string) *Results {
	r := &Results{Day: day, Place: place, Players: players, Unit: unit, UnitM: unitM, Lang: ShapingLang(lang)}
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
	// At the end everything leaves the way it came and the camera pulls back
	// to the globe of the first frame: the loop has no seam.
	gone := smooth(9.9, 10.5, t)
	keep := 1 - gone
	c.placeLabel(r.Place, smooth(4.3, 4.9, t)*(1-smooth(9.7, 10.3, t)))
	c.RoundRect(mx, my, mw, mh, 28, 28, deep, 1, nil)
	clip := c.clip
	clip.Clear()
	clip.RoundRect(mx, my, mw, mh, 28, 28)

	// The camera flies from a turning globe to the part of the world the
	// pins are in.
	fly := inOut(lin(0.2, 2.4, t)) - inOut(lin(10.2, 11.8, t))
	R := mix(250, r.fitR(mw, mh), inOut(lin(0.5, 2.5, t))-inOut(lin(10.1, 11.8, t)))
	g := newGlobeView(360, my+mh/2, R, mix(22, r.fitLat, fly)*rad, mix(-30, r.fitLon, fly)*rad)
	g.x0, g.y0, g.x1, g.y1 = mx, my, mx+mw, my+mh
	ax, ay, _ := g.at(r.Place.Lat, r.Place.Lon)
	since := t - 4.3
	c.mapDots(g, ripple{x: ax, y: ay, since: since, speed: 340, life: 1.5}, 0.92, clip)

	// The lines from the place to every pin, along the great circle.
	for i, p := range r.Players {
		pr := easeOut(lin(4.7+float64(i)*0.12, 5.6+float64(i)*0.12, t)) * (1 - inOut(lin(9.6, 10.3, t)))
		if pr <= 0 {
			continue
		}
		pts := make([][2]float64, 41)
		for s := range pts {
			la, lo := GreatCircle(r.Place.Lat, r.Place.Lon, p.Lat, p.Lon, pr*float64(s)/40)
			pts[s][0], pts[s][1], _ = g.at(la, lo)
		}
		c.mask.Dashed(pts, 2, 11, 4)
		c.Paint(c.mask, white, 0.95*keep, clip)
	}
	c.target(ax, ay, since, 12, 40, 13, 5.5, keep, clip)
	// The pins drop in the order people answered, then take the colour of
	// their place.
	rc := smooth(5.7, 6.2, t)
	for i, p := range r.Players {
		t0 := 2.3 + float64(i)*0.32
		f := lin(t0, t0+0.42, t)
		if f <= 0 {
			continue
		}
		qx, qy, _ := g.at(p.Lat, p.Lon)
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
			h := 44.0
			if p.Face != nil {
				h = 56
			}
			c.Pin(qx, qy-drop, h, PinStyle{Fill: col, Edge: deep, Hole: deep, Label: initial(p.Name), LabelColor: col, Squash: sq, Face: p.Face}, a, clip)
		}
		// The distance, beside the pin.
		if ka := smooth(5.3+float64(i)*0.12, 5.7+float64(i)*0.12, t) * keep; ka > 0 {
			s := Distance(p.Km, r.Unit, r.UnitM)
			w := c.Measure(Body, 19, s) + 16
			bx, by := qx+18, qy-46
			c.RoundRect(bx, by, w, 26, 13, 13, deep, ka, clip)
			c.Text(Body, 19, s, bx+8, by+19, Left, white, ka, clip)
		}
	}

	var win [3]*Winner
	for _, p := range r.Players {
		if p.Rank >= 1 && p.Rank <= 3 && win[p.Rank-1] == nil {
			win[p.Rank-1] = &Winner{Name: p.Name, Km: p.Km, Points: p.Points, Streak: p.Streak, Face: p.Face}
		}
	}
	c.podium(resultsPodium, win, r.Unit, r.UnitM, t)
	c.confetti(t-(6.2+0.84+0.45), podiumBase-resultsPodium.h[0])
}

// confetti flies out of the first place when it lands, since seconds ago,
// from the top of its step.
func (c *Canvas) confetti(since, top float64) {
	if since <= 0 || since >= 2.4 {
		return
	}
	rnd := rng(7)
	colors := []RGB{white, gold, paper, bronze, white}
	for i := range 90 {
		ang := -math.Pi/2 + (rnd()-0.5)*2.4
		v := 260 + rnd()*420
		size := 3 + rnd()*5
		life := 1.4 + rnd()*0.9
		if since > life {
			continue
		}
		px := 360 + math.Cos(ang)*v*since
		py := top + math.Sin(ang)*v*since + 520*since*since
		c.Disc(px, py, size, colors[i%5], 1-smooth(life-0.5, life, since), nil)
	}
}

// initial is the first letter of a name, for its pin.
func initial(name string) string {
	for _, r := range name {
		return string(r)
	}
	return ""
}
