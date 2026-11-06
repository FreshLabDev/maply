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
// have none the bot may see. Face is the photo's address, /face/<Face>.jpg:
// kept with the same photo, a new one for a new photo.
type Avatar struct {
	UniqueID  string
	JPEG      []byte
	Face      string
	CheckedAt time.Time
}

// AvatarOf is the kept photo of userID; ok is false when Telegram was never
// asked.
func (s *Store) AvatarOf(ctx context.Context, userID int64) (Avatar, bool, error) {
	var (
		a        Avatar
		id, face *string
	)
	err := s.pool.QueryRow(ctx, `SELECT file_unique_id, jpeg, face, checked_at FROM avatar WHERE user_id = $1`, userID).Scan(&id, &a.JPEG, &face, &a.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Avatar{}, false, nil
	}
	if err != nil {
		return Avatar{}, false, fmt.Errorf("avatar of %d: %w", userID, err)
	}
	if id != nil {
		a.UniqueID = *id
	}
	if face != nil {
		a.Face = *face
	}
	return a, true, nil
}

// AvatarsOf is the kept photos of ids, leaving out the people without one.
func (s *Store) AvatarsOf(ctx context.Context, ids []int64) (map[int64][]byte, error) {
	out := map[int64][]byte{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id, jpeg FROM avatar WHERE user_id = ANY($1) AND jpeg IS NOT NULL`, ids)
	if err != nil {
		return nil, fmt.Errorf("avatars: %w", err)
	}
	return collectMap(rows, out, "avatars")
}

// FacesOf is the addresses of the kept photos of ids, leaving out the people
// without one.
func (s *Store) FacesOf(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id, face FROM avatar WHERE user_id = ANY($1) AND jpeg IS NOT NULL AND face IS NOT NULL`, ids)
	if err != nil {
		return nil, fmt.Errorf("faces: %w", err)
	}
	return collectMap(rows, out, "faces")
}

// collectMap reads rows of a key and a value into out.
func collectMap[V any](rows pgx.Rows, out map[int64]V, what string) (map[int64]V, error) {
	defer rows.Close()
	for rows.Next() {
		var (
			id int64
			v  V
		)
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		out[id] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

// FaceJPEG is the photo at /face/<face>.jpg; ok is false when no kept photo
// has that address.
func (s *Store) FaceJPEG(ctx context.Context, face string) ([]byte, bool, error) {
	var b []byte
	err := s.pool.QueryRow(ctx, `SELECT jpeg FROM avatar WHERE face = $1 AND jpeg IS NOT NULL`, face).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("face: %w", err)
	}
	return b, true, nil
}

// SetAvatar keeps what Telegram said about userID's photo just now. A photo
// without a Face gets a new one; no photo has none.
func (s *Store) SetAvatar(ctx context.Context, userID int64, a Avatar) error {
	var id *string
	if a.UniqueID != "" {
		id = &a.UniqueID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO avatar (user_id, file_unique_id, jpeg, face, checked_at)
		VALUES ($1, $2, $3, CASE WHEN $3::bytea IS NULL THEN NULL ELSE COALESCE(NULLIF($4, ''), replace(gen_random_uuid()::text, '-', '')) END, $5)
		ON CONFLICT (user_id) DO UPDATE SET file_unique_id = EXCLUDED.file_unique_id, jpeg = EXCLUDED.jpeg, face = EXCLUDED.face, checked_at = EXCLUDED.checked_at`,
		userID, id, a.JPEG, a.Face, a.CheckedAt)
	if err != nil {
		return fmt.Errorf("set avatar of %d: %w", userID, err)
	}
	return nil
}
