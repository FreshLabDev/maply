// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/asterfield-dev/maply/internal/round"
)

// Quiet is where a group stands on the way to a pause.
type Quiet struct {
	// Days is the group's quiet days in a row before the date: days it got
	// the morning post and nobody answered from the group, since its last
	// answer and since quiet_since. A day without a post neither counts nor
	// breaks the run.
	Days int
	// Asked is whether the question is out, sent on AskedOn.
	Asked   bool
	AskedOn round.Date
	// Answered is whether someone answered from the group on or after
	// AskedOn, which takes the question back.
	Answered bool
}

// QuietOf is the group's quiet days up to d, d itself not counted unless
// someone answered on it, which ends the run.
func (s *Store) QuietOf(ctx context.Context, chatID int64, d round.Date) (Quiet, error) {
	var q Quiet
	var asked *time.Time
	err := s.pool.QueryRow(ctx, `
		WITH cs AS (SELECT quiet_since, quiet_asked_on FROM chat_settings WHERE chat_id = $1),
		     last AS (SELECT max(date) AS date FROM guess WHERE chat_id = $1 AND date <= $2)
		SELECT (SELECT count(*) FROM chat_round cr
		         WHERE cr.chat_id = $1 AND cr.post_msg_id IS NOT NULL AND cr.date < $2
		           AND (cs.quiet_since IS NULL OR cr.date >= cs.quiet_since)
		           AND (last.date IS NULL OR cr.date > last.date)),
		       cs.quiet_asked_on,
		       COALESCE(last.date >= cs.quiet_asked_on, false)
		  FROM cs, last`, chatID, d.Time()).Scan(&q.Days, &asked, &q.Answered)
	if errors.Is(err, pgx.ErrNoRows) {
		return Quiet{}, nil
	}
	if err != nil {
		return Quiet{}, fmt.Errorf("quiet of %d: %w", chatID, err)
	}
	if asked != nil {
		q.Asked, q.AskedOn = true, round.DateIn(*asked, time.UTC)
	}
	q.Answered = q.Answered && q.Asked
	return q, nil
}

// AskQuiet records the question sent to the group on d.
func (s *Store) AskQuiet(ctx context.Context, chatID int64, d round.Date, msgID int64) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE chat_settings SET quiet_asked_on = $2, quiet_msg_id = $3, updated_at = now() WHERE chat_id = $1`,
		chatID, d.Time(), msgID); err != nil {
		return fmt.Errorf("ask quiet %d: %w", chatID, err)
	}
	return nil
}

// ClearQuiet takes the question back: someone answered from the group. The
// answer itself ends the run of quiet days.
func (s *Store) ClearQuiet(ctx context.Context, chatID int64) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE chat_settings SET quiet_asked_on = NULL, quiet_msg_id = NULL, updated_at = now() WHERE chat_id = $1`,
		chatID); err != nil {
		return fmt.Errorf("clear quiet %d: %w", chatID, err)
	}
	return nil
}

// KeepPlaying is a press of "Keep playing": the question is answered and
// the quiet days count again from the day after it was asked. ok is false
// when there was no question to answer, or the group is paused.
func (s *Store) KeepPlaying(ctx context.Context, chatID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE chat_settings SET quiet_since = quiet_asked_on + 1, quiet_asked_on = NULL, quiet_msg_id = NULL,
		       updated_at = now()
		 WHERE chat_id = $1 AND quiet_asked_on IS NOT NULL AND paused_at IS NULL`, chatID)
	if err != nil {
		return false, fmt.Errorf("keep playing %d: %w", chatID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// Pause reasons.
const (
	PausedByAdmin   = "admin"
	PausedBySilence = "silence"
)

// PauseGroup stops the scheduler sending to the group. msgID is the question
// that was out, 0 for none; ok is false when the group was paused already.
func (s *Store) PauseGroup(ctx context.Context, chatID int64, by string) (msgID int64, ok bool, err error) {
	var msg *int64
	err = s.pool.QueryRow(ctx, `
		UPDATE chat_settings cs SET paused_at = now(), paused_by = $2, quiet_asked_on = NULL, quiet_msg_id = NULL,
		       updated_at = now()
		  FROM (SELECT chat_id, quiet_msg_id FROM chat_settings WHERE chat_id = $1 FOR UPDATE) was
		 WHERE cs.chat_id = was.chat_id AND cs.kind = 'group' AND cs.paused_at IS NULL
		RETURNING was.quiet_msg_id`, chatID, by).Scan(&msg)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("pause %d: %w", chatID, err)
	}
	return deref(msg), true, nil
}

// ResumeGroup turns a paused group back on at at. Its day starts with the
// first morning post after at, and only days from then count towards the
// next question. ok is false when it was not paused.
func (s *Store) ResumeGroup(ctx context.Context, chatID int64, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE chat_settings SET paused_at = NULL, paused_by = NULL, resumed_at = $2, quiet_since = $3,
		       updated_at = now()
		 WHERE chat_id = $1 AND paused_at IS NOT NULL`, chatID, at, round.DateIn(at, time.UTC).Time())
	if err != nil {
		return false, fmt.Errorf("resume %d: %w", chatID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// Paused reports whether the group is paused.
func (s *Store) Paused(ctx context.Context, chatID int64) (bool, error) {
	var paused bool
	err := s.pool.QueryRow(ctx, `SELECT paused_at IS NOT NULL FROM chat_settings WHERE chat_id = $1`, chatID).Scan(&paused)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("paused %d: %w", chatID, err)
	}
	return paused, nil
}
