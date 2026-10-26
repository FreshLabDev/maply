// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Avatar is the profile photo maply keeps for a player. JPEG is nil when they
// have none the bot may see.
type Avatar struct {
	UniqueID  string
	JPEG      []byte
	CheckedAt time.Time
}

// AvatarOf is the kept photo of userID; ok is false when Telegram was never
// asked.
func (s *Store) AvatarOf(ctx context.Context, userID int64) (Avatar, bool, error) {
	var (
		a  Avatar
		id *string
	)
	err := s.pool.QueryRow(ctx, `SELECT file_unique_id, jpeg, checked_at FROM avatar WHERE user_id = $1`, userID).Scan(&id, &a.JPEG, &a.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Avatar{}, false, nil
	}
	if err != nil {
		return Avatar{}, false, fmt.Errorf("avatar of %d: %w", userID, err)
	}
	if id != nil {
		a.UniqueID = *id
	}
	return a, true, nil
}

// SetAvatar keeps what Telegram said about userID's photo just now.
func (s *Store) SetAvatar(ctx context.Context, userID int64, a Avatar) error {
	var id *string
	if a.UniqueID != "" {
		id = &a.UniqueID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO avatar (user_id, file_unique_id, jpeg, checked_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET file_unique_id = $2, jpeg = $3, checked_at = $4`,
		userID, id, a.JPEG, a.CheckedAt)
	if err != nil {
		return fmt.Errorf("set avatar of %d: %w", userID, err)
	}
	return nil
}
