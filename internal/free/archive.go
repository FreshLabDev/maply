// SPDX-License-Identifier: Apache-2.0
package free

import (
	"context"
	"errors"
	"time"
)

// The archive is the days closed in every zone, played after them: one place,
// no timer, outside the day's ranking. Only a day over everywhere is in it,
// so the archive never holds a place still in play. A day is taken once, and
// not by someone who answered it or had its place in a free game: they know
// where it is.

// ArchiveDay is a closed day as the archive lists it for one player. Nothing
// of its place is in it.
type ArchiveDay struct {
	N    int    `json:"n"`
	Date string `json:"date"`
	// State is DayOpen, DayAnswered, DaySeen or DayPlayed.
	State string `json:"state"`
	// Game is the player's game of the day from the archive, and Points what
	// it has scored so far.
	Game   int64 `json:"game,omitempty"`
	Points *int  `json:"points,omitempty"`
	// DayPoints is what the player scored on the day itself, a day they
	// answered then.
	DayPoints *int `json:"day_points,omitempty"`
	DayWorld
}

// DayWorld is what a closed day's answers say without saying the place: how
// many there were and the usual miss, the median, a mark to beat.
type DayWorld struct {
	Players  int      `json:"players"`
	MedianKm *float64 `json:"median_km,omitempty"`
}

const (
	// DayOpen is a day the player may take.
	DayOpen = "open"
	// DayAnswered is a day the player answered on the day.
	DayAnswered = "answered"
	// DaySeen is a day whose place the player had in a free game.
	DaySeen = "seen"
	// DayPlayed is a day the player took from the archive.
	DayPlayed = "played"
)

var (
	// ErrDayAnswered is a day the player answered on the day.
	ErrDayAnswered = &Error{409, "day_answered"}
	// ErrDaySeen is a day whose place the player has had in a free game.
	ErrDaySeen = &Error{409, "day_seen"}
)

// ArchiveOf is the archive as w may take it, each day with its world. A
// player who has played nothing yet, a new browser, may take every day.
func (s *Service) ArchiveOf(ctx context.Context, w Who) ([]ArchiveDay, error) {
	s.init()
	days, err := s.Store.FreeArchive(ctx, w, s.closed(s.Now()))
	if err != nil {
		return nil, err
	}
	if len(days) == 0 {
		return days, nil
	}
	dates := make([]time.Time, 0, len(days))
	for _, d := range days {
		if t, err := time.Parse(time.DateOnly, d.Date); err == nil {
			dates = append(dates, t)
		}
	}
	worlds, err := s.Store.FreeDayWorlds(ctx, dates)
	if err != nil {
		return nil, err
	}
	for i := range days {
		days[i].DayWorld = worlds[days[i].Date]
	}
	return days, nil
}

// NewArchive takes closed day n: a game of its one place, begun at once and
// seen from that moment. Taking a day taken before gives its game again.
func (s *Service) NewArchive(ctx context.Context, w Who, name string, n int, lang string) (SoloView, error) {
	s.init()
	if !w.Valid() || n <= 0 {
		return SoloView{}, ErrBadRequest
	}
	if name == "" {
		name = "?"
	}
	now := s.Now()
	id, err := s.Store.CreateFreeArchive(ctx, n, s.closed(now), NewGame{Kind: Solo, Settings: ArchiveSettings, Status: StatusPlaying, At: now,
		Host: Member{M: 1, Who: w, Name: name, JoinedAt: now, FromRound: 1, LastSeen: now}})
	if errors.Is(err, ErrNoRow) {
		return SoloView{}, ErrNotFound
	}
	if err != nil {
		return SoloView{}, err
	}
	return s.SoloGame(ctx, w, id, lang)
}
