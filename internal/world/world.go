// SPDX-License-Identifier: Apache-2.0

// Package world draws the animation of a day's world ranking: every answer
// of the day on one globe and the first three on the podium. The same picture
// goes to everyone who reads one language, so it is drawn once per version of
// the ranking and then sent by file_id. Until the day has closed in every
// zone the ranking grows, and the picture is redrawn at most every few
// minutes.
package world

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/card"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// redrawEvery is the least time between two pictures of one open day in one
// language: a drawing takes some 15 s of every core.
const redrawEvery = 2 * time.Minute

// renderer is bumped when the drawing changes, so kept pictures go stale.
const renderer = "world-1"

type Store interface {
	World(ctx context.Context, d round.Date) (db.World, error)
	WorldPins(ctx context.Context, d round.Date) ([]db.WorldPin, error)
	RoundOn(ctx context.Context, d round.Date) (db.Round, bool, error)
	Answer(ctx context.Context, placeID int64) (db.Answer, error)
	WorldCard(ctx context.Context, d round.Date, lang string) (db.WorldCard, bool, error)
	SetWorldCard(ctx context.Context, d round.Date, lang string, c db.WorldCard) error
}

type Service struct {
	Store  Store
	Zones  round.Zones
	FFmpeg string
	// Avatars gives the podium's photos; nil draws letters.
	Avatars Avatars
	// Draw encodes a scene to MP4; nil runs ffmpeg. A test sets it.
	Draw func(ctx context.Context, sc anim.Scene) ([]byte, error)
	Log  *slog.Logger
	Now  func() time.Time

	draw sync.Mutex // one drawing at a time
	mu   sync.Mutex
	last map[key]Card
}

// Avatars gives the profile photos of players as square JPEGs, leaving out
// people who have none.
type Avatars interface {
	Fetch(ctx context.Context, ids []int64) map[int64][]byte
}

// facesWait bounds asking Telegram for the podium's photos.
const facesWait = 20 * time.Second

// faces is the photos of the first three Telegram players of w.
func (s *Service) faces(ctx context.Context, w db.World) map[int64]*anim.Face {
	if s.Avatars == nil {
		return nil
	}
	var ids []int64
	for i, e := range w.Entries {
		if i == 3 {
			break
		}
		if !e.Site() {
			ids = append(ids, e.UserID)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, facesWait)
	defer cancel()
	out := map[int64]*anim.Face{}
	for id, b := range s.Avatars.Fetch(ctx, ids) {
		if img, err := jpeg.Decode(bytes.NewReader(b)); err == nil {
			if f := anim.NewFace(img); f != nil {
				out[id] = f
			}
		}
	}
	return out
}

type key struct {
	date round.Date
	lang string
}

// Card is the picture of one day's ranking in one language: the file
// Telegram has, or the MP4 to upload the first time.
type Card struct {
	Version string
	Final   bool
	FileID  string
	MP4     []byte
	// World is the ranking the picture shows.
	World db.World
	drawn time.Time
}

// ErrEmpty is a day nobody has been scored on yet.
var ErrEmpty = errors.New("nobody is ranked yet")

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Final reports whether d has closed in every zone.
func (s *Service) Final(d round.Date) bool { return !s.now().Before(s.Zones.LastReveal(d)) }

// Animation is the card as Telegram takes it.
func (c Card) Animation() telegram.Animation {
	return telegram.Animation{FileID: c.FileID, MP4: c.MP4, Width: 720, Height: 900, Duration: 12}
}

// state is d's ranking now and the version of the picture that shows it.
func (s *Service) state(ctx context.Context, d round.Date) (db.World, []db.WorldPin, string, bool, error) {
	final := s.Final(d)
	w, err := s.Store.World(ctx, d)
	if err != nil {
		return db.World{}, nil, "", false, err
	}
	if w.Players() == 0 {
		return db.World{}, nil, "", false, ErrEmpty
	}
	pins, err := s.Store.WorldPins(ctx, d)
	if err != nil {
		return db.World{}, nil, "", false, err
	}
	return w, pins, fingerprint(w, pins, final), final, nil
}

// soon reports whether prev is recent enough to stand for an open day's
// newer ranking: it must already show need, the person asking.
func (s *Service) soon(prev Card, final bool, need int64) bool {
	return !final && !prev.Final && s.now().Sub(prev.drawn) < redrawEvery &&
		(need == 0 || prev.World.RankOf(need) > 0)
}

// Fresh reports whether a message showing the picture of version shown is
// as new as Card would give now, so a redraw would change nothing.
func (s *Service) Fresh(ctx context.Context, d round.Date, lang, shown string, need int64) (bool, error) {
	if shown == "" {
		return false, nil
	}
	_, _, version, final, err := s.state(ctx, d)
	if err != nil || version == shown {
		return err == nil, err
	}
	s.mu.Lock()
	prev, ok := s.last[key{d, lang}]
	s.mu.Unlock()
	return ok && prev.Version == shown && s.soon(prev, final, need), nil
}

// Card is the picture of d's ranking so far in lang: kept or drawn now. need
// is a Telegram player the picture must show when they are ranked; a picture
// drawn a moment ago without them is not good enough.
func (s *Service) Card(ctx context.Context, d round.Date, lang string, need int64) (Card, error) {
	s.draw.Lock()
	defer s.draw.Unlock()
	w, pins, version, final, err := s.state(ctx, d)
	if err != nil {
		return Card{}, err
	}
	if w.RankOf(need) == 0 {
		need = 0
	}
	k := key{d, lang}
	s.mu.Lock()
	prev, ok := s.last[k]
	s.mu.Unlock()
	if ok && (prev.Version == version || s.soon(prev, final, need)) {
		return prev, nil
	}
	if kept, ok, err := s.Store.WorldCard(ctx, d, lang); err != nil {
		return Card{}, err
	} else if ok && kept.Version == version {
		c := Card{Version: version, Final: final, FileID: kept.FileID, World: w, drawn: s.now()}
		s.remember(k, c)
		return c, nil
	}
	rd, ok, err := s.Store.RoundOn(ctx, d)
	if err != nil || !ok {
		return Card{}, errors.Join(err, fmt.Errorf("no round on %s", d))
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return Card{}, err
	}
	start := time.Now()
	mp4, err := s.render(ctx, Scene(lang, rd.DayNo, answer, w, pins, s.faces(ctx, w)))
	if err != nil {
		return Card{}, err
	}
	s.Log.Info("world animation drawn", "date", d, "lang", lang, "players", w.Players(), "final", final, "took", time.Since(start).Round(time.Millisecond))
	c := Card{Version: version, Final: final, MP4: mp4, World: w, drawn: s.now()}
	s.remember(k, c)
	return c, nil
}

func (s *Service) remember(k key, c Card) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[key]Card{}
	}
	for old := range s.last {
		if old.date.DaysSince(k.date) < -3 {
			delete(s.last, old)
		}
	}
	s.last[k] = c
}

// Keep remembers the file_id Telegram gave the card's MP4.
func (s *Service) Keep(ctx context.Context, d round.Date, lang string, c Card, fileID string) {
	if fileID == "" || c.FileID != "" {
		return
	}
	k := key{d, lang}
	s.mu.Lock()
	if cur, ok := s.last[k]; ok && cur.Version == c.Version {
		cur.FileID, cur.MP4 = fileID, nil
		s.last[k] = cur
	}
	s.mu.Unlock()
	if err := s.Store.SetWorldCard(ctx, d, lang, db.WorldCard{Version: c.Version, FileID: fileID, Final: c.Final}); err != nil {
		s.Log.Warn("world animation file id not kept", "date", d, "lang", lang, "error", err)
	}
}

func (s *Service) render(ctx context.Context, sc anim.Scene) ([]byte, error) {
	if s.Draw != nil {
		return s.Draw(ctx, sc)
	}
	f, err := os.CreateTemp("", "maply-world-*.mp4")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if _, err := anim.Encode(ctx, sc, path, anim.Options{FFmpeg: s.FFmpeg}); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// fingerprint changes whenever the picture would: a new answer, a new top
// three, the day closing, the drawing itself.
func fingerprint(w db.World, pins []db.WorldPin, final bool) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%t|%d\n", renderer, final, len(pins))
	for _, p := range pins {
		fmt.Fprintf(h, "%.4f,%.4f,%.3f\n", p.Lat, p.Lon, p.Km)
	}
	for i, e := range w.Entries {
		if i == 3 {
			break
		}
		fmt.Fprintf(h, "%s|%.3f|%d\n", e.Name, e.Km, e.Points)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Scene is the world post of the day in lang: the pins in the order they
// came, coloured by how close, and the first three named.
func Scene(lang string, dayNo int, answer db.Answer, w db.World, pins []db.WorldPin, faces map[int64]*anim.Face) anim.Scene {
	guesses := make([]anim.Guess, len(pins))
	for i, p := range pins {
		guesses[i] = anim.Guess{Lat: p.Lat, Lon: p.Lon, Km: p.Km}
		if i < 3 {
			guesses[i].Medal = i + 1
		}
	}
	// The pins rain in the order they were put down.
	order := make([]int, len(pins))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return pins[a].AnsweredAt.Compare(pins[b].AnsweredAt) })
	for n, i := range order {
		if len(order) > 1 {
			guesses[i].At = float64(n) / float64(len(order)-1)
		}
	}
	var top [3]*anim.Winner
	for i, e := range w.Entries {
		if i == 3 {
			break
		}
		name := e.Name
		if name == "" && e.Site() {
			name = i18n.T(lang, "app.guest")
		}
		top[i] = &anim.Winner{Name: anim.Name(name), Km: e.Km, Points: float64(e.Points)}
		if !e.Site() {
			top[i].Face = faces[e.UserID]
		}
	}
	sc := anim.NewWorld(dayNo, ScenePlace(lang, answer), guesses, top, Unit(lang), lang)
	sc.UnitM = UnitM(lang)
	return sc
}

// ScenePlace is the place as the posts name it: in lang when the faces have
// its letters, else in English, else unnamed.
func ScenePlace(lang string, a db.Answer) anim.Place {
	p := anim.Place{Lat: a.Point.Lat, Lon: a.Point.Lon, Name: card.City(lang, a), Country: card.Country(lang, a)}
	if !anim.Covers(p.Name + p.Country) {
		p.Name, p.Country = card.City(i18n.DefaultLang, a), card.Country(i18n.DefaultLang, a)
	}
	if !anim.Covers(p.Name + p.Country) {
		p.Name, p.Country = "", ""
	}
	return p
}

// Unit is lang's kilometre when the faces can write it, else "km".
func Unit(lang string) string {
	if u := strings.TrimSpace(i18n.T(lang, "unit.km", "km", "")); u != "" && anim.Covers(u) {
		return u
	}
	return "km"
}

// UnitM is lang's metre when the faces can write it, else none, and the
// distances stay in kilometres.
func UnitM(lang string) string {
	if u := strings.TrimSpace(i18n.T(lang, "unit.m", "m", "")); u != "" && anim.Covers(u) {
		return u
	}
	return ""
}
