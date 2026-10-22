// SPDX-License-Identifier: Apache-2.0
package geo

import "testing"

// world is a small stand-in for Natural Earth with the shapes that break naive
// point-in-polygon code: a shared border, an enclave, a country cut at the
// antimeridian, and a polar cap that closes along -90.
const world = `{"type":"FeatureCollection","features":[
 {"properties":{"iso":"AA","continent":"europe"},"geometry":{"type":"Polygon","coordinates":[[[0,40],[10,40],[10,50],[0,50],[0,40]]]}},
 {"properties":{"iso":"BB","continent":"europe"},"geometry":{"type":"Polygon","coordinates":[[[10,40],[20,40],[20,50],[10,50],[10,40]]]}},
 {"properties":{"iso":"ZA","continent":"africa"},"geometry":{"type":"Polygon","coordinates":[
   [[20,-30],[30,-30],[30,-20],[20,-20],[20,-30]],
   [[24,-26],[26,-26],[26,-24],[24,-24],[24,-26]]]}},
 {"properties":{"iso":"LS","continent":"africa"},"geometry":{"type":"Polygon","coordinates":[[[24,-26],[26,-26],[26,-24],[24,-24],[24,-26]]]}},
 {"properties":{"iso":"RU","continent":"europe"},"geometry":{"type":"MultiPolygon","coordinates":[
   [[[170,60],[180,60],[180,70],[170,70],[170,60]]],
   [[[-180,60],[-170,60],[-170,70],[-180,70],[-180,60]]]]}},
 {"properties":{"iso":"AQ","continent":"antarctica"},"geometry":{"type":"Polygon","coordinates":[[
   [-180,-90],[-180,-80],[-90,-80],[0,-80],[90,-80],[180,-80],[180,-90],[-180,-90]]]}}
]}`

func countries(t *testing.T) *Countries {
	t.Helper()
	c, err := ParseCountries([]byte(world))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLocate(t *testing.T) {
	c := countries(t)
	for _, tc := range []struct {
		name string
		p    Point
		want string
	}{
		{"west of a shared border", Point{45, 9.999}, "AA"},
		{"east of a shared border", Point{45, 10.001}, "BB"},
		{"the enclave, not the country around it", Point{-25, 25}, "LS"},
		{"the country around the enclave", Point{-22, 21}, "ZA"},
		{"just west of the antimeridian", Point{65, 179.999}, "RU"},
		{"just east of the antimeridian", Point{65, -179.999}, "RU"},
		{"near the south pole", Point{-89.999, 123}, "AQ"},
		{"the polar cap at the date line", Point{-85, -179.5}, "AQ"},
		{"the north pole is sea", Point{90, 0}, ""},
		{"open sea", Point{0, -30}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := c.Locate(tc.p)
			if got.ISO != tc.want || ok != (tc.want != "") {
				t.Fatalf("Locate(%v) = %q, %v; want %q", tc.p, got.ISO, ok, tc.want)
			}
		})
	}
}

// A city that straddles the antimeridian comes as one ring whose longitudes
// jump from +180 to -180, unlike Natural Earth, which cuts it in two.
func TestRingAcrossTheAntimeridian(t *testing.T) {
	s, err := ParseGeometry([]byte(`{"type":"Polygon","coordinates":[[[179.5,-17],[-179.5,-17],[-179.5,-16],[179.5,-16],[179.5,-17]]]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Point{{-16.5, 179.9}, {-16.5, -179.9}, {-16.5, 180}, {-16.5, -180}} {
		if !s.Contains(p) {
			t.Fatalf("%v is outside a ring across the antimeridian", p)
		}
	}
	for _, p := range []Point{{-16.5, 0}, {-16.5, 179}, {-16.5, -179}} {
		if s.Contains(p) {
			t.Fatalf("%v is inside a ring that spans one degree of longitude", p)
		}
	}
}

func TestParseGeometryRejects(t *testing.T) {
	for _, raw := range []string{
		`{"type":"Point","coordinates":[1,2]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,0],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[200,0],[1,1],[0,0]]]}`,
		`{"type":"Polygon","coordinates":[]}`,
		`not json`,
	} {
		if _, err := ParseGeometry([]byte(raw)); err == nil {
			t.Fatalf("ParseGeometry(%s) accepted it", raw)
		}
	}
}

func TestParseCountriesRejectsUnknownContinent(t *testing.T) {
	raw := `{"features":[{"properties":{"iso":"AA","continent":"atlantis"},"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}]}`
	if _, err := ParseCountries([]byte(raw)); err == nil {
		t.Fatal("ParseCountries accepted continent atlantis")
	}
}
