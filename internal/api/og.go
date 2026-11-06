// SPDX-License-Identifier: Apache-2.0
package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/card"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/thumb"
	"github.com/FreshLabDev/maply/web"
)

// A link to a day's page, /days/<n>, opens in a messenger or a feed as the
// day's card: its street, its place and the world's numbers. Only a day kept
// closed with its strip drawn has one; before that the link is the plain
// page and /og/ answers 404, the same as for a day that never was.

// ogKept is how many cards stay drawn in memory. A card is a day in one
// language, about 90 KB; Cloudflare keeps the rest.
const ogKept = 96

// ogWait is how long a request waits for its turn to draw: a link's crawler
// gives up after a few seconds anyway.
const ogWait = 5 * time.Second

// ogName is a card's file: the day's number and the language of its words.
var ogName = regexp.MustCompile(`^([1-9][0-9]{0,5})-([a-z]{2})\.jpg$`)

// dayPath is a day's page by its number, the one address a card is for.
var dayPath = regexp.MustCompile(`^/days/([1-9][0-9]{0,5})$`)

// PageMeta is the preview of a closed day's page in lang from ?lang= or the
// reader's Accept-Language; false for any other page, for a day still open
// somewhere and for a day whose strip is not drawn yet.
func (s *Server) PageMeta(r *http.Request, path string) (web.Meta, bool) {
	if u, ok := strings.CutPrefix(path, "/u/"); ok {
		return s.userMeta(r, u)
	}
	m := dayPath.FindStringSubmatch(path)
	if m == nil || s.AppURL == "" {
		return web.Meta{}, false
	}
	// Past the limit the page is the plain app: the preview reads the day.
	if !s.metaLimit.allowIP(clientIP(r)) {
		return web.Meta{}, false
	}
	n, _ := strconv.Atoi(m[1])
	c, ok, err := s.cardDay(r.Context(), n)
	if err != nil {
		s.Log.Warn("day preview not read", "day", n, "error", err)
	}
	if !ok {
		return web.Meta{}, false
	}
	lang := pageLang(r)
	city, country := card.City(lang, c.answer), card.Country(lang, c.answer)
	title := "maply #" + strconv.Itoa(n)
	if where := firstOf(city, country); where != "" {
		title += " · " + where
	}
	line := []string{}
	if city != "" && country != "" {
		line = append(line, country)
	}
	if p := c.world.Players(); p > 0 {
		line = append(line, i18n.T(lang, "app.days.played", "n", i18n.FormatNumber(lang, p)))
		if c.world.MedianKm != nil {
			line = append(line, i18n.T(lang, "app.days.median", "km", i18n.FormatDistance(lang, *c.world.MedianKm)))
		}
	} else {
		line = append(line, i18n.T(lang, "app.days.nobody"))
	}
	desc := strings.Join(line, " · ")
	if fact := localized(c.answer.Fact, lang); fact != "" {
		desc += ". " + fact
	}
	base := strings.TrimRight(s.AppURL, "/")
	w, h := thumb.Card.W, thumb.Card.H
	return web.Meta{
		Title: title, Description: desc, URL: base + path,
		Image:  fmt.Sprintf("%s/og/day/%d-%s.jpg", base, n, cardLang(lang, c)),
		ImageW: w, ImageH: h,
	}, true
}

// cardDay is day n's page once it is kept closed and its strip drawn: the
// same days that have a strip have a card.
func (s *Server) cardDay(ctx context.Context, n int) (closedDay, bool, error) {
	rd, ok, err := s.Store.RoundNo(ctx, n)
	if err != nil || !ok {
		return closedDay{}, false, err
	}
	if s.Now().Before(s.latestReveal(rd.Date)) {
		return closedDay{}, false, nil
	}
	c, err := s.closedPage(ctx, rd.Date)
	if err != nil {
		return closedDay{}, false, err
	}
	return c, c.more.Thumb != "", nil
}

// pageLang is the language a preview is asked in: ?lang= when maply has it,
// else the first of Accept-Language it has, else English. A link's crawler
// rarely says, so a link shared from the app names its language.
func pageLang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); i18n.IsSupported(l) {
		return l
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(part, ";")
		if l := i18n.Normalize(tag); i18n.IsSupported(l) {
			return l
		}
	}
	return i18n.DefaultLang
}

// cardLang is the language a card is drawn in: lang when its faces have
// every letter of the card's words, English otherwise. The faces have Latin
// and Cyrillic; a card in Chinese would be boxes.
func cardLang(lang string, c closedDay) string {
	city, country, stats := cardWords(lang, c)
	ok := anim.Covers(city) && anim.Covers(country)
	for _, st := range stats {
		ok = ok && anim.Covers(st.Value) && anim.Covers(st.Label)
	}
	if !ok {
		return i18n.DefaultLang
	}
	return lang
}

// cardWords are what a day's card says in lang.
func cardWords(lang string, c closedDay) (city, country string, stats []anim.Stat) {
	city, country = card.City(lang, c.answer), card.Country(lang, c.answer)
	w := c.world
	if w.Players() == 0 {
		return city, country, []anim.Stat{{Value: "0", Label: i18n.T(lang, "app.days.t_played")}}
	}
	stats = append(stats, anim.Stat{Value: i18n.FormatNumber(lang, w.Players()), Label: i18n.T(lang, "app.days.t_played")})
	if w.MedianKm != nil {
		stats = append(stats, anim.Stat{Value: i18n.FormatKm(lang, *w.MedianKm), Label: i18n.T(lang, "app.days.t_median")})
	}
	best := w.Entries[0].Km
	for _, e := range w.Entries {
		best = min(best, e.Km)
	}
	stats = append(stats, anim.Stat{Value: i18n.FormatKm(lang, best), Label: i18n.T(lang, "app.days.t_best")},
		anim.Stat{Value: strconv.Itoa(w.CountryPct) + "%", Label: i18n.T(lang, "app.days.t_country")})
	return city, country, stats
}

// og serves a day's card, drawn on the first request and kept.
func (s *Server) og(w http.ResponseWriter, r *http.Request) {
	m := ogName.FindStringSubmatch(r.PathValue("name"))
	if m == nil || s.ImagesDir == "" || !i18n.IsSupported(m[2]) {
		http.NotFound(w, r)
		return
	}
	if !s.ogLimit.allowIP(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	n, _ := strconv.Atoi(m[1])
	lang := m[2]
	c, ok, err := s.cardDay(r.Context(), n)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok || cardLang(lang, c) != lang {
		http.NotFound(w, r)
		return
	}
	data, err := s.card(r.Context(), m[1]+"-"+lang, n, lang, c)
	if errors.Is(err, errBusy) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		if r.Context().Err() == nil {
			// The error names the image by its random key at most.
			s.Log.Warn("day card not drawn", "day", n, "error", err)
			http.NotFound(w, r)
		}
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("X-Content-Type-Options", "nosniff")
	// A closed day's numbers stay; the drawing may change with a release.
	h.Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// errBusy is a card that would be drawn past the limit of all cards.
var errBusy = errors.New("cards busy")

// card is the day's card under key, kept or drawn. One card is drawn at a
// time: a panorama decoded and projected takes about 150 MB and a second of
// a core, so the cards drawn by everyone together are limited too, not only
// per address, and one kept in the meantime is not drawn again.
func (s *Server) card(ctx context.Context, key string, n int, lang string, c closedDay) ([]byte, error) {
	kept := func() ([]byte, bool) {
		s.ogMu.Lock()
		defer s.ogMu.Unlock()
		data, ok := s.ogs[key]
		return data, ok
	}
	if data, ok := kept(); ok {
		return data, nil
	}
	wait, cancel := context.WithTimeout(ctx, ogWait)
	defer cancel()
	select {
	case s.ogTurn <- struct{}{}:
	case <-wait.Done():
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errBusy
	}
	defer func() { <-s.ogTurn }()
	if data, ok := kept(); ok {
		return data, nil
	}
	if !s.ogAll.allowKey("all") {
		return nil, errBusy
	}
	data, err := s.drawCard(n, lang, c)
	if err != nil {
		return nil, err
	}
	s.ogMu.Lock()
	defer s.ogMu.Unlock()
	if s.ogs == nil {
		s.ogs = map[string][]byte{}
	}
	if len(s.ogs) >= ogKept {
		for k := range s.ogs {
			delete(s.ogs, k)
			break
		}
	}
	s.ogs[key] = data
	return data, nil
}

// drawCard draws day n's card in lang from the day's picture.
func (s *Server) drawCard(n int, lang string, c closedDay) ([]byte, error) {
	name := filepath.Base(c.more.Image)
	f, err := os.Open(filepath.Join(s.ImagesDir, name))
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return nil, fmt.Errorf("image %s: %w", name, err)
	}
	defer f.Close()
	decoded, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	// Turned into RGBA once: Pick and View take it as it is.
	b := decoded.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Rect, decoded, b.Min, draw.Src)
	yaw := 0.0
	if c.more.Kind == "panorama" && thumb.Pano(img) {
		if c.more.Yaw != nil {
			yaw = *c.more.Yaw
		} else {
			yaw = thumb.Pick(img)
		}
	}
	d := anim.NewDayCard(n, thumb.View(img, yaw, thumb.CardLow, thumb.CardHigh, thumb.Card), lang)
	d.City, d.Country, d.Stats = cardWords(lang, c)
	var buf bytes.Buffer
	// Encoded from pixels: no EXIF.
	if err := jpeg.Encode(&buf, anim.Still(d, 0), &jpeg.Options{Quality: thumb.Card.Quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// userMeta is the preview of a link to a profile, /u/<username>: the name,
// the username and the profile's picture, which has no photo. A profile
// hidden or not there is the plain page.
func (s *Server) userMeta(r *http.Request, username string) (web.Meta, bool) {
	st, ps := s.personaStore(), s.profileStore()
	if s.AppURL == "" || st == nil || ps == nil || !s.metaLimit.allowIP(clientIP(r)) {
		return web.Meta{}, false
	}
	ctx := r.Context()
	p, _, ok, err := st.PersonaByUsername(ctx, username)
	if err != nil {
		s.Log.Warn("profile preview not read", "error", err)
	}
	if !ok {
		return web.Meta{}, false
	}
	lang := pageLang(r)
	name := ""
	if p.UserID != 0 {
		set, err := ps.ProfileSettingOf(ctx, p.UserID)
		if err != nil || set.HideMe {
			return web.Meta{}, false
		}
		if name, err = ps.NameOf(ctx, p.UserID); err != nil {
			return web.Meta{}, false
		}
	} else if _, name, err = s.Store.FreeAccountPlayer(ctx, p.AccountID); err != nil {
		return web.Meta{}, false
	}
	if p.Name != "" {
		name = p.Name
	}
	if name == "" {
		name = i18n.T(lang, "app.guest")
	}
	base := strings.TrimRight(s.AppURL, "/")
	m := web.Meta{Title: name + " · maply", Description: "@" + p.Username + " · " + i18n.T(lang, "app.prof.og_line"),
		URL: base + "/u/" + p.Username, NoIndex: true}
	if p.UserID != 0 {
		m.Image = base + "/api/u/" + p.Username + "/card.png"
	}
	return m, true
}
