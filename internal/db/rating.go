// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/FreshLabDev/maply/internal/round"
	"github.com/jackc/pgx/v5"
)

// RatingDay is one person's day in the rating: a Telegram player's
// (UserID) or an account's without Telegram (Account).
type RatingDay struct {
	Date    round.Date
	UserID  int64
	Account string
	Points  int
	Km      float64
}

// RatingDates are the days over everywhere, newest first: the last n, or
// every one when n is 0. A skipped day is not one of them.
func (s *Store) RatingDates(ctx context.Context, n int) ([]round.Date, error) {
	var limit *int
	if n > 0 {
		limit = &n
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.date FROM day_closed c
		  JOIN round r ON r.date = c.date AND r.skipped_at IS NULL
		 ORDER BY c.date DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("rating dates: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (round.Date, error) {
		var at time.Time
		err := row.Scan(&at)
		return round.DateIn(at, time.UTC), err
	})
}

// RatingDays are the kept days from the date from on, one a person a day,
// for the people with a profile: Telegram players, and accounts without
// Telegram for the site days of every player they took in. A guest of the
// site is not in the rating. A site day of an account with Telegram counts
// for its person as on their profile: on a day without a Telegram answer,
// and only when given before the day's first reveal (site_guess.late).
func (s *Store) RatingDays(ctx context.Context, from round.Date) ([]RatingDay, error) {
	rows, err := s.pool.Query(ctx, `
		WITH site AS (
		  SELECT DISTINCT ON (ac.id, dp.date) dp.date, ac.telegram_user_id AS user_id,
		         CASE WHEN ac.telegram_user_id IS NULL THEN ac.id END AS account, dp.points, dp.km
		    FROM day_place dp
		    JOIN site_player sp ON sp.id = dp.player
		    JOIN account ac ON ac.id = sp.account_id
		    JOIN site_guess g ON g.player = dp.player AND g.date = dp.date
		   WHERE dp.player IS NOT NULL AND dp.date >= $1
		     AND (ac.telegram_user_id IS NULL
		          OR NOT g.late AND NOT EXISTS (SELECT 1 FROM day_place t WHERE t.user_id = ac.telegram_user_id AND t.date = dp.date))
		   ORDER BY ac.id, dp.date, g.answered_at, dp.player)
		SELECT dp.date, dp.user_id, NULL::uuid, dp.points, dp.km
		  FROM day_place dp JOIN round r ON r.date = dp.date AND r.skipped_at IS NULL
		 WHERE dp.user_id IS NOT NULL AND dp.date >= $1
		UNION ALL
		SELECT x.date, x.user_id, x.account, x.points, x.km
		  FROM site x JOIN round r ON r.date = x.date AND r.skipped_at IS NULL`, from.Time())
	if err != nil {
		return nil, fmt.Errorf("rating days: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (RatingDay, error) {
		var (
			d       RatingDay
			at      time.Time
			user    *int64
			account *string
		)
		if err := row.Scan(&at, &user, &account, &d.Points, &d.Km); err != nil {
			return d, err
		}
		d.Date = round.DateIn(at, time.UTC)
		if user != nil {
			d.UserID = *user
		}
		if account != nil {
			d.Account = *account
		}
		return d, nil
	})
}

// RatingNames are the names of the people the rating shows: a Telegram
// player's as maply shows it, an account's site name.
func (s *Store) RatingNames(ctx context.Context, users []int64, accounts []string) (map[int64]string, map[string]string, error) {
	byUser, byAccount := map[int64]string{}, map[string]string{}
	if len(users) > 0 {
		rows, err := s.pool.Query(ctx, `SELECT telegram_user_id, COALESCE(first_name, '') FROM person_shown WHERE telegram_user_id = ANY($1)`, users)
		if err != nil {
			return nil, nil, fmt.Errorf("rating names: %w", err)
		}
		var (
			id   int64
			name string
		)
		if _, err := pgx.ForEachRow(rows, []any{&id, &name}, func() error { byUser[id] = name; return nil }); err != nil {
			return nil, nil, fmt.Errorf("rating names: %w", err)
		}
	}
	if len(accounts) > 0 {
		rows, err := s.pool.Query(ctx, `
			SELECT a.id::text, COALESCE(p.name, '') FROM account a LEFT JOIN site_player p ON p.id = a.player
			 WHERE a.id = ANY($1::uuid[])`, accounts)
		if err != nil {
			return nil, nil, fmt.Errorf("rating names: %w", err)
		}
		var id, name string
		if _, err := pgx.ForEachRow(rows, []any{&id, &name}, func() error { byAccount[id] = name; return nil }); err != nil {
			return nil, nil, fmt.Errorf("rating names: %w", err)
		}
	}
	return byUser, byAccount, nil
}

// GroupPeople are the Telegram players core has seen with maply in the
// group chatID, or in any group viewer is in when chatID is 0; viewer is
// one of them.
func (s *Store) GroupPeople(ctx context.Context, viewer, chatID int64) ([]int64, error) {
	if viewer == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT o.telegram_user_id
		  FROM core.maply_presence me
		  JOIN core.maply_presence o ON o.chat_id = me.chat_id
		 WHERE me.telegram_user_id = $1 AND me.chat_id <> 0 AND ($2::bigint = 0 OR me.chat_id = $2)`, viewer, chatID)
	if err != nil {
		return nil, fmt.Errorf("group people of %d: %w", viewer, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("group people of %d: %w", viewer, err)
	}
	return append(out, viewer), nil
}

// PlayedWith are the people who have played free play with who: in a party
// or a duel, or at the same challenge, its maker included. A site player
// counts as on the boards: for its account's Telegram person, or for the
// account without Telegram; a guest is no one. who is among them.
func (s *Store) PlayedWith(ctx context.Context, who FreeWho) ([]int64, []string, error) {
	if who == (FreeWho{}) {
		return nil, nil, nil
	}
	var account any
	if who.Account != "" {
		account = who.Account
	}
	// A unit is something played together: a party or duel game, or a
	// challenge with its maker's game and every attempt at it.
	rows, err := s.pool.Query(ctx, `
		WITH who AS (
		  SELECT 'g' || m.game AS unit, m.user_id, m.player
		    FROM free_member m JOIN free_game g ON g.id = m.game WHERE g.kind <> 'solo'
		  UNION ALL
		  SELECT 'c' || c.id, m.user_id, m.player FROM free_challenge c JOIN free_member m ON m.game = c.game
		  UNION ALL
		  SELECT 'c' || a.challenge, a.user_id, a.player FROM free_attempt a),
		people AS (
		  SELECT w.unit, COALESCE(w.user_id, ac.telegram_user_id) AS user_id,
		         CASE WHEN w.user_id IS NULL AND ac.telegram_user_id IS NULL THEN ac.id END AS account
		    FROM who w
		    LEFT JOIN site_player sp ON sp.id = w.player
		    LEFT JOIN account ac ON ac.id = sp.account_id),
		mine AS (SELECT DISTINCT unit FROM people WHERE user_id = $1 OR account = $2::uuid)
		SELECT DISTINCT p.user_id, p.account::text
		  FROM people p JOIN mine USING (unit)
		 WHERE p.user_id IS NOT NULL OR p.account IS NOT NULL`, who.UserID, account)
	if err != nil {
		return nil, nil, fmt.Errorf("played with: %w", err)
	}
	var (
		users    []int64
		accounts []string
		user     *int64
		acc      *string
		self     bool
	)
	if _, err := pgx.ForEachRow(rows, []any{&user, &acc}, func() error {
		switch {
		case user != nil:
			users = append(users, *user)
			self = self || *user == who.UserID
		case acc != nil:
			accounts = append(accounts, *acc)
			self = self || *acc == who.Account
		}
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("played with: %w", err)
	}
	if !self {
		if who.UserID != 0 {
			users = append(users, who.UserID)
		} else {
			accounts = append(accounts, who.Account)
		}
	}
	return users, accounts, nil
}
