// SPDX-License-Identifier: Apache-2.0

// Package api serves the Mini App: today's picture, the one answer a day, the
// result, and the images.
//
// Nobody learns anything about the place before they have answered: no
// coordinates, no distance, no score. Once a person has answered, the Mini
// App shows them the place and their score at once, and the others' answers
// without a name, a photo, an id or a time: who is who opens at the
// context's reveal, with the result in the chat. The site keeps the place
// until the day has closed in every zone (see site.go).
package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	ResultCounts(context.Context, round.Date, int64) (int, int, error)
	Deadline(context.Context, round.Zones, int64, round.Date, *time.Location) (time.Time, error)
	InsertGuess(context.Context, db.NewGuess) error
	GuessOf(context.Context, round.Date, int64) (db.Guess, bool, error)
	ChatGuesses(context.Context, round.Date, int64) ([]db.Guess, error)
	ScoreDue(context.Context, round.Date, time.Time, func(geo.Point) scoring.Score) (int, error)
	ChatLang(context.Context, int64) (string, error)
	UserLang(context.Context, int64) (string, error)
	ManualLang(context.Context, int64) (string, error)
	ChatZone(context.Context, int64) (string, error)
	World(context.Context, round.Date) (db.World, error)
	DayPins(context.Context, round.Date) ([][2]float64, error)
	AdoptDMZone(context.Context, int64, string) (bool, error)
	FixZone(context.Context, int64, string) error
	ChatActive(context.Context, int64) (bool, error)
	DaysPage(context.Context, db.DaysQuery) (db.DaysPage, error)
	RoundNo(context.Context, int) (db.Round, bool, error)
	ClosedDay(context.Context, round.Date, round.Date) (db.ClosedDay, error)
	ThumbByKey(context.Context, string) (db.Thumb, bool, error)
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
	// PrepareScore is the share before the reveal: the day and the score,
	// nothing about the place.
	PrepareScore(ctx context.Context, sh card.Share) (string, error)
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
	// AppURL is the service's public address, which a link's preview names
	// its picture by; empty leaves the days' pages without previews.
	AppURL string
	// Avatars gives the players' kept photos for the result's marks; nil
	// keeps the letters.
	Avatars Avatars
	Log     *slog.Logger
	Now     func() time.Time

	ipLimit    *limiter
	userLimit  *limiter
	shareLimit *limiter
	logLimit   *limiter
	logAll     *limiter
	// siteAll caps the site's writes from everyone together: a site player
	// is a cookie, and addresses are cheap.
	siteAll *limiter
	// imgLimit bounds the place's pictures per address, thumbLimit the
	// days' strips, of which a page of the list asks for thirty.
	imgLimit   *limiter
	thumbLimit *limiter
	// ogLimit bounds the days' link pictures, which are drawn on demand.
	ogLimit *limiter

	// tied remembers who has been tied to which chat's reveal, so a result
	// polled again does not write to core every time.
	tiedMu sync.Mutex
	tied   map[string]bool

	// closed keeps what a day that is over everywhere shows, which no longer
	// changes: the days page and a day's ranking do not ask the database
	// again for every visitor.
	closedMu sync.Mutex
	closed   map[round.Date]closedDay

	// live keeps an open day's ranking and pins for the polls that saw the
	// same number of scored answers: a thousand open apps rank the day once
	// per new answer, not once each.
	liveMu sync.Mutex
	live   map[round.Date]liveDay

	// thumbs keeps the strips last served; they never change.
	thumbsMu sync.Mutex
	thumbs   map[string]db.Thumb

	// ogs keeps the days' link pictures drawn; ogTurn lets one be drawn at
	// a time.
	ogMu   sync.Mutex
	ogs    map[string][]byte
	ogTurn chan struct{}
}

func (s *Server) Handler() http.Handler {
	if s.Now == nil {
		s.Now = time.Now
	}
	s.ipLimit = newLimiter(60, time.Minute)
	s.userLimit = newLimiter(30, time.Minute)
	// A share draws a picture, which is the most expensive thing here.
	s.shareLimit = newLimiter(5, time.Minute)
	// A broken release reports itself in a few lines a minute; more is a
	// flood, and the journal is shared with every stack on the host.
	s.logLimit = newLimiter(5, time.Minute)
	s.logAll = newLimiter(30, time.Minute)
	s.siteAll = newLimiter(120, time.Minute)
	// An app open fetches one picture; Cloudflare keeps the .jpg, so a
	// loop of fetches past it is all that reaches the bot.
	s.imgLimit = newLimiter(120, time.Minute)
	s.thumbLimit = newLimiter(600, time.Minute)
	s.ogLimit = newLimiter(30, time.Minute)
	s.ogTurn = make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/today", s.today)
	mux.HandleFunc("POST /api/guess", s.guess)
	mux.HandleFunc("GET /api/result", s.result)
	mux.HandleFunc("POST /api/share", s.share)
	mux.HandleFunc("POST /api/log", s.clientLog)
	mux.HandleFunc("GET /api/site/today", s.siteToday)
	mux.HandleFunc("GET /api/site/result", s.siteResult)
	mux.HandleFunc("POST /api/site/guess", s.siteGuess)
	mux.HandleFunc("POST /api/site/name", s.siteName)
	mux.HandleFunc("GET /api/days", s.days)
	mux.HandleFunc("GET /api/day", s.day)
	mux.HandleFunc("GET /img/{key}", s.image)
	mux.HandleFunc("GET /thumb/{name}", s.thumb)
	mux.HandleFunc("GET /og/day/{name}", s.og)
	return mux
}

// play is who is asking, from where, and for which date.
type play struct {
	user   auth.User
	kind   string // dm or group
	chatID int64
	// chatType is the group's type as the launch named it.
	chatType string
	date     round.Date
	// zone is the context's: the group's for a group, the person's for the DM.
	zone *time.Location
	// lang is what the app speaks: a language the person picked by hand in
	// any bot of the family, else the group's when opened from a group that
	// has one, else the person's.
	lang string
	// shareLang is the language of what the person shares from the app: the
	// group's, the one the bot posts in there, since that is where it lands.
	shareLang string
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
	errNotAnswered  = apiError{http.StatusForbidden, "not_answered"}
	errUnavailable  = apiError{http.StatusServiceUnavailable, "unavailable"}
	// errLeft is a link from a group maply has been removed from: the group
	// no longer plays, and its reveal no longer bounds anyone's answer.
	errLeft = apiError{http.StatusGone, "left"}
)

// resolve verifies initData from "Authorization: tma <initData>" and works
// out the context. A start_param means the Mini App was opened from a group
// post; without one it is the DM.
func (s *Server) resolve(r *http.Request) (play, error) { return s.resolveAs(r, true) }

// touch records the person, and the group the app was opened from, in core.
// A group recorded is a group whose reveal closes the person's answers.
func (s *Server) touch(ctx context.Context, p play) error {
	user := tg.User{ID: p.user.ID, FirstName: p.user.FirstName, LastName: p.user.LastName,
		Username: p.user.Username, LanguageCode: p.user.LanguageCode}
	if p.kind != "group" {
		return s.Store.Touch(ctx, user, nil)
	}
	return s.Store.Touch(ctx, user, &tg.Chat{ID: p.chatID, Type: p.chatType})
}

// tieOnce is touch at most once per person, chat and date in this process.
func (s *Server) tieOnce(ctx context.Context, p play) error {
	key := fmt.Sprintf("%s:%d:%d:%s", p.kind, p.chatID, p.user.ID, p.date)
	s.tiedMu.Lock()
	done := s.tied[key]
	s.tiedMu.Unlock()
	if done {
		return nil
	}
	if err := s.touch(ctx, p); err != nil {
		return err
	}
	s.tiedMu.Lock()
	defer s.tiedMu.Unlock()
	if s.tied == nil || len(s.tied) >= 20000 {
		s.tied = map[string]bool{}
	}
	s.tied[key] = true
	return nil
}

// resolveAs is resolve that records the person and the chat in core only
// when touch is set. The result is polled every few seconds while the names
// are hidden; /api/today, which every opening of the app starts with, has
// recorded them already.
func (s *Server) resolveAs(r *http.Request, touch bool) (play, error) {
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
		if touch {
			if err := s.touch(ctx, p); err != nil {
				return play{}, err
			}
		}
		lang, err := s.personLang(ctx, data.User)
		if err != nil {
			return play{}, err
		}
		zone, err := s.Store.ChatZone(ctx, user.ID)
		if err != nil {
			return play{}, err
		}
		// Fixed at the first contact, the DM's zone no longer follows the
		// language: switching it could reopen a day the DM has seen.
		if zone == "" {
			zone = round.DefaultName(lang)
			if err := s.Store.FixZone(ctx, user.ID, zone); err != nil {
				return play{}, err
			}
		}
		p.kind, p.lang = "dm", i18n.Resolve(lang)
		p.shareLang = p.lang
		p.zone = s.Zones.For(zone, lang)
		p.date = round.DateIn(s.Now(), p.zone)
		return p, nil
	}

	payload, err := s.Payloads.Verify(data.StartParam)
	if err != nil {
		return play{}, errUnauthorized
	}
	if active, err := s.Store.ChatActive(ctx, payload.ChatID); err != nil {
		return play{}, err
	} else if !active {
		return play{}, errLeft
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
	p.kind, p.chatID, p.chatType = "group", payload.ChatID, chatType
	if touch {
		if err := s.touch(ctx, p); err != nil {
			return play{}, err
		}
	}
	zone, err := s.Store.ChatZone(ctx, payload.ChatID)
	if err != nil {
		return play{}, err
	}
	chatLang, err := s.Store.ChatLang(ctx, payload.ChatID)
	if err != nil {
		return play{}, err
	}
	// Opened from a group, what a player shares lands next to the bot's posts
	// there, so it is in the group's language, as the posts and the card are.
	// The screen is the player's own: a language they picked by hand in any
	// bot of the family wins over the group's. The language of their
	// Telegram app does not, so a group still reads as one.
	p.shareLang = i18n.Resolve(chatLang)
	manual, err := s.Store.ManualLang(ctx, user.ID)
	if err != nil {
		return play{}, err
	}
	lang := chatLang
	if c := i18n.Normalize(manual); i18n.IsSupported(c) {
		lang = c
	} else if lang == "" {
		if lang, err = s.personLang(ctx, data.User); err != nil {
			return play{}, err
		}
	}
	p.date, p.lang = payload.Date, i18n.Resolve(lang)
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
// this one included even if core has not recorded it, and chats maply left
// after their reveal.
func (s *Server) deadline(ctx context.Context, p play) (time.Time, error) {
	return s.Store.Deadline(ctx, s.Zones, p.user.ID, p.date, p.zone)
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
	Lang string `json:"lang"`
	// ShareLang is the language of what the app shares, set only when it is
	// not Lang: a group's, for a player whose screen speaks their own.
	ShareLang string    `json:"share_lang,omitempty"`
	Deadline  time.Time `json:"deadline"`
	Reveal    time.Time `json:"reveal"`
	Answer    *pin      `json:"answer"`
	// Bot is set on the site only: the Telegram bot to play in.
	Bot string `json:"bot,omitempty"`
	// Name is the site player's own name for the ranking, empty for "Guest".
	Name string `json:"name,omitempty"`
	// ZoneSet is the zone the DM just took from the phone, so the app can say
	// so once.
	ZoneSet string `json:"zone_set,omitempty"`
	// Now is the server's clock: the app counts down by it, not by a phone
	// that may run minutes off.
	Now time.Time `json:"now"`
}

func (s *Server) today(w http.ResponseWriter, r *http.Request) {
	p, err := s.resolve(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	zoneSet, err := s.adoptZone(ctx, r, &p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
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
		Date: p.date.String(), Day: rd.DayNo, Image: imagePath(puzzle.ImageKey), Kind: puzzle.Kind,
		Heading: puzzle.Heading, Zone: p.zone.String(), Lang: p.lang, Deadline: deadline.UTC(), Reveal: opens.UTC(),
		ZoneSet: zoneSet, Now: s.Now().UTC(),
	}
	if p.shareLang != "" && p.shareLang != p.lang {
		resp.ShareLang = p.shareLang
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

// adoptZone gives a DM that never picked a zone the one of the person's
// phone, sent as ?tz=, when a choice keeps the same clock. Before, the DM
// followed the language: English meant London even in Berlin.
func (s *Server) adoptZone(ctx context.Context, r *http.Request, p *play) (string, error) {
	if p.kind != "dm" {
		return "", nil
	}
	zone := s.Zones.Match(r.URL.Query().Get("tz"), s.Now())
	if zone == "" {
		return "", nil
	}
	// After the DM's reveal the person may have seen the place without
	// answering; a zone further west would give them the evening to answer
	// it. A zone a date further east would send tomorrow's post before
	// today's result. The phone's zone waits for a day where neither is so.
	to := s.Zones.For(zone, "")
	if round.Reopens(p.zone, to, s.Now()) || round.Outruns(p.zone, to, s.Now()) {
		return "", nil
	}
	set, err := s.Store.AdoptDMZone(ctx, p.user.ID, zone)
	if err != nil || !set {
		return "", err
	}
	s.Log.Info("dm zone taken from the phone", "zone", zone)
	p.zone = s.Zones.For(zone, "")
	p.date = round.DateIn(s.Now(), p.zone)
	return zone, nil
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
	// The answer is in: the person sees where it was, and how the others did.
	resp, err := s.buildResult(ctx, p, now)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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

// line is one answer of the chat, for its marker on the map. Before the
// reveal another player's line has no name.
type line struct {
	// Key is the same as the person's row in the world's table, so two answers
	// on one spot stay two markers.
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Lat    float64  `json:"lat"`
	Lon    float64  `json:"lon"`
	Km     *float64 `json:"km"`
	Points *int     `json:"points"`
	Bonus  *int     `json:"bonus"`
	// Speed is the bonus for answering early, inside Points.
	Speed int  `json:"speed,omitempty"`
	Me    bool `json:"me,omitempty"`
	// Photo is a data: URL of the profile photo, for the mark on the map.
	Photo string `json:"photo,omitempty"`
}

// worldNow is where the person stands among everyone who has answered so far,
// and where the others put their pins, without names.
type worldNow struct {
	Rank    int   `json:"rank"`
	Players int   `json:"players"`
	Pins    []pin `json:"pins"`
}

// boardLine is one row of the world's table under the map. Group marks the
// players of the chat the app was opened from. Hidden is a row whose name is
// not out yet, a site player's too: a name typed on the site comes out with
// the rest of the world's, and a guest without one reads "Guest" then.
type boardLine struct {
	// Key tells the rows apart from one poll to the next without saying whose
	// they are: it is keyed by a secret the process makes at start.
	Key    string  `json:"key"`
	Rank   int     `json:"rank"`
	Name   string  `json:"name,omitempty"`
	Km     float64 `json:"km"`
	Points int     `json:"points"`
	Bonus  int     `json:"bonus"`
	Speed  int     `json:"speed,omitempty"`
	Me     bool    `json:"me,omitempty"`
	Group  bool    `json:"group,omitempty"`
	Hidden bool    `json:"hidden,omitempty"`
	Site   bool    `json:"site,omitempty"`
	Photo  string  `json:"photo,omitempty"`
}

// boardShown is how many of the world's best rows go to the app. Your own row
// and your group's come on top of them wherever they stand.
const boardShown = 200

// randomSecret keys the rows when there is no signer, in tests.
var randomSecret = func() []byte {
	b := make([]byte, 32)
	rand.Read(b)
	return b
}()

// boardSecret keys the rows' keys. It comes from the bot's signer, so a row
// keeps its key across a restart and an open app does not see every row as
// new after a deploy.
func (s *Server) boardSecret() []byte {
	if s.Payloads == nil {
		return randomSecret
	}
	return s.Payloads.Key("maply-board-v1")
}

// boardKey names a row for the app, the same all day so a row keeps its
// place while the table grows. viewer is the chat looking: one person's key
// in a group is not their key in another group or a DM, so two chats cannot
// match up who is who among the unnamed rows.
func boardKey(secret []byte, d round.Date, viewer int64, e db.Entry) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(d.String() + "|" + strconv.FormatInt(viewer, 10) + "|" + strconv.FormatInt(e.UserID, 10) + "|" + e.Player))
	return hex.EncodeToString(m.Sum(nil)[:6])
}

// viewer is the chat a result is drawn for: the group, or the person's DM.
func (p play) viewer() int64 {
	if p.kind == "group" {
		return p.chatID
	}
	return p.user.ID
}

// resultVersion changes with a new answer in the chat or the world, at the
// chat's reveal and when the world's names open.
func resultVersion(chatN, worldN int, phase string, worldOpen bool) string {
	return strconv.Itoa(chatN) + "." + strconv.Itoa(worldN) + "." + phase + "." + strconv.FormatBool(worldOpen)
}

type resultResponse struct {
	Date  string    `json:"date"`
	Day   int       `json:"day"`
	Place placeView `json:"place"`
	Table []line    `json:"table"`
	// Phase is "live" until the context's reveal, with the others unnamed,
	// and "final" from then on. The site's result has none.
	Phase string `json:"phase,omitempty"`
	// OpensAt is when the names open, in the live phase.
	OpensAt *time.Time `json:"opens_at,omitempty"`
	World   *worldNow  `json:"world,omitempty"`
	// Board is the world's table: everyone scored so far today, of whom
	// Players in all.
	Board   []boardLine `json:"board"`
	Players int         `json:"players"`
	// Version changes when someone answers, so the app redraws only then.
	Version string `json:"version,omitempty"`
	// Same answers an app that asked with the current version: nothing has
	// changed, and the rest of the result is left out.
	Same bool `json:"same,omitempty"`
}

// worldPinsShown is how many of the world's pins go to the app: enough for a
// picture, few enough for a phone polling every few seconds.
const worldPinsShown = 300

func (s *Server) result(w http.ResponseWriter, r *http.Request) {
	p, err := s.resolveAs(r, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// A DM's day ends at midnight in its zone, while one of the person's
	// groups may still be guessing it: an app opened on that day keeps it.
	// A group's date is in its signed link.
	if p.kind == "dm" {
		if d, err := round.ParseDate(r.URL.Query().Get("date")); err == nil && d.String() == p.date.AddDays(-1).String() {
			p.date = d
		}
	}
	resp, err := s.buildResultSince(r.Context(), p, s.Now(), r.URL.Query().Get("since"))
	var early earlyError
	if errors.As(err, &early) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": early.code, "opens_at": early.opens.UTC()})
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if resp.Same {
		writeJSON(w, http.StatusOK, map[string]any{"same": true, "version": resp.Version, "phase": resp.Phase, "opens_at": resp.OpensAt})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// earlyError is a result asked for before the person may see it.
type earlyError struct {
	code  string
	opens time.Time
}

func (e earlyError) Error() string { return e.code }

// buildResult is the result as this person may see it now. Before the
// context's reveal it is there only for someone who has answered, and the
// others in it have no names.
func (s *Server) buildResult(ctx context.Context, p play, now time.Time) (resultResponse, error) {
	return s.buildResultSince(ctx, p, now, "")
}

// buildResultSince is buildResult for an app that already shows the version
// since: when nothing has changed it gets the version back and nothing else,
// without the pins and the table it has.
func (s *Server) buildResultSince(ctx context.Context, p play, now time.Time, since string) (resultResponse, error) {
	opens, err := s.opensAt(ctx, p)
	if err != nil {
		return resultResponse{}, err
	}
	live := now.Before(opens)
	own, answered, err := s.Store.GuessOf(ctx, p.date, p.user.ID)
	if err != nil {
		return resultResponse{}, err
	}
	if live && !answered {
		return resultResponse{}, earlyError{errNotAnswered.code, opens}
	}
	rd, ok, err := s.Store.RoundOn(ctx, p.date)
	if err != nil {
		return resultResponse{}, err
	}
	if !ok {
		return resultResponse{}, errNoRound
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return resultResponse{}, err
	}
	if err := s.scoreDay(ctx, p.date, answer, now); err != nil {
		return resultResponse{}, err
	}
	phase := "final"
	if live {
		phase = "live"
	}
	worldOpen := !now.Before(s.Zones.LastReveal(p.date))
	var opensAt *time.Time
	if live {
		at := opens.UTC()
		opensAt = &at
	}
	// Seeing the place without an answer ties the person to this chat's
	// reveal, as /api/today does; a launch that skipped it is caught here,
	// whatever version the app says it has.
	if !answered {
		if err := s.tieOnce(ctx, p); err != nil {
			return resultResponse{}, err
		}
	}
	// A poll that has this version already costs two counts, not the
	// day's whole ranking; the world count also keys the ranking kept for
	// the polls that did miss.
	chatN, worldN, err := s.Store.ResultCounts(ctx, p.date, p.chatID)
	if err != nil {
		return resultResponse{}, err
	}
	if p.kind != "group" {
		chatN = 0
		if answered {
			chatN = 1
		}
	}
	if version := resultVersion(chatN, worldN, phase, worldOpen); since != "" && since == version {
		return resultResponse{Phase: phase, OpensAt: opensAt, Version: version, Same: true}, nil
	}
	var guesses []db.Guess
	if p.kind == "group" {
		guesses, err = s.Store.ChatGuesses(ctx, p.date, p.chatID)
	} else if answered {
		// Scored just now: read it again for the score.
		var g db.Guess
		if g, ok, err = s.Store.GuessOf(ctx, p.date, own.UserID); ok {
			guesses = []db.Guess{g}
		}
	}
	if err != nil {
		return resultResponse{}, err
	}
	day, err := s.liveDay(ctx, p.date, worldN)
	if err != nil {
		return resultResponse{}, err
	}
	world := day.world
	version := resultVersion(len(guesses), world.Players(), phase, worldOpen)
	if since != "" && since == version {
		return resultResponse{Phase: phase, OpensAt: opensAt, Version: version, Same: true}, nil
	}
	resp := resultResponse{
		Date: p.date.String(), Day: rd.DayNo,
		Place: placeView{
			Lat: answer.Point.Lat, Lon: answer.Point.Lon, Country: answer.CountryISO,
			City: localized(answer.City, p.lang), Fact: localized(answer.Fact, p.lang),
			Author: answer.Author, License: answer.License, Source: answer.Source,
		},
		Table:   make([]line, 0, len(guesses)),
		Phase:   phase,
		OpensAt: opensAt,
		Version: version,
	}
	photos := map[int64][]byte{}
	if s.Avatars != nil && !live {
		ids := make([]int64, len(guesses))
		for i, g := range guesses {
			ids[i] = g.UserID
		}
		photos = s.Avatars.Kept(ctx, ids)
	}
	secret := s.boardSecret()
	viewer := p.viewer()
	for _, g := range guesses {
		me := g.UserID == p.user.ID
		l := line{Key: boardKey(secret, p.date, viewer, db.Entry{UserID: g.UserID}), Lat: g.Pin.Lat, Lon: g.Pin.Lon, Km: g.Km, Me: me}
		if g.PtsKm != nil && g.PtsBonus != nil {
			total := g.Total()
			l.Points, l.Bonus, l.Speed = &total, g.PtsBonus, g.Speed()
		}
		if me || !live {
			l.Name = g.FirstName
		}
		if b := photos[g.UserID]; b != nil {
			l.Photo = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
		}
		resp.Table = append(resp.Table, l)
	}
	if answered {
		resp.World = worldNowOf(p, world, day.pins)
	}
	group := map[int64]bool{}
	if p.kind == "group" {
		for _, g := range guesses {
			group[g.UserID] = true
		}
	}
	isMe := func(e db.Entry) bool { return e.UserID != 0 && e.UserID == p.user.ID }
	resp.Board = board(secret, p.date, viewer, world, isMe, group, photos, !live, worldOpen)
	resp.Players = world.Players()
	return resp, nil
}

// liveFor is how long an open day's ranking serves the polls that saw the
// same number of scored answers: a name changed shows within it.
const liveFor = 10 * time.Second

// liveDay is an open day's ranking and pins at n scored answers.
type liveDay struct {
	at    time.Time
	n     int
	world db.World
	pins  [][2]float64
}

// liveDay is d's ranking and pins when n answers are scored, read once for
// every poll that saw n. The lock is held while the day is read, so polls
// that miss together wait for one read instead of each making their own.
func (s *Server) liveDay(ctx context.Context, d round.Date, n int) (liveDay, error) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if c, ok := s.live[d]; ok && c.n == n && s.Now().Sub(c.at) < liveFor {
		return c, nil
	}
	world, err := s.Store.World(ctx, d)
	if err != nil {
		return liveDay{}, err
	}
	pins, err := s.Store.DayPins(ctx, d)
	if err != nil {
		return liveDay{}, err
	}
	// Kept under the count it really has: one more answer may have come in
	// since the poll counted.
	c := liveDay{at: s.Now(), n: world.Players(), world: world, pins: pins}
	if s.live == nil || len(s.live) >= 8 {
		s.live = map[round.Date]liveDay{}
	}
	s.live[d] = c
	return c, nil
}

// worldNowOf is the person's place among everyone scored so far today, and
// a sample of everyone's pins.
func worldNowOf(p play, w db.World, pins [][2]float64) *worldNow {
	out := &worldNow{Rank: w.RankOf(p.user.ID), Players: w.Players(), Pins: make([]pin, 0, min(len(pins), worldPinsShown))}
	step := 1
	if len(pins) > worldPinsShown {
		step = (len(pins) + worldPinsShown - 1) / worldPinsShown
	}
	for i := 0; i < len(pins); i += step {
		out.Pins = append(out.Pins, pin{Lat: pins[i][0], Lon: pins[i][1]})
	}
	return out
}

// board is the world's table as this person may see it: the best rows, and
// theirs and their group's wherever they stand. Your own row has its name.
// Your group's names open at the chat's reveal, groupOpen; everyone else's
// once the day has closed in every zone, worldOpen, so a group in Tokyo does
// not learn who is who in a Kyiv group still guessing.
func board(secret []byte, d round.Date, viewer int64, w db.World, isMe func(db.Entry) bool, group map[int64]bool, photos map[int64][]byte, groupOpen, worldOpen bool) []boardLine {
	out := make([]boardLine, 0, min(len(w.Entries), boardShown+len(group)+1))
	for i, e := range w.Entries {
		me := isMe(e)
		inGroup := e.UserID != 0 && group[e.UserID]
		if i >= boardShown && !me && !inGroup {
			continue
		}
		l := boardLine{Key: boardKey(secret, d, viewer, e), Rank: e.Rank, Km: e.Km, Points: e.Points, Bonus: e.Bonus, Speed: e.Speed,
			Me: me, Group: inGroup && !me, Site: e.Site()}
		if me || worldOpen || groupOpen && inGroup {
			l.Name = e.Name
		} else {
			l.Hidden = true
		}
		if b := photos[e.UserID]; b != nil && e.UserID != 0 {
			l.Photo = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
		}
		out = append(out, l)
	}
	return out
}

// scoreDay scores the day's answers that have no score yet: every Telegram
// answer, and the site's whose deadline has passed.
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
// Telegram's shareMessage, and answers with its id. Before the context's
// reveal it is the person's score alone, with nothing about the place. The
// card is drawn for the chat the app was opened from: the group's table, or
// the person's own line from a DM.
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
	chat := p.chatID
	if p.kind == "dm" {
		chat = p.user.ID
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
	// An answer sent as a location in the DM comes in unscored.
	if err := s.scoreDay(ctx, p.date, answer, now); err != nil {
		s.fail(w, r, err)
		return
	}
	if now.Before(opens) {
		// Before the reveal the share is the score alone: whoever gets it may
		// not have answered yet.
		g, answered, err := s.Store.GuessOf(ctx, p.date, p.user.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !answered || g.Km == nil {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": errNotAnswered.code, "opens_at": opens.UTC()})
			return
		}
		id, err := s.Shares.PrepareScore(ctx, card.Share{UserID: p.user.ID, ChatID: chat, Date: p.date})
		if err != nil {
			s.Log.Warn("score share not prepared", "chat", chat, "date", p.date, "error", err)
			s.fail(w, r, errUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
		return
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

// imagePath is where the app fetches a place's picture. The .jpg is for
// Cloudflare, which caches by extension: without it every open reached the
// bot over the home uplink. The path without it still works for apps opened
// before the change.
func imagePath(key string) string { return "/img/" + key + ".jpg" }

func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSuffix(r.PathValue("key"), ".jpg")
	if s.ImagesDir == "" || !imageKey.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	if !s.imgLimit.allowIP(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
