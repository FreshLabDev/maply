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

// ErrNoPlace means the queue has no approved place left for a new round.
// Only the days' pool is the queue: a place of the free game is never a
// day's place, and the free game never sees a future day's.
var ErrNoPlace = errors.New("no approved place in the queue")

// ErrDaySkipped means the date's round was skipped: nobody answered it, its
// place went back to the queue, and no new place is drawn for it. A past date
// can be asked for through a link's payload.
var ErrDaySkipped = errors.New("the day was skipped")

// ReturnDays is how long a place that came back from a skipped day waits
// before it can be drawn again.
const ReturnDays = 30

type Round struct {
	Date    round.Date
	DayNo   int
	PlaceID int64
}

// roundLockKey serializes round creation, so two processes asking for the same
// new date cannot spend two places on it.
const roundLockKey int64 = 0x6d61706c7952 // "maplyR"

// EnsureRound returns the round for d, creating it from the place queue the
// first time anyone asks. The place is the approved one whose difficulty is
// closest to the weekday's. Among those the order is a fixed shuffle, so a new
// batch mixes with the places left from the old ones instead of queueing
// behind them. A place that came back from a skipped day is not drawn before
// its back_after, and a skipped date gets no place at all (ErrDaySkipped).
func (s *Store) EnsureRound(ctx context.Context, d round.Date) (Round, error) {
	if r, ok, err := s.liveRound(ctx, s.pool, d); err != nil || ok {
		return r, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Round{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, roundLockKey); err != nil {
		return Round{}, fmt.Errorf("lock round creation: %w", err)
	}
	if r, ok, err := s.liveRound(ctx, tx, d); err != nil || ok {
		return r, err
	}

	dayNo := 1
	var first *time.Time
	if err := tx.QueryRow(ctx, `SELECT min(date) FROM round`).Scan(&first); err != nil {
		return Round{}, fmt.Errorf("first round: %w", err)
	}
	if first != nil {
		firstDate := round.DateIn(*first, time.UTC)
		if d.Before(firstDate) {
			return Round{}, fmt.Errorf("round for %s: before the first round %s", d, firstDate)
		}
		dayNo = round.DayNo(firstDate, d)
	}

	var placeID int64
	err = tx.QueryRow(ctx, `
		SELECT id FROM place
		 WHERE status = 'approved' AND pool = 'daily' AND kind = 'panorama'
		   AND (back_after IS NULL OR back_after <= $2::date)
		 ORDER BY abs(difficulty - $1), md5(id::text), id
		 LIMIT 1
		 FOR UPDATE`, round.TargetDifficulty(d.Weekday()), d.Time()).Scan(&placeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Round{}, ErrNoPlace
	}
	if err != nil {
		return Round{}, fmt.Errorf("pick place: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE place SET status = 'used' WHERE id = $1`, placeID); err != nil {
		return Round{}, fmt.Errorf("use place %d: %w", placeID, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO round (date, day_no, place_id) VALUES ($1, $2, $3)`, d.Time(), dayNo, placeID); err != nil {
		return Round{}, fmt.Errorf("insert round %s: %w", d, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Round{}, err
	}
	return Round{Date: d, DayNo: dayNo, PlaceID: placeID}, nil
}

// RoundOn returns the round for d if it exists, without creating it. A
// skipped round is no round.
func (s *Store) RoundOn(ctx context.Context, d round.Date) (Round, bool, error) {
	r, state, err := s.roundState(ctx, s.pool, d)
	return r, state == roundLive, err
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type roundStatus int

const (
	roundNone roundStatus = iota
	roundLive
	roundSkipped
)

func (s *Store) roundState(ctx context.Context, q querier, d round.Date) (Round, roundStatus, error) {
	r := Round{Date: d}
	var skipped bool
	err := q.QueryRow(ctx, `SELECT day_no, place_id, skipped_at IS NOT NULL FROM round WHERE date = $1`, d.Time()).
		Scan(&r.DayNo, &r.PlaceID, &skipped)
	if errors.Is(err, pgx.ErrNoRows) {
		return Round{}, roundNone, nil
	}
	if err != nil {
		return Round{}, roundNone, fmt.Errorf("round %s: %w", d, err)
	}
	if skipped {
		return Round{}, roundSkipped, nil
	}
	return r, roundLive, nil
}

// liveRound is the round for d, or ErrDaySkipped when it was skipped.
func (s *Store) liveRound(ctx context.Context, q querier, d round.Date) (Round, bool, error) {
	r, state, err := s.roundState(ctx, q, d)
	if err == nil && state == roundSkipped {
		return Round{}, false, ErrDaySkipped
	}
	return r, state == roundLive, err
}

// Context is a chat a person plays in: 0 for the DM, a negative id for a
// group. Zone is the chat's own choice and may be empty. Lang is what its zone
// defaults to: for a group the group's language in core, never a member's, so
// every member of one group gets the same reveal; for the DM the person's.
type Context struct {
	ChatID int64
	Zone   string
	Lang   string
}

// Contexts lists every chat core has seen the person in with maply, minus
// chats maply has left.
func (s *Store) Contexts(ctx context.Context, userID int64) ([]Context, error) {
	all, err := s.contexts(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	out := make([]Context, len(all))
	for i, c := range all {
		out[i] = c.Context
	}
	return out, nil
}

// leftContext is a context with when maply left it, nil while it is there.
type leftContext struct {
	Context
	left *time.Time
}

// contexts lists the person's chats: those maply is in, and with withLeft
// also those it has left.
func (s *Store) contexts(ctx context.Context, userID int64, withLeft bool) ([]leftContext, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.chat_id,
		       COALESCE(cs.timezone, ''),
		       COALESCE(CASE WHEN p.chat_id = 0 THEN core.effective_language($1, NULL, 'user')
		                     ELSE core.effective_language(0, p.chat_id, 'chat') END,
		                ''),
		       CASE WHEN COALESCE(cs.active, true) THEN NULL ELSE COALESCE(cs.left_at, cs.updated_at) END
		  FROM core.maply_presence p
		  LEFT JOIN chat_settings cs
		         ON cs.chat_id = CASE WHEN p.chat_id = 0 THEN $1 ELSE p.chat_id END
		 WHERE p.telegram_user_id = $1
		   AND (COALESCE(cs.active, true) OR $2)
		 ORDER BY p.chat_id`, userID, withLeft)
	if err != nil {
		return nil, fmt.Errorf("contexts of %d: %w", userID, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (leftContext, error) {
		var c leftContext
		err := row.Scan(&c.ChatID, &c.Zone, &c.Lang, &c.left)
		return c, err
	})
}

// Deadline is when userID's answer window for d closes: the earliest reveal
// among own (the chat answering now, which core may not have recorded yet;
// nil for none), the chats the person plays in, and the chats maply left
// after their reveal of d, whose result the person may have seen before it
// went. With none of these it is the default zone's reveal.
func (s *Store) Deadline(ctx context.Context, zones round.Zones, userID int64, d round.Date, own *time.Location) (time.Time, error) {
	contexts, err := s.contexts(ctx, userID, true)
	if err != nil {
		return time.Time{}, err
	}
	locs := make([]*time.Location, 0, len(contexts)+1)
	if own != nil {
		locs = append(locs, own)
	}
	for _, c := range contexts {
		loc := zones.For(c.Zone, c.Lang)
		if c.left != nil && c.left.Before(d.Reveal(loc)) {
			continue
		}
		locs = append(locs, loc)
	}
	if len(locs) == 0 {
		locs = append(locs, zones.For("", ""))
	}
	return round.Deadline(d, locs)
}
