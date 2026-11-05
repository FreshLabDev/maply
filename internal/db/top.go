// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/free"
)

// TopBoard is one of the free play's leaderboards: a mode and its settings.
// Seconds is the time a place has, or the whole game's in a timed one;
// Rounds is 0 for a streak and a timed game.
type TopBoard struct {
	Mode    string
	Seconds int
	Rounds  int
}

// TopRow is a player's best on a board. Equal scores rank by who reached
// theirs first, as the day ranks equal points by who answered first.
type TopRow struct {
	UserID int64
	Player string
	Name   string
	Score  int
	Rank   int
}

type TopTable struct {
	// Rows are the first rows and the viewer's own wherever it is.
	Rows  []TopRow
	Total int
}

// TopPlace is the viewer's place on one board.
type TopPlace struct {
	Board TopBoard
	Score int
	Rank  int
	Total int
}

// topWho is who a free result counts for. Only Telegram people have results
// today. A site account without Telegram will have free_result's own
// column, and this becomes "r.user_id, r.player": the rest reads both.
const topWho = "r.user_id, NULL::uuid"

// topWith is the WITH list of a board query: the results the boards count,
// each player's best and its rank. Only a game on a timer counts: without
// one a place can be looked up at leisure. An attempt at a challenge does
// not either: its places were played before by the one who made it, who
// can tell. A day from the archive has no timer. $1 is the earliest finish,
// NULL for all time; filter narrows the results to one board; mine ranks
// only the boards the viewer is on.
func topWith(filter string, mine bool) string {
	from, own := "results", ""
	if mine {
		own = `,
	mine AS (SELECT DISTINCT mode, seconds, rounds FROM results WHERE ` + topMe("") + `)`
		from = "results JOIN mine USING (mode, seconds, rounds)"
	}
	return `
	results (user_id, player, score, at, mode, seconds, rounds) AS (
	  SELECT ` + topWho + `,
	         CASE r.mode WHEN 'streak' THEN r.streak WHEN 'timed' THEN r.countries ELSE r.points END,
	         r.finished_at, r.mode, r.seconds, COALESCE(r.rounds, 0)
	    FROM free_result r
	   WHERE r.seconds IS NOT NULL AND r.mode IN ('classic', 'streak', 'timed') AND r.kind <> 'challenge'
	     AND ($1::timestamptz IS NULL OR r.finished_at >= $1) ` + filter + `
	)` + own + `,
	best AS (
	  SELECT DISTINCT ON (mode, seconds, rounds, user_id, player) results.* FROM ` + from + `
	   ORDER BY mode, seconds, rounds, user_id, player, score DESC, at
	),
	ranked AS (
	  SELECT best.*,
	         row_number() OVER (PARTITION BY mode, seconds, rounds ORDER BY score DESC, at, user_id, player) AS rank,
	         count(*) OVER (PARTITION BY mode, seconds, rounds) AS total
	    FROM best
	)`
}

// topMe matches the viewer's rows: $2 a Telegram id, $3 a site player.
func topMe(a string) string {
	return "(" + a + "user_id = $2 OR " + a + "player = NULLIF($3, '')::uuid)"
}

func sinceArg(since time.Time) any {
	if since.IsZero() {
		return nil
	}
	return since
}

// Top is a board's first limit rows over the games finished since since
// (zero for all time), with the viewer's row wherever it stands.
func (s *Store) Top(ctx context.Context, b TopBoard, since time.Time, limit int, me free.Who) (TopTable, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('free_result') IS NOT NULL`).Scan(&exists); err != nil {
		return TopTable{}, fmt.Errorf("top: %w", err)
	}
	if !exists {
		return TopTable{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		WITH `+topWith(`AND r.mode = $4 AND r.seconds = $5 AND COALESCE(r.rounds, 0) = $6`, false)+`
		SELECT COALESCE(ranked.user_id, 0), COALESCE(ranked.player::text, ''),
		       COALESCE(p.first_name, sp.name, ''), ranked.score, ranked.rank, ranked.total
		  FROM ranked
		  LEFT JOIN core.person p ON p.telegram_user_id = ranked.user_id
		  LEFT JOIN site_player sp ON sp.id = ranked.player
		 WHERE ranked.rank <= $7 OR `+topMe("ranked.")+`
		 ORDER BY ranked.rank`,
		sinceArg(since), me.UserID, me.Player, b.Mode, b.Seconds, b.Rounds, limit)
	if err != nil {
		return TopTable{}, fmt.Errorf("top %s/%d/%d: %w", b.Mode, b.Seconds, b.Rounds, err)
	}
	var t TopTable
	t.Rows, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (TopRow, error) {
		var r TopRow
		err := row.Scan(&r.UserID, &r.Player, &r.Name, &r.Score, &r.Rank, &t.Total)
		return r, err
	})
	if err != nil {
		return TopTable{}, fmt.Errorf("top %s/%d/%d: %w", b.Mode, b.Seconds, b.Rounds, err)
	}
	return t, nil
}

// TopPlaces are the viewer's places on every board they have a result on,
// over the games finished since since (zero for all time).
func (s *Store) TopPlaces(ctx context.Context, me free.Who, since time.Time) ([]TopPlace, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('free_result') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("top places: %w", err)
	}
	if !exists {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		WITH `+topWith("", true)+`
		SELECT mode, seconds, rounds, score, rank, total
		  FROM ranked
		 WHERE `+topMe("ranked.")+`
		 ORDER BY CASE mode WHEN 'streak' THEN 0 WHEN 'timed' THEN 1 ELSE 2 END, seconds, rounds`,
		sinceArg(since), me.UserID, me.Player)
	if err != nil {
		return nil, fmt.Errorf("top places: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TopPlace, error) {
		var p TopPlace
		err := row.Scan(&p.Board.Mode, &p.Board.Seconds, &p.Board.Rounds, &p.Score, &p.Rank, &p.Total)
		return p, err
	})
}
