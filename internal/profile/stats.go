// SPDX-License-Identifier: Apache-2.0
package profile

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
)

// The profile's numbers are worked out here from a player's scored days, so
// the rules of what counts are in one place and tested without a database.
// What a viewer may see is decided before: the days handed in are already
// only the ones that viewer may read (internal/api/profile.go).

// Missed are the upper ends of the misses' bins, in km; one more bin holds
// everything further.
var Missed = []float64{1, 10, 50, 150, 500, 1000, 2500}

// SeriesDays is how many of the last rounds the points chart shows.
const SeriesDays = 30

// Ranked is the ranked tab: the day's game only.
type Ranked struct {
	Days   int `json:"days"`
	Points int `json:"points"`
	// Avg30 is the mean of the days played in the last 30 calendar days,
	// nil when there was none.
	Avg30      *int `json:"avg30"`
	Streak     int  `json:"streak"`
	BestStreak int  `json:"best_streak"`
	// MedianKm is the typical miss, nil before the first scored day.
	MedianKm   *float64 `json:"median_km"`
	CountryPct int      `json:"country_pct"`
	CityPct    int      `json:"city_pct"`
	BestDay    *DayRef  `json:"best_day"`
	BestPlace  *Place   `json:"best_place"`
	// Percentile is how many of the other players of a day the player
	// beat, on average over the days that are over everywhere: "better than
	// 78% of players". Nil until such a day had two players.
	Percentile *int      `json:"percentile"`
	Series     []Point   `json:"series"`
	Median30   *int      `json:"median30"`
	Misses     []int     `json:"misses"`
	Continents []Region  `json:"continents"`
	Best       string    `json:"best_continent,omitempty"`
	Worst      string    `json:"worst_continent,omitempty"`
	Passport   Passport  `json:"passport"`
	History    []History `json:"history,omitempty"`
}

// DayRef points at one day.
type DayRef struct {
	Day    int    `json:"day"`
	Date   string `json:"date"`
	Points int    `json:"points"`
}

// Place is a final world place.
type Place struct {
	Day  int    `json:"day"`
	Date string `json:"date"`
	Rank int    `json:"rank"`
	Of   int    `json:"of"`
}

// Point is one round on the points chart; Points is nil on a day not played.
type Point struct {
	Day    int    `json:"day"`
	Date   string `json:"date"`
	Points *int   `json:"points"`
}

// Region is how a player does on one continent's places.
type Region struct {
	Continent  string  `json:"continent"`
	Name       string  `json:"name"`
	Days       int     `json:"days"`
	Avg        int     `json:"avg"`
	MedianKm   float64 `json:"median_km"`
	CountryPct int     `json:"country_pct"`
}

// Passport holds the countries a player found: a day in which the pin landed
// in the right country colours that country. Streak30 and City1km are its two
// stamps: thirty days in a row, and a pin within a kilometre in the right city.
type Passport struct {
	Countries []string `json:"countries"`
	Streak30  bool     `json:"streak30"`
	City1km   bool     `json:"city1km"`
}

// History is one played day in the list under the charts, with the pin.
type History struct {
	Day    int     `json:"day"`
	Date   string  `json:"date"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Km     float64 `json:"km"`
	Points int     `json:"points"`
	Bonus  int     `json:"bonus"`
	// Country and City are the day's place.
	Country string `json:"country,omitempty"`
	City    string `json:"city,omitempty"`
	// Rank among Of and Pct are set once the day is over everywhere.
	Rank int  `json:"rank,omitempty"`
	Of   int  `json:"of,omitempty"`
	Pct  *int `json:"pct,omitempty"`
	// Final says the day is over everywhere: its page opens.
	Final bool `json:"final"`
}

// Input is what Rank works from.
type Input struct {
	Days      []db.PlayedDay // oldest first
	Standings []db.Standing
	Rounds    []db.RoundDay // the last rounds up to Ref, newest first
	// Ref is the date the profile is as of: the streak may end on it or the
	// day before, and the last 30 days end on it.
	Ref round.Date
	// Closed tells a day over everywhere.
	Closed func(round.Date) bool
	// Lang names the cities in History.
	Lang string
	// WithHistory adds the list of days: the owner's and the public page's.
	WithHistory bool
}

// Rank works out the ranked tab.
func Rank(in Input) Ranked {
	r := Ranked{Misses: make([]int, len(Missed)+1), Continents: []Region{}, Series: []Point{}}
	r.Passport.Countries = []string{}
	pct := map[string]int{}
	for _, st := range in.Standings {
		if p, ok := percentile(st); ok && in.Closed(st.Date) {
			pct[st.Date.String()] = p
		}
	}
	var (
		kms       []float64
		countries = map[string]bool{}
		recent    []int
		pctSum    int
		pctN      int
		dates     []round.Date
		regions   = map[string]*regionAcc{}
	)
	from := in.Ref.AddDays(-29)
	for _, d := range in.Days {
		r.Days++
		r.Points += d.Points
		kms = append(kms, d.Km)
		dates = append(dates, d.Date)
		if d.Bonus >= 800 {
			r.CountryPct++
			if d.Country != "" {
				countries[d.Country] = true
			}
		}
		if d.Bonus >= 1500 {
			r.CityPct++
			if d.Km <= 1 {
				r.Passport.City1km = true
			}
		}
		if !d.Date.Before(from) && !in.Ref.Before(d.Date) {
			recent = append(recent, d.Points)
		}
		r.Misses[bin(d.Km)]++
		if r.BestDay == nil || d.Points > r.BestDay.Points {
			r.BestDay = &DayRef{Day: d.DayNo, Date: d.Date.String(), Points: d.Points}
		}
		final := in.Closed(d.Date)
		if final && d.Rank > 0 && (r.BestPlace == nil || d.Rank < r.BestPlace.Rank || d.Rank == r.BestPlace.Rank && d.Of > r.BestPlace.Of) {
			r.BestPlace = &Place{Day: d.DayNo, Date: d.Date.String(), Rank: d.Rank, Of: d.Of}
		}
		if p, ok := pct[d.Date.String()]; ok {
			pctSum += p
			pctN++
		}
		if d.Continent != "" {
			a := regions[d.Continent]
			if a == nil {
				a = &regionAcc{}
				regions[d.Continent] = a
			}
			a.points += d.Points
			a.kms = append(a.kms, d.Km)
			if d.Bonus >= 800 {
				a.countries++
			}
		}
		if in.WithHistory {
			h := History{Day: d.DayNo, Date: d.Date.String(), Lat: d.Lat, Lon: d.Lon, Km: d.Km, Points: d.Points, Bonus: d.Bonus,
				Country: d.Country, City: localized(d.City, in.Lang), Final: final}
			if final && d.Rank > 0 {
				h.Rank, h.Of = d.Rank, d.Of
			}
			if p, ok := pct[d.Date.String()]; ok {
				h.Pct = &p
			}
			r.History = append(r.History, h)
		}
	}
	if r.Days > 0 {
		r.CountryPct = share(r.CountryPct, r.Days)
		r.CityPct = share(r.CityPct, r.Days)
		m := median(kms)
		r.MedianKm = &m
	}
	if len(recent) > 0 {
		avg := mean(recent)
		r.Avg30 = &avg
	}
	if pctN > 0 {
		p := int(math.Round(float64(pctSum) / float64(pctN)))
		r.Percentile = &p
	}
	r.Streak, r.BestStreak = streaks(dates, in.Ref)
	r.Passport.Streak30 = r.BestStreak >= 30
	for c := range countries {
		r.Passport.Countries = append(r.Passport.Countries, c)
	}
	slices.Sort(r.Passport.Countries)
	r.Series, r.Median30 = series(in.Days, in.Rounds)
	r.Continents, r.Best, r.Worst = continents(regions)
	for i := range r.Continents {
		r.Continents[i].Name = i18n.T(in.Lang, "continent."+r.Continents[i].Continent)
	}
	slices.Reverse(r.History)
	return r
}

type regionAcc struct {
	points    int
	kms       []float64
	countries int
}

// percentile is the share of a day's other players a player beat, a tie
// counted as half: 0 when last, 100 when first. A day with one player has none.
func percentile(st db.Standing) (int, bool) {
	if st.N < 2 {
		return 0, false
	}
	return int(math.Round(100 * (float64(st.Below) + float64(st.Ties)/2) / float64(st.N-1))), true
}

func bin(km float64) int {
	for i, top := range Missed {
		if km <= top {
			return i
		}
	}
	return len(Missed)
}

func share(n, of int) int { return int(math.Round(100 * float64(n) / float64(of))) }

func mean(xs []int) int {
	sum := 0
	for _, x := range xs {
		sum += x
	}
	return int(math.Round(float64(sum) / float64(len(xs))))
}

func median(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// streaks are the run of days in a row that ends on ref or the day before,
// as the bot counts it, and the longest run there has been. dates are
// ascending and distinct.
func streaks(dates []round.Date, ref round.Date) (current, best int) {
	run := 0
	var prev round.Date
	for i, d := range dates {
		if i > 0 && prev.AddDays(1).String() == d.String() {
			run++
		} else {
			run = 1
		}
		best = max(best, run)
		prev = d
	}
	if len(dates) > 0 {
		last := dates[len(dates)-1].String()
		if last == ref.String() || last == ref.AddDays(-1).String() {
			current = run
		}
	}
	return current, best
}

// series lays the played days on the last rounds, oldest first, and the
// median of the points among them.
func series(days []db.PlayedDay, rounds []db.RoundDay) ([]Point, *int) {
	byDate := map[string]int{}
	for _, d := range days {
		byDate[d.Date.String()] = d.Points
	}
	out := make([]Point, 0, min(len(rounds), SeriesDays))
	var played []float64
	for i := min(len(rounds), SeriesDays) - 1; i >= 0; i-- {
		rd := rounds[i]
		p := Point{Day: rd.DayNo, Date: rd.Date.String()}
		if v, ok := byDate[rd.Date.String()]; ok {
			p.Points = &v
			played = append(played, float64(v))
		}
		out = append(out, p)
	}
	if len(played) == 0 {
		return out, nil
	}
	m := int(math.Round(median(played)))
	return out, &m
}

// continents rank the player's continents by mean points. The best and the
// worst are named only when there are two continents to tell apart, each with
// two days or more when any has that many, so one lucky day is not a "best".
func continents(acc map[string]*regionAcc) ([]Region, string, string) {
	out := make([]Region, 0, len(acc))
	enough := 1
	for _, a := range acc {
		if len(a.kms) >= 2 {
			enough = 2
		}
	}
	for c, a := range acc {
		out = append(out, Region{Continent: c, Days: len(a.kms), Avg: int(math.Round(float64(a.points) / float64(len(a.kms)))),
			MedianKm: median(a.kms), CountryPct: share(a.countries, len(a.kms))})
	}
	slices.SortFunc(out, func(a, b Region) int {
		return cmp.Or(cmp.Compare(b.Avg, a.Avg), cmp.Compare(b.Days, a.Days), cmp.Compare(a.Continent, b.Continent))
	})
	var ranked []Region
	for _, r := range out {
		if r.Days >= enough {
			ranked = append(ranked, r)
		}
	}
	if len(ranked) < 2 {
		return out, "", ""
	}
	return out, ranked[0].Continent, ranked[len(ranked)-1].Continent
}

func localized(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	return m["en"]
}

// Mini is what anyone who sees a player's name may learn by tapping it:
// numbers over the days that are over everywhere, no pins and no history.
type Mini struct {
	Name      string `json:"name"`
	Days      int    `json:"days"`
	Streak    int    `json:"streak"`
	Avg30     *int   `json:"avg30"`
	BestPlace *Place `json:"best_place"`
	Countries int    `json:"countries"`
	Free      *Brief `json:"free,omitempty"`
}

// MiniOf cuts the ranked numbers down to the mini profile's.
func MiniOf(name string, r Ranked, free Free) Mini {
	m := Mini{Name: name, Days: r.Days, Streak: r.Streak, Avg30: r.Avg30, BestPlace: r.BestPlace, Countries: len(r.Passport.Countries)}
	if free.Games > 0 {
		m.Free = &Brief{Avg: free.Avg, Duels: free.Duels}
	}
	return m
}

// LatestClosed is the newest date over in every zone at now.
func LatestClosed(z round.Zones, now time.Time) round.Date {
	d := z.Newest(now)
	for z.LastReveal(d).After(now) {
		d = d.AddDays(-1)
	}
	return d
}
