// SPDX-License-Identifier: Apache-2.0

// Package round knows the calendar of the game: which date a chat is playing,
// when that date is revealed, and when a person's answer window closes.
package round

import (
	"fmt"
	"time"
)

// RevealHour is fixed for every chat. Groups may move the morning post, never
// the reveal: the answer window depends on it.
const RevealHour = 20

// Date is a calendar date, independent of any zone.
type Date struct {
	t time.Time // midnight UTC
}

func NewDate(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// DateIn is the calendar date at instant t in loc.
func DateIn(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return NewDate(y, m, d)
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return Date{t}, nil
}

func (d Date) String() string        { return d.t.Format(time.DateOnly) }
func (d Date) IsZero() bool          { return d.t.IsZero() }
func (d Date) Weekday() time.Weekday { return d.t.Weekday() }
func (d Date) AddDays(n int) Date    { return Date{d.t.AddDate(0, 0, n)} }
func (d Date) Before(o Date) bool    { return d.t.Before(o.t) }

// Time is midnight UTC of the date, for storing in a DATE column.
func (d Date) Time() time.Time { return d.t }

// DaysSince counts whole days from o to d.
func (d Date) DaysSince(o Date) int {
	return int(d.t.Sub(o.t).Hours() / 24)
}

// At is the instant of hour:min on this date in loc. Zone rules come from
// tzdata, so a change of law is a tzdata update, not a code change.
func (d Date) At(hour, min int, loc *time.Location) time.Time {
	y, m, day := d.t.Date()
	return time.Date(y, m, day, hour, min, 0, 0, loc)
}

// Reveal is when the date's answer is shown in loc.
func (d Date) Reveal(loc *time.Location) time.Time {
	return d.At(RevealHour, 0, loc)
}

// DayNo numbers days from the first round: the first date is maply #1.
func DayNo(first, d Date) int {
	return d.DaysSince(first) + 1
}

// TargetDifficulty is the difficulty a weekday aims for: Monday easy, Friday
// hard, the weekend gentle.
func TargetDifficulty(w time.Weekday) int {
	switch w {
	case time.Monday:
		return 1
	case time.Tuesday:
		return 2
	case time.Wednesday:
		return 3
	case time.Thursday:
		return 4
	case time.Friday:
		return 5
	default:
		return 2
	}
}

// Deadline is when a person's answer window for d closes: the earliest reveal
// among every zone they play in. A person in a Kyiv group who is also in a
// Moscow one must answer before the Moscow reveal, or the Moscow group would
// hand them the answer.
func Deadline(d Date, zones []*time.Location) (time.Time, error) {
	if len(zones) == 0 {
		return time.Time{}, fmt.Errorf("deadline for %s: no zones", d)
	}
	deadline := d.Reveal(zones[0])
	for _, loc := range zones[1:] {
		if r := d.Reveal(loc); r.Before(deadline) {
			deadline = r
		}
	}
	return deadline, nil
}
