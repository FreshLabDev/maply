// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"fmt"
	"math"
	"strconv"

	"github.com/go-text/typesetting/language"
)

// Intrigue is the afternoon tease: what time it is there now, on a dial of
// the day that is dark or light as it is there, and how much of the chat's
// day is left before the answer.
type Intrigue struct {
	Day          int
	Hour, Minute int     // the time there
	Weather      string  // "22 °C", or empty
	ChatHour     float64 // the time in the chat when the post goes out, 16.5 for 16:30
	Reveal       string  // "20:00"
	Lang         language.Language
}

const intrigueDur = 12

func NewIntrigue(day, hour, minute int, weather string, chatHour float64, reveal, lang string) *Intrigue {
	return &Intrigue{Day: day, Hour: hour, Minute: minute, Weather: weather, ChatHour: chatHour, Reveal: reveal, Lang: ShapingLang(lang)}
}

func (in *Intrigue) Size() (int, int)  { return 720, 720 }
func (in *Intrigue) Duration() float64 { return intrigueDur }

// dialAngle is where hour h is on the dial: midnight at the bottom, noon on
// top, the day running clockwise.
func dialAngle(h float64) float64 { return math.Pi/2 + h/24*2*math.Pi }

func (in *Intrigue) Draw(c *Canvas, t float64) {
	const cx, cy, R = 360, 360, 230
	c.Lang = in.Lang
	hour := float64(in.Hour) + float64(in.Minute)/60
	night := math.Sin(dialAngle(hour)) > -0.1
	c.Background("intrigue"+strconv.Itoa(in.Day), func() {
		c.Fill(field)
		c.header(in.Day, 46, 62)
	})
	// Stars inside the dial when it is night there.
	if night {
		r := rng(3)
		for range 46 {
			a, d, ph, s := r()*2*math.Pi, math.Sqrt(r())*(R-40), r(), 1+r()*2.2
			tw := 0.25 + 0.75*(0.5+0.5*math.Cos(2*math.Pi*(t/3+ph)))
			c.Disc(cx+math.Cos(a)*d, cy+math.Sin(a)*d*0.9, s, paper, tw*0.7, nil)
		}
	}
	// The day as a ring of dots: big and white where the sun is up, a glint
	// running round twice a loop.
	const n = 72
	for i := range n {
		a := dialAngle(float64(i) / n * 24)
		up := -math.Sin(a)
		gl := math.Pow(max(0, math.Cos(a-dialAngle(hour)-t/6*2*math.Pi)), 30)
		r := 2.6 + 3.8*smooth(-0.15, 1, up) + 3*gl
		col := tint.Mix(white, smooth(-0.05, 0.35, up)).Mix(white, gl)
		c.Disc(cx+math.Cos(a)*R, cy+math.Sin(a)*R, r, col, 1, nil)
	}
	// The moon or the sun where the hour is, with a slow pulse.
	a := dialAngle(hour)
	mx, my := cx+math.Cos(a)*R, cy+math.Sin(a)*R
	for k := range 2 {
		ph := math.Mod(t+float64(k)*1.5, 3) / 3
		c.Ring(mx, my, 26+ph*34, 3, white, 0.55*(1-ph), nil)
	}
	c.Disc(mx, my, 30, field, 1, nil)
	if night {
		c.Disc(mx, my, 22, white, 1, nil)
		c.Disc(mx+10, my-7, 19, field, 1, nil)
	} else {
		c.Disc(mx, my, 14, white, 1, nil)
		for i := range 8 {
			b := float64(i)/8*2*math.Pi + t/6*math.Pi/2
			c.Disc(mx+math.Cos(b)*22, my+math.Sin(b)*22, 3, white, 1, nil)
		}
	}
	// The time there, its colon ticking, and the weather.
	hh, mm := fmt.Sprintf("%02d", in.Hour), fmt.Sprintf("%02d", in.Minute)
	wh, wc, wm := c.Measure(Display, 196, hh), c.Measure(Display, 196, ":"), c.Measure(Display, 196, mm)
	x0 := cx - (wh+wc+wm)/2
	c.Text(Display, 196, hh, x0, cy+52, Left, white, 1, nil)
	c.Text(Display, 196, ":", x0+wh, cy+40, Left, white, 0.25+0.75*smooth(-0.3, 0.3, math.Cos(2*math.Pi*t)), nil)
	c.Text(Display, 196, mm, x0+wh+wc, cy+52, Left, white, 1, nil)
	if in.Weather != "" {
		c.Text(Display, 64, in.Weather, cx, cy+128, Center, ink, 1, nil)
	}
	// Here: how much of the day is left before the answer. The badge turns
	// once a loop.
	c.badge(60, 664, 28, -0.6+t/intrigueDur*2*math.Pi, nil)
	c.timeline(112, 552, 664, t, 3, (in.ChatHour-7)/13)
	c.Text(Display, 50, in.Reveal, 688, 682, Right, white, 1, nil)
}
