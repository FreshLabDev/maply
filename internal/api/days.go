// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/people"
	"github.com/FreshLabDev/maply/internal/round"
)

// The days page is the same for the site and the Mini App: every day, newest
// first, as a diary. A day over in every zone shows its place, its strip, the
// world's numbers and the viewer's own; a closed day opens its page with the
// whole ranking. A day still open somewhere shows only how many have
// answered and when it closes: an early zone's numbers would hint at the
// place to a later one.

const (
	// daysShown is a page of the list; daysMost the most one may ask for.
	daysShown = 30
	daysMost  = 60
	// rankShown is how many of a day's rows the ranking sends; the viewer's
	// own row comes on top of them when it is further down.
	rankShown = 500
	// dayPinsShown and missesShown bound the day page's map and scale.
	dayPinsShown = 1000
	missesShown  = 1000
	// closedFor is how long a closed day is kept before it is read again: a
	// name changed or an answer removed by hand shows within it.
	closedFor = 10 * time.Minute
	// closedSoon is how long a day without its strip is kept: the service
	// draws it within a minute of the day being kept.
	closedSoon = 30 * time.Second
	// dayMost is the highest day number asked for; past it, no day was.
	dayMost = 999999
)

type groupDay struct {
	Answered int      `json:"answered"`
	Members  int      `json:"members"`
	BestName string   `json:"best_name,omitempty"`
	BestKm   *float64 `json:"best_km,omitempty"`
}

type myDay struct {
	Km     float64 `json:"km"`
	Points int     `json:"points"`
	Rank   int     `json:"rank"`
}

type dayView struct {
	Date  string `json:"date"`
	Day   int    `json:"day"`
	Final bool   `json:"final"`
	// OpensAt is when an open day closes everywhere, and its place comes out.
	OpensAt string `json:"opens_at,omitempty"`
	Players int    `json:"players"`
	// Pending is, on an open day, everyone who has answered so far; on a
	// closed one the site answers not scored yet.
	Pending    int      `json:"pending"`
	MedianKm   *float64 `json:"median_km,omitempty"`
	BestKm     *float64 `json:"best_km,omitempty"`
	CountryPct int      `json:"country_pct"`
	// Place is set once the day is final.
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
	// Thumb is the day's strip, once the day is kept closed and drawn.
	Thumb string `json:"thumb,omitempty"`
	// Image is an open day's puzzle, which anyone playing it sees, on a
	// date begun where the viewer is.
	Image string `json:"image,omitempty"`
	Kind  string `json:"kind,omitempty"`
	// Played says the viewer has answered; on an open day it is all their
	// line says.
	Played bool      `json:"played,omitempty"`
	Group  *groupDay `json:"group,omitempty"`
	Me     *myDay    `json:"me,omitempty"`
}

type daysResponse struct {
	Days []dayView `json:"days"`
	// Next is the cursor of the older page, as ?before=; empty at the end.
	Next string `json:"next,omitempty"`
	// Best is the date of the viewer's best day by points.
	Best string `json:"best,omitempty"`
}

// finalDate is the newest date, up to newest, that is over in every zone.
func (s *Server) finalDate(now time.Time, newest round.Date) round.Date {
	d := newest
	// The last reveal of a date is less than two days after it begins.
	for i := 0; i < 4 && now.Before(s.latestReveal(d)); i++ {
		d = d.AddDays(-1)
	}
	return d
}

// days lists a page of dates, newest first, with ?before= the cursor of the
// page that comes after another.
func (s *Server) days(w http.ResponseWriter, r *http.Request) {
	v, err := s.viewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	n := daysShown
	if raw := q.Get("n"); raw != "" {
		if n, err = strconv.Atoi(raw); err != nil || n < 1 || n > daysMost {
			s.fail(w, r, errBadRequest)
			return
		}
	}
	var before round.Date
	if raw := q.Get("before"); raw != "" {
		if before, err = round.ParseDate(raw); err != nil {
			s.fail(w, r, errBadRequest)
			return
		}
	}
	now := s.Now()
	newest := s.firstDate()
	final := s.finalDate(now, newest)
	seen := round.DateIn(now, v.zone)
	if newest.Before(seen) {
		seen = newest
	}
	page, err := s.Store.DaysPage(r.Context(), db.DaysQuery{
		Newest: newest, Before: before, N: n, Final: final, Seen: seen,
		UserID: v.user, Player: v.player, Players: v.more(), ChatID: v.chatID,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := daysResponse{Days: make([]dayView, 0, len(page.Days))}
	for _, d := range page.Days {
		x := dayView{Date: d.Date.String(), Day: d.DayNo, Played: d.Played}
		if final.Before(d.Date) {
			// Open somewhere: how many have answered, and when it closes.
			x.OpensAt = s.latestReveal(d.Date).UTC().Format(time.RFC3339)
			x.Pending = d.Players + d.Pending
			if d.Image != "" {
				x.Image, x.Kind = imagePath(d.Image), d.Kind
			}
			out.Days = append(out.Days, x)
			continue
		}
		x.Final = true
		x.Players, x.Pending, x.MedianKm, x.BestKm, x.CountryPct = d.Players, d.Pending, d.MedianKm, d.BestKm, d.CountryPct
		x.City, x.Country = localized(d.City, v.lang), d.Country
		if d.Thumb != "" {
			x.Thumb = thumbPath(d.Thumb, false)
		}
		if g := d.Group; g != nil {
			x.Group = &groupDay{Answered: g.Answered, Members: g.Members, BestName: g.BestName, BestKm: g.BestKm}
		}
		if m := d.Me; m != nil {
			x.Me = &myDay{Km: m.Km, Points: m.Points, Rank: m.Rank}
		}
		out.Days = append(out.Days, x)
	}
	if page.More && len(page.Days) > 0 {
		out.Next = page.Days[len(page.Days)-1].Date.String()
	}
	if !page.Best.IsZero() {
		out.Best = page.Best.String()
	}
	writeJSON(w, http.StatusOK, out)
}

type rankLine struct {
	Rank int `json:"rank"`
	people.Person
	Km     float64 `json:"km"`
	Points int     `json:"points"`
}

// dayMe is the viewer's own answer on a closed day, with the parts of its
// points.
type dayMe struct {
	Rank   int     `json:"rank"`
	Km     float64 `json:"km"`
	Points int     `json:"points"`
	Bonus  int     `json:"bonus"`
	Speed  int     `json:"speed,omitempty"`
	Pin    *pin    `json:"pin,omitempty"`
}

type dayResponse struct {
	Date       string     `json:"date"`
	Day        int        `json:"day"`
	Place      placeView  `json:"place"`
	MedianKm   *float64   `json:"median_km"`
	BestKm     *float64   `json:"best_km,omitempty"`
	CountryPct int        `json:"country_pct"`
	Players    int        `json:"players"`
	Ranking    []rankLine `json:"ranking"`
	// Thumb is the wide strip, once drawn.
	Thumb string `json:"thumb,omitempty"`
	// Image is the day's panorama or photo, for the 360° view.
	Image   string   `json:"image"`
	Kind    string   `json:"kind"`
	Heading *float64 `json:"heading,omitempty"`
	// Pins are everyone's answers without names, Misses everyone's
	// distances, shortest first.
	Pins   [][2]float64 `json:"pins"`
	Misses []float64    `json:"misses"`
	Me     *dayMe       `json:"me,omitempty"`
	Prev   int          `json:"prev,omitempty"`
	Next   int          `json:"next,omitempty"`
}

// day is one day's page, asked for by its number (?n=) or its date. A day
// still open somewhere answers 403 with when it closes, and nothing about the
// place.
func (s *Server) day(w http.ResponseWriter, r *http.Request) {
	v, err := s.viewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	var d round.Date
	dayNo := 0
	if raw := q.Get("n"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > dayMost {
			s.fail(w, r, errBadRequest)
			return
		}
		rd, ok, err := s.Store.RoundNo(ctx, n)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !ok {
			s.fail(w, r, errNoRound)
			return
		}
		d, dayNo = rd.Date, rd.DayNo
	} else if d, err = round.ParseDate(q.Get("date")); err != nil {
		s.fail(w, r, errBadRequest)
		return
	}
	if opens := s.latestReveal(d); s.Now().Before(opens) {
		body := map[string]any{"error": errNotRevealed.code, "opens_at": opens.UTC(), "date": d.String()}
		if dayNo != 0 {
			body["day"] = dayNo
		}
		writeJSON(w, http.StatusForbidden, body)
		return
	}
	c, err := s.closedPage(ctx, d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rd, answer, world, more := c.round, c.answer, *c.world, *c.more
	resp := dayResponse{
		Date: d.String(), Day: rd.DayNo,
		Place: placeView{
			Lat: answer.Point.Lat, Lon: answer.Point.Lon, Country: answer.CountryISO,
			City: localized(answer.City, v.lang), Fact: localized(answer.Fact, v.lang),
			Author: answer.Author, License: answer.License, Source: answer.Source,
		},
		MedianKm: world.MedianKm, CountryPct: world.CountryPct, Players: world.Players(),
		Ranking: make([]rankLine, 0, min(len(world.Entries), rankShown+1)),
		Image:   imagePath(more.Image), Kind: more.Kind, Heading: more.Heading,
		Pins: sample(more.Pins, dayPinsShown), Prev: more.Prev, Next: more.Next,
	}
	if more.Thumb != "" {
		resp.Thumb = thumbPath(more.Thumb, true)
	}
	misses := make([]float64, len(world.Entries))
	own := ownRow(world.Entries, func(e db.Entry) bool { return v.owns(e.UserID, e.Player) })
	for i, e := range world.Entries {
		misses[i] = e.Km
		me := i == own
		if i < rankShown || me {
			l := rankLine{Rank: e.Rank, Km: e.Km, Points: e.Points}
			if e.Site() {
				l.Person = people.SitePlayer(e.Name, me)
			} else {
				l.Person = people.Telegram(e.UserID, e.Name, me)
			}
			resp.Ranking = append(resp.Ranking, l)
		}
		if me {
			resp.Me = &dayMe{Rank: e.Rank, Km: e.Km, Points: e.Points, Bonus: e.Bonus, Speed: e.Speed}
		}
	}
	who := make([]*people.Person, len(resp.Ranking))
	for i := range resp.Ranking {
		who[i] = &resp.Ranking[i].Person
	}
	if err := s.dress(ctx, dressAsk{chatID: v.chatID, viewer: v.user, lang: v.lang, scope: handleScope("day", d, v.chatID)}, who...); err != nil {
		s.fail(w, r, err)
		return
	}
	sort.Float64s(misses)
	if len(misses) > 0 {
		resp.BestKm = &misses[0]
	}
	resp.Misses = sample(misses, missesShown)
	if resp.Me != nil {
		// The viewer's own pin is theirs to see on the map.
		if pt, ok, err := s.pinOf(ctx, d, world.Entries[own]); err == nil && ok {
			resp.Me.Pin = &pin{Lat: pt.Lat, Lon: pt.Lon}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// sample keeps at most n of list, evenly spread, the first and the last
// among them.
func sample[T any](list []T, n int) []T {
	if len(list) <= n {
		return list
	}
	out := make([]T, 0, n)
	for i := range n {
		out = append(out, list[i*(len(list)-1)/(n-1)])
	}
	return out
}

// closedDay is what a day over everywhere shows; world and more are nil
// until the day's page was asked for.
type closedDay struct {
	until  time.Time
	round  db.Round
	answer db.Answer
	world  *db.World
	more   *db.ClosedDay
}

// closedPage is a closed day's round, place, ranking and the rest of its
// page, read once per closedFor or sooner. The ranking is scored first: whatever was
// not since the last reveal.
func (s *Server) closedPage(ctx context.Context, d round.Date) (closedDay, error) {
	s.closedMu.Lock()
	c, ok := s.closed[d]
	s.closedMu.Unlock()
	if ok && s.Now().Before(c.until) {
		return c, nil
	}
	rd, ok, err := s.Store.RoundOn(ctx, d)
	if err != nil {
		return closedDay{}, err
	}
	if !ok {
		return closedDay{}, errNoRound
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return closedDay{}, err
	}
	if err := s.scoreDay(ctx, d, answer, s.Now()); err != nil {
		return closedDay{}, err
	}
	world, err := s.Store.World(ctx, d)
	if err != nil {
		return closedDay{}, err
	}
	// The arrow to the next day stops at the newest one over everywhere.
	more, err := s.Store.ClosedDay(ctx, d, s.finalDate(s.Now(), s.firstDate()))
	if err != nil {
		return closedDay{}, err
	}
	now := s.Now()
	until := now.Add(closedFor)
	if more.Thumb == "" {
		// A day closed a minute ago has no strip yet.
		until = now.Add(closedSoon)
	}
	if next := s.latestReveal(d.AddDays(1)); more.Next == 0 && next.After(now) && next.Before(until) {
		// The newest day over everywhere gets its arrow forward when the
		// next one closes.
		until = next
	}
	c = closedDay{until: until, round: rd, answer: answer, world: &world, more: &more}
	s.closedMu.Lock()
	if s.closed == nil {
		s.closed = map[round.Date]closedDay{}
	}
	// A few days' pages at a time: one of the others makes room.
	if _, ok := s.closed[d]; !ok && len(s.closed) >= 2*daysShown {
		for k := range s.closed {
			delete(s.closed, k)
			break
		}
	}
	s.closed[d] = c
	s.closedMu.Unlock()
	return c, nil
}

// thumbName is how a strip is asked for: its key, with ".w" for the wide one.
var thumbName = regexp.MustCompile(`^([A-Za-z0-9]{20})(\.w)?\.jpg$`)

func thumbPath(key string, wide bool) string {
	if wide {
		return "/thumb/" + key + ".w.jpg"
	}
	return "/thumb/" + key + ".jpg"
}

// thumbsKept is how many strips the server keeps in memory: a page of the
// list and the pages opened from it. Cloudflare keeps the rest.
const thumbsKept = 256

// thumb serves a closed day's strip. The key exists only once the day is
// kept closed (migrations/017_day_thumb.sql), and names nothing.
func (s *Server) thumb(w http.ResponseWriter, r *http.Request) {
	m := thumbName.FindStringSubmatch(r.PathValue("name"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	if !s.thumbLimit.allowIP(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	key, wide := m[1], m[2] != ""
	s.thumbsMu.Lock()
	t, ok := s.thumbs[key]
	s.thumbsMu.Unlock()
	if !ok {
		var err error
		t, ok, err = s.Store.ThumbByKey(r.Context(), key)
		if err != nil {
			s.Log.Error("thumb not read", "error", err)
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.thumbsMu.Lock()
		if s.thumbs == nil || len(s.thumbs) >= thumbsKept {
			s.thumbs = map[string]db.Thumb{}
		}
		s.thumbs[key] = t
		s.thumbsMu.Unlock()
	}
	data := t.Small
	if wide {
		data = t.Wide
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}
