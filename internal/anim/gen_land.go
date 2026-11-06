// SPDX-License-Identifier: Apache-2.0

//go:build ignore

// gen_land writes the land bits of the halftone globes: land4.bin for the
// logo's globe (the same dots as web/app/land.js) and land1.bin for the map of
// the results. The dots sit in rows along the parallels, step degrees apart,
// every other row shifted by half a dot (see dots in globe.go); each file holds
// one bit per dot, low bit first.
//
//	go run gen_land.go
package main

import (
	"log"
	"math"
	"os"

	"github.com/FreshLabDev/maply/internal/geo"
)

func main() {
	world, err := geo.World()
	if err != nil {
		log.Fatal(err)
	}
	for _, step := range []float64{4, 1} {
		var bits []byte
		n := 0
		for i, lat := 0, -90+step/2; lat < 90; i, lat = i+1, lat+step {
			count := max(1, int(math.Round(360*math.Cos(lat*math.Pi/180)/step)))
			for j := 0; j < count; j++ {
				lon := -180 + (float64(j)+0.5+float64(i%2)*0.5)*360/float64(count)
				if lon >= 180 {
					lon -= 360
				}
				if n%8 == 0 {
					bits = append(bits, 0)
				}
				if _, ok := world.Locate(geo.Point{Lat: lat, Lon: lon}); ok {
					bits[n/8] |= 1 << (n % 8)
				}
				n++
			}
		}
		name := map[float64]string{4: "land4.bin", 1: "land1.bin"}[step]
		if err := os.WriteFile(name, bits, 0o644); err != nil {
			log.Fatal(err)
		}
		log.Printf("%s: %d dots, %d bytes", name, n, len(bits))
	}
}
