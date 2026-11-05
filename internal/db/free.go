// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/geo"
)

// The free game's tables are its own: nothing here reads guess or writes
// core, and the day's statistics never read these.

var _ free.Store = (*Store)(nil)

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func whoArgs(w free.Who) (any, any) {
	var user, player any
	if w.UserID != 0 {
		user = w.UserID
	}
	if w.Player != "" {
		player = w.Player
	}
	return user, player
}

// codeTaken is whether err is another game holding the party code.
func codeTaken(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "free_game_code_key"
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func insertGame(ctx context.Context, q execer, g free.NewGame) (int64, error) {
	var code any
	if g.Code != "" {
		code = g.Code
	}
	var id int64
	err := q.QueryRow(ctx, `
		INSERT INTO free_game (code, kind, mode, seconds, rounds, host, status, created_at, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CASE WHEN $7 = 'playing' THEN $8::timestamptz END)
		RETURNING id`,
		code, g.Kind, g.Settings.Mode, nullInt(g.Settings.Seconds), nullInt(g.Settings.Rounds), g.Host.M, g.Status, g.At).Scan(&id)
	if codeTaken(err) {
		return 0, free.ErrCodeTaken
	}
	if err != nil {
		return 0, fmt.Errorf("insert free game: %w", err)
	}
	if err := insertMember(ctx, q, id, g.Host); err != nil {
		return 0, err
	}
	return id, nil
}

func insertMember(ctx context.Context, q execer, game int64, m free.Member) error {
	user, player := whoArgs(m.Who)
	_, err := q.Exec(ctx, `
		INSERT INTO free_member (game, m, user_id, player, name, joined_at, from_round, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		game, m.M, user, player, m.Name, m.JoinedAt, m.FromRound, m.LastSeen)
	if err != nil {
		return fmt.Errorf("insert free member: %w", err)
	}
	return nil
}

func (s *Store) CreateFreeGame(ctx context.Context, g free.NewGame) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id, err := insertGame(ctx, tx, g)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

func (s *Store) FreeGameID(ctx context.Context, code string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM free_game WHERE code = $1`, code).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, free.ErrNoRow
	}
	return id, err
}

// FreeSnapshot reads a game whole in one round trip.
func (s *Store) FreeSnapshot(ctx context.Context, id int64) (free.Snapshot, error) {
	var snap free.Snapshot
	b := &pgx.Batch{}
	b.Queue(`
		SELECT g.id, COALESCE(g.code, ''), g.kind, g.mode, COALESCE(g.seconds, 0), COALESCE(g.rounds, 0),
		       g.host, g.status, g.version, COALESCE(n.code, ''), g.created_at, g.started_at, g.finished_at
		  FROM free_game g LEFT JOIN free_game n ON n.id = g.next_id
		 WHERE g.id = $1`, id).QueryRow(func(row pgx.Row) error {
		g := &snap.Game
		err := row.Scan(&g.ID, &g.Code, &g.Kind, &g.Settings.Mode, &g.Settings.Seconds, &g.Settings.Rounds,
			&g.Host, &g.Status, &g.Version, &g.NextCode, &g.CreatedAt, &g.StartedAt, &g.FinishedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return free.ErrNoRow
		}
		return err
	})
	b.Queue(`
		SELECT m, COALESCE(user_id, 0), COALESCE(player::text, ''), name, joined_at, from_round, last_seen_at, left_at
		  FROM free_member WHERE game = $1 ORDER BY joined_at, m`, id).Query(func(rows pgx.Rows) error {
		var err error
		snap.Members, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (free.Member, error) {
			var m free.Member
			err := row.Scan(&m.M, &m.Who.UserID, &m.Who.Player, &m.Name, &m.JoinedAt, &m.FromRound, &m.LastSeen, &m.LeftAt)
			return m, err
		})
		return err
	})
	b.Queue(`
		SELECT n, place_id, repeat, starts_at, ends_at, closed_at
		  FROM free_round WHERE game = $1 ORDER BY n`, id).Query(func(rows pgx.Rows) error {
		var err error
		snap.Rounds, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (free.Round, error) {
			var r free.Round
			err := row.Scan(&r.N, &r.PlaceID, &r.Repeat, &r.StartsAt, &r.EndsAt, &r.ClosedAt)
			return r, err
		})
		return err
	})
	b.Queue(`
		SELECT n, m, lat, lon, answered_at, auto, COALESCE(km, 0), pts_km, pts_bonus, pts_speed, country_ok, time_ms
		  FROM free_guess WHERE game = $1 ORDER BY n, m`, id).Query(func(rows pgx.Rows) error {
		var err error
		snap.Guesses, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (free.Guess, error) {
			var g free.Guess
			var lat, lon *float64
			err := row.Scan(&g.N, &g.M, &lat, &lon, &g.AnsweredAt, &g.Auto, &g.Km, &g.PtsKm, &g.PtsBonus, &g.PtsSpeed, &g.CountryOK, &g.TimeMs)
			if lat != nil && lon != nil {
				g.Pin = &geo.Point{Lat: *lat, Lon: *lon}
			}
			return g, err
		})
		return err
	})
	if err := s.pool.SendBatch(ctx, b).Close(); err != nil {
		if errors.Is(err, free.ErrNoRow) {
			return free.Snapshot{}, free.ErrNoRow
		}
		return free.Snapshot{}, fmt.Errorf("free game %d: %w", id, err)
	}
	return snap, nil
}

// FreeKnown is whether a Telegram person has had the place: answered the day
// it was the place of, or had it in any free game they were in.
func (s *Store) FreeKnown(ctx context.Context, userID, placeID int64) (bool, error) {
	var known bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM free_round fr JOIN free_member fm ON fm.game = fr.game
		                WHERE fr.place_id = $2 AND fm.user_id = $1)
		    OR EXISTS (SELECT 1 FROM round r JOIN guess g ON g.date = r.date
		                WHERE r.place_id = $2 AND g.user_id = $1)`, userID, placeID).Scan(&known)
	if err != nil {
		return false, fmt.Errorf("free known %d/%d: %w", userID, placeID, err)
	}
	return known, nil
}

// FreeCandidates lists the places a game may get next: the free pool, and the
// days closed in every zone that none of the game's players answered, minus
// the game's own places. Each comes with when one of the players last had it
// in any free game, nil when none has.
func (s *Store) FreeCandidates(ctx context.Context, id int64) ([]free.Candidate, map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `
		WITH m AS (SELECT user_id, player FROM free_member WHERE game = $1),
		     games AS (SELECT DISTINCT fm.game FROM free_member fm
		                 JOIN m ON fm.user_id = m.user_id OR fm.player = m.player),
		     seen AS (SELECT fr.place_id, max(fr.starts_at) AS at FROM free_round fr
		               WHERE fr.game IN (SELECT game FROM games) GROUP BY fr.place_id),
		     cand AS (
		       SELECT p.id, p.country_iso, false AS day FROM place p WHERE p.pool = 'free' AND p.status = 'approved'
		       UNION ALL
		       SELECT p.id, p.country_iso, true
		         FROM day_closed dc JOIN round r ON r.date = dc.date JOIN place p ON p.id = r.place_id
		        WHERE NOT EXISTS (SELECT 1 FROM guess g JOIN m ON g.user_id = m.user_id WHERE g.date = dc.date)
		          AND NOT EXISTS (SELECT 1 FROM site_guess g JOIN m ON g.player = m.player WHERE g.date = dc.date))
		SELECT c.id, c.country_iso, s.at, c.day
		  FROM cand c LEFT JOIN seen s ON s.place_id = c.id
		 WHERE NOT EXISTS (SELECT 1 FROM free_round fr WHERE fr.game = $1 AND fr.place_id = c.id)`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("free candidates of %d: %w", id, err)
	}
	cands, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (free.Candidate, error) {
		var c free.Candidate
		err := row.Scan(&c.PlaceID, &c.Country, &c.SeenAt, &c.Day)
		return c, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("free candidates of %d: %w", id, err)
	}
	rows, err = s.pool.Query(ctx, `
		SELECT DISTINCT p.country_iso FROM free_round fr JOIN place p ON p.id = fr.place_id WHERE fr.game = $1`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("free countries of %d: %w", id, err)
	}
	countries, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, nil, fmt.Errorf("free countries of %d: %w", id, err)
	}
	used := make(map[string]bool, len(countries))
	for _, c := range countries {
		used[c] = true
	}
	return cands, used, nil
}

func (s *Store) FreePlace(ctx context.Context, placeID int64) (free.Place, error) {
	var (
		p                free.Place
		city, poly, fact []byte
		author           *string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, image_key, kind, heading, lat, lon, country_iso, continent, city, city_poly, fact, author, license, source
		  FROM place WHERE id = $1`, placeID).
		Scan(&p.ID, &p.ImageKey, &p.Kind, &p.Heading, &p.Point.Lat, &p.Point.Lon, &p.Country, &p.Continent,
			&city, &poly, &fact, &author, &p.License, &p.Source)
	if err != nil {
		return free.Place{}, fmt.Errorf("free place %d: %w", placeID, err)
	}
	if author != nil {
		p.Author = *author
	}
	if err := decodeLangs(city, &p.City); err != nil {
		return free.Place{}, fmt.Errorf("free place %d city: %w", placeID, err)
	}
	if err := decodeLangs(fact, &p.Fact); err != nil {
		return free.Place{}, fmt.Errorf("free place %d fact: %w", placeID, err)
	}
	if len(poly) > 0 {
		if p.CityShape, err = geo.ParseGeometry(poly); err != nil {
			return free.Place{}, fmt.Errorf("free place %d city polygon: %w", placeID, err)
		}
	}
	return p, nil
}

func insertRound(ctx context.Context, q execer, id int64, r free.NewRound) error {
	_, err := q.Exec(ctx, `
		INSERT INTO free_round (game, n, place_id, repeat, starts_at, ends_at)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
		id, r.N, r.PlaceID, r.Repeat, r.StartsAt, r.EndsAt)
	if err != nil {
		return fmt.Errorf("insert free round %d/%d: %w", id, r.N, err)
	}
	return nil
}

func (s *Store) InsertFreeRound(ctx context.Context, id int64, r free.NewRound) error {
	return insertRound(ctx, s.pool, id, r)
}

func (s *Store) InsertFreeGuess(ctx context.Context, id int64, g free.Guess) (bool, error) {
	var lat, lon, km any
	if g.Pin != nil {
		lat, lon, km = g.Pin.Lat, g.Pin.Lon, g.Km
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO free_guess (game, n, m, lat, lon, answered_at, auto, km, pts_km, pts_bonus, pts_speed, country_ok, time_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (game, n, m) DO NOTHING`,
		id, g.N, g.M, lat, lon, g.AnsweredAt, g.Auto, km, g.PtsKm, g.PtsBonus, g.PtsSpeed, g.CountryOK, g.TimeMs)
	if err != nil {
		return false, fmt.Errorf("insert free guess %d/%d: %w", id, g.N, err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) CloseFreeRound(ctx context.Context, id int64, n int, at time.Time, next *free.NewRound) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE free_round SET closed_at = $3 WHERE game = $1 AND n = $2 AND closed_at IS NULL`, id, n, at); err != nil {
		return fmt.Errorf("close free round %d/%d: %w", id, n, err)
	}
	if next != nil {
		if err := insertRound(ctx, tx, id, *next); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) JoinFree(ctx context.Context, id int64, m free.Member) error {
	return insertMember(ctx, s.pool, id, m)
}

func (s *Store) RejoinFree(ctx context.Context, id int64, m int) error {
	_, err := s.pool.Exec(ctx, `UPDATE free_member SET left_at = NULL WHERE game = $1 AND m = $2`, id, m)
	return err
}

func (s *Store) LeaveFree(ctx context.Context, id int64, m int, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE free_member SET left_at = $3 WHERE game = $1 AND m = $2 AND left_at IS NULL`, id, m, at)
	return err
}

func (s *Store) SeenFree(ctx context.Context, id int64, m int, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE free_member SET last_seen_at = GREATEST(last_seen_at, $3) WHERE game = $1 AND m = $2`, id, m, at)
	return err
}

func (s *Store) SetFreeSettings(ctx context.Context, id int64, set free.Settings) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE free_game SET mode = $2, seconds = $3, rounds = $4, version = version + 1
		 WHERE id = $1 AND status = 'lobby'`, id, set.Mode, nullInt(set.Seconds), nullInt(set.Rounds))
	return err
}

func (s *Store) SetFreeHost(ctx context.Context, id int64, m int) error {
	_, err := s.pool.Exec(ctx, `UPDATE free_game SET host = $2 WHERE id = $1`, id, m)
	return err
}

func (s *Store) StartFree(ctx context.Context, id int64, version int, kind string, at time.Time, first free.NewRound) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE free_game SET status = 'playing', kind = $3, started_at = $4
		 WHERE id = $1 AND status = 'lobby' AND version = $2`, id, version, kind, at)
	if err != nil {
		return false, fmt.Errorf("start free game %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if err := insertRound(ctx, tx, id, first); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Store) FinishFree(ctx context.Context, id int64, status string, at time.Time, results []free.Result) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE free_game SET status = $2, finished_at = $3
		 WHERE id = $1 AND status IN ('lobby', 'playing')`, id, status, at)
	if err != nil {
		return false, fmt.Errorf("finish free game %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	for _, r := range results {
		var place, players, beaten, streak any
		if r.Kind != free.Solo {
			place, players, beaten = r.Place, r.Players, r.Beaten
		}
		if r.Settings.Mode == free.Streak {
			streak = r.Streak
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO free_result (game, m, user_id, kind, mode, seconds, rounds, points, points_base, km, km_rounds,
			                         time_ms, played, answered, countries, from_round, place, players, beaten, streak, finished_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
			ON CONFLICT DO NOTHING`,
			id, r.M, r.UserID, r.Kind, r.Settings.Mode, nullInt(r.Settings.Seconds), nullInt(r.Settings.Rounds),
			r.Points, r.Base, r.Km, r.KmRounds, r.TimeMs, r.Played, r.Answered, r.Countries, r.FromRound,
			place, players, beaten, streak, at); err != nil {
			return false, fmt.Errorf("free result %d/%d: %w", id, r.M, err)
		}
	}
	return true, tx.Commit(ctx)
}

func (s *Store) RematchFree(ctx context.Context, id int64, g free.NewGame) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var next *int64
	if err := tx.QueryRow(ctx, `SELECT next_id FROM free_game WHERE id = $1 FOR UPDATE`, id).Scan(&next); err != nil {
		return 0, fmt.Errorf("rematch of %d: %w", id, err)
	}
	if next != nil {
		return *next, nil
	}
	nid, err := insertGame(ctx, tx, g)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE free_game SET next_id = $2 WHERE id = $1`, id, nid); err != nil {
		return 0, fmt.Errorf("rematch of %d: %w", id, err)
	}
	return nid, tx.Commit(ctx)
}

// memberOf is the condition on free_member m for a player, using the index of
// whichever kind of player it is.
func memberOf(w free.Who) (string, any) {
	if w.UserID != 0 {
		return `m.user_id = $1`, w.UserID
	}
	return `m.player = $1::uuid`, w.Player
}

func (s *Store) LastFreeSettings(ctx context.Context, w free.Who) (free.Settings, bool, error) {
	cond, arg := memberOf(w)
	var set free.Settings
	err := s.pool.QueryRow(ctx, `
		SELECT g.mode, COALESCE(g.seconds, 0), COALESCE(g.rounds, 0)
		  FROM free_member m JOIN free_game g ON g.id = m.game
		 WHERE `+cond+` AND g.kind = 'solo'
		 ORDER BY g.created_at DESC, g.id DESC LIMIT 1`, arg).Scan(&set.Mode, &set.Seconds, &set.Rounds)
	if errors.Is(err, pgx.ErrNoRows) {
		return free.Settings{}, false, nil
	}
	if err != nil {
		return free.Settings{}, false, fmt.Errorf("last free settings: %w", err)
	}
	return set, true, nil
}

func (s *Store) OpenFreeGames(ctx context.Context, w free.Who, kind string) ([]int64, error) {
	cond, arg := memberOf(w)
	rows, err := s.pool.Query(ctx, `
		SELECT g.id FROM free_member m JOIN free_game g ON g.id = m.game
		 WHERE `+cond+` AND g.status IN ('lobby', 'playing') AND (g.kind = 'solo') = $2
		 ORDER BY g.id DESC`, arg, kind == free.Solo)
	if err != nil {
		return nil, fmt.Errorf("open free games: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func (s *Store) FreeBest(ctx context.Context, userID int64, except int64) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT mode, COALESCE(seconds, 0), COALESCE(rounds, 0),
		       max(CASE WHEN mode = 'streak' THEN streak ELSE points END)
		  FROM free_result
		 WHERE user_id = $1 AND game <> $2
		 GROUP BY 1, 2, 3`, userID, except)
	if err != nil {
		return nil, fmt.Errorf("free best of %d: %w", userID, err)
	}
	defer rows.Close()
	best := map[string]int{}
	for rows.Next() {
		var set free.Settings
		var v int
		if err := rows.Scan(&set.Mode, &set.Seconds, &set.Rounds, &v); err != nil {
			return nil, err
		}
		best[set.Key()] = v
	}
	return best, rows.Err()
}

func (s *Store) StaleFreeGames(ctx context.Context, idle, lobby, now time.Time) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id FROM free_game g
		 WHERE g.status IN ('lobby', 'playing')
		   AND ((g.kind <> 'solo' AND g.status = 'playing')
		     OR (g.status = 'lobby'
		         AND GREATEST(g.created_at, (SELECT max(last_seen_at) FROM free_member WHERE game = g.id)) < $2)
		     OR (g.kind = 'solo'
		         AND (GREATEST(g.created_at,
		                       (SELECT max(starts_at) FROM free_round WHERE game = g.id),
		                       (SELECT max(answered_at) FROM free_guess WHERE game = g.id)) < $1
		              OR EXISTS (SELECT 1 FROM free_round r
		                          WHERE r.game = g.id AND r.ends_at + interval '2 seconds' < $3
		                            AND NOT EXISTS (SELECT 1 FROM free_guess q WHERE q.game = r.game AND q.n = r.n)))))
		 ORDER BY g.id LIMIT 500`, idle, lobby, now)
	if err != nil {
		return nil, fmt.Errorf("stale free games: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
