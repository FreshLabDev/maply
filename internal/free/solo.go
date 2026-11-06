// SPDX-License-Identifier: Apache-2.0
package free

import (
	"context"
	"errors"
	"time"

	"github.com/FreshLabDev/maply/internal/geo"
)

// A solo game is one player and no lobby. Its places are drawn one at a time,
// when the player starts each, so two games played side by side cannot hand
// one place to both before either is seen.

// Home is the solo screen: the settings to offer, a game still going, and the
// player's best for each set of settings.
type Home struct {
	// Last is the settings of the player's last solo game, the screen's
	// default.
	Last Settings `json:"last"`
	// Open is a solo game the player has not finished.
	Open *int64 `json:"open"`
	// Best is the player's best for every settings set they finished.
	Best map[string]int `json:"best"`
	// Party is the code of a party the player is in, to go back to.
	Party string `json:"party,omitempty"`
	// Archive is how many closed days the player may still take.
	Archive int `json:"archive"`
	// Challenges are the latest challenges the person is in.
	Challenges []ChallengeBrief `json:"challenges"`
}

// SoloRound is a place of a solo game that has been answered.
type SoloRound struct {
	N      int       `json:"n"`
	Repeat bool      `json:"repeat,omitempty"`
	Place  PlaceView `json:"place"`
	Guess  GuessView `json:"guess"`
	// Late is a pin that came after the time and its grace: the place counts
	// as missed.
	Late bool `json:"late,omitempty"`
}

// SoloView is a solo game as its player may see it.
type SoloView struct {
	ID       int64    `json:"id"`
	Settings Settings `json:"settings"`
	Status   string   `json:"status"`
	// Done is every place answered, with where it was.
	Done []SoloRound `json:"done"`
	// Current is the place started and not answered yet.
	Current *RoundView `json:"current"`
	// Next is the number of the place to start next, 0 when there is none.
	Next   int `json:"next"`
	Points int `json:"points"`
	Streak int `json:"streak"`
	// Countries is how many places had the right country: the score against
	// the clock.
	Countries int `json:"countries"`
	// EndsAt is when a timed game ends, once its first place has begun.
	EndsAt *time.Time `json:"ends_at,omitempty"`
	// Day is the closed day a game from the archive plays.
	Day *DayView `json:"day,omitempty"`
	// Challenge is the code of the challenge an attempt is at, or of the one
	// made from this game.
	Challenge string    `json:"challenge,omitempty"`
	Final     *Final    `json:"final,omitempty"`
	Now       time.Time `json:"now"`
}

// DayView names a closed day: its number and date.
type DayView struct {
	N    int    `json:"n"`
	Date string `json:"date"`
}

// Final is a finished game's sum, and where it stands against the player's
// best on these settings. Best is the best before this game.
type Final struct {
	Line
	Key    string `json:"key"`
	Best   *int   `json:"best"`
	Record bool   `json:"record"`
	// DayRank is where a day from the archive would have stood among the
	// day's answers, and DayOf of how many, this one included.
	DayRank int `json:"day_rank,omitempty"`
	DayOf   int `json:"day_of,omitempty"`
}

// GuessIn is an answer: a pin, or nothing for a place given up or run out of
// time. Auto is a pin the app sent itself at zero.
type GuessIn struct {
	N    int
	Pin  *geo.Point
	Auto bool
	Skip bool
}

func (s *Service) lockSolo(id int64) func() {
	m := &s.solo[uint64(id)%uint64(len(s.solo))]
	m.Lock()
	return m.Unlock
}

// SoloHome is the solo screen for w.
func (s *Service) SoloHome(ctx context.Context, w Who) (Home, error) {
	s.init()
	h := Home{Last: DefaultSolo, Best: map[string]int{}, Challenges: []ChallengeBrief{}}
	if !w.Valid() {
		days, err := s.Store.FreeArchive(ctx, w, s.closed(s.Now()))
		h.Archive = len(days)
		return h, err
	}
	last, ok, err := s.Store.LastFreeSettings(ctx, w)
	if err != nil {
		return Home{}, err
	}
	if ok {
		h.Last = last
	}
	days, err := s.Store.FreeArchive(ctx, w, s.closed(s.Now()))
	if err != nil {
		return Home{}, err
	}
	for _, d := range days {
		if d.State == DayOpen {
			h.Archive++
		}
	}
	open, err := s.Store.OpenFreeGames(ctx, w, Solo)
	if err != nil {
		return Home{}, err
	}
	for _, id := range open {
		v, err := s.SoloGame(ctx, w, id, "")
		if err != nil {
			return Home{}, err
		}
		if v.Status == StatusPlaying && h.Open == nil {
			h.Open = &id
		}
	}
	if w.Keeps() {
		if h.Best, err = s.Store.FreeBest(ctx, w, 0); err != nil {
			return Home{}, err
		}
		if h.Party, err = s.openParty(ctx, w); err != nil {
			return Home{}, err
		}
		if h.Challenges, err = s.Store.FreeChallenges(ctx, w, s.Now().Add(-ChallengeShown)); err != nil {
			return Home{}, err
		}
	}
	return h, nil
}

// openParty is the code of a party w is in and has not left, if any.
func (s *Service) openParty(ctx context.Context, w Who) (string, error) {
	open, err := s.Store.OpenFreeGames(ctx, w, Party)
	if err != nil {
		return "", err
	}
	for _, id := range open {
		p, err := s.load(ctx, id)
		if err != nil {
			return "", err
		}
		p.mu.Lock()
		err = s.advance(ctx, p, s.Now())
		g := p.snap.Game
		m, ok := p.snap.member(w)
		p.mu.Unlock()
		if err != nil {
			return "", err
		}
		if ok && m.LeftAt == nil && (g.Status == StatusLobby || g.Status == StatusPlaying) {
			return g.Code, nil
		}
	}
	return "", nil
}

// NewSolo starts a solo game on set. A game the player still had going is
// given up: one solo game at a time.
func (s *Service) NewSolo(ctx context.Context, w Who, name string, set Settings) (SoloView, error) {
	s.init()
	if !w.Valid() || !set.ValidSolo() {
		return SoloView{}, ErrBadRequest
	}
	now := s.Now()
	open, err := s.Store.OpenFreeGames(ctx, w, Solo)
	if err != nil {
		return SoloView{}, err
	}
	for _, id := range open {
		if err := s.giveUp(ctx, id, now); err != nil {
			return SoloView{}, err
		}
	}
	if name == "" {
		name = "?"
	}
	id, err := s.create(ctx, NewGame{Kind: Solo, Settings: set, Status: StatusPlaying, At: now,
		Host: Member{M: 1, Who: w, Name: name, JoinedAt: now, FromRound: 1, LastSeen: now}})
	if err != nil {
		return SoloView{}, err
	}
	return s.SoloGame(ctx, w, id, "")
}

// giveUp settles an old solo game and abandons it if it is still going.
func (s *Service) giveUp(ctx context.Context, id int64, now time.Time) error {
	unlock := s.lockSolo(id)
	defer unlock()
	snap, err := s.Store.FreeSnapshot(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.settle(ctx, &snap, now); err != nil {
		return err
	}
	if snap.Game.Status == StatusPlaying {
		_, err = s.Store.FinishFree(ctx, id, StatusAbandoned, now, nil)
	}
	return err
}

// soloSnap reads a solo game of w's and brings it up to the clock.
func (s *Service) soloSnap(ctx context.Context, w Who, id int64, now time.Time) (Snapshot, error) {
	snap, err := s.Store.FreeSnapshot(ctx, id)
	if errors.Is(err, ErrNoRow) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	if !alone(snap.Game.Kind) {
		return Snapshot{}, ErrNotFound
	}
	if _, ok := snap.member(w); !ok {
		return Snapshot{}, ErrNotMember
	}
	if _, err := s.settle(ctx, &snap, now); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// settle writes what the clock has decided: a place whose time and grace ran
// out without an answer is a miss, a game whose last place is answered is
// finished, and a game untouched for a day is abandoned, but for a day from
// the archive. snap is read again after a change.
func (s *Service) settle(ctx context.Context, snap *Snapshot, now time.Time) (bool, error) {
	if snap.Game.Status != StatusPlaying {
		return false, nil
	}
	changed := false
	if r, ok := snap.last(); ok && r.EndsAt != nil && now.After(r.Over()) {
		if _, answered := snap.guess(r.N, 1); !answered {
			at := r.Over()
			g := Guess{N: r.N, M: 1, AnsweredAt: at, TimeMs: r.EndsAt.Sub(r.StartsAt).Milliseconds()}
			if _, err := s.Store.InsertFreeGuess(ctx, snap.Game.ID, g); err != nil {
				return false, err
			}
			changed = true
		}
	}
	if changed {
		if err := s.reread(ctx, snap); err != nil {
			return false, err
		}
	}
	if at, over := soloOver(snap, now); over {
		if err := s.finish(ctx, snap, at); err != nil {
			return false, err
		}
		return true, s.reread(ctx, snap)
	}
	// A day from the archive has no clock and is taken once: it waits for its
	// answer however long it takes, or the day could never be played again.
	if snap.Game.Settings.Mode != Archive && now.Sub(lastMove(snap)) > AbandonAfter {
		if _, err := s.Store.FinishFree(ctx, snap.Game.ID, StatusAbandoned, now, nil); err != nil {
			return false, err
		}
		return true, s.reread(ctx, snap)
	}
	return changed, nil
}

func (s *Service) reread(ctx context.Context, snap *Snapshot) error {
	fresh, err := s.Store.FreeSnapshot(ctx, snap.Game.ID)
	if err != nil {
		return err
	}
	*snap = fresh
	return nil
}

// soloOver is whether a solo game has no place left to play, and when it
// ended: at the answer to every place of a classic, to the first miss of a
// streak, and when the time of a timed game ran out.
func soloOver(snap *Snapshot, now time.Time) (time.Time, bool) {
	r, ok := snap.last()
	if !ok {
		return time.Time{}, false
	}
	g, answered := snap.guess(r.N, 1)
	if !answered {
		return time.Time{}, false
	}
	set := snap.Game.Settings
	switch set.Mode {
	case Streak:
		return g.AnsweredAt, !g.CountryOK
	case Timed:
		end, _ := timedEnd(snap)
		return later(end, g.AnsweredAt), !now.Before(end)
	}
	return g.AnsweredAt, r.N >= set.Rounds
}

// timedEnd is when a timed game ends: its time after its first place began.
func timedEnd(snap *Snapshot) (time.Time, bool) {
	first, ok := snap.round(1)
	if !ok || snap.Game.Settings.Mode != Timed {
		return time.Time{}, false
	}
	return first.StartsAt.Add(snap.Game.Settings.Limit()), true
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// SoloGame is a solo game as its player sees it now, its places named in
// lang.
func (s *Service) SoloGame(ctx context.Context, w Who, id int64, lang string) (SoloView, error) {
	s.init()
	unlock := s.lockSolo(id)
	defer unlock()
	now := s.Now()
	snap, err := s.soloSnap(ctx, w, id, now)
	if err != nil {
		return SoloView{}, err
	}
	return s.soloView(ctx, &snap, w, now, lang)
}

// StartSolo starts place n: it writes when the place began and when it ends,
// and only then gives the picture. Starting it again gives the same picture
// and what is left of the same time, so a reload does not reset the clock.
// From the start the place counts as seen.
func (s *Service) StartSolo(ctx context.Context, w Who, id int64, n int) (RoundView, error) {
	s.init()
	unlock := s.lockSolo(id)
	defer unlock()
	now := s.Now()
	snap, err := s.soloSnap(ctx, w, id, now)
	if err != nil {
		return RoundView{}, err
	}
	if r, ok := snap.round(n); ok {
		p, err := s.place(ctx, r.PlaceID)
		if err != nil {
			return RoundView{}, err
		}
		return s.roundView(id, r, p, now), nil
	}
	if snap.Game.Status != StatusPlaying {
		return RoundView{}, ErrOver
	}
	if n != nextSolo(&snap, now) {
		return RoundView{}, ErrOrder
	}
	place, repeat, err := s.placeFor(ctx, &snap, w, n)
	if err != nil {
		return RoundView{}, err
	}
	nr := NewRound{N: n, PlaceID: place, Repeat: repeat, StartsAt: now}
	if end, ok := timedEnd(&snap); ok {
		nr.EndsAt = &end
	} else if limit := snap.Game.Settings.Limit(); limit > 0 {
		ends := now.Add(limit)
		nr.EndsAt = &ends
	}
	if err := s.Store.InsertFreeRound(ctx, id, nr); err != nil {
		return RoundView{}, err
	}
	if err := s.reread(ctx, &snap); err != nil {
		return RoundView{}, err
	}
	r, ok := snap.round(n)
	if !ok {
		return RoundView{}, ErrOrder
	}
	p, err := s.place(ctx, r.PlaceID)
	if err != nil {
		return RoundView{}, err
	}
	return s.roundView(id, r, p, now), nil
}

// placeFor is the place a solo game gets as its place n, and whether the
// player has seen it: an attempt at a challenge plays the places of the game
// it was made from, in their order, and every other game draws one.
func (s *Service) placeFor(ctx context.Context, snap *Snapshot, w Who, n int) (int64, bool, error) {
	if snap.Game.From == 0 {
		c, repeat, err := s.pick(ctx, snap.Game.ID)
		return c.PlaceID, repeat, err
	}
	place, err := s.Store.FreeRoundPlace(ctx, snap.Game.From, n)
	if errors.Is(err, ErrNoRow) {
		return 0, false, ErrOver
	}
	if err != nil {
		return 0, false, err
	}
	known, err := s.Store.FreeKnown(ctx, w, place)
	return place, known, err
}

// nextSolo is the place a solo game may start next: one past the last, once
// the last is answered or over. 0 when the game has no place left. A day from
// the archive has its place from the start.
func nextSolo(snap *Snapshot, now time.Time) int {
	if snap.Game.Status != StatusPlaying {
		return 0
	}
	r, ok := snap.last()
	if !ok {
		return 1
	}
	if _, answered := snap.guess(r.N, 1); !answered {
		return 0
	}
	if _, over := soloOver(snap, now); over {
		return 0
	}
	return r.N + 1
}

// GuessSolo takes the answer to place n. A pin counts until the end and the
// grace; one that comes later is a miss. Nothing is given up while the time
// runs, except without a timer, where giving up is the way out: 0 points and
// the answer. Where the place was comes back with the answer and nowhere
// before it.
func (s *Service) GuessSolo(ctx context.Context, w Who, id int64, in GuessIn, lang string) (SoloView, error) {
	s.init()
	unlock := s.lockSolo(id)
	defer unlock()
	now := s.Now()
	snap, err := s.soloSnap(ctx, w, id, now)
	if err != nil {
		return SoloView{}, err
	}
	r, ok := snap.round(in.N)
	if !ok {
		return SoloView{}, ErrOrder
	}
	switch {
	// Against the clock a place may be passed: the time it would cost runs
	// on all the same.
	case in.Skip && r.EndsAt != nil && now.Before(*r.EndsAt) && snap.Game.Settings.Mode != Timed:
		return SoloView{}, ErrBadRequest
	case !in.Skip && (in.Pin == nil || !in.Pin.Valid()):
		return SoloView{}, ErrBadRequest
	}
	if prev, answered := snap.guess(in.N, 1); answered {
		// A pin after the time and its grace finds the place settled as a
		// miss: it gets the answer, and nothing for the pin.
		if late := r.EndsAt != nil && now.After(r.Over()); late && prev.Pin == nil && !in.Skip {
			v, err := s.soloView(ctx, &snap, w, now, lang)
			if err == nil {
				v.markLate(in.N)
			}
			return v, err
		}
		return SoloView{}, ErrAnswered
	}
	if snap.Game.Status != StatusPlaying {
		return SoloView{}, ErrOver
	}
	set := snap.Game.Settings
	g := Guess{N: in.N, M: 1, AnsweredAt: now, TimeMs: timeOf(set, r, now)}
	if !in.Skip {
		p, err := s.place(ctx, r.PlaceID)
		if err != nil {
			return SoloView{}, err
		}
		pin := *in.Pin
		g.Pin, g.Auto = &pin, in.Auto
		g = s.score(p, set, r, g)
	}
	if _, err := s.Store.InsertFreeGuess(ctx, id, g); err != nil {
		return SoloView{}, err
	}
	if err := s.reread(ctx, &snap); err != nil {
		return SoloView{}, err
	}
	if _, err := s.settle(ctx, &snap, now); err != nil {
		return SoloView{}, err
	}
	return s.soloView(ctx, &snap, w, now, lang)
}

func (v *SoloView) markLate(n int) {
	for i := range v.Done {
		if v.Done[i].N == n {
			v.Done[i].Late = true
		}
	}
}

func (s *Service) soloView(ctx context.Context, snap *Snapshot, w Who, now time.Time, lang string) (SoloView, error) {
	g := snap.Game
	v := SoloView{ID: g.ID, Settings: g.Settings, Status: g.Status, Done: []SoloRound{}, Next: nextSolo(snap, now), Now: now.UTC()}
	lines := Lines(g.Settings, snap.Members, snap.Rounds, snap.Guesses)
	if len(lines) == 1 {
		v.Points, v.Streak, v.Countries = lines[0].Points, lines[0].Streak, lines[0].Countries
	}
	if end, ok := timedEnd(snap); ok {
		end = end.UTC()
		v.EndsAt = &end
	}
	if g.DayNo != 0 {
		v.Day = &DayView{N: g.DayNo, Date: g.Day.Format(time.DateOnly)}
	}
	v.Challenge = g.Challenge
	for _, r := range snap.Rounds {
		p, err := s.place(ctx, r.PlaceID)
		if err != nil {
			return SoloView{}, err
		}
		gs, answered := snap.guess(r.N, 1)
		if !answered {
			rv := s.roundView(g.ID, r, p, now)
			v.Current = &rv
			continue
		}
		v.Done = append(v.Done, SoloRound{N: r.N, Repeat: r.Repeat, Place: placeView(p, lang), Guess: guessView(gs)})
	}
	if g.Status == StatusDone && len(lines) == 1 {
		f := &Final{Line: lines[0], Key: g.Settings.Key()}
		// An attempt at a challenge is no record: its places were drawn for
		// someone else, who may have told the player where they are.
		if w.Keeps() && g.Settings.Mode != Archive && g.Kind != Challenge {
			best, err := s.Store.FreeBest(ctx, w, g.ID)
			if err != nil {
				return SoloView{}, err
			}
			if b, ok := best[f.Key]; ok {
				f.Best = &b
				f.Record = Score(g.Settings, f.Line) > b
			}
		}
		if g.Settings.Mode == Archive && f.Answered > 0 {
			var err error
			if f.DayRank, f.DayOf, err = s.Store.FreeDayRank(ctx, g.Day, f.Base); err != nil {
				return SoloView{}, err
			}
		}
		v.Final = f
	}
	return v, nil
}

// Score is what a finished game is measured by: the points of a classic, the
// length of a streak, the places with the right country against the clock.
func Score(s Settings, l Line) int {
	switch s.Mode {
	case Streak:
		return l.Streak
	case Timed:
		return l.Countries
	}
	return l.Points
}
