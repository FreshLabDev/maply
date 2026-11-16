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

// Warmup is a group's warm-up: a day of the archive played from the group's
// welcome, with its own result at ResultAt.
type Warmup struct {
	ChatID      int64
	Day         round.Date
	DayNo       int
	ResultAt    time.Time
	ResultMsgID int64
}

// Open is whether the warm-up still takes answers: its result has not gone
// out and its time has not come.
func (w Warmup) Open(now time.Time) bool { return w.ResultMsgID == 0 && now.Before(w.ResultAt) }

// StartWarmup gives a group its warm-up on the newest day of the archive
// closed by closed: over in every zone, kept, and a panorama, as the archive
// has it. A group gets one only while it has had no morning post and no
// warm-up before; ok is false otherwise, and when no day is there to play.
func (s *Store) StartWarmup(ctx context.Context, chatID int64, closed round.Date, resultAt time.Time) (Warmup, bool, error) {
	var day time.Time
	var dayNo int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO warmup (chat_id, day, result_at)
		SELECT $1, r.date, $3 FROM round r
		  JOIN day_closed dc ON dc.date = r.date
		  JOIN place p ON p.id = r.place_id AND p.kind = 'panorama'
		 WHERE r.skipped_at IS NULL AND dc.date <= $2
		   AND NOT EXISTS (SELECT 1 FROM chat_round cr WHERE cr.chat_id = $1 AND cr.post_msg_id IS NOT NULL)
		 ORDER BY r.date DESC
		 LIMIT 1
		ON CONFLICT (chat_id) DO NOTHING
		RETURNING day, (SELECT day_no FROM round WHERE date = warmup.day)`,
		chatID, closed.Time(), resultAt).Scan(&day, &dayNo)
	if errors.Is(err, pgx.ErrNoRows) {
		return Warmup{}, false, nil
	}
	if err != nil {
		return Warmup{}, false, fmt.Errorf("start warm-up of %d: %w", chatID, err)
	}
	return Warmup{ChatID: chatID, Day: round.DateIn(day, time.UTC), DayNo: dayNo, ResultAt: resultAt}, true, nil
}

// WarmupOf is the group's warm-up, if it has had one.
func (s *Store) WarmupOf(ctx context.Context, chatID int64) (Warmup, bool, error) {
	w := Warmup{ChatID: chatID}
	var day time.Time
	var msg *int64
	err := s.pool.QueryRow(ctx, `
		SELECT w.day, r.day_no, w.result_at, w.result_msg_id
		  FROM warmup w JOIN round r ON r.date = w.day
		 WHERE w.chat_id = $1`, chatID).Scan(&day, &w.DayNo, &w.ResultAt, &msg)
	if errors.Is(err, pgx.ErrNoRows) {
		return Warmup{}, false, nil
	}
	if err != nil {
		return Warmup{}, false, fmt.Errorf("warm-up of %d: %w", chatID, err)
	}
	w.Day = round.DateIn(day, time.UTC)
	if msg != nil {
		w.ResultMsgID = *msg
	}
	return w, true, nil
}

// JoinWarmup counts a game of the archive in the group's warm-up.
func (s *Store) JoinWarmup(ctx context.Context, chatID, game int64) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO warmup_game (chat_id, game) VALUES ($1, $2) ON CONFLICT DO NOTHING`, chatID, game); err != nil {
		return fmt.Errorf("join warm-up of %d: %w", chatID, err)
	}
	return nil
}

// WarmupGuesses are the answers given in the group's warm-up, best first:
// each player's one place of the day, with the points the archive gave it.
// A game given up, or not answered yet, is not in it.
func (s *Store) WarmupGuesses(ctx context.Context, chatID int64) ([]Guess, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(fm.user_id, 0), fm.name, fg.lat, fg.lon, fg.answered_at, fg.km, fg.pts_km, fg.pts_bonus
		  FROM warmup_game wg
		  JOIN free_member fm ON fm.game = wg.game AND fm.m = 1
		  JOIN free_guess fg ON fg.game = wg.game AND fg.n = 1 AND fg.m = 1
		 WHERE wg.chat_id = $1 AND fg.lat IS NOT NULL
		 ORDER BY fg.pts_km + fg.pts_bonus DESC, fg.km, fg.answered_at`, chatID)
	if err != nil {
		return nil, fmt.Errorf("warm-up guesses of %d: %w", chatID, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Guess, error) {
		var g Guess
		var km float64
		var ptsKm, ptsBonus int
		err := row.Scan(&g.UserID, &g.FirstName, &g.Pin.Lat, &g.Pin.Lon, &g.AnsweredAt, &km, &ptsKm, &ptsBonus)
		g.Km, g.PtsKm, g.PtsBonus = &km, &ptsKm, &ptsBonus
		return g, err
	})
}

// SetWarmupResult records the warm-up's result message.
func (s *Store) SetWarmupResult(ctx context.Context, chatID, msgID int64) error {
	if _, err := s.pool.Exec(ctx, `UPDATE warmup SET result_msg_id = $2 WHERE chat_id = $1`, chatID, msgID); err != nil {
		return fmt.Errorf("warm-up result of %d: %w", chatID, err)
	}
	return nil
}
