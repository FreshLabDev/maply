// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"image"
	"image/draw"
	"math"
)

// Pano is an equirectangular panorama with its mip levels, read-only once
// built, so the frames rendered at once share it.
type Pano struct {
	lv []mip
}

type mip struct {
	w, h int
	pix  []float32 // r, g, b in sRGB
}

// NewPano takes the panorama of the day, 2:1, and builds its mip levels by
// halving it down to one pixel high.
func NewPano(img image.Image) *Pano {
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Rect, img, b.Min, draw.Src)
	l := mip{w: b.Dx(), h: b.Dy(), pix: make([]float32, 3*b.Dx()*b.Dy())}
	for i, j := 0, 0; i < len(l.pix); i, j = i+3, j+4 {
		l.pix[i], l.pix[i+1], l.pix[i+2] = float32(rgba.Pix[j])/255, float32(rgba.Pix[j+1])/255, float32(rgba.Pix[j+2])/255
	}
	p := &Pano{lv: []mip{l}}
	for l.w > 1 && l.h > 1 {
		n := mip{w: l.w / 2, h: l.h / 2}
		n.pix = make([]float32, 3*n.w*n.h)
		for y := 0; y < n.h; y++ {
			for x := 0; x < n.w; x++ {
				for ch := 0; ch < 3; ch++ {
					at := func(dx, dy int) float32 { return l.pix[3*((2*y+dy)*l.w+2*x+dx)+ch] }
					n.pix[3*(y*n.w+x)+ch] = (at(0, 0) + at(1, 0) + at(0, 1) + at(1, 1)) / 4
				}
			}
		}
		p.lv = append(p.lv, n)
		l = n
	}
	return p
}

// bilinear samples level l at u, v: u wraps round, v stops at the poles.
func (l *mip) bilinear(u, v float64) (r, g, b float32) {
	x, y := u*float64(l.w)-0.5, v*float64(l.h)-0.5
	fx, fy := math.Floor(x), math.Floor(y)
	tx, ty := float32(x-fx), float32(y-fy)
	x0 := int(fx) % l.w
	if x0 < 0 {
		x0 += l.w
	}
	x1 := x0 + 1
	if x1 == l.w {
		x1 = 0
	}
	y0, y1 := int(fy), int(fy)+1
	y0, y1 = min(max(y0, 0), l.h-1), min(max(y1, 0), l.h-1)
	p := l.pix
	a, b2, c, d := 3*(y0*l.w+x0), 3*(y0*l.w+x1), 3*(y1*l.w+x0), 3*(y1*l.w+x1)
	lerp := func(i int) float32 {
		top := p[a+i] + (p[b2+i]-p[a+i])*tx
		bot := p[c+i] + (p[d+i]-p[c+i])*tx
		return top + (bot-top)*ty
	}
	return lerp(0), lerp(1), lerp(2)
}

// sample filters between the two mip levels around lod, as a GPU's
// trilinear filtering does.
func (p *Pano) sample(u, v, lod float64) (r, g, b float32) {
	lod = max(0, min(lod, float64(len(p.lv)-1)))
	i := int(lod)
	r, g, b = p.lv[i].bilinear(u, v)
	if f := float32(lod - float64(i)); f > 0 && i+1 < len(p.lv) {
		r2, g2, b2 := p.lv[i+1].bilinear(u, v)
		r, g, b = r+(r2-r)*f, g+(g2-g)*f, b+(b2-b)*f
	}
	return
}

// View is where the camera of the morning post looks. It sits D behind the
// centre of the unit sphere the panorama is painted on: D = 1 is the
// stereographic little planet, D = 0 a plain view. F is the focal length in
// pixels of the image being rendered.
type View struct {
	D, F, Yaw, Pitch float64
}

// ray is where the pixel q (from the centre, y up) lands on the panorama:
// u, v in 0..1 and the latitude in degrees.
func (v *View) ray(qx, qy float64, cp, sp, cy, sy float64) (u, w, lat float64) {
	l := 1 / math.Sqrt(qx*qx+qy*qy+v.F*v.F)
	dx, dy, dz := qx*l, qy*l, v.F*l
	b := -v.D * dz
	t := -b + math.Sqrt(max(b*b-(v.D*v.D-1), 0))
	px, py, pz := t*dx, t*dy, t*dz-v.D
	l = 1 / math.Sqrt(px*px+py*py+pz*pz)
	px, py, pz = px*l, py*l, pz*l
	ry, rz := py*cp+pz*sp, -py*sp+pz*cp
	rx, rz := px*cy+rz*sy, -px*sy+rz*cy
	ry = max(-1, min(1, ry))
	la := atan2(ry, math.Sqrt(1-ry*ry))
	return 0.5 + atan2(rx, rz)/(2*math.Pi), 0.5 - la/math.Pi, la / rad
}

type panoScratch struct {
	buf  []float32
	rows [2][]float64 // u, v, lat per column of a row and the next
}

// nadir is the brand's deep green the painted nadir fades into.
var nadir = RGB{0.102, 0.255, 0.169}

// render draws the view into an n×n image, only the pixels in r.
func (p *Pano) render(s *panoScratch, n int, v View, r image.Rectangle) []float32 {
	if len(s.buf) < 3*n*n {
		s.buf = make([]float32, 3*n*n)
	}
	r = r.Intersect(image.Rect(0, 0, n, n))
	if r.Empty() {
		return s.buf
	}
	cp, sp := math.Cos(v.Pitch), math.Sin(v.Pitch)
	cy, sy := math.Cos(v.Yaw), math.Sin(v.Yaw)
	cols := r.Dx() + 1
	for i := range s.rows {
		if len(s.rows[i]) < 3*cols {
			s.rows[i] = make([]float64, 3*cols)
		}
	}
	half := float64(n) / 2
	fill := func(row []float64, y int) {
		qy := half - (float64(y) + 0.5)
		for i := 0; i < cols; i++ {
			qx := float64(r.Min.X+i) + 0.5 - half
			row[3*i], row[3*i+1], row[3*i+2] = v.ray(qx, qy, cp, sp, cy, sy)
		}
	}
	w0, h0 := float64(p.lv[0].w), float64(p.lv[0].h)
	cur, next := s.rows[0], s.rows[1]
	fill(cur, r.Min.Y)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		fill(next, y+1)
		for i := 0; i < cols-1; i++ {
			u, w, lat := cur[3*i], cur[3*i+1], cur[3*i+2]
			dxu, dxv := wrap(cur[3*i+3]-u)*w0, (cur[3*i+4]-w)*h0
			dyu, dyv := wrap(next[3*i]-u)*w0, (next[3*i+1]-w)*h0
			a, b := dxu*dxu+dxv*dxv, dyu*dyu+dyv*dyv
			// A little sharper than isotropic filtering, as the anisotropic
			// filtering of the design's GPU is.
			rho2 := max(max(a, b)/4, min(a, b))
			lod := 0.0
			if rho2 > 1 {
				lod = 0.5 * math.Log2(rho2)
			}
			cr, cg, cb := p.sample(u, w, lod)
			if k := float32(smooth(-39.5, -46, lat)); k > 0 {
				cr, cg, cb = cr+(nadir.R-cr)*k, cg+(nadir.G-cg)*k, cb+(nadir.B-cb)*k
			}
			o := 3 * (y*n + r.Min.X + i)
			s.buf[o], s.buf[o+1], s.buf[o+2] = cr, cg, cb
		}
		cur, next = next, cur
	}
	return s.buf
}

// wrap brings a step in u across the seam of the panorama back to the short
// way round.
func wrap(d float64) float64 { return d - math.Floor(d+0.5) }

// atan2 is math.Atan2 to within 1e-7 radians and several times faster: the
// panorama needs two for every pixel of every frame.
func atan2(y, x float64) float64 {
	ax, ay := math.Abs(x), math.Abs(y)
	if ax == 0 && ay == 0 {
		return 0
	}
	a := min(ax, ay) / max(ax, ay)
	s := a * a
	r := a * (1 + s*(-0.3333314528+s*(0.1999355085+s*(-0.1420889944+s*(0.1065626393+s*(-0.0752896400+s*(0.0429096138+s*(-0.0161657367+s*0.0028662257))))))))
	if ay > ax {
		r = math.Pi/2 - r
	}
	if x < 0 {
		r = math.Pi - r
	}
	if y < 0 {
		r = -r
	}
	return r
}
