// SPDX-License-Identifier: Apache-2.0

// maply-anim renders the animated posts with sample data, to look at them and
// to time the renderer.
//
//	go run ./cmd/maply-anim -pano pano.jpg -o out
//
// It writes results.mp4 and morning.mp4 to the output folder, and with -still
// a PNG of one moment of each instead.
package main

import (
	"context"
	"flag"
	"fmt"
	"image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/FreshLabDev/maply/internal/anim"
)

func main() {
	pano := flag.String("pano", "", "the day's equirectangular panorama, JPEG (needed for the morning post)")
	out := flag.String("o", ".", "output folder")
	scenes := flag.String("scenes", "results,morning", "which posts, comma-separated")
	still := flag.Float64("still", -1, "write a PNG of the frame at this second instead of the video")
	workers := flag.Int("workers", 0, "frames drawn at once; 0 is one per CPU")
	flag.Parse()

	for _, name := range strings.Split(*scenes, ",") {
		s, err := scene(name, *pano)
		if err != nil {
			log.Fatal(err)
		}
		if *still >= 0 {
			path := filepath.Join(*out, fmt.Sprintf("%s-%05.2f.png", name, *still))
			f, err := os.Create(path)
			if err != nil {
				log.Fatal(err)
			}
			if err := png.Encode(f, anim.Still(s, *still)); err != nil {
				log.Fatal(err)
			}
			f.Close()
			log.Printf("%s", path)
			continue
		}
		path := filepath.Join(*out, name+".mp4")
		st, err := anim.Encode(context.Background(), s, path, anim.Options{Workers: *workers})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("%s: %d frames in %.2fs (drawing %.2fs of CPU, %.1f ms a frame)", path, st.Frames,
			st.Total.Seconds(), st.Draw.Seconds(), st.Draw.Seconds()*1000/float64(st.Frames))
	}
}

// The sample is day #1, Zanzibar, with ten made-up players.
func scene(name, pano string) (anim.Scene, error) {
	switch name {
	case "results":
		place := anim.Place{Lat: -6.16, Lon: 39.19, Name: "Занзибар", Country: "Танзания"}
		players := []anim.Player{
			{Name: "Дима", Lat: 30.04, Lon: 31.24},
			{Name: "Макс", Lat: -4.04, Lon: 39.67, Streak: 6},
			{Name: "Саша", Lat: 19.08, Lon: 72.88},
			{Name: "Аня", Lat: -15.12, Lon: 39.27},
			{Name: "Оля", Lat: -18.88, Lon: 47.51},
			{Name: "Ира", Lat: -1.29, Lon: 36.82},
			{Name: "Женя", Lat: 9.03, Lon: 38.74},
			{Name: "Lucía", Lat: -25.97, Lon: 32.57},
			{Name: "Kenji", Lat: 2.05, Lon: 45.32},
			{Name: "Петя", Lat: -13.96, Lon: 33.79},
		}
		rank(place, players)
		return anim.NewResults(1, place, players, "км", "ru"), nil
	case "morning":
		if pano == "" {
			return nil, fmt.Errorf("the morning post needs -pano")
		}
		f, err := os.Open(pano)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		img, err := jpeg.Decode(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pano, err)
		}
		return anim.NewMorning(1, anim.NewPano(img), "20:00", "ru"), nil
	}
	return nil, fmt.Errorf("no scene %q", name)
}

// rank scores the sample as the game does by distance: 5000 points at the
// place, fewer the further.
func rank(place anim.Place, players []anim.Player) {
	for i := range players {
		p := &players[i]
		p.Km = anim.Km(p.Lat, p.Lon, place.Lat, place.Lon)
		p.Points = math.Round(5000 * math.Exp(-p.Km/3000))
	}
	for i := range players {
		players[i].Rank = 1
		for j := range players {
			if players[j].Km < players[i].Km {
				players[i].Rank++
			}
		}
	}
}
