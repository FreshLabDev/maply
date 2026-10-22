// SPDX-License-Identifier: Apache-2.0
package round

import (
	"fmt"
	"time"
)

// Zones maps an interface language to the zone of its capital.
type Zones struct {
	byLang   map[string]*time.Location
	fallback string
}

// capitals is the rule from docs/design.md: time follows the capital of the
// chat's or person's language.
var capitals = map[string]string{
	"ru": "Europe/Moscow",
	"be": "Europe/Minsk",
	"uk": "Europe/Kyiv",
}

// Fallback is the language whose zone a chat with no supported language
// gets. Moscow and Minsk reveal no later than Kyiv all year, so the fallback
// can never stretch anyone's window past a real reveal.
const Fallback = "ru"

// LoadZones resolves the capitals from tzdata.
func LoadZones() (Zones, error) {
	z := Zones{byLang: map[string]*time.Location{}, fallback: Fallback}
	for lang, name := range capitals {
		loc, err := time.LoadLocation(name)
		if err != nil {
			return Zones{}, fmt.Errorf("load zone %s for %s: %w", name, lang, err)
		}
		z.byLang[lang] = loc
	}
	return z, nil
}

// For returns the zone for lang and the language it actually used.
func (z Zones) For(lang string) (*time.Location, string) {
	if loc, ok := z.byLang[lang]; ok {
		return loc, lang
	}
	return z.byLang[z.fallback], z.fallback
}

// With returns a copy where lang uses loc instead. Tests use it to stand in
// for a change of law.
func (z Zones) With(lang string, loc *time.Location) Zones {
	out := Zones{byLang: map[string]*time.Location{}, fallback: z.fallback}
	for k, v := range z.byLang {
		out.byLang[k] = v
	}
	out.byLang[lang] = loc
	return out
}

// Supported reports whether lang is an interface language of the game.
func Supported(lang string) bool {
	_, ok := capitals[lang]
	return ok
}
