// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"net/http"
	"regexp"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/profile"
)

// The old public page /p/<slug>: a link a player turned on before profiles
// had one address. It now leads to their profile, /u/<username>, while the
// player has not hidden themselves; the link's preview picture follows.

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

// publicPage sends the app on to the profile the old link names.
func (s *Server) publicPage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.publicPersona(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, movedUser{Moved: p.Username})
}

// publicCard sends a link preview on to the profile's picture.
func (s *Server) publicCard(w http.ResponseWriter, r *http.Request) {
	p, ok := s.publicPersona(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.Redirect(w, r, "/api/u/"+p.Username+"/card.png", http.StatusFound)
}

// publicPersona is the profile of the player whose old link the request
// names, unless they hid themselves.
func (s *Server) publicPersona(w http.ResponseWriter, r *http.Request) (db.Persona, bool) {
	ps, id, ok := s.publicOwner(w, r, nil)
	if !ok {
		return db.Persona{}, false
	}
	st := s.personaStore()
	if st == nil {
		s.fail(w, r, errUnavailable)
		return db.Persona{}, false
	}
	ctx := r.Context()
	set, err := ps.ProfileSettingOf(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return db.Persona{}, false
	}
	if set.HideMe {
		s.fail(w, r, errNoPage)
		return db.Persona{}, false
	}
	p, ok, err := st.PersonaOfUser(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return db.Persona{}, false
	}
	if !ok {
		s.fail(w, r, errNoPage)
		return db.Persona{}, false
	}
	return p, true
}
