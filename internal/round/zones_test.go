// SPDX-License-Identifier: Apache-2.0
package round

import (
	"testing"
	"time"
)

// A phone's zone becomes the choice with the same clock all year, or none.
func TestMatchKeepsTheClock(t *testing.T) {
	z, err := LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	for device, want := range map[string]string{
		"Europe/Berlin":      "Europe/Berlin",
		"Europe/Paris":       "Europe/Berlin",
		"Europe/Warsaw":      "Europe/Berlin",
		"Europe/Riga":        "Europe/Kyiv",
		"Europe/Dublin":      "Europe/London",
		"Asia/Yekaterinburg": "",
		"Africa/Lagos":       "", // UTC+1 all year, Berlin moves in summer
		"Not/AZone":          "",
		"":                   "",
	} {
		if got := z.Match(device, now); got != want {
			t.Errorf("Match(%q) = %q, want %q", device, got, want)
		}
	}
}

// A zone change must not reopen a day the chat has already seen.
func TestReopens(t *testing.T) {
	load := func(n string) *time.Location {
		l, err := time.LoadLocation(n)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	london, la, tokyo := load("Europe/London"), load("America/Los_Angeles"), load("Asia/Tokyo")
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		from, to *time.Location
		now      string
		want     bool
	}{
		{london, la, "2026-11-04T20:30:00Z", true},  // seen in London, LA answers until 04:00 UTC
		{london, la, "2026-11-05T00:30:00Z", true},  // London's 5th began, but LA is still on the 4th
		{london, la, "2026-11-04T19:00:00Z", false}, // London has not revealed yet
		{london, la, "2026-11-05T05:00:00Z", false}, // LA has closed the 4th too
		{la, london, "2026-11-04T21:00:00Z", false}, // moving east only closes earlier
		{london, tokyo, "2026-11-04T20:30:00Z", false},
	} {
		if got := Reopens(c.from, c.to, at(c.now)); got != c.want {
			t.Errorf("%s → %s at %s = %v, want %v", c.from, c.to, c.now, got, c.want)
		}
	}
}

// A zone change must not bring tomorrow's post before today's result.
func TestOutruns(t *testing.T) {
	load := func(n string) *time.Location {
		l, err := time.LoadLocation(n)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	london, la, tokyo := load("Europe/London"), load("America/Los_Angeles"), load("Asia/Tokyo")
	for _, c := range []struct {
		from, to *time.Location
		now      string
		want     bool
	}{
		{la, tokyo, "2026-11-05T20:00:00Z", true},      // noon in LA, already the 6th in Tokyo
		{la, tokyo, "2026-11-06T04:30:00Z", false},     // LA has revealed the 5th
		{london, tokyo, "2026-11-04T16:00:00Z", true},  // Tokyo is past midnight, London not at 20:00
		{london, tokyo, "2026-11-04T10:00:00Z", false}, // the same date in both
		{tokyo, la, "2026-11-05T01:00:00Z", false},     // moving west never runs ahead
	} {
		now, err := time.Parse(time.RFC3339, c.now)
		if err != nil {
			t.Fatal(err)
		}
		if got := Outruns(c.from, c.to, now); got != c.want {
			t.Errorf("%s → %s at %s = %v, want %v", c.from, c.to, c.now, got, c.want)
		}
	}
}

// The newest date runs ahead of UTC once midnight has passed in the east.
func TestNewestRunsAheadOfUTC(t *testing.T) {
	z, err := LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	evening := time.Date(2026, time.November, 9, 18, 0, 0, 0, time.UTC)
	if got, want := z.Newest(evening), NewDate(2026, 11, 10); got != want {
		t.Errorf("Newest at 18:00 UTC = %v, want %v", got, want)
	}
	morning := time.Date(2026, time.November, 9, 6, 0, 0, 0, time.UTC)
	if got, want := z.Newest(morning), NewDate(2026, 11, 9); got != want {
		t.Errorf("Newest at 06:00 UTC = %v, want %v", got, want)
	}
	if got, want := (Zones{}).Newest(evening), NewDate(2026, 11, 9); got != want {
		t.Errorf("Newest with no zones = %v, want the UTC date %v", got, want)
	}
}

// A browser's zone outside the span of the choices plays in the nearer
// edge: UTC+14 would reveal before every chat.
func TestWithinKeepsTheSpanOfTheChoices(t *testing.T) {
	z, err := LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.December, 10, 9, 0, 0, 0, time.UTC)
	for device, want := range map[string]string{
		"Europe/Minsk":       "Europe/Minsk",
		"Asia/Yekaterinburg": "Asia/Yekaterinburg",
		"America/Denver":     "America/Denver",
		"Etc/GMT-14":         "Australia/Sydney",
		"Pacific/Kiritimati": "Australia/Sydney",
		"Pacific/Auckland":   "Australia/Sydney",
		"Pacific/Honolulu":   "America/Los_Angeles",
		"Etc/GMT+12":         "America/Los_Angeles",
	} {
		loc, err := time.LoadLocation(device)
		if err != nil {
			t.Fatal(err)
		}
		if got := z.Within(loc, now).String(); got != want {
			t.Errorf("Within(%s) = %s, want %s", device, got, want)
		}
		d := DateIn(now, z.Within(loc, now))
		if r := d.Reveal(z.Within(loc, now)); r.Before(z.FirstReveal(d)) || r.After(z.LastReveal(d)) {
			t.Errorf("%s reveals %v, outside the chats' %v to %v", device, r, z.FirstReveal(d), z.LastReveal(d))
		}
	}
}
