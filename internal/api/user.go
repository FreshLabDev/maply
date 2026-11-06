// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/FreshLabDev/maply/internal/avatar"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/people"
	"github.com/FreshLabDev/maply/internal/profile"
	"github.com/FreshLabDev/maply/internal/round"
)

// A player's profile has one address, /u/<username>, the same for everyone
// who opens it. The username comes from Telegram, or maply makes one up for
// someone without; the person may type their own, and the old one keeps
// leading to them until someone takes it. The name and the photo the person
// chose show over Telegram's everywhere until they remove them.
//
// Who sees what on someone else's profile: the numbers over the days over
// everywhere, the charts, the closed days and the free play records, as a
// group would. The photo only to a viewer who has signed in, Mini App or
// account, and never in a link's preview. A Telegram player who chose "Hide
// me" has no profile for anyone but themselves.

var (
	errNoUser       = apiError{http.StatusNotFound, "not_found"}
	errUsernameForm = apiError{http.StatusBadRequest, "username_form"}
	errUsernameUsed = apiError{http.StatusConflict, "username_taken"}
	errPhoto        = apiError{http.StatusBadRequest, "photo"}
)

// profilePrefix starts the start_param of a link that opens the Mini App
// on a profile: u_<username>.
const profilePrefix = "u_"

// profileLink says whether a start_param opens a profile.
func profileLink(startParam string) bool {
	u, ok := strings.CutPrefix(startParam, profilePrefix)
	if !ok {
		return false
	}
	_, err := db.CleanUsername(u)
	return err == nil
}

// profileEdit is what the person may change of how maply shows them.
type profileEdit struct {
	Username string `json:"username"`
	// Name is the person's own name, empty while Telegram's shows; Photo
	// says they have their own photo.
	Name  string `json:"name,omitempty"`
	Photo bool   `json:"photo,omitempty"`
	// Telegram is the first name Telegram gives, which shows without Name;
	// empty for an account without Telegram, which is "Guest" then.
	Telegram string `json:"telegram,omitempty"`
	// Ask is an account without Telegram that was never asked for a name,
	// a username and a photo: the app asks once.
	Ask bool `json:"ask,omitempty"`
}

// personaFor is the viewer's own profile, made now if they have none yet:
// a Telegram person met before profiles existed, or an account without
// Telegram before its first look. ok is false for a guest.
func (s *Server) personaFor(ctx context.Context, v profileViewer, name string) (db.Persona, bool, error) {
	st := s.personaStore()
	switch {
	case st == nil:
		return db.Persona{}, false, nil
	case v.user != 0:
		if p, ok, err := st.PersonaOfUser(ctx, v.user); err != nil || ok {
			return p, ok, err
		}
		if ps := s.profileStore(); ps != nil {
			if err := s.touchProfile(ctx, ps, v); err != nil {
				return db.Persona{}, false, err
			}
		}
		if err := st.EnsurePersona(ctx, v.user, v.person.Username, name); err != nil {
			return db.Persona{}, false, err
		}
		return st.PersonaOfUser(ctx, v.user)
	case v.account != "":
		p, err := st.EnsureAccountPersona(ctx, v.account, name)
		return p, err == nil, err
	}
	return db.Persona{}, false, nil
}

// editOf is the viewer's profileEdit, nil for a guest.
func (s *Server) editOf(ctx context.Context, v profileViewer, name string) (*profileEdit, error) {
	p, ok, err := s.personaFor(ctx, v, name)
	if err != nil || !ok {
		return nil, err
	}
	out := &profileEdit{Username: p.Username, Name: p.Name, Photo: p.Face != "", Ask: p.UserID == 0 && !p.Asked}
	if p.UserID != 0 {
		if tn, ok := s.Store.(telegramNamer); ok {
			if out.Telegram, err = tn.TelegramNameOf(ctx, p.UserID); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// telegramNamer reads the first name Telegram gives a person, under any
// name of their own.
type telegramNamer interface {
	TelegramNameOf(ctx context.Context, userID int64) (string, error)
}

// owns says whether the profile p is the viewer's own.
func (v profileViewer) owns(p db.Persona) bool {
	return (p.UserID != 0 && p.UserID == v.user) || (p.AccountID != "" && p.AccountID == v.account)
}

// userProfile is someone else's profile.
type userProfile struct {
	people.Person
	Ranked profile.Ranked `json:"ranked"`
	Free   *profile.Free  `json:"free"`
	// Duel is the head to head with the viewer over the group's open days,
	// when the profile is opened from a group both are in.
	Duel *headToHead `json:"duel,omitempty"`
}

// movedUser is a username its owner left: the app goes to the new one.
type movedUser struct {
	Moved string `json:"moved"`
}

// userOf is the profile at the request's username, and whether the viewer
// may see it: a profile hidden from them reads as one that is not there.
func (s *Server) userOf(w http.ResponseWriter, r *http.Request, v profileViewer) (db.Persona, bool, bool) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	st, ps := s.personaStore(), s.profileStore()
	if st == nil || ps == nil {
		s.fail(w, r, errUnavailable)
		return db.Persona{}, false, false
	}
	ctx := r.Context()
	p, moved, ok, err := st.PersonaByUsername(ctx, r.PathValue("username"))
	if err != nil {
		s.fail(w, r, err)
		return db.Persona{}, false, false
	}
	if !ok {
		s.fail(w, r, errNoUser)
		return db.Persona{}, false, false
	}
	if p.UserID != 0 && !v.owns(p) {
		set, err := ps.ProfileSettingOf(ctx, p.UserID)
		if err != nil {
			s.fail(w, r, err)
			return db.Persona{}, false, false
		}
		if set.HideMe {
			s.fail(w, r, errNoUser)
			return db.Persona{}, false, false
		}
	}
	return p, moved, true
}

// userPage is the profile at /u/<username>: the viewer's own with all of
// it, anyone else's as a stranger may see it.
func (s *Server) userPage(w http.ResponseWriter, r *http.Request) {
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p, moved, ok := s.userOf(w, r, v)
	if !ok {
		return
	}
	if moved {
		writeJSON(w, http.StatusOK, movedUser{Moved: p.Username})
		return
	}
	ctx := r.Context()
	ps := s.profileStore()
	if v.owns(p) {
		out, err := s.ownProfileOf(ctx, ps, v)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	now := s.Now()
	upTo := profile.LatestClosed(s.Zones, now)
	out := userProfile{}
	who, free := db.Who{UserID: p.UserID}, db.FreeWho{UserID: p.UserID}
	if p.UserID != 0 {
		name, err := ps.NameOf(ctx, p.UserID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out.Person = people.Telegram(p.UserID, name, false)
	} else {
		who, free = db.Who{Account: p.AccountID}, db.FreeWho{Account: p.AccountID}
		_, name, err := s.Store.FreeAccountPlayer(ctx, p.AccountID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out.Person = people.Person{Name: name, Site: true, Account: p.AccountID}
	}
	// No percentile: its line speaks to the player ("you are better than"),
	// and it reads every answer of every day they played.
	if out.Ranked, err = s.ranked(ctx, ps, rankedAsk{who: who, upTo: upTo, history: true}, v.lang); err != nil {
		s.fail(w, r, err)
		return
	}
	out.Free = s.free(ctx, ps, free)
	if err := s.dress(ctx, dressAsk{chatID: v.chatID, viewer: v.user, lang: v.lang, guest: v.guest()}, &out.Person); err != nil {
		s.fail(w, r, err)
		return
	}
	if v.chatID != 0 && v.user != 0 && p.UserID != 0 {
		if out.Duel, err = s.duel(ctx, ps, v, p.UserID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// duel is the head to head of the viewer with id over the days the group
// the profile is opened from has seen the result of, when id plays there.
func (s *Server) duel(ctx context.Context, ps ProfileStore, v profileViewer, id int64) (*headToHead, error) {
	in, err := ps.Present(ctx, v.chatID, []int64{id})
	if err != nil || !in[id] {
		return nil, err
	}
	// Today only once the group's own reveal has passed.
	now := s.Now()
	upTo := round.DateIn(now, v.zone)
	if now.Before(upTo.Reveal(v.zone)) {
		upTo = upTo.AddDays(-1)
	}
	closer, of, err := ps.HeadToHead(ctx, v.user, id, upTo)
	if err != nil || of == 0 {
		return nil, err
	}
	return &headToHead{Closer: closer, Of: of}, nil
}

// userCard is the picture of a link to /u/<username>: the profile's globe
// and numbers, without the photo. Only a Telegram player has one.
func (s *Server) userCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if s.Stills == nil {
		s.fail(w, r, errUnavailable)
		return
	}
	if !s.ipLimit.allowIP(clientIP(r)) {
		s.fail(w, r, errRateLimited)
		return
	}
	// The picture has one address. Cloudflare keys its copy by the query
	// too: any query is sent back to the address without one.
	if r.URL.RawQuery != "" {
		u, err := db.CleanUsername(r.PathValue("username"))
		if err != nil {
			s.fail(w, r, errNoUser)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.Redirect(w, r, "/api/u/"+u+"/card.png", http.StatusMovedPermanently)
		return
	}
	p, moved, ok := s.userOf(w, r, profileViewer{})
	if !ok {
		return
	}
	if moved {
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.Redirect(w, r, "/api/u/"+p.Username+"/card.png", http.StatusFound)
		return
	}
	if p.UserID == 0 {
		s.fail(w, r, errNoUser)
		return
	}
	s.stillOf(w, r, p.UserID)
}

// stillOf writes the person's picture for a link preview.
func (s *Server) stillOf(w http.ResponseWriter, r *http.Request, id int64) {
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
		if !s.cardAll.allowKey("") {
			s.fail(w, r, errRateLimited)
			return
		}
		png, err = s.Stills.Still(r.Context(), id, lang)
		if errors.Is(err, profile.ErrNothing) {
			s.fail(w, r, errNoUser)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}
	w.Header().Set("Content-Type", "image/png")
	// Short: a profile hidden or renamed takes its picture with it once the
	// edge's copy runs out.
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(png)
}

// usernameCheck says whether the viewer may take a username: as they type
// it in the editor.
func (s *Server) usernameCheck(w http.ResponseWriter, r *http.Request) {
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if v.guest() {
		s.fail(w, r, errUnauthorized)
		return
	}
	if !s.userLimit.allowKey("username:" + v.key()) {
		s.fail(w, r, errRateLimited)
		return
	}
	ctx := r.Context()
	p, ok, err := s.personaFor(ctx, v, v.name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.fail(w, r, errUnauthorized)
		return
	}
	out := struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}{}
	name := r.URL.Query().Get("name")
	if _, err := db.CleanUsername(name); err != nil {
		out.Error = errUsernameForm.code
	} else if free, err := s.personaStore().UsernameFree(ctx, p.ID, name); err != nil {
		s.fail(w, r, err)
		return
	} else if !free {
		out.Error = errUsernameUsed.code
	}
	out.OK = out.Error == ""
	writeJSON(w, http.StatusOK, out)
}

// key names the viewer for a limit.
func (v profileViewer) key() string {
	if v.user != 0 {
		return "u" + strconv.FormatInt(v.user, 10)
	}
	return "a" + v.account
}

// editBody is a change to the profile: each field left out stays.
type editBody struct {
	// Name is the person's own name; "" removes it and Telegram's shows.
	Name     *string `json:"name"`
	Username *string `json:"username"`
	// Asked records that the person answered the first question, whatever
	// they chose: they are not asked again.
	Asked bool `json:"asked"`
}

// editResult is the profile after a change: what the editor shows, and the
// person as every list now shows them.
type editResult struct {
	Edit   *profileEdit  `json:"edit"`
	Person people.Person `json:"person"`
}

// profileEditGet is the viewer's profile as the editor shows it: the
// site asks for it once an account without Telegram signs in, to ask for a
// name, a username and a photo the first time.
func (s *Server) profileEditGet(w http.ResponseWriter, r *http.Request) {
	if s.personaStore() == nil || s.profileStore() == nil {
		s.fail(w, r, errUnavailable)
		return
	}
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if v.guest() {
		s.fail(w, r, errUnauthorized)
		return
	}
	s.writeEdited(w, r, v)
}

// profileEditPost changes the viewer's own name, username or both.
func (s *Server) profileEditPost(w http.ResponseWriter, r *http.Request) {
	v, ok := s.editor(w, r)
	if !ok {
		return
	}
	var body editBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		s.fail(w, r, errBadRequest)
		return
	}
	ctx := r.Context()
	p, ok, err := s.personaFor(ctx, v, v.name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.fail(w, r, errUnauthorized)
		return
	}
	st := s.personaStore()
	if body.Username != nil {
		switch err := st.SetUsername(ctx, p.ID, *body.Username); {
		case errors.Is(err, db.ErrUsername):
			s.fail(w, r, errUsernameForm)
			return
		case errors.Is(err, db.ErrUsernameTaken):
			s.fail(w, r, errUsernameUsed)
			return
		case err != nil:
			s.fail(w, r, err)
			return
		}
	}
	if body.Name != nil {
		if err := st.SetOwnName(ctx, p.ID, *body.Name); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if body.Asked {
		if err := st.MarkAsked(ctx, p.ID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.writeEdited(w, r, v)
}

// photoBody is a new photo of one's own, base64 of a JPEG, PNG or WebP the
// app has cut square, or Remove to show Telegram's again.
type photoBody struct {
	Photo  string `json:"photo"`
	Remove bool   `json:"remove"`
}

// photoLimit bounds a photo sent: the app sends one of a few hundred
// kilobytes; base64 adds a third.
const photoLimit = 6 << 20

// profilePhoto sets or removes the viewer's own photo.
func (s *Server) profilePhoto(w http.ResponseWriter, r *http.Request) {
	v, ok := s.editor(w, r)
	if !ok {
		return
	}
	var body photoBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, photoLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Remove == (body.Photo != "") {
		s.fail(w, r, errBadRequest)
		return
	}
	var jpeg []byte
	if !body.Remove {
		raw, err := base64.StdEncoding.DecodeString(body.Photo)
		if err != nil {
			s.fail(w, r, errPhoto)
			return
		}
		if jpeg, err = avatar.Own(raw); err != nil {
			s.fail(w, r, errPhoto)
			return
		}
	}
	ctx := r.Context()
	p, ok, err := s.personaFor(ctx, v, v.name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.fail(w, r, errUnauthorized)
		return
	}
	if _, err := s.personaStore().SetOwnPhoto(ctx, p.ID, jpeg); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeEdited(w, r, v)
}

// editor is the viewer who changes their own profile: signed in, within
// the limit on changes, and from maply's own page when a cookie signs them.
func (s *Server) editor(w http.ResponseWriter, r *http.Request) (profileViewer, bool) {
	if s.personaStore() == nil || s.profileStore() == nil {
		s.fail(w, r, errUnavailable)
		return profileViewer{}, false
	}
	if r.Header.Get("Authorization") == "" && !ownJSON(r) {
		s.fail(w, r, apiError{http.StatusUnsupportedMediaType, "json_only"})
		return profileViewer{}, false
	}
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return profileViewer{}, false
	}
	if v.guest() {
		s.fail(w, r, errUnauthorized)
		return profileViewer{}, false
	}
	if !s.makeLimit.allowKey("profile:" + v.key()) {
		s.fail(w, r, errRateLimited)
		return profileViewer{}, false
	}
	return v, true
}

// writeEdited answers a change with the profile as it is now.
func (s *Server) writeEdited(w http.ResponseWriter, r *http.Request, v profileViewer) {
	ctx := r.Context()
	name := v.name
	var person people.Person
	switch {
	case v.user != 0:
		if ps := s.profileStore(); ps != nil {
			n, err := ps.NameOf(ctx, v.user)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			if n != "" {
				name = n
			}
		}
		person = people.Telegram(v.user, name, true)
	default:
		_, n, err := s.Store.FreeAccountPlayer(ctx, v.account)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		person = people.Person{Name: n, Site: true, Me: true, Account: v.account}
	}
	edit, err := s.editOf(ctx, v, name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.dress(ctx, dressAsk{viewer: v.user, lang: v.lang}, &person); err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, editResult{Edit: edit, Person: person})
}
