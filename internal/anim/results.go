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
	far     []bool  // a pin too far round the globe to show where it is
	east    float64 // how far the globe turns to show the far pins east, degrees
	west    float64 // and west
}

const resultsDur = 12

var resultsPodium = podium{h: [3]float64{172, 132, 102}, rise: 6.2, step: 0.42, down: 9.5, num: 84, numY: 76, nameY: -44, lineY: -14}

// NewResults ranks nobody: the players come with their distance, points and
// rank, as the bot scored them.
func NewResults(day int, place Place, players []Player, unit, unitM, lang string) *Results {
	r := &Results{Day: day, Place: place, Players: players, Unit: unit, UnitM: unitM, Lang: ShapingLang(lang)}
	r.fitLat, r.fitLon = r.aim()
	r.far = make([]bool, len(players))
	for i, p := range players {
		_, _, z := Ortho(p.Lat*rad, p.Lon*rad, r.fitLat*rad, r.fitLon*rad)
		r.far[i] = z < farZ
		// The globe turns towards a far pin, at most half way round.
		if r.far[i] {
			dl := math.Mod(p.Lon-r.fitLon+540, 360) - 180
			if dl > 0 {
				r.east = max(r.east, min(dl-60, maxTurn))
			} else {
				r.west = max(r.west, min(-dl-60, maxTurn))
			}
		}
	}
	return r
}

// maxTurn is as far as the globe turns to show a far pin, so the place stays
// in view.
const maxTurn = 55.0

// turn is how far the view has turned at t: while the podium stands, east to
// the far pins there and back, then west, a slow sine that ends where it
// began.
func (r *Results) turn(t float64) float64 {
	u := lin(5.9, 9.5, t)
	if u <= 0 || u >= 1 {
		return 0
	}
	if v := math.Sin(2 * math.Pi * u); v > 0 {
		return r.east * v
	} else {
		return r.west * v
	}
}

// A pin further than about 75° from the middle of the view would sit on the
// globe's edge or behind it. It goes where its line leaves the globe instead,
// with its name, so it still says who went which way.
const farZ = 0.26

// aim is the middle of the view: of the candidates (the middle of all the
// pins, the place, half way to each pin) the one that shows every pin near
// enough and is the tightest. When none does, the place: the far pins then
// stand round the edge in the very direction they are from it.
func (r *Results) aim() (lat, lon float64) {
	pts := [][3]float64{unitVec(r.Place.Lat, r.Place.Lon)}
	for _, p := range r.Players {
		pts = append(pts, unitVec(p.Lat, p.Lon))
	}
	sum := func(vs ...[3]float64) [3]float64 {
		var s [3]float64
		for _, v := range vs {
			s[0], s[1], s[2] = s[0]+v[0], s[1]+v[1], s[2]+v[2]
		}
		return s
	}
	cands := [][3]float64{sum(append(pts, pts[0])...), pts[0]}
	for _, p := range pts[1:] {
		cands = append(cands, sum(pts[0], p))
	}
	best, bestN, bestMin := pts[0], -1, -2.0
	for _, c := range cands {
		n := math.Sqrt(c[0]*c[0] + c[1]*c[1] + c[2]*c[2])
		if n < 1e-9 {
			continue
		}
		c = [3]float64{c[0] / n, c[1] / n, c[2] / n}
		dot := func(v [3]float64) float64 { return c[0]*v[0] + c[1]*v[1] + c[2]*v[2] }
		if dot(pts[0]) < farZ {
			continue
		}
		near, low := 0, 1.0
		for _, v := range pts {
			if d := dot(v); d >= farZ {
				near++
				low = min(low, d)
			}
		}
		if near > bestN || near == bestN && low > bestMin {
			best, bestN, bestMin = c, near, low
		}
	}
	if bestN < len(pts) {
		return r.Place.Lat, r.Place.Lon
	}
	return math.Asin(max(-1, min(1, best[2]))) / rad, math.Atan2(best[1], best[0]) / rad
}

func unitVec(lat, lon float64) [3]float64 {
	cl := math.Cos(lat * rad)
	return [3]float64{cl * math.Cos(lon*rad), cl * math.Sin(lon*rad), math.Sin(lat * rad)}
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
	whole := false
	for i, p := range r.Players {
		if r.far[i] {
			whole = true
			continue
		}
		fit(p.Lat, p.Lon)
	}
	// A far pin stands on the globe's edge, so the whole globe must show.
	if whole {
		R = min(R, (w/2 - 130), (h/2 - 80))
	}
	return R
}

// pinAt is where a pin stands: where it is, or for a far one, where the great
// circle from the place goes over the edge. pr is how much of the line is
// drawn, from the place; pts gets the visible part of it.
func (r *Results) pinAt(g globeView, i int, pr float64, pts [][2]float64) (x, y float64, line [][2]float64) {
	p := r.Players[i]
	line = pts[:0]
	px, py, pz := g.at(r.Place.Lat, r.Place.Lon)
	for s := range 41 {
		la, lo := GreatCircle(r.Place.Lat, r.Place.Lon, p.Lat, p.Lon, float64(s)/40)
		x, y, z := g.at(la, lo)
		if z < 0 {
			// Cut at the edge, between the last sample in front and this one.
			k := pz / (pz - z)
			x, y = px+(x-px)*k, py+(y-py)*k
			if float64(s)/40 <= pr {
				line = append(line, [2]float64{x, y})
			}
			return x, y, line
		}
		if float64(s)/40 <= pr {
			line = append(line, [2]float64{x, y})
		}
		px, py, pz = x, y, z
	}
	x, y, _ = g.at(p.Lat, p.Lon)
	return x, y, line
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
	g := newGlobeView(360, my+mh/2, R, mix(22, r.fitLat, fly)*rad, (mix(-30, r.fitLon, fly)+r.turn(t))*rad)
	g.x0, g.y0, g.x1, g.y1 = mx, my, mx+mw, my+mh
	ax, ay, _ := g.at(r.Place.Lat, r.Place.Lon)
	since := t - 4.3
	c.mapDots(g, ripple{x: ax, y: ay, since: since, speed: 340, life: 1.5}, 0.92, clip)

	// The lines from the place to every pin, along the great circle, and
	// where the pins stand.
	at := make([][2]float64, len(r.Players))
	buf := make([][2]float64, 0, 42)
	pinStep, lineStep := r.steps()
	for i := range r.Players {
		pr := easeOut(lin(4.7+float64(i)*lineStep, 5.6+float64(i)*lineStep, t)) * (1 - inOut(lin(9.6, 10.3, t)))
		x, y, line := r.pinAt(g, i, pr, buf)
		at[i] = [2]float64{x, y}
		if pr <= 0 || len(line) < 2 {
			continue
		}
		c.mask.Dashed(line, 2, 11, 4)
		c.Paint(c.mask, white, 0.95*keep, clip)
	}
	c.target(ax, ay, since, 12, 40, 13, 5.5, keep, clip)
	// The pins drop in the order people answered, then take the colour of
	// their place.
	rc := smooth(5.7, 6.2, t)
	for i, p := range r.Players {
		t0 := 2.3 + float64(i)*pinStep
		f := lin(t0, t0+0.42, t)
		if f <= 0 {
			continue
		}
		qx, qy := at[i][0], at[i][1]
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
	}
	r.chips(c, at, t, keep, clip, [4]float64{mx, my, mx + mw, my + mh})

	var win [3]*Winner
	for _, p := range r.Players {
		if p.Rank >= 1 && p.Rank <= 3 && win[p.Rank-1] == nil {
			win[p.Rank-1] = &Winner{Name: p.Name, Km: p.Km, Points: p.Points, Streak: p.Streak, Face: p.Face}
		}
	}
	c.podium(resultsPodium, win, r.Unit, r.UnitM, t)
	c.confetti(t-(6.2+0.84+0.45), podiumBase-resultsPodium.h[0])
}

// steps are the delays between one pin's drop and the next, and between one
// line or chip and the next. They shrink with the group, so the last pin
// lands before the podium rises however many people answered.
func (r *Results) steps() (pin, line float64) {
	n := float64(max(1, len(r.Players)))
	return min(0.32, 3.2/n), min(0.12, 0.6/n)
}

// maxChips is how many places get a name beside their pin: past that the
// chips only cover each other.
const maxChips = 12

// chips are the name and distance beside each pin. The best place first takes
// its spot; each chip goes to the side of its pin where it covers the least of
// the other chips, the pins' heads and the window's edge.
func (r *Results) chips(c *Canvas, at [][2]float64, t, keep float64, clip *Mask, win [4]float64) {
	order := make([]int, len(r.Players))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return r.Players[a].Rank - r.Players[b].Rank })
	if len(order) > maxChips {
		order = order[:maxChips]
	}
	_, step := r.steps()
	type box struct{ x, y, w, h, weight float64 }
	over := func(a, b box) float64 {
		return max(0, min(a.x+a.w, b.x+b.w)-max(a.x, b.x)) * max(0, min(a.y+a.h, b.y+b.h)-max(a.y, b.y))
	}
	// The pins' heads are taken from the start, but covering one is less bad
	// than covering another chip.
	var taken []box
	for _, q := range at {
		taken = append(taken, box{q[0] - 16, q[1] - 50, 32, 50, 0.4})
	}
	cost := func(a box) float64 {
		v := 0.0
		for _, b := range taken {
			v += over(a, b) * b.weight
		}
		inside := box{x: win[0], y: win[1], w: win[2] - win[0], h: win[3] - win[1]}
		return v + 2*(a.w*a.h-over(a, inside))
	}
	for _, i := range order {
		p := r.Players[i]
		ka := smooth(5.3+float64(i)*step, 5.7+float64(i)*step, t) * keep
		if ka <= 0 {
			continue
		}
		s := chipName(p.Name) + " · " + Distance(p.Km, r.Unit, r.UnitM)
		w := c.Measure(Body, 19, s) + 16
		qx, qy := at[i][0], at[i][1]
		var b box
		best := math.Inf(1)
		for _, dy := range []float64{-46, 4, -76, 34} {
			for _, x := range []float64{qx + 18, qx - 18 - w} {
				sp := box{x, qy + dy, w, 26, 1}
				if v := cost(sp); v < best-0.5 {
					b, best = sp, v
				}
			}
		}
		taken = append(taken, b)
		c.RoundRect(b.x, b.y, b.w, b.h, 13, 13, deep, ka, clip)
		c.Text(Body, 19, s, b.x+8, b.y+19, Left, white, ka, clip)
	}
}

// chipName keeps a chip short: the first word of a name, at most 10 letters.
func chipName(name string) string {
	rs := []rune(name)
	for i, ch := range rs {
		if ch == ' ' && i > 0 {
			rs = rs[:i]
			break
		}
	}
	if len(rs) > 10 {
		return string(rs[:9]) + "…"
	}
	return string(rs)
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

// initial is the first letter of a name, for its pin: a whole emoji when the
// name starts with one, a flag or a joined sequence included.
func initial(name string) string {
	rs := []rune(name)
	if len(rs) == 0 {
		return ""
	}
	n := 1
	if regional(rs[0]) && len(rs) > 1 && regional(rs[1]) {
		n = 2
	}
	for n < len(rs) && sticks(rs[n]) {
		n++
		if rs[n-1] == 0x200D && n < len(rs) {
			n++ // the code point the joiner joins on
		}
	}
	return string(rs[:n])
}

// regional is one half of a flag.
func regional(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }
