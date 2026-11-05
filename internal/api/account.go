// SPDX-License-Identifier: Apache-2.0
package api

import (
	"net/http"
	"strings"

	"github.com/FreshLabDev/maply/internal/account"
	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
)

// Accounts on the site (internal/account). A browser signed in with an
// account that has Telegram plays as that Telegram player, through the same
// /api/today, /api/guess and /api/result as the Mini App, with the session
// cookie instead of initData: one answer a day across the bot and the site,
// the window of the person's DM with the speed bonus, the place right after
// the answer, the names at the usual time. It writes nothing to core: a
// sign-in on the site is not a DM with the bot, and must not put the person
// on the morning post or tie them to a chat.
//
// A guest and an account with Google alone play the site's game
// (site.go); an account keeps its history across browsers.

// withViewer puts the site's viewer in the context of the requests that
// read it. Accounts is read at request time: the stand sets it after the
// handler is built.
func (s *Server) withViewer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Accounts == nil || strings.HasPrefix(r.URL.Path, "/img/") || r.URL.Path == "/api/log" {
			next.ServeHTTP(w, r)
			return
		}
		s.Accounts.Middleware(next).ServeHTTP(w, r)
	})
}

// accounts serves /api/me and /api/account/*, or says there are no
// accounts on this server.
func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		if r.Method == http.MethodGet && r.URL.Path == "/api/me" {
			writeJSON(w, http.StatusOK, map[string]any{"available": map[string]bool{"telegram": false, "google": false}, "account": nil})
			return
		}
		s.fail(w, r, errUnavailable)
		return
	}
	s.Accounts.Handler().ServeHTTP(w, r)
}

// siteViewer is who asks from the site: the account and its player, or the
// browser's guest. Without accounts it is the guest cookie as before.
func (s *Server) siteViewer(r *http.Request) account.Viewer {
	if v, ok := account.ViewerFrom(r.Context()); ok {
		return v
	}
	return account.Viewer{Player: playerID(r)}
}

// resolveSite is resolveAs for a browser signed in with Telegram. It plays
// as the person's DM: the DM's zone and date, without touching core.
func (s *Server) resolveSite(r *http.Request, v account.Viewer) (play, error) {
	// The session is a cookie: another site's page must not answer for it.
	if r.Method != http.MethodGet && !ownJSON(r) {
		return play{}, errBadRequest
	}
	if !s.userLimit.allow(v.TelegramUserID) {
		return play{}, errRateLimited
	}
	ctx := r.Context()
	lang, err := s.Store.UserLang(ctx, v.TelegramUserID)
	if err != nil {
		return play{}, err
	}
	if lang == "" {
		lang = i18n.Normalize(r.URL.Query().Get("lang"))
	}
	zone, err := s.Store.ChatZone(ctx, v.TelegramUserID)
	if err != nil {
		return play{}, err
	}
	// As at the DM's first contact: the zone is fixed now, so a later change
	// of language cannot reopen a day the person has seen. It is a setting,
	// not presence: the morning post goes by presence.
	if zone == "" {
		zone = round.DefaultName(lang)
		if err := s.Store.FixZone(ctx, v.TelegramUserID, zone); err != nil {
			return play{}, err
		}
	}
	p := play{user: auth.User{ID: v.TelegramUserID}, kind: "site", lang: i18n.Resolve(lang)}
	p.shareLang = p.lang
	p.zone = s.Zones.For(zone, lang)
	p.date = round.DateIn(s.Now(), p.zone)
	return p, nil
}

// siteGuessOf is the site viewer's answer of d: an account's from any of
// its site players, a guest's from the cookie's.
func (s *Server) siteGuessOf(r *http.Request, d round.Date, v account.Viewer) (db.SiteGuess, bool, error) {
	if v.SignedIn() {
		return s.Store.AccountSiteGuessOf(r.Context(), d, v.AccountID)
	}
	if v.Player == "" {
		return db.SiteGuess{}, false, nil
	}
	return s.Store.SiteGuessOf(r.Context(), d, v.Player)
}

// sitePlayers are the site players whose rows are the viewer's own.
func (s *Server) sitePlayers(r *http.Request, v account.Viewer) (map[string]bool, error) {
	out := map[string]bool{}
	if v.SignedIn() {
		list, err := s.Store.AccountPlayers(r.Context(), v.AccountID)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			out[p] = true
		}
		return out, nil
	}
	if v.Player != "" {
		out[v.Player] = true
	}
	return out, nil
}
