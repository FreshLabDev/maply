// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"net/http"
	"regexp"

	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/people"
)

// Every player the API sends goes through dress: the tables of the day and
// of the world, the days' pages, the leaderboards, the profiles and the
// rooms of free play. A handler fills in who a row is (people.Telegram,
// SitePlayer, Unnamed) and dress decides what the viewer sees of them, so
// "Hide me", the handle and the photo follow one rule everywhere.

// dressAsk is who looks at the players, and from where.
type dressAsk struct {
	// chatID and viewer are the chat the view is opened from and the
	// viewer's Telegram id, zero on the site without Telegram: a player who
	// hid themselves keeps their name in their own groups.
	chatID, viewer int64
	lang           string
	// scope names the list for the rows' handles (handleScope); empty sends
	// none, where the list opens no mini profile.
	scope string
	// together keeps every name: a party and a challenge, whose players
	// chose to play with each other.
	together bool
}

// dress fills in what the viewer sees of each person. A Hidden row loses
// its name, face and handle. A Telegram player who hid themselves is
// "Anonymous" with no face or handle, except to themselves, to their group
// (Group) and to the groups they share with the viewer. Any other Telegram
// player gets their handle and the address of their kept photo, and
// Telegram is asked in the background for the photos not asked today, so a
// new face shows on a later page.
func (s *Server) dress(ctx context.Context, a dressAsk, ps ...*people.Person) error {
	var asked []int64
	for _, p := range ps {
		if p.Hidden {
			p.Name, p.Photo, p.Who, p.Anon = "", "", "", false
			continue
		}
		p.Photo, p.Who, p.Anon = "", "", false
		if p.ID != 0 && !p.Me && !p.Group && !a.together {
			asked = append(asked, p.ID)
		}
	}
	hidden, err := s.maskHidden(ctx, a.chatID, a.viewer, asked)
	if err != nil {
		return err
	}
	handles := s.handles()
	var ids []int64
	for _, p := range ps {
		switch {
		case p.Hidden || p.ID == 0:
		case hidden[p.ID]:
			p.Name, p.Anon = anonName(a.lang), true
		default:
			if a.scope != "" {
				p.Who = handles.Of(p.ID, a.scope)
			}
			ids = append(ids, p.ID)
		}
	}
	faces := s.faces(ctx, ids)
	for _, p := range ps {
		if key, ok := faces[p.ID]; ok && !p.Hidden && !p.Anon && p.ID != 0 {
			p.Photo = facePath(key)
		}
	}
	return nil
}

// faces is the addresses of the kept photos of ids, and asks Telegram in the
// background for the ones not asked today.
func (s *Server) faces(ctx context.Context, ids []int64) map[int64]string {
	if s.Avatars == nil || len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]bool, len(ids))
	uniq := ids[:0:0]
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	if len(uniq) == 0 {
		return nil
	}
	s.Avatars.Refresh(uniq)
	return s.Avatars.Faces(ctx, uniq)
}

// facePath is the address of a kept photo. A new photo gets a new key, so
// the address names one picture for good and is cached as such.
func facePath(key string) string { return "/face/" + key + ".jpg" }

var faceName = regexp.MustCompile(`^([0-9a-f]{32})\.jpg$`)

// facePhoto serves a kept photo by its address. The key is random and only
// ever sent next to the name it goes with.
func (s *Server) facePhoto(w http.ResponseWriter, r *http.Request) {
	m := faceName.FindStringSubmatch(r.PathValue("key"))
	if m == nil || s.Avatars == nil {
		http.NotFound(w, r)
		return
	}
	b, ok := s.Avatars.Face(r.Context(), m[1])
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}

// writeTogether sends a party or a challenge with its players' faces. Their
// names stay as the game keeps them: they chose to play with each other.
func (s *Server) writeTogether(w http.ResponseWriter, r *http.Request, v any) {
	var who []*people.Person
	switch x := v.(type) {
	case free.PartyView:
		for i := range x.Members {
			who = append(who, &x.Members[i].Person)
		}
	case free.ChallengeView:
		for i := range x.Lines {
			who = append(who, &x.Lines[i].Person)
		}
	}
	if err := s.dress(r.Context(), dressAsk{together: true}, who...); err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
