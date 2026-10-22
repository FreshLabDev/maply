// SPDX-License-Identifier: Apache-2.0
package scoring

import (
	"math"
	"testing"

	"github.com/FreshLabDev/maply/internal/geo"
)

const world = `{"type":"FeatureCollection","features":[
 {"properties":{"iso":"AA","continent":"europe"},"geometry":{"type":"Polygon","coordinates":[[[0,40],[10,40],[10,50],[0,50],[0,40]]]}},
 {"properties":{"iso":"BB","continent":"europe"},"geometry":{"type":"Polygon","coordinates":[[[10,40],[20,40],[20,50],[10,50],[10,40]]]}},
 {"properties":{"iso":"CC","continent":"asia"},"geometry":{"type":"Polygon","coordinates":[[[20,40],[30,40],[30,50],[20,50],[20,40]]]}},
 {"properties":{"iso":"RU","continent":"europe"},"geometry":{"type":"MultiPolygon","coordinates":[
   [[[170,60],[180,60],[180,70],[170,70],[170,60]]],
   [[[-180,60],[-170,60],[-170,70],[-180,70],[-180,60]]]]}}
]}`

func countries(t *testing.T) *geo.Countries {
	t.Helper()
	c, err := geo.ParseCountries([]byte(world))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func near(got, want, tolerance float64) bool {
	return math.Abs(got-want) <= tolerance
}

func TestDistanceKm(t *testing.T) {
	halfMeridian := math.Pi * earthRadiusKm
	for _, tc := range []struct {
		name      string
		a, b      geo.Point
		want, tol float64
	}{
		{"Kyiv to Moscow", geo.Point{Lat: 50.4501, Lon: 30.5234}, geo.Point{Lat: 55.7558, Lon: 37.6173}, 756, 5},
		{"across the antimeridian", geo.Point{Lat: 0, Lon: 179.99}, geo.Point{Lat: 0, Lon: -179.99}, 2.22, 0.01},
		{"the north pole from any longitude", geo.Point{Lat: 90, Lon: 0}, geo.Point{Lat: 90, Lon: 135}, 0, 1e-9},
		{"pole to pole", geo.Point{Lat: 90, Lon: 0}, geo.Point{Lat: -90, Lon: 0}, halfMeridian, 1e-6},
		{"antipodes", geo.Point{Lat: 10, Lon: 20}, geo.Point{Lat: -10, Lon: -160}, halfMeridian, 1e-6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DistanceKm(tc.a, tc.b); !near(got, tc.want, tc.tol) {
				t.Fatalf("DistanceKm = %.4f, want %.4f ± %g", got, tc.want, tc.tol)
			}
		})
	}
}

func TestDistancePointsFollowsTheCurve(t *testing.T) {
	for _, tc := range []struct {
		km   float64
		want int
	}{
		{0, 3500},
		{-1, 3500},
		{500, 2504},
		{2000, 917},
		{20015, 0},
	} {
		if got := DistancePoints(tc.km); got != tc.want {
			t.Fatalf("DistancePoints(%g) = %d, want %d", tc.km, got, tc.want)
		}
	}
}

func TestBonusIsALadder(t *testing.T) {
	for _, tc := range []struct {
		m    Match
		want int
	}{
		{Match{}, 0},
		{Match{Continent: true}, 300},
		{Match{Continent: true, Country: true}, 800},
		{Match{Continent: true, Country: true, City: true}, 1500},
		// A rung cannot be skipped.
		{Match{Country: true, City: true}, 0},
		{Match{Continent: true, City: true}, 300},
	} {
		if got := Bonus(tc.m); got != tc.want {
			t.Fatalf("Bonus(%+v) = %d, want %d", tc.m, got, tc.want)
		}
	}
}

func TestJudgeAtABorder(t *testing.T) {
	c := countries(t)
	city, err := geo.ParseGeometry([]byte(`{"type":"Polygon","coordinates":[[[10.2,44.8],[10.8,44.8],[10.8,45.2],[10.2,45.2],[10.2,44.8]]]}`))
	if err != nil {
		t.Fatal(err)
	}
	place := Place{Point: geo.Point{Lat: 45, Lon: 10.5}, Country: "BB", Continent: "europe", City: city}

	wrongSide := Judge(geo.Point{Lat: 45, Lon: 9.999}, place, c)
	rightSide := Judge(geo.Point{Lat: 45, Lon: 10.001}, place, c)
	inCity := Judge(geo.Point{Lat: 45, Lon: 10.4}, place, c)
	otherContinent := Judge(geo.Point{Lat: 45, Lon: 20.001}, place, c)
	sea := Judge(geo.Point{Lat: 30, Lon: 10.5}, place, c)

	if wrongSide.Bonus != 300 || rightSide.Bonus != 800 || inCity.Bonus != 1500 || otherContinent.Bonus != 0 || sea.Bonus != 0 {
		t.Fatalf("bonuses: wrong side %d, right side %d, city %d, other continent %d, sea %d",
			wrongSide.Bonus, rightSide.Bonus, inCity.Bonus, otherContinent.Bonus, sea.Bonus)
	}
	// Two pins 160 m apart differ by a point or two of distance and by the
	// whole country bonus: that is what makes the right country visible.
	if d := rightSide.Distance - wrongSide.Distance; d < 0 || d > 2 {
		t.Fatalf("distance points differ by %d across the border", d)
	}
	if rightSide.Total()-wrongSide.Total() < CountryBonus {
		t.Fatalf("right side %d, wrong side %d", rightSide.Total(), wrongSide.Total())
	}
}

func TestJudgeAcrossTheAntimeridian(t *testing.T) {
	c := countries(t)
	place := Place{Point: geo.Point{Lat: 65, Lon: 179.9}, Country: "RU", Continent: "europe"}
	s := Judge(geo.Point{Lat: 65, Lon: -179.9}, place, c)
	// 0.2° of longitude at 65° north: about 9.4 km, the short way round.
	if s.Bonus != ContinentBonus+CountryBonus || !near(s.Km, 9.40, 0.05) || s.Distance != 3478 {
		t.Fatalf("score = %+v", s)
	}
}

func TestJudgeWithoutCityPolygon(t *testing.T) {
	c := countries(t)
	place := Place{Point: geo.Point{Lat: 45, Lon: 5}, Country: "AA", Continent: "europe"}
	s := Judge(place.Point, place, c)
	if s.Total() != MaxDistancePoints+ContinentBonus+CountryBonus || s.Match.City {
		t.Fatalf("score = %+v", s)
	}
}

func TestJudgeSameCountryOnAnotherContinent(t *testing.T) {
	c := countries(t)
	// The test world has Russia in Europe only; the place says Asia, as the
	// real data does east of the Urals.
	place := Place{Point: geo.Point{Lat: 65, Lon: 179.9}, Country: "RU", Continent: "asia"}
	if s := Judge(geo.Point{Lat: 65, Lon: 175}, place, c); s.Bonus != ContinentBonus+CountryBonus {
		t.Fatalf("score = %+v", s)
	}
	// Another country on the pin's continent still earns nothing.
	if s := Judge(geo.Point{Lat: 45, Lon: 5}, place, c); s.Bonus != 0 {
		t.Fatalf("score = %+v", s)
	}
}
