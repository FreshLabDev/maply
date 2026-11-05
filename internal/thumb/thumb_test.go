// SPDX-License-Identifier: Apache-2.0
package thumb

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

// pano is a 2:1 panorama painted by paint, given the longitude and latitude
// of each pixel in degrees: longitude 0 is the centre of the image, east to
// the right.
func pano(w int, paint func(lon, lat float64) color.RGBA) *image.RGBA {
	h := w / 2
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		lat := 90 - (float64(y)+0.5)/float64(h)*180
		for x := range w {
			lon := (float64(x)+0.5)/float64(w)*360 - 180
			img.SetRGBA(x, y, paint(lon, lat))
		}
	}
	return img
}

var (
	red   = color.RGBA{220, 30, 30, 255}
	green = color.RGBA{30, 200, 30, 255}
	blue  = color.RGBA{30, 30, 220, 255}
	grey  = color.RGBA{128, 128, 128, 255}
)

func near(c color.RGBA, want color.RGBA) bool {
	d := func(a, b uint8) float64 { return math.Abs(float64(a) - float64(b)) }
	return d(c.R, want.R)+d(c.G, want.G)+d(c.B, want.B) < 60
}

func TestFrameIsTheBandFacingYaw(t *testing.T) {
	// Red to the east, blue elsewhere; green above the band, grey below it.
	img := pano(1024, func(lon, lat float64) color.RGBA {
		switch {
		case lat > High+4:
			return green
		case lat < Low-4:
			return grey
		case lon > 80 && lon < 100:
			return red
		}
		return blue
	})
	for _, c := range []struct {
		yaw  float64
		want color.RGBA
	}{{0, blue}, {90, red}, {450, red}, {-270, red}, {180, blue}} {
		f := Frame(img, c.yaw, Small)
		if f.Rect.Dx() != Small.W || f.Rect.Dy() != Small.H {
			t.Fatalf("size %v", f.Rect)
		}
		// Down the middle the strip is all band: neither sky nor road.
		for y := 2; y < Small.H-2; y++ {
			if got := f.RGBAAt(Small.W/2, y); !near(got, c.want) {
				t.Fatalf("yaw %v, row %d: %v, want %v", c.yaw, y, got, c.want)
			}
		}
	}
}

func TestFrameCropsAPhoto(t *testing.T) {
	// A tall photo: the strip keeps its width and more of the bottom.
	img := image.NewRGBA(image.Rect(0, 0, 300, 400))
	for y := range 400 {
		for x := range 300 {
			c := blue
			if y < 80 {
				c = green
			}
			img.SetRGBA(x, y, c)
		}
	}
	f := Frame(img, 123, Wide)
	if f.Rect.Dx() != Wide.W || f.Rect.Dy() != Wide.H {
		t.Fatalf("size %v", f.Rect)
	}
	if got := f.RGBAAt(Wide.W/2, 2); !near(got, blue) {
		t.Fatalf("the crop took the top: %v", got)
	}
	if Pick(img) != 0 {
		t.Fatal("a photo has one direction")
	}
}

func TestPickLooksAlongTheStreet(t *testing.T) {
	// Flat walls all round but for a stretch around 210° east of the centre,
	// where there is detail under some sky.
	img := pano(1024, func(lon, lat float64) color.RGBA {
		d := math.Mod(lon-210+540, 360) - 180
		if math.Abs(d) > 30 {
			return grey
		}
		if lat > 12 {
			return color.RGBA{150, 190, 235, 255}
		}
		if (int(math.Floor(lon*2))+int(math.Floor(lat*2)))%2 == 0 {
			return color.RGBA{200, 120, 60, 255}
		}
		return color.RGBA{40, 60, 30, 255}
	})
	got := Pick(img)
	if math.Abs(got-210) > pickStep {
		t.Fatalf("Pick = %v, want about 210", got)
	}
	if math.Mod(got, pickStep) != 0 || got < 0 || got >= 360 {
		t.Fatalf("Pick = %v, not a step", got)
	}
}

func TestJPEGCarriesNoMetadata(t *testing.T) {
	data, err := JPEG(Frame(pano(512, func(float64, float64) color.RGBA { return grey }), 0, Small), Small)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{0xff, 0xd8}) || bytes.Contains(data, []byte("Exif")) || bytes.Contains(data, []byte{0xff, 0xe1}) {
		t.Fatalf("not a bare JPEG: % x", data[:16])
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func TestNewKey(t *testing.T) {
	a, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewKey()
	if !regexp.MustCompile(`^[A-Za-z0-9]{20}$`).MatchString(a) || a == b {
		t.Fatalf("keys %q, %q", a, b)
	}
}

type store struct {
	undrawn []db.Undrawn
	put     []db.Thumb
	notKept map[round.Date]bool
	asked   int
}

func (s *store) Undrawn(context.Context, int) ([]db.Undrawn, error) {
	s.asked++
	return s.undrawn, nil
}

func (s *store) PutThumb(_ context.Context, t db.Thumb) error {
	if s.notKept[t.Date] {
		return db.ErrNotKept
	}
	s.put = append(s.put, t)
	return nil
}

func TestDrawAll(t *testing.T) {
	dir := t.TempDir()
	img := pano(512, func(lon, _ float64) color.RGBA {
		if lon > 80 && lon < 100 {
			return red
		}
		return blue
	})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "panoKey"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	east := 90.0
	drawn, missing, gone := round.NewDate(2026, 11, 1), round.NewDate(2026, 11, 2), round.NewDate(2026, 11, 3)
	st := &store{
		undrawn: []db.Undrawn{
			{Date: drawn, ImageKey: "panoKey", Kind: "panorama", Yaw: &east},
			{Date: missing, ImageKey: "lostKey", Kind: "photo"},
			{Date: gone, ImageKey: "panoKey", Kind: "panorama"},
		},
		notKept: map[round.Date]bool{gone: true},
	}
	var logs strings.Builder
	s := &Service{Store: st, ImagesDir: dir, Log: slog.New(slog.NewTextHandler(&logs, nil))}
	n, err := s.DrawAll(context.Background())
	if err != nil || n != 1 || len(st.put) != 1 {
		t.Fatalf("drew %d, put %d, %v", n, len(st.put), err)
	}
	th := st.put[0]
	if th.Date != drawn || th.Recipe != Recipe || len(th.Key) != 20 || len(th.Small) == 0 || len(th.Wide) <= len(th.Small) {
		t.Fatalf("thumb = %+v", th)
	}
	small, err := jpeg.Decode(bytes.NewReader(th.Small))
	if err != nil {
		t.Fatal(err)
	}
	if b := small.Bounds(); b.Dx() != Small.W || b.Dy() != Small.H {
		t.Fatalf("small is %v", b)
	}
	// The curation's direction, east, is where the red is.
	if r, g, b, _ := small.At(Small.W/2, Small.H/2).RGBA(); r>>8 < 150 || g>>8 > 90 || b>>8 > 90 {
		t.Fatalf("the strip does not face the given yaw: %d %d %d", r>>8, g>>8, b>>8)
	}
	// The missing file is logged once, without the images directory.
	if strings.Count(logs.String(), "day strip not drawn") != 1 || strings.Contains(logs.String(), dir) {
		t.Fatalf("logs = %s", logs.String())
	}
	st.undrawn = st.undrawn[1:]
	if n, err := s.DrawAll(context.Background()); err != nil || n != 0 {
		t.Fatalf("second pass drew %d, %v", n, err)
	}
	if strings.Count(logs.String(), "day strip not drawn") != 1 {
		t.Fatalf("logged again: %s", logs.String())
	}
}

func TestDrawNeedsTheImagesDirectory(t *testing.T) {
	s := &Service{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := s.Draw(db.Undrawn{ImageKey: "k"}); err == nil {
		t.Fatal("drew without a directory")
	}
	s.ImagesDir = t.TempDir()
	if _, err := s.Draw(db.Undrawn{ImageKey: "../../etc/passwd"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a path out of the directory: %v", err)
	}
}
