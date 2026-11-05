// SPDX-License-Identifier: Apache-2.0
package account

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/oidc"
)

// The result codes of a sign-in, in ?auth= on the page it comes back to.
const (
	codeOK            = "ok"
	codeLinked        = "linked"
	codeMerged        = "merged"
	codeCancelled     = "cancelled"
	codeExpired       = "expired"
	codeFailed        = "failed"
	codeTaken         = "taken"
	codeAlreadyLinked = "already_linked"
	codeUnavailable   = "unavailable"
	codeRateLimited   = "rate_limited"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

// ownJSON reports whether a write that rides on a cookie came from maply's
// own pages: JSON, which a page elsewhere cannot send without a CORS
// preflight maply never grants, and not marked cross-site by the browser.
func ownJSON(r *http.Request) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		return false
	}
	return r.Header.Get("Sec-Fetch-Site") != "cross-site"
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	dec.DisallowUnknownFields()
	return dec.Decode(v) == nil
}

func (s *Service) redirect(w http.ResponseWriter, r *http.Request, to string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// result logs how a sign-in ended, by its code alone, and sends the browser
// back with it.
func (s *Service) result(w http.ResponseWriter, r *http.Request, f flow, code string, err error) {
	clearCookie(w, FlowCookie)
	if err != nil {
		s.log.Warn("sign-in", "provider", f.Provider, "intent", f.Intent, "result", code, "error", errClass(err))
	} else {
		s.log.Info("sign-in", "provider", f.Provider, "intent", f.Intent, "result", code)
	}
	s.redirect(w, r, withCode(f.ReturnTo, code))
}

func (s *Service) viewerOf(r *http.Request) Viewer {
	v, _ := ViewerFrom(r.Context())
	return v
}

// begin makes a sign-in's flow and the provider's address for it.
func (s *Service) begin(ctx context.Context, p Provider, f flow) (string, flow, error) {
	f.State, f.Nonce, f.Verifier, f.At = newToken(), newToken(), oidc.NewVerifier(), s.now().Unix()
	to, err := p.AuthURL(ctx, f.State, f.Nonce, f.Verifier)
	return to, f, err
}

// start signs the browser in: to the provider, and back to callback.
func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	back := safeReturnTo(r.URL.Query().Get("return_to"))
	f := flow{Provider: name, Intent: intentLogin, ReturnTo: back}
	if !s.authLimit.allow(clientIP(r)) {
		s.result(w, r, f, codeRateLimited, nil)
		return
	}
	p := s.provider(name)
	if p == nil {
		s.result(w, r, f, codeUnavailable, nil)
		return
	}
	// The flow names the session the browser has now, so a callback that
	// comes into another one, after a sign-out or in someone else's
	// browser, is refused.
	if h, ok := sessionHash(r); ok {
		f.Session = encodeHash(h)
	}
	to, f, err := s.begin(r.Context(), p, f)
	if err != nil {
		s.result(w, r, f, codeUnavailable, err)
		return
	}
	if err := s.setFlow(w, f); err != nil {
		s.result(w, r, f, codeFailed, err)
		return
	}
	s.redirect(w, r, to)
}

// link starts linking a second sign-in to the session's account. It is a
// post from maply's page, never a link: a link can be sent to someone.
func (s *Service) link(w http.ResponseWriter, r *http.Request) {
	if !ownJSON(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	var body struct {
		ReturnTo string `json:"return_to"`
	}
	if !decode(w, r, &body) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	v := s.viewerOf(r)
	if !v.SignedIn() {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !s.acctLimit.allow(v.AccountID) {
		writeError(w, http.StatusTooManyRequests, codeRateLimited)
		return
	}
	name := r.PathValue("provider")
	p := s.provider(name)
	if p == nil {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable)
		return
	}
	if (name == db.KindTelegram && v.TelegramUserID != 0) || (name == db.KindGoogle && v.google) {
		writeError(w, http.StatusConflict, codeAlreadyLinked)
		return
	}
	f := flow{Provider: name, Intent: intentLink, ReturnTo: safeReturnTo(body.ReturnTo), Account: v.AccountID, Session: encodeHash(v.hash)}
	to, f, err := s.begin(r.Context(), p, f)
	if err != nil {
		s.log.Warn("link", "provider", name, "result", codeUnavailable, "error", errClass(err))
		writeError(w, http.StatusServiceUnavailable, codeUnavailable)
		return
	}
	if err := s.setFlow(w, f); err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": to})
}

// callback is where the provider sends the browser back.
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	f, err := s.readFlow(r)
	if err != nil || f.Provider != name {
		s.result(w, r, flow{Provider: name, ReturnTo: f.ReturnTo}, codeExpired, nil)
		return
	}
	if !s.authLimit.allow(clientIP(r)) {
		s.result(w, r, f, codeRateLimited, nil)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		code := codeFailed
		if e == "access_denied" {
			code = codeCancelled
		}
		s.result(w, r, f, code, nil)
		return
	}
	// The state proves the browser that comes back is the one that left;
	// the session, that it is still the same person in it.
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(f.State)) != 1 {
		s.result(w, r, f, codeFailed, errors.New("state does not match"))
		return
	}
	if !f.Ticket {
		h, ok := sessionHash(r)
		if (f.Session == "") == ok || ok && !sameHash(h, f.Session) {
			s.result(w, r, f, codeExpired, errors.New("the session changed during the sign-in"))
			return
		}
	}
	p := s.provider(name)
	if p == nil {
		s.result(w, r, f, codeUnavailable, nil)
		return
	}
	ctx := r.Context()
	tok, err := p.Exchange(ctx, q.Get("code"), f.Verifier, f.Nonce)
	if err != nil {
		code := codeFailed
		var oe *oidc.Error
		if errors.As(err, &oe) && oe.Code == oidc.CodeUnavailable {
			code = codeUnavailable
		}
		s.result(w, r, f, code, err)
		return
	}
	id, err := s.identity(ctx, name, tok)
	if err != nil {
		s.result(w, r, f, codeFailed, err)
		return
	}
	if f.Intent == intentLink {
		s.finishLink(w, r, f, id)
		return
	}
	s.finishLogin(w, r, f, id)
}

// identity is the sign-in a verified token names. A Telegram one is put in
// core first: the account refers to core.person.
func (s *Service) identity(ctx context.Context, provider string, tok *oidc.Token) (db.Identity, error) {
	if provider == db.KindGoogle {
		sub := strings.TrimSpace(tok.Subject)
		if sub == "" || len(sub) > 255 {
			return db.Identity{}, errors.New("the id_token has no usable sub")
		}
		return db.Identity{Kind: db.KindGoogle, GoogleSub: sub}, nil
	}
	var c telegramClaims
	if err := tok.Claims(&c); err != nil {
		return db.Identity{}, err
	}
	u, err := c.user(false)
	if err != nil {
		return db.Identity{}, err
	}
	known, err := s.cfg.Store.PersonKnown(ctx, u.ID)
	if err != nil {
		return db.Identity{}, err
	}
	if u, err = c.user(known); err != nil {
		return db.Identity{}, err
	}
	if err := s.cfg.Store.TouchPerson(ctx, u); err != nil {
		return db.Identity{}, err
	}
	return db.Identity{Kind: db.KindTelegram, TelegramUserID: u.ID}, nil
}

func (s *Service) finishLogin(w http.ResponseWriter, r *http.Request, f flow, id db.Identity) {
	ctx := r.Context()
	// The guest of this browser goes into a new account. An account that
	// was there already takes nobody in by itself: the page asks.
	guest := guestID(r)
	acc, created, err := s.cfg.Store.SignIn(ctx, id, guest, s.rules())
	if err != nil {
		s.result(w, r, f, codeFailed, err)
		return
	}
	if err := s.rotate(ctx, w, r, acc.ID, id.Kind); err != nil {
		s.result(w, r, f, codeFailed, err)
		return
	}
	if guest != "" {
		if n, ok, err := s.cfg.Store.GuestOf(ctx, guest); err != nil || created || !ok || n == 0 {
			clearCookie(w, GuestCookie)
		}
	}
	s.result(w, r, f, codeOK, nil)
}

func (s *Service) finishLink(w http.ResponseWriter, r *http.Request, f flow, id db.Identity) {
	ctx := r.Context()
	res, err := s.cfg.Store.Link(ctx, f.Account, id, s.rules())
	if errors.Is(err, db.ErrNoAccount) {
		s.result(w, r, f, codeExpired, err)
		return
	}
	if err != nil {
		s.result(w, r, f, codeFailed, err)
		return
	}
	switch res {
	case db.Taken:
		s.result(w, r, f, codeTaken, nil)
		return
	case db.AlreadyLinked:
		s.result(w, r, f, codeAlreadyLinked, nil)
		return
	case db.Merged:
		// The merged account's sessions went with it; whatever of them the
		// cache holds goes too.
		s.forgetAll()
	default:
		s.forgetAccount(f.Account)
	}
	// A new session for the browser that proved the sign-in. A ticket's
	// browser proved only the new one.
	via := id.Kind
	if v := s.viewerOf(r); !f.Ticket && v.AccountID == f.Account && v.Via != "" {
		via = v.Via
	}
	if err := s.rotate(ctx, w, r, f.Account, via); err != nil {
		s.result(w, r, f, codeFailed, err)
		return
	}
	code := codeLinked
	if res == db.Merged {
		code = codeMerged
	}
	s.result(w, r, f, code, nil)
}

// rotate ends the browser's session, if it has one, and starts a new one
// for the account: the token after a sign-in or a link is always new.
func (s *Service) rotate(ctx context.Context, w http.ResponseWriter, r *http.Request, accountID, via string) error {
	if h, ok := sessionHash(r); ok {
		if err := s.cfg.Store.DeleteSession(ctx, h); err != nil {
			return err
		}
		s.forget(h)
	}
	s.sweep(ctx)
	now := s.now()
	token := newToken()
	expires := now.Add(SessionLife)
	if err := s.cfg.Store.CreateSession(ctx, db.Session{Hash: hashToken(token), AccountID: accountID, Via: via, Created: now, Expires: expires}); err != nil {
		return err
	}
	s.setSession(w, token, expires)
	return nil
}

// sweep drops expired sessions and tickets, at most once an hour.
func (s *Service) sweep(ctx context.Context) {
	s.sweepMu.Lock()
	now := s.now()
	due := now.Sub(s.sweepAt) >= sweepEvery
	if due {
		s.sweepAt = now
	}
	s.sweepMu.Unlock()
	if !due {
		return
	}
	if err := s.cfg.Store.DeleteExpired(ctx, now.Add(-sweepGrace)); err != nil {
		s.log.Warn("sweep sessions", "error", errClass(err))
	}
}

func (s *Service) forgetAll() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.gen++
	clear(s.cache)
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if !ownJSON(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if h, ok := sessionHash(r); ok {
		if err := s.cfg.Store.DeleteSession(r.Context(), h); err != nil {
			s.log.Error("sign out", "error", errClass(err))
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		s.forget(h)
	}
	clearCookie(w, SessionCookie)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) logoutAll(w http.ResponseWriter, r *http.Request) {
	if !ownJSON(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	v := s.viewerOf(r)
	if !v.SignedIn() {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if _, err := s.cfg.Store.DeleteSessions(r.Context(), v.AccountID, ""); err != nil {
		s.log.Error("sign out everywhere", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	s.forgetAccount(v.AccountID)
	clearCookie(w, SessionCookie)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// miniApp is the Telegram user of a Mini App request, verified.
func (s *Service) miniApp(r *http.Request) (auth.User, bool) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "tma ")
	if !ok || s.cfg.InitData == nil {
		return auth.User{}, false
	}
	data, err := s.cfg.InitData.Verify(raw)
	if err != nil {
		return auth.User{}, false
	}
	return data.User, true
}

// subject is the account a request acts on: the session's, or from the Mini
// App the one of its Telegram user. ok is false when there is none.
func (s *Service) subject(r *http.Request) (accountID string, fromApp bool, ok bool, err error) {
	if u, isApp := s.miniApp(r); isApp {
		acc, found, err := s.cfg.Store.AccountOf(r.Context(), db.Identity{Kind: db.KindTelegram, TelegramUserID: u.ID})
		return acc.ID, true, found, err
	}
	if r.Header.Get("Authorization") != "" {
		return "", false, false, nil
	}
	v := s.viewerOf(r)
	return v.AccountID, false, v.SignedIn(), nil
}

// newTicket is the Mini App's way to Google, which refuses to sign in
// inside Telegram: a ticket good once for five minutes, and the site's
// address that takes it in the phone's browser. The ticket travels in the
// fragment, which no server and no log sees.
func (s *Service) newTicket(w http.ResponseWriter, r *http.Request) {
	u, ok := s.miniApp(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !s.ticketLimit.allow(itoa(u.ID)) {
		writeError(w, http.StatusTooManyRequests, codeRateLimited)
		return
	}
	if s.cfg.Google == nil {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable)
		return
	}
	ctx := r.Context()
	// The person opened the Mini App, so the bot knows them; touch_person
	// keeps core right without counting this as a DM.
	if err := s.cfg.Store.TouchPerson(ctx, tg.User{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Username: u.Username, LanguageCode: u.LanguageCode, IsBot: u.IsBot}); err != nil {
		s.log.Error("ticket", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	acc, _, err := s.cfg.Store.SignIn(ctx, db.Identity{Kind: db.KindTelegram, TelegramUserID: u.ID}, "", s.rules())
	if err != nil {
		s.log.Error("ticket", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if acc.Has(db.KindGoogle) {
		writeError(w, http.StatusConflict, codeAlreadyLinked)
		return
	}
	now := s.now()
	token := newToken()
	if err := s.cfg.Store.CreateTicket(ctx, hashToken(token), acc.ID, now, now.Add(TicketLife)); err != nil {
		s.log.Error("ticket", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	s.log.Info("ticket", "result", codeOK)
	writeJSON(w, http.StatusOK, map[string]string{"url": strings.TrimRight(s.cfg.AppURL, "/") + "/#link=" + token})
}

type ticketBody struct {
	Ticket   string `json:"ticket"`
	ReturnTo string `json:"return_to,omitempty"`
}

func (s *Service) readTicket(w http.ResponseWriter, r *http.Request) (ticketBody, bool) {
	var body ticketBody
	if !ownJSON(r) || !decode(w, r, &body) || !tokenShape.MatchString(body.Ticket) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return body, false
	}
	if !s.authLimit.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, codeRateLimited)
		return body, false
	}
	return body, true
}

// peekTicket says whose Telegram account a ticket would link Google to, so
// the person sees it before going anywhere: a ticket someone sent them is
// not theirs.
func (s *Service) peekTicket(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readTicket(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	accID, found, err := s.cfg.Store.PeekTicket(ctx, hashToken(body.Ticket), s.now())
	if err != nil {
		s.log.Error("ticket", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if !found {
		writeError(w, http.StatusGone, codeExpired)
		return
	}
	v, found, err := s.cfg.Store.AccountView(ctx, accID)
	if err != nil || !found {
		writeError(w, http.StatusGone, codeExpired)
		return
	}
	if v.Has(db.KindGoogle) {
		writeError(w, http.StatusConflict, codeAlreadyLinked)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": v.TelegramName, "username": v.TelegramUsername, "google": s.cfg.Google != nil})
}

// confirmTicket uses the ticket and sends the browser to Google.
func (s *Service) confirmTicket(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readTicket(w, r)
	if !ok {
		return
	}
	if s.cfg.Google == nil {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable)
		return
	}
	ctx := r.Context()
	accID, found, err := s.cfg.Store.UseTicket(ctx, hashToken(body.Ticket), s.now())
	if err != nil {
		s.log.Error("ticket", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if !found {
		writeError(w, http.StatusGone, codeExpired)
		return
	}
	f := flow{Provider: db.KindGoogle, Intent: intentLink, ReturnTo: safeReturnTo(body.ReturnTo), Account: accID, Ticket: true}
	to, f, err := s.begin(ctx, s.cfg.Google, f)
	if err != nil {
		s.log.Warn("ticket", "result", codeUnavailable, "error", errClass(err))
		writeError(w, http.StatusServiceUnavailable, codeUnavailable)
		return
	}
	if err := s.setFlow(w, f); err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": to})
}

type meTelegram struct {
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
}

type meAccount struct {
	Telegram *meTelegram `json:"telegram"`
	Google   bool        `json:"google"`
	// Name is what the site's rows are called, empty for "Guest".
	Name string `json:"name,omitempty"`
	// Via is the sign-in this browser's session started with.
	Via string `json:"via,omitempty"`
}

type meResponse struct {
	// Available are the sign-ins this server has keys for.
	Available map[string]bool `json:"available"`
	Account   *meAccount      `json:"account"`
	// Guest is set for a signed-in browser whose guest has answers no
	// account has taken: the page asks whether to take them in.
	Guest *struct {
		Answers int `json:"answers"`
	} `json:"guest,omitempty"`
}

// me is the account of the browser or the Mini App, and nothing that is not
// the person's own: no account id, no Google number.
func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	accID, fromApp, ok, err := s.subject(r)
	if err != nil {
		s.log.Error("me", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	resp := meResponse{Available: s.Available()}
	if !ok {
		if r.Header.Get("Authorization") != "" && !fromApp {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	ctx := r.Context()
	v, found, err := s.cfg.Store.AccountView(ctx, accID)
	if err != nil {
		s.log.Error("me", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	a := &meAccount{Google: v.Has(db.KindGoogle), Name: v.Name}
	if v.Has(db.KindTelegram) {
		a.Telegram = &meTelegram{Name: v.TelegramName, Username: v.TelegramUsername}
	}
	if !fromApp {
		a.Via = s.viewerOf(r).Via
		if guest := guestID(r); guest != "" {
			if n, ok, err := s.cfg.Store.GuestOf(ctx, guest); err == nil && ok && n > 0 {
				resp.Guest = &struct {
					Answers int `json:"answers"`
				}{n}
			}
		}
	}
	resp.Account = a
	writeJSON(w, http.StatusOK, resp)
}

// adopt takes the browser's guest into the account, or leaves it, as the
// person said. Either way the browser forgets it.
func (s *Service) adopt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Take bool `json:"take"`
	}
	if !ownJSON(r) || !decode(w, r, &body) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	v := s.viewerOf(r)
	if !v.SignedIn() {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	n := 0
	if guest := guestID(r); body.Take && guest != "" {
		var err error
		if n, err = s.cfg.Store.AdoptGuest(r.Context(), v.AccountID, guest, s.rules()); err != nil {
			s.log.Error("adopt guest", "error", errClass(err))
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		s.forgetAccount(v.AccountID)
	}
	clearCookie(w, GuestCookie)
	writeJSON(w, http.StatusOK, map[string]int{"answers": n})
}

// setLang is the interface language core keeps for a Telegram account,
// shared with every bot of the family. "" follows Telegram again. An
// account without Telegram keeps its language in the browser.
func (s *Service) setLang(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lang string `json:"lang"`
	}
	if !ownJSON(r) || !decode(w, r, &body) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	v := s.viewerOf(r)
	if v.TelegramUserID == 0 {
		writeError(w, http.StatusConflict, "no_telegram")
		return
	}
	ctx := r.Context()
	var err error
	if body.Lang == "" {
		err = s.cfg.Store.ClearUserLang(ctx, v.TelegramUserID)
	} else if !supportedLang(body.Lang) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	} else {
		err = s.cfg.Store.SetUserLang(ctx, v.TelegramUserID, body.Lang)
	}
	if err != nil {
		s.log.Error("set language", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"lang": body.Lang})
}

// unlink removes one sign-in. The sessions it opened end; the browser that
// asked keeps a new one by the other sign-in.
func (s *Service) unlink(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" && !ownJSON(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	kind := r.PathValue("provider")
	if kind != db.KindTelegram && kind != db.KindGoogle {
		writeError(w, http.StatusNotFound, "not_linked")
		return
	}
	accID, fromApp, ok, err := s.subject(r)
	if err != nil {
		s.log.Error("unlink", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !s.acctLimit.allow(accID) {
		writeError(w, http.StatusTooManyRequests, codeRateLimited)
		return
	}
	ctx := r.Context()
	switch err := s.cfg.Store.Unlink(ctx, accID, kind); {
	case errors.Is(err, db.ErrLastIdentity):
		writeError(w, http.StatusConflict, "last_identity")
		return
	case errors.Is(err, db.ErrNotLinked), errors.Is(err, db.ErrNoAccount):
		writeError(w, http.StatusNotFound, "not_linked")
		return
	case err != nil:
		s.log.Error("unlink", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	// A sign-in linked by someone else is unlinked to shut them out, so the
	// sessions it opened end with it.
	if _, err := s.cfg.Store.DeleteSessions(ctx, accID, kind); err != nil {
		s.log.Error("unlink", "error", errClass(err))
	}
	s.forgetAccount(accID)
	if !fromApp {
		other := db.KindGoogle
		if kind == db.KindGoogle {
			other = db.KindTelegram
		}
		if err := s.rotate(ctx, w, r, accID, other); err != nil {
			s.log.Error("unlink", "error", errClass(err))
		}
	}
	s.log.Info("unlink", "provider", kind, "result", codeOK)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// deleteAccount erases the account and its sign-ins. Its site rows stay as
// "Guest", in the places they hold.
func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm string `json:"confirm"`
	}
	if !ownJSON(r) || !decode(w, r, &body) || body.Confirm != "delete" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	accID, fromApp, ok, err := s.subject(r)
	if err != nil {
		s.log.Error("delete account", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := s.cfg.Store.DeleteAccount(r.Context(), accID); err != nil && !errors.Is(err, db.ErrNoAccount) {
		s.log.Error("delete account", "error", errClass(err))
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	s.forgetAll()
	if !fromApp {
		clearCookie(w, SessionCookie)
		clearCookie(w, GuestCookie)
	}
	s.log.Info("delete account", "result", codeOK)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func supportedLang(code string) bool { return code == i18n.Normalize(code) && i18n.IsSupported(code) }
