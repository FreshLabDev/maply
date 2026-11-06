// SPDX-License-Identifier: Apache-2.0
package free

import (
	"context"
	"errors"
	"time"
)

// A challenge is a party for friends who are not online at once: one person
// plays a solo classic on a timer and makes a challenge of it, and everyone
// who takes it plays those places, in that order, on those settings, once and
// in their own time. An attempt is a game of its own with its own clock,
// played as a solo game is: nobody learns a place before starting it, and the
// challenge shows the attempts' sums only, never a place. A challenge is
// for players who keep records: a Telegram person or an account on the
// site. A guest of the site is asked to sign in, even to look: the table
// names the players.
//
// A place the player has had before is played all the same and marked: the
// places were drawn for the maker, and a friend who has played a lot will
// know some of them. The standings say how many.

// ChallengeShown is how long a challenge stays on the free screen of the
// people in it.
const ChallengeShown = 7 * 24 * time.Hour

// ChallengeBrief is a challenge on the free screen.
type ChallengeBrief struct {
	Code     string    `json:"code"`
	By       string    `json:"by"`
	Settings Settings  `json:"settings"`
	Players  int       `json:"players"`
	Maker    bool      `json:"maker,omitempty"`
	Status   string    `json:"status"`
	At       time.Time `json:"at"`
}

// ChallengeView is a challenge as anyone with its code sees it: the settings,
// who made it and every attempt's sums.
type ChallengeView struct {
	Code     string          `json:"code"`
	Settings Settings        `json:"settings"`
	By       string          `json:"by"`
	Lines    []ChallengeLine `json:"lines"`
	// Mine is the asker's game at the challenge, nil before they take it or
	// when they took it as another of their people, and Status how it
	// stands. Their line is marked Me either way.
	Mine   *int64    `json:"mine"`
	Status string    `json:"status,omitempty"`
	Now    time.Time `json:"now"`
}

// In is whether the asker has an attempt at the challenge, as any of their
// people: whoever is in it may send it on.
func (v ChallengeView) In() bool {
	for _, l := range v.Lines {
		if l.Me {
			return true
		}
	}
	return v.Mine != nil
}

// ChallengeLine is one attempt: its sums so far, ranked once finished.
type ChallengeLine struct {
	Line
	Name   string `json:"name"`
	Me     bool   `json:"me,omitempty"`
	Maker  bool   `json:"maker,omitempty"`
	Status string `json:"status"`
	Seen   int    `json:"seen,omitempty"`
}

var (
	// ErrNotDone is a game that is not over yet.
	ErrNotDone = &Error{409, "not_done"}
	// ErrChallengeTaken is a challenge the person took as another of their
	// people: as the account's site player before linking Telegram.
	ErrChallengeTaken = &Error{409, "challenge_taken"}
)

// MakeChallenge makes a challenge of a solo classic on a timer that w has
// finished, or gives the one made of it before.
func (s *Service) MakeChallenge(ctx context.Context, w Who, id int64) (string, error) {
	s.init()
	if !w.Keeps() {
		return "", ErrSignIn
	}
	unlock := s.lockSolo(id)
	defer unlock()
	now := s.Now()
	snap, err := s.soloSnap(ctx, w, id, now)
	if err != nil {
		return "", err
	}
	g := snap.Game
	if g.Kind != Solo || g.Settings.Mode != Classic || g.Settings.Seconds == 0 {
		return "", ErrBadRequest
	}
	if g.Status != StatusDone {
		return "", ErrNotDone
	}
	if g.Challenge != "" {
		return g.Challenge, nil
	}
	for i := 0; ; i++ {
		code, err := s.Store.MakeFreeChallenge(ctx, id, w, newCode(), now)
		if errors.Is(err, ErrCodeTaken) && i < 3 {
			continue
		}
		return code, err
	}
}

// ChallengeOf is the challenge with code as w sees it. Their own attempt is
// brought up to the clock first, so a game whose time ran out reads as over.
func (s *Service) ChallengeOf(ctx context.Context, w Who, code string) (ChallengeView, error) {
	s.init()
	// An account that has not played yet has no site player, and may look
	// all the same.
	if !w.Keeps() && !w.Account {
		return ChallengeView{}, ErrSignIn
	}
	if !ValidCode(code) {
		return ChallengeView{}, ErrNotFound
	}
	c, err := s.Store.FreeChallenge(ctx, code, w)
	if errors.Is(err, ErrNoRow) {
		return ChallengeView{}, ErrNotFound
	}
	if err != nil {
		return ChallengeView{}, err
	}
	if a, ok := c.attempt(w); ok && a.Status == StatusPlaying {
		if _, err := s.SoloGame(ctx, w, a.Game, ""); err != nil {
			return ChallengeView{}, err
		}
		if c, err = s.Store.FreeChallenge(ctx, code, w); err != nil {
			return ChallengeView{}, err
		}
	}
	return challengeView(c, w, s.Now()), nil
}

// attempt is w's own attempt, the one they may open and play.
func (c ChallengeRow) attempt(w Who) (Attempt, bool) {
	if !w.Keeps() {
		return Attempt{}, false
	}
	for _, a := range c.Attempts {
		if a.who().Same(w) {
			return a, true
		}
	}
	return Attempt{}, false
}

// taken is whether any of the asker's people has an attempt.
func (c ChallengeRow) taken() bool {
	for _, a := range c.Attempts {
		if a.Own {
			return true
		}
	}
	return false
}

// challengeView ranks the finished attempts, best first, and puts the rest
// after them as they came.
func challengeView(c ChallengeRow, w Who, now time.Time) ChallengeView {
	v := ChallengeView{Code: c.Code, Settings: c.Settings, Lines: []ChallengeLine{}, Now: now.UTC()}
	done := []Line{}
	for i, a := range c.Attempts {
		if a.Maker {
			v.By = a.Name
		}
		if a.Status == StatusDone {
			l := a.Line
			l.M = i + 1
			done = append(done, l)
		}
	}
	Rank(done)
	for _, l := range done {
		a := c.Attempts[l.M-1]
		l.M = 0
		v.Lines = append(v.Lines, ChallengeLine{Line: l, Name: a.Name, Me: a.Own,
			Maker: a.Maker, Status: a.Status, Seen: a.Seen})
	}
	for _, a := range c.Attempts {
		if a.Status == StatusDone {
			continue
		}
		l := a.Line
		l.M, l.Rank, l.Beaten = 0, 0, 0
		v.Lines = append(v.Lines, ChallengeLine{Line: l, Name: a.Name, Me: a.Own,
			Maker: a.Maker, Status: a.Status, Seen: a.Seen})
	}
	if a, ok := c.attempt(w); ok {
		id := a.Game
		v.Mine, v.Status = &id, a.Status
	}
	return v
}

// TakeChallenge makes w's attempt at the challenge with code, or gives the
// one they have: a challenge is played once.
func (s *Service) TakeChallenge(ctx context.Context, w Who, name, code, lang string) (SoloView, error) {
	s.init()
	if !w.Keeps() {
		return SoloView{}, ErrSignIn
	}
	if !ValidCode(code) {
		return SoloView{}, ErrNotFound
	}
	c, err := s.Store.FreeChallenge(ctx, code, w)
	if errors.Is(err, ErrNoRow) {
		return SoloView{}, ErrNotFound
	}
	if err != nil {
		return SoloView{}, err
	}
	id := int64(0)
	if a, ok := c.attempt(w); ok {
		id = a.Game
	} else if c.taken() {
		return SoloView{}, ErrChallengeTaken
	} else {
		if name == "" {
			name = "?"
		}
		now := s.Now()
		id, err = s.Store.TakeFreeChallenge(ctx, c.ID, NewGame{Kind: Challenge, Settings: c.Settings, Status: StatusPlaying, At: now,
			Host: Member{M: 1, Who: w, Name: name, JoinedAt: now, FromRound: 1, LastSeen: now}})
		if err != nil {
			return SoloView{}, err
		}
	}
	return s.SoloGame(ctx, w, id, lang)
}
