// SPDX-License-Identifier: Apache-2.0
package rating

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/round"
)

func dates(n int) []round.Date {
	first := round.NewDate(2026, time.October, 1)
	out := make([]round.Date, n)
	for i := range out {
		out[i] = first.AddDays(i)
	}
	return out
}

func TestLeagues(t *testing.T) {
	for _, c := range []struct {
		avg, days int
		want      string
	}{
		{5000, 4, Novice},
		{0, 5, Tourist},
		{1999, 5, Tourist},
		{2000, 5, Traveler},
		{3199, 9, Traveler},
		{3200, 9, Navigator},
		{4199, 30, Navigator},
		{4200, 30, Cartographer},
	} {
		if got := LeagueOf(c.avg, c.days); got != c.want {
			t.Errorf("LeagueOf(%d, %d) = %s, want %s", c.avg, c.days, got, c.want)
		}
	}
	if l, from := Next(Traveler); l != Navigator || from != 3200 {
		t.Errorf("after a traveler = %s %d", l, from)
	}
	if l, _ := Next(Cartographer); l != "" {
		t.Errorf("after the top = %s", l)
	}
	if l, _ := Next(Novice); l != "" {
		t.Errorf("after a new person = %s", l)
	}
}

// The month counts the 20 best of its days; the week and all time count
// every one.
func TestScores(t *testing.T) {
	ds := dates(30)
	var days []Day
	for i, d := range ds {
		days = append(days, Day{Key: "a", Date: d, Points: 1000 + i*10, Km: float64(i)})
	}
	days = append(days, Day{Key: "b", Date: ds[0], Points: 4900, Km: 3}, Day{Key: "late", Date: ds[29].AddDays(1), Points: 5000})
	month := Lines(Month, ds, days)
	if len(month) != 2 {
		t.Fatalf("lines = %+v", month)
	}
	a := month[0]
	// The best 20 are days 10 to 29: 1100 … 1290.
	if a.Score != 23900 || a.Days != 30 || a.Avg != 1145 || a.Best != 1290 || a.BestDate != ds[29] || a.Streak != 30 || a.League != Tourist {
		t.Errorf("a over the month = %+v", a)
	}
	if a.Km != 14.5 {
		t.Errorf("a's median = %v", a.Km)
	}
	if all := Lines(All, ds, days); all[0].Score != 34350 {
		t.Errorf("a over all time = %+v", all[0])
	}
	if b := month[1]; b.Score != 4900 || b.League != Novice || b.Streak != 1 {
		t.Errorf("b = %+v", b)
	}
}

// A run of days counts the days the period has: a date left out of them, a
// skipped day, breaks nothing.
func TestStreakStepsOverMissingDates(t *testing.T) {
	ds := dates(6)
	kept := []round.Date{ds[0], ds[1], ds[3], ds[4], ds[5]}
	var days []Day
	for _, d := range kept {
		days = append(days, Day{Key: "a", Date: d, Points: 3000})
	}
	if l := Lines(All, kept, days)[0]; l.Streak != 5 {
		t.Errorf("streak over a skipped date = %d", l.Streak)
	}
	days = days[:4]
	days = append(days[:1], days[2:]...)
	if l := Lines(All, kept, days)[0]; l.Streak != 2 {
		t.Errorf("streak with a missed day = %d", l.Streak)
	}
}

func line(r []Ranked) string {
	var b strings.Builder
	for i, x := range r {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d %s", x.Rank, x.Key)
	}
	return b.String()
}

func TestRank(t *testing.T) {
	lines := []Line{
		{Key: "a", Score: 9000, Days: 3, Avg: 3000, Km: 50},
		{Key: "b", Score: 9000, Days: 5, Avg: 1800, Km: 400},
		{Key: "c", Score: 12000, Days: 6, Avg: 2000, Km: 900},
		{Key: "d", Score: 4000, Days: 1, Avg: 4000, Km: 1},
	}
	// Equal scores share a place; more days come first among them.
	if got := line(Rank(lines, ByScore)); got != "1 c, 2 b, 2 a, 4 d" {
		t.Errorf("by score: %s", got)
	}
	// One lucky day is not an average: five days first.
	if got := line(Rank(lines, ByAvg)); got != "1 c, 2 b, 3 d, 4 a" {
		t.Errorf("by average: %s", got)
	}
	if got := line(Rank(lines, ByKm)); got != "1 b, 2 c, 3 d, 4 a" {
		t.Errorf("by miss: %s", got)
	}
	if got := line(Rank(lines, ByDays)); got != "1 c, 2 b, 3 a, 4 d" {
		t.Errorf("by days: %s", got)
	}
}

func TestCards(t *testing.T) {
	d := dates(3)
	lines := []Line{
		{Key: "a", Days: 9, Score: 100, Km: 30, Best: 4000, BestDate: d[1], Streak: 4},
		{Key: "b", Days: 9, Score: 200, Km: 900, Best: 4000, BestDate: d[0], Streak: 9},
		{Key: "c", Days: 2, Score: 50, Km: 1, Best: 3000, BestDate: d[2], Streak: 2},
	}
	c := CardsOf(lines)
	if c.Active != "b" || c.Accurate != "a" || c.Day != "b" || c.Streak != "b" {
		t.Errorf("cards = %+v", c)
	}
	// A run of one day is no streak, and nobody with fewer than five days is
	// the most accurate.
	c = CardsOf([]Line{{Key: "x", Days: 1, Best: 10, Streak: 1}})
	if c.Streak != "" || c.Accurate != "" || c.Active != "x" || c.Day != "x" {
		t.Errorf("one day's cards = %+v", c)
	}
	if c := CardsOf(nil); c != (Cards{}) {
		t.Errorf("no cards = %+v", c)
	}
}
