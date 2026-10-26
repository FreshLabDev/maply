// SPDX-License-Identifier: Apache-2.0
package anim

import "math"

// The pin of the logo, in its own units: a circle of radius 150 around
// (320, 289) drawn out to a tip at (320, 501), with a hole of radius 60.
const (
	pinCX, pinCY, pinR = 320, 289, 150
	pinTip             = 501
	pinHole            = 60
	pinHeight          = 362
	faceR              = 112 // a photo in the head leaves a ring of the fill
)

// pinDist is the signed distance from x, y to the pin's outline in pin
// units, negative inside. The sides are tangent to the circle, so the
// outline is the hull of the circle and the tip.
func pinDist(x, y float64) float64 {
	const h, r1 = pinTip - pinCY, pinR
	px, py := math.Abs(x-pinCX), y-pinCY
	b := float64(r1) / h
	a := math.Sqrt(1 - b*b)
	switch k := -b*px + a*py; {
	case k < 0:
		return math.Hypot(px, py) - r1
	case k > a*h:
		return math.Hypot(px, py-h)
	}
	return a*px + b*py - r1
}

// PinStyle is how a pin is painted. The edge is a stroke round the outline
// and the hole that keeps the pin apart from what is behind it.
type PinStyle struct {
	Fill, Edge, Hole RGB
	NoEdge           bool    // the edge is transparent
	EdgeWidth        float64 // in pin units; 0 means 34
	Label            string  // a letter in the hole
	LabelColor       RGB
	Face             *Face   // a photo filling the head instead of the hole and the letter
	Squash           float64 // > 0 flattens the pin as it lands
}

// Pin draws the logo's pin with its tip at tx, ty, h pixels tall.
func (c *Canvas) Pin(tx, ty, h float64, st PinStyle, alpha float64, clip *Mask) {
	k := h / pinHeight
	kx, ky := k*(1+st.Squash), k*(1-st.Squash)
	kp := math.Sqrt(kx * ky) // pixels per pin unit, for the edges' softness
	ew := st.EdgeWidth
	if ew == 0 {
		ew = 34
	}
	e := ew / 2
	at := func(u, v float64) (float64, float64) { return tx + (u-pinCX)*kx, ty + (v-pinTip)*ky }
	unit := func(x, y float64) (float64, float64) { return (x-tx)/kx + pinCX, (y-ty)/ky + pinTip }
	hx, hy := at(pinCX, pinCY)
	x0, y0 := at(pinCX-pinR-e, pinCY-pinR-e)
	x1, y1 := at(pinCX+pinR+e, pinTip+e)
	x0, y0, x1, y1 = x0-1, y0-1, x1+1, y1+1

	if st.Face != nil {
		// The head is the photo inside a ring of the pin's colour.
		if !st.NoEdge {
			c.mask.Field(x0, y0, x1, y1, func(x, y float64) float32 {
				u, v := unit(x, y)
				return cover((math.Abs(pinDist(u, v)) - e) * kp)
			})
			c.Paint(c.mask, st.Edge, alpha, clip)
		}
		c.mask.Field(x0, y0, x1, y1, func(x, y float64) float32 {
			u, v := unit(x, y)
			return cover(pinDist(u, v) * kp)
		})
		c.Paint(c.mask, st.Fill, alpha, clip)
		c.Face(st.Face, hx, hy, faceR*kp, alpha, clip)
		return
	}
	c.mask.Ellipse(hx, hy, 64*kx, 64*ky, 0)
	c.Paint(c.mask, st.Hole, alpha, clip)
	if !st.NoEdge {
		c.mask.Field(x0, y0, x1, y1, func(x, y float64) float32 {
			u, v := unit(x, y)
			hole := math.Hypot(u-pinCX, v-pinCY) - pinHole
			return max(cover((math.Abs(pinDist(u, v))-e)*kp), cover((math.Abs(hole)-e)*kp))
		})
		c.Paint(c.mask, st.Edge, alpha, clip)
	}
	c.mask.Field(x0, y0, x1, y1, func(x, y float64) float32 {
		u, v := unit(x, y)
		hole := math.Hypot(u-pinCX, v-pinCY) - pinHole
		return min(cover(pinDist(u, v)*kp), cover(-hole*kp))
	})
	c.Paint(c.mask, st.Fill, alpha, clip)
	if st.Label != "" {
		r := Body.Layout(st.Label, 96, c.Lang)
		ox, oy := pinCX-r.Width/2, 296+r.middle
		c.mask.Run(r, &c.text, func(p [2]float64) [2]float64 {
			x, y := at(ox+p[0], oy+p[1])
			return [2]float64{x, y}
		})
		c.Paint(c.mask, st.LabelColor, alpha, clip)
	}
}
