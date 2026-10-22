// SPDX-License-Identifier: Apache-2.0
package geo

import (
	"encoding/json"
	"fmt"
)

// Continents as the place table stores them.
var Continents = []string{"africa", "antarctica", "asia", "europe", "north_america", "oceania", "south_america"}

type Country struct {
	ISO       string
	Continent string
	shape     Shape
}

// Countries finds the country under a point. It is built once at start from
// an embedded FeatureCollection and only read afterwards.
type Countries struct {
	list []Country
}

// ParseCountries reads a GeoJSON FeatureCollection whose features carry an
// "iso" (ISO 3166-1 alpha-2) and a "continent" property.
func ParseCountries(raw []byte) (*Countries, error) {
	var fc struct {
		Features []struct {
			Properties struct {
				ISO       string `json:"iso"`
				Continent string `json:"continent"`
			} `json:"properties"`
			Geometry json.RawMessage `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		return nil, fmt.Errorf("decode countries: %w", err)
	}
	known := map[string]bool{}
	for _, c := range Continents {
		known[c] = true
	}
	out := &Countries{}
	for i, f := range fc.Features {
		if len(f.Properties.ISO) != 2 || !known[f.Properties.Continent] {
			return nil, fmt.Errorf("feature %d: iso %q, continent %q", i, f.Properties.ISO, f.Properties.Continent)
		}
		shape, err := ParseGeometry(f.Geometry)
		if err != nil {
			return nil, fmt.Errorf("feature %d (%s): %w", i, f.Properties.ISO, err)
		}
		out.list = append(out.list, Country{ISO: f.Properties.ISO, Continent: f.Properties.Continent, shape: shape})
	}
	return out, nil
}

// Locate returns the country under p, or false over the sea. Countries do not
// overlap in Natural Earth admin-0, so the first match is the only one.
func (c *Countries) Locate(p Point) (Country, bool) {
	for _, country := range c.list {
		if country.shape.Contains(p) {
			return country, true
		}
	}
	return Country{}, false
}
