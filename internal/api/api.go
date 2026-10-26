// SPDX-License-Identifier: Apache-2.0

// Package api serves the Mini App: today's picture, the one answer a day, the
// result after the reveal, and the images.
//
// Before a context's reveal nothing here may say anything about where the
// place is: no coordinates, no names, no distance, no score. An answer is
// "accepted" and that is all.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/card"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

type Store interface {
	Touch(context.Context, tg.User, *tg.Chat) error
	EnsureRound(context.Context, round.Date) (db.Round, error)
	RoundOn(context.Context, round.Date) (db.Round, bool, error)
	Puzzle(context.Context, int64) (db.Puzzle, error)
	Answer(context.Context, int64) (db.Answer, error)
	Contexts(context.Context, int64) ([]db.Context, error)
	InsertGuess(context.Context, db.NewGuess) error
	GuessOf(context.Context, round.Date, int64) (db.Guess, bool, error)
	ChatGuesses(context.Context, round.Date, int64) ([]db.Guess, error)
	ScoreDue(context.Context, round.Date, time.Time, func(geo.Point) scoring.Score) (int, error)
	ChatLang(context.Context, int64) (string, error)
	UserLang(context.Context, int64) (string, error)
	ChatZone(context.Context, int64) (string, error)
	World(context.Context, round.Date) (db.World, error)
	Days(context.Context, round.Date, int) ([]db.Day, error)
	ChatDays(context.Context, int64, []db.Day) (map[round.Date]db.ChatDay, error)
	MyDays(context.Context, int64, []db.Day) (map[round.Date]db.Mine, error)
	SiteDays(context.Context, string, []db.Day) (map[round.Date]db.Mine, error)
	SitePlayer(ctx context.Context, id string, name *string) error
	SiteName(ctx context.Context, id string) (string, error)
	InsertSiteGuess(context.Context, db.NewSiteGuess) error
	SiteGuessOf(context.Context, round.Date, string) (db.SiteGuess, bool, error)
}

type MemberChecker interface {
	IsMember(ctx context.Context, d round.Date, chatID, userID int64) (bool, error)
}

// Sharer prepares the result card for shareMessage.
type Sharer interface {
	CanShare() bool
	Prepare(ctx context.Context, sh card.Share) (string, error)
}

// Avatars gives the kept profile photos of players as square JPEGs, without
// asking Telegram, leaving out people who have none.
type Avatars interface {
	Kept(ctx context.Context, ids []int64) map[int64][]byte
}

type Server struct {
	Store     Store
	InitData  *auth.Verifier
	Payloads  *auth.Signer
	Members   MemberChecker
	Zones     round.Zones
	Countries *geo.Countries
	// Shares draws the card and saves it with Telegram; nil, or one that
	// cannot share, leaves the Mini App on t.me/share.
	Shares    Sharer
	ImagesDir string
	// BotUsername is where the site sends a visitor to play in Telegram.
	BotUsername string
	// Avatars gives the players' kept photos for the result's marks; nil
	// keeps the letters.
	Avatars Avatars
	Log     *slog.Logger
	Now     func() time.Time

	ipLimit    *limiter
	userLimit  *limiter
	shareLimit *limiter
}

func (s *Server) Handler() http.Handler {
	if s.Now == nil {
		s.Now = time.Now
	}
	s.ipLimit = newLimiter(60, time.Minute)
	s.userLimit = newLimiter(30, time.Minute)
	// A share draws a picture, which is the most expensive thing here.
	s.shareLimit = newLimiter(5, time.Minute)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/today", s.today)
	mux.HandleFunc("POST /api/guess", s.guess)
	mux.HandleFunc("GET /api/result", s.result)
	mux.HandleFunc("POST /api/share", s.share)
	mux.HandleFunc("GET /api/site/today", s.siteToday)
	mux.HandleFunc("GET /api/site/result", s.siteResult)
	mux.HandleFunc("POST /api/site/guess", s.siteGuess)
	mux.HandleFunc("POST /api/site/name", s.siteName)
	mux.HandleFunc("GET /api/days", s.days)
	mux.HandleFunc("GET /api/day", s.day)
	mux.HandleFunc("GET /img/{key}", s.image)
	return mux
}

// play is who is asking, from where, and for which date.
type play struct {
	user   auth.User
	kind   string // dm or group
	chatID int64
	date   round.Date
	// zone is the context's: the group's for a group, the person's for the DM.
	zone *time.Location
	// lang is what the person reads the app in, the same in every context.
	lang string
}

type apiError struct {
	status int
	code   string
}

func (e apiError) Error() string { return e.code }

var (
	errUnauthorized = apiError{http.StatusUnauthorized, "unauthorized"}
	errNotMember    = apiError{http.StatusForbidden, "not_a_member"}
	errNotStarted   = apiError{http.StatusNotFound, "not_started"}
	errNoRound      = apiError{http.StatusNotFound, "no_round"}
	errRateLimited  = apiError{http.StatusTooManyRequests, "rate_limited"}
	errBadRequest   = apiError{http.StatusBadRequest, "bad_request"}
	errClosed       = apiError{http.StatusConflict, "closed"}
	errAnswered     = apiError{http.StatusConflict, "already_answered"}
	errNotRevealed  = apiError{http.StatusForbidden, "not_revealed"}
	errUnavailable  = apiError{http.StatusServiceUnavailable, "unavailable"}
)

// resolve verifies initData from "Authorization: tma <initData>" and works
// out the context. A start_param means the Mini App was opened from a group
// post; without one it is the DM.
func (s *Server) resolve(r *http.Request) (play, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "tma ")
	if !ok {
		return play{}, errUnauthorized
	}
	data, err := s.InitData.Verify(raw)
	if err != nil {
		return play{}, errUnauthorized
	}
	if !s.userLimit.allow(data.User.ID) {
		return play{}, errRateLimited
	}
	ctx := r.Context()
	p := play{user: data.User}
	user := tg.User{ID: data.User.ID, FirstName: data.User.FirstName, LastName: data.User.LastName,
		Username: data.User.Username, LanguageCode: data.User.LanguageCode}

	if data.StartParam == "" {
		if err := s.Store.Touch(ctx, user, nil); err != nil {
			return play{}, err
		}
		lang, err := s.personLang(ctx, data.User)
		if err != nil {
			return play{}, err
		}
		zone, err := s.Store.ChatZone(ctx, user.ID)
		if err != nil {
			return play{}, err
		}
		p.kind, p.lang = "dm", i18n.Resolve(lang)
		p.zone = s.Zones.For(zone, lang)
		p.date = round.DateIn(s.Now(), p.zone)
		return p, nil
	}

	payload, err := s.Payloads.Verify(data.StartParam)
	if err != nil {
		return play{}, errUnauthorized
	}
	member, err := s.Members.IsMember(ctx, payload.Date, payload.ChatID, user.ID)
	if err != nil {
		return play{}, err
	}
	if !member {
		return play{}, errNotMember
	}
	chatType := data.ChatType
	if chatType != "group" && chatType != "supergroup" {
		chatType = "supergroup"
	}
	if err := s.Store.Touch(ctx, user, &tg.Chat{ID: payload.ChatID, Type: chatType}); err != nil {
		return play{}, err
	}
	zone, err := s.Store.ChatZone(ctx, payload.ChatID)
	if err != nil {
		return play{}, err
	}
	chatLang, err := s.Store.ChatLang(ctx, payload.ChatID)
	if err != nil {
		return play{}, err
	}
	lang, err := s.personLang(ctx, data.User)
	if err != nil {
		return play{}, err
	}
	p.kind, p.chatID, p.date, p.lang = "group", payload.ChatID, payload.Date, i18n.Resolve(lang)
	p.zone = s.Zones.For(zone, chatLang)
	// A post for a date that has not begun in the chat's zone was not sent by
	// the bot.
	if round.DateIn(s.Now(), p.zone).Before(p.date) {
		return play{}, errNotStarted
	}
	return p, nil
}

// personLang is the person's language in core, else their Telegram client's.
func (s *Server) personLang(ctx context.Context, u auth.User) (string, error) {
	lang, err := s.Store.UserLang(ctx, u.ID)
	if err != nil {
		return "", err
	}
	if lang == "" {
		lang = i18n.Normalize(u.LanguageCode)
	}
	return lang, nil
}

// deadline is the earliest reveal among every context the person plays in,
// this one included even if core has not recorded it.
func (s *Server) deadline(ctx context.Context, p play) (time.Time, error) {
	contexts, err := s.Store.Contexts(ctx, p.user.ID)
	if err != nil {
		return time.Time{}, err
	}
	locs := []*time.Location{p.zone}
	for _, c := range contexts {
		locs = append(locs, s.Zones.For(c.Zone, c.Lang))
	}
	return round.Deadline(p.date, locs)
}

// opensAt is when this context may see the answer. A group sees it at its own
// reveal. A DM waits for the latest reveal among the person's chats, so the
// DM never tells someone what one of their groups is still guessing.
func (s *Server) opensAt(ctx context.Context, p play) (time.Time, error) {
	reveal := p.date.Reveal(p.zone)
	if p.kind == "group" {
		return reveal, nil
	}
	contexts, err := s.Store.Contexts(ctx, p.user.ID)
	if err != nil {
		return time.Time{}, err
	}
	for _, c := range contexts {
		if r := p.date.Reveal(s.Zones.For(c.Zone, c.Lang)); r.After(reveal) {
			reveal = r
		}
	}
	return reveal, nil
}

type pin struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type todayResponse struct {
	Date    string   `json:"date"`
	Day     int      `json:"day"`
	Image   string   `json:"image"`
	Kind    string   `json:"kind"`
	Heading *float64 `json:"heading"`
	// Zone is this context's zone, so the app can say "at 20:00, Kyiv"; it is
	// about the player, not the place.
	Zone string `json:"zone"`
	// Lang is the language the app speaks, from core: the one the person
	// picked in any bot of the family.
	Lang     string    `json:"lang"`
	Deadline time.Time `json:"deadline"`
	Reveal   time.Time `json:"reveal"`
	Answer   *pin      `json:"answer"`
	// Bot is set on the site only: the Telegram bot to play in.
	Bot string `json:"bot,omitempty"`
	// Name is the site player's own name for the ranking, empty for "Guest".
	Name string `json:"name,omitempty"`
}

func (s *Server) today(w http.ResponseWriter, r *http.Request) {
	p, err := s.resolve(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	rd, err := s.Store.EnsureRound(ctx, p.date)
	if errors.Is(err, db.ErrNoPlace) {
		s.fail(w, r, errNoRound)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	puzzle, err := s.Store.Puzzle(ctx, rd.PlaceID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	deadline, err := s.deadline(ctx, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	opens, err := s.opensAt(ctx, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	resp := todayResponse{
		Date: p.date.String(), Day: rd.DayNo, Image: "/img/" + puzzle.ImageKey, Kind: puzzle.Kind,
		Heading: puzzle.Heading, Zone: p.zone.String(), Lang: p.lang, Deadline: deadline.UTC(), Reveal: opens.UTC(),
	}
	// Their own pin is theirs to see; it says nothing about the place.
	if g, ok, err := s.Store.GuessOf(ctx, p.date, p.user.ID); err != nil {
		s.fail(w, r, err)
		return
	} else if ok {
		resp.Answer = &pin{Lat: g.Pin.Lat, Lon: g.Pin.Lon}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) guess(w http.ResponseWriter, r *http.Request) {
	if !s.ipLimit.allowIP(clientIP(r)) {
		s.fail(w, r, errRateLimited)
		return
	}
	p, err := s.resolve(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body pin
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || !(geo.Point{Lat: body.Lat, Lon: body.Lon}).Valid() {
		s.fail(w, r, errBadRequest)
		return
	}
	ctx := r.Context()
	if _, ok, err := s.Store.RoundOn(ctx, p.date); err != nil {
		s.fail(w, r, err)
		return
	} else if !ok {
		s.fail(w, r, errNoRound)
		return
	}
	deadline, err := s.deadline(ctx, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := s.Now()
	if now.After(deadline) {
		s.fail(w, r, errClosed)
		return
	}
	err = s.Store.InsertGuess(ctx, db.NewGuess{
		Date: p.date, UserID: p.user.ID, Context: p.kind, ChatID: p.chatID,
		Pin: geo.Point{Lat: body.Lat, Lon: body.Lon}, AnsweredAt: now, Deadline: deadline,
	})
	if errors.Is(err, db.ErrAlreadyAnswered) {
		s.fail(w, r, errAnswered)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

type placeView struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Country string  `json:"country"`
	City    string  `json:"city,omitempty"`
	Fact    string  `json:"fact,omitempty"`
	Author  string  `json:"author,omitempty"`
	License string  `json:"license"`
	Source  string  `json:"source"`
}

type line struct {
	Name   string   `json:"name"`
	Lat    float64  `json:"lat"`
	Lon    float64  `json:"lon"`
	Km     *float64 `json:"km"`
	Points *int     `json:"points"`
	Bonus  *int     `json:"bonus"`
	// Speed is the bonus for answering early; the site's anonymous check has none.
	Speed int  `json:"speed,omitempty"`
	Me    bool `json:"me,omitempty"`
	// Photo is a data: URL of the profile photo, for the mark on the map.
	Photo string `json:"photo,omitempty"`
}

type resultResponse struct {
	Date  string    `json:"date"`
	Day   int       `json:"day"`
	Place placeView `json:"place"`
	Table []line    `json:"table"`
}

func (s *Server) result(w http.ResponseWriter, r *http.Request) {
	p, err := s.resolve(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	opens, err := s.opensAt(ctx, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := s.Now()
	if now.Before(opens) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": errNotRevealed.code, "opens_at": opens.UTC()})
		return
	}
	rd, ok, err := s.Store.RoundOn(ctx, p.date)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.fail(w, r, errNoRound)
		return
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.scoreDay(ctx, p.date, answer, now); err != nil {
		s.fail(w, r, err)
		return
	}
	var guesses []db.Guess
	if p.kind == "group" {
		guesses, err = s.Store.ChatGuesses(ctx, p.date, p.chatID)
	} else {
		var g db.Guess
		if g, ok, err = s.Store.GuessOf(ctx, p.date, p.user.ID); ok {
			guesses = []db.Guess{g}
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	resp := resultResponse{
		Date: p.date.String(), Day: rd.DayNo,
		Place: placeView{
			Lat: answer.Point.Lat, Lon: answer.Point.Lon, Country: answer.CountryISO,
			City: localized(answer.City, p.lang), Fact: localized(answer.Fact, p.lang),
			Author: answer.Author, License: answer.License, Source: answer.Source,
		},
		Table: make([]line, 0, len(guesses)),
	}
	photos := map[int64][]byte{}
	if s.Avatars != nil {
		ids := make([]int64, len(guesses))
		for i, g := range guesses {
			ids[i] = g.UserID
		}
		photos = s.Avatars.Kept(ctx, ids)
	}
	for _, g := range guesses {
		l := line{Name: g.FirstName, Lat: g.Pin.Lat, Lon: g.Pin.Lon, Km: g.Km, Bonus: g.PtsBonus, Speed: g.Speed(), Me: g.UserID == p.user.ID}
		if b := photos[g.UserID]; b != nil {
			l.Photo = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
		}
		if g.PtsKm != nil && g.PtsBonus != nil {
			total := g.Total()
			l.Points = &total
		}
		resp.Table = append(resp.Table, l)
	}
	writeJSON(w, http.StatusOK, resp)
}

// scoreDay scores every answer for d whose deadline has passed.
func (s *Server) scoreDay(ctx context.Context, d round.Date, answer db.Answer, now time.Time) error {
	place := scoring.Place{Point: answer.Point, Country: answer.CountryISO, Continent: answer.Continent, City: answer.CityShape}
	_, err := s.Store.ScoreDue(ctx, d, now, func(pt geo.Point) scoring.Score {
		return scoring.Judge(pt, place, s.Countries)
	})
	return err
}

// shareWait is how long a share may take, the renderer's own limit and
// Telegram's call included. It is longer than the server's usual write
// deadline.
const shareWait = 40 * time.Second

// share prepares the result card as a message the Mini App hands to
// Telegram's shareMessage, and answers with its id. Like /api/result it says
// nothing before the context's reveal. The card is drawn for the chat the
// app was opened from: the group's table, or the person's own line from a DM.
func (s *Server) share(w http.ResponseWriter, r *http.Request) {
	p, err := s.resolve(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.Shares == nil || !s.Shares.CanShare() {
		s.fail(w, r, errUnavailable)
		return
	}
	if !s.shareLimit.allow(p.user.ID) {
		s.fail(w, r, errRateLimited)
		return
	}
	// The card can take longer than the server's usual write deadline.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(shareWait))
	ctx := r.Context()
	opens, err := s.opensAt(ctx, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := s.Now()
	if now.Before(opens) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": errNotRevealed.code, "opens_at": opens.UTC()})
		return
	}
	rd, ok, err := s.Store.RoundOn(ctx, p.date)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.fail(w, r, errNoRound)
		return
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.scoreDay(ctx, p.date, answer, now); err != nil {
		s.fail(w, r, err)
		return
	}
	chat := p.chatID
	if p.kind == "dm" {
		chat = p.user.ID
	}
	id, err := s.Shares.Prepare(ctx, card.Share{UserID: p.user.ID, ChatID: chat, Date: p.date})
	if err != nil {
		// The card is a courtesy: the app falls back to t.me/share.
		s.Log.Warn("share card not prepared", "chat", chat, "date", p.date, "error", err)
		s.fail(w, r, errUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

// localized is a curated text in lang, else in English. A language the
// curators have not written yet gets English rather than a guess at another.
func localized(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	return m[i18n.DefaultLang]
}

// imageKey is the shape of a key images are stored under: random, so a name
// says nothing about the place.
var imageKey = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if s.ImagesDir == "" || !imageKey.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.ImagesDir, key))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The key never changes for a file, so a modification time would only
	// tell a client when the place was prepared.
	http.ServeContent(w, r, "", time.Time{}, f)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae apiError
	if errors.As(err, &ae) {
		writeJSON(w, ae.status, map[string]string{"error": ae.code})
		return
	}
	// The error comes from the store or Telegram; neither includes request
	// bodies, so no pin reaches the log.
	s.Log.Error("api request failed", "path", r.URL.Path, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
