// SPDX-License-Identifier: Apache-2.0
package api

// The archive and the challenges of the free game. A site guest takes days
// from the archive as the browser's player; a challenge is for those who
// keep records, a Telegram person or an account, as a party is.

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/telegram"
)

func (s *Server) archiveList(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, false)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	days, err := s.Free.ArchiveOf(r.Context(), v.who)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days})
}

func (s *Server) archiveTake(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.makeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	var b struct {
		N int `json:"n"`
	}
	if err := readBody(w, r, &b); err != nil {
		s.freeFail(w, r, err)
		return
	}
	if err := s.met(r.Context(), v); err != nil {
		s.freeFail(w, r, err)
		return
	}
	g, err := s.Free.NewArchive(r.Context(), v.who, v.user.FirstName, b.N, v.lang)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) challengeMake(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.makeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	id, ok := gameID(r)
	if !ok {
		s.freeFail(w, r, free.ErrNotFound)
		return
	}
	code, err := s.Free.MakeChallenge(r.Context(), v.who, id)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

func (s *Server) challengeView(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.freeLimit, false)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	c, err := s.Free.ChallengeOf(r.Context(), v.who, r.PathValue("code"))
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) challengeTake(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.makeLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	if !v.keeps() {
		s.freeFail(w, r, free.ErrSignIn)
		return
	}
	if err := s.met(r.Context(), v); err != nil {
		s.freeFail(w, r, err)
		return
	}
	g, err := s.Free.TakeChallenge(r.Context(), v.who, v.user.FirstName, r.PathValue("code"), v.lang)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// challengeInvite prepares the message someone in a challenge sends on: the
// settings, the best score so far and a button that opens it. Without
// Invites the app shares the link, and the site its own, /play?c=<code>.
func (s *Server) challengeInvite(w http.ResponseWriter, r *http.Request) {
	v, err := s.freeViewer(w, r, s.inviteLimit, true)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	code := r.PathValue("code")
	c, err := s.Free.ChallengeOf(r.Context(), v.who, code)
	if err != nil {
		s.freeFail(w, r, err)
		return
	}
	if c.Mine == nil {
		s.freeFail(w, r, free.ErrNotMember)
		return
	}
	if v.site {
		writeJSON(w, http.StatusOK, map[string]string{"link": s.siteLink("/play?c=" + code)})
		return
	}
	link := "https://t.me/" + s.BotUsername + "/play?startapp=" + free.ChallengePrefix + code
	if s.Invites == nil {
		writeJSON(w, http.StatusOK, map[string]string{"link": link})
		return
	}
	best := 0
	for _, l := range c.Lines {
		if l.Status == free.StatusDone {
			best = max(best, l.Points)
		}
	}
	caption := InviteCaption(v.lang, c.Settings)
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	msg, err := s.Invites.SavePreparedText(r.Context(), v.user.ID, telegram.PreparedText{
		ID: base64.RawURLEncoding.EncodeToString(raw[:]), Title: i18n.T(v.lang, "free.challenge.title"), Description: caption,
		Text: "<b>maply · " + caption + "</b>\n" + i18n.T(v.lang, "free.challenge.text",
			"name", htmlEscape(firstName(v.user)), "points", i18n.FormatNumber(v.lang, best)),
		Markup: telegram.Markup{{{Text: i18n.T(v.lang, "free.challenge.join"), URL: link}}},
	})
	if err != nil {
		s.Log.Warn("challenge invitation not prepared", "error", err)
		writeJSON(w, http.StatusOK, map[string]string{"link": link})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": msg.ID, "link": link})
}
