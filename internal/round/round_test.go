// SPDX-License-Identifier: Apache-2.0
package round

import (
	"log/slog"
	"strings"
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

// deadline takes zone names: a chat's own choice, as a group admin sets it.
func deadline(t *testing.T, z Zones, d Date, names ...string) time.Time {
	t.Helper()
	var locs []*time.Location
	for _, n := range names {
		locs = append(locs, z.For(n, ""))
	}
	got, err := Deadline(d, locs)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

const (
	kyiv   = "Europe/Kyiv"
	moscow = "Europe/Moscow"
	minsk  = "Europe/Minsk"
)

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
		{"only Kyiv", []string{kyiv}, "2026-12-10T18:00:00Z"},
		{"only Moscow", []string{moscow}, "2026-12-10T17:00:00Z"},
		{"a Kyiv group and a Moscow group", []string{kyiv, moscow}, "2026-12-10T17:00:00Z"},
		{"Kyiv, Minsk and Moscow", []string{kyiv, minsk, moscow}, "2026-12-10T17:00:00Z"},
		{"Kyiv and Tokyo", []string{kyiv, "Asia/Tokyo"}, "2026-12-10T11:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deadline(t, z, winter, tc.langs...); !got.Equal(utc(tc.want)) {
				t.Fatalf("deadline = %s, want %s", got.UTC().Format(time.RFC3339), tc.want)
			}
		})
	}
	// 20:00 in Moscow is 19:00 in Kyiv: the Kyiv player's window closes an
	// hour before their own group's reveal.
	if got := deadline(t, z, winter, kyiv, moscow).In(z.For(kyiv, "")).Hour(); got != 19 {
		t.Fatalf("Kyiv player's deadline is %d:00 Kyiv time, want 19:00", got)
	}
}

func TestDaylightSavingSwitch(t *testing.T) {
	z := zones(t)
	before := NewDate(2026, time.October, 24) // Kyiv still UTC+3
	after := NewDate(2026, time.October, 26)  // Kyiv back to UTC+2
	if got := deadline(t, z, before, kyiv, moscow); !got.Equal(utc("2026-10-24T17:00:00Z")) {
		t.Fatalf("summer deadline = %s", got.UTC())
	}
	if got := deadline(t, z, before, kyiv); !got.Equal(utc("2026-10-24T17:00:00Z")) {
		t.Fatalf("summer Kyiv reveal = %s, want the same instant as Moscow", got.UTC())
	}
	if got := deadline(t, z, after, kyiv); !got.Equal(utc("2026-10-26T18:00:00Z")) {
		t.Fatalf("winter Kyiv reveal = %s", got.UTC())
	}
}

// Ukraine's summer-time rules have been changed by law before. A new rule is a
// new tzdata zone, and the window follows it without a code change. Here Kyiv
// stays on UTC+3 all year, as if summer time were abolished: in December its
// reveal lands on Moscow's.
func TestChangedRulesComeFromTheZone(t *testing.T) {
	z := zones(t).With(kyiv, time.FixedZone(kyiv, 3*60*60))
	winter := NewDate(2026, time.December, 10)
	if got := deadline(t, z, winter, kyiv); !got.Equal(utc("2026-12-10T17:00:00Z")) {
		t.Fatalf("Kyiv reveal under changed rules = %s", got.UTC())
	}
}

// A chat nobody has set up follows its language; Russian and anything
// unknown follow Kyiv.
func TestDefaultZoneFollowsTheLanguage(t *testing.T) {
	z := zones(t)
	for _, tc := range []struct{ chosen, lang, want string }{
		{"", "ru", kyiv},
		{"", "uk", kyiv},
		{"", "be", minsk},
		{"", "en", "Europe/London"},
		{"", "de", "Europe/Berlin"},
		{"", "ja", "Asia/Tokyo"},
		{"", "", DefaultZone},
		{"", "kk", DefaultZone},
		{moscow, "uk", moscow},
		// Only a listed zone counts as a choice.
		{"Pacific/Kiritimati", "ru", kyiv},
		{"UTC", "en", "Europe/London"},
	} {
		if got := Name(tc.chosen, tc.lang); got != tc.want {
			t.Errorf("Name(%q, %q) = %s, want %s", tc.chosen, tc.lang, got, tc.want)
		}
		if got := z.For(tc.chosen, tc.lang).String(); got != tc.want {
			t.Errorf("For(%q, %q) = %s, want %s", tc.chosen, tc.lang, got, tc.want)
		}
	}
}

func TestDateInFollowsTheZone(t *testing.T) {
	z := zones(t)
	// 21:30 UTC on 31 December: already 1 January in Moscow, not yet in Kyiv.
	at := utc("2026-12-31T21:30:00Z")
	if got := DateIn(at, z.For(moscow, "")).String(); got != "2027-01-01" {
		t.Fatalf("Moscow date = %s", got)
	}
	if got := DateIn(at, z.For(kyiv, "")).String(); got != "2026-12-31" {
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

func TestDateLogsAsItsDay(t *testing.T) {
	var b strings.Builder
	slog.New(slog.NewJSONHandler(&b, nil)).Info("x", "date", mustDate(t, "2026-10-28"))
	if !strings.Contains(b.String(), `"date":"2026-10-28"`) {
		t.Fatalf("log line %s", b.String())
	}
}

func mustDate(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
