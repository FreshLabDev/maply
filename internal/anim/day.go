// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"image"

	"github.com/go-text/typesetting/language"
)

// Stat is one of the world's numbers of a day on its card.
type Stat struct{ Value, Label string }

// DayCard is the picture of a link to a closed day's page: the day's street
// across the whole card, "maply #N" over its sky, and the place with the
// world's numbers over a shade of the field at the bottom. It stands still.
type DayCard struct {
	Day     int
	Photo   *image.RGBA // 1200×630, the street or the photo
	City    string
	Country string
	Stats   []Stat
	Lang    language.Language
}

func NewDayCard(day int, photo *image.RGBA, lang string) *DayCard {
	return &DayCard{Day: day, Photo: photo, Lang: ShapingLang(lang)}
}

func (d *DayCard) Size() (int, int)  { return 1200, 630 }
func (d *DayCard) Duration() float64 { return 1 }

func (d *DayCard) Draw(c *Canvas, _ float64) {
	c.Lang = d.Lang
	c.Fill(deep)
	c.picture(d.Photo)
	// The shade the texts stand on: the field from below, a lighter one from
	// above for the logo.
	for y := range c.H {
		f := float64(y) / float64(c.H)
		k := float32(0.94*smooth(0.36, 0.86, f) + 0.5*(1-smooth(0, 0.26, f)))
		for x := range c.W {
			o := 3 * (y*c.W + x)
			c.Pix[o] += (deep.R - c.Pix[o]) * k
			c.Pix[o+1] += (deep.G - c.Pix[o+1]) * k
			c.Pix[o+2] += (deep.B - c.Pix[o+2]) * k
		}
	}
	c.header(d.Day, 64, 92)

	const x0, x1 = 40, 1160
	title := d.City
	sub := d.Country
	if title == "" {
		title, sub = d.Country, ""
	}
	y := 470.0
	if sub == "" {
		y = 500
	}
	c.Text(Display, c.fit(Display, 112, 56, title, x1-x0), title, x0, y, Left, white, 1, nil)
	if sub != "" {
		c.Text(Body, c.fit(Body, 34, 20, sub, x1-x0), sub, x0, y+50, Left, paper, 1, nil)
	}
	// The numbers in a row along the bottom, each its value and what it
	// counts, smaller together until the row fits.
	if len(d.Stats) == 0 {
		return
	}
	const gap = 44
	size := 54.0
	width := func(size float64) float64 {
		w := float64(gap * (len(d.Stats) - 1))
		for _, s := range d.Stats {
			w += c.Measure(Display, size, s.Value) + 12 + c.Measure(BodyMedium, size*0.44, s.Label)
		}
		return w
	}
	for size > 30 && width(size) > x1-x0 {
		size--
	}
	x := float64(x0)
	for _, s := range d.Stats {
		x += c.Text(Display, size, s.Value, x, 590, Left, white, 1, nil) + 12
		x += c.Text(BodyMedium, size*0.44, s.Label, x, 590, Left, ink, 1, nil) + gap
	}
}

// picture copies img over the canvas from its top left corner.
func (c *Canvas) picture(img *image.RGBA) {
	if img == nil {
		return
	}
	b := img.Rect
	for y := 0; y < min(c.H, b.Dy()); y++ {
		for x := 0; x < min(c.W, b.Dx()); x++ {
			p := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			o := 3 * (y*c.W + x)
			c.Pix[o], c.Pix[o+1], c.Pix[o+2] = float32(img.Pix[p])/255, float32(img.Pix[p+1])/255, float32(img.Pix[p+2])/255
		}
	}
}
