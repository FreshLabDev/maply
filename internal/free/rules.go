// SPDX-License-Identifier: Apache-2.0
package free

import (
	"sort"
	"time"
)

// Line is one player's game: the sums a standing and a record are made of.
type Line struct {
	M int `json:"m"`
	// Points has the speed bonus; Base does not, which makes it a scale of
	// accuracy that compares across timers.
	Points int `json:"points"`
	Base   int `json:"base"`
	// Km is the sum a tie is broken on: a place without a pin counts as the
	// farthest a pin can be. KmRounds are the distances of the pins alone.
	Km       float64   `json:"km"`
	KmRounds []float64 `json:"-"`
	// TimeMs counts a place without an answer as its full time, so silence
	// never looks like a quick answer.
	TimeMs    int64 `json:"time_ms"`
	Played    int   `json:"played"`
	Answered  int   `json:"answered"`
	Countries int   `json:"countries"`
	FromRound int   `json:"from_round"`
	Streak    int   `json:"streak,omitempty"`
	Rank      int   `json:"rank"`
	// Beaten is how many of the others finished below.
	Beaten int `json:"beaten"`
}

// Lines sums every member's game over the places the game has had, ranked.
// A place before a late player joined counts as a miss for the tie, and not
// as played. A streak counts a player's places up to their first miss, where
// they are out and only watch; against the clock a player has only the places
// they reached, each at their own pace.
func Lines(s Settings, members []Member, rounds []Round, guesses []Guess) []Line {
	byKey := map[[2]int]Guess{}
	for _, g := range guesses {
		byKey[[2]int{g.N, g.M}] = g
	}
	full := s.Limit().Milliseconds()
	out := make([]Line, 0, len(members))
	for _, m := range members {
		l := Line{M: m.M, FromRound: m.FromRound, KmRounds: []float64{}}
		streak := true
		for _, r := range rounds {
			if s.Mode == Streak {
				if !streak {
					break
				}
				if r.N < m.FromRound {
					continue
				}
			}
			g, ok := byKey[[2]int{r.N, m.M}]
			if s.Mode == Timed && !ok {
				continue
			}
			if r.N >= m.FromRound {
				l.Played++
			}
			if !ok || g.Pin == nil {
				l.Km += MaxKm
				if ok {
					l.TimeMs += g.TimeMs
				} else {
					l.TimeMs += full
				}
				streak = false
				continue
			}
			l.Points += g.Points()
			l.Base += g.PtsKm + g.PtsBonus
			l.Km += g.Km
			l.KmRounds = append(l.KmRounds, g.Km)
			l.TimeMs += g.TimeMs
			l.Answered++
			if g.CountryOK {
				l.Countries++
				if streak {
					l.Streak++
				}
			} else {
				streak = false
			}
		}
		out = append(out, l)
	}
	RankFor(s.Mode, out)
	return out
}

// better is whether a stands above b: the mode's own score first (a streak's
// length, the countries found against the clock), then more points, then
// fewer kilometres in all, then less time in all.
func better(mode string, a, b Line) bool {
	switch {
	case mode == Streak && a.Streak != b.Streak:
		return a.Streak > b.Streak
	case mode == Timed && a.Countries != b.Countries:
		return a.Countries > b.Countries
	}
	if a.Points != b.Points {
		return a.Points > b.Points
	}
	if a.Km != b.Km {
		return a.Km < b.Km
	}
	return a.TimeMs < b.TimeMs
}

// Rank orders lines of a classic game best first and gives each its place.
// Lines equal in all three share a place.
func Rank(lines []Line) { RankFor(Classic, lines) }

// RankFor is Rank for the mode's own order.
func RankFor(mode string, lines []Line) {
	sort.SliceStable(lines, func(i, j int) bool {
		if better(mode, lines[i], lines[j]) != better(mode, lines[j], lines[i]) {
			return better(mode, lines[i], lines[j])
		}
		return lines[i].M < lines[j].M
	})
	for i := range lines {
		lines[i].Rank, lines[i].Beaten = 1, 0
		for j := range lines {
			if better(mode, lines[j], lines[i]) {
				lines[i].Rank++
			}
			if better(mode, lines[i], lines[j]) {
				lines[i].Beaten++
			}
		}
	}
}

// Pick chooses a game's next place. A place none of the players has seen
// comes first, in a country the game has not had yet if there is one, and
// from the free pool before a closed day's: a closed day is public with its
// answer on the days page, and its picture is the day's own. Only when every
// place has been seen does a repeat come: the one seen longest ago, again in a
// new country first.
func Pick(cands []Candidate, used map[string]bool, intn func(int) int) (Candidate, bool, bool) {
	tiers := [6][]Candidate{}
	for _, c := range cands {
		t := 0
		if used[c.Country] {
			t = 1
		}
		switch {
		case c.SeenAt != nil:
			t += 4
		case c.Day:
			t += 2
		}
		tiers[t] = append(tiers[t], c)
	}
	for t, tier := range tiers {
		if len(tier) == 0 {
			continue
		}
		if t < 4 {
			c := tier[intn(len(tier))]
			return c, false, true
		}
		oldest := tier[0]
		for _, c := range tier[1:] {
			if c.SeenAt.Before(*oldest.SeenAt) || c.SeenAt.Equal(*oldest.SeenAt) && c.PlaceID < oldest.PlaceID {
				oldest = c
			}
		}
		return oldest, true, true
	}
	return Candidate{}, false, false
}

// away is whether a member missed the last two closed places in a row: the
// party stops waiting for them until they answer again.
func away(m Member, rounds []Round, answered func(n, m int) bool) bool {
	missed := 0
	for i := len(rounds) - 1; i >= 0 && missed < 2; i-- {
		r := rounds[i]
		if r.ClosedAt == nil {
			continue
		}
		if r.N < m.FromRound || answered(r.N, m.M) {
			return false
		}
		missed++
	}
	return missed >= 2
}

// lastMove is the latest thing that happened in a game.
func lastMove(s *Snapshot) time.Time {
	at := s.Game.CreatedAt
	if s.Game.StartedAt != nil && s.Game.StartedAt.After(at) {
		at = *s.Game.StartedAt
	}
	for _, m := range s.Members {
		if m.JoinedAt.After(at) {
			at = m.JoinedAt
		}
		if m.LastSeen.After(at) {
			at = m.LastSeen
		}
	}
	for _, r := range s.Rounds {
		if r.StartsAt.After(at) {
			at = r.StartsAt
		}
	}
	for _, g := range s.Guesses {
		if g.AnsweredAt.After(at) {
			at = g.AnsweredAt
		}
	}
	return at
}
