// SPDX-License-Identifier: Apache-2.0
package api

// The free game's endpoints: alone under /api/free, with friends under
// /api/party. The rules are in internal/free; here is who is asking and how
// often.
//
// A Telegram person is whoever initData names, whatever group the app was
// opened from: the free game has no chat. They are recorded in core with
// TouchPerson, never Touch, which would make the free game a DM with the bot
// and move the day's answer window. A browser signed in with an account that
// has Telegram is that person too, alone and with friends; it writes nothing
// to core, which the sign-in has done. An account with Google alone plays as
// its own site player, account.player, from any browser: it keeps records
// and plays with friends too, and never touches core. A site guest plays
// alone as the browser's player, with no records.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/account"
	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// People records a person in core without a chat.
type People interface {
	TouchPerson(ctx context.Context, user tg.User) error
}

// Inviter saves a party's invitation for shareMessage.
type Inviter interface {
	SavePreparedText(ctx context.Context, userID int64, t telegram.PreparedText) (telegram.PreparedMessage, error)
}

var errFreeOff = apiError{http.StatusNotFound, "not_found"}

func (s *Server) freeRoutes(mux *http.ServeMux) {
	// A place is three requests and a lobby a few more; a quick player
	// makes twenty a minute.
	s.freeLimit = newLimiter(120, time.Minute)
	// A party polls every two seconds: thirty a minute, and a second tab.
	s.pollLimit = newLimiter(100, time.Minute)
	// Making games: each new one reads the player's whole history of places.
	s.makeLimit = newLimiter(20, time.Minute)
	s.inviteLimit = newLimiter(10, time.Minute)
	s.freeSiteAll = newLimiter(600, time.Minute)
	mux.HandleFunc("GET /api/free", s.freeHome)
	mux.HandleFunc("POST /api/free/solo", s.freeNew)
	mux.HandleFunc("GET /api/free/solo/{id}", s.freeGame)
	mux.HandleFunc("POST /api/free/solo/{id}/start", s.freeStart)
	mux.HandleFunc("POST /api/free/solo/{id}/guess", s.freeGuess)
	mux.HandleFunc("GET /api/free/img/{token}", s.freeImage)
	mux.HandleFunc("POST /api/party", s.partyNew)
	mux.HandleFunc("GET /api/party/{code}", s.partyPoll)
	mux.HandleFunc("POST /api/party/{code}/join", s.partyJoin)
	mux.HandleFunc("POST /api/party/{code}/settings", s.partySettings)
	mux.HandleFunc("POST /api/party/{code}/start", s.partyStart)
	mux.HandleFunc("POST /api/party/{code}/guess", s.partyGuess)
	mux.HandleFunc("POST /api/party/{code}/leave", s.partyLeave)
	mux.HandleFunc("POST /api/party/{code}/rematch", s.partyRematch)
	mux.HandleFunc("POST /api/party/{code}/invite", s.partyInvite)
	mux.HandleFunc("GET /api/free/archive", s.archiveList)
	mux.HandleFunc("POST /api/free/archive", s.archiveTake)
	mux.HandleFunc("POST /api/free/solo/{id}/challenge", s.challengeMake)
	mux.HandleFunc("GET /api/free/challenge/{code}", s.challengeView)
	mux.HandleFunc("POST /api/free/challenge/{code}/play", s.challengeTake)
	mux.HandleFunc("POST /api/free/challenge/{code}/invite", s.challengeInvite)
}

// freeViewer is who is asking, as the free game knows them.
type freeViewer struct {
	who free.Who
	// user is the Telegram person; for an account's site player only its
	// first name is set, the name its games show.
	user auth.User
	lang string
	// site is a browser rather than the Mini App: its invitations are the
	// site's own links.
	site bool
}

// keeps is whether the viewer keeps records and may play with friends. An
// account that has not played yet has no site player, and keeps all the
// same: the player is made on its first game.
func (v freeViewer) keeps() bool { return v.who.UserID != 0 || v.who.Account }

// namer reads a Telegram person's first name from core.
type namer interface {
	NameOf(ctx context.Context, userID int64) (string, error)
}

// viewer reads the asker: initData when the request has it, else the site's
// account, else the browser's cookie player. A write makes the cookie player
// on its first request, and takes only JSON from maply's own pages; a read
// without a cookie is nobody.
func (s *Server) freeViewer(w http.ResponseWriter, r *http.Request, lim *limiter, write bool) (freeViewer, error) {
	if s.Free == nil {
		return freeViewer{}, errFreeOff
	}
	if raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "tma "); ok {
		data, err := s.InitData.Verify(raw)
		if err != nil {
			return freeViewer{}, errUnauthorized
		}
		if !lim.allow(data.User.ID) {
			return freeViewer{}, errRateLimited
		}
		lang, err := s.personLang(r.Context(), data.User)
		if err != nil {
			return freeViewer{}, err
		}
		return freeViewer{who: free.Who{UserID: data.User.ID}, user: data.User, lang: i18n.Resolve(lang)}, nil
	}
	if write && !ownJSON(r) {
		return freeViewer{}, apiError{http.StatusUnsupportedMediaType, "json_only"}
	}
	sv := s.siteViewer(r)
	if sv.TelegramUserID != 0 {
		return s.freeAccount(r, lim, sv)
	}
	if sv.SignedIn() {
		return s.freeSitePlayer(r, lim, sv, write)
	}
	if !s.ipLimit.allowIP(clientIP(r)) || !s.freeSiteAll.allowKey("all") {
		return freeViewer{}, errRateLimited
	}
	v := freeViewer{who: free.Who{Player: sv.Player}, lang: i18n.Resolve(r.URL.Query().Get("lang")), site: true}
	if v.who.Player == "" && write {
		v.who.Player = newPlayerID()
		setPlayer(w, r, v.who.Player)
	}
	return v, nil
}

// freeSitePlayer is a browser signed in with an account without Telegram:
// the account's own site player, never the browser's guest, made on its
// first write, so its games and records follow it from browser to browser.
// The session is a cookie, so a page of another site may not ask for it, as
// in freeAccount. Nothing here writes to core.
func (s *Server) freeSitePlayer(r *http.Request, lim *limiter, sv account.Viewer, write bool) (freeViewer, error) {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return freeViewer{}, errBadRequest
	}
	if !lim.allowKey("account:" + sv.AccountID) {
		return freeViewer{}, errRateLimited
	}
	ctx := r.Context()
	lang := i18n.Resolve(r.URL.Query().Get("lang"))
	player, name, err := s.Store.FreeAccountPlayer(ctx, sv.AccountID)
	if err != nil {
		return freeViewer{}, err
	}
	if player == "" && write {
		if player, err = s.Store.AccountSitePlayer(ctx, sv.AccountID, newPlayerID(), nil); err != nil {
			return freeViewer{}, err
		}
	}
	if name == "" {
		name = i18n.T(lang, "app.guest")
	}
	return freeViewer{who: free.Who{Player: player, Account: true}, user: auth.User{FirstName: name}, lang: lang, site: true}, nil
}

// freeAccount is a browser signed in with an account that has Telegram: the
// person, as in the Mini App. The session is a cookie, so a page of another
// site, even another ws04.cc one, may not ask for it.
func (s *Server) freeAccount(r *http.Request, lim *limiter, sv account.Viewer) (freeViewer, error) {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return freeViewer{}, errBadRequest
	}
	if !lim.allow(sv.TelegramUserID) {
		return freeViewer{}, errRateLimited
	}
	ctx := r.Context()
	lang, err := s.Store.UserLang(ctx, sv.TelegramUserID)
	if err != nil {
		return freeViewer{}, err
	}
	if lang == "" {
		lang = i18n.Normalize(r.URL.Query().Get("lang"))
	}
	u := auth.User{ID: sv.TelegramUserID}
	if n, ok := s.Store.(namer); ok {
		if u.FirstName, err = n.NameOf(ctx, sv.TelegramUserID); err != nil {
			return freeViewer{}, err
		}
	}
	return freeViewer{who: free.Who{UserID: sv.TelegramUserID}, user: u, lang: i18n.Resolve(lang), site: true}, nil
}

// met records a Telegram person who makes or joins a game.
func (s *Server) met(ctx context.Context, v freeViewer) error {
	if !v.who.Telegram() || v.site || s.People == nil {
		return nil
	}
	u := v.user
	return s.People.TouchPerson(ctx, tg.User{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName,
		Username: u.Username, LanguageCode: u.LanguageCode, IsBot: u.IsBot})
}

func (s *Server) freeFail(w http.ResponseWriter, r *http.Request, err error) {
	var fe *free.Error
	if errors.As(err, &fe) {
		writeJSON(w, fe.Status, map[string]string{"error": fe.Code})
		return
	}
	s.fail(w, r, err)
}

// readBody decodes a small JSON body; an empty one leaves v as it is.
func readBody(w http.ResponseWriter, r *http.Request, v any) error {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		return errBadRequest
	}
	return nil
}

type pinBody struct {
	N    int      `json:"n"`
	Lat  *float64 `json:"lat"`
	Lon  *float64 `json:"lon"`
	Auto bool     `json:"auto"`
	Skip bool     `json:"skip"`
}

func (b pinBody) in() (free.GuessIn, error) {
	in := free.GuessIn{N: b.N, Auto: b.Auto, Skip: b.Skip}
	if (b.Lat == nil) != (b.Lon == nil) {
		return in, errBadRequest
	}
	if b.Lat != nil {
		p := geo.Point{Lat: *b.Lat, Lon: *b.Lon}
		if !p.Valid() {
			return in, errBadRequest
		}
		in.Pin = &p
	}
	return in, nil
}

func gameID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) freeHome(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, false)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	h, err := s.Free.SoloHome(r.Context(), v.who)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	// The free screen may be the first the app opens, so it names the
	// language the person speaks and the bot to share, as /api/today does,
	// and whether the viewer keeps records and may play with friends: a
	// guest is asked to sign in for them.
	writeJSON(w, http.StatusOK, struct {
		free.Home
		Lang  string `json:"lang"`
		Bot   string `json:"bot,omitempty"`
		Keeps bool   `json:"keeps"`
	}{h, v.lang, s.BotUsername, v.keeps()})
}

func (s *Server) freeNew(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.makeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	var set free.Settings
	if err := readBody(w, r, &set); err != nil {
		s.freeFail(w, r, err)
		return
	}
	if err := s.met(r.Context(), v); err != nil {
		s.freeFail(w, r, err)
		return
	}
	g, err := s.Free.NewSolo(r.Context(), v.who, v.user.FirstName, set)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) freeGame(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, false)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	id, ok := gameID(r)
	if !ok || !v.who.Valid() {
		s.freeFail(w, r, free.ErrNotFound)
		return
	}
	g, err := s.Free.SoloGame(r.Context(), v.who, id, v.lang)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) freeStart(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	id, ok := gameID(r)
	var b struct {
		N int `json:"n"`
	}
	if err := readBody(w, r, &b); err != nil || !ok {
		s.freeFail(w, r, errBadRequest)
		return
	}
	rv, err := s.Free.StartSolo(r.Context(), v.who, id, b.N)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rv)
}

func (s *Server) freeGuess(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	id, ok := gameID(r)
	var b pinBody
	if err := readBody(w, r, &b); err != nil || !ok {
		s.freeFail(w, r, errBadRequest)
		return
	}
	in, err := b.in()
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	g, err := s.Free.GuessSolo(r.Context(), v.who, id, in, v.lang)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// freeImage serves a round's picture under the round's own address, once
// the round has begun. The address is signed and names no image key.
func (s *Server) freeImage(w http.ResponseWriter, r *http.Request) {
	if s.Free == nil || s.ImagesDir == "" {
		http.NotFound(w, r)
		return
	}
	if !s.imgLimit.allowIP(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	key, err := s.Free.Image(r.Context(), strings.TrimSuffix(r.PathValue("token"), ".jpg"))
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	if !imageKey.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.ImagesDir, key))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	// The address is the round's and the round never changes its place.
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", time.Time{}, f)
}

func (s *Server) partyNew(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.makeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	var set free.Settings
	if err := readBody(w, r, &set); err != nil {
		s.freeFail(w, r, err)
		return
	}
	if err := s.met(r.Context(), v); err != nil {
		s.freeFail(w, r, err)
		return
	}
	p, err := s.Free.NewParty(r.Context(), v.who, v.user.FirstName, v.lang, set)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	s.writeTogether(w, r, p)
}

// partyPoll is the poll: ?v= is the version the app has, and a party at
// that version answers with the version and the server's clock only.
func (s *Server) partyPoll(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.pollLimit, false)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("v"), 10, 64)
	p, err := s.Free.Poll(r.Context(), v.who, r.PathValue("code"), since, v.lang)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	s.writeTogether(w, r, p)
}

func (s *Server) partyJoin(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	if !v.keeps() {
		s.freeFail(w, r, free.ErrSignIn)
		return
	}
	if err := s.met(r.Context(), v); err != nil {
		s.freeFail(w, r, err)
		return
	}
	p, err := s.Free.Join(r.Context(), v.who, v.user.FirstName, r.PathValue("code"), v.lang)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	s.writeTogether(w, r, p)
}

func (s *Server) partySettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	var set free.Settings
	if err := readBody(w, r, &set); err != nil {
		s.freeFail(w, r, err)
		return
	}
	p, err := s.Free.SetParty(r.Context(), v.who, r.PathValue("code"), v.lang, set)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	s.writeTogether(w, r, p)
}

func (s *Server) partyStart(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	var b struct {
		Version int `json:"version"`
	}
	if err := readBody(w, r, &b); err != nil {
		s.freeFail(w, r, err)
		return
	}
	p, err := s.Free.StartParty(r.Context(), v.who, r.PathValue("code"), v.lang, b.Version)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	s.writeTogether(w, r, p)
}

func (s *Server) partyGuess(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	var b pinBody
	if err := readBody(w, r, &b); err != nil {
		s.freeFail(w, r, err)
		return
	}
	in, err := b.in()
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	p, err := s.Free.GuessParty(r.Context(), v.who, r.PathValue("code"), v.lang, in)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	s.writeTogether(w, r, p)
}

func (s *Server) partyLeave(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	if err := s.Free.LeaveParty(r.Context(), v.who, r.PathValue("code")); err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) partyRematch(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.makeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	code, err := s.Free.Rematch(r.Context(), v.who, v.user.FirstName, r.PathValue("code"))
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

// partyInvite prepares the invitation the player sends themselves: the
// settings in its caption and a button that opens the lobby. Without
// Invites the app shares the link through t.me/share. The site shares its
// own address, /party/<code>, where whoever opens it joins as who they are
// there: a Telegram account as the person, a Google one as its player, and
// a guest is asked to sign in.
func (s *Server) partyInvite(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.inviteLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	code := r.PathValue("code")
	set, status, err := s.Free.PartySettings(r.Context(), v.who, code)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	if status != free.StatusLobby && status != free.StatusPlaying {
		s.freeFail(w, r, free.ErrOver)
		return
	}
	// The site has no shareMessage: it shares the link.
	if v.site {
		writeJSON(w, http.StatusOK, map[string]string{"link": s.siteLink("/party/" + code)})
		return
	}
	link := "https://t.me/" + s.BotUsername + "/play?startapp=" + free.InvitePrefix + code
	if s.Invites == nil {
		writeJSON(w, http.StatusOK, map[string]string{"link": link})
		return
	}
	caption := InviteCaption(v.lang, set)
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	key := base64.RawURLEncoding.EncodeToString(raw[:])
	msg, err := s.Invites.SavePreparedText(r.Context(), v.user.ID, telegram.PreparedText{
		ID: key, Title: i18n.T(v.lang, "free.invite.title"), Description: caption,
		Text: "<b>maply · " + caption + "</b>\n" + i18n.T(v.lang, "free.invite.text",
			"name", htmlEscape(firstName(v.user))),
		Markup: telegram.Markup{{{Text: i18n.T(v.lang, "free.invite.join"), URL: link}}},
	})
	if err != nil {
		s.Log.Warn("party invitation not prepared", "error", err)
		writeJSON(w, http.StatusOK, map[string]string{"link": link})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": msg.ID, "link": link})
}

// siteLink is an address of the site, absolute when the service knows its
// own; relative otherwise, for the page to complete.
func (s *Server) siteLink(path string) string {
	return strings.TrimRight(s.AppURL, "/") + path
}

// InviteCaption is a party's settings in a line: "Blitz · 5 places · 30 s".
func InviteCaption(lang string, set free.Settings) string {
	parts := []string{}
	if set.Seconds == 30 {
		parts = append(parts, i18n.T(lang, "free.blitz"))
	}
	parts = append(parts, i18n.T(lang, "free.rounds."+strconv.Itoa(set.Rounds)),
		i18n.T(lang, "free.seconds", "n", strconv.Itoa(set.Seconds)))
	return strings.Join(parts, " · ")
}

func firstName(u auth.User) string {
	if u.FirstName != "" {
		return u.FirstName
	}
	return u.Username
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func htmlEscape(s string) string { return htmlEscaper.Replace(s) }
