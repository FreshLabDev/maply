// SPDX-License-Identifier: Apache-2.0
package free

import (
	"time"

	"github.com/FreshLabDev/maply/internal/i18n"
)

// RoundView is a place being played: its picture and its time, nothing of
// where it is.
type RoundView struct {
	N        int        `json:"n"`
	Image    string     `json:"image"`
	Kind     string     `json:"kind"`
	Heading  *float64   `json:"heading"`
	StartsAt time.Time  `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
	// Repeat is a place one of the players has seen before.
	Repeat bool      `json:"repeat,omitempty"`
	Now    time.Time `json:"now"`
}

// PlaceView is where a place was, after it has been answered.
type PlaceView struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Country string  `json:"country"`
	City    string  `json:"city,omitempty"`
	Fact    string  `json:"fact,omitempty"`
	Author  string  `json:"author,omitempty"`
	License string  `json:"license"`
	Source  string  `json:"source"`
}

// GuessView is an answer with its points in parts: Distance and Bonus, which
// the average counts, and Speed on top.
type GuessView struct {
	N        int      `json:"n,omitempty"`
	M        int      `json:"m,omitempty"`
	Lat      *float64 `json:"lat"`
	Lon      *float64 `json:"lon"`
	Km       *float64 `json:"km"`
	Points   int      `json:"points"`
	Distance int      `json:"distance"`
	Bonus    int      `json:"bonus"`
	Speed    int      `json:"speed"`
	Country  bool     `json:"country"`
	Auto     bool     `json:"auto,omitempty"`
	TimeMs   int64    `json:"time_ms"`
}

func guessView(g Guess) GuessView {
	v := GuessView{N: g.N, M: g.M, Points: g.Points(), Distance: g.PtsKm, Bonus: g.PtsBonus, Speed: g.PtsSpeed,
		Country: g.CountryOK, Auto: g.Auto, TimeMs: g.TimeMs}
	if g.Pin != nil {
		lat, lon, km := g.Pin.Lat, g.Pin.Lon, g.Km
		v.Lat, v.Lon, v.Km = &lat, &lon, &km
	}
	return v
}

func placeView(p Place, lang string) PlaceView {
	return PlaceView{Lat: p.Point.Lat, Lon: p.Point.Lon, Country: p.Country,
		City: localized(p.City, lang), Fact: localized(p.Fact, lang),
		Author: p.Author, License: p.License, Source: p.Source}
}

// localized is a curated text in lang, else in English.
func localized(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	return m[i18n.DefaultLang]
}

func (s *Service) roundView(id int64, r Round, p Place, now time.Time) RoundView {
	return RoundView{N: r.N, Image: s.imagePath(id, r.N), Kind: p.Kind, Heading: p.Heading,
		StartsAt: r.StartsAt.UTC(), EndsAt: utc(r.EndsAt), Repeat: r.Repeat, Now: now.UTC()}
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
