// SPDX-License-Identifier: Apache-2.0

// maply-anim renders the animated posts with sample data, to look at them and
// to time the renderer.
//
//	go run ./cmd/maply-anim -pano pano.jpg -o out
//
// It writes results.mp4, morning.mp4, world.mp4 and intrigue.mp4 to the output
// folder, and with -still a PNG of one moment of each instead.
package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"image/jpeg"
	"image/png"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/FreshLabDev/maply/internal/anim"
)

func main() {
	pano := flag.String("pano", "", "the day's equirectangular panorama, JPEG (needed for the morning post)")
	out := flag.String("o", ".", "output folder")
	scenes := flag.String("scenes", "results,morning,world,intrigue", "which posts, comma-separated")
	still := flag.Float64("still", -1, "write a PNG of the frame at this second instead of the video")
	workers := flag.Int("workers", 0, "frames drawn at once; 0 is one per CPU")
	faces := flag.String("faces", "", "a folder of JPEG profile photos, given to the sample players in name order")
	flag.Parse()
	if *faces != "" {
		var err error
		if sampleFaces, err = loadFaces(*faces); err != nil {
			log.Fatal(err)
		}
	}

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

// sampleFaces are the photos -faces gives the sample players.
var sampleFaces []*anim.Face

func loadFaces(dir string) ([]*anim.Face, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []*anim.Face
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		img, err := jpeg.Decode(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, anim.NewFace(img))
	}
	return out, nil
}

// The sample is day #1, Zanzibar: ten made-up players in a group and 348
// made-up guesses in the world.
var zanzibar = anim.Place{Lat: -6.16, Lon: 39.19, Name: "Занзибар", Country: "Танзания"}

func scene(name, pano string) (anim.Scene, error) {
	switch name {
	case "world":
		guesses, top := worldSample(zanzibar, 348)
		return anim.NewWorld(1, zanzibar, guesses, top, "км", "ru"), nil
	case "stats", "stats-de":
		return statsSample(strings.TrimPrefix(strings.TrimPrefix(name, "stats"), "-") + map[bool]string{true: "ru"}[name == "stats"]), nil
	case "intrigue":
		return anim.NewIntrigue(1, 3, 40, "22 °C", 16+40.0/60, "20:00", "ru"), nil
	case "results":
		place := zanzibar
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
		for i := range players {
			if i < len(sampleFaces) {
				players[i].Face = sampleFaces[i]
			}
		}
		return anim.NewResults(1, place, players, "км", "м", "ru"), nil
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

// worldSample scatters n guesses the way people guess: most near the coast
// of East Africa, the rest wherever it might also look like this. The three
// best are named.
func worldSample(place anim.Place, n int) ([]anim.Guess, [3]*anim.Winner) {
	r := rand.New(rand.NewPCG(11, 0))
	gauss := func() float64 {
		u := 0.0
		for range 6 {
			u += r.Float64()
		}
		return (u - 3) / math.Sqrt(0.5)
	}
	blobs := [][4]float64{{-6, 39, 4, 0.3}, {2, 25, 12, 0.2}, {20, 78, 6, 0.12}, {5, 105, 8, 0.1}, {-10, -45, 10, 0.1}, {38, 15, 6, 0.08}, {-25, 135, 10, 0.05}, {15, -80, 8, 0.05}}
	type scored struct {
		g      anim.Guess
		points float64
		name   string
	}
	var all []scored
	add := func(name string, lat, lon, at, speed float64) {
		km := anim.Km(lat, lon, place.Lat, place.Lon)
		ladder := 0.0
		switch {
		case km < 25:
			ladder = 1500
		case km < 400:
			ladder = 800
		case km < 4000:
			ladder = 300
		}
		all = append(all, scored{anim.Guess{Lat: lat, Lon: lon, Km: km, At: at}, math.Round(3500*math.Exp(-km/1492.7)) + ladder + speed, name})
	}
	for range n - 3 {
		k, b := r.Float64(), blobs[0]
		for _, x := range blobs {
			if k < x[3] {
				b = x
				break
			}
			k -= x[3]
		}
		lat := min(70, max(-60, b[0]+gauss()*b[2]))
		add("", lat, b[1]+gauss()*b[2]/math.Cos(lat*math.Pi/180), r.Float64(), math.Round(r.Float64()*25)*10)
	}
	add("Lucía", -6.1, 39.27, 0.05, 250)
	add("Kenji", -6.3, 39.05, 0.2, 240)
	add("Аня", -6.55, 39.4, 0.1, 250)
	order := make([]int, len(all))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(all[b].points, all[a].points) })
	var top [3]*anim.Winner
	for m, i := range order[:3] {
		all[i].g.Medal = m + 1
		top[m] = &anim.Winner{Name: all[i].name, Km: all[i].g.Km, Points: all[i].points}
	}
	guesses := make([]anim.Guess, len(all))
	for i, s := range all {
		guesses[i] = s.g
	}
	return guesses, top
}

// statsSample is a made-up person after thirty days: pins wherever they
// guessed, the best day in gold, two days missed. The texts come in Russian or
// in German, the longest the labels get.
func statsSample(lang string) *anim.Profile {
	labels := map[string][9]string{
		"ru": {"статистика", "очков за 30 дней", "дней сыграно", "дней подряд", "средняя ошибка", "лучший день, #17", "место в мире вчера", "лучшее место", "последние 14 дней"},
		"de": {"Statistik", "Punkte in 30 Tagen", "Tage gespielt", "Tage in Folge", "durchschnittlicher Fehler", "bester Tag, #17", "Weltrang gestern", "bester Weltrang", "die letzten 14 Tage"},
	}[lang]
	unit := map[string]string{"ru": "км", "de": "km"}[lang]
	r := rand.New(rand.NewPCG(5, 0))
	s := anim.NewProfile(lang)
	s.Title, s.Name = labels[0], "_amti"
	s.Total = anim.Tile{Value: "84\u00a0310", Label: labels[1]}
	s.Tiles = [6]anim.Tile{
		{Value: "28", Label: labels[2]},
		{Value: "5", Label: labels[3], Flame: true},
		{Value: "1\u00a0240 " + unit, Label: labels[4]},
		{Value: "4\u00a0820", Label: labels[5], Gold: true},
		{Value: "3 / 214", Label: labels[6]},
		{Value: "1", Label: labels[7], Gold: true},
	}
	for range 28 {
		lat, lon := -40+r.Float64()*100, -120+r.Float64()*270
		s.Pins = append(s.Pins, anim.Guess{Lat: lat, Lon: lon, Km: r.Float64() * 4000})
	}
	s.Pins[6] = anim.Guess{Lat: 31, Lon: 48, Medal: 1}
	for i := range 14 {
		s.Days = append(s.Days, anim.Day{No: 17 + i, Played: i != 4 && i != 8, Best: i == 0})
	}
	s.DaysLabel = labels[8]
	return s
}
