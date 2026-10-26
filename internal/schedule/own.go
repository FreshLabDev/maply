// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"html"
	"slices"
	"strconv"
	"strings"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// ownGroups is how many of a person's groups their DM result names.
const ownGroups = 3

// usualFrom is how many scored days before this one a person needs before
// their DM result compares the day with their usual miss.
const usualFrom = 3

// ownLines is what a person's DM result says about their own day, beyond
// what the group's result shows everyone: the points by where they came
// from, how the day compares with the person's others, the streak, and their
// place in each group they play in.
func (s *Scheduler) ownLines(ctx context.Context, lang string, d round.Date, g db.Guess) ([]string, error) {
	lines := []string{i18n.T(lang, "result.your", "dist", i18n.FormatDistance(lang, *g.Km), "points", i18n.FormatNumber(lang, g.Total()))}
	if g.PtsKm != nil && g.PtsBonus != nil {
		lines = append(lines, pointsLine(lang, *g.PtsKm, *g.PtsBonus, g.Speed()))
	}
	past, err := s.Store.ScoredBefore(ctx, g.UserID, d)
	if err != nil {
		return nil, err
	}
	if line := compared(lang, g, past); line != "" {
		lines = append(lines, line)
	}
	streaks, err := s.Store.Streaks(ctx, []int64{g.UserID}, d)
	if err != nil {
		return nil, err
	}
	if n := streaks[g.UserID]; n >= 2 {
		lines = append(lines, i18n.T(lang, "result.streak", "n", strconv.Itoa(n)))
	}
	groups, err := s.Store.GroupsOf(ctx, g.UserID)
	if err != nil {
		return nil, err
	}
	shown := 0
	for _, gr := range groups {
		if shown == ownGroups {
			break
		}
		guesses, err := s.Store.ChatGuesses(ctx, d, gr.ChatID)
		if err != nil {
			return nil, err
		}
		rank, n := 0, 0
		for _, x := range guesses {
			if x.Km == nil {
				continue
			}
			n++
			if x.UserID == g.UserID {
				rank = n
			}
		}
		// A table of one is not a place.
		if rank == 0 || n < 2 {
			continue
		}
		title := strings.TrimSpace(gr.Title)
		if title == "" {
			continue
		}
		if r := []rune(title); len(r) > 40 {
			title = string(r[:40]) + "…"
		}
		lines = append(lines, i18n.T(lang, "result.group", "title", html.EscapeString(title), "rank", strconv.Itoa(rank), "n", strconv.Itoa(n)))
		shown++
	}
	return lines, nil
}

// pointsLine is where the points came from: the distance, the bonus by what
// was right, and the speed.
func pointsLine(lang string, km, bonus, speed int) string {
	num := func(n int) string { return i18n.FormatNumber(lang, n) }
	parts := []string{i18n.T(lang, "result.pts_km", "n", num(km))}
	switch bonus {
	case 0:
	case scoring.CityBonus + scoring.CountryBonus + scoring.ContinentBonus:
		parts = append(parts, i18n.T(lang, "result.hit3", "n", num(bonus)))
	case scoring.CountryBonus + scoring.ContinentBonus:
		parts = append(parts, i18n.T(lang, "result.hit2", "n", num(bonus)))
	case scoring.ContinentBonus:
		parts = append(parts, i18n.T(lang, "result.hit1", "n", num(bonus)))
	default:
		parts = append(parts, i18n.T(lang, "result.hit_other", "n", num(bonus)))
	}
	if speed > 0 {
		parts = append(parts, i18n.T(lang, "result.pts_speed", "n", num(speed)))
	}
	return strings.Join(parts, ", ")
}

// compared is the day against the person's others: a new record, or their
// usual miss once there are enough days to have one.
func compared(lang string, g db.Guess, past []db.Scored) string {
	if len(past) == 0 {
		return ""
	}
	best := 0
	kms := make([]float64, len(past))
	for i, p := range past {
		best = max(best, p.Points)
		kms[i] = p.Km
	}
	if g.Total() > best {
		return i18n.T(lang, "result.record", "points", i18n.FormatNumber(lang, best))
	}
	if len(past) < usualFrom {
		return ""
	}
	slices.Sort(kms)
	median := kms[len(kms)/2]
	if len(kms)%2 == 0 {
		median = (kms[len(kms)/2-1] + kms[len(kms)/2]) / 2
	}
	return i18n.T(lang, "result.usual", "dist", i18n.FormatDistance(lang, median))
}
