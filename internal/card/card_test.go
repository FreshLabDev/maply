// SPDX-License-Identifier: Apache-2.0

package card

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
	"github.com/FreshLabDev/maply/web"
)

const botToken = "123456:test-token"

var (
	day    = round.NewDate(2026, time.November, 2)
	group  = int64(-1001234567890)
	person = int64(777)
)

type fakeStore struct {
	guesses  []db.Guess
	zone     string
	lang     string
	contexts []db.Context
}

func (f *fakeStore) RoundOn(context.Context, round.Date) (db.Round, bool, error) {
	return db.Round{Date: day, DayNo: 12, PlaceID: 5}, true, nil
}
func (f *fakeStore) Answer(context.Context, int64) (db.Answer, error) {
	return db.Answer{
		Point: geo.Point{Lat: 38.72, Lon: -9.14}, CountryISO: "PT",
		City: map[string]string{"en": "Lisbon", "ru": "Лиссабон"},
		Fact: map[string]string{"en": "Hills"},
	}, nil
}
func (f *fakeStore) ChatGuesses(context.Context, round.Date, int64) ([]db.Guess, error) {
	return f.guesses, nil
}
func (f *fakeStore) GuessOf(_ context.Context, _ round.Date, id int64) (db.Guess, bool, error) {
	for _, g := range f.guesses {
		if g.UserID == id {
			return g, true, nil
		}
	}
	return db.Guess{}, false, nil
}
func (f *fakeStore) ChatLang(context.Context, int64) (string, error) { return f.lang, nil }
func (f *fakeStore) UserLang(context.Context, int64) (string, error) { return f.lang, nil }
func (f *fakeStore) ChatZone(context.Context, int64) (string, error) { return f.zone, nil }
func (f *fakeStore) Contexts(context.Context, int64) ([]db.Context, error) {
	return f.contexts, nil
}

type fakeShot struct {
	mu    sync.Mutex
	urls  []string
	err   error
	image []byte
}

func (f *fakeShot) Screenshot(_ context.Context, pageURL string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, pageURL)
	return f.image, f.err
}

type fakeSaver struct {
	user int64
	got  telegram.PreparedPhoto
	err  error
}

func (f *fakeSaver) SavePreparedPhoto(_ context.Context, user int64, p telegram.PreparedPhoto) (telegram.PreparedMessage, error) {
	f.user, f.got = user, p
	return telegram.PreparedMessage{ID: p.ID}, f.err
}

func blankPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, Size, Size))
	for y := 0; y < Size; y++ {
		for x := 0; x < Size; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 36, G: 90, B: 60, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func guess(id int64, name string, rank int) db.Guess {
	km, a, b := float64(100*rank), 3000-100*rank, 500
	return db.Guess{UserID: id, FirstName: name, Pin: geo.Point{Lat: 40 + float64(rank), Lon: -8}, Km: &km, PtsKm: &a, PtsBonus: &b}
}

// Kyiv reveals at 20:00, which is 18:00 UTC on 2 November 2026. By 06:00 UTC
// the next morning every zone maply offers has revealed.
func service(t *testing.T, st *fakeStore, shot *fakeShot, now time.Time) *Service {
	t.Helper()
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	return &Service{
		Store: st, Signer: auth.NewSigner(botToken), Zones: zones, Shot: shot,
		PageURL: "http://maply-bot:8080", PublicURL: "https://maply.example/", Username: "maply_bot",
		Now: func() time.Time { return now },
	}
}

var (
	afterKyivReveal = time.Date(2026, time.November, 2, 18, 5, 0, 0, time.UTC)
	afterReveal     = time.Date(2026, time.November, 3, 6, 0, 0, 0, time.UTC)
)

func get(h http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestResultAsksTheRendererForASignedPageOfThatChat(t *testing.T) {
	shot := &fakeShot{image: []byte("png")}
	s := service(t, &fakeStore{}, shot, afterReveal)
	got, err := s.Result(context.Background(), group, day)
	if err != nil || string(got) != "png" {
		t.Fatalf("Result = %q, %v", got, err)
	}
	u, err := url.Parse(shot.urls[0])
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "maply-bot:8080" || u.Path != "/card/result/-1001234567890/2026-11-02" {
		t.Fatalf("page = %s", shot.urls[0])
	}
	p, err := s.Signer.VerifyCard(u.Query().Get("t"))
	if err != nil || p.ChatID != group || p.Date != day {
		t.Fatalf("token = %+v, %v", p, err)
	}
}

func TestResultWithoutARendererIsDisabled(t *testing.T) {
	s := service(t, &fakeStore{}, nil, afterReveal)
	s.Shot = nil
	if _, err := s.Result(context.Background(), group, day); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
	var none *Service
	if none.Enabled() {
		t.Fatal("a nil service is enabled")
	}
}

func TestResultKeepsTheRenderersFailure(t *testing.T) {
	boom := errors.New("browser is gone")
	s := service(t, &fakeStore{}, &fakeShot{err: boom}, afterReveal)
	if _, err := s.Result(context.Background(), group, day); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func pageURL(t *testing.T, s *Service, chat int64, d round.Date) string {
	t.Helper()
	tok, err := s.Signer.SignCard(auth.Payload{Date: d, ChatID: chat})
	if err != nil {
		t.Fatal(err)
	}
	return "/card/result/" + itoa(chat) + "/" + d.String() + "?t=" + tok
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestPageShowsTheTopThreeInTheChatsLanguage(t *testing.T) {
	st := &fakeStore{lang: "ru", guesses: []db.Guess{
		guess(1, "Анна", 1), guess(2, "<script>alert(1)</script>", 2), guess(3, "Cy", 3), guess(4, "Dee", 4), guess(5, "Eve", 5),
	}}
	s := service(t, st, &fakeShot{}, afterReveal)
	rec := get(s.Handler(), pageURL(t, s, group, day))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{`lang="ru"`, "maply", "#12", "Лиссабон, Португалия", "100 км", "Анна", "&#43;2", `<i class="av w">А</i>`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>alert") || !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("a name went into the page unescaped:\n%s", body)
	}
	if strings.Contains(body, "Dee") {
		t.Errorf("a fourth player is in the table:\n%s", body)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v", rec.Header())
	}
	// All five pins go on the map, and the data cannot close its own tag.
	data := body[strings.Index(body, `id="card-data">`):]
	data = data[:strings.Index(data, "</script>")]
	if strings.Count(data, `"initial"`) != 5 || strings.Contains(data[len(`id="card-data">`):], "<") {
		t.Errorf("map data = %s", data)
	}
}

func TestPageForALoneDMHoldsOnlyThatPerson(t *testing.T) {
	st := &fakeStore{lang: "en", guesses: []db.Guess{guess(person, "Mia", 1), guess(9, "Other", 2)}}
	s := service(t, st, &fakeShot{}, afterReveal)
	rec := get(s.Handler(), pageURL(t, s, person, day))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Mia") || strings.Contains(rec.Body.String(), "Other") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestPageWithNoAnswersSaysSo(t *testing.T) {
	s := service(t, &fakeStore{lang: "en"}, &fakeShot{}, afterReveal)
	rec := get(s.Handler(), pageURL(t, s, group, day))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Nobody answered today.") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestPageRefusesWhatTheTokenDoesNotCover(t *testing.T) {
	st := &fakeStore{lang: "en", guesses: []db.Guess{guess(1, "A", 1)}}
	s := service(t, st, &fakeShot{}, afterReveal)
	h := s.Handler()
	good := pageURL(t, s, group, day)
	other, _ := s.Signer.SignCard(auth.Payload{Date: day, ChatID: group + 1})
	start, _ := s.Signer.Sign(auth.Payload{Date: day, ChatID: group})
	for name, target := range map[string]string{
		"no token":           "/card/result/-1001234567890/2026-11-02",
		"another chat":       "/card/result/-1001234567890/2026-11-02?t=" + other,
		"another date":       strings.Replace(good, "2026-11-02", "2026-11-03", 1),
		"a startapp payload": "/card/result/-1001234567890/2026-11-02?t=" + start,
		"a bad date":         "/card/result/-1001234567890/yesterday?t=" + other,
		"a bad chat":         "/card/result/abc/2026-11-02?t=" + other,
	} {
		rec := get(h, target)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `data-ready="failed"`) {
			t.Errorf("%s: status %d, body %q", name, rec.Code, rec.Body)
		}
	}
	if rec := get(h, good); rec.Code != http.StatusOK {
		t.Errorf("the good link got %d", rec.Code)
	}
}

func TestPageStaysClosedUntilTheChatsReveal(t *testing.T) {
	st := &fakeStore{lang: "uk", guesses: []db.Guess{guess(1, "A", 1)}}
	// 17:55 UTC is 19:55 in Kyiv, five minutes before the reveal.
	s := service(t, st, &fakeShot{}, time.Date(2026, time.November, 2, 17, 55, 0, 0, time.UTC))
	rec := get(s.Handler(), pageURL(t, s, group, day))
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Lisbon") || strings.Contains(rec.Body.String(), "Лісабон") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	// A DM waits for the latest reveal among the person's chats: here a
	// group in Los Angeles, hours after Kyiv.
	st.contexts = []db.Context{{ChatID: -5, Zone: "America/Los_Angeles"}}
	s.Now = func() time.Time { return afterKyivReveal }
	if rec := get(s.Handler(), pageURL(t, s, person, day)); rec.Code != http.StatusForbidden {
		t.Fatalf("a DM opened before its latest reveal: %d", rec.Code)
	}
}

func TestPrepareSavesAJPEGMessageTelegramCanFetch(t *testing.T) {
	st := &fakeStore{lang: "ru", guesses: []db.Guess{guess(person, "Mia", 1)}}
	shot := &fakeShot{image: blankPNG(t)}
	saver := &fakeSaver{}
	s := service(t, st, shot, afterReveal)
	s.TG = saver
	id, err := s.Prepare(context.Background(), Share{UserID: person, ChatID: group, Date: day})
	if err != nil {
		t.Fatal(err)
	}
	p := saver.got
	if saver.user != person || id != p.ID || len(p.ID) != 22 {
		t.Fatalf("saved for %d as %q, returned %q", saver.user, p.ID, id)
	}
	if want := "https://maply.example/card/img/" + p.ID; p.PhotoURL != want+".jpg" || p.ThumbURL != want+".t.jpg" {
		t.Fatalf("urls = %q, %q", p.PhotoURL, p.ThumbURL)
	}
	for _, want := range []string{"<b>maply #12</b>\n<blockquote>Лиссабон, Португалия", "</blockquote>\nТвой итог: 100 км · 3\u00a0400"} {
		if !strings.Contains(p.Caption, want) {
			t.Errorf("caption lacks %q: %q", want, p.Caption)
		}
	}
	if len(p.Markup) != 1 || p.Markup[0][0].URL != "https://t.me/maply_bot/play" || p.Markup[0][0].Text != "Играть" {
		t.Fatalf("markup = %+v", p.Markup)
	}

	// What Telegram will fetch.
	h := s.Handler()
	full := get(h, "/card/img/"+p.ID+".jpg")
	if full.Code != http.StatusOK || full.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("image: %d %v", full.Code, full.Header())
	}
	cfg, err := jpeg.DecodeConfig(full.Body)
	if err != nil || cfg.Width != Size || cfg.Height != Size {
		t.Fatalf("image is %+v, %v", cfg, err)
	}
	thumb := get(h, "/card/img/"+p.ID+".t.jpg")
	cfg, err = jpeg.DecodeConfig(thumb.Body)
	if thumb.Code != http.StatusOK || err != nil || cfg.Width != thumbSide {
		t.Fatalf("thumbnail: %d %+v %v", thumb.Code, cfg, err)
	}
	if rec := get(h, "/card/img/AAAAAAAAAAAAAAAAAAAAAA.jpg"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown key got %d", rec.Code)
	}
	if rec := get(h, "/card/img/../../etc/passwd"); rec.Code == http.StatusOK {
		t.Errorf("a path got through: %d", rec.Code)
	}

	// The same card shared again soon reuses the render.
	if _, err := s.Prepare(context.Background(), Share{UserID: person, ChatID: group, Date: day}); err != nil || len(shot.urls) != 1 {
		t.Fatalf("second share: %v, %d renders", err, len(shot.urls))
	}
	// An hour of the prepared message's life, plus: a later share renders again.
	s.Now = func() time.Time { return afterReveal.Add(imageReuse + time.Second) }
	if _, err := s.Prepare(context.Background(), Share{UserID: person, ChatID: group, Date: day}); err != nil || len(shot.urls) != 2 {
		t.Fatalf("late share: %v, %d renders", err, len(shot.urls))
	}
}

func TestPrepareOmitsYourLineWhenYouDidNotAnswer(t *testing.T) {
	st := &fakeStore{lang: "en", guesses: []db.Guess{guess(1, "A", 1)}}
	saver := &fakeSaver{}
	s := service(t, st, &fakeShot{image: blankPNG(t)}, afterReveal)
	s.TG = saver
	if _, err := s.Prepare(context.Background(), Share{UserID: person, ChatID: group, Date: day}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saver.got.Caption, "result") || strings.Count(saver.got.Caption, "\n\n") != 0 {
		t.Fatalf("caption = %q", saver.got.Caption)
	}
}

func TestPrepareNeedsARendererAPublicAddressAndTelegram(t *testing.T) {
	for name, tweak := range map[string]func(*Service){
		"no renderer":       func(s *Service) { s.Shot = nil },
		"no public address": func(s *Service) { s.PublicURL = "" },
		"no telegram":       func(s *Service) { s.TG = nil },
	} {
		s := service(t, &fakeStore{}, &fakeShot{image: blankPNG(t)}, afterReveal)
		s.TG = &fakeSaver{}
		tweak(s)
		if s.CanShare() {
			t.Errorf("%s: sharing is on", name)
		}
		if _, err := s.Prepare(context.Background(), Share{UserID: person, ChatID: group, Date: day}); !errors.Is(err, ErrDisabled) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestPrepareStopsWhenTheRendererFails(t *testing.T) {
	boom := errors.New("timeout")
	saver := &fakeSaver{}
	s := service(t, &fakeStore{lang: "en"}, &fakeShot{err: boom}, afterReveal)
	s.TG = saver
	if _, err := s.Prepare(context.Background(), Share{UserID: person, ChatID: group, Date: day}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if saver.got.ID != "" {
		t.Fatal("a message was saved without a picture")
	}
}

func TestSharedImagesExpireAndTheCacheIsBounded(t *testing.T) {
	var m images
	now := afterReveal
	for i := 0; i < imageMax+10; i++ {
		m.put(&shared{key: string(rune('a'+i%26)) + itoa(int64(i)), made: now.Add(time.Duration(i) * time.Second), ref: ref{int64(i), day}}, now.Add(time.Duration(i)*time.Second))
	}
	if len(m.byKey) > imageMax {
		t.Fatalf("%d images kept", len(m.byKey))
	}
	img := &shared{key: "k", made: now, ref: ref{1, day}}
	m.put(img, now)
	if m.get("k", now.Add(imageLife-time.Second)) == nil || m.get("k", now.Add(imageLife)) != nil {
		t.Fatal("an image does not live exactly imageLife")
	}
}

func TestAssetsAreServedAndNothingElse(t *testing.T) {
	s := service(t, &fakeStore{}, &fakeShot{}, afterReveal)
	h := s.Handler()
	if rec := get(h, "/card/assets/card.js"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "data-ready") && !strings.Contains(rec.Body.String(), "dataset.ready") {
		t.Fatalf("card.js: %d", rec.Code)
	}
	if rec := get(h, "/card/assets/card.css"); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("card.css: %d %v", rec.Code, rec.Header())
	}
	for _, name := range []string{"card.html", "..%2Fcard.go", "other"} {
		if rec := get(h, "/card/assets/"+name); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

// The page is only as good as what it loads: every script, style and font it
// names must come from the Mini App's files or the card's own.
func TestEveryResourceThePageNamesIsServed(t *testing.T) {
	st := &fakeStore{lang: "en", guesses: []db.Guess{guess(1, "A", 1)}}
	s := service(t, st, &fakeShot{}, afterReveal)
	cardPages := s.Handler()
	app, err := web.Handler()
	if err != nil {
		t.Fatal(err)
	}
	body := get(cardPages, pageURL(t, s, group, day)).Body.String()
	refs := regexp.MustCompile(`(?:src|href)="(/[^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(refs) < 5 {
		t.Fatalf("the page names %d resources:\n%s", len(refs), body)
	}
	for _, m := range refs {
		h := app
		if strings.HasPrefix(m[1], "/card/") {
			h = cardPages
		}
		if rec := get(h, m[1]); rec.Code != http.StatusOK {
			t.Errorf("%s: %d", m[1], rec.Code)
		}
	}
	// Fonts are named by fonts.css, relative to it.
	css := get(app, "/fonts.css").Body.String()
	fonts := regexp.MustCompile(`url\((vendor/[^)]+)\)`).FindAllStringSubmatch(css, -1)
	if len(fonts) == 0 {
		t.Fatal("fonts.css names no font")
	}
	for _, m := range fonts {
		if rec := get(app, "/"+m[1]); rec.Code != http.StatusOK {
			t.Errorf("/%s: %d", m[1], rec.Code)
		}
	}
}
