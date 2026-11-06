// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/people"
	"github.com/FreshLabDev/maply/internal/profile"
)

// The public page /p/<slug>: a player's numbers under a link they turned on,
// for anyone who has it. It holds what a stranger's view of a profile holds
// and the charts, over the days over in every zone, and nothing that finds
// the person: no id, username, photo, pins, history or handle. Search
// engines are asked to stay away. A new link replaces the old, which then
// answers like one that never was.

// Stills draws the picture shown in a link's preview.
type Stills interface {
	Still(ctx context.Context, userID int64, lang string) ([]byte, error)
}

// keptStills says which pictures are drawn already, so serving one costs
// nothing of the limit on drawing.
type keptStills interface {
	Kept(userID int64, lang string) []byte
}

var _ keptStills = (*profile.Service)(nil)

var (
	errNoPage = apiError{http.StatusNotFound, "not_found"}
	slugForm  = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
)

type publicProfile struct {
	people.Person
	Ranked profile.Ranked `json:"ranked"`
	Free   *profile.Free  `json:"free"`
}

// publicOwner is whose page the request's slug names. The slug is the
// page's only key: fail logs the route's pattern, not the path. Finding the
// owner is one indexed read and counts per address; all, when set, is the
// limit from everyone on what comes after it, so made-up links from many
// addresses do not use it up for the pages that exist.
func (s *Server) publicOwner(w http.ResponseWriter, r *http.Request, all *limiter) (ProfileStore, int64, bool) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	ps := s.profileStore()
	if ps == nil {
		s.fail(w, r, errUnavailable)
		return nil, 0, false
	}
	if !s.ipLimit.allowIP(clientIP(r)) {
		s.fail(w, r, errRateLimited)
		return nil, 0, false
	}
	slug := r.PathValue("slug")
	if !slugForm.MatchString(slug) {
		s.fail(w, r, errNoPage)
		return nil, 0, false
	}
	id, ok, err := ps.PublicOwner(r.Context(), slug)
	if err != nil {
		s.fail(w, r, err)
		return nil, 0, false
	}
	if !ok {
		s.fail(w, r, errNoPage)
		return nil, 0, false
	}
	if all != nil && !all.allowKey("") {
		s.fail(w, r, errRateLimited)
		return nil, 0, false
	}
	return ps, id, true
}

// publicPage is the page's numbers.
func (s *Server) publicPage(w http.ResponseWriter, r *http.Request) {
	// A page reads a player's whole history: from everyone together too.
	ps, id, ok := s.publicOwner(w, r, s.profAll)
	if !ok {
		return
	}
	ctx := r.Context()
	lang := i18n.Resolve(r.URL.Query().Get("lang"))
	// No percentile: its line speaks to the player ("you are better than"),
	// and it reads every answer of every day they played.
	ask := rankedAsk{who: db.Who{UserID: id}, upTo: profile.LatestClosed(s.Zones, s.Now())}
	ranked, err := s.ranked(ctx, ps, ask, lang)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name, err := ps.NameOf(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// A stranger's view: the name the owner made public, and no photo or id,
	// so the person has no ID for dress to fill anything in by.
	writeJSON(w, http.StatusOK, publicProfile{Person: people.Person{Name: name}, Ranked: ranked, Free: s.free(ctx, ps, db.FreeWho{UserID: id})})
}

// publicCard is the page's picture for a link preview.
func (s *Server) publicCard(w http.ResponseWriter, r *http.Request) {
	if s.Stills == nil {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		s.fail(w, r, errUnavailable)
		return
	}
	// The picture has one address. Cloudflare keys its copy by the query
	// too, so "?x=1", "?x=2" would each come here: any query is sent back to
	// the address without one, which the edge then keeps.
	if r.URL.RawQuery != "" {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		slug := r.PathValue("slug")
		if !slugForm.MatchString(slug) {
			s.fail(w, r, errNoPage)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.Redirect(w, r, "/api/p/"+slug+"/card.png", http.StatusMovedPermanently)
		return
	}
	_, id, ok := s.publicOwner(w, r, nil)
	if !ok {
		return
	}
	// In the owner's language, whoever fetches it: one picture per player.
	lang, err := s.Store.UserLang(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	lang = i18n.Resolve(lang)
	png := []byte(nil)
	if k, ok := s.Stills.(keptStills); ok {
		png = k.Kept(id, lang)
	}
	if png == nil {
		// Drawing takes a moment, so only a drawing counts against everyone:
		// a picture drawn already is served to any number of fetches.
		// Cloudflare keeps it for the minutes below, so a preview fetched in
		// many chats reaches the bot once.
		if !s.cardAll.allowKey("") {
			s.fail(w, r, errRateLimited)
			return
		}
		png, err = s.Stills.Still(r.Context(), id, lang)
		if errors.Is(err, profile.ErrNothing) {
			s.fail(w, r, errNoPage)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}
	w.Header().Set("Content-Type", "image/png")
	// Short: a link turned off or replaced takes its picture with it once
	// the edge's copy runs out.
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(png)
}

// profilePublic turns the player's public page on with a new link, which
// revokes the old, or off.
func (s *Server) profilePublic(w http.ResponseWriter, r *http.Request) {
	ps := s.profileStore()
	if ps == nil {
		s.fail(w, r, errUnavailable)
		return
	}
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if v.user == 0 {
		s.fail(w, r, errUnauthorized)
		return
	}
	var body struct {
		On *bool `json:"on"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.On == nil {
		s.fail(w, r, errBadRequest)
		return
	}
	slug := ""
	if *body.On {
		slug = profile.NewSlug()
	}
	ctx := r.Context()
	if err := s.touchProfile(ctx, ps, v); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := ps.SetPublicSlug(ctx, v.user, slug); err != nil {
		s.fail(w, r, err)
		return
	}
	set, err := ps.ProfileSettingOf(ctx, v.user)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingView(set))
}
