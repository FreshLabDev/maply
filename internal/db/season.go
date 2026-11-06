// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/round"
)

// SeasonDays is how many of a month's days make a season: the best ones, so a
// missed day costs nothing until a player has missed more than the month can
// spare.
const SeasonDays = 20

// SeasonEntry is a Telegram player's month.
type SeasonEntry struct {
	UserID int64
	Name   string
	// Points is the sum of the best SeasonDays days; Days how many counted.
	Points, Days int
}

// Season ranks the Telegram players of the days from first to last by the
// sum of their best SeasonDays days, the best limit of them. With a chat, only
// the people maply has seen in it. Site players have no season: their days
// carry no speed bonus and their names are not checked. Equal sums go to the
// one who needed fewer days, then to the better single day.
func (s *Store) Season(ctx context.Context, first, last round.Date, chatID int64, limit int) ([]SeasonEntry, error) {
	rows, err := s.pool.Query(ctx, `
		WITH d AS (
		  SELECT g.user_id, g.points,
		         row_number() OVER (PARTITION BY g.user_id ORDER BY g.points DESC, g.date) AS k
		    FROM day_score g
		   WHERE g.user_id IS NOT NULL AND g.date BETWEEN $1 AND $2
		     AND ($3::bigint = 0 OR EXISTS (SELECT 1 FROM core.maply_presence p
		                                     WHERE p.chat_id = $3 AND p.telegram_user_id = g.user_id)))
		SELECT d.user_id, COALESCE(pe.first_name, ''), sum(d.points)::int, count(*)::int
		  FROM d JOIN person_shown pe ON pe.telegram_user_id = d.user_id
		 WHERE d.k <= $4
		 GROUP BY d.user_id, pe.first_name
		 ORDER BY 3 DESC, 4, max(d.points) DESC, d.user_id
		 LIMIT $5`, first.Time(), last.Time(), chatID, SeasonDays, limit)
	if err != nil {
		return nil, fmt.Errorf("season %s..%s: %w", first, last, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SeasonEntry, error) {
		var e SeasonEntry
		err := row.Scan(&e.UserID, &e.Name, &e.Points, &e.Days)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("season %s..%s: %w", first, last, err)
	}
	return out, nil
}

// SeasonRank is a player's place in a season: zero Rank when they have no
// scored day in it.
type SeasonRank struct {
	SeasonEntry
	Rank, Of int
}

// SeasonOf is userID's place in the season Season ranks, among everyone in
// it.
func (s *Store) SeasonOf(ctx context.Context, first, last round.Date, chatID, userID int64) (SeasonRank, error) {
	var r SeasonRank
	err := s.pool.QueryRow(ctx, `
		WITH d AS (
		  SELECT g.user_id, g.points,
		         row_number() OVER (PARTITION BY g.user_id ORDER BY g.points DESC, g.date) AS k
		    FROM day_score g
		   WHERE g.user_id IS NOT NULL AND g.date BETWEEN $1 AND $2
		     AND ($3::bigint = 0 OR EXISTS (SELECT 1 FROM core.maply_presence p
		                                     WHERE p.chat_id = $3 AND p.telegram_user_id = g.user_id))),
		s AS (
		  SELECT d.user_id, sum(d.points)::int AS points, count(*)::int AS days, max(d.points) AS top
		    FROM d WHERE d.k <= $4 GROUP BY d.user_id),
		r AS (
		  SELECT s.*, row_number() OVER (ORDER BY s.points DESC, s.days, s.top DESC, s.user_id)::int AS rank,
		         count(*) OVER ()::int AS n
		    FROM s)
		SELECT r.points, r.days, r.rank, r.n FROM r WHERE r.user_id = $5`,
		first.Time(), last.Time(), chatID, SeasonDays, userID).Scan(&r.Points, &r.Days, &r.Rank, &r.Of)
	if errors.Is(err, pgx.ErrNoRows) {
		return SeasonRank{SeasonEntry: SeasonEntry{UserID: userID}}, nil
	}
	if err != nil {
		return r, fmt.Errorf("season of %d %s..%s: %w", userID, first, last, err)
	}
	r.UserID = userID
	return r, nil
}
