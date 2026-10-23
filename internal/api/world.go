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
	"github.com/FreshLabDev/maply/internal/round"
)

// The days page is the same for the site and the Mini App: every past day
// with its world numbers, and each closed day's full ranking. A day opens
// only after the last zone's reveal, when its place is out everywhere.

// playerCookie holds a site player's random id for a year.
const (
	playerCookie = "maply_player"
	playerAge    = 365 * 24 * time.Hour
	// daysShown is how far back the days page goes.
	daysShown = 30
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
	if !s.ipLimit.allowIP(clientIP(r)) {
		s.fail(w, r, errRateLimited)
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
	err = s.Store.InsertSiteGuess(ctx, db.NewSiteGuess{Date: d, Player: player, Pin: pt, AnsweredAt: now, Deadline: deadline})
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

// siteName sets or clears the site player's name for the ranking.
func (s *Server) siteName(w http.ResponseWriter, r *http.Request) {
	if !s.ipLimit.allowIP(clientIP(r)) {
		s.fail(w, r, errRateLimited)
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
	player := playerID(r)
	if player == "" {
		player = newPlayerID()
	}
	if err := s.Store.SitePlayer(r.Context(), player, &body.Name); err != nil {
		s.fail(w, r, err)
		return
	}
	setPlayer(w, player)
	writeJSON(w, http.StatusOK, map[string]string{"name": db.CleanName(body.Name)})
}

type dayView struct {
	Date       string   `json:"date"`
	Day        int      `json:"day"`
	Final      bool     `json:"final"`
	OpensAt    string   `json:"opens_at,omitempty"`
	Players    int      `json:"players"`
	Pending    int      `json:"pending"`
	MedianKm   *float64 `json:"median_km,omitempty"`
	BestKm     *float64 `json:"best_km,omitempty"`
	CountryPct int      `json:"country_pct"`
	// Place is set once the day is final.
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
	Group   *struct {
		Answered int      `json:"answered"`
		Members  int      `json:"members"`
		BestName string   `json:"best_name,omitempty"`
		BestKm   *float64 `json:"best_km,omitempty"`
	} `json:"group,omitempty"`
	Me *struct {
		Km     float64 `json:"km"`
		Points int     `json:"points"`
		Rank   int     `json:"rank"`
	} `json:"me,omitempty"`
}

// viewer is who asks for the days page: a Telegram player from the Mini App,
// a site player from the cookie, or nobody.
type viewer struct {
	user   int64
	chatID int64
	player string
	lang   string
}

func (s *Server) viewer(r *http.Request) (viewer, error) {
	if r.Header.Get("Authorization") != "" {
		p, err := s.resolve(r)
		if err != nil {
			return viewer{}, err
		}
		return viewer{user: p.user.ID, chatID: p.chatID, lang: p.lang}, nil
	}
	if !s.ipLimit.allowIP(clientIP(r)) {
		return viewer{}, errRateLimited
	}
	return viewer{player: playerID(r), lang: i18n.Resolve(r.URL.Query().Get("lang"))}, nil
}

// days lists the last daysShown dates, newest first. A day still open
// somewhere shows only how many have answered and when it closes.
func (s *Server) days(w http.ResponseWriter, r *http.Request) {
	v, err := s.viewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	now := s.Now()
	list, err := s.Store.Days(ctx, s.firstDate(), daysShown)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var chat map[round.Date]db.ChatDay
	if v.chatID != 0 {
		if chat, err = s.Store.ChatDays(ctx, v.chatID, list); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var mine map[round.Date]db.Mine
	switch {
	case v.user != 0:
		mine, err = s.Store.MyDays(ctx, v.user, list)
	case v.player != "":
		mine, err = s.Store.SiteDays(ctx, v.player, list)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]dayView, 0, len(list))
	for _, d := range list {
		opens := s.latestReveal(d.Date)
		x := dayView{Date: d.Date.String(), Day: d.DayNo, Final: !now.Before(opens), Pending: d.Players + d.Pending}
		if !x.Final {
			// Nothing scored is shown before the day closes: an early zone's
			// numbers would hint at the place to a later one.
			x.OpensAt = opens.UTC().Format(time.RFC3339)
			out = append(out, x)
			continue
		}
		x.Pending = d.Pending
		x.Players, x.MedianKm, x.BestKm, x.CountryPct = d.Players, d.MedianKm, d.BestKm, d.CountryPct
		if c, ok := chat[d.Date]; ok {
			x.Group = &struct {
				Answered int      `json:"answered"`
				Members  int      `json:"members"`
				BestName string   `json:"best_name,omitempty"`
				BestKm   *float64 `json:"best_km,omitempty"`
			}{c.Answered, c.Members, c.BestName, c.BestKm}
		}
		if m, ok := mine[d.Date]; ok {
			x.Me = &struct {
				Km     float64 `json:"km"`
				Points int     `json:"points"`
				Rank   int     `json:"rank"`
			}{m.Km, m.Points, m.Rank}
		}
		if rd, ok, err := s.Store.RoundOn(ctx, d.Date); err == nil && ok {
			if a, err := s.Store.Answer(ctx, rd.PlaceID); err == nil {
				x.City, x.Country = localized(a.City, v.lang), a.CountryISO
			}
		}
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": out})
}

type rankLine struct {
	Rank   int     `json:"rank"`
	Name   string  `json:"name"`
	Site   bool    `json:"site,omitempty"`
	Km     float64 `json:"km"`
	Points int     `json:"points"`
	Me     bool    `json:"me,omitempty"`
}

// day is one closed day: the place and everyone's place in the ranking.
func (s *Server) day(w http.ResponseWriter, r *http.Request) {
	v, err := s.viewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := round.ParseDate(r.URL.Query().Get("date"))
	if err != nil {
		s.fail(w, r, errBadRequest)
		return
	}
	if opens := s.latestReveal(d); s.Now().Before(opens) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": errNotRevealed.code, "opens_at": opens.UTC()})
		return
	}
	ctx := r.Context()
	rd, ok, err := s.Store.RoundOn(ctx, d)
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
	// A day nobody opened since its last reveal may still hold unscored answers.
	if err := s.scoreDay(ctx, d, answer, s.Now()); err != nil {
		s.fail(w, r, err)
		return
	}
	world, err := s.Store.World(ctx, d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ranking := make([]rankLine, 0, len(world.Entries))
	for _, e := range world.Entries {
		me := (v.user != 0 && e.UserID == v.user) || (v.player != "" && e.Player == v.player)
		ranking = append(ranking, rankLine{Rank: e.Rank, Name: e.Name, Site: e.Site(), Km: e.Km, Points: e.Points, Me: me})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date": d.String(), "day": rd.DayNo,
		"place": placeView{
			Lat: answer.Point.Lat, Lon: answer.Point.Lon, Country: answer.CountryISO,
			City: localized(answer.City, v.lang), Fact: localized(answer.Fact, v.lang),
			Author: answer.Author, License: answer.License, Source: answer.Source,
		},
		"median_km": world.MedianKm, "country_pct": world.CountryPct,
		"ranking": ranking, "players": world.Players(),
	})
}
