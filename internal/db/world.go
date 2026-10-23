// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// Entry is one scored answer in a day's world ranking. A Telegram player has
// a UserID; a site player has a Player id and no UserID.
type Entry struct {
	Rank   int
	UserID int64
	Player string
	Name   string
	Km     float64
	Points int
}

// Site reports whether the answer came from the site.
func (e Entry) Site() bool { return e.Player != "" }

// World is a day's ranking over the answers scored so far. Until the last
// zone's reveal it is not final: later zones are still answering, and Pending
// counts the answers already in that wait for their deadline.
type World struct {
	Entries []Entry // best first
	Pending int
	// MedianKm is nil while nothing is scored.
	MedianKm *float64
	// CountryPct is the share of scored answers inside the right country,
	// which is a ladder bonus of 800 or more.
	CountryPct int
}

// Players is how many answers the ranking holds.
func (w World) Players() int { return len(w.Entries) }

// RankOf is the place of a Telegram player, 0 when they are not ranked.
func (w World) RankOf(userID int64) int {
	for _, e := range w.Entries {
		if e.UserID == userID {
			return e.Rank
		}
	}
	return 0
}

// PlaceOf is the place of a site player, 0 when they are not ranked.
func (w World) PlaceOf(player string) int {
	for _, e := range w.Entries {
		if e.Player == player {
			return e.Rank
		}
	}
	return 0
}

// A day's ranking: more points first, then the earlier answer, then the
// closer pin; the ids only make a full tie come out the same every time.

// World ranks every scored answer of d. The whole day comes back: at the
// size maply is now that is a few hundred rows, and every caller needs some
// of them by person.
func (s *Store) World(ctx context.Context, d round.Date) (World, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT row_number() OVER (ORDER BY ds.points DESC, ds.answered_at, ds.km, ds.user_id, ds.player),
		       COALESCE(ds.user_id, 0), COALESCE(ds.player::text, ''),
		       COALESCE(pe.first_name, sp.name, ''), ds.km, ds.points
		  FROM day_score ds
		  LEFT JOIN core.person pe ON pe.telegram_user_id = ds.user_id
		  LEFT JOIN site_player sp ON sp.id = ds.player
		 WHERE ds.date = $1
		 ORDER BY 1`, d.Time())
	if err != nil {
		return World{}, fmt.Errorf("world %s: %w", d, err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) {
		var e Entry
		err := row.Scan(&e.Rank, &e.UserID, &e.Player, &e.Name, &e.Km, &e.Points)
		return e, err
	})
	if err != nil {
		return World{}, fmt.Errorf("world %s: %w", d, err)
	}
	w := World{Entries: entries}
	err = s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM guess WHERE date = $1 AND km IS NULL)
		     + (SELECT count(*) FROM site_guess WHERE date = $1 AND km IS NULL),
		       (SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY km) FROM day_score WHERE date = $1),
		       (SELECT COALESCE(round(100.0 * count(*) FILTER (WHERE pts_bonus >= 800) / NULLIF(count(*), 0)), 0)::int
		          FROM day_score WHERE date = $1)`, d.Time()).
		Scan(&w.Pending, &w.MedianKm, &w.CountryPct)
	if err != nil {
		return World{}, fmt.Errorf("world %s totals: %w", d, err)
	}
	return w, nil
}

// Day is one date's numbers for the Days tab: the world's, and a group's or
// a person's when asked for.
type Day struct {
	Date     round.Date
	DayNo    int
	Players  int
	Pending  int
	MedianKm *float64
	BestKm   *float64
	// CountryPct is the share of scored answers inside the right country.
	CountryPct int
}

// Days are the world's numbers for the n dates up to and including last,
// newest first. A date without a round is left out.
func (s *Store) Days(ctx context.Context, last round.Date, n int) ([]Day, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.date, r.day_no,
		       count(ds.km),
		       (SELECT count(*) FROM guess g WHERE g.date = r.date AND g.km IS NULL)
		     + (SELECT count(*) FROM site_guess sg WHERE sg.date = r.date AND sg.km IS NULL),
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY ds.km),
		       min(ds.km),
		       COALESCE(round(100.0 * count(*) FILTER (WHERE ds.pts_bonus >= 800) / NULLIF(count(ds.km), 0)), 0)::int
		  FROM round r
		  LEFT JOIN day_score ds ON ds.date = r.date
		 WHERE r.date <= $1 AND r.date > $1::date - $2::int
		 GROUP BY r.date, r.day_no
		 ORDER BY r.date DESC`, last.Time(), n)
	if err != nil {
		return nil, fmt.Errorf("days to %s: %w", last, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Day, error) {
		var (
			x  Day
			at time.Time
		)
		err := row.Scan(&at, &x.DayNo, &x.Players, &x.Pending, &x.MedianKm, &x.BestKm, &x.CountryPct)
		x.Date = round.DateIn(at, time.UTC)
		return x, err
	})
}

// ChatDay is a group's part of a date: how many of the people core has seen
// there with maply answered, and the best of them by points.
type ChatDay struct {
	Answered int
	Members  int
	BestName string
	BestKm   *float64
}

// ChatDays are the group's numbers for the dates in days, keyed by date.
func (s *Store) ChatDays(ctx context.Context, chatID int64, days []Day) (map[round.Date]ChatDay, error) {
	out := map[round.Date]ChatDay{}
	if len(days) == 0 {
		return out, nil
	}
	dates := make([]time.Time, len(days))
	for i, d := range days {
		dates[i] = d.Date.Time()
	}
	rows, err := s.pool.Query(ctx, `
		WITH m AS (SELECT telegram_user_id AS id FROM core.maply_presence WHERE chat_id = $1)
		SELECT d.date,
		       (SELECT count(*) FROM m),
		       count(g.user_id),
		       (array_agg(COALESCE(pe.first_name, '') ORDER BY ds.points DESC, ds.answered_at, ds.km, ds.user_id)
		          FILTER (WHERE ds.points IS NOT NULL))[1],
		       (array_agg(ds.km ORDER BY ds.points DESC, ds.answered_at, ds.km, ds.user_id)
		          FILTER (WHERE ds.points IS NOT NULL))[1]
		  FROM unnest($2::date[]) AS d(date)
		  LEFT JOIN guess g ON g.date = d.date AND g.user_id IN (SELECT id FROM m)
		  LEFT JOIN day_score ds ON ds.date = g.date AND ds.user_id = g.user_id
		  LEFT JOIN core.person pe ON pe.telegram_user_id = g.user_id
		 GROUP BY d.date`, chatID, dates)
	if err != nil {
		return nil, fmt.Errorf("chat days %d: %w", chatID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			at   time.Time
			x    ChatDay
			best *string
		)
		if err := rows.Scan(&at, &x.Members, &x.Answered, &best, &x.BestKm); err != nil {
			return nil, fmt.Errorf("chat days %d: %w", chatID, err)
		}
		if best != nil {
			x.BestName = *best
		}
		out[round.DateIn(at, time.UTC)] = x
	}
	return out, rows.Err()
}

// Mine is a person's own line on a date: their score and place in the world.
type Mine struct {
	Km     float64
	Points int
	Rank   int
}

// MyDays are a Telegram player's scored answers on the dates in days, with
// the place each had in that day's ranking.
func (s *Store) MyDays(ctx context.Context, userID int64, days []Day) (map[round.Date]Mine, error) {
	return s.myDays(ctx, "user_id = $1", userID, days)
}

// SiteDays is MyDays for a site player.
func (s *Store) SiteDays(ctx context.Context, player string, days []Day) (map[round.Date]Mine, error) {
	return s.myDays(ctx, "player = $1::uuid", player, days)
}

func (s *Store) myDays(ctx context.Context, who string, id any, days []Day) (map[round.Date]Mine, error) {
	out := map[round.Date]Mine{}
	if len(days) == 0 {
		return out, nil
	}
	dates := make([]time.Time, len(days))
	for i, d := range days {
		dates[i] = d.Date.Time()
	}
	rows, err := s.pool.Query(ctx, `
		SELECT date, km, points, rank FROM (
		  SELECT date, user_id, player, km, points,
		         row_number() OVER (PARTITION BY date ORDER BY points DESC, answered_at, km, user_id, player) AS rank
		    FROM day_score WHERE date = ANY($2::date[])) r
		 WHERE `+who, id, dates)
	if err != nil {
		return nil, fmt.Errorf("my days: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			at time.Time
			m  Mine
		)
		if err := rows.Scan(&at, &m.Km, &m.Points, &m.Rank); err != nil {
			return nil, fmt.Errorf("my days: %w", err)
		}
		out[round.DateIn(at, time.UTC)] = m
	}
	return out, rows.Err()
}

// NameLimit is the longest name a site player may give.
const NameLimit = 24

// CleanName is the name as the ranking shows it: trimmed, without control
// characters, at most NameLimit characters. Empty means "Guest".
func CleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > NameLimit {
		name = strings.TrimSpace(string(r[:NameLimit]))
	}
	return name
}

// SitePlayer makes sure the browser's id exists. A name sets the player's
// name, a blank one clears it back to "Guest", and nil leaves it as it is.
func (s *Store) SitePlayer(ctx context.Context, id string, name *string) error {
	var n any
	if name != nil {
		if clean := CleanName(*name); clean != "" {
			n = clean
		}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO site_player (id, name) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET name = CASE WHEN $3 THEN EXCLUDED.name ELSE site_player.name END`, id, n, name != nil)
	if err != nil {
		return fmt.Errorf("site player: %w", err)
	}
	return nil
}

// SiteName is the name a site player gave, empty when none.
func (s *Store) SiteName(ctx context.Context, id string) (string, error) {
	var name *string
	err := s.pool.QueryRow(ctx, `SELECT name FROM site_player WHERE id = $1`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) || name == nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("site name: %w", err)
	}
	return *name, nil
}

type NewSiteGuess struct {
	Date       round.Date
	Player     string
	Pin        geo.Point
	AnsweredAt time.Time
	Deadline   time.Time
}

// InsertSiteGuess stores the site player's one answer of the day.
func (s *Store) InsertSiteGuess(ctx context.Context, g NewSiteGuess) error {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO site_guess (date, player, lat, lon, answered_at, deadline_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (date, player) DO NOTHING`,
		g.Date.Time(), g.Player, g.Pin.Lat, g.Pin.Lon, g.AnsweredAt, g.Deadline)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return fmt.Errorf("site guess for an unknown player or date: %w", err)
	}
	if err != nil {
		return fmt.Errorf("insert site guess: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyAnswered
	}
	return nil
}

// SiteGuess is a site player's answer of d with its score once scored.
type SiteGuess struct {
	Pin      geo.Point
	Km       *float64
	PtsKm    *int
	PtsBonus *int
}

func (g SiteGuess) Total() int {
	if g.PtsKm == nil || g.PtsBonus == nil {
		return 0
	}
	return *g.PtsKm + *g.PtsBonus
}

func (s *Store) SiteGuessOf(ctx context.Context, d round.Date, player string) (SiteGuess, bool, error) {
	var g SiteGuess
	err := s.pool.QueryRow(ctx, `
		SELECT lat, lon, km, pts_km, pts_bonus FROM site_guess WHERE date = $1 AND player = $2`, d.Time(), player).
		Scan(&g.Pin.Lat, &g.Pin.Lon, &g.Km, &g.PtsKm, &g.PtsBonus)
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteGuess{}, false, nil
	}
	if err != nil {
		return SiteGuess{}, false, fmt.Errorf("site guess: %w", err)
	}
	return g, true, nil
}

// scoreSite scores the site answers of d whose deadline has passed.
func (s *Store) scoreSite(ctx context.Context, d round.Date, now time.Time, judge func(geo.Point) scoring.Score) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT player::text, lat, lon FROM site_guess
		 WHERE date = $1 AND km IS NULL AND deadline_at <= $2`, d.Time(), now)
	if err != nil {
		return 0, fmt.Errorf("due site guesses: %w", err)
	}
	type due struct {
		player string
		pin    geo.Point
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
		var x due
		err := row.Scan(&x.player, &x.pin.Lat, &x.pin.Lon)
		return x, err
	})
	if err != nil {
		return 0, err
	}
	for _, x := range list {
		sc := judge(x.pin)
		if _, err := s.pool.Exec(ctx, `
			UPDATE site_guess SET km = $3, pts_km = $4, pts_bonus = $5
			 WHERE date = $1 AND player = $2 AND km IS NULL`,
			d.Time(), x.player, sc.Km, sc.Distance, sc.Bonus); err != nil {
			return 0, fmt.Errorf("score site guess: %w", err)
		}
	}
	return len(list), nil
}
