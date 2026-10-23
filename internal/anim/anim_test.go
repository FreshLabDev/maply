// SPDX-License-Identifier: Apache-2.0
package anim

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/go-text/typesetting/language"
)

func near(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestOrtho(t *testing.T) {
	for _, c := range []struct {
		lat, lon, lat0, lon0 float64
		x, y, z              float64
	}{
		{10, 20, 10, 20, 0, 0, 1},  // the centre faces the viewer
		{0, 90, 0, 0, 1, 0, 0},     // a quarter east is on the right edge
		{90, 0, 0, 0, 0, 1, 0},     // the pole is on top
		{0, 180, 0, 0, 0, 0, -1},   // the far side is behind
		{-90, 0, -90, 0, 0, 0, 1},  // from below the south pole
		{0, -30, 0, 0, -0.5, 0, 0}, // x is the sine of the longitude
	} {
		x, y, z := Ortho(c.lat*rad, c.lon*rad, c.lat0*rad, c.lon0*rad)
		if !near(x, c.x, 1e-9) || !near(y, c.y, 1e-9) || (c.z != 0 && !near(z, c.z, 1e-9)) {
			t.Errorf("Ortho(%v, %v from %v, %v) = %.3f, %.3f, %.3f, want %v, %v, %v", c.lat, c.lon, c.lat0, c.lon0, x, y, z, c.x, c.y, c.z)
		}
	}
}

func TestGreatCircle(t *testing.T) {
	if lat, lon := GreatCircle(0, 0, 0, 90, 0.5); !near(lat, 0, 1e-9) || !near(lon, 45, 1e-9) {
		t.Errorf("halfway along the equator = %v, %v", lat, lon)
	}
	if lat, lon := GreatCircle(0, 0, 0, 90, 0); lat != 0 || lon != 0 {
		t.Errorf("start = %v, %v", lat, lon)
	}
	// Zanzibar to Cairo: every step is on the shortest path, so the two
	// parts add up to the whole and grow evenly.
	a, b := [2]float64{-6.16, 39.19}, [2]float64{30.04, 31.24}
	whole := Km(a[0], a[1], b[0], b[1])
	for _, f := range []float64{0.1, 0.5, 0.9} {
		lat, lon := GreatCircle(a[0], a[1], b[0], b[1], f)
		if got := Km(a[0], a[1], lat, lon); !near(got, f*whole, 1e-6) {
			t.Errorf("at %v: %v km from the start, want %v", f, got, f*whole)
		}
		if got := Km(lat, lon, b[0], b[1]); !near(got, (1-f)*whole, 1e-6) {
			t.Errorf("at %v: %v km to the end, want %v", f, got, (1-f)*whole)
		}
	}
	// Across the date line the short way, not round the world.
	if _, lon := GreatCircle(0, 170, 0, -170, 0.5); !near(math.Abs(lon), 180, 1e-9) {
		t.Errorf("across the date line: lon %v", lon)
	}
}

func TestAtan2(t *testing.T) {
	worst := 0.0
	for y := -1.0; y <= 1; y += 0.01 {
		for x := -1.0; x <= 1; x += 0.0137 {
			worst = max(worst, math.Abs(atan2(y, x)-math.Atan2(y, x)))
		}
	}
	if worst > 1e-7 {
		t.Errorf("atan2 is off by %g", worst)
	}
}

// The camera of the morning post: a plain view looks where yaw and pitch
// say; the little planet has the nadir in the middle.
func TestPanoProjection(t *testing.T) {
	ray := func(v View, qx, qy float64) (u, w, lat float64) {
		return v.ray(qx, qy, math.Cos(v.Pitch), math.Sin(v.Pitch), math.Cos(v.Yaw), math.Sin(v.Yaw))
	}
	if u, w, lat := ray(View{D: 0, F: 300}, 0, 0); !near(u, 0.5, 1e-7) || !near(w, 0.5, 1e-7) || !near(lat, 0, 1e-5) {
		t.Errorf("ahead: u %v, v %v, lat %v", u, w, lat)
	}
	if u, _, _ := ray(View{D: 0, F: 300, Yaw: 90 * rad}, 0, 0); !near(u, 0.75, 1e-7) {
		t.Errorf("turned right: u %v", u)
	}
	if _, _, lat := ray(View{D: 0, F: 300, Pitch: 30 * rad}, 0, 0); !near(lat, 30, 1e-5) {
		t.Errorf("looking up: lat %v", lat)
	}
	planet := View{D: 1, F: 200, Pitch: -90 * rad}
	if _, _, lat := ray(planet, 0, 0); !near(lat, -90, 1e-4) {
		t.Errorf("little planet centre: lat %v", lat)
	}
	// Stereographic from the far pole: the horizon is a circle F from the
	// centre.
	if _, _, lat := ray(planet, 200, 0); !near(lat, 0, 1e-4) {
		t.Errorf("little planet horizon: lat %v", lat)
	}
	if x, y, ok := nadirSpot(planet); !ok || x != 0 || !near(y, 0, 1e-9) {
		t.Errorf("nadir of the planet at %v, %v", x, y)
	}
}

// synthPano is a made-up panorama: bands of colour by latitude and stripes
// by longitude, so a turn or a tilt shows.
func synthPano() *Pano {
	img := image.NewRGBA(image.Rect(0, 0, 256, 128))
	for y := range 128 {
		for x := range 256 {
			img.Set(x, y, color.RGBA{uint8(y * 2), uint8(x), uint8((x / 16 % 2) * 200), 255})
		}
	}
	return NewPano(img)
}

func sampleResults() *Results {
	place := Place{Lat: -6.16, Lon: 39.19, Name: "Занзибар", Country: "Танзания"}
	players := []Player{
		{Name: "Дима", Lat: 30.04, Lon: 31.24, Rank: 4},
		{Name: "Макс", Lat: -4.04, Lon: 39.67, Rank: 1, Streak: 6},
		{Name: "Саша", Lat: 19.08, Lon: 72.88, Rank: 5},
		{Name: "Аня", Lat: -15.12, Lon: 39.27, Rank: 2},
		{Name: "Оля", Lat: -18.88, Lon: 47.51, Rank: 3},
	}
	for i := range players {
		players[i].Km = Km(players[i].Lat, players[i].Lon, place.Lat, place.Lon)
		players[i].Points = 5000 * math.Exp(-players[i].Km/3000)
	}
	return NewResults(1, place, players, "км", "ru")
}

// The last frame of a loop leads into the first: the frame at the loop's
// length, frame N, is the first frame again.
func TestLoopSeam(t *testing.T) {
	for name, s := range map[string]Scene{
		"results": sampleResults(),
		"morning": NewMorning(1, synthPano(), "20:00", "ru"),
		"world": NewWorld(1, Place{Lat: -6.16, Lon: 39.19, Name: "Занзибар", Country: "Танзания"},
			[]Guess{{Lat: -6.1, Lon: 39.27, Km: 12, At: 0.1, Medal: 1}, {Lat: 2, Lon: 25, Km: 1900, At: 0.6}},
			[3]*Winner{{Name: "Lucía", Km: 12, Points: 5200}}, "км", "ru"),
		"intrigue":        NewIntrigue(1, 3, 40, "22 °C", 16.5, "20:00", "ru"),
		"intrigue by day": NewIntrigue(1, 13, 5, "", 17, "20:00", "ru"),
	} {
		t.Run(name, func(t *testing.T) {
			n := Frames(s)
			first, seam := Still(s, 0), Still(s, float64(n)/FPS)
			if d := maxDiff(first, seam); d > 1 {
				t.Errorf("frame %d differs from frame 0 by %d levels", n, d)
			}
			// It does move in between.
			if d := maxDiff(first, Still(s, float64(n/2)/FPS)); d < 100 {
				t.Errorf("the middle of the loop looks like its start (%d)", d)
			}
		})
	}
}

func maxDiff(a, b *image.RGBA) int {
	worst := 0
	for i := range a.Pix {
		worst = max(worst, int(a.Pix[i])-int(b.Pix[i]), int(b.Pix[i])-int(a.Pix[i]))
	}
	return worst
}

// Sofia Sans draws Bulgarian forms by default; the language picks the
// Russian ones.
func TestCyrillicFollowsLanguage(t *testing.T) {
	ru := Body.Layout("д", 40, language.NewLanguage("ru"))
	bg := Body.Layout("д", 40, language.NewLanguage("bg"))
	if slices.EqualFunc(ru.segs, bg.segs, func(a, b glyphSeg) bool { return a == b }) {
		t.Error("the Russian д is drawn like the Bulgarian one")
	}
}

func TestRngIsMulberry32(t *testing.T) {
	r := rng(7)
	// The design's JavaScript gives these for seed 7.
	for _, want := range []float64{0.011704753153026104, 0.06195825757458806, 0.97690763277933} {
		if got := r(); got != want {
			t.Errorf("rng = %v, want %v", got, want)
		}
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[float64]string{0: "0", 242.4: "242", 4613: "4\u00a0613", 1234567: "1\u00a0234\u00a0567", -1500: "-1\u00a0500"} {
		if got := Thousands(n); got != want {
			t.Errorf("Thousands(%v) = %q, want %q", n, got, want)
		}
	}
}

// Every glyph the posts need is in the fonts, the narrow space included.
func TestFontsHaveTheGlyphs(t *testing.T) {
	for _, f := range []*Font{Display, Body, BodyMedium} {
		for _, r := range "maply#0123456789:·\u00a0ЗанзибарТнияДМкСОЛЖИПЁёЇїЄєІіЎўҐґLucíaKenji" {
			if _, ok := f.face.NominalGlyph(r); !ok {
				t.Errorf("no glyph for %q", r)
			}
		}
	}
}

func BenchmarkResultsFrame(b *testing.B) {
	s, c := sampleResults(), NewCanvas(720, 900)
	for i := 0; b.Loop(); i++ {
		s.Draw(c, 2+float64(i%240)/FPS)
	}
}

func BenchmarkMorningFrame(b *testing.B) {
	s, c := NewMorning(1, synthPano(), "20:00", "ru"), NewCanvas(720, 900)
	for i := 0; b.Loop(); i++ {
		s.Draw(c, 3+float64(i%240)/FPS)
	}
}

// counter paints frame i in grey level i, so the raw stream shows the order.
type counter struct{}

func (counter) Size() (int, int)  { return 8, 4 }
func (counter) Duration() float64 { return 2 }
func (counter) Draw(c *Canvas, t float64) {
	v := float32(math.Round(t*FPS)) / 255
	c.Fill(RGB{v, v, v})
}

// Frames drawn at once are written in order, every one of them.
func TestFramesComeInOrder(t *testing.T) {
	var out bytes.Buffer
	st, err := frames(context.Background(), counter{}, 7, &out)
	if err != nil {
		t.Fatal(err)
	}
	const size = 3 * 8 * 4
	if st.Frames != 60 || out.Len() != 60*size {
		t.Fatalf("%d frames, %d bytes", st.Frames, out.Len())
	}
	for i := range 60 {
		if got := out.Bytes()[i*size]; int(got) != i {
			t.Fatalf("frame %d holds frame %d", i, got)
		}
	}
}

func TestNameKeepsWhatTheFacesWrite(t *testing.T) {
	for in, want := range map[string]string{
		"Аня":                        "Аня",
		"  Lucía  ":                  "Lucía",
		"李雷":                         "?",
		"Ann 🙂":                      "Ann",
		"":                           "?",
		"Christopher-Alexander Long": "Christopher-Ale…",
	} {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}
