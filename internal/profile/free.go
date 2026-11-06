// SPDX-License-Identifier: Apache-2.0
package profile

import (
	"cmp"
	"math"
	"slices"

	"github.com/FreshLabDev/maply/internal/db"
)

// Free is the free tab. Free games have no limit, so it holds averages,
// medians and records only: a sum would reward playing more, not playing
// better.
type Free struct {
	Games  int `json:"games"`
	Places int `json:"places"`
	// Avg is the mean points per place without the speed bonus.
	Avg      *int     `json:"avg"`
	MedianKm *float64 `json:"median_km"`
	// Records are the best game of each timer and length of a classic game.
	Records []Record `json:"records"`
	// Streaks are the longest country streak of each timer.
	Streaks []StreakRecord `json:"streaks"`
	Duels   *Duels         `json:"duels"`
	// Beaten is the mean share of the other players a player finished ahead
	// of in a party, in percent; nil before a party.
	Beaten *int `json:"beaten"`
}

// Record is a classic game's best total for one setting; Seconds 0 is no timer.
type Record struct {
	Seconds int `json:"seconds"`
	Rounds  int `json:"rounds"`
	Points  int `json:"points"`
}

type StreakRecord struct {
	Seconds int `json:"seconds"`
	Streak  int `json:"streak"`
}

type Duels struct {
	Won  int `json:"won"`
	Lost int `json:"lost"`
}

// Brief is the free part of a mini profile.
type Brief struct {
	Avg   *int   `json:"avg"`
	Duels *Duels `json:"duels"`
}

// FreeOf works out the free tab from a player's finished free games.
func FreeOf(games []db.FreeGame) Free {
	f := Free{Records: []Record{}, Streaks: []StreakRecord{}}
	var (
		base, places int
		kms          []float64
		beaten       float64
		parties      int
		records      = map[[2]int]int{}
		streaks      = map[int]int{}
	)
	for _, g := range games {
		f.Games++
		f.Places += g.Played
		base += g.Base
		places += g.Played
		kms = append(kms, g.KmRounds...)
		switch g.Mode {
		case "classic":
			k := [2]int{g.Seconds, g.Rounds}
			if v, ok := records[k]; !ok || g.Points > v {
				records[k] = g.Points
			}
		case "streak":
			streaks[g.Seconds] = max(streaks[g.Seconds], g.Streak)
		}
		// A tie shares the place and beats nobody: a drawn duel is neither won
		// nor lost.
		switch {
		case g.Kind == "duel" && g.Players > 1:
			if f.Duels == nil {
				f.Duels = &Duels{}
			}
			if g.Beaten > 0 {
				f.Duels.Won++
			} else if g.Place > 1 {
				f.Duels.Lost++
			}
		case g.Kind == "party" && g.Players > 1:
			beaten += float64(g.Beaten) / float64(g.Players-1)
			parties++
		}
	}
	if places > 0 {
		avg := int(math.Round(float64(base) / float64(places)))
		f.Avg = &avg
	}
	if len(kms) > 0 {
		m := median(kms)
		f.MedianKm = &m
	}
	if parties > 0 {
		b := int(math.Round(100 * beaten / float64(parties)))
		f.Beaten = &b
	}
	for k, v := range records {
		f.Records = append(f.Records, Record{Seconds: k[0], Rounds: k[1], Points: v})
	}
	// Shortest timer first, no timer last; then by length.
	timer := func(s int) int {
		if s == 0 {
			return math.MaxInt
		}
		return s
	}
	slices.SortFunc(f.Records, func(a, b Record) int {
		return cmp.Or(cmp.Compare(timer(a.Seconds), timer(b.Seconds)), cmp.Compare(a.Rounds, b.Rounds))
	})
	for s, v := range streaks {
		f.Streaks = append(f.Streaks, StreakRecord{Seconds: s, Streak: v})
	}
	slices.SortFunc(f.Streaks, func(a, b StreakRecord) int { return cmp.Compare(timer(a.Seconds), timer(b.Seconds)) })
	return f
}
