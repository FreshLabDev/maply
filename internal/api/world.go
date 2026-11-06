// SPDX-License-Identifier: Apache-2.0
package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/FreshLabDev/maply/internal/account"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
)

// playerCookie holds a site player's random id for a year.
const playerCookie = account.GuestCookie

// playerID is the site player's id from the cookie, empty without a valid one.
func playerID(r *http.Request) string { return account.GuestID(r) }

func newPlayerID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func setPlayer(w http.ResponseWriter, r *http.Request, id string) { account.SetGuest(w, r, id) }

type siteGuessBody struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	// Name is left out to keep the player's name; "" clears it.
	Name *string `json:"name"`
}

// siteGuess takes a site player's one answer of the day, making the player
// on the first answer.
func (s *Server) siteGuess(w http.ResponseWriter, r *http.Request) {
	if !s.ipLimit.allowIP(clientIP(r)) || !s.siteAll.allowKey("all") {
		s.fail(w, r, errRateLimited)
		return
	}
	// The player is a cookie: another site's form must not answer for it.
	if !ownJSON(r) {
		s.fail(w, r, errBadRequest)
		return
	}
	var body siteGuessBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	err := dec.Decode(&body)
	pt := geo.Point{Lat: body.Lat, Lon: body.Lon}
	if err != nil || !pt.Valid() {
		s.fail(w, r, errBadRequest)
		return
	}
	ctx := r.Context()
	d, _, deadline, _, err := s.siteDay(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := s.Now()
	if now.After(deadline) {
		s.fail(w, r, errClosed)
		return
	}
	v := s.siteViewer(r)
	// An account with Telegram answers as its Telegram player, once for the
	// bot and the site together (/api/guess).
	if v.TelegramUserID != 0 {
		s.fail(w, r, errTelegram)
		return
	}
	player := v.Player
	if v.SignedIn() {
		// An account may have taken in players that answered today already.
		if _, ok, err := s.Store.AccountSiteGuessOf(ctx, d, v.AccountID); err != nil {
			s.fail(w, r, err)
			return
		} else if ok {
			s.fail(w, r, errAnswered)
			return
		}
		if player, err = s.Store.AccountSitePlayer(ctx, v.AccountID, newPlayerID(), body.Name); err != nil {
			s.fail(w, r, err)
			return
		}
	} else {
		if player == "" {
			player = newPlayerID()
		}
		if err := s.Store.SitePlayer(ctx, player, body.Name); err != nil {
			s.fail(w, r, err)
			return
		}
		setPlayer(w, r, player)
	}
	g := db.NewSiteGuess{
		Date: d, Player: player, Pin: pt, AnsweredAt: now, Deadline: deadline,
		Late: now.After(s.Zones.FirstReveal(d)),
	}
	if v.SignedIn() {
		// Under the account's row: Telegram linked a moment ago wins.
		err = s.Store.InsertAccountSiteGuess(ctx, v.AccountID, g)
	} else {
		err = s.Store.InsertSiteGuess(ctx, g)
	}
	if errors.Is(err, db.ErrTelegramAccount) {
		s.fail(w, r, errTelegram)
		return
	}
	// A second answer, or a guest's for a player that a sign-in in another
	// tab took in a moment ago: the page asks for the day again.
	if errors.Is(err, db.ErrAlreadyAnswered) || errors.Is(err, db.ErrPlayerTaken) {
		s.fail(w, r, errAnswered)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// siteName sets or clears the site player's name for the ranking. A browser
// that has not answered yet is no player: its name comes with its answer,
// and nothing is stored before that.
func (s *Server) siteName(w http.ResponseWriter, r *http.Request) {
	if !s.ipLimit.allowIP(clientIP(r)) || !s.siteAll.allowKey("all") {
		s.fail(w, r, errRateLimited)
		return
	}
	if !ownJSON(r) {
		s.fail(w, r, errBadRequest)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		s.fail(w, r, errBadRequest)
		return
	}
	if v := s.siteViewer(r); v.SignedIn() {
		if err := s.Store.SetAccountName(r.Context(), v.AccountID, body.Name); err != nil {
			s.fail(w, r, err)
			return
		}
	} else if v.Player != "" {
		if err := s.Store.SitePlayer(r.Context(), v.Player, &body.Name); err != nil {
			s.fail(w, r, err)
			return
		}
		setPlayer(w, r, v.Player)
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": db.CleanName(body.Name)})
}

// viewer is who asks for the days page: a Telegram player from the Mini App,
// an account, a site player from the cookie, or nobody.
type viewer struct {
	user   int64
	chatID int64
	player string
	lang   string
	// account is the site's signed-in account, and players every site
	// player it has taken in.
	account string
	players map[string]bool
	// zone is where the viewer is: the chat's, or the browser's own.
	zone *time.Location
}

// guest is a viewer who has not signed in: neither the Mini App nor a site
// account.
func (v viewer) guest() bool { return v.user == 0 && v.account == "" }

func (s *Server) viewer(r *http.Request) (viewer, error) {
	if r.Header.Get("Authorization") != "" {
		p, err := s.resolve(r)
		if err != nil {
			return viewer{}, err
		}
		return viewer{user: p.user.ID, chatID: p.chatID, lang: p.lang, zone: p.zone}, nil
	}
	if !s.ipLimit.allowIP(clientIP(r)) {
		return viewer{}, errRateLimited
	}
	sv := s.siteViewer(r)
	out := viewer{player: sv.Player, lang: i18n.Resolve(r.URL.Query().Get("lang")), zone: s.siteZone(r)}
	if sv.SignedIn() {
		players, err := s.sitePlayers(r, sv)
		if err != nil {
			return viewer{}, err
		}
		out.user, out.account, out.players = sv.TelegramUserID, sv.AccountID, players
	}
	return out, nil
}

// owns says a row of the world's ranking is the viewer's own.
func (v viewer) owns(userID int64, player string) bool {
	return (v.user != 0 && userID == v.user) || (player != "" && (player == v.player || v.players[player]))
}

// more is the account's site players besides the cookie's.
func (v viewer) more() []string {
	out := make([]string, 0, len(v.players))
	for p := range v.players {
		if p != v.player {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
