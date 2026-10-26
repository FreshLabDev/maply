// SPDX-License-Identifier: Apache-2.0

// Package card draws the result as a picture: a map with the true point,
// every player's pin and dashed lines to the point, the day's number, the
// place and the top three. The service serves the card as an internal page in
// the Mini App's style, and a headless Chromium in its own container opens
// that page and takes the screenshot. Nothing here is on the critical path of
// the game: when the renderer is missing or fails, callers fall back to the
// place's photo.
//
// The page opens only with a token signed for the chat and the date, and
// only after that chat's reveal, so a leaked link shows nothing the chat has
// not already been shown.
package card

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// Store is what the page and the share caption read.
type Store interface {
	RoundOn(ctx context.Context, d round.Date) (db.Round, bool, error)
	Answer(ctx context.Context, placeID int64) (db.Answer, error)
	ChatGuesses(ctx context.Context, d round.Date, chatID int64) ([]db.Guess, error)
	GuessOf(ctx context.Context, d round.Date, userID int64) (db.Guess, bool, error)
	ChatLang(ctx context.Context, chatID int64) (string, error)
	UserLang(ctx context.Context, userID int64) (string, error)
	ChatZone(ctx context.Context, chatID int64) (string, error)
	Contexts(ctx context.Context, userID int64) ([]db.Context, error)
	// ResultAnimation is the file_id of the animation the chat's result went
	// out as, or empty.
	ResultAnimation(ctx context.Context, chatID int64, d round.Date) (string, error)
}

// Saver hands a message to Telegram for a person to send with shareMessage.
type Saver interface {
	SavePreparedPhoto(ctx context.Context, userID int64, p telegram.PreparedPhoto) (telegram.PreparedMessage, error)
	SavePreparedAnimation(ctx context.Context, userID int64, a telegram.PreparedAnimation) (telegram.PreparedMessage, error)
}

// ErrDisabled means there is no renderer; callers use the photo.
var ErrDisabled = errors.New("card renderer is not configured")

type Service struct {
	Store  Store
	Signer *auth.Signer
	Zones  round.Zones
	// Shot opens a page and returns its screenshot. Nil disables cards.
	Shot Shot
	// PageURL is where the renderer reaches this service, on the internal
	// network: http://maply-bot:8080.
	PageURL string
	// PublicURL is the https address Telegram fetches shared images from.
	// Empty disables sharing, not the cards.
	PublicURL string
	// Username is the bot's, for the Play button on a shared card.
	Username string
	TG       Saver
	// Avatars gives the players' kept photos for their marks; nil keeps the
	// letters.
	Avatars Avatars
	Log     *slog.Logger
	Now     func() time.Time

	images images
}

// Avatars gives the kept profile photos of players as square JPEGs, without
// asking Telegram, leaving out people who have none.
type Avatars interface {
	Kept(ctx context.Context, ids []int64) map[int64][]byte
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Enabled reports whether cards can be drawn at all.
func (s *Service) Enabled() bool { return s != nil && s.Shot != nil }

// CanShare reports whether a card can go through shareMessage.
func (s *Service) CanShare() bool {
	return s.Enabled() && s.PublicURL != "" && s.TG != nil
}

// Result is the card of chatID's result for d as a 1280×1280 PNG. A negative
// chatID is a group; a positive one is a person's DM, and the card holds only
// their own pin.
func (s *Service) Result(ctx context.Context, chatID int64, d round.Date) ([]byte, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	token, err := s.Signer.SignCard(auth.Payload{Date: d, ChatID: chatID})
	if err != nil {
		return nil, err
	}
	page := fmt.Sprintf("%s/card/result/%d/%s?t=%s", strings.TrimRight(s.PageURL, "/"), chatID, d, token)
	return s.Shot.Screenshot(ctx, page)
}

// Share is one person sharing the result they are looking at.
type Share struct {
	UserID int64
	// ChatID is the context the Mini App was opened from: the group, or the
	// person's own id for a DM.
	ChatID int64
	Date   round.Date
}

// Prepare returns the id of the prepared message the Mini App passes to
// shareMessage. A result that went out as an animation is shared as that
// animation; otherwise the card is drawn and made reachable for Telegram.
// The caller has checked that the result is open to this person and has
// scored the day.
func (s *Service) Prepare(ctx context.Context, sh Share) (string, error) {
	if !s.CanShare() {
		return "", ErrDisabled
	}
	rd, ok, err := s.Store.RoundOn(ctx, sh.Date)
	if err != nil || !ok {
		return "", orMissing(err, "round")
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return "", err
	}
	lang, err := s.langOf(ctx, sh.ChatID)
	if err != nil {
		return "", err
	}
	caption := Quoted(lang, rd.DayNo, answer)
	if g, answered, err := s.Store.GuessOf(ctx, sh.Date, sh.UserID); err != nil {
		return "", err
	} else if answered && g.Km != nil {
		caption += "\n" + i18n.T(lang, "result.your", "dist", i18n.FormatDistance(lang, *g.Km), "points", i18n.FormatNumber(lang, g.Total()))
	}
	markup := telegram.Markup{{{Text: i18n.T(lang, "btn.play"), URL: "https://t.me/" + s.Username + "/play"}}}
	if fileID, err := s.Store.ResultAnimation(ctx, sh.ChatID, sh.Date); err != nil {
		return "", err
	} else if fileID != "" {
		id, err := newKey()
		if err != nil {
			return "", err
		}
		msg, err := s.TG.SavePreparedAnimation(ctx, sh.UserID, telegram.PreparedAnimation{ID: id, FileID: fileID, Caption: caption, Markup: markup})
		if err == nil {
			return msg.ID, nil
		}
		if s.Log != nil {
			s.Log.Warn("result animation not prepared, sharing the card", "chat", sh.ChatID, "date", sh.Date, "error", err)
		}
	}
	img, err := s.sharedImage(ctx, sh.ChatID, sh.Date)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(s.PublicURL, "/") + "/card/img/" + img.key
	msg, err := s.TG.SavePreparedPhoto(ctx, sh.UserID, telegram.PreparedPhoto{
		ID: img.key, PhotoURL: base + ".jpg", ThumbURL: base + ".t.jpg", Caption: caption, Markup: markup,
	})
	if err != nil {
		return "", err
	}
	return msg.ID, nil
}

func orMissing(err error, what string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s not found", what)
}

// langOf is the language of the card's text: the group's, or the person's
// for their DM.
func (s *Service) langOf(ctx context.Context, chatID int64) (string, error) {
	var lang string
	var err error
	if chatID < 0 {
		lang, err = s.Store.ChatLang(ctx, chatID)
	} else {
		lang, err = s.Store.UserLang(ctx, chatID)
	}
	return i18n.Resolve(lang), err
}

// opens is when the result is out for this chat. A group's is its own reveal.
// A person's DM waits for the latest reveal among their chats, as the API
// does, so a card never tells someone what one of their groups is still
// guessing.
func (s *Service) opens(ctx context.Context, chatID int64, d round.Date) (time.Time, error) {
	zone, err := s.Store.ChatZone(ctx, chatID)
	if err != nil {
		return time.Time{}, err
	}
	lang, err := s.langOf(ctx, chatID)
	if err != nil {
		return time.Time{}, err
	}
	reveal := d.Reveal(s.Zones.For(zone, lang))
	if chatID < 0 {
		return reveal, nil
	}
	contexts, err := s.Store.Contexts(ctx, chatID)
	if err != nil {
		return time.Time{}, err
	}
	for _, c := range contexts {
		if r := d.Reveal(s.Zones.For(c.Zone, c.Lang)); r.After(reveal) {
			reveal = r
		}
	}
	return reveal, nil
}

// images keeps what shared cards point at. Telegram fetches the picture when
// the message is sent, up to an hour after it was prepared, so an image
// outlives that; the same card shared again soon after reuses the render.
type images struct {
	mu     sync.Mutex
	byKey  map[string]*shared
	recent map[ref]string
}

type ref struct {
	chat int64
	date round.Date
}

type shared struct {
	key         string
	jpeg, thumb []byte
	made        time.Time
	ref         ref
}

const (
	imageLife   = 2 * time.Hour
	imageReuse  = 10 * time.Minute
	imageMax    = 64
	thumbSide   = 320
	jpegQuality = 88
)

func (s *Service) sharedImage(ctx context.Context, chatID int64, d round.Date) (*shared, error) {
	r, now := ref{chatID, d}, s.now()
	if img := s.images.reusable(r, now); img != nil {
		return img, nil
	}
	pngData, err := s.Result(ctx, chatID, d)
	if err != nil {
		return nil, err
	}
	full, thumb, err := encodeShared(pngData)
	if err != nil {
		return nil, err
	}
	key, err := newKey()
	if err != nil {
		return nil, err
	}
	img := &shared{key: key, jpeg: full, thumb: thumb, made: now, ref: r}
	s.images.put(img, now)
	return img, nil
}

func (m *images) reusable(r ref, now time.Time) *shared {
	m.mu.Lock()
	defer m.mu.Unlock()
	if img := m.byKey[m.recent[r]]; img != nil && now.Sub(img.made) < imageReuse {
		return img
	}
	return nil
}

func (m *images) put(img *shared, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byKey == nil {
		m.byKey, m.recent = map[string]*shared{}, map[ref]string{}
	}
	for k, old := range m.byKey {
		if now.Sub(old.made) >= imageLife {
			m.drop(k, old)
		}
	}
	for len(m.byKey) >= imageMax {
		var oldest *shared
		for _, old := range m.byKey {
			if oldest == nil || old.made.Before(oldest.made) {
				oldest = old
			}
		}
		m.drop(oldest.key, oldest)
	}
	m.byKey[img.key] = img
	m.recent[img.ref] = img.key
}

func (m *images) drop(key string, img *shared) {
	delete(m.byKey, key)
	if m.recent[img.ref] == key {
		delete(m.recent, img.ref)
	}
}

func (m *images) get(key string, now time.Time) *shared {
	m.mu.Lock()
	defer m.mu.Unlock()
	if img := m.byKey[key]; img != nil && now.Sub(img.made) < imageLife {
		return img
	}
	return nil
}

func newKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// encodeShared makes what an inline photo result takes: JPEG, with a small
// thumbnail beside it. The renderer's PNG is lossless and several times larger.
func encodeShared(pngData []byte) (full, thumb []byte, err error) {
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, nil, fmt.Errorf("decode card: %w", err)
	}
	// The PNG is opaque, but a JPEG has no alpha: flatten it onto white.
	flat := image.NewRGBA(src.Bounds())
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), src, src.Bounds().Min, draw.Over)
	var a, b bytes.Buffer
	if err := jpeg.Encode(&a, flat, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, nil, err
	}
	if err := jpeg.Encode(&b, shrink(flat, thumbSide), &jpeg.Options{Quality: 80}); err != nil {
		return nil, nil, err
	}
	return a.Bytes(), b.Bytes(), nil
}

// shrink averages blocks of pixels down to a picture of side `side`; the
// card is square. It is for a thumbnail, so a box filter is enough.
func shrink(src *image.RGBA, side int) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= side || h <= side {
		return src
	}
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		y0, y1 := y*h/side, (y+1)*h/side
		for x := 0; x < side; x++ {
			x0, x1 := x*w/side, (x+1)*w/side
			var r, g, b, n int
			for yy := y0; yy < y1; yy++ {
				row := src.Pix[yy*src.Stride:]
				for xx := x0; xx < x1; xx++ {
					p := row[xx*4:]
					r, g, b, n = r+int(p[0]), g+int(p[1]), b+int(p[2]), n+1
				}
			}
			o := out.Pix[y*out.Stride+x*4:]
			o[0], o[1], o[2], o[3] = uint8(r/n), uint8(g/n), uint8(b/n), 255
		}
	}
	return out
}
