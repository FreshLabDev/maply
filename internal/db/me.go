// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/round"
)

// Who is whose profile it is: a Telegram person, a site account without
// Telegram, or a site player known only by the cookie. Exactly one is set.
//
// An account's site rows come from every site player it has taken in, and
// a Telegram person's include their account's on the days they have no
// Telegram answer: the guest's history comes along with a sign-in, and
// closed days are never rewritten into Telegram rows. Two browsers merged
// after a day closed may leave two site rows of it; the earliest counts, as
// the account's own answer of an open day does.
type Who struct {
	UserID  int64
	Account string
	Player  string
}

func (w Who) arg() any {
	switch {
	case w.UserID != 0:
		return w.UserID
	case w.Account != "":
		return w.Account
	}
	return w.Player
}

// siteRows is the condition on who's own rows of site_guess under alias a,
// on days up to closed, the placeholder of the latest day over everywhere.
//
// A Telegram person's site rows are the guests their account took in, and
// they count only on a day over everywhere and only when given before the
// day's first reveal. Before that the person may still answer the day in a
// chat whose reveal is ahead, and the row's place would tell them where to
// pin; a late row was given while the place was out in a chat they may
// have seen. Both stay off every profile of theirs, their own too.
func (w Who) siteRows(a, closed string) string {
	upTo := a + "date <= " + closed
	switch {
	case w.UserID != 0:
		return a + `player IN (SELECT sp.id FROM site_player sp JOIN account ac ON ac.id = sp.account_id
		                        WHERE ac.telegram_user_id = $1)
		   AND NOT ` + a + `late AND ` + upTo + `
		   AND NOT EXISTS (SELECT 1 FROM guess t
		                    WHERE t.user_id = $1 AND t.date = ` + a + `date AND ` + scored("t.") + `)`
	case w.Account != "":
		return a + "player IN (SELECT id FROM site_player WHERE account_id = $1::uuid) AND " + upTo
	}
	return a + "player = $1::uuid AND " + upTo
}

// scored is the condition on a guess or site_guess row that has its score.
func scored(a string) string {
	return a + "km IS NOT NULL AND " + a + "pts_km IS NOT NULL AND " + a + "pts_bonus IS NOT NULL"
}

// PlayedDay is one scored answer with what the profile shows of it.
type PlayedDay struct {
	Date  round.Date
	DayNo int
	Lat   float64
	Lon   float64
	Km    float64
	// Points has the speed bonus in it; Base does not. The site has no speed
	// bonus, so days are compared with everyone on Base.
	Points int
	Base   int
	// Bonus is the ladder: 300 the continent, 800 the country, 1500 the city.
	Bonus     int
	Continent string
	Country   string
	// City is the place's name by language.
	City map[string]string
	// Rank among Of is the day's final world place, zero until the day is
	// kept as over everywhere.
	Rank int
	Of   int
}

// PlayedDays are who's scored answers on dates up to upTo, oldest first, and
// their site rows only up to closed, the latest day over everywhere (see
// siteRows). The caller decides upTo from what the viewer may see: a day
// still open somewhere is not anyone else's to read.
func (s *Store) PlayedDays(ctx context.Context, who Who, upTo, closed round.Date) ([]PlayedDay, error) {
	days := `(SELECT DISTINCT ON (g.date) g.date, g.lat, g.lon, g.km, g.pts_km + g.pts_bonus AS points, 0 AS speed,
	                 g.pts_bonus, NULL::bigint AS user_id, g.player
	            FROM site_guess g
	           WHERE ` + who.siteRows("g.", "$3") + ` AND g.date <= $2 AND ` + scored("g.") + `
	           ORDER BY g.date, g.answered_at, g.player)`
	if who.UserID != 0 {
		days = `SELECT date, lat, lon, km, pts_km + pts_bonus + COALESCE(pts_speed, 0) AS points, COALESCE(pts_speed, 0) AS speed,
		               pts_bonus, user_id, NULL::uuid AS player
		          FROM guess
		         WHERE user_id = $1 AND date <= $2 AND ` + scored("") + `
		        UNION ALL ` + days
	}
	q := `
		SELECT d.date, r.day_no, d.lat, d.lon, d.km, d.points, d.points - d.speed, d.pts_bonus,
		       COALESCE(p.continent, ''), COALESCE(p.country_iso, ''), p.city,
		       COALESCE(dt.rank, ds.rank, 0), COALESCE(dt.n, ds.n, 0)
		  FROM (` + days + `) d
		  JOIN round r ON r.date = d.date
		  JOIN place p ON p.id = r.place_id
		  LEFT JOIN day_place dt ON dt.user_id = d.user_id AND dt.date = d.date
		  LEFT JOIN day_place ds ON ds.player = d.player AND ds.date = d.date
		 ORDER BY d.date`
	arg := who.arg()
	rows, err := s.pool.Query(ctx, q, arg, upTo.Time(), closed.Time())
	if err != nil {
		return nil, fmt.Errorf("played days: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (PlayedDay, error) {
		var (
			d    PlayedDay
			at   time.Time
			city []byte
		)
		if err := row.Scan(&at, &d.DayNo, &d.Lat, &d.Lon, &d.Km, &d.Points, &d.Base, &d.Bonus,
			&d.Continent, &d.Country, &city, &d.Rank, &d.Of); err != nil {
			return d, err
		}
		d.Date = round.DateIn(at, time.UTC)
		if len(city) > 0 {
			if err := json.Unmarshal(city, &d.City); err != nil {
				return d, fmt.Errorf("city of %s: %w", d.Date, err)
			}
		}
		return d, nil
	})
}

// Standing is how one of a player's days compares with everyone's that day:
// how many scored below them and how many the same, the player left out,
// among N answers in all.
type Standing struct {
	Date  round.Date
	Below int
	Ties  int
	N     int
}

// Standings compare who's days on dates with every scored answer of those
// days, Telegram and site alike, on points without the speed bonus. Site
// rows count up to closed, as in PlayedDays.
func (s *Store) Standings(ctx context.Context, who Who, dates []round.Date, closed round.Date) ([]Standing, error) {
	if len(dates) == 0 {
		return nil, nil
	}
	ts := make([]time.Time, len(dates))
	for i, d := range dates {
		ts[i] = d.Time()
	}
	mine := `(SELECT DISTINCT ON (g.date) g.date, g.pts_km + g.pts_bonus AS base
	            FROM site_guess g
	           WHERE ` + who.siteRows("g.", "$3") + ` AND g.date = ANY($2::date[]) AND ` + scored("g.") + `
	           ORDER BY g.date, g.answered_at, g.player)`
	if who.UserID != 0 {
		mine = `SELECT date, pts_km + pts_bonus AS base FROM guess
		         WHERE user_id = $1 AND date = ANY($2::date[]) AND ` + scored("") + `
		        UNION ALL ` + mine
	}
	rows, err := s.pool.Query(ctx, `
		WITH mine AS (`+mine+`)
		SELECT m.date,
		       count(*) FILTER (WHERE ds.points - ds.pts_speed < m.base),
		       count(*) FILTER (WHERE ds.points - ds.pts_speed = m.base) - 1,
		       count(*)
		  FROM mine m JOIN day_score ds ON ds.date = m.date
		 GROUP BY m.date
		 ORDER BY m.date`, who.arg(), ts, closed.Time())
	if err != nil {
		return nil, fmt.Errorf("standings: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Standing, error) {
		var (
			st Standing
			at time.Time
		)
		err := row.Scan(&at, &st.Below, &st.Ties, &st.N)
		st.Date = round.DateIn(at, time.UTC)
		return st, err
	})
}

// LastRounds are the last n rounds up to upTo, newest first.
func (s *Store) LastRounds(ctx context.Context, upTo round.Date, n int) ([]RoundDay, error) {
	rows, err := s.pool.Query(ctx, `SELECT date, day_no FROM round WHERE date <= $1 ORDER BY date DESC LIMIT $2`, upTo.Time(), n)
	if err != nil {
		return nil, fmt.Errorf("rounds to %s: %w", upTo, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (RoundDay, error) {
		var (
			d  RoundDay
			at time.Time
		)
		err := row.Scan(&at, &d.DayNo)
		d.Date = round.DateIn(at, time.UTC)
		return d, err
	})
}

// NameOf is a Telegram person's first name, empty when core has none.
func (s *Store) NameOf(ctx context.Context, userID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(first_name, '') FROM person_shown WHERE telegram_user_id = $1`, userID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("name of %d: %w", userID, err)
	}
	return name, nil
}

// ProfileSetting is what a Telegram player chose about their profile.
type ProfileSetting struct {
	HideMe bool
	// PublicSlug names the public page; empty while it is off.
	PublicSlug string
}

func (s *Store) ProfileSettingOf(ctx context.Context, userID int64) (ProfileSetting, error) {
	var ps ProfileSetting
	err := s.pool.QueryRow(ctx, `SELECT hide_me, COALESCE(public_slug, '') FROM profile_setting WHERE user_id = $1`, userID).
		Scan(&ps.HideMe, &ps.PublicSlug)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProfileSetting{}, nil
	}
	if err != nil {
		return ProfileSetting{}, fmt.Errorf("profile setting of %d: %w", userID, err)
	}
	return ps, nil
}

func (s *Store) SetHideMe(ctx context.Context, userID int64, on bool) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO profile_setting (user_id, hide_me) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET hide_me = $2, updated_at = now()`, userID, on); err != nil {
		return fmt.Errorf("hide %d: %w", userID, err)
	}
	return nil
}

// SetPublicSlug turns the public page on under slug, or off with "". A new
// slug replaces the old one, which stops working at once.
func (s *Store) SetPublicSlug(ctx context.Context, userID int64, slug string) error {
	var v any
	if slug != "" {
		v = slug
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO profile_setting (user_id, public_slug) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET public_slug = $2, updated_at = now()`, userID, v); err != nil {
		return fmt.Errorf("public page of %d: %w", userID, err)
	}
	return nil
}

// PublicOwner is the person whose public page slug names, when it is on and
// they have not hidden themselves: the page's days would match the hidden
// rows of the days' tables. Unhidden, the same link works again.
func (s *Store) PublicOwner(ctx context.Context, slug string) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM profile_setting WHERE public_slug = $1 AND NOT hide_me`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("public page: %w", err)
	}
	return id, true, nil
}

// Hidden is which of ids asked to be hidden in the site's tables.
func (s *Store) Hidden(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id FROM profile_setting WHERE hide_me AND user_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("hidden players: %w", err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("hidden players: %w", err)
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// Present is which of ids core has seen with maply in chatID: the group's
// own people, who see each other's names and photos there.
func (s *Store) Present(ctx context.Context, chatID int64, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 || chatID == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT telegram_user_id FROM core.maply_presence WHERE chat_id = $1 AND telegram_user_id = ANY($2)`, chatID, ids)
	if err != nil {
		return nil, fmt.Errorf("present in %d: %w", chatID, err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("present in %d: %w", chatID, err)
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// SharesGroup is which of ids core has seen with maply in a group viewer was
// seen in too.
func (s *Store) SharesGroup(ctx context.Context, viewer int64, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 || viewer == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT o.telegram_user_id
		  FROM core.maply_presence me
		  JOIN core.maply_presence o ON o.chat_id = me.chat_id
		 WHERE me.telegram_user_id = $1 AND me.chat_id <> 0 AND o.telegram_user_id = ANY($2)`, viewer, ids)
	if err != nil {
		return nil, fmt.Errorf("groups shared with %d: %w", viewer, err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("groups shared with %d: %w", viewer, err)
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// HeadToHead is how two people did on the days both answered, up to upTo:
// on how many of them other's pin was closer than me's, among of in all.
func (s *Store) HeadToHead(ctx context.Context, me, other int64, upTo round.Date) (closer, of int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE b.km < a.km), count(*)
		  FROM guess a JOIN guess b ON b.date = a.date AND b.user_id = $2
		 WHERE a.user_id = $1 AND a.date <= $3 AND a.km IS NOT NULL AND b.km IS NOT NULL`,
		me, other, upTo.Time()).Scan(&closer, &of)
	if err != nil {
		return 0, 0, fmt.Errorf("head to head: %w", err)
	}
	return closer, of, nil
}

// FreeGame is one finished free game of a player's, from free_result.
type FreeGame struct {
	Kind    string // solo, party or duel
	Mode    string // classic or streak
	Seconds int    // 0 without a timer
	Rounds  int    // 0 for a streak
	// Played is how many places the player had in the game, answered or not.
	Played int
	Points int
	// Base is Points without the speed bonus.
	Base int
	// KmRounds are the distances of the places answered with a pin.
	KmRounds []float64
	// Place among Players in a party or a duel, shared by a tie; zero alone.
	// Beaten is how many of the others finished below.
	Place, Players, Beaten int
	Streak                 int
	At                     time.Time
}

// FreeGames are the player's finished free games, oldest first: a Telegram
// person's with those their account's site players finished before the
// link, or an account's without Telegram. Nil while free play has no
// results table.
func (s *Store) FreeGames(ctx context.Context, w FreeWho) ([]FreeGame, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('free_result') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("free results: %w", err)
	}
	if !exists {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT kind, mode, COALESCE(seconds, 0), COALESCE(rounds, 0), played, points, points_base, km_rounds,
		       COALESCE(place, 0), COALESCE(players, 0), COALESCE(beaten, 0), COALESCE(streak, 0), finished_at
		  FROM free_result
		 WHERE user_id = $1
		    OR player IN (SELECT sp.id FROM site_player sp JOIN account a ON a.id = sp.account_id
		                   WHERE a.telegram_user_id = $1 OR (a.telegram_user_id IS NULL AND a.id = NULLIF($2, '')::uuid))
		 ORDER BY finished_at`, w.UserID, w.Account)
	if err != nil {
		return nil, fmt.Errorf("free results: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (FreeGame, error) {
		var g FreeGame
		err := row.Scan(&g.Kind, &g.Mode, &g.Seconds, &g.Rounds, &g.Played, &g.Points, &g.Base, &g.KmRounds,
			&g.Place, &g.Players, &g.Beaten, &g.Streak, &g.At)
		return g, err
	})
}
