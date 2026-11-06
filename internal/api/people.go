// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"net/http"
	"regexp"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/people"
	"github.com/FreshLabDev/maply/internal/round"
)

// Every player the API sends goes through dress: the tables of the day and
// of the world, the days' pages, the leaderboards, the profiles and the
// rooms of free play. A handler fills in who a row is (people.Telegram,
// SitePlayer, Unnamed) and dress decides what the viewer sees of them, so
// "Hide me", the username, the person's own name and the photo follow one
// rule everywhere.

// PersonaStore is the players' profiles: their usernames and what they chose
// to show over Telegram. The store has it.
type PersonaStore interface {
	PersonasOf(ctx context.Context, ids []int64, players, accounts []string) (db.Personas, error)
	PersonaByUsername(ctx context.Context, username string) (p db.Persona, moved, ok bool, err error)
	PersonaOfUser(ctx context.Context, userID int64) (db.Persona, bool, error)
	PersonaOfAccount(ctx context.Context, accountID string) (db.Persona, bool, error)
	EnsurePersona(ctx context.Context, userID int64, tgUsername, firstName string) error
	EnsureAccountPersona(ctx context.Context, accountID, name string) (db.Persona, error)
	SetUsername(ctx context.Context, id, username string) error
	UsernameFree(ctx context.Context, id, username string) (bool, error)
	SetOwnName(ctx context.Context, id, name string) error
	SetOwnPhoto(ctx context.Context, id string, jpeg []byte) (string, error)
	MarkAsked(ctx context.Context, id string) error
}

var _ PersonaStore = (*db.Store)(nil)

// personaStore is the store's profiles, nil when it has none.
func (s *Server) personaStore() PersonaStore {
	ps, _ := s.Store.(PersonaStore)
	return ps
}

// dressAsk is who looks at the players, and from where.
type dressAsk struct {
	// chatID and viewer are the chat the view is opened from and the
	// viewer's Telegram id, zero on the site without Telegram: a player who
	// hid themselves keeps their name in their own groups.
	chatID, viewer int64
	lang           string
	// guest is a viewer who has not signed in: a site visitor without an
	// account. They see no photos, only names.
	guest bool
	// together keeps every name: a party and a challenge, whose players
	// chose to play with each other.
	together bool
}

// dress fills in what the viewer sees of each person. A Hidden row loses
// its name, face and username. A Telegram player who hid themselves is
// "Anonymous" with no face, except to themselves, to their group (Group)
// and to the groups they share with the viewer; a site row of their account
// keeps the site's name and nothing else. Their username goes to nobody but
// themselves, as their profile opens to nobody else. Anyone else gets their
// username, the name they chose over Telegram's, and the address of their
// photo, their own or the one kept from Telegram. Telegram is asked in the
// background for the photos not asked today, so a new face shows on a later
// page.
func (s *Server) dress(ctx context.Context, a dressAsk, ps ...*people.Person) error {
	var ids []int64
	var players, accounts []string
	for _, p := range ps {
		p.Photo, p.U, p.Anon = "", "", false
		switch {
		case p.Hidden:
			p.Name = ""
		case p.ID != 0:
			ids = append(ids, p.ID)
		case p.Player != "":
			players = append(players, p.Player)
		case p.Account != "":
			accounts = append(accounts, p.Account)
		}
	}
	pers := db.Personas{}
	if st := s.personaStore(); st != nil && len(ids)+len(players)+len(accounts) > 0 {
		var err error
		if pers, err = st.PersonasOf(ctx, ids, players, accounts); err != nil {
			return err
		}
	}
	personaOf := func(p *people.Person) (db.Persona, bool) {
		switch {
		case p.ID != 0:
			x, ok := pers.ByUser[p.ID]
			return x, ok
		case p.Player != "":
			x, ok := pers.ByPlayer[p.Player]
			return x, ok
		case p.Account != "":
			x, ok := pers.ByAccount[p.Account]
			return x, ok
		}
		return db.Persona{}, false
	}
	// The Telegram person behind a row: a site row of an account with
	// Telegram is that person too.
	telegramOf := func(p *people.Person) int64 {
		if p.ID != 0 {
			return p.ID
		}
		x, _ := personaOf(p)
		return x.UserID
	}
	// closed are the people who hid themselves: their profile opens to them
	// alone, so no row of theirs carries the username that leads to it, even
	// where the name stays. hidden are those of them the viewer does not see
	// by name.
	var others []int64
	for _, p := range ps {
		// The viewer's own rows are theirs whichever player wrote them: a
		// site row their account took in is not Me in the Mini App.
		if id := telegramOf(p); id != 0 && id != a.viewer && !p.Hidden && !p.Me {
			others = append(others, id)
		}
	}
	closed := map[int64]bool{}
	if pst := s.profileStore(); pst != nil && len(others) > 0 {
		var err error
		if closed, err = pst.Hidden(ctx, others); err != nil {
			return err
		}
	}
	var asked []int64
	for _, p := range ps {
		if id := telegramOf(p); closed[id] && !p.Hidden && !p.Me && !p.Group && !a.together {
			asked = append(asked, id)
		}
	}
	hidden, err := s.maskHidden(ctx, a.chatID, a.viewer, asked)
	if err != nil {
		return err
	}
	var faceIDs []int64
	for _, p := range ps {
		if p.Hidden {
			continue
		}
		tid := telegramOf(p)
		if tid != 0 && hidden[tid] {
			if p.ID != 0 {
				p.Name, p.Anon = anonName(a.lang), true
			}
			continue
		}
		x, ok := personaOf(p)
		if ok {
			if !closed[tid] {
				p.U = x.Username
			}
			if x.Name != "" {
				p.Name = x.Name
			}
		}
		switch {
		case a.guest:
		case tid != 0:
			faceIDs = append(faceIDs, tid)
		case ok && x.Face != "":
			p.Photo = facePath(x.Face)
		}
	}
	faces := s.faces(ctx, faceIDs)
	for _, p := range ps {
		if p.Hidden || p.Anon || a.guest {
			continue
		}
		if key, ok := faces[telegramOf(p)]; ok {
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

// ownRow is the row of the world's ranking that is the viewer's, -1 when
// none is. An account may hold two answers of a date, from before its
// players were one: the earlier is theirs, as the days' list shows it
// (db.DaysPage), and the other stays as everyone else sees it.
func ownRow(entries []db.Entry, owns func(db.Entry) bool) int {
	own := -1
	for i, e := range entries {
		if owns(e) && (own < 0 || e.At.Before(entries[own].At)) {
			own = i
		}
	}
	return own
}

// pinOf is where an entry of the world's ranking put its pin.
func (s *Server) pinOf(ctx context.Context, d round.Date, e db.Entry) (geo.Point, bool, error) {
	if e.UserID != 0 {
		g, ok, err := s.Store.GuessOf(ctx, d, e.UserID)
		return g.Pin, ok, err
	}
	g, ok, err := s.Store.SiteGuessOf(ctx, d, e.Player)
	return g.Pin, ok, err
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
