// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TopBoard is one of the free play's leaderboards: a mode and its settings.
// Seconds is the time a place has, or the whole game's in a timed one;
// Rounds is 0 for a streak and a timed game.
type TopBoard struct {
	Mode    string
	Seconds int
	Rounds  int
}

// FreeWho is a player of free play as the boards and the profile count
// them: a Telegram person, or an account without Telegram. A result is a
// Telegram person's or a site player's, and a site player's counts for its
// account, or for the account's Telegram person once it has one. A guest
// keeps no results.
type FreeWho struct {
	UserID  int64
	Account string
}

// TopRow is a player's best on a board. Equal scores rank by who reached
// theirs first, as the day ranks equal points by who answered first.
type TopRow struct {
	FreeWho
	Name  string
	Score int
	Rank  int
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

// topWith is the WITH list of a board query: the results the boards count,
// each player's best and its rank. A result is a Telegram person's
// (user_id) or a site player's (player); a site player's counts for its
// account, or for the account's Telegram person once it has one, and a
// site player no account holds any more is on no board. Only a game on a
// timer counts: without one a place can be looked up at leisure. An attempt
// at a challenge does not either: its places were played before by the one
// who made it, who can tell. A day from the archive has no timer. Only a
// classic game has a number of places. $1 is the earliest finish, NULL for
// all time; filter narrows the results to one board; mine ranks only the
// boards the viewer is on. The conditions on seconds and mode are those of
// free_result_top_idx, written out so the index applies.
func topWith(filter string, mine bool) string {
	from, own := "results", ""
	if mine {
		own = `,
	mine AS (SELECT DISTINCT mode, seconds, rounds FROM results WHERE ` + topMe("") + `)`
		from = "results JOIN mine USING (mode, seconds, rounds)"
	}
	return `
	results (user_id, account, score, at, mode, seconds, rounds) AS (
	  SELECT COALESCE(r.user_id, a.telegram_user_id),
	         CASE WHEN r.user_id IS NULL AND a.telegram_user_id IS NULL THEN a.id END,
	         CASE r.mode WHEN 'streak' THEN r.streak WHEN 'timed' THEN r.countries ELSE r.points END,
	         r.finished_at, r.mode, r.seconds, CASE r.mode WHEN 'classic' THEN r.rounds ELSE 0 END
	    FROM free_result r
	    LEFT JOIN site_player sp ON sp.id = r.player
	    LEFT JOIN account a ON a.id = sp.account_id
	   WHERE r.seconds IS NOT NULL AND r.mode <> 'archive' AND r.kind <> 'challenge'
	     AND (r.user_id IS NOT NULL OR a.id IS NOT NULL)
	     AND ($1::timestamptz IS NULL OR r.finished_at >= $1) ` + filter + `
	)` + own + `,
	best AS (
	  SELECT DISTINCT ON (mode, seconds, rounds, user_id, account) results.* FROM ` + from + `
	   ORDER BY mode, seconds, rounds, user_id, account, score DESC, at
	),
	ranked AS (
	  SELECT best.*,
	         row_number() OVER (PARTITION BY mode, seconds, rounds ORDER BY score DESC, at, user_id, account) AS rank,
	         count(*) OVER (PARTITION BY mode, seconds, rounds) AS total
	    FROM best
	)`
}

// topMe matches the viewer's rows: $2 a Telegram id, $3 an account.
func topMe(a string) string {
	return "(" + a + "user_id = $2 OR " + a + "account = NULLIF($3, '')::uuid)"
}

func sinceArg(since time.Time) any {
	if since.IsZero() {
		return nil
	}
	return since
}

// Top is a board's first limit rows over the games finished since since
// (zero for all time), with the viewer's row wherever it stands.
func (s *Store) Top(ctx context.Context, b TopBoard, since time.Time, limit int, me FreeWho) (TopTable, error) {
	// A board without places has no rounds condition: the mode says it.
	filter, args := `AND r.mode = $4 AND r.seconds = $5`, []any{sinceArg(since), me.UserID, me.Account, b.Mode, b.Seconds, limit}
	if b.Rounds != 0 {
		filter, args = filter+` AND r.rounds = $7`, append(args, b.Rounds)
	}
	// An account's name is its main site player's, as on the site.
	rows, err := s.pool.Query(ctx, `
		WITH `+topWith(filter, false)+`
		SELECT COALESCE(ranked.user_id, 0), COALESCE(ranked.account::text, ''),
		       COALESCE(p.first_name, mp.name, ''), ranked.score, ranked.rank, ranked.total
		  FROM ranked
		  LEFT JOIN person_shown p ON p.telegram_user_id = ranked.user_id
		  LEFT JOIN account a ON a.id = ranked.account
		  LEFT JOIN site_player mp ON mp.id = a.player
		 WHERE ranked.rank <= $6 OR `+topMe("ranked.")+`
		 ORDER BY ranked.rank`, args...)
	if err != nil {
		return TopTable{}, fmt.Errorf("top %s/%d/%d: %w", b.Mode, b.Seconds, b.Rounds, err)
	}
	var t TopTable
	t.Rows, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (TopRow, error) {
		var r TopRow
		err := row.Scan(&r.UserID, &r.Account, &r.Name, &r.Score, &r.Rank, &t.Total)
		return r, err
	})
	if err != nil {
		return TopTable{}, fmt.Errorf("top %s/%d/%d: %w", b.Mode, b.Seconds, b.Rounds, err)
	}
	return t, nil
}

// TopPlaces are the viewer's places on every board they have a result on,
// over the games finished since since (zero for all time).
func (s *Store) TopPlaces(ctx context.Context, me FreeWho, since time.Time) ([]TopPlace, error) {
	rows, err := s.pool.Query(ctx, `
		WITH `+topWith("", true)+`
		SELECT mode, seconds, rounds, score, rank, total
		  FROM ranked
		 WHERE `+topMe("ranked.")+`
		 ORDER BY CASE mode WHEN 'streak' THEN 0 WHEN 'timed' THEN 1 ELSE 2 END, seconds, rounds`,
		sinceArg(since), me.UserID, me.Account)
	if err != nil {
		return nil, fmt.Errorf("top places: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TopPlace, error) {
		var p TopPlace
		err := row.Scan(&p.Board.Mode, &p.Board.Seconds, &p.Board.Rounds, &p.Score, &p.Rank, &p.Total)
		return p, err
	})
}
