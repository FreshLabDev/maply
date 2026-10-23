// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/card"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// Animator draws the posts as looping MP4s through ffmpeg, one at a time:
// each already uses every core. Nothing it does is fatal to a post, which
// goes out as a photo or text instead.
type Animator struct {
	// FFmpeg is the ffmpeg binary.
	FFmpeg string
	mu     sync.Mutex

	// The morning post is the same picture in every chat, so it is drawn
	// once a day and then sent by file_id. A failed day is remembered too,
	// so the other chats go straight to the photo.
	day     round.Date
	mp4     []byte
	fileID  string
	failed  error
	dayLock sync.Mutex
}

// render draws one loop of the scene and returns the MP4.
func (a *Animator) render(ctx context.Context, s anim.Scene) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.CreateTemp("", "maply-*.mp4")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if _, err := anim.Encode(ctx, s, path, anim.Options{FFmpeg: a.FFmpeg}); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func animation(s anim.Scene, mp4 []byte) telegram.Animation {
	w, h := s.Size()
	return telegram.Animation{MP4: mp4, Width: w, Height: h, Duration: int(math.Round(s.Duration()))}
}

// sendPost sends the morning post: the little planet of the day's panorama
// when it can be drawn, else the photo.
func (s *Scheduler) sendPost(ctx context.Context, chatID int64, rd db.Round, caption string, markup telegram.Markup) (tg.Message, error) {
	if s.Anim != nil {
		a, err := s.morning(ctx, rd)
		if err == nil {
			var msg tg.Message
			msg, err = s.TG.SendAnimation(ctx, chatID, a, caption, markup)
			if err == nil {
				if a.FileID == "" && msg.Document != nil {
					s.Anim.keep(rd.Date, msg.Document.FileID)
				}
				return msg, nil
			}
			if !refusedPicture(err) {
				return msg, err
			}
			s.Anim.forget(rd.Date, err)
		}
		if !errors.Is(err, errNotPanorama) {
			s.Log.Warn("morning animation not sent, sending the photo", "chat", chatID, "date", rd.Date, "error", err)
		}
	}
	return s.sendPhoto(ctx, chatID, rd, caption, markup)
}

var errNotPanorama = errors.New("the place is a photo, not a panorama")

// morning is the day's morning animation: the file Telegram has, or the MP4
// drawn the first time it is asked for.
func (s *Scheduler) morning(ctx context.Context, rd db.Round) (telegram.Animation, error) {
	a := s.Anim
	a.dayLock.Lock()
	defer a.dayLock.Unlock()
	if a.day != rd.Date {
		a.day, a.mp4, a.fileID, a.failed = rd.Date, nil, "", nil
	}
	switch {
	case a.failed != nil:
		return telegram.Animation{}, a.failed
	case a.fileID != "":
		return telegram.Animation{FileID: a.fileID, Width: 720, Height: 900, Duration: 14}, nil
	case a.mp4 != nil:
		return telegram.Animation{MP4: a.mp4, Width: 720, Height: 900, Duration: 14}, nil
	}
	puzzle, err := s.Store.Puzzle(ctx, rd.PlaceID)
	if err != nil {
		return telegram.Animation{}, err
	}
	if puzzle.Kind != "panorama" {
		a.failed = errNotPanorama
		return telegram.Animation{}, a.failed
	}
	pano, err := loadPano(filepath.Join(s.ImagesDir, puzzle.ImageKey))
	if err != nil {
		a.failed = err
		return telegram.Animation{}, err
	}
	scene := anim.NewMorning(rd.DayNo, pano, revealText(), "en")
	start := time.Now()
	mp4, err := a.render(ctx, scene)
	if err != nil {
		a.failed = fmt.Errorf("morning animation: %w", err)
		return telegram.Animation{}, a.failed
	}
	s.Log.Info("morning animation drawn", "date", rd.Date, "bytes", len(mp4), "took", time.Since(start).Round(time.Millisecond))
	a.mp4 = mp4
	return animation(scene, mp4), nil
}

func (a *Animator) keep(d round.Date, fileID string) {
	a.dayLock.Lock()
	defer a.dayLock.Unlock()
	if a.day == d {
		a.fileID, a.mp4 = fileID, nil
	}
}

func (a *Animator) forget(d round.Date, err error) {
	a.dayLock.Lock()
	defer a.dayLock.Unlock()
	if a.day == d {
		a.failed = err
	}
}

func loadPano(path string) (*anim.Pano, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("panorama: %w", err)
	}
	if b := img.Bounds(); b.Dx() != 2*b.Dy() {
		return nil, fmt.Errorf("panorama is %dx%d, not 2:1", b.Dx(), b.Dy())
	}
	return anim.NewPano(img), nil
}

// sendScene sends a scene drawn for one chat. ok is false when it was not
// sent and nothing reached the chat, so the caller sends what it sent before
// animations.
func (s *Scheduler) sendScene(ctx context.Context, chatID int64, scene anim.Scene, caption string, markup telegram.Markup) (tg.Message, bool, error) {
	if s.Anim == nil || scene == nil {
		return tg.Message{}, false, nil
	}
	start := time.Now()
	mp4, err := s.Anim.render(ctx, scene)
	if err == nil {
		s.Log.Debug("animation drawn", "chat", chatID, "bytes", len(mp4), "took", time.Since(start).Round(time.Millisecond))
		var msg tg.Message
		if msg, err = s.TG.SendAnimation(ctx, chatID, animation(scene, mp4), caption, markup); err == nil || !refusedPicture(err) {
			return msg, true, err
		}
	}
	s.Log.Warn("animation not sent, falling back", "chat", chatID, "error", err)
	return tg.Message{}, false, nil
}

// animationID is the file_id of a message sent as an animation, for sharing
// the same one later; a picture has none. Telegram sends an animation back
// with its document too, whether it was uploaded or sent by file_id.
func animationID(msg tg.Message) string {
	if msg.Document == nil || len(msg.Photo) > 0 {
		return ""
	}
	return msg.Document.FileID
}

// resultScene is the group's result as an animation: the pins of who
// answered, the place and the podium. Nil when nobody was scored.
func resultScene(lang string, dayNo int, answer db.Answer, guesses []db.Guess, streaks map[int64]int) anim.Scene {
	// The guesses come best first, as in the table; the pins drop in the
	// order people answered.
	rank := make(map[int64]int, len(guesses))
	for i, g := range guesses {
		rank[g.UserID] = i + 1
	}
	var players []anim.Player
	byTime := append([]db.Guess(nil), guesses...)
	sortByAnswer(byTime)
	for _, g := range byTime {
		if g.Km == nil {
			continue
		}
		p := anim.Player{Name: anim.Name(g.FirstName), Lat: g.Pin.Lat, Lon: g.Pin.Lon, Km: *g.Km, Points: float64(g.Total()), Rank: rank[g.UserID]}
		if n := streaks[g.UserID]; n >= 2 {
			p.Streak = n
		}
		players = append(players, p)
	}
	if len(players) == 0 {
		return nil
	}
	return anim.NewResults(dayNo, postPlace(lang, answer), players, postUnit(lang), lang)
}

// The brand's faces write Latin and Cyrillic. A place in another script goes
// on the post in the default language, a unit as "km", and a name keeps the
// letters the faces have, or becomes "?".
func postPlace(lang string, a db.Answer) anim.Place {
	p := anim.Place{Lat: a.Point.Lat, Lon: a.Point.Lon, Name: card.City(lang, a), Country: card.Country(lang, a)}
	if !anim.Covers(p.Name + p.Country) {
		p.Name, p.Country = card.City(i18n.DefaultLang, a), card.Country(i18n.DefaultLang, a)
	}
	if !anim.Covers(p.Name + p.Country) {
		p.Name, p.Country = "", ""
	}
	return p
}

func postUnit(lang string) string {
	if u := strings.TrimSpace(i18n.T(lang, "unit.km", "km", "")); u != "" && anim.Covers(u) {
		return u
	}
	return "km"
}
