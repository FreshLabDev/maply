// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/round"
)

// DayAnswers is how many answers d has anywhere, scored or not: the raw rows
// of guess and site_guess, late site answers included. It is what tells a day
// nobody answered, so it counts rows and not the ranking, which holds only
// what has been scored.
func (s *Store) DayAnswers(ctx context.Context, d round.Date) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM guess WHERE date = $1)
		     + (SELECT count(*) FROM site_guess WHERE date = $1)`, d.Time()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("answers of %s: %w", d, err)
	}
	return n, nil
}

// SkipDay skips d if nobody answered it: the round stays, with its number and
// its chats' rows, but counts as no round, and its place goes back to the
// queue, not to be drawn before backAfter. It reports whether it skipped; an
// answer that is there by now keeps the day. The check and the mark are one
// statement, so an answer cannot slip in between.
func (s *Store) SkipDay(ctx context.Context, d round.Date, backAfter round.Date) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		WITH skipped AS (
		  UPDATE round SET skipped_at = now()
		   WHERE date = $1 AND skipped_at IS NULL
		     AND NOT EXISTS (SELECT 1 FROM guess WHERE date = $1)
		     AND NOT EXISTS (SELECT 1 FROM site_guess WHERE date = $1)
		  RETURNING place_id)
		UPDATE place p SET status = 'approved', back_after = $2
		  FROM skipped s WHERE p.id = s.place_id`, d.Time(), backAfter.Time())
	if err != nil {
		return false, fmt.Errorf("skip day %s: %w", d, err)
	}
	return tag.RowsAffected() > 0, nil
}

// Unclosed lists the dates up to upTo that have a round which is neither
// kept nor skipped, oldest first.
func (s *Store) Unclosed(ctx context.Context, upTo round.Date) ([]round.Date, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.date FROM round r
		 WHERE r.skipped_at IS NULL AND r.date <= $1
		   AND NOT EXISTS (SELECT 1 FROM day_closed c WHERE c.date = r.date)
		 ORDER BY r.date`, upTo.Time())
	if err != nil {
		return nil, fmt.Errorf("unclosed days: %w", err)
	}
	return dateList(rows)
}

// SkippedDates lists the skipped dates up to upTo, which a streak steps over.
func (s *Store) SkippedDates(ctx context.Context, upTo round.Date) ([]round.Date, error) {
	rows, err := s.pool.Query(ctx, `SELECT date FROM round WHERE skipped_at IS NOT NULL AND date <= $1 ORDER BY date`, upTo.Time())
	if err != nil {
		return nil, fmt.Errorf("skipped days: %w", err)
	}
	return dateList(rows)
}

func dateList(rows pgx.Rows) ([]round.Date, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (round.Date, error) {
		var at time.Time
		err := row.Scan(&at)
		return round.DateIn(at, time.UTC), err
	})
}

// SetHeld marks that a chat's result waits for the day to close: the world
// had no answer at the chat's reveal, so the place stays unseen.
func (s *Store) SetHeld(ctx context.Context, chatID int64, d round.Date) error {
	return s.updateChatRound(ctx, chatID, d, `held = true`)
}

// HeldChats lists the chats whose result of d waits and has not gone out.
func (s *Store) HeldChats(ctx context.Context, d round.Date) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT chat_id FROM chat_round WHERE date = $1 AND held AND result_msg_id IS NULL ORDER BY chat_id`, d.Time())
	if err != nil {
		return nil, fmt.Errorf("held results of %s: %w", d, err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// Post is a chat's morning post of a date.
type Post struct {
	ChatID int64
	MsgID  int64
	Pinned bool
}

// Posts lists the morning posts that went out on d.
func (s *Store) Posts(ctx context.Context, d round.Date) ([]Post, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT chat_id, post_msg_id, pinned FROM chat_round
		 WHERE date = $1 AND post_msg_id IS NOT NULL ORDER BY chat_id`, d.Time())
	if err != nil {
		return nil, fmt.Errorf("posts of %s: %w", d, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Post, error) {
		var p Post
		err := row.Scan(&p.ChatID, &p.MsgID, &p.Pinned)
		return p, err
	})
}
