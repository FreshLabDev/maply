// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"
)

// Stats is a person's record over the days that have a result. A day still
// open is left out: its distance does not exist yet.
type Stats struct {
	Days   int
	Points int
	Best   int
	// MedianKm is nil until there is a scored day.
	MedianKm *float64
}

func (s *Store) Stats(ctx context.Context, userID int64) (Stats, error) {
	var st Stats
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       COALESCE(sum(pts_km + pts_bonus + COALESCE(pts_speed, 0)), 0),
		       COALESCE(max(pts_km + pts_bonus + COALESCE(pts_speed, 0)), 0),
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY km)
		FROM guess
		WHERE user_id = $1 AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL`, userID).
		Scan(&st.Days, &st.Points, &st.Best, &st.MedianKm)
	if err != nil {
		return Stats{}, fmt.Errorf("stats of %d: %w", userID, err)
	}
	return st, nil
}
