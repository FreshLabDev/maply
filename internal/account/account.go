// SPDX-License-Identifier: Apache-2.0

// Package account signs people in on the site and keeps their accounts
// (docs/design.md, "Accounts on the site"). An account has at most one
// Telegram sign-in and one Google sign-in; both go through this server by
// OpenID Connect with PKCE (internal/oidc), with no script of theirs on the
// page.
//
//   - A session is 256 random bits in the `__Host-maply_session` cookie; the
//     database keeps only their SHA-256. It lasts 90 days from the sign-in and
//     is not extended. Every sign-in and every link starts a new one.
//   - What a sign-in needs between the two legs (provider, state, nonce, PKCE
//     verifier, where to go back, and for a link the account and the session
//     it started in) is in the `__Host-maply_oauth` cookie for ten minutes,
//     signed: it names an account, and a forged one would link a stranger's
//     Google to it.
//   - A Mini App cannot sign in with Google, which refuses embedded browsers:
//     it asks for a five-minute ticket and opens the site in the phone's
//     browser, which shows whose Telegram account the Google will join
//     before it goes there.
//
// The log gets the result of a sign-in and nothing else: no Telegram id, no
// Google number, no address, no token.
//
// Middleware puts the site's viewer in each request's context: the account,
// its Telegram user id and its site player, or the guest of the browser.
// Other packages read it with ViewerFrom.
package account

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/oidc"
	"github.com/FreshLabDev/maply/internal/round"
)

const (
	// SessionCookie is a signed-in browser's. The __Host- prefix makes the
	// browser refuse it unless it is Secure, has Path=/ and no Domain.
	SessionCookie = "__Host-maply_session"
	// SessionLife is how long a session lasts from the sign-in. A daily game
	// is opened daily, and Telegram asks the phone at every sign-in.
	SessionLife = 90 * 24 * time.Hour
	// GuestCookie is the site player of a browser without an account; the
	// api package sets it on the first answer.
	GuestCookie = "maply_player"
	// TicketLife is how long the Mini App's ticket to Google lasts.
	TicketLife = 5 * time.Minute

	cacheTTL   = 30 * time.Second
	maxCached  = 10_000
	sweepEvery = time.Hour
	sweepGrace = 24 * time.Hour
)

// Store is what the accounts are kept in; *db.Store is one.
type Store interface {
	SignIn(ctx context.Context, id db.Identity, guest string, rules db.Rules) (db.Account, bool, error)
	Link(ctx context.Context, accountID string, id db.Identity, rules db.Rules) (db.LinkResult, error)
	LinkOnly(ctx context.Context, accountID string, id db.Identity, rules db.Rules) (db.LinkResult, error)
	Unlink(ctx context.Context, accountID, kind string) error
	AdoptGuest(ctx context.Context, accountID, guest string, rules db.Rules) (int, error)
	DeleteAccount(ctx context.Context, accountID, guest string) error
	AccountOf(ctx context.Context, id db.Identity) (db.Account, bool, error)
	AccountView(ctx context.Context, accountID string) (db.AccountView, bool, error)
	GuestOf(ctx context.Context, player string) (int, bool, error)
	PlayerOwned(ctx context.Context, player string) (bool, error)

	CreateSession(ctx context.Context, s db.Session) error
	LookupSession(ctx context.Context, hash [32]byte, now time.Time) (db.SessionAccount, bool, error)
	DeleteSession(ctx context.Context, hash [32]byte) error
	DeleteSessions(ctx context.Context, accountID, via string) (int64, error)
	DeleteExpired(ctx context.Context, before time.Time) error

	CreateTicket(ctx context.Context, hash [32]byte, accountID string, now, expires time.Time) error
	PeekTicket(ctx context.Context, hash [32]byte, now time.Time) (string, bool, error)
	UseTicket(ctx context.Context, hash [32]byte, now time.Time) (string, bool, error)
	TicketHeld(ctx context.Context, hash [32]byte, accountID string) (bool, error)
	DeleteTickets(ctx context.Context, accountID string) error

	// TouchPerson records a Telegram sign-in in core without a chat: Touch
	// would make it a DM with the bot, with the morning post and a new
	// answer window.
	TouchPerson(ctx context.Context, user tg.User) error
	PersonKnown(ctx context.Context, userID int64) (bool, error)
	ChatZone(ctx context.Context, chatID int64) (string, error)
	UserLang(ctx context.Context, userID int64) (string, error)
	SetUserLang(ctx context.Context, userID int64, lang string) error
	ClearUserLang(ctx context.Context, userID int64) error
	Deadline(ctx context.Context, zones round.Zones, userID int64, d round.Date, own *time.Location) (time.Time, error)
}

// Provider is one sign-in provider; *oidc.Provider is one.
type Provider interface {
	AuthURL(ctx context.Context, state, nonce, verifier string) (string, error)
	Exchange(ctx context.Context, code, verifier, nonce string) (*oidc.Token, error)
}

// Config is what the service needs. A provider left nil is a sign-in that
// is not configured: it answers "unavailable" and the rest works.
type Config struct {
	Store    Store
	Telegram Provider
	Google   Provider
	// Key signs the sign-in cookie.
	Key []byte
	// InitData checks the Mini App's requests.
	InitData *auth.Verifier
	Zones    round.Zones
	// AppURL is the site's public origin, where the Mini App's ticket opens.
	AppURL string
	Log    *slog.Logger
	Now    func() time.Time
}

// Service is the accounts. Its methods are safe for concurrent use.
type Service struct {
	cfg Config
	log *slog.Logger
	now func() time.Time

	// cache holds sessions found lately, by token hash. gen counts the
	// changes: a lookup that began before one is not cached.
	cacheMu sync.Mutex
	cache   map[[32]byte]cached
	gen     uint64

	sweepMu sync.Mutex
	sweepAt time.Time

	handlerOnce sync.Once
	handler     http.Handler

	authLimit   *limiter
	ticketLimit *limiter
	acctLimit   *limiter
}

type cached struct {
	s     db.SessionAccount
	until time.Time
}

// New builds the service. It makes no network call.
func New(cfg Config) *Service {
	s := &Service{cfg: cfg, log: cfg.Log, now: cfg.Now, cache: map[[32]byte]cached{}}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.authLimit = newLimiter(20, time.Minute, s.now)
	s.ticketLimit = newLimiter(5, time.Minute, s.now)
	s.acctLimit = newLimiter(10, time.Minute, s.now)
	return s
}

// SetTelegram and SetGoogle turn a sign-in on. They are for the start, before
// the service serves anything.
func (s *Service) SetTelegram(p Provider) { s.cfg.Telegram = p }

func (s *Service) SetGoogle(p Provider) { s.cfg.Google = p }

func (s *Service) provider(name string) Provider {
	switch name {
	case db.KindTelegram:
		if s.cfg.Telegram != nil {
			return s.cfg.Telegram
		}
	case db.KindGoogle:
		if s.cfg.Google != nil {
			return s.cfg.Google
		}
	}
	return nil
}

// Available reports which sign-ins are configured.
func (s *Service) Available() map[string]bool {
	return map[string]bool{db.KindTelegram: s.cfg.Telegram != nil, db.KindGoogle: s.cfg.Google != nil}
}

// Handler serves the sign-in and the account:
//
//	GET    /auth/{provider}/start?return_to=   sign in
//	POST   /auth/{provider}/link               {url} to link to the session's account
//	GET    /auth/{provider}/callback           the provider's redirect back
//	POST   /auth/logout, /auth/logout/all      out here, or everywhere
//	POST   /auth/ticket, /auth/ticket/confirm  the Mini App's ticket: whose, then use it
//	GET    /api/me                             the account and the browser's guest
//	POST   /api/account/adopt                  take the browser's guest in, or not
//	POST   /api/account/ticket                 a ticket, from the Mini App
//	POST   /api/account/lang                   the language core keeps for the person
//	DELETE /api/account/identity/{provider}    unlink
//	DELETE /api/account                        delete
//
// Mount it behind Middleware.
func (s *Service) Handler() http.Handler {
	s.handlerOnce.Do(func() { s.handler = s.routes() })
	return s.handler
}

func (s *Service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/{provider}/start", s.start)
	mux.HandleFunc("POST /auth/{provider}/link", s.link)
	mux.HandleFunc("GET /auth/{provider}/callback", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("POST /auth/logout/all", s.logoutAll)
	mux.HandleFunc("POST /auth/ticket", s.peekTicket)
	mux.HandleFunc("POST /auth/ticket/confirm", s.confirmTicket)
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("POST /api/account/adopt", s.adopt)
	mux.HandleFunc("POST /api/account/ticket", s.newTicket)
	mux.HandleFunc("POST /api/account/lang", s.setLang)
	mux.HandleFunc("DELETE /api/account/identity/{provider}", s.unlink)
	mux.HandleFunc("DELETE /api/account", s.deleteAccount)
	return mux
}

// Viewer is who asks from the site. A guest has no AccountID; Player is
// their browser's site player, empty before the first answer.
type Viewer struct {
	// AccountID is the signed-in account, empty for a guest.
	AccountID string
	// TelegramUserID is the account's Telegram, 0 without one. With it the
	// site plays as the bot's player.
	TelegramUserID int64
	// Player is the site player new site answers go to: the account's main
	// one (empty until its first site answer), or the guest's.
	Player string
	// Via is the sign-in the session started with.
	Via string
	// hash is the session's, for a link started from it; google says the
	// account has Google.
	hash   [32]byte
	google bool
}

// SignedIn reports whether the viewer has an account.
func (v Viewer) SignedIn() bool { return v.AccountID != "" }

type viewerKey struct{}

// ViewerFrom is the site viewer Middleware put in the context. ok is false
// for a request that did not come through it. A request that signs for
// itself, as the Mini App's do with initData, is nobody on the site.
func ViewerFrom(ctx context.Context) (Viewer, bool) {
	v, ok := ctx.Value(viewerKey{}).(Viewer)
	return v, ok
}

// WithViewer is a context carrying v, for tests of the packages that read it.
func WithViewer(ctx context.Context, v Viewer) context.Context {
	return context.WithValue(ctx, viewerKey{}, v)
}

// Middleware finds the site viewer of the request and puts it in its
// context. A guest cookie that names a player an account has taken in opens
// nothing: that browser is a new guest.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			// Signed for itself, the request is neither the session's
			// account nor the guest cookie's player. Left without a viewer,
			// a handler would take the cookie's player as it is, even one an
			// account has taken in, by any Authorization at all.
			next.ServeHTTP(w, r.WithContext(WithViewer(r.Context(), Viewer{})))
			return
		}
		v, err := s.viewer(r)
		if err != nil {
			// Taken for a guest, a Telegram player would answer again as
			// one: the request waits for the database instead.
			s.log.Error("look up a session", "error", errClass(err))
			writeError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithViewer(r.Context(), v)))
	})
}

func (s *Service) viewer(r *http.Request) (Viewer, error) {
	if hash, ok := sessionHash(r); ok {
		sa, found, err := s.lookup(r.Context(), hash)
		if err != nil {
			return Viewer{}, err
		}
		if found {
			return Viewer{AccountID: sa.AccountID, TelegramUserID: sa.Account.TelegramUserID, Player: sa.Account.Player,
				Via: sa.Via, hash: hash, google: sa.Account.Has(db.KindGoogle)}, nil
		}
	}
	guest := guestID(r)
	if guest == "" {
		return Viewer{}, nil
	}
	owned, err := s.cfg.Store.PlayerOwned(r.Context(), guest)
	if err != nil {
		return Viewer{}, err
	}
	if owned {
		return Viewer{}, nil
	}
	return Viewer{Player: guest}, nil
}

func (s *Service) lookup(ctx context.Context, hash [32]byte) (db.SessionAccount, bool, error) {
	now := s.now()
	s.cacheMu.Lock()
	if e, ok := s.cache[hash]; ok {
		if now.Before(e.until) {
			s.cacheMu.Unlock()
			return e.s, true, nil
		}
		delete(s.cache, hash)
	}
	gen := s.gen
	s.cacheMu.Unlock()
	sa, found, err := s.cfg.Store.LookupSession(ctx, hash, now)
	if err != nil || !found {
		return db.SessionAccount{}, false, err
	}
	s.cacheMu.Lock()
	if s.gen == gen {
		if len(s.cache) >= maxCached {
			clear(s.cache)
		}
		until := now.Add(cacheTTL)
		if sa.Expires.Before(until) {
			until = sa.Expires
		}
		s.cache[hash] = cached{s: sa, until: until}
	}
	s.cacheMu.Unlock()
	return sa, true, nil
}

// forget drops one session from the cache.
func (s *Service) forget(hash [32]byte) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.gen++
	delete(s.cache, hash)
}

// forgetAccount drops the sessions of accounts that changed: a link, an
// unlink, a merge or a deletion is seen by the next request.
func (s *Service) forgetAccount(ids ...string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.gen++
	for h, e := range s.cache {
		for _, id := range ids {
			if e.s.AccountID == id {
				delete(s.cache, h)
			}
		}
	}
}

// rules are how answers settle when an account takes in Telegram or a guest.
func (s *Service) rules() db.Rules {
	now := s.now()
	return db.Rules{
		Now:  now,
		Open: func(d round.Date) bool { return now.Before(s.cfg.Zones.LastReveal(d)) },
		Deadline: func(ctx context.Context, userID int64, d round.Date) (time.Time, error) {
			zone, err := s.DMZone(ctx, userID)
			if err != nil {
				return time.Time{}, err
			}
			return s.cfg.Store.Deadline(ctx, s.cfg.Zones, userID, d, zone)
		},
	}
}

// DMZone is the zone of the person's DM, the one the site plays in for them:
// theirs when it is set, else their language's.
func (s *Service) DMZone(ctx context.Context, userID int64) (*time.Location, error) {
	zone, err := s.cfg.Store.ChatZone(ctx, userID)
	if err != nil {
		return nil, err
	}
	lang, err := s.cfg.Store.UserLang(ctx, userID)
	if err != nil {
		return nil, err
	}
	if zone == "" {
		zone = round.DefaultName(i18n.Normalize(lang))
	}
	return s.cfg.Zones.For(zone, lang), nil
}

// errClass is what of an error may go to the log: an oidc step's code, a
// database error's SQLSTATE, never a value. A Postgres detail can carry a
// Telegram id.
func errClass(err error) string {
	var oe *oidc.Error
	var pe *pgconn.PgError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &oe):
		return "oidc " + oe.Code
	case errors.As(err, &pe):
		return "sqlstate " + pe.Code
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "context"
	case errors.Is(err, db.ErrNoAccount):
		return "no account"
	}
	return fmt.Sprintf("%T", err)
}
