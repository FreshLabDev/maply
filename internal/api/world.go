// SPDX-License-Identifier: Apache-2.0
package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
)

// playerCookie holds a site player's random id for a year.
const (
	playerCookie = "maply_player"
	playerAge    = 365 * 24 * time.Hour
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// playerID is the site player's id from the cookie, empty without a valid one.
func playerID(r *http.Request) string {
	c, err := r.Cookie(playerCookie)
	if err != nil || !uuidPattern.MatchString(c.Value) {
		return ""
	}
	return c.Value
}

func newPlayerID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func setPlayer(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name: playerCookie, Value: id, Path: "/", MaxAge: int(playerAge.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

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
	player := playerID(r)
	if player == "" {
		player = newPlayerID()
	}
	if err := s.Store.SitePlayer(ctx, player, body.Name); err != nil {
		s.fail(w, r, err)
		return
	}
	setPlayer(w, player)
	err = s.Store.InsertSiteGuess(ctx, db.NewSiteGuess{
		Date: d, Player: player, Pin: pt, AnsweredAt: now, Deadline: deadline,
		Late: now.After(s.Zones.FirstReveal(d)),
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
	if player := playerID(r); player != "" {
		if err := s.Store.SitePlayer(r.Context(), player, &body.Name); err != nil {
			s.fail(w, r, err)
			return
		}
		setPlayer(w, player)
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": db.CleanName(body.Name)})
}

// viewer is who asks for the days page: a Telegram player from the Mini App,
// a site player from the cookie, or nobody.
type viewer struct {
	user   int64
	chatID int64
	player string
	lang   string
	// zone is where the viewer is: the chat's, or the browser's own.
	zone *time.Location
}

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
	return viewer{player: playerID(r), lang: i18n.Resolve(r.URL.Query().Get("lang")), zone: s.siteZone(r)}, nil
}
