// SPDX-License-Identifier: Apache-2.0
package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
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
// language, about 120 KB; Cloudflare keeps the rest.
const ogKept = 48

// ogName is a card's file: the day's number and the language of its words.
var ogName = regexp.MustCompile(`^([1-9][0-9]{0,5})-([a-z]{2})\.jpg$`)

// dayPath is a day's page by its number, the one address a card is for.
var dayPath = regexp.MustCompile(`^/days/([1-9][0-9]{0,5})$`)

// PageMeta is the preview of a closed day's page in lang from ?lang= or the
// reader's Accept-Language; false for any other page, for a day still open
// somewhere and for a day whose strip is not drawn yet.
func (s *Server) PageMeta(r *http.Request, path string) (web.Meta, bool) {
	m := dayPath.FindStringSubmatch(path)
	if m == nil || s.AppURL == "" {
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
	key := m[1] + "-" + lang
	s.ogMu.Lock()
	data, ok := s.ogs[key]
	s.ogMu.Unlock()
	if !ok {
		// One card is drawn at a time: a panorama decoded takes 32 MB.
		select {
		case s.ogTurn <- struct{}{}:
		case <-r.Context().Done():
			return
		}
		data, err = s.drawCard(n, lang, c)
		<-s.ogTurn
		if err != nil {
			// The error names the image by its random key at most.
			s.Log.Warn("day card not drawn", "day", n, "error", err)
			http.NotFound(w, r)
			return
		}
		s.ogMu.Lock()
		if s.ogs == nil || len(s.ogs) >= ogKept {
			s.ogs = map[string][]byte{}
		}
		s.ogs[key] = data
		s.ogMu.Unlock()
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("X-Content-Type-Options", "nosniff")
	// A closed day's numbers stay; the drawing may change with a release.
	h.Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
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
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
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
