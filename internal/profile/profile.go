// SPDX-License-Identifier: Apache-2.0

// Package profile draws a person's statistics as a looping animation: their
// pins of every day on a turning Earth, their points and their places in the
// world. Drawing one takes seconds, and the numbers change at most a few
// times a day, so a picture is drawn once for its numbers: a fingerprint of
// everything on it names it, Telegram's file_id for it is kept in the
// database, and a picture drawn but not yet sent waits on disk. Pictures are
// drawn ahead of time for the people who play, so the button rarely waits.
package profile

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

type Store interface {
	ProfileOf(ctx context.Context, userID int64, today round.Date, n int) (db.Profile, error)
	ProfileCard(ctx context.Context, userID int64, lang, version string) (string, error)
	SetProfileCard(ctx context.Context, userID int64, lang, version, fileID string) error
	RecentPlayers(ctx context.Context, since round.Date) ([]int64, error)
	UserLang(ctx context.Context, userID int64) (string, error)
}

// ErrNothing means the person has no scored day yet: there is nothing to draw.
var ErrNothing = errors.New("no scored day to draw")

// renderer changes when the picture's look does, so every kept picture is
// drawn again.
const renderer = 1

// recent is how many of the last days the strip shows.
const recent = 14

type Service struct {
	Store  Store
	Zones  round.Zones
	FFmpeg string
	// Dir keeps pictures drawn but not sent yet; empty is a folder in the
	// system's temporary one.
	Dir string
	Log *slog.Logger
	Now func() time.Time

	mu    sync.Mutex
	drawn map[key]drawn
	busy  map[key]*drawing
}

type key struct {
	user int64
	lang string
}

type drawn struct {
	version, path string
}

type drawing struct {
	version string
	done    chan struct{}
	err     error
}

// Card is a person's picture: a file Telegram has, or one to upload.
type Card struct {
	Version string
	FileID  string
	MP4     []byte
}

func (c Card) Animation() telegram.Animation {
	w, h := (&anim.Profile{}).Size()
	return telegram.Animation{FileID: c.FileID, MP4: c.MP4, Width: w, Height: h, Duration: int((&anim.Profile{}).Duration())}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) dir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return filepath.Join(os.TempDir(), "maply-profiles")
}

// Ready is the person's picture for their numbers now, when it is drawn: ok
// is false when it still has to be. ErrNothing when there is nothing to draw.
func (s *Service) Ready(ctx context.Context, userID int64, lang string) (Card, bool, error) {
	_, version, err := s.picture(ctx, userID, lang)
	if err != nil {
		return Card{}, false, err
	}
	return s.ready(ctx, key{userID, lang}, version)
}

func (s *Service) ready(ctx context.Context, k key, version string) (Card, bool, error) {
	id, err := s.Store.ProfileCard(ctx, k.user, k.lang, version)
	if err != nil {
		return Card{}, false, err
	}
	if id != "" {
		return Card{Version: version, FileID: id}, true, nil
	}
	s.mu.Lock()
	d, ok := s.drawn[k]
	s.mu.Unlock()
	if ok && d.version == version {
		if mp4, err := os.ReadFile(d.path); err == nil {
			return Card{Version: version, MP4: mp4}, true, nil
		}
	}
	return Card{Version: version}, false, nil
}

// Draw draws the person's picture unless it is drawn already, and returns
// it. Two calls for the same person wait for one drawing. workers is how many
// frames are drawn at once; 0 is one per CPU.
func (s *Service) Draw(ctx context.Context, userID int64, lang string, workers int) (Card, error) {
	p, version, err := s.picture(ctx, userID, lang)
	if err != nil {
		return Card{}, err
	}
	k := key{userID, lang}
	if c, ok, err := s.ready(ctx, k, version); err != nil || ok {
		return c, err
	}
	s.mu.Lock()
	if s.busy == nil {
		s.busy, s.drawn = map[key]*drawing{}, map[key]drawn{}
	}
	if d := s.busy[k]; d != nil && d.version == version {
		s.mu.Unlock()
		select {
		case <-d.done:
		case <-ctx.Done():
			return Card{}, ctx.Err()
		}
		if d.err != nil {
			return Card{}, d.err
		}
		c, _, err := s.ready(ctx, k, version)
		return c, err
	}
	d := &drawing{version: version, done: make(chan struct{})}
	s.busy[k] = d
	s.mu.Unlock()

	path, err := s.render(ctx, k, version, p, workers)
	s.mu.Lock()
	if err == nil {
		if old, ok := s.drawn[k]; ok && old.path != path {
			os.Remove(old.path)
		}
		s.drawn[k] = drawn{version: version, path: path}
	}
	if s.busy[k] == d {
		delete(s.busy, k)
	}
	d.err = err
	close(d.done)
	s.mu.Unlock()
	if err != nil {
		return Card{}, err
	}
	mp4, err := os.ReadFile(path)
	return Card{Version: version, MP4: mp4}, err
}

func (s *Service) render(ctx context.Context, k key, version string, p *anim.Profile, workers int) (string, error) {
	if err := os.MkdirAll(s.dir(), 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(s.dir(), fmt.Sprintf("%d-%s-%s.mp4", k.user, k.lang, version[:16]))
	start := time.Now()
	if _, err := anim.Encode(ctx, p, path, anim.Options{FFmpeg: s.FFmpeg, Workers: workers}); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("profile of %d: %w", k.user, err)
	}
	if s.Log != nil {
		s.Log.Info("profile drawn", "user", k.user, "lang", k.lang, "took", time.Since(start).Round(time.Millisecond))
	}
	return path, nil
}

// Keep records the file_id Telegram gave the picture once it was sent, and
// drops the file it was sent from.
func (s *Service) Keep(ctx context.Context, userID int64, lang, version, fileID string) error {
	if fileID == "" || version == "" {
		return nil
	}
	if err := s.Store.SetProfileCard(ctx, userID, lang, version, fileID); err != nil {
		return err
	}
	k := key{userID, lang}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.drawn[k]; ok && d.version == version {
		os.Remove(d.path)
		delete(s.drawn, k)
	}
	return nil
}

// Run draws the pictures of everyone who played in the last days whenever
// their numbers change: after they answer, after the reveal and once the
// world's ranking of a day is final. It draws one at a time, with few
// workers, so the posts keep the CPU.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	// What a previous process left: its pictures are not in s.drawn.
	os.RemoveAll(s.dir())
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.Warm(ctx); err != nil && ctx.Err() == nil && s.Log != nil {
			s.Log.Warn("profiles not drawn ahead", "error", err)
		}
		t.Reset(every)
	}
}

// Warm draws the pictures that are missing for the recent players, in their
// own language.
func (s *Service) Warm(ctx context.Context) error {
	ids, err := s.Store.RecentPlayers(ctx, s.today().AddDays(-2))
	if err != nil {
		return err
	}
	for _, id := range ids {
		raw, err := s.Store.UserLang(ctx, id)
		if err != nil {
			return err
		}
		if _, err := s.Draw(ctx, id, i18n.Resolve(raw), 2); err != nil && !errors.Is(err, ErrNothing) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if s.Log != nil {
				s.Log.Warn("profile not drawn", "user", id, "error", err)
			}
		}
	}
	return nil
}

// today is the newest date a round can be on: the date in UTC. A round on it
// shows on the strip only once the person has answered it.
func (s *Service) today() round.Date { return round.DateIn(s.now(), time.UTC) }

// picture builds the person's picture and its fingerprint.
func (s *Service) picture(ctx context.Context, userID int64, lang string) (*anim.Profile, string, error) {
	today := s.today()
	data, err := s.Store.ProfileOf(ctx, userID, today, recent+1)
	if err != nil {
		return nil, "", err
	}
	p := build(data, lang, today, func(d round.Date) bool { return !s.Zones.LastReveal(d).After(s.now()) })
	if p == nil {
		return nil, "", ErrNothing
	}
	b, err := json.Marshal(struct {
		Renderer int
		P        *anim.Profile
	}{renderer, p})
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	return p, hex.EncodeToString(sum[:]), nil
}

// build lays the numbers out. closed tells whether a day's world ranking is
// final. Nil when no day is scored yet.
func build(data db.Profile, lang string, today round.Date, closed func(round.Date) bool) *anim.Profile {
	var (
		scored []db.Answered
		best   *db.Answered
		total  int
		kms    []float64
	)
	answered := map[string]int{}
	for i, a := range data.Answers {
		answered[a.Date.String()] = a.DayNo
		if a.Points == nil || a.Km == nil {
			continue
		}
		scored = append(scored, a)
		total += *a.Points
		kms = append(kms, *a.Km)
		if best == nil || *a.Points > *best.Points {
			best = &data.Answers[i]
		}
	}
	if len(scored) == 0 {
		return nil
	}
	// The picture's own language: one the brand's faces write, else English.
	text := lang
	if !anim.Covers(i18n.T(lang, "profile.title") + i18n.T(lang, "profile.played") + i18n.T(lang, "profile.world_best")) {
		text = i18n.DefaultLang
	}
	t := func(k string, pairs ...string) string { return i18n.T(text, k, pairs...) }
	unit := strings.TrimSpace(i18n.T(text, "unit.km", "km", ""))
	if unit == "" || !anim.Covers(unit) {
		unit = "km"
	}

	p := anim.NewProfile(text)
	p.Title, p.Name, p.DaysLabel = t("profile.title"), anim.Name(data.Name), t("profile.recent")
	p.Total = anim.Tile{Value: anim.Thousands(float64(total)), Label: t("profile.points")}

	world, worldLabel, worldBest := "—", t("profile.world", "n", strconv.Itoa(scored[len(scored)-1].DayNo)), 0
	for _, w := range data.World {
		if !closed(w.Date) {
			continue
		}
		world = strconv.Itoa(w.Rank) + " / " + strconv.Itoa(w.Of)
		worldLabel = t("profile.world", "n", strconv.Itoa(answered[w.Date.String()]))
		if worldBest == 0 || w.Rank < worldBest {
			worldBest = w.Rank
		}
	}
	bestPlace := "—"
	if worldBest > 0 {
		bestPlace = strconv.Itoa(worldBest)
	}
	slices.Sort(kms)
	median := kms[len(kms)/2]
	if len(kms)%2 == 0 {
		median = (kms[len(kms)/2-1] + kms[len(kms)/2]) / 2
	}
	p.Tiles = [6]anim.Tile{
		{Value: strconv.Itoa(len(scored)), Label: t("profile.played")},
		{Value: strconv.Itoa(data.Streak), Label: t("profile.streak"), Flame: data.Streak > 0},
		{Value: anim.Thousands(median) + " " + unit, Label: t("profile.median")},
		{Value: anim.Thousands(float64(*best.Points)), Label: t("profile.best", "n", strconv.Itoa(best.DayNo)), Gold: true},
		{Value: world, Label: worldLabel},
		{Value: bestPlace, Label: t("profile.world_best"), Gold: worldBest == 1},
	}

	for _, a := range data.Answers {
		g := anim.Guess{Lat: a.Lat, Lon: a.Lon, Km: 4000}
		if a.Km != nil {
			g.Km = *a.Km
		}
		if a.Date.String() == best.Date.String() {
			g.Medal = 1
		}
		p.Pins = append(p.Pins, g)
	}
	for _, r := range data.Rounds {
		_, played := answered[r.Date.String()]
		if r.Date.String() == today.String() && !played {
			continue
		}
		p.Days = append(p.Days, anim.Day{No: r.DayNo, Played: played, Best: r.Date.String() == best.Date.String()})
		if len(p.Days) == recent {
			break
		}
	}
	slices.SortFunc(p.Days, func(a, b anim.Day) int { return cmp.Compare(a.No, b.No) })
	return p
}
