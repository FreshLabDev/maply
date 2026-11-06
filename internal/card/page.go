// SPDX-License-Identifier: Apache-2.0

package card

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/web"
)

//go:embed assets
var assets embed.FS

var page = template.Must(template.ParseFS(assets, "assets/card.html"))

const (
	topRows   = 3
	nameLimit = 24
	// From this many players on the marks shrink so the map stays readable.
	manyPlayers  = 9
	crowdPlayers = 17
)

type row struct {
	Rank    int
	Initial string
	Photo   template.URL // a data: URL of the profile photo, or empty
	Name    string
	Dist    string
	Points  string
}

type pageData struct {
	Lang, Dir, Density string
	Day                int
	Place              string
	Rows               []row
	More               string
	Nobody             string
	// Data is what card.js reads: the true point and every pin.
	Data template.JS
}

type mapPin struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Initial string  `json:"initial"`
	Photo   string  `json:"photo,omitempty"`
}

type mapData struct {
	Lang  string `json:"lang"`
	Truth struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"truth"`
	Pins []mapPin `json:"pins"`
}

var errClosed = errors.New("result is not open yet")

// view gathers the card's page for a chat and a date. It refuses before the
// chat's reveal.
func (s *Service) view(ctx context.Context, chatID int64, d round.Date) (pageData, error) {
	opens, err := s.opens(ctx, chatID, d)
	if err != nil {
		return pageData{}, err
	}
	if s.now().Before(opens) {
		return pageData{}, errClosed
	}
	rd, ok, err := s.Store.RoundOn(ctx, d)
	if err != nil || !ok {
		return pageData{}, orMissing(err, "round")
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return pageData{}, err
	}
	lang, err := s.langOf(ctx, chatID)
	if err != nil {
		return pageData{}, err
	}
	var guesses []db.Guess
	if chatID < 0 {
		guesses, err = s.Store.ChatGuesses(ctx, d, chatID)
	} else if g, ok, gerr := s.Store.GuessOf(ctx, d, chatID); gerr != nil {
		err = gerr
	} else if ok {
		guesses = []db.Guess{g}
	}
	if err != nil {
		return pageData{}, err
	}

	md := mapData{Lang: lang, Pins: make([]mapPin, 0, len(guesses))}
	md.Truth.Lat, md.Truth.Lon = answer.Point.Lat, answer.Point.Lon
	pd := pageData{
		Lang: lang, Dir: "ltr", Density: density(len(guesses)), Day: rd.DayNo,
		Place: Place(lang, answer), Nobody: i18n.T(lang, "result.nobody"),
	}
	if lang == "ar" {
		pd.Dir = "rtl"
	}
	photos := s.photos(ctx, guesses)
	for i, g := range guesses {
		md.Pins = append(md.Pins, mapPin{Lat: g.Pin.Lat, Lon: g.Pin.Lon, Initial: initial(g.FirstName), Photo: photos[g.UserID]})
		if i >= topRows {
			continue
		}
		r := row{Rank: i + 1, Initial: initial(g.FirstName), Photo: template.URL(photos[g.UserID]), Name: shortName(g.FirstName), Dist: "…"}
		if g.Km != nil {
			r.Dist = i18n.FormatDistance(lang, *g.Km)
			r.Points = i18n.FormatNumber(lang, g.Total())
		}
		pd.Rows = append(pd.Rows, r)
	}
	if n := len(guesses) - topRows; n > 0 {
		pd.More = "+" + strconv.Itoa(n)
	}
	// json.Marshal escapes <, > and &, so the data cannot close its own tag.
	raw, err := json.Marshal(md)
	if err != nil {
		return pageData{}, err
	}
	pd.Data = template.JS(raw)
	return pd, nil
}

// photos is the kept photo of each player as a data: URL. The bytes are
// JPEGs maply encoded itself, so the URL is safe to put in the page.
func (s *Service) photos(ctx context.Context, guesses []db.Guess) map[int64]string {
	out := map[int64]string{}
	if s.Avatars == nil {
		return out
	}
	ids := make([]int64, len(guesses))
	for i, g := range guesses {
		ids[i] = g.UserID
	}
	for id, b := range s.Avatars.Kept(ctx, ids) {
		out[id] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
	}
	return out
}

func density(players int) string {
	switch {
	case players >= crowdPlayers:
		return "crowd"
	case players >= manyPlayers:
		return "many"
	}
	return "few"
}

func initial(first string) string {
	r, _ := utf8.DecodeRuneInString(strings.TrimSpace(first))
	if r == utf8.RuneError {
		return "?"
	}
	return strings.ToUpper(string(r))
}

func shortName(first string) string {
	first = strings.TrimSpace(first)
	if first == "" {
		return "?"
	}
	if r := []rune(first); len(r) > nameLimit {
		return string(r[:nameLimit]) + "…"
	}
	return first
}

// imageName is a key and one of the two suffixes the shared image is served
// under.
var imageName = regexp.MustCompile(`^([A-Za-z0-9_-]{22})(\.t)?\.jpg$`)

// Handler serves the card page, its two assets and the shared images.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /card/result/{chat}/{date}", s.servePage)
	mux.HandleFunc("GET /card/assets/{name}", serveAsset)
	mux.HandleFunc("GET /card/img/{name}", s.serveImage)
	return mux
}

// pageError is a page the renderer can tell from a drawn card: it carries
// data-ready="failed". A browser sees a short note.
func pageError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html data-ready="failed"><meta charset="utf-8"><title>%d</title><p>%d`, status, status)
}

func (s *Service) servePage(w http.ResponseWriter, r *http.Request) {
	chatID, err := strconv.ParseInt(r.PathValue("chat"), 10, 64)
	date, derr := round.ParseDate(r.PathValue("date"))
	if err != nil || derr != nil {
		pageError(w, http.StatusNotFound)
		return
	}
	// One answer for every kind of failure: a guess at a token learns nothing.
	p, err := s.Signer.VerifyCard(r.URL.Query().Get("t"))
	if err != nil || p.ChatID != chatID || p.Date != date {
		pageError(w, http.StatusNotFound)
		return
	}
	pd, err := s.view(r.Context(), chatID, date)
	if errors.Is(err, errClosed) {
		pageError(w, http.StatusForbidden)
		return
	}
	if err != nil {
		if s.Log != nil {
			s.Log.Error("card page failed", "chat", chatID, "date", date, "error", err)
		}
		pageError(w, http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", web.CSP)
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Robots-Tag", "noindex")
	if err := page.Execute(w, pd); err != nil && s.Log != nil {
		s.Log.Error("card page not written", "error", err)
	}
}

var assetTypes = map[string]string{
	"card.css": "text/css; charset=utf-8",
	"card.js":  "text/javascript; charset=utf-8",
}

func serveAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctype, ok := assetTypes[name]
	data, err := assets.ReadFile("assets/" + name)
	if !ok || err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func (s *Service) serveImage(w http.ResponseWriter, r *http.Request) {
	m := imageName.FindStringSubmatch(r.PathValue("name"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	img := s.images.get(m[1], s.now())
	if img == nil {
		http.NotFound(w, r)
		return
	}
	body := img.jpeg
	if m[2] != "" {
		body = img.thumb
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", "public, max-age=3600")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Robots-Tag", "noindex")
	_, _ = w.Write(body)
}
