// SPDX-License-Identifier: Apache-2.0
package profile

import (
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

var day0 = round.NewDate(2026, time.October, 1)

// played is a day n days after day0 with its numbers.
func played(n, points int, km float64, bonus int, continent, country string) db.PlayedDay {
	return db.PlayedDay{Date: day0.AddDays(n), DayNo: n + 1, Km: km, Points: points, Base: points, Bonus: bonus,
		Continent: continent, Country: country, City: map[string]string{"en": "City", "ru": "Город"}}
}

func rounds(from, to int) []db.RoundDay {
	var out []db.RoundDay
	for n := to; n >= from; n-- {
		out = append(out, db.RoundDay{Date: day0.AddDays(n), DayNo: n + 1})
	}
	return out
}

func everyDayClosed(round.Date) bool { return true }

func TestPercentileCountsTiesAsHalf(t *testing.T) {
	for _, c := range []struct {
		st   db.Standing
		want int
		ok   bool
	}{
		{db.Standing{Below: 9, N: 10}, 100, true},
		{db.Standing{Below: 0, N: 10}, 0, true},
		{db.Standing{Below: 3, Ties: 2, N: 6}, 80, true},
		{db.Standing{N: 1}, 0, false},
	} {
		got, ok := percentile(c.st)
		if got != c.want || ok != c.ok {
			t.Errorf("percentile(%+v) = %d, %v; want %d, %v", c.st, got, ok, c.want, c.ok)
		}
	}
}

func TestRankAveragesPercentileOverClosedDaysOnly(t *testing.T) {
	days := []db.PlayedDay{played(0, 4000, 100, 800, "europe", "FR"), played(1, 3000, 400, 300, "europe", "DE"), played(2, 2000, 900, 0, "asia", "JP")}
	st := []db.Standing{
		{Date: days[0].Date, Below: 9, N: 11},  // 90
		{Date: days[1].Date, Below: 7, N: 11},  // 70
		{Date: days[2].Date, Below: 0, N: 101}, // open: left out
	}
	open := days[2].Date
	r := Rank(Input{Days: days, Standings: st, Ref: days[2].Date, Closed: func(d round.Date) bool { return d.String() != open.String() }})
	if r.Percentile == nil || *r.Percentile != 80 {
		t.Fatalf("percentile = %v, want 80", r.Percentile)
	}
}

func TestRankNumbers(t *testing.T) {
	days := []db.PlayedDay{
		played(0, 4000, 0.5, 1500, "europe", "FR"),
		played(1, 3000, 40, 800, "europe", "FR"),
		played(2, 1000, 3000, 0, "africa", "EG"),
		played(4, 2000, 600, 300, "africa", "MA"),
		played(5, 4500, 20, 800, "europe", "DE"),
	}
	days[1].Rank, days[1].Of = 3, 40
	days[4].Rank, days[4].Of = 3, 90
	r := Rank(Input{Days: days, Rounds: rounds(0, 5), Ref: day0.AddDays(5), Closed: everyDayClosed, Lang: "ru", WithHistory: true})
	if r.Days != 5 || r.Points != 14500 {
		t.Fatalf("days %d points %d", r.Days, r.Points)
	}
	if r.Streak != 2 || r.BestStreak != 3 {
		t.Fatalf("streak %d best %d, want 2 and 3", r.Streak, r.BestStreak)
	}
	if *r.MedianKm != 40 {
		t.Fatalf("median km %v", *r.MedianKm)
	}
	if r.CountryPct != 60 || r.CityPct != 20 {
		t.Fatalf("country %d%% city %d%%", r.CountryPct, r.CityPct)
	}
	if r.BestDay.Day != 6 || r.BestDay.Points != 4500 {
		t.Fatalf("best day %+v", r.BestDay)
	}
	// The same rank among more players is the better place.
	if r.BestPlace.Rank != 3 || r.BestPlace.Of != 90 {
		t.Fatalf("best place %+v", r.BestPlace)
	}
	if got := r.Passport.Countries; len(got) != 2 || got[0] != "DE" || got[1] != "FR" {
		t.Fatalf("passport %v", got)
	}
	if !r.Passport.City1km || r.Passport.Streak30 {
		t.Fatalf("stamps %+v", r.Passport)
	}
	if r.Best != "europe" || r.Worst != "africa" {
		t.Fatalf("best %q worst %q", r.Best, r.Worst)
	}
	// ≤1, ≤10, ≤50, ≤150, ≤500, ≤1000, ≤2500, more
	want := []int{1, 0, 2, 0, 0, 1, 0, 1}
	for i := range want {
		if r.Misses[i] != want[i] {
			t.Fatalf("misses %v, want %v", r.Misses, want)
		}
	}
	if len(r.Series) != 6 || r.Series[3].Points != nil || *r.Series[5].Points != 4500 {
		t.Fatalf("series %+v", r.Series)
	}
	if *r.Median30 != 3000 {
		t.Fatalf("median30 %d", *r.Median30)
	}
	if len(r.History) != 5 || r.History[0].Day != 6 || r.History[0].City != "Город" || r.History[0].Rank != 3 {
		t.Fatalf("history %+v", r.History[0])
	}
}

func TestStreakEndsTodayOrYesterday(t *testing.T) {
	dates := []round.Date{day0, day0.AddDays(1), day0.AddDays(2)}
	for _, c := range []struct {
		ref       round.Date
		cur, best int
	}{
		{day0.AddDays(2), 3, 3},
		{day0.AddDays(3), 3, 3},
		{day0.AddDays(4), 0, 3},
	} {
		cur, best := streaks(dates, c.ref, nil)
		if cur != c.cur || best != c.best {
			t.Errorf("ref %s: %d, %d; want %d, %d", c.ref, cur, best, c.cur, c.best)
		}
	}
}

// A date nobody answered is no day missed: a run goes over it, and a run
// that ends before it still ends "yesterday".
func TestStreakStepsOverSkippedDays(t *testing.T) {
	skipped := []round.Date{day0.AddDays(1), day0.AddDays(2)}
	dates := []round.Date{day0, day0.AddDays(3), day0.AddDays(4)}
	for _, c := range []struct {
		name      string
		dates     []round.Date
		skipped   []round.Date
		ref       round.Date
		cur, best int
	}{
		{"over two skipped days", dates, skipped, day0.AddDays(4), 3, 3},
		{"not skipped, a gap", dates, nil, day0.AddDays(4), 2, 2},
		{"skipped day before ref", dates[:1], skipped, day0.AddDays(2), 1, 1},
		{"the day before the skipped ones", dates[:1], skipped, day0.AddDays(3), 1, 1},
		{"a skipped day is not today", dates[:1], skipped[:1], day0.AddDays(4), 0, 1},
	} {
		cur, best := streaks(c.dates, c.ref, c.skipped)
		if cur != c.cur || best != c.best {
			t.Errorf("%s: %d, %d; want %d, %d", c.name, cur, best, c.cur, c.best)
		}
	}
}

func TestAvg30KeepsTheLastThirtyDays(t *testing.T) {
	days := []db.PlayedDay{played(0, 1000, 1, 0, "", ""), played(40, 3000, 1, 0, "", ""), played(45, 4000, 1, 0, "", "")}
	r := Rank(Input{Days: days, Ref: day0.AddDays(45), Closed: everyDayClosed})
	if r.Avg30 == nil || *r.Avg30 != 3500 {
		t.Fatalf("avg30 = %v", r.Avg30)
	}
	r = Rank(Input{Days: days[:1], Ref: day0.AddDays(45), Closed: everyDayClosed})
	if r.Avg30 != nil {
		t.Fatalf("avg30 = %v, want none", *r.Avg30)
	}
}

func TestOneContinentHasNoBestOrWorst(t *testing.T) {
	r := Rank(Input{Days: []db.PlayedDay{played(0, 1000, 1, 0, "europe", ""), played(1, 2000, 1, 0, "europe", "")}, Ref: day0.AddDays(1), Closed: everyDayClosed})
	if r.Best != "" || r.Worst != "" {
		t.Fatalf("best %q worst %q", r.Best, r.Worst)
	}
}

func TestOpenDayHasNoWorldPlace(t *testing.T) {
	d := played(0, 5000, 0, 1500, "europe", "FR")
	d.Rank, d.Of = 1, 2
	r := Rank(Input{Days: []db.PlayedDay{d}, Ref: d.Date, Closed: func(round.Date) bool { return false }, WithHistory: true})
	if r.BestPlace != nil || r.History[0].Rank != 0 || r.History[0].Final {
		t.Fatalf("an open day shows a world place: %+v %+v", r.BestPlace, r.History[0])
	}
}

func TestEmptyProfileHasEmptyLists(t *testing.T) {
	r := Rank(Input{Ref: day0, Closed: everyDayClosed})
	if r.Series == nil || r.Continents == nil || r.Passport.Countries == nil || len(r.Misses) != len(Missed)+1 {
		t.Fatalf("empty profile: %+v", r)
	}
}

func TestFreeHasAveragesAndRecordsOnly(t *testing.T) {
	km := func(v ...float64) []float64 { return v }
	f := FreeOf([]db.FreeGame{
		{Kind: "solo", Mode: "classic", Seconds: 60, Rounds: 5, Played: 5, Points: 20000, Base: 18000, KmRounds: km(300, 300)},
		{Kind: "solo", Mode: "classic", Seconds: 60, Rounds: 5, Played: 5, Points: 22000, Base: 20000, KmRounds: km(100)},
		{Kind: "solo", Mode: "classic", Rounds: 10, Played: 10, Points: 30000, Base: 30000, KmRounds: km(500, 700)},
		{Kind: "solo", Mode: "streak", Seconds: 30, Played: 7, Points: 0, Base: 0, Streak: 6},
		{Kind: "duel", Mode: "classic", Seconds: 30, Rounds: 3, Played: 3, Points: 9000, Base: 8000, Place: 1, Players: 2, Beaten: 1},
		{Kind: "duel", Mode: "classic", Seconds: 30, Rounds: 3, Played: 3, Points: 7000, Base: 7000, Place: 2, Players: 2},
		// A drawn duel: both first, nobody beaten.
		{Kind: "duel", Mode: "classic", Seconds: 30, Rounds: 3, Played: 0, Points: 0, Base: 0, Place: 1, Players: 2},
		{Kind: "party", Mode: "classic", Seconds: 90, Rounds: 5, Played: 5, Points: 15000, Base: 14000, Place: 2, Players: 5, Beaten: 3},
	})
	if f.Games != 8 || f.Places != 38 {
		t.Fatalf("games %d places %d", f.Games, f.Places)
	}
	if *f.Avg != 2553 { // 97 000 over 38 places
		t.Fatalf("avg %d", *f.Avg)
	}
	if *f.MedianKm != 300 {
		t.Fatalf("median %v", *f.MedianKm)
	}
	if f.Duels.Won != 1 || f.Duels.Lost != 1 {
		t.Fatalf("duels %+v", f.Duels)
	}
	if *f.Beaten != 75 {
		t.Fatalf("beaten %d", *f.Beaten)
	}
	// 30 s first, no timer last.
	if len(f.Records) != 4 || f.Records[0].Seconds != 30 || f.Records[1].Points != 22000 || f.Records[3].Seconds != 0 {
		t.Fatalf("records %+v", f.Records)
	}
	if len(f.Streaks) != 1 || f.Streaks[0].Streak != 6 {
		t.Fatalf("streaks %+v", f.Streaks)
	}
	empty := FreeOf(nil)
	if empty.Avg != nil || empty.Records == nil || empty.Duels != nil {
		t.Fatalf("empty free %+v", empty)
	}
}

func TestLatestClosedIsOverEverywhere(t *testing.T) {
	z, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	d := LatestClosed(z, now)
	if z.LastReveal(d).After(now) || !z.LastReveal(d.AddDays(1)).After(now) {
		t.Fatalf("latest closed %s at %s", d, now)
	}
}
