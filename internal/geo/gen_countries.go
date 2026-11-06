// SPDX-License-Identifier: Apache-2.0

//go:build ignore

// gen_countries turns Natural Earth's 1:10m admin-0 countries into the small
// countries.geojson that maply embeds. It takes the Ukrainian point of view
// (ne_10m_admin_0_countries_ukr.geojson), so Crimea and Sevastopol are part of
// Ukraine; the default file draws them inside Russia.
//
// Every polygon gets its own continent: Natural Earth gives one per country,
// which would put Vladivostok in Europe and French Guiana there too. Russia is
// cut at 60°E, the rest goes by the overrides below. Features without an ISO
// code (sovereign base areas, Bir Tawil, the Spratlys) are dropped, so a pin
// there is in no country.
//
//	go run gen_countries.go -src ne_10m_admin_0_countries_ukr.geojson
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
)

type ring [][2]float64
type polygon []ring

// tolerance is the Douglas-Peucker distance in degrees: about 220 m of
// latitude. Border cities a river apart still land on the right side.
const tolerance = 0.002

// uralLon splits Russia into its European and Asian parts.
const uralLon = 60.0

var continentOf = map[string]string{
	"Africa": "africa", "Antarctica": "antarctica", "Asia": "asia", "Europe": "europe",
	"North America": "north_america", "Oceania": "oceania", "South America": "south_america",
}

// oceanContinent places the islands Natural Earth files under "Seven seas".
var oceanContinent = map[string]string{
	"TF": "africa", "SC": "africa", "HM": "africa", "SH": "africa", "MU": "africa", "IO": "africa",
	"MV": "asia", "GS": "south_america", "FR": "north_america", // FR here is Clipperton
}

// override returns the continent of one polygon of a country that spans more
// than one; lon and lat are the centre of its bounding box.
func override(iso, continent string, lon, lat float64) string {
	switch iso {
	case "FR":
		switch {
		case lon < -30 && lat < 8:
			return "south_america" // French Guiana
		case lon < -30:
			return "north_america" // Guadeloupe, Martinique, Saint-Martin
		case lon > 30:
			return "africa" // Réunion, Mayotte
		}
	case "NL":
		if lon < -30 {
			return "north_america" // Caribbean Netherlands
		}
	case "US":
		if lon < -140 && lat < 30 {
			return "oceania" // Hawaii
		}
	case "TR":
		if lat > 40.6 && lon < 29.2 {
			return "europe" // East Thrace and Istanbul's European side
		}
	}
	return continent
}

type feature struct {
	Properties map[string]any `json:"properties"`
	Geometry   struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
}

type outFeature struct {
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
	Geometry   outGeometry       `json:"geometry"`
}

type outGeometry struct {
	Type        string    `json:"type"`
	Coordinates []polygon `json:"coordinates"`
}

func main() {
	src := flag.String("src", "", "path to ne_10m_admin_0_countries_ukr.geojson")
	dst := flag.String("o", "countries.geojson", "output path")
	flag.Parse()
	if *src == "" {
		log.Fatal("-src is required")
	}
	raw, err := os.ReadFile(*src)
	if err != nil {
		log.Fatal(err)
	}
	var fc struct {
		Features []feature `json:"features"`
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		log.Fatal(err)
	}

	// byKey groups polygons by ISO and continent, one feature each.
	byKey := map[[2]string][]polygon{}
	for _, f := range fc.Features {
		iso, _ := f.Properties["ISO_A2_EH"].(string)
		name, _ := f.Properties["ADMIN"].(string)
		if len(iso) != 2 || strings.Trim(iso, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			log.Printf("drop %s (iso %q)", name, iso)
			continue
		}
		ne, _ := f.Properties["CONTINENT"].(string)
		continent, ok := continentOf[ne]
		if !ok {
			if continent, ok = oceanContinent[iso]; !ok {
				log.Fatalf("%s (%s): no continent for %q", name, iso, ne)
			}
		}
		var polys []polygon
		switch f.Geometry.Type {
		case "Polygon":
			var p polygon
			err = json.Unmarshal(f.Geometry.Coordinates, &p)
			polys = []polygon{p}
		case "MultiPolygon":
			err = json.Unmarshal(f.Geometry.Coordinates, &polys)
		default:
			err = fmt.Errorf("geometry %s", f.Geometry.Type)
		}
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		for _, p := range polys {
			if iso == "RU" {
				west, east := clip(p, true), clip(p, false)
				if west != nil {
					add(byKey, iso, "europe", west)
				}
				if east != nil {
					add(byKey, iso, "asia", east)
				}
				continue
			}
			lon, lat := centre(p[0])
			add(byKey, iso, override(iso, continent, lon, lat), p)
		}
	}

	keys := make([][2]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1]
	})
	out := struct {
		Type     string       `json:"type"`
		Features []outFeature `json:"features"`
	}{Type: "FeatureCollection"}
	points := 0
	for _, k := range keys {
		var polys []polygon
		for _, p := range byKey[k] {
			if s := simplifyPolygon(p); s != nil {
				polys = append(polys, s)
				for _, r := range s {
					points += len(r)
				}
			}
		}
		out.Features = append(out.Features, outFeature{
			Type:       "Feature",
			Properties: map[string]string{"iso": k[0], "continent": k[1]},
			Geometry:   outGeometry{Type: "MultiPolygon", Coordinates: polys},
		})
	}
	data, err := json.Marshal(out)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*dst, append(data, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%d features, %d points, %d bytes", len(out.Features), points, len(data))
}

func add(m map[[2]string][]polygon, iso, continent string, p polygon) {
	k := [2]string{iso, continent}
	m[k] = append(m[k], p)
}

func centre(r ring) (lon, lat float64) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, q := range r {
		minX, maxX = math.Min(minX, q[0]), math.Max(maxX, q[0])
		minY, maxY = math.Min(minY, q[1]), math.Max(maxY, q[1])
	}
	return (minX + maxX) / 2, (minY + maxY) / 2
}

// clip keeps the part of p west (or east) of uralLon, Sutherland-Hodgman
// style. A concave ring may come back with zero-width bridges along the cut;
// they cancel out in the even-odd test. Chukotka's polygons sit at negative
// longitudes and count as east.
func clip(p polygon, west bool) polygon {
	inside := func(q [2]float64) bool {
		east := q[0] >= uralLon || q[0] < 0
		return east != west
	}
	var out polygon
	for i, r := range p {
		var c ring
		for j := range r {
			cur, prev := r[j], r[(j+len(r)-1)%len(r)]
			// Chukotka never meets the cut, so negative longitudes need no
			// intersection.
			if inside(cur) != inside(prev) && cur[0] >= 0 && prev[0] >= 0 {
				t := (uralLon - prev[0]) / (cur[0] - prev[0])
				c = append(c, [2]float64{uralLon, prev[1] + t*(cur[1]-prev[1])})
			}
			if inside(cur) {
				c = append(c, cur)
			}
		}
		if len(c) < 3 {
			if i == 0 {
				return nil
			}
			continue
		}
		if c[0] != c[len(c)-1] {
			c = append(c, c[0])
		}
		out = append(out, c)
	}
	return out
}

// simplifyPolygon runs Douglas-Peucker on every ring and rounds to 4 decimals
// (about 11 m). An outer ring that collapses is simplified again with a finer
// tolerance until it is a polygon, so no island disappears; a hole that
// collapses is dropped.
func simplifyPolygon(p polygon) polygon {
	var out polygon
	for i, r := range p {
		s := round(simplifyRing(r, tolerance))
		for tol := tolerance / 4; len(s) < 4 && i == 0 && tol > 1e-6; tol /= 4 {
			s = round(simplifyRing(r, tol))
		}
		if len(s) < 4 {
			if i == 0 {
				return nil
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

func simplifyRing(r ring, tol float64) ring {
	if len(r) <= 4 {
		return r
	}
	// Split the closed ring at the vertex farthest from its start.
	far, best := 0, -1.0
	for i, q := range r {
		if d := math.Hypot(q[0]-r[0][0], q[1]-r[0][1]); d > best {
			far, best = i, d
		}
	}
	a := dp(r[:far+1], tol)
	b := dp(r[far:], tol)
	return append(a[:len(a)-1], b...)
}

func dp(line ring, tol float64) ring {
	if len(line) <= 2 {
		return line
	}
	first, last := line[0], line[len(line)-1]
	idx, dmax := 0, 0.0
	for i := 1; i < len(line)-1; i++ {
		if d := segDist(line[i], first, last); d > dmax {
			idx, dmax = i, d
		}
	}
	if dmax <= tol {
		return ring{first, last}
	}
	a := dp(line[:idx+1], tol)
	b := dp(line[idx:], tol)
	return append(a[:len(a)-1], b...)
}

func segDist(p, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	if dx == 0 && dy == 0 {
		return math.Hypot(p[0]-a[0], p[1]-a[1])
	}
	t := ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p[0]-a[0]-t*dx, p[1]-a[1]-t*dy)
}

func round(r ring) ring {
	var out ring
	for _, q := range r {
		q = [2]float64{math.Round(q[0]*1e4) / 1e4, math.Round(q[1]*1e4) / 1e4}
		if len(out) == 0 || out[len(out)-1] != q {
			out = append(out, q)
		}
	}
	return out
}
