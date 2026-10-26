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

// WorldPin is one scored answer of a day where it was put, in the order of
// the world ranking.
type WorldPin struct {
	Lat, Lon   float64
	Km         float64
	AnsweredAt time.Time
}

// WorldPins is every scored answer of d, from Telegram and the site, best
// first, in the order World ranks them.
func (s *Store) WorldPins(ctx context.Context, d round.Date) ([]WorldPin, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT lat, lon, km, answered_at FROM (
		  SELECT lat, lon, km, answered_at, pts_km + pts_bonus + COALESCE(pts_speed, 0) AS points, user_id, NULL::uuid AS player
		    FROM guess WHERE date = $1 AND km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL
		  UNION ALL
		  SELECT lat, lon, km, answered_at, pts_km + pts_bonus, NULL, player
		    FROM site_guess WHERE date = $1 AND km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL) a
		 ORDER BY points DESC, answered_at, km, user_id, player`, d.Time())
	if err != nil {
		return nil, fmt.Errorf("world pins %s: %w", d, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (WorldPin, error) {
		var p WorldPin
		err := row.Scan(&p.Lat, &p.Lon, &p.Km, &p.AnsweredAt)
		return p, err
	})
}

// WorldCard is the kept animation of a day's world ranking in one language.
type WorldCard struct {
	Version string
	FileID  string
	Final   bool
}

func (s *Store) WorldCard(ctx context.Context, d round.Date, lang string) (WorldCard, bool, error) {
	var c WorldCard
	err := s.pool.QueryRow(ctx, `SELECT version, file_id, final FROM world_card WHERE date = $1 AND lang = $2`, d.Time(), lang).
		Scan(&c.Version, &c.FileID, &c.Final)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorldCard{}, false, nil
	}
	if err != nil {
		return WorldCard{}, false, fmt.Errorf("world card %s %s: %w", d, lang, err)
	}
	return c, true, nil
}

func (s *Store) SetWorldCard(ctx context.Context, d round.Date, lang string, c WorldCard) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO world_card (date, lang, version, file_id, final) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (date, lang) DO UPDATE SET version = $3, file_id = $4, final = $5, updated_at = now()`,
		d.Time(), lang, c.Version, c.FileID, c.Final)
	if err != nil {
		return fmt.Errorf("set world card %s %s: %w", d, lang, err)
	}
	return nil
}

// DMResult is a result the bot sent to a person's DM.
type DMResult struct {
	ChatID int64
	MsgID  int64
}

// DMResults is the DM results of d that went out as an animation, for
// putting the final world ranking in them once the day closes everywhere.
func (s *Store) DMResults(ctx context.Context, d round.Date) ([]DMResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cr.chat_id, cr.result_msg_id FROM chat_round cr
		  JOIN chat_settings cs ON cs.chat_id = cr.chat_id
		 WHERE cr.date = $1 AND cs.kind = 'dm' AND cs.active
		   AND cr.result_msg_id IS NOT NULL AND cr.result_file_id IS NOT NULL
		 ORDER BY cr.chat_id`, d.Time())
	if err != nil {
		return nil, fmt.Errorf("dm results %s: %w", d, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DMResult, error) {
		var r DMResult
		err := row.Scan(&r.ChatID, &r.MsgID)
		return r, err
	})
}

// SetWorldResult records a DM result that shows the world picture of
// version. An empty fileID keeps the one recorded.
func (s *Store) SetWorldResult(ctx context.Context, chatID int64, d round.Date, msgID int64, fileID, version string) error {
	return s.updateChatRound(ctx, chatID, d,
		`result_msg_id = $3, result_file_id = COALESCE(NULLIF($4, ''), result_file_id), world_version = $5`, msgID, fileID, version)
}

// WorldVersion is the version of the world picture chatID's result of d
// shows, empty when it shows none.
func (s *Store) WorldVersion(ctx context.Context, chatID int64, d round.Date) (string, error) {
	var v *string
	err := s.pool.QueryRow(ctx, `SELECT world_version FROM chat_round WHERE chat_id = $1 AND date = $2`, chatID, d.Time()).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) || v == nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("world version of %d %s: %w", chatID, d, err)
	}
	return *v, nil
}
