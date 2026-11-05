// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
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

// groupPlaces is how many of a group's members the world line names.
const groupPlaces = 10

// A median of two players or "ahead of 0%" says nothing: the day's typical
// miss shows from worldDayFrom players, the share beaten from worldAheadFrom
// and only when it is above zero.
const (
	worldDayFrom   = 5
	worldAheadFrom = 10
)

// entryName is how a post names someone in the ranking: the first name, or
// "Guest" for the site. A site name is anything typed into a browser, so it
// stays on the site and in the Mini App, not in every chat's post.
func entryName(lang string, e db.Entry) string {
	if e.Site() {
		return html.EscapeString(i18n.T(lang, "app.guest"))
	}
	return displayName(e.Name)
}

// kmText is "242 km" in lang, or "40 m" under a kilometre.
func kmText(lang string, km float64) string {
	return i18n.FormatDistance(lang, km)
}

// betterPct is the share of the other players someone at rank beat.
func betterPct(rank, n int) int {
	if n <= 1 {
		return 100
	}
	return int(math.Floor(float64(n-rank) * 100 / float64(n-1)))
}

// lookBack is what a chat hears about the world for d, which must be final:
// who came first, how close the day came, and the person's place for a DM;
// at a month's end, its season too. Empty when nobody was scored.
func (s *Scheduler) lookBack(ctx context.Context, lang string, d round.Date, c db.Chat) (string, error) {
	// The morning post can come before the world job has run: a site answer
	// waits for its deadline to be scored, and would be left out of the day.
	if err := s.scoreClosed(ctx, d); err != nil {
		return "", err
	}
	w, err := s.Store.World(ctx, d)
	if err != nil {
		return "", err
	}
	lines := s.worldLines(lang, w, c)
	season, err := s.seasonLines(ctx, lang, d, c)
	if err != nil {
		return "", err
	}
	if len(lines) > 0 && len(season) > 0 {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, season...), "\n"), nil
}

// worldLines are the day's part of the look back, none when nobody was
// scored.
func (s *Scheduler) worldLines(lang string, w db.World, c db.Chat) []string {
	if w.Players() == 0 {
		return nil
	}
	n := i18n.FormatNumber(lang, w.Players())
	first := w.Entries[0]
	// A heading and one fact a line: in a long caption Telegram folds the
	// end, and a run-on sentence there reads as noise.
	lines := []string{"<b>" + i18n.T(lang, "world.yesterday") + "</b>", "🥇 " + entryName(lang, first) + " · " + kmText(lang, first.Km)}
	if w.MedianKm != nil && w.Players() >= worldDayFrom {
		lines = append(lines, i18n.T(lang, "world.day", "km", kmText(lang, *w.MedianKm), "pct", strconv.Itoa(w.CountryPct)))
	}
	// A group's own places are in its result, which the world job made final.
	if rank := w.RankOf(c.ID); c.Kind == "dm" && rank > 0 {
		pct := betterPct(rank, w.Players())
		if w.Players() >= worldAheadFrom && pct > 0 {
			lines = append(lines, i18n.T(lang, "world.you_ahead", "rank", strconv.Itoa(rank), "n", n, "pct", strconv.Itoa(pct)))
		} else {
			lines = append(lines, i18n.T(lang, "world.you", "rank", strconv.Itoa(rank), "n", n))
		}
	}
	return lines
}

// seasonLines close a month in the look back at its last day, so the
// champion comes inside the morning post of the 1st and not as a fourth
// message: the group's own champion in a group, the world's in a DM with the
// person's place. A season is the sum of the best db.SeasonDays days.
func (s *Scheduler) seasonLines(ctx context.Context, lang string, d round.Date, c db.Chat) ([]string, error) {
	if d.AddDays(1).Time().Day() != 1 {
		return nil, nil
	}
	t := d.Time()
	first := round.NewDate(t.Year(), t.Month(), 1)
	chat, key := int64(0), "season.world"
	if c.Kind == "group" {
		chat, key = c.ID, "season.group"
	}
	top, err := s.Store.Season(ctx, first, d, chat, 1)
	if err != nil || len(top) == 0 {
		return nil, err
	}
	lines := []string{"<b>" + i18n.T(lang, "season.title") + "</b>",
		"🏆 " + i18n.T(lang, key, "name", displayName(top[0].Name), "points", i18n.FormatNumber(lang, top[0].Points))}
	if c.Kind == "dm" {
		me, err := s.Store.SeasonOf(ctx, first, d, 0, c.ID)
		if err != nil {
			return nil, err
		}
		if me.Rank > 0 {
			lines = append(lines, i18n.T(lang, "season.you", "rank", strconv.Itoa(me.Rank), "n", i18n.FormatNumber(lang, me.Of), "points", i18n.FormatNumber(lang, me.Points)))
		}
	}
	return lines, nil
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
	// The post's quote may hold only the request for the right to pin, which
	// is no look back.
	if cr.Yesterday != "" && cr.Yesterday != i18n.T(lang, "group.pin_rights") {
		return "", nil
	}
	return s.yesterday(ctx, lang, d, c)
}

// scoreClosed scores what is left of d once it has closed everywhere.
func (s *Scheduler) scoreClosed(ctx context.Context, d round.Date) error {
	rd, ok, err := s.Store.RoundOn(ctx, d)
	if err != nil || !ok {
		return err
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return err
	}
	return s.scoreDue(ctx, d, answer)
}
