// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/image/draw"
)

// The first places were cut from Mapillary's 2048 px thumbnails, which look
// soft on a phone: a 95° view of a 2048 px panorama is some 540 px across a
// screen of 1,170. sharpen fetches the originals again and keeps panoramas
// at panoWidth, photos at photoSide on the long edge, under the same keys.
const (
	// Pannellum needs half the width within the device's largest texture;
	// 2,048 is the least any phone has.
	panoWidth = 4096
	photoSide = 3072
)

// sharpen reads "id,kind,source_id,image_key" rows from csvPath (the header
// is skipped) and writes each place's sharper image to dir under its key.
// A place whose original is no larger than what it has is left out.
func (p *preparer) sharpen(csvPath, dir string) error {
	f, err := os.Open(csvPath)
	if err != nil {
		return err
	}
	rows, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	work := make(chan []string)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for r := range work {
				if err := p.sharpenOne(r[1] == "panorama", r[2], filepath.Join(dir, r[3])); err != nil {
					log.Printf("place %s: %v", r[0], err)
				}
			}
		})
	}
	for _, r := range rows[1:] {
		if len(r) == 4 {
			work <- r
		}
	}
	close(work)
	wg.Wait()
	return nil
}

func (p *preparer) sharpenOne(pano bool, sourceID, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	q := url.Values{"fields": {"thumb_original_url,width,height"}, "access_token": {p.token}}
	raw, err := p.get("https://graph.mapillary.com/"+url.PathEscape(sourceID)+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("mapillary: %w", err)
	}
	var meta struct {
		URL           string `json:"thumb_original_url"`
		Width, Height int
	}
	if err := json.Unmarshal(raw, &meta); err != nil || meta.URL == "" {
		return fmt.Errorf("mapillary answer has no original")
	}
	raw, err = p.get(meta.URL, nil)
	if err != nil {
		return fmt.Errorf("original: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	b := img.Bounds()
	var w, h int
	if pano {
		if b.Dx() != 2*b.Dy() {
			return fmt.Errorf("panorama is %dx%d, not 2:1", b.Dx(), b.Dy())
		}
		w = min(panoWidth, b.Dx())
		h = w / 2
	} else {
		k := min(1, float64(photoSide)/float64(max(b.Dx(), b.Dy())))
		w, h = int(float64(b.Dx())*k+0.5), int(float64(b.Dy())*k+0.5)
	}
	if w <= 2048 && max(w, h) <= 2048 {
		return fmt.Errorf("the original is %dx%d, no larger than what the place has", b.Dx(), b.Dy())
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	var res image.Image = out
	if pano {
		res = maskNadir(out)
	}
	var buf bytes.Buffer
	// Encoding from pixels writes no EXIF, GPS or maker notes.
	if err := jpeg.Encode(&buf, res, &jpeg.Options{Quality: 86}); err != nil {
		return err
	}
	return os.WriteFile(dst, buf.Bytes(), 0o644)
}
