// SPDX-License-Identifier: Apache-2.0
package round

import (
	"testing"
	"time"
)

func zones(t *testing.T) Zones {
	t.Helper()
	z, err := LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func deadline(t *testing.T, z Zones, d Date, langs ...string) time.Time {
	t.Helper()
	var locs []*time.Location
	for _, l := range langs {
		loc, _ := z.For(l)
		locs = append(locs, loc)
	}
	got, err := Deadline(d, locs)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// In winter Kyiv is UTC+2 and Moscow UTC+3, so the Moscow reveal comes first.
// Ukraine leaves summer time on the last Sunday of October: 25 October 2026.
func TestWinterKyivAgainstMoscow(t *testing.T) {
	z := zones(t)
	winter := NewDate(2026, time.December, 10)
	for _, tc := range []struct {
		name  string
		langs []string
		want  string
	}{
		{"only Kyiv", []string{"uk"}, "2026-12-10T18:00:00Z"},
		{"only Moscow", []string{"ru"}, "2026-12-10T17:00:00Z"},
		{"a Kyiv group and a Moscow group", []string{"uk", "ru"}, "2026-12-10T17:00:00Z"},
		{"Kyiv, Minsk and Moscow", []string{"uk", "be", "ru"}, "2026-12-10T17:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deadline(t, z, winter, tc.langs...); !got.Equal(utc(tc.want)) {
				t.Fatalf("deadline = %s, want %s", got.UTC().Format(time.RFC3339), tc.want)
			}
		})
	}
	// 20:00 in Moscow is 19:00 in Kyiv: the Kyiv player's window closes an
	// hour before their own group's reveal.
	kyiv, _ := z.For("uk")
	if got := deadline(t, z, winter, "uk", "ru").In(kyiv).Hour(); got != 19 {
		t.Fatalf("Kyiv player's deadline is %d:00 Kyiv time, want 19:00", got)
	}
}

func TestDaylightSavingSwitch(t *testing.T) {
	z := zones(t)
	before := NewDate(2026, time.October, 24) // Kyiv still UTC+3
	after := NewDate(2026, time.October, 26)  // Kyiv back to UTC+2
	if got := deadline(t, z, before, "uk", "ru"); !got.Equal(utc("2026-10-24T17:00:00Z")) {
		t.Fatalf("summer deadline = %s", got.UTC())
	}
	if got := deadline(t, z, before, "uk"); !got.Equal(utc("2026-10-24T17:00:00Z")) {
		t.Fatalf("summer Kyiv reveal = %s, want the same instant as Moscow", got.UTC())
	}
	if got := deadline(t, z, after, "uk"); !got.Equal(utc("2026-10-26T18:00:00Z")) {
		t.Fatalf("winter Kyiv reveal = %s", got.UTC())
	}
}

// Ukraine's summer-time rules have been changed by law before. A new rule is a
// new tzdata zone, and the window follows it without a code change. Here Kyiv
// stays on UTC+3 all year, as if summer time were abolished: in December its
// reveal lands on Moscow's.
func TestChangedRulesComeFromTheZone(t *testing.T) {
	z := zones(t).With("uk", time.FixedZone("Europe/Kyiv", 3*60*60))
	winter := NewDate(2026, time.December, 10)
	if got := deadline(t, z, winter, "uk"); !got.Equal(utc("2026-12-10T17:00:00Z")) {
		t.Fatalf("Kyiv reveal under changed rules = %s", got.UTC())
	}
}

func TestUnknownLanguageFallsBackToTheEarliestReveal(t *testing.T) {
	z := zones(t)
	loc, lang := z.For("en")
	moscow, _ := z.For("ru")
	if lang != Fallback || loc != moscow {
		t.Fatalf("For(en) = %s, %s", loc, lang)
	}
	// Whatever the season, the fallback reveal is never later than Kyiv's.
	for d := NewDate(2026, time.January, 1); d.Before(NewDate(2027, time.January, 1)); d = d.AddDays(1) {
		if deadline(t, z, d, "en").After(deadline(t, z, d, "uk")) {
			t.Fatalf("%s: fallback reveal is after Kyiv's", d)
		}
	}
}

func TestDateInFollowsTheZone(t *testing.T) {
	z := zones(t)
	moscow, _ := z.For("ru")
	kyiv, _ := z.For("uk")
	// 21:30 UTC on 31 December: already 1 January in Moscow, not yet in Kyiv.
	at := utc("2026-12-31T21:30:00Z")
	if got := DateIn(at, moscow).String(); got != "2027-01-01" {
		t.Fatalf("Moscow date = %s", got)
	}
	if got := DateIn(at, kyiv).String(); got != "2026-12-31" {
		t.Fatalf("Kyiv date = %s", got)
	}
}

func TestDayNo(t *testing.T) {
	first := NewDate(2026, time.November, 1)
	if got := DayNo(first, first); got != 1 {
		t.Fatalf("first day = %d", got)
	}
	// 29 March 2027 is a summer-time switch; whole-day arithmetic is in UTC
	// and does not care.
	if got := DayNo(first, NewDate(2027, time.March, 29)); got != 149 {
		t.Fatalf("day = %d, want 149", got)
	}
	d, err := ParseDate("2026-11-15")
	if err != nil || DayNo(first, d) != 15 {
		t.Fatalf("ParseDate = %v, %v", d, err)
	}
}

func TestDeadlineNeedsAZone(t *testing.T) {
	if _, err := Deadline(NewDate(2026, time.November, 1), nil); err == nil {
		t.Fatal("Deadline without zones succeeded")
	}
}
