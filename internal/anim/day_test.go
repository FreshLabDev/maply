// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"image"
	"image/color"
	"testing"
)

// A day's card is the street across the whole picture, darkened under the
// words: the sky shows the photo, the bottom rows are the field with the
// place and the numbers in white on it.
func TestDayCardShowsTheStreetUnderTheWords(t *testing.T) {
	photo := image.NewRGBA(image.Rect(0, 0, 1200, 630))
	for i := range photo.Pix {
		photo.Pix[i] = 0xff // a white street
	}
	d := NewDayCard(14, photo, "uk")
	d.City, d.Country = "Львів", "Україна"
	d.Stats = []Stat{{"1 204", "грали"}, {"612", "км, медіана"}, {"3,2", "км, найкращий"}, {"41%", "знайшли країну"}}
	img := Still(d, 0)
	if b := img.Bounds(); b.Dx() != 1200 || b.Dy() != 630 {
		t.Fatalf("size = %v", b)
	}
	lum := func(x, y int) int {
		c := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
		return int(c.Y)
	}
	// The middle of the sky, right of the logo, is the photo, lightly shaded.
	if l := lum(900, 200); l < 200 {
		t.Errorf("sky = %d, the photo is lost", l)
	}
	// The very bottom between the words is the field.
	if l := lum(1190, 625); l > 80 {
		t.Errorf("bottom = %d, the words stand on the photo", l)
	}
	// Something white is written along the stats' baseline.
	white := 0
	for x := 40; x < 600; x++ {
		for y := 560; y < 592; y++ {
			if lum(x, y) > 230 {
				white++
			}
		}
	}
	if white < 300 {
		t.Errorf("only %d white pixels in the numbers' row", white)
	}
}

// A card with the country only puts it where the city goes and still draws.
func TestDayCardWithoutACity(t *testing.T) {
	d := NewDayCard(3, nil, "en")
	d.Country = "Iceland"
	if img := Still(d, 0); img.Bounds().Dx() != 1200 {
		t.Fatal("no card")
	}
}
