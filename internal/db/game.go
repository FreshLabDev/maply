// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// Puzzle is what anyone playing may see before the reveal.
type Puzzle struct {
	ImageKey string
	Kind     string
	// Heading is where the frame centre points, clockwise from north; nil
	// when the source did not say.
	Heading *float64
}

func (s *Store) Puzzle(ctx context.Context, placeID int64) (Puzzle, error) {
	var p Puzzle
	err := s.pool.QueryRow(ctx, `SELECT image_key, kind, heading FROM place WHERE id = $1`, placeID).Scan(&p.ImageKey, &p.Kind, &p.Heading)
	if err != nil {
		return Puzzle{}, fmt.Errorf("puzzle %d: %w", placeID, err)
	}
	return p, nil
}

// Answer is the place after the reveal. Fact and City hold one string per
// interface language.
type Answer struct {
	Point      geo.Point
	CountryISO string
	Continent  string
	City       map[string]string
	CityShape  geo.Shape
	Fact       map[string]string
	Author     string
	License    string
	Source     string
}

func (s *Store) Answer(ctx context.Context, placeID int64) (Answer, error) {
	var (
		a                Answer
		city, poly, fact []byte
		author           *string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT lat, lon, country_iso, continent, city, city_poly, fact, author, license, source
		  FROM place WHERE id = $1`, placeID).
		Scan(&a.Point.Lat, &a.Point.Lon, &a.CountryISO, &a.Continent, &city, &poly, &fact, &author, &a.License, &a.Source)
	if err != nil {
		return Answer{}, fmt.Errorf("answer %d: %w", placeID, err)
	}
	if author != nil {
		a.Author = *author
	}
	if err := decodeLangs(city, &a.City); err != nil {
		return Answer{}, fmt.Errorf("answer %d city: %w", placeID, err)
	}
	if err := decodeLangs(fact, &a.Fact); err != nil {
		return Answer{}, fmt.Errorf("answer %d fact: %w", placeID, err)
	}
	if len(poly) > 0 {
		if a.CityShape, err = geo.ParseGeometry(poly); err != nil {
			return Answer{}, fmt.Errorf("answer %d city polygon: %w", placeID, err)
		}
	}
	return a, nil
}

func decodeLangs(raw []byte, out *map[string]string) error {
	if len(raw) == 0 {
		*out = map[string]string{}
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ErrAlreadyAnswered means the person has an answer for the date already,
// from this chat or any other.
var ErrAlreadyAnswered = errors.New("already answered")

type NewGuess struct {
	Date       round.Date
	UserID     int64
	Context    string // dm, group, site
	ChatID     int64  // groups only
	Pin        geo.Point
	AnsweredAt time.Time
	Deadline   time.Time
}

func (s *Store) InsertGuess(ctx context.Context, g NewGuess) error {
	var chatID any
	if g.Context == "group" {
		chatID = g.ChatID
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO guess (date, user_id, context, chat_id, lat, lon, answered_at, deadline_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (date, user_id) DO NOTHING`,
		g.Date.Time(), g.UserID, g.Context, chatID, g.Pin.Lat, g.Pin.Lon, g.AnsweredAt, g.Deadline)
	if err != nil {
		return fmt.Errorf("insert guess: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyAnswered
	}
	return nil
}

// Guess is one stored answer. Score fields are nil until it is scored, which
// happens only after its deadline.
type Guess struct {
	UserID     int64
	FirstName  string
	Pin        geo.Point
	AnsweredAt time.Time
	Km         *float64
	PtsKm      *int
	PtsBonus   *int
}

func (g Guess) Total() int {
	if g.PtsKm == nil || g.PtsBonus == nil {
		return 0
	}
	return *g.PtsKm + *g.PtsBonus
}

func (s *Store) GuessOf(ctx context.Context, d round.Date, userID int64) (Guess, bool, error) {
	rows, err := s.pool.Query(ctx, guessSelect+` WHERE g.date = $1 AND g.user_id = $2`, d.Time(), userID)
	if err != nil {
		return Guess{}, false, fmt.Errorf("guess of %d: %w", userID, err)
	}
	list, err := pgx.CollectRows(rows, scanGuess)
	if err != nil || len(list) == 0 {
		return Guess{}, false, err
	}
	return list[0], true, nil
}

// ChatGuesses is the day's table for a group: the answers of everyone core has
// seen there with maply, wherever they answered from. Best score first, the
// earlier answer first on a tie.
func (s *Store) ChatGuesses(ctx context.Context, d round.Date, chatID int64) ([]Guess, error) {
	rows, err := s.pool.Query(ctx, guessSelect+`
		 WHERE g.date = $1
		   AND EXISTS (SELECT 1 FROM core.maply_presence p
		                WHERE p.telegram_user_id = g.user_id AND p.chat_id = $2)
		 ORDER BY COALESCE(g.pts_km + g.pts_bonus, -1) DESC, g.answered_at`, d.Time(), chatID)
	if err != nil {
		return nil, fmt.Errorf("chat guesses %d: %w", chatID, err)
	}
	return pgx.CollectRows(rows, scanGuess)
}

const guessSelect = `
	SELECT g.user_id, COALESCE(pe.first_name, ''), g.lat, g.lon, g.answered_at, g.km, g.pts_km, g.pts_bonus
	  FROM guess g
	  LEFT JOIN core.person pe ON pe.telegram_user_id = g.user_id`

func scanGuess(row pgx.CollectableRow) (Guess, error) {
	var g Guess
	err := row.Scan(&g.UserID, &g.FirstName, &g.Pin.Lat, &g.Pin.Lon, &g.AnsweredAt, &g.Km, &g.PtsKm, &g.PtsBonus)
	return g, err
}

// ScoreDue scores every answer for d whose deadline has passed and that has no
// score yet. A distance exists only once the person can no longer answer.
func (s *Store) ScoreDue(ctx context.Context, d round.Date, now time.Time, judge func(geo.Point) scoring.Score) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, lat, lon FROM guess
		 WHERE date = $1 AND km IS NULL AND deadline_at <= $2`, d.Time(), now)
	if err != nil {
		return 0, fmt.Errorf("due guesses: %w", err)
	}
	type due struct {
		user int64
		pin  geo.Point
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
		var x due
		err := row.Scan(&x.user, &x.pin.Lat, &x.pin.Lon)
		return x, err
	})
	if err != nil {
		return 0, err
	}
	for _, x := range list {
		sc := judge(x.pin)
		if _, err := s.pool.Exec(ctx, `
			UPDATE guess SET km = $3, pts_km = $4, pts_bonus = $5
			 WHERE date = $1 AND user_id = $2 AND km IS NULL`,
			d.Time(), x.user, sc.Km, sc.Distance, sc.Bonus); err != nil {
			return 0, fmt.Errorf("score guess of %d: %w", x.user, err)
		}
	}
	return len(list), nil
}

// UserLang is what core resolves for the person, empty when it has nothing.
func (s *Store) UserLang(ctx context.Context, userID int64) (string, error) {
	var lang *string
	if err := s.pool.QueryRow(ctx, `SELECT core.effective_language($1, NULL, 'user')`, userID).Scan(&lang); err != nil {
		return "", fmt.Errorf("user lang %d: %w", userID, err)
	}
	if lang == nil {
		return "", nil
	}
	return *lang, nil
}
