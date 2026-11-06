// SPDX-License-Identifier: Apache-2.0
package free

import (
	"context"
	"errors"
	"time"
)

// A streak and a game against the clock played together. A streak is the
// classic party's rhythm with players dropping out: the same place at the same
// time for whoever still stands. Against the clock there is no common place:
// one list of places, each player at their own pace through one shared three
// minutes, so nothing but the clock is global and the party's phase is worked
// out for every player alone.

var (
	// ErrOut is a streak place for a player whose streak is over: they only
	// watch.
	ErrOut = &Error{409, "out"}
)

// kept is whether m's answer to place n keeps a streak going: a pin in the
// right country.
func (s *Snapshot) kept(n, m int) bool {
	g, ok := s.guess(n, m)
	return ok && g.Pin != nil && g.CountryOK
}

// standing is who plays streak place n: players still in the party, who came
// in by that place and kept their streak through every place closed before
// it. A place closed without their answer broke it.
func (s *Snapshot) standing(n int) []Member {
	out := []Member{}
	for _, m := range s.Members {
		if m.LeftAt != nil || m.FromRound > n || s.out(m, n) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// out is whether m's streak ended before place n. Every place before n is
// over, closed or being closed.
func (s *Snapshot) out(m Member, n int) bool {
	for _, r := range s.Rounds {
		if r.N >= n || r.N < m.FromRound {
			continue
		}
		if !s.kept(r.N, m.M) {
			return true
		}
	}
	return false
}

func standsAt(standing []Member, m int) bool {
	for _, x := range standing {
		if x.M == m {
			return true
		}
	}
	return false
}

// answers is how many places m has answered, or passed.
func (s *Snapshot) answers(m int) int {
	n := 0
	for _, g := range s.Guesses {
		if g.M == m {
			n++
		}
	}
	return n
}

// more is whether a place follows the closed place r: the next of a classic,
// a streak's while someone stands for it. A pool with no place left ends the
// game.
func (p *party) more(r Round) bool {
	if p.exhausted {
		return false
	}
	set := p.snap.Game.Settings
	if set.Mode == Streak {
		return len(p.snap.standing(r.N+1)) > 0
	}
	return r.N < set.Rounds
}

// timedEndOf is when a party against the clock ends: its time after the first
// place began.
func timedEndOf(snap *Snapshot) (time.Time, bool) {
	first, ok := snap.round(1)
	if !ok || first.EndsAt == nil {
		return time.Time{}, false
	}
	return *first.EndsAt, true
}

// stepTimed is a party against the clock moving on with the clock. Nothing is
// global in it but the end: at the end and the grace the unanswered places are
// written as missed, as a solo game does, and the game is over.
func (s *Service) stepTimed(ctx context.Context, p *party, now time.Time) (bool, error) {
	end, ok := timedEndOf(&p.snap)
	if !ok {
		return false, nil
	}
	over := end.Add(Grace)
	if now.Before(over) && !s.timedFinished(p) {
		return false, nil
	}
	at := over
	if now.Before(over) {
		at = now
	}
	changed := false
	for _, m := range players(&p.snap) {
		n := p.snap.answers(m.M) + 1
		if _, ok := p.snap.round(n); !ok {
			continue
		}
		g := Guess{N: n, M: m.M, AnsweredAt: at, TimeMs: s.timedSince(&p.snap, m, at)}
		if _, err := s.Store.InsertFreeGuess(ctx, p.snap.Game.ID, g); err != nil {
			return false, err
		}
		changed = true
	}
	if changed {
		if err := s.reload(ctx, p); err != nil {
			return false, err
		}
	}
	return true, s.finish(ctx, &p.snap, at)
}

// timedFinished is whether the pool ran dry and everyone who plays has
// answered every place there is: nothing is left to wait for.
func (s *Service) timedFinished(p *party) bool {
	if !p.exhausted {
		return false
	}
	here := false
	for _, m := range s.present(p) {
		here = true
		if _, ok := p.snap.round(p.snap.answers(m.M) + 1); ok {
			return false
		}
	}
	return here
}

// timedSince is how long m has been on their place at at: since their last
// answer, or since the game began.
func (s *Service) timedSince(snap *Snapshot, m Member, at time.Time) int64 {
	since := time.Time{}
	if first, ok := snap.round(1); ok {
		since = first.StartsAt
	}
	for _, g := range snap.Guesses {
		if g.M == m.M && g.AnsweredAt.After(since) {
			since = g.AnsweredAt
		}
	}
	d := max(at.Sub(since), 0)
	if limit := snap.Game.Settings.Limit(); d > limit {
		d = limit
	}
	return d.Milliseconds()
}

// guessTimed takes w's answer to their next place of a game against the
// clock, until the end and the grace. The place after it is drawn when the
// first player gets there, and is the same for everyone.
func (s *Service) guessTimed(ctx context.Context, p *party, me Member, now time.Time, lang string, in GuessIn) (PartyView, error) {
	g := p.snap.Game
	done := p.snap.answers(me.M)
	switch {
	case in.N <= done:
		return PartyView{}, ErrAnswered
	case in.N > done+1:
		return PartyView{}, ErrOrder
	}
	r, ok := p.snap.round(in.N)
	switch {
	case !ok:
		return PartyView{}, ErrOrder
	case now.Before(r.StartsAt):
		return PartyView{}, ErrNotYet
	case now.After(r.Over()):
		return PartyView{}, ErrClosed
	}
	gs := Guess{N: in.N, M: me.M, AnsweredAt: now, TimeMs: s.timedSince(&p.snap, me, now)}
	if !in.Skip {
		pl, err := s.place(ctx, r.PlaceID)
		if err != nil {
			return PartyView{}, err
		}
		pin := *in.Pin
		gs.Pin, gs.Auto = &pin, in.Auto
		gs = s.score(pl, g.Settings, r, gs)
	}
	if _, err := s.Store.InsertFreeGuess(ctx, g.ID, gs); err != nil {
		return PartyView{}, err
	}
	// An answer in the grace after the time has run out is taken, but there is
	// no place to go on to.
	if _, ok := p.snap.round(in.N + 1); !ok && !p.exhausted && now.Before(*r.EndsAt) {
		c, repeat, err := s.pick(ctx, g.ID)
		switch {
		case errors.Is(err, ErrNoPlace):
			s.Log.Warn("free pool has no place left for a party", "game", g.ID)
			p.exhausted = true
		case err != nil:
			return PartyView{}, err
		default:
			next := NewRound{N: in.N + 1, PlaceID: c.PlaceID, Repeat: repeat, StartsAt: now, EndsAt: r.EndsAt}
			if err := s.Store.InsertFreeRound(ctx, g.ID, next); err != nil {
				return PartyView{}, err
			}
		}
	}
	if err := s.reload(ctx, p); err != nil {
		return PartyView{}, err
	}
	return s.partyView(ctx, p, me, now, lang)
}

// dayOpen is whether w may play closed day in a party: not one they answered
// or had the place of, as the archive asks of a solo game.
func (s *Service) dayOpen(ctx context.Context, w Who, day time.Time) error {
	answered, seen, err := s.Store.FreeDayKnown(ctx, w, day)
	switch {
	case err != nil:
		return err
	case answered:
		return ErrDayAnswered
	case seen:
		return ErrDaySeen
	}
	return nil
}

// timedView is a party against the clock as one of its players sees it while
// it is played: the place they are on and the one they have just answered,
// with the answer, and who is where in numbers, in the members. The places
// the others have reached and their pins stay in the database until the game
// is over for everyone.
func (s *Service) timedView(ctx context.Context, p *party, me Member, v PartyView, lang string) (PartyView, error) {
	g := p.snap.Game
	if end, ok := timedEndOf(&p.snap); ok {
		end = end.UTC()
		v.EndsAt = &end
	}
	done := p.snap.answers(me.M)
	for _, n := range []int{done, done + 1} {
		r, ok := p.snap.round(n)
		if !ok {
			continue
		}
		pl, err := s.place(ctx, r.PlaceID)
		if err != nil {
			return PartyView{}, err
		}
		pr := PartyRound{N: r.N, Image: s.imagePath(g.ID, r.N), Kind: pl.Kind, Heading: pl.Heading,
			StartsAt: r.StartsAt.UTC(), EndsAt: utc(r.EndsAt), Repeat: r.Repeat}
		if mine, ok := p.snap.guess(r.N, me.M); ok {
			place := placeView(pl, lang)
			at := mine.AnsweredAt.UTC()
			pr.ClosedAt, pr.Place, pr.Guesses = &at, &place, []GuessView{guessView(mine)}
		}
		v.Rounds = append(v.Rounds, pr)
	}
	return v, nil
}
