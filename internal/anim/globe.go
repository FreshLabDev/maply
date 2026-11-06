// SPDX-License-Identifier: Apache-2.0
package anim

import (
	_ "embed"
	"math"
)

// The land bits of the halftone globes, written by gen_land.go from the
// countries of internal/geo: 4° dots for the logo's globe, as web/app/land.js,
// and 1° dots for the map of the results.
var (
	//go:embed land4.bin
	land4 []byte
	//go:embed land1.bin
	land1 []byte

	coarse = dots(4, land4)
	fine   = dots(1, land1)
)

const rad = math.Pi / 180

// light falls from the upper left, in front: x right, y down, z towards the
// viewer.
var light = func() [3]float64 {
	v := [3]float64{-0.62, -0.58, 0.53}
	l := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	return [3]float64{v[0] / l, v[1] / l, v[2] / l}
}()

type dot struct {
	lon, sinLat, cosLat float64 // lon in radians
	land                bool
}

// dots lays the globe's dots in rows along the parallels, step degrees apart,
// every other row shifted by half a dot.
func dots(step float64, bits []byte) []dot {
	var out []dot
	n := 0
	for i, lat := 0, -90+step/2; lat < 90; i, lat = i+1, lat+step {
		count := max(1, int(math.Round(360*math.Cos(lat*rad)/step)))
		for j := 0; j < count; j, n = j+1, n+1 {
			lon := -180 + (float64(j)+0.5+float64(i%2)*0.5)*360/float64(count)
			out = append(out, dot{lon: lon * rad, sinLat: math.Sin(lat * rad), cosLat: math.Cos(lat * rad), land: bits[n>>3]>>(n&7)&1 == 1})
		}
	}
	return out
}

// Ortho projects lat, lon onto the globe seen from above lat0, lon0, all in
// radians: x right, y up, z towards the viewer (behind the globe when < 0).
func Ortho(lat, lon, lat0, lon0 float64) (x, y, z float64) {
	return orthoDot(math.Sin(lat), math.Cos(lat), lon, math.Sin(lat0), math.Cos(lat0), lon0)
}

func orthoDot(sl, cl, lon, s0, c0, lon0 float64) (x, y, z float64) {
	sd, cd := math.Sincos(lon - lon0)
	return cl * sd, c0*sl - s0*cl*cd, s0*sl + c0*cl*cd
}

// GreatCircle is the point a fraction t of the way from a to b along the
// shortest path on the sphere, in degrees, as d3.geoInterpolate.
func GreatCircle(lat1, lon1, lat2, lon2, t float64) (lat, lon float64) {
	v := func(lat, lon float64) [3]float64 {
		cl := math.Cos(lat * rad)
		return [3]float64{cl * math.Cos(lon*rad), cl * math.Sin(lon*rad), math.Sin(lat * rad)}
	}
	a, b := v(lat1, lon1), v(lat2, lon2)
	d := math.Acos(max(-1, min(1, a[0]*b[0]+a[1]*b[1]+a[2]*b[2])))
	if d < 1e-12 {
		return lat1, lon1
	}
	ka, kb := math.Sin((1-t)*d)/math.Sin(d), math.Sin(t*d)/math.Sin(d)
	p := [3]float64{ka*a[0] + kb*b[0], ka*a[1] + kb*b[1], ka*a[2] + kb*b[2]}
	return math.Atan2(p[2], math.Hypot(p[0], p[1])) / rad, math.Atan2(p[1], p[0]) / rad
}

// Km is the great-circle distance in kilometres.
func Km(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2 := lat1*rad, lat2*rad
	dp, dl := p2-p1, (lon2-lon1)*rad
	h := math.Pow(math.Sin(dp/2), 2) + math.Cos(p1)*math.Cos(p2)*math.Pow(math.Sin(dl/2), 2)
	return 2 * 6371 * math.Asin(math.Sqrt(h))
}

// wave is a ripple through the dots: where it started and how long ago.
type wave struct {
	x, y, since float64
}

// logoGlobe draws the globe of the logo: 4° dots, tilted, lit from the upper
// left, turned by spin radians.
func (c *Canvas) logoGlobe(cx, cy, r, spin float64, w *wave, clip *Mask) {
	const tilt, view = 23.4 * rad, 24 * rad
	sp := 4 * rad
	ct, st, cv, sv := math.Cos(tilt), math.Sin(tilt), math.Cos(view), math.Sin(view)
	sea, land := c.mask, c.layer
	for _, d := range coarse {
		sl, cl := math.Sincos(d.lon + spin)
		px0, py0, pz0 := d.cosLat*cl, d.cosLat*sl, d.sinLat
		x1, z1 := px0*cv+pz0*sv, -px0*sv+pz0*cv
		if x1 < 0.02 {
			continue
		}
		sx, sy, nz := py0*ct+z1*st, -(-py0*st + z1*ct), x1
		lit := smooth(-0.15, 0.95, sx*light[0]+sy*light[1]+nz*light[2])
		px, py := cx+sx*r, cy+sy*r
		k := 0.0
		if w != nil && w.since >= 0 {
			ring, dist := w.since/0.65, math.Hypot(px-w.x, py-w.y)/r
			k = max(0, 1-math.Abs(dist-ring)/0.09) * max(0, 1-ring)
		}
		size := 0.12 + 0.3*lit + 0.35*k
		if d.land {
			size = 0.26 + 0.68*lit + 0.55*k
		}
		rd := size * sp * r / 2
		if rd < 0.4 {
			continue
		}
		m := sea
		if d.land || k > 0.5 {
			m = land
		}
		m.Ellipse(px, py, max(0.4, rd*nz), rd, math.Atan2(sy, sx))
	}
	c.Paint(sea, tint, 1, clip)
	c.Paint(land, white, 1, clip)
}

// badge is the avatar: the globe in a field disc, with its pin.
func (c *Canvas) badge(cx, cy, r, spin float64, clip *Mask) {
	R := r * 74 / 92
	c.Disc(cx, cy, r, field, 1, clip)
	c.logoGlobe(cx, cy+R*0.08, R, spin, nil, clip)
	c.Pin(cx, cy+R*0.08-R*0.32, R*0.62, PinStyle{Fill: white, Edge: field, Hole: field}, 1, clip)
}
