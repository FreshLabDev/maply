// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
)

// The world ranking puts everyone who answered a day in one list, from a
// group, a DM or the site; groups are not ranked against each other. A day
// is final only after the last zone's reveal, so a chat hears about the world
// once, the day after: in the morning post when yesterday has closed
// everywhere by then, else in that evening's result.

// groupPlaces is how many of a group's members the look back names.
const groupPlaces = 10

// entryName is how the ranking names someone: the first name, the site's
// own name, or "Guest".
func entryName(lang string, e db.Entry) string {
	if e.Name == "" && e.Site() {
		return html.EscapeString(i18n.T(lang, "app.guest"))
	}
	return displayName(e.Name)
}

// kmText is "242 km" in lang.
func kmText(lang string, km float64) string {
	return i18n.T(lang, "unit.km", "km", i18n.FormatKm(lang, km))
}

// betterPct is the share of the other players someone at rank beat.
func betterPct(rank, n int) int {
	if n <= 1 {
		return 100
	}
	return int(math.Floor(float64(n-rank) * 100 / float64(n-1)))
}

// lookBack is what a chat hears about the world for d, which must be final:
// how many played and who came first, how close the day came, and the
// person's place for a DM or the members' places for a group. Empty when
// nobody was scored.
func (s *Scheduler) lookBack(ctx context.Context, lang string, d round.Date, c db.Chat) (string, error) {
	w, err := s.Store.World(ctx, d)
	if err != nil || w.Players() == 0 {
		return "", err
	}
	n := i18n.FormatNumber(lang, w.Players())
	first := w.Entries[0]
	lines := []string{i18n.T(lang, "world.first", "n", n, "name", entryName(lang, first), "km", kmText(lang, first.Km))}
	if w.MedianKm != nil {
		lines[0] += " " + i18n.T(lang, "world.day", "km", kmText(lang, *w.MedianKm), "pct", strconv.Itoa(w.CountryPct))
	}
	if c.Kind == "dm" {
		if rank := w.RankOf(c.ID); rank > 0 {
			lines = append(lines, i18n.T(lang, "world.you", "rank", strconv.Itoa(rank), "n", n, "pct", strconv.Itoa(betterPct(rank, w.Players()))))
		}
		return strings.Join(lines, "\n"), nil
	}
	guesses, err := s.Store.ChatGuesses(ctx, d, c.ID)
	if err != nil {
		return "", err
	}
	var places []string
	for _, g := range guesses {
		rank := w.RankOf(g.UserID)
		if rank == 0 {
			continue
		}
		if len(places) == groupPlaces {
			places = append(places, "…")
			break
		}
		places = append(places, fmt.Sprintf("%s — %d", displayName(g.FirstName), rank))
	}
	if len(places) > 0 {
		lines = append(lines, i18n.T(lang, "world.group", "places", strings.Join(places, ", ")))
	}
	return strings.Join(lines, "\n"), nil
}

// yesterday is the look back the morning post of d carries: yesterday's,
// when it has closed in every zone. A chat far east posts before that and
// gets it with the result instead.
func (s *Scheduler) yesterday(ctx context.Context, lang string, d round.Date, c db.Chat) (string, error) {
	y := d.AddDays(-1)
	if s.now().Before(s.Zones.LastReveal(y)) {
		return "", nil
	}
	return s.lookBack(ctx, lang, y, c)
}

// lateLookBack is yesterday's look back for a result whose morning post went
// out without it.
func (s *Scheduler) lateLookBack(ctx context.Context, lang string, d round.Date, c db.Chat, cr db.ChatRound) (string, error) {
	if cr.Yesterday != "" {
		return "", nil
	}
	return s.yesterday(ctx, lang, d, c)
}
