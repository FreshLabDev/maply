// SPDX-License-Identifier: Apache-2.0
package free

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	mrand "math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// Error is a refusal the API passes on as it is: an HTTP status and a code
// the app has a text for.
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string { return e.Code }

var (
	ErrNotFound   = &Error{404, "not_found"}
	ErrNotMember  = &Error{403, "not_a_member"}
	ErrNotHost    = &Error{403, "not_host"}
	ErrTelegram   = &Error{403, "telegram_only"}
	ErrBadRequest = &Error{400, "bad_request"}
	ErrOrder      = &Error{409, "out_of_order"}
	ErrNotYet     = &Error{409, "not_yet"}
	ErrClosed     = &Error{409, "closed"}
	ErrAnswered   = &Error{409, "already_answered"}
	ErrOver       = &Error{409, "game_over"}
	ErrChanged    = &Error{409, "settings_changed"}
	ErrFull       = &Error{409, "full"}
	ErrTooLate    = &Error{409, "too_late"}
	ErrAlone      = &Error{409, "alone"}
	ErrNoPlace    = &Error{503, "no_place"}
)

// ErrCodeTaken is a party code another game has; a new one is drawn.
var ErrCodeTaken = errors.New("party code taken")

// Service plays the free game. One process holds the parties it serves in
// memory, so a poll that finds nothing new does not touch the database;
// everything that matters is written before it is shown, so a restart loses
// nothing.
type Service struct {
	Store     Store
	Countries *geo.Countries
	// ImageKey signs the rounds' picture addresses.
	ImageKey []byte
	Now      func() time.Time
	Log      *slog.Logger
	// Intn draws among the places nobody has seen; nil is math/rand.
	Intn func(int) int

	once    sync.Once
	mu      sync.Mutex
	parties map[int64]*party
	codes   map[string]int64
	places  map[int64]Place
	// versions hands out the parties' versions. It starts at the clock, above
	// any an app may have from before a restart, and never gives one twice,
	// so a party read again after it was dropped from memory is new to every
	// app that polls it.
	versions atomic.Int64
	// solo serializes the requests of one solo game.
	solo [64]sync.Mutex
}

func (s *Service) init() {
	s.once.Do(func() {
		if s.Now == nil {
			s.Now = time.Now
		}
		if s.Log == nil {
			s.Log = slog.New(slog.DiscardHandler)
		}
		if s.Intn == nil {
			s.Intn = mrand.IntN
		}
		if len(s.ImageKey) == 0 {
			s.ImageKey = make([]byte, 32)
			_, _ = rand.Read(s.ImageKey)
		}
		s.parties = map[int64]*party{}
		s.codes = map[string]int64{}
		s.places = map[int64]Place{}
		s.versions.Store(time.Now().UnixMilli())
	})
}

// place is a place read once and kept: a place does not change while it is
// played.
func (s *Service) place(ctx context.Context, id int64) (Place, error) {
	s.mu.Lock()
	p, ok := s.places[id]
	s.mu.Unlock()
	if ok {
		return p, nil
	}
	p, err := s.Store.FreePlace(ctx, id)
	if err != nil {
		return Place{}, err
	}
	s.mu.Lock()
	if len(s.places) >= 512 {
		s.places = map[int64]Place{}
	}
	s.places[id] = p
	s.mu.Unlock()
	return p, nil
}

// judge scores a pin by the judge of the day's game.
func (s *Service) judge(p Place, pin geo.Point) scoring.Score {
	return scoring.Judge(pin, scoring.Place{Point: p.Point, Country: p.Country, Continent: p.Continent, City: p.CityShape}, s.Countries)
}

// pick draws the next place of a game and the moment it is for.
func (s *Service) pick(ctx context.Context, id int64) (Candidate, bool, error) {
	cands, used, err := s.Store.FreeCandidates(ctx, id)
	if err != nil {
		return Candidate{}, false, err
	}
	c, repeat, ok := Pick(cands, used, s.Intn)
	if !ok {
		return Candidate{}, false, ErrNoPlace
	}
	return c, repeat, nil
}

// score is a guess with its points, for a pin put on place p.
func (s *Service) score(p Place, set Settings, r Round, g Guess) Guess {
	if g.Pin == nil {
		return g
	}
	sc := s.judge(p, *g.Pin)
	g.Km, g.PtsKm, g.PtsBonus, g.CountryOK = sc.Km, sc.Distance, sc.Bonus, sc.Match.Country
	if r.EndsAt != nil {
		g.PtsSpeed = Speed(set.Seconds, *r.EndsAt, g.AnsweredAt, g.PtsKm, g.Auto)
	}
	return g
}

// timeOf is how long the answer took, never more than the place's time.
func timeOf(set Settings, r Round, at time.Time) int64 {
	d := max(at.Sub(r.StartsAt), 0)
	if limit := set.Limit(); limit > 0 && d > limit {
		d = limit
	}
	return d.Milliseconds()
}

// results is a finished game's lines for its Telegram players.
func results(snap *Snapshot, at time.Time) []Result {
	members := players(snap)
	lines := Lines(snap.Game.Settings, members, snap.Rounds, snap.Guesses)
	out := make([]Result, 0, len(lines))
	for _, l := range lines {
		m, _ := snap.memberNo(l.M)
		if !m.Who.Telegram() {
			continue
		}
		r := Result{Line: l, UserID: m.Who.UserID, Kind: snap.Game.Kind, Settings: snap.Game.Settings, At: at}
		if snap.Game.Kind != Solo {
			r.Place, r.Players, r.Beaten = l.Rank, len(lines), l.Beaten
		}
		out = append(out, r)
	}
	return out
}

// players is who played a game: everyone but those who left its lobby.
func players(snap *Snapshot) []Member {
	out := make([]Member, 0, len(snap.Members))
	for _, m := range snap.Members {
		if m.LeftAt != nil && (snap.Game.StartedAt == nil || !m.LeftAt.After(*snap.Game.StartedAt)) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// finish ends a game that is over and writes its results.
func (s *Service) finish(ctx context.Context, snap *Snapshot, at time.Time) error {
	_, err := s.Store.FinishFree(ctx, snap.Game.ID, StatusDone, at, results(snap, at))
	return err
}

// imagePath is where a round's picture is fetched. It names the game and the
// place by number, signed, and never the image's key.
func (s *Service) imagePath(game int64, n int) string {
	buf := make([]byte, 10, 10+12)
	binary.BigEndian.PutUint64(buf, uint64(game))
	binary.BigEndian.PutUint16(buf[8:], uint16(n))
	buf = append(buf, s.mac(buf)[:12]...)
	return "/api/free/img/" + base64.RawURLEncoding.EncodeToString(buf) + ".jpg"
}

func (s *Service) mac(msg []byte) []byte {
	m := hmac.New(sha256.New, s.ImageKey)
	m.Write(msg)
	return m.Sum(nil)
}

// Image is the key of the picture behind a round's address, once the round
// has begun. Before, the answer is ErrNotYet and nothing else.
func (s *Service) Image(ctx context.Context, token string) (string, error) {
	s.init()
	buf, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(buf) != 22 || !hmac.Equal(buf[10:], s.mac(buf[:10])[:12]) {
		return "", ErrNotFound
	}
	game := int64(binary.BigEndian.Uint64(buf))
	n := int(binary.BigEndian.Uint16(buf[8:]))
	var r Round
	var ok bool
	if p := s.cached(game); p != nil {
		p.mu.Lock()
		r, ok = p.snap.round(n)
		p.mu.Unlock()
	}
	if !ok {
		snap, err := s.Store.FreeSnapshot(ctx, game)
		if errors.Is(err, ErrNoRow) {
			return "", ErrNotFound
		}
		if err != nil {
			return "", err
		}
		if r, ok = snap.round(n); !ok {
			return "", ErrNotFound
		}
	}
	if s.Now().Before(r.StartsAt) {
		return "", ErrNotYet
	}
	p, err := s.place(ctx, r.PlaceID)
	if err != nil {
		return "", err
	}
	return p.ImageKey, nil
}

// codeAlphabet is Crockford's base 32: no I, L, O or U to misread.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newCode is a party's invitation code: 40 random bits.
func newCode() string {
	b := make([]byte, 8)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		b[i] = codeAlphabet[n.Int64()]
	}
	return string(b)
}

// ValidCode is whether s has a party code's shape.
func ValidCode(s string) bool {
	if len(s) != 8 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ok := false
		for j := 0; j < len(codeAlphabet); j++ {
			if s[i] == codeAlphabet[j] {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// create makes a game, drawing a new party code if one is taken.
func (s *Service) create(ctx context.Context, g NewGame) (int64, error) {
	for i := 0; ; i++ {
		if g.Kind != Solo {
			g.Code = newCode()
		}
		id, err := s.Store.CreateFreeGame(ctx, g)
		if errors.Is(err, ErrCodeTaken) && i < 3 {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("create free game: %w", err)
		}
		return id, nil
	}
}

// Sweep settles the games nobody is looking at: a solo game untouched for a
// day is abandoned, a lobby never started goes after half an hour, and a
// party that ran out of time with nobody polling is finished, so its results
// are written.
func (s *Service) Sweep(ctx context.Context) error {
	s.init()
	now := s.Now()
	ids, err := s.Store.StaleFreeGames(ctx, now.Add(-AbandonAfter), now.Add(-LobbyFor), now)
	if err != nil {
		return err
	}
	for _, id := range ids {
		// One game that fails is logged and left for the next sweep; the
		// games after it are not held up.
		if err := s.sweep(ctx, id, now); err != nil {
			s.Log.Warn("free sweep", "game", id, "err", err)
		}
	}
	s.evict(now)
	return nil
}

func (s *Service) sweep(ctx context.Context, id int64, now time.Time) (err error) {
	snap, err := s.Store.FreeSnapshot(ctx, id)
	if err != nil {
		return err
	}
	if snap.Game.Kind == Solo {
		unlock := s.lockSolo(id)
		defer unlock()
		_, err = s.settle(ctx, &snap, now)
		return err
	}
	p, err := s.hold(ctx, id)
	if err != nil {
		return err
	}
	defer s.release(p, &err)
	return s.advance(ctx, p, now)
}

// Run sweeps every so often until ctx is done.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
				s.Log.Warn("free game sweep failed", "error", err)
			}
		}
	}
}
