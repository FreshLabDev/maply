// SPDX-License-Identifier: Apache-2.0
package round

import (
	"fmt"
	"time"
	// The reveal and the answer window are computed in each chat's zone. The
	// image carries tzdata too, but a binary that depends on the host for this
	// would run every chat in UTC the day the package goes missing.
	_ "time/tzdata"
)

// DefaultZone is the zone of a chat that has neither its own choice nor a
// language with a zone of its own. Kyiv, as in quoto: the owner's decision of
// 22 October 2026, which also moved Russian there.
const DefaultZone = "Europe/Kyiv"

// ZoneChoices are the zones a chat may pick, west to east. Every language's
// default is one of them, so the picker always has the current zone marked.
var ZoneChoices = []string{
	"America/Los_Angeles",
	"America/Chicago",
	"America/New_York",
	"America/Sao_Paulo",
	"Europe/London",
	"Europe/Berlin",
	"Europe/Kyiv",
	"Europe/Minsk",
	"Europe/Moscow",
	"Europe/Istanbul",
	"Asia/Riyadh",
	"Asia/Dubai",
	"Asia/Kolkata",
	"Asia/Shanghai",
	"Asia/Tokyo",
	"Australia/Sydney",
}

// languageZones is where a chat's reveal falls before anyone picks a zone: the
// capital of its language. Russian follows Kyiv by the owner's decision. The
// languages of Central Europe share Europe/Berlin, whose rules are theirs:
// 20:00 in Madrid, Paris, Rome, Warsaw, Prague and Stockholm is the same
// instant all year.
var languageZones = map[string]string{
	"en": "Europe/London",
	"ru": "Europe/Kyiv",
	"uk": "Europe/Kyiv",
	"be": "Europe/Minsk",
	"es": "Europe/Berlin",
	"fr": "Europe/Berlin",
	"de": "Europe/Berlin",
	"it": "Europe/Berlin",
	"pl": "Europe/Berlin",
	"cs": "Europe/Berlin",
	"sv": "Europe/Berlin",
	"ca": "Europe/Berlin",
	"tr": "Europe/Istanbul",
	"ar": "Asia/Riyadh",
	"zh": "Asia/Shanghai",
	"ja": "Asia/Tokyo",
}

// Zones holds every zone a chat can be in, loaded once.
type Zones struct {
	locs map[string]*time.Location
}

// LoadZones resolves ZoneChoices from tzdata.
func LoadZones() (Zones, error) {
	z := Zones{locs: map[string]*time.Location{}}
	for _, name := range ZoneChoices {
		loc, err := time.LoadLocation(name)
		if err != nil {
			return Zones{}, fmt.Errorf("load zone %s: %w", name, err)
		}
		z.locs[name] = loc
	}
	if _, ok := z.locs[DefaultZone]; !ok {
		return Zones{}, fmt.Errorf("default zone %s is not a choice", DefaultZone)
	}
	for lang, name := range languageZones {
		if _, ok := z.locs[name]; !ok {
			return Zones{}, fmt.Errorf("zone %s of %s is not a choice", name, lang)
		}
	}
	return z, nil
}

// DefaultName is the zone a chat in lang gets until someone picks one.
func DefaultName(lang string) string {
	if name, ok := languageZones[lang]; ok {
		return name
	}
	return DefaultZone
}

// IsChoice reports whether name is one of ZoneChoices. A zone outside the
// list is never stored, so a forged callback cannot move a reveal anywhere.
func IsChoice(name string) bool {
	for _, c := range ZoneChoices {
		if c == name {
			return true
		}
	}
	return false
}

// Name is the zone a chat is in: its own choice when it made a valid one,
// else the default of its language.
func Name(chosen, lang string) string {
	if IsChoice(chosen) {
		return chosen
	}
	return DefaultName(lang)
}

// For is the location of a chat whose choice is chosen (may be empty) and
// whose language is lang (may be empty).
func (z Zones) For(chosen, lang string) *time.Location {
	return z.locs[Name(chosen, lang)]
}

// With returns a copy where name uses loc instead. Tests use it to stand in
// for a change of law.
func (z Zones) With(name string, loc *time.Location) Zones {
	out := Zones{locs: map[string]*time.Location{}}
	for k, v := range z.locs {
		out.locs[k] = v
	}
	out.locs[name] = loc
	return out
}

// LastReveal is when d's place is out in every zone maply offers. Until then
// someone may still be answering d, so the day's world ranking is not final.
func (z Zones) LastReveal(d Date) time.Time {
	var last time.Time
	for _, loc := range z.locs {
		if r := d.Reveal(loc); r.After(last) {
			last = r
		}
	}
	return last
}
