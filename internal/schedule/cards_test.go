// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

type photoStore struct {
	Store
	fileID string
}

func (p photoStore) PhotoFileID(context.Context, round.Date) (string, error)  { return p.fileID, nil }
func (p photoStore) SetPhotoFileID(context.Context, round.Date, string) error { return nil }
func (p photoStore) Puzzle(context.Context, int64) (db.Puzzle, error) {
	return db.Puzzle{ImageKey: "Zk3qT9xWm2LpV7aN"}, nil
}

type photoTG struct {
	Telegram
	photos []telegram.Photo
}

func (f *photoTG) SendPhoto(_ context.Context, _ int64, p telegram.Photo, _ string, _ telegram.Markup) (tg.Message, error) {
	f.photos = append(f.photos, p)
	return tg.Message{MessageID: int64(len(f.photos))}, nil
}

type fakeCards struct {
	png   []byte
	err   error
	calls []int64
}

func (f *fakeCards) Result(_ context.Context, chatID int64, _ round.Date) ([]byte, error) {
	f.calls = append(f.calls, chatID)
	return f.png, f.err
}

func resultSender(cards Cards, store Store) (*Scheduler, *photoTG) {
	tgc := &photoTG{}
	return &Scheduler{
		Store: store, TG: tgc, Cards: cards, ImagesDir: "/images",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now,
	}, tgc
}

func TestResultGoesOutAsTheCard(t *testing.T) {
	cards := &fakeCards{png: []byte("png")}
	s, tgc := resultSender(cards, photoStore{})
	chat := db.Chat{ID: -100, Kind: "group"}
	if _, err := s.sendResult(context.Background(), chat, db.Round{Date: round.NewDate(2026, 11, 2)}, "caption", nil); err != nil {
		t.Fatal(err)
	}
	if len(tgc.photos) != 1 || string(tgc.photos[0].PNG) != "png" || tgc.photos[0].Path != "" || tgc.photos[0].FileID != "" {
		t.Fatalf("photos = %+v", tgc.photos)
	}
	if len(cards.calls) != 1 || cards.calls[0] != -100 {
		t.Fatalf("renderer was asked for %v", cards.calls)
	}
}

func TestResultFallsBackToThePhotoWhenTheCardFails(t *testing.T) {
	for name, cards := range map[string]Cards{
		"renderer error": &fakeCards{err: errors.New("browser is gone")},
		"no renderer":    nil,
	} {
		s, tgc := resultSender(cards, photoStore{})
		if _, err := s.sendResult(context.Background(), db.Chat{ID: -100, Kind: "group"}, db.Round{Date: round.NewDate(2026, 11, 2)}, "caption", nil); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(tgc.photos) != 1 || tgc.photos[0].PNG != nil || tgc.photos[0].Path != "/images/Zk3qT9xWm2LpV7aN" {
			t.Errorf("%s: photos = %+v", name, tgc.photos)
		}
	}
}

// Telegram refusing the picture itself means nothing was delivered, so the
// photo goes instead. A failure that may have delivered the card, or a chat
// that cannot be reached, is returned for the job's retry and its bookkeeping.
func TestResultFallsBackWhenTelegramRefusesThePicture(t *testing.T) {
	round1 := db.Round{Date: round.NewDate(2026, 11, 2)}
	for name, c := range map[string]struct {
		err      error
		wantSent int
		wantErr  bool
	}{
		"bad picture":    {&tg.APIError{StatusCode: 400, ErrorCode: 400, Description: "Bad Request: PHOTO_INVALID_DIMENSIONS"}, 2, false},
		"network":        {errors.New("connection reset"), 1, true},
		"flood":          {&tg.APIError{StatusCode: 429, ErrorCode: 429, Description: "Too Many Requests"}, 1, true},
		"chat gone":      {&tg.APIError{StatusCode: 403, ErrorCode: 403, Description: "Forbidden: bot was blocked by the user"}, 1, true},
		"chat not found": {&tg.APIError{StatusCode: 400, ErrorCode: 400, Description: "Bad Request: chat not found"}, 1, true},
	} {
		s, tgc := resultSender(&fakeCards{png: []byte("png")}, photoStore{})
		s.TG = &refusingTG{photoTG: tgc, err: c.err, once: true}
		_, err := s.sendResult(context.Background(), db.Chat{ID: -100}, round1, "caption", nil)
		if (err != nil) != c.wantErr || len(tgc.photos) != c.wantSent {
			t.Errorf("%s: err = %v, %d sends; want error %v, %d sends", name, err, len(tgc.photos), c.wantErr, c.wantSent)
		}
	}
}

// refusingTG refuses the first send, then behaves.
type refusingTG struct {
	*photoTG
	err  error
	once bool
}

func (f *refusingTG) SendPhoto(ctx context.Context, chatID int64, p telegram.Photo, caption string, m telegram.Markup) (tg.Message, error) {
	if f.once {
		f.once = false
		_, _ = f.photoTG.SendPhoto(ctx, chatID, p, caption, m)
		return tg.Message{}, f.err
	}
	return f.photoTG.SendPhoto(ctx, chatID, p, caption, m)
}
