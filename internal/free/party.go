// SPDX-License-Identifier: Apache-2.0
package free

import (
	"context"
	"errors"
	"sync"
	"time"
)

// A party is up to eight people on the same places at once. Every player asks
// every two seconds what has changed. The party's version lives in this
// process, so a poll that finds it the same costs no database read; the phase
// is worked out from the clock on every read, and whatever the clock decides
// (a place closed, the next one drawn, the game over) is written by the first
// read that sees it, guarded so that a second write changes nothing. A restart
// in the middle of a game loses nothing: the times are in the database.

// seenEvery is how often a poll writes that its player is still there.
const seenEvery = 10 * time.Second

// keepFor is how long a party nobody asks about stays in memory.
const keepFor = 15 * time.Minute

type party struct {
	mu      sync.Mutex
	snap    Snapshot
	version int64
	// seen is when each player last polled, saved when they last wrote it.
	seen  map[int]time.Time
	saved map[int]time.Time
	used  time.Time
	// exhausted is a game the pool has no next place for: it ends early.
	exhausted bool
}

func (p *party) seenAt(m Member) time.Time {
	if t := p.seen[m.M]; t.After(m.LastSeen) {
		return t
	}
	return m.LastSeen
}

func (p *party) answered(n, m int) bool {
	_, ok := p.snap.guess(n, m)
	return ok
}

// PartyView is a party as one of its players may see it. During a place it
// says who has answered and nothing else; a place's answer and everyone's
// pins come once it has closed.
type PartyView struct {
	V        int64     `json:"v"`
	Now      time.Time `json:"now"`
	Code     string    `json:"code"`
	Status   string    `json:"status"`
	Kind     string    `json:"kind"`
	Settings Settings  `json:"settings"`
	// Version is the settings' version, which a start names.
	Version int `json:"version"`
	// Length is how long the game lasts if everyone answers at the end of
	// the time, in seconds; Reveal, Countdown and Grace are its phases.
	Length    int          `json:"length"`
	Reveal    int          `json:"reveal"`
	Countdown int          `json:"countdown"`
	Grace     int          `json:"grace"`
	Host      int          `json:"host"`
	Me        int          `json:"me"`
	Members   []MemberView `json:"members"`
	StartedAt *time.Time   `json:"started_at"`
	Rounds    []PartyRound `json:"rounds"`
	// Totals is the standing over the closed places; the final one once the
	// game is done.
	Totals []Line `json:"totals"`
	// Best is the game's best answer to a place so far.
	Best *GuessView `json:"best,omitempty"`
	// Next is the rematch's code, once someone has asked for one.
	Next string `json:"next,omitempty"`
}

type MemberView struct {
	M    int    `json:"m"`
	Name string `json:"name"`
	From int    `json:"from"`
	// Away is a player who missed the last two places: the party does not
	// wait for them.
	Away bool `json:"away,omitempty"`
	Left bool `json:"left,omitempty"`
}

type PartyRound struct {
	N        int        `json:"n"`
	Image    string     `json:"image"`
	Kind     string     `json:"kind"`
	Heading  *float64   `json:"heading"`
	StartsAt time.Time  `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
	ClosedAt *time.Time `json:"closed_at"`
	Repeat   bool       `json:"repeat,omitempty"`
	// Answered is who has answered, while the place is open.
	Answered []int `json:"answered,omitempty"`
	// Place and Guesses come when it has closed.
	Place   *PlaceView  `json:"place,omitempty"`
	Guesses []GuessView `json:"guesses,omitempty"`
}

// Short is a poll's answer when nothing has changed.
type Short struct {
	V   int64     `json:"v"`
	Now time.Time `json:"now"`
}

func (s *Service) cached(id int64) *party {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.parties[id]
}

func (s *Service) load(ctx context.Context, id int64) (*party, error) {
	if p := s.cached(id); p != nil {
		return p, nil
	}
	snap, err := s.Store.FreeSnapshot(ctx, id)
	if errors.Is(err, ErrNoRow) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if snap.Game.Kind == Solo {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.parties[id]; p != nil {
		return p, nil
	}
	now := s.Now()
	p := &party{snap: snap, version: s.base, seen: map[int]time.Time{}, saved: map[int]time.Time{}, used: now}
	// The database has when a player was last seen to within seenEvery. A
	// player it saw that recently counts as here for now, so that the first
	// answer after a restart does not close a place on people still playing.
	for _, m := range snap.Members {
		if m.LeftAt == nil && now.Sub(m.LastSeen) <= seenEvery+Silent {
			p.seen[m.M] = now
		}
	}
	s.parties[id] = p
	s.codes[snap.Game.Code] = id
	return p, nil
}

func (s *Service) byCode(ctx context.Context, code string) (*party, error) {
	if !ValidCode(code) {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	id, ok := s.codes[code]
	s.mu.Unlock()
	if !ok {
		var err error
		id, err = s.Store.FreeGameID(ctx, code)
		if errors.Is(err, ErrNoRow) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	return s.load(ctx, id)
}

// reload reads the party again after a write and moves its version on.
func (s *Service) reload(ctx context.Context, p *party) error {
	snap, err := s.Store.FreeSnapshot(ctx, p.snap.Game.ID)
	if err != nil {
		return err
	}
	p.snap = snap
	p.version++
	return nil
}

func (s *Service) evict(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.parties {
		if now.Sub(p.used) > keepFor {
			delete(s.parties, id)
			delete(s.codes, p.snap.Game.Code)
		}
	}
}

// advance writes what the clock has decided since the party was last read.
func (s *Service) advance(ctx context.Context, p *party, now time.Time) error {
	for range 4 {
		changed, err := s.step(ctx, p, now)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if err := s.reload(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) step(ctx context.Context, p *party, now time.Time) (bool, error) {
	g := p.snap.Game
	switch g.Status {
	case StatusLobby:
		present := s.present(p)
		if len(present) == 0 || now.Sub(lastMove(&p.snap)) > LobbyFor {
			_, err := s.Store.FinishFree(ctx, g.ID, StatusAbandoned, now, nil)
			return true, err
		}
		host, ok := p.snap.memberNo(g.Host)
		if ok && host.LeftAt == nil && now.Sub(p.seenAt(host)) <= HostAfter {
			return false, nil
		}
		// The host left or went silent: the next player by joining time who
		// is still there takes over.
		for _, m := range present {
			if m.M != g.Host && now.Sub(p.seenAt(m)) <= HostAfter {
				return true, s.Store.SetFreeHost(ctx, g.ID, m.M)
			}
		}
		return false, nil
	case StatusPlaying:
		r, ok := p.snap.last()
		if !ok {
			return false, nil
		}
		if r.ClosedAt == nil {
			if now.Before(r.StartsAt) {
				return false, nil
			}
			at := time.Time{}
			if over := r.EndsAt.Add(Grace); !now.Before(over) {
				at = over
			} else if s.allAnswered(p, r, now) {
				at = now
			}
			if at.IsZero() {
				return false, nil
			}
			return true, s.close(ctx, p, r, at)
		}
		if r.N >= g.Settings.Rounds || p.exhausted {
			end := r.ClosedAt.Add(RevealFor(g.Settings.Seconds))
			if now.Before(end) {
				return false, nil
			}
			return true, s.finish(ctx, &p.snap, end)
		}
		// A close whose next place was not written: write it now.
		return true, s.close(ctx, p, r, *r.ClosedAt)
	}
	return false, nil
}

// present is who is in the party now, by joining time.
func (s *Service) present(p *party) []Member {
	out := []Member{}
	for _, m := range p.snap.Members {
		if m.LeftAt == nil {
			out = append(out, m)
		}
	}
	return out
}

// allAnswered is whether the place may close before its time: someone has
// answered, and nobody it waits for has not. It does not wait for a player
// who left, joined for a later place, has been silent for more than Silent,
// or missed the last two places.
func (s *Service) allAnswered(p *party, r Round, now time.Time) bool {
	answered := 0
	for _, m := range p.snap.Members {
		if m.FromRound > r.N {
			continue
		}
		if p.answered(r.N, m.M) {
			answered++
			continue
		}
		if m.LeftAt != nil || now.Sub(p.seenAt(m)) > Silent || away(m, p.snap.Rounds, p.answered) {
			continue
		}
		return false
	}
	return answered > 0
}

// close closes place r at at, and draws the next place, which begins after
// the reveal and the countdown.
func (s *Service) close(ctx context.Context, p *party, r Round, at time.Time) error {
	set := p.snap.Game.Settings
	var next *NewRound
	if r.N < set.Rounds {
		c, repeat, err := s.pick(ctx, p.snap.Game.ID)
		if errors.Is(err, ErrNoPlace) {
			s.Log.Warn("free pool has no place left for a party", "game", p.snap.Game.ID)
			p.exhausted = true
		} else if err != nil {
			return err
		} else {
			starts := at.Add(RevealFor(set.Seconds) + Countdown)
			ends := starts.Add(set.Limit())
			next = &NewRound{N: r.N + 1, PlaceID: c.PlaceID, Repeat: repeat, StartsAt: starts, EndsAt: &ends}
		}
	}
	return s.Store.CloseFreeRound(ctx, p.snap.Game.ID, r.N, at, next)
}

// touch notes that a player polled, and writes it now and then: it is how a
// restart knows who was there.
func (s *Service) touch(ctx context.Context, p *party, m Member, now time.Time) error {
	p.seen[m.M] = now
	p.used = now
	if now.Sub(p.saved[m.M]) < seenEvery {
		return nil
	}
	p.saved[m.M] = now
	return s.Store.SeenFree(ctx, p.snap.Game.ID, m.M, now)
}

// party is the party behind code, locked, with the player w is in it.
func (s *Service) party(ctx context.Context, w Who, code string) (*party, Member, func(), error) {
	if !w.Telegram() {
		return nil, Member{}, nil, ErrTelegram
	}
	p, err := s.byCode(ctx, code)
	if err != nil {
		return nil, Member{}, nil, err
	}
	p.mu.Lock()
	m, ok := p.snap.member(w)
	if !ok {
		p.mu.Unlock()
		return nil, Member{}, nil, ErrNotMember
	}
	return p, m, p.mu.Unlock, nil
}

// NewParty makes a lobby on set with w as its host. A lobby w was hosting is
// given up.
func (s *Service) NewParty(ctx context.Context, w Who, name, lang string, set Settings) (PartyView, error) {
	s.init()
	if !w.Telegram() {
		return PartyView{}, ErrTelegram
	}
	if !set.ValidParty() {
		return PartyView{}, ErrBadRequest
	}
	now := s.Now()
	open, err := s.Store.OpenFreeGames(ctx, w, Party)
	if err != nil {
		return PartyView{}, err
	}
	for _, id := range open {
		p, err := s.load(ctx, id)
		if err != nil {
			return PartyView{}, err
		}
		p.mu.Lock()
		g := p.snap.Game
		var ferr error
		if host, ok := p.snap.memberNo(g.Host); g.Status == StatusLobby && ok && host.Who == w {
			if _, ferr = s.Store.FinishFree(ctx, id, StatusAbandoned, now, nil); ferr == nil {
				ferr = s.reload(ctx, p)
			}
		}
		p.mu.Unlock()
		if ferr != nil {
			return PartyView{}, ferr
		}
	}
	id, err := s.create(ctx, NewGame{Kind: Party, Settings: set, Status: StatusLobby, At: now,
		Host: Member{M: 1, Who: w, Name: nameOr(name), JoinedAt: now, FromRound: 1, LastSeen: now}})
	if err != nil {
		return PartyView{}, err
	}
	p, err := s.load(ctx, id)
	if err != nil {
		return PartyView{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	me, _ := p.snap.member(w)
	p.seen[me.M], p.saved[me.M] = now, now
	return s.partyView(ctx, p, me, now, lang)
}

func nameOr(name string) string {
	if name == "" {
		return "?"
	}
	if r := []rune(name); len(r) > 64 {
		return string(r[:64])
	}
	return name
}

// Join puts w in the party behind code. In the lobby they play from the
// first place; once it has begun, from the next place that has not started,
// and the places before count as missed. Someone who left comes back.
func (s *Service) Join(ctx context.Context, w Who, name, code, lang string) (PartyView, error) {
	s.init()
	if !w.Telegram() {
		return PartyView{}, ErrTelegram
	}
	p, err := s.byCode(ctx, code)
	if err != nil {
		return PartyView{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := s.Now()
	if err := s.advance(ctx, p, now); err != nil {
		return PartyView{}, err
	}
	g := p.snap.Game
	me, ok := p.snap.member(w)
	switch {
	case ok && me.LeftAt == nil:
	case ok:
		if g.Status != StatusLobby && g.Status != StatusPlaying {
			return PartyView{}, ErrOver
		}
		if len(s.present(p)) >= MaxMembers {
			return PartyView{}, ErrFull
		}
		if err := s.Store.RejoinFree(ctx, g.ID, me.M); err != nil {
			return PartyView{}, err
		}
	default:
		from := 1
		switch g.Status {
		case StatusLobby:
		case StatusPlaying:
			r, _ := p.snap.last()
			from = r.N
			if !now.Before(r.StartsAt) {
				from = r.N + 1
			}
			if from > g.Settings.Rounds {
				return PartyView{}, ErrTooLate
			}
		default:
			return PartyView{}, ErrOver
		}
		if len(s.present(p)) >= MaxMembers {
			return PartyView{}, ErrFull
		}
		next := 1
		for _, m := range p.snap.Members {
			next = max(next, m.M+1)
		}
		if next > maxMemberNo {
			return PartyView{}, ErrFull
		}
		me = Member{M: next, Who: w, Name: nameOr(name), JoinedAt: now, FromRound: from, LastSeen: now}
		if err := s.Store.JoinFree(ctx, g.ID, me); err != nil {
			return PartyView{}, err
		}
	}
	if err := s.reload(ctx, p); err != nil {
		return PartyView{}, err
	}
	me, _ = p.snap.member(w)
	if err := s.touch(ctx, p, me, now); err != nil {
		return PartyView{}, err
	}
	return s.partyView(ctx, p, me, now, lang)
}

// maxMemberNo bounds the numbers a party hands out, people who left included.
const maxMemberNo = 32

// Poll is the party as w may see it now, or just its version and the clock
// when the app has that version already.
func (s *Service) Poll(ctx context.Context, w Who, code string, since int64, lang string) (any, error) {
	s.init()
	p, me, unlock, err := s.party(ctx, w, code)
	if err != nil {
		return nil, err
	}
	defer unlock()
	now := s.Now()
	if err := s.touch(ctx, p, me, now); err != nil {
		return nil, err
	}
	if err := s.advance(ctx, p, now); err != nil {
		return nil, err
	}
	if since == p.version {
		return Short{V: p.version, Now: now.UTC()}, nil
	}
	me, _ = p.snap.member(w)
	return s.partyView(ctx, p, me, now, lang)
}

// SetParty changes the lobby's settings; only the host may.
func (s *Service) SetParty(ctx context.Context, w Who, code, lang string, set Settings) (PartyView, error) {
	s.init()
	if !set.ValidParty() {
		return PartyView{}, ErrBadRequest
	}
	p, me, unlock, err := s.party(ctx, w, code)
	if err != nil {
		return PartyView{}, err
	}
	defer unlock()
	now := s.Now()
	if err := s.advance(ctx, p, now); err != nil {
		return PartyView{}, err
	}
	if p.snap.Game.Status != StatusLobby {
		return PartyView{}, ErrOver
	}
	if p.snap.Game.Host != me.M {
		return PartyView{}, ErrNotHost
	}
	if set != p.snap.Game.Settings {
		if err := s.Store.SetFreeSettings(ctx, p.snap.Game.ID, set); err != nil {
			return PartyView{}, err
		}
		if err := s.reload(ctx, p); err != nil {
			return PartyView{}, err
		}
	}
	_ = s.touch(ctx, p, me, now)
	return s.partyView(ctx, p, me, now, lang)
}

// StartParty starts the lobby on the settings of version, the ones the host
// saw. When they have changed since, nothing starts: a game never begins on
// settings nobody saw. Two players at least; with two it is a duel.
func (s *Service) StartParty(ctx context.Context, w Who, code, lang string, version int) (PartyView, error) {
	s.init()
	p, me, unlock, err := s.party(ctx, w, code)
	if err != nil {
		return PartyView{}, err
	}
	defer unlock()
	now := s.Now()
	if err := s.advance(ctx, p, now); err != nil {
		return PartyView{}, err
	}
	g := p.snap.Game
	if g.Status == StatusPlaying && g.Version == version {
		return s.partyView(ctx, p, me, now, lang)
	}
	if g.Status != StatusLobby {
		return PartyView{}, ErrOver
	}
	if g.Host != me.M {
		return PartyView{}, ErrNotHost
	}
	if g.Version != version {
		return PartyView{}, ErrChanged
	}
	present := s.present(p)
	if len(present) < 2 {
		return PartyView{}, ErrAlone
	}
	kind := Party
	if len(present) == 2 {
		kind = Duel
	}
	c, repeat, err := s.pick(ctx, g.ID)
	if err != nil {
		return PartyView{}, err
	}
	starts := now.Add(Countdown)
	ends := starts.Add(g.Settings.Limit())
	ok, err := s.Store.StartFree(ctx, g.ID, version, kind, now,
		NewRound{N: 1, PlaceID: c.PlaceID, Repeat: repeat, StartsAt: starts, EndsAt: &ends})
	if err != nil {
		return PartyView{}, err
	}
	if err := s.reload(ctx, p); err != nil {
		return PartyView{}, err
	}
	if !ok && p.snap.Game.Status != StatusPlaying {
		return PartyView{}, ErrChanged
	}
	_ = s.touch(ctx, p, me, now)
	return s.partyView(ctx, p, me, now, lang)
}

// GuessParty takes w's pin for place n, until its end and the grace. When
// everyone the place waits for has answered, it closes at once.
func (s *Service) GuessParty(ctx context.Context, w Who, code, lang string, in GuessIn) (PartyView, error) {
	s.init()
	if in.Skip || in.Pin == nil || !in.Pin.Valid() {
		return PartyView{}, ErrBadRequest
	}
	p, me, unlock, err := s.party(ctx, w, code)
	if err != nil {
		return PartyView{}, err
	}
	defer unlock()
	now := s.Now()
	if err := s.advance(ctx, p, now); err != nil {
		return PartyView{}, err
	}
	g := p.snap.Game
	if g.Status != StatusPlaying {
		return PartyView{}, ErrOver
	}
	r, ok := p.snap.round(in.N)
	switch {
	case !ok:
		return PartyView{}, ErrOrder
	case me.FromRound > in.N:
		return PartyView{}, ErrTooLate
	case now.Before(r.StartsAt):
		return PartyView{}, ErrNotYet
	case p.answered(in.N, me.M):
		return PartyView{}, ErrAnswered
	case r.ClosedAt != nil || now.After(r.Over()):
		return PartyView{}, ErrClosed
	}
	pl, err := s.place(ctx, r.PlaceID)
	if err != nil {
		return PartyView{}, err
	}
	pin := *in.Pin
	gs := s.score(pl, g.Settings, r, Guess{N: in.N, M: me.M, Pin: &pin, Auto: in.Auto, AnsweredAt: now, TimeMs: timeOf(g.Settings, r, now)})
	if _, err := s.Store.InsertFreeGuess(ctx, g.ID, gs); err != nil {
		return PartyView{}, err
	}
	if err := s.reload(ctx, p); err != nil {
		return PartyView{}, err
	}
	_ = s.touch(ctx, p, me, now)
	if r, _ := p.snap.round(in.N); r.ClosedAt == nil && s.allAnswered(p, r, now) {
		if err := s.close(ctx, p, r, now); err != nil {
			return PartyView{}, err
		}
		if err := s.reload(ctx, p); err != nil {
			return PartyView{}, err
		}
	}
	return s.partyView(ctx, p, me, now, lang)
}

// LeaveParty takes w out of the party. A host who leaves the lobby hands it
// to the next player at once.
func (s *Service) LeaveParty(ctx context.Context, w Who, code string) error {
	s.init()
	p, me, unlock, err := s.party(ctx, w, code)
	if err != nil {
		return err
	}
	defer unlock()
	now := s.Now()
	if me.LeftAt != nil {
		return nil
	}
	if err := s.Store.LeaveFree(ctx, p.snap.Game.ID, me.M, now); err != nil {
		return err
	}
	if err := s.reload(ctx, p); err != nil {
		return err
	}
	if p.snap.Game.Status == StatusLobby && p.snap.Game.Host == me.M {
		for _, m := range s.present(p) {
			if err := s.Store.SetFreeHost(ctx, p.snap.Game.ID, m.M); err != nil {
				return err
			}
			return s.reload(ctx, p)
		}
	}
	return s.advance(ctx, p, now)
}

// Rematch is the code of the lobby after a finished party: the one someone
// made already, or a new one on the same settings with w as its host. w is
// in it either way; the host may change the settings there.
func (s *Service) Rematch(ctx context.Context, w Who, name, code string) (string, error) {
	s.init()
	p, _, unlock, err := s.party(ctx, w, code)
	if err != nil {
		return "", err
	}
	now := s.Now()
	if err := s.advance(ctx, p, now); err != nil {
		unlock()
		return "", err
	}
	g := p.snap.Game
	if g.Status != StatusDone {
		unlock()
		return "", ErrOrder
	}
	var next int64
	for i := 0; ; i++ {
		next, err = s.Store.RematchFree(ctx, g.ID, NewGame{Kind: Party, Code: newCode(), Settings: g.Settings,
			Status: StatusLobby, At: now, Host: Member{M: 1, Who: w, Name: nameOr(name), JoinedAt: now, FromRound: 1, LastSeen: now}})
		if errors.Is(err, ErrCodeTaken) && i < 3 {
			continue
		}
		break
	}
	if err == nil {
		err = s.reload(ctx, p)
	}
	unlock()
	if err != nil {
		return "", err
	}
	np, err := s.load(ctx, next)
	if err != nil {
		return "", err
	}
	if _, err := s.Join(ctx, w, name, np.snap.Game.Code, ""); err != nil {
		return "", err
	}
	return np.snap.Game.Code, nil
}

// PartySettings is the settings of a party w is in, for the invitation.
func (s *Service) PartySettings(ctx context.Context, w Who, code string) (Settings, string, error) {
	s.init()
	p, _, unlock, err := s.party(ctx, w, code)
	if err != nil {
		return Settings{}, "", err
	}
	defer unlock()
	return p.snap.Game.Settings, p.snap.Game.Status, nil
}

func (s *Service) partyView(ctx context.Context, p *party, me Member, now time.Time, lang string) (PartyView, error) {
	g := p.snap.Game
	set := g.Settings
	v := PartyView{V: p.version, Now: now.UTC(), Code: g.Code, Status: g.Status, Kind: g.Kind, Settings: set,
		Version: g.Version, Length: int(Length(set) / time.Second), Reveal: int(RevealFor(set.Seconds) / time.Second),
		Countdown: int(Countdown / time.Second), Grace: int(Grace / time.Second), Host: g.Host, Me: me.M,
		StartedAt: utc(g.StartedAt), Members: []MemberView{}, Rounds: []PartyRound{}, Totals: []Line{}, Next: g.NextCode}
	for _, m := range players(&p.snap) {
		if g.Status == StatusLobby && m.LeftAt != nil {
			continue
		}
		v.Members = append(v.Members, MemberView{M: m.M, Name: m.Name, From: m.FromRound,
			Away: g.Status == StatusPlaying && away(m, p.snap.Rounds, p.answered), Left: m.LeftAt != nil})
	}
	var closed []Round
	var shown []Guess
	for _, r := range p.snap.Rounds {
		pl, err := s.place(ctx, r.PlaceID)
		if err != nil {
			return PartyView{}, err
		}
		pr := PartyRound{N: r.N, Image: s.imagePath(g.ID, r.N), Kind: pl.Kind, Heading: pl.Heading,
			StartsAt: r.StartsAt.UTC(), EndsAt: utc(r.EndsAt), ClosedAt: utc(r.ClosedAt), Repeat: r.Repeat}
		if r.ClosedAt == nil {
			for _, gs := range p.snap.Guesses {
				if gs.N == r.N {
					pr.Answered = append(pr.Answered, gs.M)
				}
			}
		} else {
			closed = append(closed, r)
			place := placeView(pl, lang)
			pr.Place = &place
			pr.Guesses = []GuessView{}
			for _, gs := range p.snap.Guesses {
				if gs.N != r.N {
					continue
				}
				gv := guessView(gs)
				pr.Guesses = append(pr.Guesses, gv)
				shown = append(shown, gs)
				if gs.Pin != nil && (v.Best == nil || gv.Points > v.Best.Points || gv.Points == v.Best.Points && *gv.Km < *v.Best.Km) {
					best := gv
					best.M = gs.M
					v.Best = &best
				}
			}
		}
		v.Rounds = append(v.Rounds, pr)
	}
	if len(closed) > 0 {
		v.Totals = Lines(set, players(&p.snap), closed, shown)
	}
	return v, nil
}
