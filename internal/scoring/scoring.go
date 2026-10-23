// SPDX-License-Identifier: Apache-2.0

// Package scoring turns a pin into points: up to 3,500 for distance on the
// GeoGuessr world-map curve, plus a ladder of bonuses for the continent, the
// country and the city, plus a small bonus for answering early. It makes no
// outside calls.
package scoring

import (
	"math"
	"time"

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

	// The speed bonus: SpeedEarly for an answer when the morning post goes
	// out, falling to SpeedAtHint when the afternoon hints begin, then from
	// SpeedAfterHint to nothing at the deadline. It is a nudge, not a race:
	// at most 250 against 5,000 for the pin.
	SpeedEarly     = 250
	SpeedAtHint    = 100
	SpeedAfterHint = 50
	// The post goes out at 07:00, the hints from 16:00, the result at 20:00,
	// each in the chat's own time; measured back from the deadline, that is
	// 13 and 4 hours.
	postBefore = 13 * time.Hour
	hintBefore = 4 * time.Hour

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

// SpeedBonus is for how early the answer came, counted back from the
// person's deadline: the reveal of the earliest zone among their chats.
// Rounded to tens, so the table shows round numbers.
func SpeedBonus(answered, deadline time.Time) int {
	left := deadline.Sub(answered)
	var p float64
	switch {
	case left <= 0:
		return 0
	case left > hintBefore:
		f := math.Min(1, float64(left-hintBefore)/float64(postBefore-hintBefore))
		p = SpeedAtHint + (SpeedEarly-SpeedAtHint)*f
	default:
		p = SpeedAfterHint * float64(left) / float64(hintBefore)
	}
	return int(math.Round(p/10) * 10)
}

type Score struct {
	Km       float64
	Distance int
	Bonus    int
	Match    Match
	// Speed is the bonus for answering early. Judge leaves it at zero: it
	// depends on when the answer came, not on the pin.
	Speed int
}

func (s Score) Total() int {
	return s.Distance + s.Bonus + s.Speed
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
