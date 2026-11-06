// SPDX-License-Identifier: Apache-2.0

// Package rating ranks the people of the daily game over the days that are
// over everywhere: the page's main table, its four cards and the leagues.
// Free play is not in it: its games are unlimited, and the boards of
// internal/api/top.go rank them on their own.
//
// The owner's call of 6 November. Over the last 30 days the score is the sum
// of a person's 20 best, so a missed day costs nothing while twenty remain
// and a lucky one cannot carry a month alone; over 7 days it is the sum of
// the days played, over all time the sum of every day. A league is the
// average day over the period, from five days played: below that a person
// is new.
package rating

import (
	"cmp"
	"slices"

	"github.com/FreshLabDev/maply/internal/round"
)

// Periods, as the page names them.
const (
	Week  = "7"
	Month = "30"
	All   = "all"
)

// Best is how many of the month's days count.
const Best = 20

// Window is how many days over everywhere a period looks back on, 0 for all.
func Window(period string) int {
	switch period {
	case Week:
		return 7
	case Month:
		return 30
	}
	return 0
}

// Known says whether period is one the page offers.
func Known(period string) bool { return period == Week || period == Month || period == All }

// Leagues, from the top. A person needs MinDays days in the period to be in
// one of them; the average day decides which.
const (
	Cartographer = "cartographer"
	Navigator    = "navigator"
	Traveler     = "traveler"
	Tourist      = "tourist"
	Novice       = "novice"
)

// MinDays is how many days a league and the accuracy card ask for.
const MinDays = 5

// League is one step of the ladder: From is the lowest average day in it.
type League struct {
	ID   string `json:"id"`
	From int    `json:"from"`
}

// Ladder is every league with an average, from the top; Novice is below it.
var Ladder = []League{
	{Cartographer, 4200},
	{Navigator, 3200},
	{Traveler, 2000},
	{Tourist, 0},
}

// LeagueOf is the league of a person with days days and that average.
func LeagueOf(avg, days int) string {
	if days < MinDays {
		return Novice
	}
	for _, l := range Ladder {
		if avg >= l.From {
			return l.ID
		}
	}
	return Tourist
}

// Next is the league above and the average it starts at; "" at the top and
// for a new person, whose next step is days rather than points.
func Next(league string) (string, int) {
	for i, l := range Ladder {
		if l.ID == league && i > 0 {
			return Ladder[i-1].ID, Ladder[i-1].From
		}
	}
	return "", 0
}

// KnownLeague says whether id names a league.
func KnownLeague(id string) bool {
	return id == Novice || slices.ContainsFunc(Ladder, func(l League) bool { return l.ID == id })
}

// Day is one person's scored day. Key names the person: a Telegram player or
// an account without Telegram, as the caller writes it.
type Day struct {
	Key    string
	Date   round.Date
	Points int
	Km     float64
}

// Line is one person over the period.
type Line struct {
	Key   string
	Score int
	Days  int
	// Avg is the mean day, rounded.
	Avg int
	// Km is the median miss.
	Km float64
	// Best is the best day and BestDate its date.
	Best     int
	BestDate round.Date
	// Streak is the longest run of days played in a row, over the days the
	// period has: a skipped day is not one of them, so it breaks nothing.
	Streak int
	League string
}

// Lines sums each person's days. dates are the period's days over
// everywhere; a day on another date is left out.
func Lines(period string, dates []round.Date, days []Day) []Line {
	at := make(map[round.Date]int, len(dates))
	sorted := slices.Clone(dates)
	slices.SortFunc(sorted, func(a, b round.Date) int { return a.Time().Compare(b.Time()) })
	for i, d := range sorted {
		at[d] = i
	}
	by := map[string][]Day{}
	var keys []string
	for _, d := range days {
		if _, ok := at[d.Date]; !ok {
			continue
		}
		if _, ok := by[d.Key]; !ok {
			keys = append(keys, d.Key)
		}
		by[d.Key] = append(by[d.Key], d)
	}
	out := make([]Line, 0, len(keys))
	for _, k := range keys {
		ds := by[k]
		l := Line{Key: k, Days: len(ds)}
		points := make([]int, len(ds))
		kms := make([]float64, len(ds))
		idx := make([]int, len(ds))
		sum := 0
		for i, d := range ds {
			points[i], kms[i], idx[i] = d.Points, d.Km, at[d.Date]
			sum += d.Points
			if d.Points > l.Best || d.Points == l.Best && d.Date.Before(l.BestDate) {
				l.Best, l.BestDate = d.Points, d.Date
			}
		}
		l.Avg = (sum + len(ds)/2) / len(ds)
		l.Score = sum
		if period == Month && len(points) > Best {
			slices.SortFunc(points, func(a, b int) int { return cmp.Compare(b, a) })
			l.Score = 0
			for _, p := range points[:Best] {
				l.Score += p
			}
		}
		l.Km = median(kms)
		l.Streak = longest(idx)
		l.League = LeagueOf(l.Avg, l.Days)
		out = append(out, l)
	}
	return out
}

func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// longest is the longest run of consecutive numbers in idx.
func longest(idx []int) int {
	s := slices.Clone(idx)
	slices.Sort(s)
	best, run := 0, 0
	for i, v := range s {
		if i > 0 && v == s[i-1]+1 {
			run++
		} else if i == 0 || v != s[i-1] {
			run = 1
		}
		best = max(best, run)
	}
	return best
}

// Sorts, as the page names them.
const (
	ByScore = "score"
	ByAvg   = "avg"
	ByDays  = "days"
	ByKm    = "km"
)

// KnownSort says whether by is one the page offers.
func KnownSort(by string) bool { return by == ByScore || by == ByAvg || by == ByDays || by == ByKm }

// Ranked is a line with its place.
type Ranked struct {
	Line
	Rank int
}

// Rank orders lines by one measure, the score unless by says otherwise, and
// gives equal ones the same place. The average and the median miss rank
// those with MinDays days first: one lucky day is not an average. Ties then
// go to more days, then to the key, so the order never changes between
// reads.
func Rank(lines []Line, by string) []Ranked {
	full := func(l Line) int {
		if l.Days >= MinDays {
			return 0
		}
		return 1
	}
	measure := func(a, b Line) int {
		switch by {
		case ByAvg:
			return cmp.Or(cmp.Compare(full(a), full(b)), cmp.Compare(b.Avg, a.Avg))
		case ByDays:
			return cmp.Or(cmp.Compare(b.Days, a.Days), cmp.Compare(b.Score, a.Score))
		case ByKm:
			return cmp.Or(cmp.Compare(full(a), full(b)), cmp.Compare(a.Km, b.Km))
		}
		return cmp.Compare(b.Score, a.Score)
	}
	s := slices.Clone(lines)
	slices.SortStableFunc(s, func(a, b Line) int {
		return cmp.Or(measure(a, b), cmp.Compare(b.Days, a.Days), cmp.Compare(a.Key, b.Key))
	})
	out := make([]Ranked, len(s))
	for i, l := range s {
		out[i] = Ranked{Line: l, Rank: i + 1}
		if i > 0 && measure(s[i-1], l) == 0 {
			out[i].Rank = out[i-1].Rank
		}
	}
	return out
}

// Cards are the period's four standouts, each the key of a line; "" when
// nobody qualifies.
type Cards struct {
	// Active played the most days, the higher score first among equals.
	Active string
	// Accurate has the smallest median miss over MinDays days or more.
	Accurate string
	// Day had the best single day, the earlier first among equals.
	Day string
	// Streak played the most days in a row.
	Streak string
}

// CardsOf picks the cards from lines.
func CardsOf(lines []Line) Cards {
	var c Cards
	var active, accurate, day, streak *Line
	for i := range lines {
		l := &lines[i]
		if active == nil || l.Days > active.Days || l.Days == active.Days && (l.Score > active.Score || l.Score == active.Score && l.Key < active.Key) {
			active = l
		}
		if l.Days >= MinDays && (accurate == nil || l.Km < accurate.Km || l.Km == accurate.Km && l.Key < accurate.Key) {
			accurate = l
		}
		if l.Best > 0 && (day == nil || l.Best > day.Best || l.Best == day.Best && (l.BestDate.Before(day.BestDate) || l.BestDate == day.BestDate && l.Key < day.Key)) {
			day = l
		}
		if streak == nil || l.Streak > streak.Streak || l.Streak == streak.Streak && (l.Days > streak.Days || l.Days == streak.Days && l.Key < streak.Key) {
			streak = l
		}
	}
	if active != nil {
		c.Active = active.Key
	}
	if accurate != nil {
		c.Accurate = accurate.Key
	}
	if day != nil {
		c.Day = day.Key
	}
	if streak != nil && streak.Streak > 1 {
		c.Streak = streak.Key
	}
	return c
}
