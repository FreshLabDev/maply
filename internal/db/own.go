// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/round"
)

// Group is a group maply is in, by its title.
type Group struct {
	ChatID int64
	Title  string
}

// GroupsOf is the active groups the person plays in, by title.
func (s *Store) GroupsOf(ctx context.Context, userID int64) ([]Group, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.chat_id, COALESCE(c.title, '')
		  FROM core.maply_presence p
		  JOIN chat_settings cs ON cs.chat_id = p.chat_id
		  LEFT JOIN core.chat c ON c.chat_id = p.chat_id
		 WHERE p.telegram_user_id = $1 AND p.chat_id <> 0 AND cs.active
		 ORDER BY COALESCE(c.title, ''), p.chat_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("groups of %d: %w", userID, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Group, error) {
		var g Group
		err := row.Scan(&g.ChatID, &g.Title)
		return g, err
	})
}

// Scored is one scored day of a person's: how far off and the points.
type Scored struct {
	Km     float64
	Points int
}

// ScoredBefore is the person's scored answers before d, oldest first.
func (s *Store) ScoredBefore(ctx context.Context, userID int64, d round.Date) ([]Scored, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT km, pts_km + pts_bonus + COALESCE(pts_speed, 0)
		  FROM guess
		 WHERE user_id = $1 AND date < $2 AND km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL
		 ORDER BY date`, userID, d.Time())
	if err != nil {
		return nil, fmt.Errorf("scored days of %d: %w", userID, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Scored, error) {
		var sc Scored
		err := row.Scan(&sc.Km, &sc.Points)
		return sc, err
	})
}
