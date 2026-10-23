// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

func TestPostNameKeepsWhatTheFacesWrite(t *testing.T) {
	for in, want := range map[string]string{
		"Аня":                        "Аня",
		"  Lucía  ":                  "Lucía",
		"李雷":                         "?",
		"Ann 🙂":                      "Ann",
		"":                           "?",
		"Christopher-Alexander Long": "Christopher-Ale…",
	} {
		if got := postName(in); got != want {
			t.Errorf("postName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPostPlaceFallsBackToTheDefaultLanguage(t *testing.T) {
	a := db.Answer{Point: geo.Point{Lat: -6.16, Lon: 39.19}, CountryISO: "TZ",
		City: map[string]string{"en": "Zanzibar", "ru": "Занзибар", "zh": "桑给巴尔"}}
	if p := postPlace("ru", a); p.Name != "Занзибар" || p.Country != "Танзания" {
		t.Errorf("ru: %+v", p)
	}
	if p := postPlace("zh", a); p.Name != "Zanzibar" || p.Country != "Tanzania" {
		t.Errorf("zh: %+v", p)
	}
	if postUnit("uk") != "км" || postUnit("ja") != "km" {
		t.Errorf("units: %q, %q", postUnit("uk"), postUnit("ja"))
	}
}

// The pins drop in the order people answered and take their place from the
// table, which comes best first.
func TestResultSceneRanksAndOrders(t *testing.T) {
	km := func(v float64) *float64 { return &v }
	pts := func(v int) *int { return &v }
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	guesses := []db.Guess{
		{UserID: 2, FirstName: "Ana", Km: km(10), PtsKm: pts(4000), PtsBonus: pts(800), AnsweredAt: at.Add(time.Hour)},
		{UserID: 1, FirstName: "Pin", Km: km(900), PtsKm: pts(2000), PtsBonus: pts(0), AnsweredAt: at},
		{UserID: 3, FirstName: "Late", AnsweredAt: at.Add(-time.Hour)},
	}
	s := resultScene("en", 3, db.Answer{CountryISO: "PT", City: map[string]string{"en": "Lisbon"}}, guesses, map[int64]int{2: 6, 1: 1})
	r, ok := s.(*anim.Results)
	if !ok {
		t.Fatalf("scene = %T", s)
	}
	if len(r.Players) != 2 || r.Players[0].Name != "Pin" || r.Players[0].Rank != 2 || r.Players[1].Rank != 1 {
		t.Fatalf("players = %+v", r.Players)
	}
	if r.Players[1].Streak != 6 || r.Players[0].Streak != 0 || r.Players[1].Points != 4800 {
		t.Fatalf("streaks and points = %+v", r.Players)
	}
	if resultScene("en", 3, db.Answer{}, guesses[2:], nil) != nil {
		t.Fatal("a result nobody was scored in is drawn")
	}
}

type animTG struct {
	photoTG
	animations []telegram.Animation
	refuse     bool
}

func (f *animTG) SendAnimation(_ context.Context, _ int64, a telegram.Animation, _ string, _ telegram.Markup) (tg.Message, error) {
	if f.refuse {
		return tg.Message{}, &tg.APIError{StatusCode: 400, Description: "Bad Request: wrong file"}
	}
	f.animations = append(f.animations, a)
	return tg.Message{MessageID: 100, Document: &tg.Document{FileID: "A1"}}, nil
}

type panoStore struct {
	photoStore
	kind string
}

func (p panoStore) Puzzle(context.Context, int64) (db.Puzzle, error) {
	return db.Puzzle{ImageKey: "pano", Kind: p.kind}, nil
}

// The morning post is drawn once a day and goes to every other chat by
// file_id; a photo place, or Telegram refusing the animation, sends the
// photo.
func TestMorningAnimationOnceADay(t *testing.T) {
	ffmpeg, err := osexec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 128, 64))
	for y := range 64 {
		for x := range 128 {
			img.Set(x, y, color.RGBA{uint8(2 * x), uint8(4 * y), 90, 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, "pano"))
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	f.Close()

	send := func(kind string, refuse bool, chats ...int64) *animTG {
		tgc := &animTG{refuse: refuse}
		s, _ := resultSender(nil, panoStore{kind: kind})
		s.TG, s.ImagesDir, s.Anim = tgc, dir, &Animator{FFmpeg: ffmpeg}
		rd := db.Round{Date: round.NewDate(2026, 11, 2), DayNo: 4, PlaceID: 1}
		for _, c := range chats {
			if _, err := s.sendPost(context.Background(), c, rd, "caption", nil); err != nil {
				t.Fatal(err)
			}
		}
		return tgc
	}
	tgc := send("panorama", false, 1, 2)
	if len(tgc.animations) != 2 || len(tgc.animations[0].MP4) == 0 || tgc.animations[1].FileID != "A1" || len(tgc.photos) != 0 {
		t.Fatalf("animations %d, photos %d", len(tgc.animations), len(tgc.photos))
	}
	if tgc := send("photo", false, 1); len(tgc.animations) != 0 || len(tgc.photos) != 1 {
		t.Fatalf("photo place: animations %d, photos %d", len(tgc.animations), len(tgc.photos))
	}
	if tgc := send("panorama", true, 1, 2); len(tgc.photos) != 2 {
		t.Fatalf("refused: photos %d", len(tgc.photos))
	}
}
