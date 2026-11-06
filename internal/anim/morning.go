// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"image"
	"math"
	"strconv"

	"github.com/go-text/typesetting/language"
)

// The window of the morning post: the panorama is rendered at ss times its
// size and averaged down.
const (
	winX, winY, winW = 32, 112, 656
	ss               = 2
)

// Morning is the post of the morning: the logo's globe on the field, the pin
// lands, the little planet of the day's panorama opens around it and unrolls
// into the street, turns, and rolls back into the globe.
type Morning struct {
	Day    int
	Pano   *Pano
	Reveal string // when the answer comes, "20:00"
	Lang   language.Language
	yaw    []float64
}

// The camera's timing. The planet turns 11°/s, the street pans 5°/s.
const (
	morningDur = 14
	planetF    = 200
	viewF      = 358
	pinLands   = 0.9
)

func NewMorning(day int, pano *Pano, reveal, lang string) *Morning {
	m := &Morning{Day: day, Pano: pano, Reveal: reveal, Lang: ShapingLang(lang)}
	n := morningDur*FPS + 2
	m.yaw = make([]float64, n)
	for i := 1; i < n; i++ {
		m.yaw[i] = m.yaw[i-1] + mix(11, 5, unroll(float64(i)/FPS))/FPS
	}
	at := m.yaw[int(math.Round(7.6*FPS))]
	for i := range m.yaw {
		m.yaw[i] = (m.yaw[i] - at - 15) * rad
	}
	return m
}

func (m *Morning) Size() (int, int)  { return 720, 900 }
func (m *Morning) Duration() float64 { return morningDur }

// unroll is 0 while the panorama is a planet and 1 while it is a street.
func unroll(t float64) float64 { return inOut(lin(2.3, 4.9, t)) - inOut(lin(10.4, 12.6, t)) }

type morningView struct {
	u float64
	View
}

func (m *Morning) view(t float64) morningView {
	u := unroll(t)
	i := min(int(math.Round(t*FPS)), len(m.yaw)-1)
	return morningView{u: u, View: View{
		Pitch: mix(-90, 3, sine(clamp01(u*1.12))) * rad,
		D:     mix(1, 0, smooth(0.25, 1, u)),
		F:     math.Exp(mix(math.Log(planetF), math.Log(viewF), smooth(0.1, 1, u))),
		Yaw:   m.yaw[i],
	}}
}

// nadirSpot is where the masked nadir is in the window, from its centre.
func nadirSpot(v View) (x, y float64, ok bool) {
	cp, sp := math.Cos(v.Pitch), math.Sin(v.Pitch)
	den := -sp + v.D
	if den <= 1e-3 {
		return 0, 0, false
	}
	return 0, v.F * cp / den, true
}

func (m *Morning) Draw(c *Canvas, t float64) {
	const cx, cy = winX + winW/2, winY + winW/2
	c.Lang = m.Lang
	c.Background("morning"+strconv.Itoa(m.Day), func() {
		c.Fill(field)
		c.header(m.Day, 64, 84)
	})
	v := m.view(t)
	// The planet opens from where the pin lands, as the ripple of the loader,
	// and closes the same way.
	open := easeOut(lin(pinLands, 1.9, t)) * (1 - easeIn(lin(12.6, 13.5, t)))
	shape := inOut(lin(2.4, 4.5, t)) - inOut(lin(10.6, 12.5, t))
	hw, cr := mix(300*open, winW/2, shape), mix(300*open, 28, shape)
	clip := c.clip
	clip.Clear()
	if open > 0 {
		clip.RoundRect(cx-hw, cy-hw, 2*hw, 2*hw, cr, cr)
		c.street(m.Pano, v.View, shape, clip)
		// The edge of the opening: a white ring that thins out.
		if open < 1 && shape < 0.01 {
			c.Ring(cx, cy, 300*open, 2+10*(1-open), white, 0.9*(1-open), nil)
		}
	}

	// The globe: alone on the field, then the core of the planet; it sinks
	// with the floor as the street rises and comes back up with it.
	spin := -0.6 + t/morningDur*2*math.Pi
	R := mix(74, 150, inOut(lin(12.6, 13.5, t)))
	if t < 7 {
		R = mix(150, 74, inOut(lin(1.0, 1.9, t)))
	}
	core := 1 - easeIn(lin(2.3, 3.4, t)) + easeOut(lin(11.5, 12.6, t))
	switch {
	case v.u < 1e-4:
		tipY := cy + R*0.08 - R*0.32
		c.Disc(cx, cy, R*92/74, field, 1, nil)
		c.logoGlobe(cx, cy+R*0.08, R, spin, &wave{x: cx, y: tipY, since: t - pinLands}, nil)
		// The pin hops once and lands as the planet opens; at the end of the
		// loop it stands, as at the start.
		hop := math.Pow(math.Sin(math.Pi*lin(0.2, pinLands, t)), 1.6) * 0.55
		f := lin(pinLands, pinLands+0.45, t)
		sq := math.Sin(f*math.Pi) * 0.16 * (1 - f)
		c.Pin(cx, tipY-hop*R, R*0.62, PinStyle{Fill: white, Edge: field, Hole: field, Squash: sq}, 1, nil)
	case core > 0.01 && open > 0:
		if x, y, ok := nadirSpot(v.View); ok {
			c.badge(cx+x, cy+y, 92*core, spin, clip)
		}
	}
	// The dots that run to the reveal.
	c.timeline(40, 548, 832, t, 3.5, 0)
	c.Text(Display, 58, m.Reveal, 688, 852, Right, white, 1, nil)
}

// street draws the panorama into the window through the clip, with a soft
// shade towards the planet's edge that is gone once it is a street.
func (c *Canvas) street(p *Pano, v View, shape float64, clip *Mask) {
	const n = winW * ss
	box := clip.box.Intersect(image.Rect(winX, winY, winX+winW, winY+winW))
	if box.Empty() {
		return
	}
	v.F *= ss
	r := image.Rect((box.Min.X-winX)*ss, (box.Min.Y-winY)*ss, (box.Max.X-winX)*ss, (box.Max.Y-winY)*ss)
	buf := p.render(&c.pano, n, v, r)
	shade := float32(0.35 * (1 - shape))
	for y := box.Min.Y; y < box.Max.Y; y++ {
		j := (y - winY) * ss
		dy := float64(y) + 0.5 - (winY + winW/2)
		for x := box.Min.X; x < box.Max.X; x++ {
			k := clip.A[y*c.W+x]
			if k <= 0 {
				continue
			}
			i := (x - winX) * ss
			a, b := 3*(j*n+i), 3*((j+1)*n+i)
			pr := (buf[a] + buf[a+3] + buf[b] + buf[b+3]) / 4
			pg := (buf[a+1] + buf[a+4] + buf[b+1] + buf[b+4]) / 4
			pb := (buf[a+2] + buf[a+5] + buf[b+2] + buf[b+5]) / 4
			if shade > 0 {
				dx := float64(x) + 0.5 - (winX + winW/2)
				s := shade * float32(clamp01((math.Hypot(dx, dy)-200)/130))
				pr, pg, pb = pr+(deep.R-pr)*s, pg+(deep.G-pg)*s, pb+(deep.B-pb)*s
			}
			o := 3 * (y*c.W + x)
			c.Pix[o] += (pr - c.Pix[o]) * k
			c.Pix[o+1] += (pg - c.Pix[o+1]) * k
			c.Pix[o+2] += (pb - c.Pix[o+2]) * k
		}
	}
}
