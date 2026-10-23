// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/round"
)

// Answered is one of a person's answers for their statistics picture. Km and
// Points are nil until the answer is scored.
type Answered struct {
	Date   round.Date
	DayNo  int
	Lat    float64
	Lon    float64
	Km     *float64
	Points *int
}

// RoundDay is a round's date and number.
type RoundDay struct {
	Date  round.Date
	DayNo int
}

// WorldPlace is a person's place in one day's world ranking among Of.
type WorldPlace struct {
	Date round.Date
	Rank int
	Of   int
}

// Profile is what a person's statistics picture is drawn from.
type Profile struct {
	Name    string
	Answers []Answered // oldest first
	Rounds  []RoundDay // the last rounds up to the date asked, newest first
	World   []WorldPlace
	Streak  int
}

// ProfileOf reads userID's answers, the last n rounds up to today and their
// places in the world rankings of every day; the caller decides which days
// have closed.
func (s *Store) ProfileOf(ctx context.Context, userID int64, today round.Date, n int) (Profile, error) {
	var p Profile
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(first_name, '') FROM core.person WHERE telegram_user_id = $1`, userID).Scan(&p.Name)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, fmt.Errorf("name of %d: %w", userID, err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT g.date, r.day_no, g.lat, g.lon, g.km,
		       CASE WHEN g.pts_km IS NOT NULL AND g.pts_bonus IS NOT NULL
		            THEN g.pts_km + g.pts_bonus + COALESCE(g.pts_speed, 0) END
		  FROM guess g JOIN round r ON r.date = g.date
		 WHERE g.user_id = $1 AND g.date <= $2
		 ORDER BY g.date`, userID, today.Time())
	if err != nil {
		return Profile{}, fmt.Errorf("answers of %d: %w", userID, err)
	}
	p.Answers, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Answered, error) {
		var (
			a  Answered
			at time.Time
		)
		err := row.Scan(&at, &a.DayNo, &a.Lat, &a.Lon, &a.Km, &a.Points)
		a.Date = round.DateIn(at, time.UTC)
		return a, err
	})
	if err != nil {
		return Profile{}, fmt.Errorf("answers of %d: %w", userID, err)
	}
	rows, err = s.pool.Query(ctx, `SELECT date, day_no FROM round WHERE date <= $1 ORDER BY date DESC LIMIT $2`, today.Time(), n)
	if err != nil {
		return Profile{}, fmt.Errorf("rounds to %s: %w", today, err)
	}
	p.Rounds, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (RoundDay, error) {
		var (
			d  RoundDay
			at time.Time
		)
		err := row.Scan(&at, &d.DayNo)
		d.Date = round.DateIn(at, time.UTC)
		return d, err
	})
	if err != nil {
		return Profile{}, fmt.Errorf("rounds to %s: %w", today, err)
	}
	// The same order as the days page: more points, then the earlier answer.
	rows, err = s.pool.Query(ctx, `
		SELECT date, rank, n FROM (
		  SELECT date, user_id,
		         row_number() OVER (PARTITION BY date ORDER BY points DESC, answered_at, km, user_id, player) AS rank,
		         count(*) OVER (PARTITION BY date) AS n
		    FROM day_score WHERE date <= $2) r
		 WHERE user_id = $1
		 ORDER BY date`, userID, today.Time())
	if err != nil {
		return Profile{}, fmt.Errorf("world places of %d: %w", userID, err)
	}
	p.World, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (WorldPlace, error) {
		var (
			w  WorldPlace
			at time.Time
		)
		err := row.Scan(&at, &w.Rank, &w.Of)
		w.Date = round.DateIn(at, time.UTC)
		return w, err
	})
	if err != nil {
		return Profile{}, fmt.Errorf("world places of %d: %w", userID, err)
	}
	streaks, err := s.Streaks(ctx, []int64{userID}, today)
	if err != nil {
		return Profile{}, err
	}
	p.Streak = streaks[userID]
	return p, nil
}

// ProfileCard is the file_id of userID's picture in lang when it was drawn
// for version, else empty.
func (s *Store) ProfileCard(ctx context.Context, userID int64, lang, version string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT file_id FROM profile_card WHERE user_id = $1 AND lang = $2 AND version = $3`,
		userID, lang, version).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("profile card of %d: %w", userID, err)
	}
	return id, nil
}

func (s *Store) SetProfileCard(ctx context.Context, userID int64, lang, version, fileID string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO profile_card (user_id, lang, version, file_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, lang) DO UPDATE SET version = $3, file_id = $4, updated_at = now()`,
		userID, lang, version, fileID); err != nil {
		return fmt.Errorf("profile card of %d: %w", userID, err)
	}
	return nil
}

// RecentPlayers are the Telegram players who answered on or after since, for
// drawing their pictures ahead of time.
func (s *Store) RecentPlayers(ctx context.Context, since round.Date) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT user_id FROM guess WHERE date >= $1 ORDER BY user_id`, since.Time())
	if err != nil {
		return nil, fmt.Errorf("recent players: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
