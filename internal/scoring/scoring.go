// SPDX-License-Identifier: Apache-2.0

// Package scoring turns a pin into points: up to 3,500 for distance on the
// GeoGuessr world-map curve, plus a ladder of bonuses for the continent, the
// country and the city. It makes no outside calls.
package scoring

import (
	"math"

	"github.com/FreshLabDev/maply/internal/geo"
)

const (
	MaxDistancePoints = 3500
	// curveKm is the distance at which the distance score falls to 1/e of
	// its maximum: about 2,500 at 500 km and about 900 at 2,000 km.
	curveKm = 1492.7

	ContinentBonus = 300
	CountryBonus   = 500
	CityBonus      = 700

	// earthRadiusKm is the IUGG mean radius.
	earthRadiusKm = 6371.0088
)

// DistanceKm is the great-circle distance by the haversine formula, which
// stays exact near the antimeridian and at the poles.
func DistanceKm(a, b geo.Point) float64 {
	lat1, lat2 := radians(a.Lat), radians(b.Lat)
	dLat := lat2 - lat1
	dLon := radians(b.Lon - a.Lon)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(h)))
}

func DistancePoints(km float64) int {
	if km <= 0 {
		return MaxDistancePoints
	}
	return int(math.Round(MaxDistancePoints * math.Exp(-km/curveKm)))
}

// Match is what the pin got right about the place.
type Match struct {
	Continent bool
	Country   bool
	City      bool
}

// Bonus climbs the ladder: the country counts only together with the
// continent, the city only together with the country.
func Bonus(m Match) int {
	if !m.Continent {
		return 0
	}
	if !m.Country {
		return ContinentBonus
	}
	if !m.City {
		return ContinentBonus + CountryBonus
	}
	return ContinentBonus + CountryBonus + CityBonus
}

// Place is what scoring needs to know about the answer.
type Place struct {
	Point     geo.Point
	Country   string
	Continent string
	// City may be empty; then no city bonus is possible.
	City geo.Shape
}

type Score struct {
	Km       float64
	Distance int
	Bonus    int
	Match    Match
}

func (s Score) Total() int {
	return s.Distance + s.Bonus
}

// Judge scores a pin against the place. countries locates the pin; a pin over
// the sea is in no country and on no continent. The right country counts as
// the right continent too: Paris for a place in French Guiana, or Moscow for
// one in Vladivostok, earns the country bonus.
func Judge(pin geo.Point, place Place, countries *geo.Countries) Score {
	km := DistanceKm(pin, place.Point)
	var m Match
	if c, ok := countries.Locate(pin); ok {
		m.Country = c.ISO == place.Country
		m.Continent = m.Country || c.Continent == place.Continent
	}
	m.City = m.Country && !place.City.Empty() && place.City.Contains(pin)
	return Score{Km: km, Distance: DistancePoints(km), Bonus: Bonus(m), Match: m}
}

func radians(deg float64) float64 {
	return deg * math.Pi / 180
}
