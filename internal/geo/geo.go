// SPDX-License-Identifier: Apache-2.0

// Package geo answers which country, continent and city polygon a point is in.
// Polygons are GeoJSON geometries in WGS 84 degrees; nothing here calls out.
package geo

import (
	"encoding/json"
	"fmt"
	"math"
)

type Point struct {
	Lat float64
	Lon float64
}

func (p Point) Valid() bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lon >= -180 && p.Lon <= 180 &&
		!math.IsNaN(p.Lat) && !math.IsNaN(p.Lon)
}

// ring is a closed list of vertices. A ring that crosses the antimeridian is
// stored unwrapped, with longitudes moved into 0..360, so that the planar test
// sees one continuous shape instead of two halves a world apart.
type ring struct {
	pts       []Point
	unwrapped bool
	minLat    float64
	maxLat    float64
	minLon    float64
	maxLon    float64
}

type polygon struct {
	outer ring
	holes []ring
}

// Shape is a Polygon or a MultiPolygon.
type Shape struct {
	polys []polygon
}

// Contains reports whether p lies inside the shape. A point exactly on an edge
// may land on either side; at the precision of a pin on a map that does not
// matter.
func (s Shape) Contains(p Point) bool {
	for _, poly := range s.polys {
		if poly.outer.contains(p) && !anyContains(poly.holes, p) {
			return true
		}
	}
	return false
}

func (s Shape) Empty() bool {
	return len(s.polys) == 0
}

func anyContains(rings []ring, p Point) bool {
	for _, r := range rings {
		if r.contains(p) {
			return true
		}
	}
	return false
}

func (r ring) contains(p Point) bool {
	if r.containsPlanar(p) {
		return true
	}
	// An unwrapped ring lives in 0..360, so a point west of Greenwich is
	// tried once more a full turn to the east.
	if r.unwrapped && p.Lon < 0 {
		return r.containsPlanar(Point{Lat: p.Lat, Lon: p.Lon + 360})
	}
	return false
}

// containsPlanar is the even-odd ray test in the lon/lat plane. Natural Earth
// and OSM rings are dense enough that treating edges as straight in degrees
// is well inside the error of a pin.
func (r ring) containsPlanar(p Point) bool {
	if p.Lat < r.minLat || p.Lat > r.maxLat || p.Lon < r.minLon || p.Lon > r.maxLon {
		return false
	}
	inside := false
	n := len(r.pts)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		a, b := r.pts[i], r.pts[j]
		if (a.Lat > p.Lat) != (b.Lat > p.Lat) {
			x := a.Lon + (p.Lat-a.Lat)*(b.Lon-a.Lon)/(b.Lat-a.Lat)
			if p.Lon < x {
				inside = !inside
			}
		}
	}
	return inside
}

func newRing(coords [][]float64) (ring, error) {
	if len(coords) < 4 {
		return ring{}, fmt.Errorf("ring has %d positions, want at least 4", len(coords))
	}
	r := ring{pts: make([]Point, 0, len(coords))}
	for _, c := range coords {
		if len(c) < 2 {
			return ring{}, fmt.Errorf("position has %d values, want 2", len(c))
		}
		p := Point{Lon: c[0], Lat: c[1]}
		if !p.Valid() {
			return ring{}, fmt.Errorf("position %v is not a WGS 84 coordinate", c)
		}
		r.pts = append(r.pts, p)
	}
	r.bounds()
	if crossesAntimeridian(r.pts) {
		for i := range r.pts {
			if r.pts[i].Lon < 0 {
				r.pts[i].Lon += 360
			}
		}
		r.unwrapped = true
		r.bounds()
	}
	return r, nil
}

// crossesAntimeridian spots a ring that jumps from about +180 to about -180
// between two neighbouring vertices. A ring that reaches a pole is a polar
// cap instead: Natural Earth's Antarctica runs from -180 to 180 and closes
// along -90, and unwrapping it would tear it in two.
func crossesAntimeridian(pts []Point) bool {
	jump := false
	for i := 1; i < len(pts); i++ {
		if math.Abs(pts[i].Lat) == 90 {
			return false
		}
		if math.Abs(pts[i].Lon-pts[i-1].Lon) > 180 {
			jump = true
		}
	}
	return jump
}

func (r *ring) bounds() {
	r.minLat, r.maxLat = math.Inf(1), math.Inf(-1)
	r.minLon, r.maxLon = math.Inf(1), math.Inf(-1)
	for _, p := range r.pts {
		r.minLat = math.Min(r.minLat, p.Lat)
		r.maxLat = math.Max(r.maxLat, p.Lat)
		r.minLon = math.Min(r.minLon, p.Lon)
		r.maxLon = math.Max(r.maxLon, p.Lon)
	}
}

func newPolygon(rings [][][]float64) (polygon, error) {
	if len(rings) == 0 {
		return polygon{}, fmt.Errorf("polygon has no rings")
	}
	outer, err := newRing(rings[0])
	if err != nil {
		return polygon{}, err
	}
	poly := polygon{outer: outer}
	for _, h := range rings[1:] {
		hole, err := newRing(h)
		if err != nil {
			return polygon{}, err
		}
		poly.holes = append(poly.holes, hole)
	}
	return poly, nil
}

// ParseGeometry reads a GeoJSON Polygon or MultiPolygon geometry.
func ParseGeometry(raw []byte) (Shape, error) {
	var g struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return Shape{}, fmt.Errorf("decode geometry: %w", err)
	}
	var shape Shape
	switch g.Type {
	case "Polygon":
		var rings [][][]float64
		if err := json.Unmarshal(g.Coordinates, &rings); err != nil {
			return Shape{}, fmt.Errorf("decode polygon: %w", err)
		}
		poly, err := newPolygon(rings)
		if err != nil {
			return Shape{}, err
		}
		shape.polys = append(shape.polys, poly)
	case "MultiPolygon":
		var polys [][][][]float64
		if err := json.Unmarshal(g.Coordinates, &polys); err != nil {
			return Shape{}, fmt.Errorf("decode multipolygon: %w", err)
		}
		for _, rings := range polys {
			poly, err := newPolygon(rings)
			if err != nil {
				return Shape{}, err
			}
			shape.polys = append(shape.polys, poly)
		}
	default:
		return Shape{}, fmt.Errorf("geometry type %q is not a polygon", g.Type)
	}
	return shape, nil
}
