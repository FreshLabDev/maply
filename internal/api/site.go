// SPDX-License-Identifier: Apache-2.0
package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// The site is the same game in a browser without Telegram. Nobody signs in:
// the browser keeps a random player id in a cookie, and that player's one
// answer a day goes into the world ranking under the name they gave, or
// "Guest". There are no streaks or personal stats. Because anyone can ask as
// if from any zone, the place is shown only after the latest reveal among the
// zones maply offers, when no chat is still guessing it.

// latestReveal is when d's place is out in every zone maply offers.
func (s *Server) latestReveal(d round.Date) time.Time { return s.Zones.LastReveal(d) }

// firstDate is the newest date that has begun in a zone maply offers.
func (s *Server) firstDate() round.Date {
	var first round.Date
	for i, name := range round.ZoneChoices {
		if d := round.DateIn(s.Now(), s.Zones.For(name, "")); i == 0 || first.Before(d) {
			first = d
		}
	}
	return first
}

// siteZone is the visitor's zone as the browser names it, or the default.
func (s *Server) siteZone(r *http.Request) *time.Location {
	if name := r.URL.Query().Get("tz"); name != "" && len(name) < 64 {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return s.Zones.For("", "")
}

// siteDay is the date a browser plays, its round and when its answers close.
// A browser may claim any zone. It gets no date that has not begun in some
// zone maply offers, so the site never shows a photo before a chat could
// have it, and no deadline after the latest reveal.
func (s *Server) siteDay(r *http.Request) (round.Date, db.Round, time.Time, *time.Location, error) {
	loc := s.siteZone(r)
	d := round.DateIn(s.Now(), loc)
	if first := s.firstDate(); first.Before(d) {
		d = first
	}
	rd, err := s.Store.EnsureRound(r.Context(), d)
	if errors.Is(err, db.ErrNoPlace) {
		return d, rd, time.Time{}, loc, errNoRound
	}
	if err != nil {
		return d, rd, time.Time{}, loc, err
	}
	deadline := d.Reveal(loc)
	if reveal := s.latestReveal(d); deadline.After(reveal) {
		deadline = reveal
	}
	return d, rd, deadline, loc, nil
}

func (s *Server) siteToday(w http.ResponseWriter, r *http.Request) {
	if !s.ipLimit.allowIP(clientIP(r)) {
		s.fail(w, r, errRateLimited)
		return
	}
	ctx := r.Context()
	d, rd, deadline, loc, err := s.siteDay(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	puzzle, err := s.Store.Puzzle(ctx, rd.PlaceID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	resp := todayResponse{
		Date: d.String(), Day: rd.DayNo, Image: "/img/" + puzzle.ImageKey, Kind: puzzle.Kind,
		Heading: puzzle.Heading, Zone: loc.String(), Lang: i18n.Normalize(r.URL.Query().Get("lang")),
		Deadline: deadline.UTC(), Reveal: s.latestReveal(d).UTC(), Bot: s.BotUsername,
	}
	if player := playerID(r); player != "" {
		g, ok, err := s.Store.SiteGuessOf(ctx, d, player)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if ok {
			resp.Answer = &pin{Lat: g.Pin.Lat, Lon: g.Pin.Lon}
		}
		if resp.Name, err = s.Store.SiteName(ctx, player); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// siteResult is the place of a past date and the player's scored answer.
func (s *Server) siteResult(w http.ResponseWriter, r *http.Request) {
	if !s.ipLimit.allowIP(clientIP(r)) {
		s.fail(w, r, errRateLimited)
		return
	}
	q := r.URL.Query()
	d, err := round.ParseDate(q.Get("date"))
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
	lang := i18n.Resolve(q.Get("lang"))
	resp := resultResponse{
		Date: d.String(), Day: rd.DayNo,
		Place: placeView{
			Lat: answer.Point.Lat, Lon: answer.Point.Lon, Country: answer.CountryISO,
			City: localized(answer.City, lang), Fact: localized(answer.Fact, lang),
			Author: answer.Author, License: answer.License, Source: answer.Source,
		},
		Table: []line{},
	}
	if err := s.scoreDay(ctx, d, answer, s.Now()); err != nil {
		s.fail(w, r, err)
		return
	}
	if player := playerID(r); player != "" {
		g, ok, err := s.Store.SiteGuessOf(ctx, d, player)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if ok && g.Km != nil {
			total, bonus := g.Total(), *g.PtsBonus
			resp.Table = append(resp.Table, line{Lat: g.Pin.Lat, Lon: g.Pin.Lon, Km: g.Km, Points: &total, Bonus: &bonus, Me: true})
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
	// An answer kept only in the browser, from before answers were stored,
	// is scored on the spot and not ranked.
	lat, errLat := strconv.ParseFloat(q.Get("lat"), 64)
	lon, errLon := strconv.ParseFloat(q.Get("lon"), 64)
	if pt := (geo.Point{Lat: lat, Lon: lon}); errLat == nil && errLon == nil && pt.Valid() {
		place := scoring.Place{Point: answer.Point, Country: answer.CountryISO, Continent: answer.Continent, City: answer.CityShape}
		sc := scoring.Judge(pt, place, s.Countries)
		km, total, bonus := sc.Km, sc.Total(), sc.Bonus
		resp.Table = append(resp.Table, line{Lat: lat, Lon: lon, Km: &km, Points: &total, Bonus: &bonus, Me: true})
	}
	writeJSON(w, http.StatusOK, resp)
}
