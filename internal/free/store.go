// SPDX-License-Identifier: Apache-2.0
package free

import (
	"context"
	"errors"
	"time"

	"github.com/FreshLabDev/maply/internal/geo"
)

// Who is a player: a Telegram person, or a site player in a browser. Exactly
// one is set. Account marks a site player that belongs to an account on the
// site: an account with Google alone plays as its own site player.
type Who struct {
	UserID  int64
	Player  string
	Account bool
}

func (w Who) Valid() bool { return (w.UserID != 0) != (w.Player != "") }

// Telegram is whether the player is a Telegram person: the one kind recorded
// in core and sent invitations by the bot.
func (w Who) Telegram() bool { return w.UserID != 0 }

// Keeps is whether the player keeps records and may play with friends: a
// Telegram person, or an account's site player. A guest of the site plays
// alone and keeps nothing.
func (w Who) Keeps() bool { return w.UserID != 0 || (w.Player != "" && w.Account) }

// Same is whether w and o are one player, whatever is known of their
// accounts.
func (w Who) Same(o Who) bool { return w.UserID == o.UserID && w.Player == o.Player }

type Game struct {
	ID       int64
	Code     string
	Kind     string
	Settings Settings
	Host     int
	Status   string
	// Version is the settings' version; a party's start names it.
	Version    int
	NextCode   string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	// Day is the closed day a game from the archive plays, and DayNo its
	// number; the zero time otherwise.
	Day   time.Time
	DayNo int
	// From is the game whose places an attempt at a challenge plays.
	From int64
	// Challenge is the code of the challenge the game is an attempt at, or
	// was made into.
	Challenge string
}

type Member struct {
	M         int
	Who       Who
	Name      string
	JoinedAt  time.Time
	FromRound int
	LastSeen  time.Time
	LeftAt    *time.Time
}

// Round is a place of a game. EndsAt is nil without a timer.
type Round struct {
	N        int
	PlaceID  int64
	Repeat   bool
	StartsAt time.Time
	EndsAt   *time.Time
	ClosedAt *time.Time
}

// Over is when no answer is taken any more: the end plus the grace, or when
// the place closed, whichever came first. The zero time without a timer.
func (r Round) Over() time.Time {
	if r.ClosedAt != nil {
		return *r.ClosedAt
	}
	if r.EndsAt != nil {
		return r.EndsAt.Add(Grace)
	}
	return time.Time{}
}

// Guess is one player's answer to a place. Pin is nil for a place given up or
// run out of time.
type Guess struct {
	N          int
	M          int
	Pin        *geo.Point
	AnsweredAt time.Time
	Auto       bool
	Km         float64
	PtsKm      int
	PtsBonus   int
	PtsSpeed   int
	CountryOK  bool
	TimeMs     int64
}

func (g Guess) Points() int { return g.PtsKm + g.PtsBonus + g.PtsSpeed }

// Snapshot is a whole game as the database has it.
type Snapshot struct {
	Game    Game
	Members []Member
	Rounds  []Round
	Guesses []Guess
}

func (s *Snapshot) member(w Who) (Member, bool) {
	for _, m := range s.Members {
		if m.Who.Same(w) {
			return m, true
		}
	}
	return Member{}, false
}

func (s *Snapshot) memberNo(m int) (Member, bool) {
	for _, x := range s.Members {
		if x.M == m {
			return x, true
		}
	}
	return Member{}, false
}

func (s *Snapshot) round(n int) (Round, bool) {
	for _, r := range s.Rounds {
		if r.N == n {
			return r, true
		}
	}
	return Round{}, false
}

func (s *Snapshot) last() (Round, bool) {
	if len(s.Rounds) == 0 {
		return Round{}, false
	}
	return s.Rounds[len(s.Rounds)-1], true
}

func (s *Snapshot) guess(n, m int) (Guess, bool) {
	for _, g := range s.Guesses {
		if g.N == n && g.M == m {
			return g, true
		}
	}
	return Guess{}, false
}

// Place is what the free game needs of a place: the picture to show, and the
// answer to judge by and show after.
type Place struct {
	ID        int64
	ImageKey  string
	Kind      string
	Heading   *float64
	Point     geo.Point
	Country   string
	Continent string
	City      map[string]string
	CityShape geo.Shape
	Fact      map[string]string
	Author    string
	License   string
	Source    string
}

// Candidate is a place a game may get next. SeenAt is when one of the game's
// players last had it, nil for a place none of them has seen. Day is a closed
// day's place rather than the free pool's.
type Candidate struct {
	PlaceID int64
	Country string
	SeenAt  *time.Time
	Day     bool
}

type NewGame struct {
	Kind     string
	Code     string
	Settings Settings
	Status   string
	Host     Member
	At       time.Time
}

type NewRound struct {
	N        int
	PlaceID  int64
	Repeat   bool
	StartsAt time.Time
	EndsAt   *time.Time
}

// Result is a finished game's line for one player who keeps records, as the
// profile reads it: a Telegram person's, or an account's site player's.
type Result struct {
	Line
	UserID   int64
	Player   string
	Kind     string
	Settings Settings
	At       time.Time
	// Place and Players are the standing in a party; zero alone.
	Place   int
	Players int
	Beaten  int
}

// ErrNoRow is a store's "not there".
var ErrNoRow = errors.New("not found")

// Store keeps the free game. Each method is one statement or one transaction,
// and every write is guarded so that doing it twice changes nothing.
type Store interface {
	CreateFreeGame(ctx context.Context, g NewGame) (int64, error)
	FreeGameID(ctx context.Context, code string) (int64, error)
	FreeSnapshot(ctx context.Context, id int64) (Snapshot, error)
	// FreeCandidates is every place the game may get next, and the countries
	// its places so far are in.
	// closed is the newest date over in every zone.
	FreeCandidates(ctx context.Context, id int64, closed time.Time) ([]Candidate, map[string]bool, error)
	FreePlace(ctx context.Context, placeID int64) (Place, error)
	// FreeKnown is whether the player has had the place: answered its day,
	// or had it in a free game, as any of their people.
	FreeKnown(ctx context.Context, w Who, placeID int64) (bool, error)
	InsertFreeRound(ctx context.Context, id int64, r NewRound) error
	// InsertFreeGuess is false when the player has answered the place already.
	InsertFreeGuess(ctx context.Context, id int64, g Guess) (bool, error)
	// CloseFreeRound closes a party's place at at, once, and writes the next
	// one when there is one.
	CloseFreeRound(ctx context.Context, id int64, n int, at time.Time, next *NewRound) error
	JoinFree(ctx context.Context, id int64, m Member) error
	RejoinFree(ctx context.Context, id int64, m int) error
	LeaveFree(ctx context.Context, id int64, m int, at time.Time) error
	SeenFree(ctx context.Context, id int64, m int, at time.Time) error
	// SetFreeSettings changes a lobby's settings and moves its version on.
	SetFreeSettings(ctx context.Context, id int64, s Settings) error
	SetFreeHost(ctx context.Context, id int64, m int) error
	// StartFree starts a lobby on the settings of version, and is false when
	// they have changed since or the game is no lobby any more.
	StartFree(ctx context.Context, id int64, version int, kind string, at time.Time, first NewRound) (bool, error)
	// FinishFree ends a game that is playing or in its lobby, with the
	// results of a finished one.
	FinishFree(ctx context.Context, id int64, status string, at time.Time, results []Result) (bool, error)
	// RematchFree is the game after id: the one made already, or g.
	RematchFree(ctx context.Context, id int64, g NewGame) (int64, error)
	// LastFreeSettings is what the player's last solo game was on.
	LastFreeSettings(ctx context.Context, w Who) (Settings, bool, error)
	// OpenFreeGames is the player's games still playing or in a lobby.
	OpenFreeGames(ctx context.Context, w Who, kind string) ([]int64, error)
	// FreeBest is the player's best finished game for every settings key:
	// points for a classic, the length for a streak, leaving out one game.
	FreeBest(ctx context.Context, w Who, except int64) (map[string]int, error)
	// StaleFreeGames is the games still open whose last move was before
	// idle, or whose lobby was made before lobby, for the sweep.
	StaleFreeGames(ctx context.Context, idle, lobby, now time.Time) ([]int64, error)

	// FreeRoundPlace is the place of a game's place n.
	FreeRoundPlace(ctx context.Context, game int64, n int) (int64, error)

	// FreeArchive is every day closed in every zone by closed, newest first,
	// as the player may take it from the archive.
	FreeArchive(ctx context.Context, w Who, closed time.Time) ([]ArchiveDay, error)
	// CreateFreeArchive makes the player's game of closed day dayNo with its
	// place begun at g.At, or gives the game they took the day in before.
	// ErrNoRow is no such closed day; ErrDayAnswered and ErrDaySeen a day the
	// player knows.
	CreateFreeArchive(ctx context.Context, dayNo int, closed time.Time, g NewGame) (int64, error)
	// FreeDayRank is where an answer worth base points would have stood
	// among a closed day's answers, counted without their speed bonus, and
	// of how many, the answer included. A tie counts as behind.
	FreeDayRank(ctx context.Context, date time.Time, base int) (rank, of int, err error)

	// MakeFreeChallenge makes the challenge of a finished game for its
	// player, or gives the code of the one made before.
	MakeFreeChallenge(ctx context.Context, game int64, w Who, code string, at time.Time) (string, error)
	// FreeChallenge is a challenge with every attempt at it, those of w's
	// people marked as theirs.
	FreeChallenge(ctx context.Context, code string, w Who) (ChallengeRow, error)
	// TakeFreeChallenge makes the player's attempt at a challenge, or gives
	// the one they made before. ErrChallengeTaken is an attempt made as
	// another of their people.
	TakeFreeChallenge(ctx context.Context, challenge int64, g NewGame) (int64, error)
	// FreeChallenges is the challenges the player is in that were made since.
	FreeChallenges(ctx context.Context, w Who, since time.Time) ([]ChallengeBrief, error)
}

// ChallengeRow is a challenge as the store has it.
type ChallengeRow struct {
	ID        int64
	Code      string
	Game      int64
	Settings  Settings
	CreatedAt time.Time
	Attempts  []Attempt
}

// Attempt is one player's game at a challenge, summed over the places they
// have answered so far. Exactly one of UserID and Player is set.
type Attempt struct {
	UserID int64
	Player string
	Name   string
	Game   int64
	Status string
	// Maker is the game the challenge was made from.
	Maker bool
	Line  Line
	// Seen is how many of the places the player had had before.
	Seen int
	// Own is an attempt of the asker's people: their own, or one made as
	// another of them, the account's site player before Telegram was linked.
	Own bool
}

func (a Attempt) who() Who { return Who{UserID: a.UserID, Player: a.Player} }
