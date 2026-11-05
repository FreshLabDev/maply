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
)

// The archive and the challenges of the free game. The archive reads the
// day's tables, guess and day_place among them, and writes only its own.

// violates is whether err is a unique violation of constraint.
func violates(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == constraint
}

func (s *Store) FreeRoundPlace(ctx context.Context, game int64, n int) (int64, error) {
	var place int64
	err := s.pool.QueryRow(ctx, `SELECT place_id FROM free_round WHERE game = $1 AND n = $2`, game, n).Scan(&place)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, free.ErrNoRow
	}
	if err != nil {
		return 0, fmt.Errorf("free round place %d/%d: %w", game, n, err)
	}
	return place, nil
}

// FreeArchive lists only days in day_closed: a day still in play anywhere is
// not in the archive, whoever asks. A player who answered a day, as a
// Telegram person or as the site's player, or had its place in a free game,
// sees it so.
func (s *Store) FreeArchive(ctx context.Context, w free.Who) ([]free.ArchiveDay, error) {
	user, player := whoArgs(w)
	rows, err := s.pool.Query(ctx, `
		SELECT r.day_no, r.date,
		       CASE WHEN a.game IS NOT NULL THEN 'played'
		            WHEN EXISTS (SELECT 1 FROM guess g WHERE g.date = r.date AND g.user_id = $1)
		              OR EXISTS (SELECT 1 FROM site_guess g WHERE g.date = r.date AND g.player = $2::uuid) THEN 'answered'
		            WHEN EXISTS (SELECT 1 FROM free_round fr JOIN free_member fm ON fm.game = fr.game
		                          WHERE fr.place_id = r.place_id AND (fm.user_id = $1 OR fm.player = $2::uuid)) THEN 'seen'
		            ELSE 'open' END,
		       COALESCE(a.game, 0),
		       (SELECT sum(q.pts_km + q.pts_bonus + q.pts_speed) FROM free_guess q WHERE q.game = a.game)
		  FROM day_closed dc JOIN round r ON r.date = dc.date
		  LEFT JOIN free_archive a ON a.date = dc.date AND (a.user_id = $1 OR a.player = $2::uuid)
		 ORDER BY r.day_no DESC
		 LIMIT 1000`, user, player)
	if err != nil {
		return nil, fmt.Errorf("free archive: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (free.ArchiveDay, error) {
		var d free.ArchiveDay
		var date time.Time
		var points *int64
		if err := row.Scan(&d.N, &date, &d.State, &d.Game, &points); err != nil {
			return d, err
		}
		d.Date = date.Format(time.DateOnly)
		if points != nil {
			p := int(*points)
			d.Points = &p
		}
		return d, nil
	})
}

func (s *Store) CreateFreeArchive(ctx context.Context, dayNo int, g free.NewGame) (int64, error) {
	id, err := s.createArchive(ctx, dayNo, g)
	// Taken at the same moment in another request: that game is the one.
	if violates(err, "free_archive_user_idx") || violates(err, "free_archive_player_idx") {
		return s.createArchive(ctx, dayNo, g)
	}
	return id, err
}

func (s *Store) createArchive(ctx context.Context, dayNo int, g free.NewGame) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var date time.Time
	var place int64
	err = tx.QueryRow(ctx, `
		SELECT r.date, r.place_id FROM round r JOIN day_closed dc ON dc.date = r.date
		 WHERE r.day_no = $1`, dayNo).Scan(&date, &place)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, free.ErrNoRow
	}
	if err != nil {
		return 0, fmt.Errorf("archive day %d: %w", dayNo, err)
	}
	user, player := whoArgs(g.Host.Who)
	var game int64
	err = tx.QueryRow(ctx, `
		SELECT game FROM free_archive WHERE date = $1 AND (user_id = $2 OR player = $3::uuid)`, date, user, player).Scan(&game)
	if err == nil {
		return game, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("archive day %d: %w", dayNo, err)
	}
	var answered, seen bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM guess WHERE date = $1 AND user_id = $2)
		    OR EXISTS (SELECT 1 FROM site_guess WHERE date = $1 AND player = $3::uuid),
		       EXISTS (SELECT 1 FROM free_round fr JOIN free_member fm ON fm.game = fr.game
		                WHERE fr.place_id = $4 AND (fm.user_id = $2 OR fm.player = $3::uuid))`,
		date, user, player, place).Scan(&answered, &seen)
	if err != nil {
		return 0, fmt.Errorf("archive day %d: %w", dayNo, err)
	}
	switch {
	case answered:
		return 0, free.ErrDayAnswered
	case seen:
		return 0, free.ErrDaySeen
	}
	id, err := insertGame(ctx, tx, g)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO free_archive (game, date, user_id, player) VALUES ($1, $2, $3, $4)`, id, date, user, player); err != nil {
		return 0, fmt.Errorf("archive day %d: %w", dayNo, err)
	}
	if err := insertRound(ctx, tx, id, free.NewRound{N: 1, PlaceID: place, StartsAt: g.At}); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// FreeDayRank compares with every answer the day kept, each counted as its
// distance and its continent, country or city: the speed bonus a timer gave
// on the day the archive has nothing to match. An answer worth as much stood
// ahead: it came first.
func (s *Store) FreeDayRank(ctx context.Context, date time.Time, base int, km float64) (int, int, error) {
	var rank, of int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE b > $2 OR (b = $2 AND km <= $3)) + 1, count(*) + 1
		  FROM (SELECT d.km, COALESCE(g.pts_km + g.pts_bonus, s.pts_km + s.pts_bonus, d.points) AS b
		          FROM day_place d
		          LEFT JOIN guess g ON g.date = d.date AND g.user_id = d.user_id
		          LEFT JOIN site_guess s ON s.date = d.date AND s.player = d.player
		         WHERE d.date = $1) x`, date, base, km).Scan(&rank, &of)
	if err != nil {
		return 0, 0, fmt.Errorf("day rank of %s: %w", date.Format(time.DateOnly), err)
	}
	return rank, of, nil
}

func (s *Store) MakeFreeChallenge(ctx context.Context, game, userID int64, code string, at time.Time) (string, error) {
	got, err := s.makeChallenge(ctx, game, userID, code, at)
	if violates(err, "free_challenge_code_key") {
		return "", free.ErrCodeTaken
	}
	if violates(err, "free_challenge_game_key") {
		return s.makeChallenge(ctx, game, userID, code, at)
	}
	return got, err
}

func (s *Store) makeChallenge(ctx context.Context, game, userID int64, code string, at time.Time) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var made string
	err = tx.QueryRow(ctx, `SELECT code FROM free_challenge WHERE game = $1`, game).Scan(&made)
	if err == nil {
		return made, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("challenge of %d: %w", game, err)
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO free_challenge (code, game, created_at) VALUES ($1, $2, $3) RETURNING id`, code, game, at).Scan(&id); err != nil {
		return "", fmt.Errorf("challenge of %d: %w", game, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO free_attempt (challenge, user_id, game) VALUES ($1, $2, $3)`, id, userID, game); err != nil {
		return "", fmt.Errorf("challenge of %d: %w", game, err)
	}
	return code, tx.Commit(ctx)
}

// FreeChallenge sums each attempt over the places answered so far, a place
// without a pin as the farthest a pin can be, as free.Lines does.
func (s *Store) FreeChallenge(ctx context.Context, code string) (free.ChallengeRow, error) {
	var c free.ChallengeRow
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.code, c.game, g.mode, COALESCE(g.seconds, 0), COALESCE(g.rounds, 0), c.created_at
		  FROM free_challenge c JOIN free_game g ON g.id = c.game WHERE c.code = $1`, code).
		Scan(&c.ID, &c.Code, &c.Game, &c.Settings.Mode, &c.Settings.Seconds, &c.Settings.Rounds, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return free.ChallengeRow{}, free.ErrNoRow
	}
	if err != nil {
		return free.ChallengeRow{}, fmt.Errorf("challenge %s: %w", code, err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.user_id, m.name, g.id, g.status, g.id = $2,
		       count(q.n), count(q.n) FILTER (WHERE q.lat IS NOT NULL),
		       COALESCE(sum(q.pts_km + q.pts_bonus + q.pts_speed), 0), COALESCE(sum(q.pts_km + q.pts_bonus), 0),
		       COALESCE(sum(COALESCE(q.km, $3)), 0), COALESCE(sum(q.time_ms), 0),
		       count(q.n) FILTER (WHERE q.country_ok),
		       (SELECT count(*) FROM free_round r WHERE r.game = g.id AND r.repeat)
		  FROM free_attempt a
		  JOIN free_game g ON g.id = a.game
		  JOIN free_member m ON m.game = g.id AND m.m = 1
		  LEFT JOIN free_guess q ON q.game = g.id AND q.m = 1
		 WHERE a.challenge = $1
		 GROUP BY a.user_id, m.name, g.id
		 ORDER BY g.id = $2 DESC, g.created_at, g.id`, c.ID, c.Game, free.MaxKm)
	if err != nil {
		return free.ChallengeRow{}, fmt.Errorf("challenge %s attempts: %w", code, err)
	}
	c.Attempts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (free.Attempt, error) {
		var a free.Attempt
		l := &a.Line
		err := row.Scan(&a.UserID, &a.Name, &a.Game, &a.Status, &a.Maker, &l.Played, &l.Answered,
			&l.Points, &l.Base, &l.Km, &l.TimeMs, &l.Countries, &a.Seen)
		l.FromRound = 1
		return a, err
	})
	if err != nil {
		return free.ChallengeRow{}, fmt.Errorf("challenge %s attempts: %w", code, err)
	}
	return c, nil
}

func (s *Store) TakeFreeChallenge(ctx context.Context, challenge int64, g free.NewGame) (int64, error) {
	id, err := s.takeChallenge(ctx, challenge, g)
	if violates(err, "free_attempt_pkey") {
		return s.takeChallenge(ctx, challenge, g)
	}
	return id, err
}

func (s *Store) takeChallenge(ctx context.Context, challenge int64, g free.NewGame) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var game int64
	err = tx.QueryRow(ctx, `
		SELECT game FROM free_attempt WHERE challenge = $1 AND user_id = $2`, challenge, g.Host.Who.UserID).Scan(&game)
	if err == nil {
		return game, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("attempt at %d: %w", challenge, err)
	}
	id, err := insertGame(ctx, tx, g)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO free_attempt (challenge, user_id, game) VALUES ($1, $2, $3)`, challenge, g.Host.Who.UserID, id); err != nil {
		return 0, fmt.Errorf("attempt at %d: %w", challenge, err)
	}
	return id, tx.Commit(ctx)
}

func (s *Store) FreeChallenges(ctx context.Context, userID int64, since time.Time) ([]free.ChallengeBrief, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.code, mk.name, g0.mode, COALESCE(g0.seconds, 0), COALESCE(g0.rounds, 0),
		       (SELECT count(*) FROM free_attempt x WHERE x.challenge = c.id),
		       a.game = c.game, g.status,
		       GREATEST(c.created_at, (SELECT max(xg.finished_at) FROM free_attempt x JOIN free_game xg ON xg.id = x.game
		                                WHERE x.challenge = c.id)) AS at
		  FROM free_attempt a
		  JOIN free_challenge c ON c.id = a.challenge
		  JOIN free_game g0 ON g0.id = c.game
		  JOIN free_member mk ON mk.game = c.game AND mk.m = 1
		  JOIN free_game g ON g.id = a.game
		 WHERE a.user_id = $1 AND c.created_at > $2
		 ORDER BY at DESC, c.id DESC LIMIT 5`, userID, since)
	if err != nil {
		return nil, fmt.Errorf("challenges of %d: %w", userID, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (free.ChallengeBrief, error) {
		var b free.ChallengeBrief
		err := row.Scan(&b.Code, &b.By, &b.Settings.Mode, &b.Settings.Seconds, &b.Settings.Rounds, &b.Players,
			&b.Maker, &b.Status, &b.At)
		return b, err
	})
}
