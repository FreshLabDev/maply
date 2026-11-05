// SPDX-License-Identifier: Apache-2.0
//go:build integration

// The sign-in end to end: a browser with a cookie jar, the real store, and
// fake Telegram and Google providers.
//
//	MAPLY_TEST_DATABASE_URL=postgres://… go test -tags=integration ./internal/account/
package account_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/maply/internal/account"
	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/oidc/oidctest"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/migrations"
)

const botToken = "123456:account-test"

type stand struct {
	t        *testing.T
	store    *db.Store
	pool     *pgxpool.Pool
	svc      *account.Service
	srv      *httptest.Server
	telegram *oidctest.Provider
	google   *oidctest.Provider
	logs     *lockedBuffer
	clock    time.Time
	clockMu  sync.Mutex
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (s *stand) now() time.Time {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	return s.clock
}

func (s *stand) move(d time.Duration) {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	s.clock = s.clock.Add(d)
}

func setup(t *testing.T) *stand {
	t.Helper()
	rawURL := os.Getenv("MAPLY_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set MAPLY_TEST_DATABASE_URL to run the sign-in tests")
	}
	if u, err := url.Parse(rawURL); err != nil || !strings.Contains(strings.TrimPrefix(u.Path, "/"), "test") {
		t.Fatal("MAPLY_TEST_DATABASE_URL must name a disposable database containing \"test\"")
	}
	ctx := context.Background()
	store, err := db.Connect(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	pool, err := pgxpool.New(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	boot, err := os.ReadFile(filepath.Join("..", "..", "deploy", "core-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(boot)); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`TRUNCATE maply.session, maply.link_ticket, maply.account, maply.job, maply.chat_round, maply.guess, maply.site_guess, maply.site_player, maply.chat_settings, maply.round, maply.place, maply.day_closed CASCADE`,
		`TRUNCATE core.person, core.chat CASCADE`,
		`TRUNCATE core.language_observation`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	s := &stand{t: t, store: store, pool: pool, logs: &lockedBuffer{}, clock: time.Now()}
	var h http.Handler
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(s.srv.Close)
	s.telegram = oidctest.Start(t, "7000001", "tg-secret-DO-NOT-LOG", true)
	s.google = oidctest.Start(t, "g-client", "g-secret-DO-NOT-LOG", false)
	s.svc = account.New(account.Config{
		Store: store, Key: []byte("flow-key"), InitData: auth.NewVerifier(botToken), Zones: zones, AppURL: s.srv.URL,
		Log: slog.New(slog.NewTextHandler(s.logs, &slog.HandlerOptions{Level: slog.LevelDebug})), Now: s.now,
	})
	s.svc.SetTelegram(account.Telegram(s.telegram.Issuer, s.srv.URL, "7000001", "tg-secret-DO-NOT-LOG"))
	s.svc.SetGoogle(account.Google(s.google.Issuer, s.srv.URL, "g-client", "g-secret-DO-NOT-LOG"))
	mux := http.NewServeMux()
	mux.Handle("/auth/", s.svc.Middleware(s.svc.Handler()))
	mux.Handle("/api/", s.svc.Middleware(s.svc.Handler()))
	mux.Handle("/privacy", s.svc.Privacy())
	// The game's own pages stand in as a page that says who it sees.
	mux.Handle("/", s.svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := account.ViewerFrom(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{"account": v.AccountID, "telegram": v.TelegramUserID, "player": v.Player})
	})))
	h = mux
	return s
}

type browser struct {
	s   *stand
	c   *http.Client
	jar *cookiejar.Jar
}

func (s *stand) browser() *browser {
	jar, _ := cookiejar.New(nil)
	// A copy: srv.Client() is one client, and every browser has its own jar.
	c := *s.srv.Client()
	c.Jar = jar
	return &browser{s: s, c: &c, jar: jar}
}

func (b *browser) cookie(name string) string {
	u, _ := url.Parse(b.s.srv.URL)
	for _, c := range b.jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func (b *browser) setCookie(name, value string) {
	u, _ := url.Parse(b.s.srv.URL)
	b.jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}

// open follows a link to its end and says which ?auth= it came back with.
func (b *browser) open(to string) string {
	b.s.t.Helper()
	if strings.HasPrefix(to, "/") {
		to = b.s.srv.URL + to
	}
	resp, err := b.c.Get(to)
	if err != nil {
		b.s.t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.Request.URL.Query().Get("auth")
}

func (b *browser) send(method, path, body string, header ...string) (int, map[string]any) {
	b.s.t.Helper()
	req, _ := http.NewRequest(method, b.s.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (b *browser) me() map[string]any {
	b.s.t.Helper()
	status, out := b.send("GET", "/api/me", "")
	if status != http.StatusOK {
		b.s.t.Fatalf("/api/me = %d %v", status, out)
	}
	acc, _ := out["account"].(map[string]any)
	return acc
}

func (b *browser) seen() map[string]any {
	b.s.t.Helper()
	_, out := b.send("GET", "/", "")
	return out
}

// startURL is where the sign-in button sends the browser.
func startURL(provider string) string {
	return "/auth/" + provider + "/start?return_to=%2Fdays%3Fx%3D1"
}

// link asks the page for a link to provider and follows it.
func (b *browser) link(provider string) string {
	b.s.t.Helper()
	status, out := b.send("POST", "/auth/"+provider+"/link", `{"return_to":"/"}`)
	if status != http.StatusOK {
		b.s.t.Fatalf("link %s = %d %v", provider, status, out)
	}
	return b.open(out["url"].(string))
}

func (s *stand) countOf(q string, args ...any) int {
	s.t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		s.t.Fatal(err)
	}
	return n
}

var anya = oidctest.Identity{ID: 4242, Name: "Anya Petrova", Username: "anya"}

func TestSignInWithTelegramAndGoogle(t *testing.T) {
	s := setup(t)
	b := s.browser()
	s.telegram.SignInAs(anya)
	if code := b.open(startURL("telegram")); code != "ok" {
		t.Fatalf("Telegram sign-in = %q", code)
	}
	first := b.cookie(account.SessionCookie)
	acc := b.me()
	tgv, _ := acc["telegram"].(map[string]any)
	if first == "" || tgv["name"] != "Anya Petrova" || tgv["username"] != "anya" || acc["google"] != false || acc["via"] != "telegram" {
		t.Fatalf("me = %v, cookie %q", acc, first)
	}
	if seen := b.seen(); seen["telegram"] != float64(4242) {
		t.Fatalf("the viewer = %v", seen)
	}
	// The request asked for what an id_token needs, and nothing else.
	if q := s.telegram.LastAuthRequest(); q.Get("scope") != "openid profile" || q.Get("nonce") == "" || q.Get("state") == "" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("Telegram's request = %v", q)
	}
	// A sign-in on the site is not a DM with the bot: core knows the person
	// and nothing about where they were seen.
	if n := s.countOf(`SELECT count(*) FROM core.person WHERE telegram_user_id = 4242`); n != 1 {
		t.Fatal("the person is not in core")
	}
	if n := s.countOf(`SELECT count(*) FROM core.maply_presence WHERE telegram_user_id = 4242`); n != 0 {
		t.Fatal("a site sign-in wrote presence: the person would get the morning post")
	}

	// Linking Google starts a new session and ends the old one.
	s.google.SignInAs(oidctest.Identity{Sub: "1098765"})
	if code := b.link("google"); code != "linked" {
		t.Fatalf("link Google = %q", code)
	}
	second := b.cookie(account.SessionCookie)
	if second == "" || second == first {
		t.Fatal("the session was not replaced after the link")
	}
	if q := s.google.LastAuthRequest(); q.Get("scope") != "openid" || q.Get("prompt") != "select_account" {
		t.Fatalf("Google's request = %v", q)
	}
	old := s.browser()
	old.setCookie(account.SessionCookie, first)
	if old.me() != nil {
		t.Fatal("the session from before the link still opens the account")
	}
	if acc := b.me(); acc["google"] != true || acc["via"] != "telegram" {
		t.Fatalf("me after the link = %v", acc)
	}

	// Another browser signs in with Google into the same account.
	other := s.browser()
	if code := other.open(startURL("google")); code != "ok" {
		t.Fatalf("Google sign-in = %q", code)
	}
	if acc := other.me(); acc["telegram"] == nil {
		t.Fatalf("Google opened another account: %v", acc)
	}

	// Out here, then everywhere.
	if status, _ := other.send("POST", "/auth/logout", "{}"); status != http.StatusOK || other.me() != nil {
		t.Fatalf("sign out = %d", status)
	}
	other.open(startURL("google"))
	if status, _ := b.send("POST", "/auth/logout/all", "{}"); status != http.StatusOK {
		t.Fatalf("sign out everywhere = %d", status)
	}
	if b.me() != nil || other.me() != nil {
		t.Fatal("a session outlived signing out everywhere")
	}

	// The log says how sign-ins ended and nothing about who.
	logs := s.logs.String()
	for _, secret := range []string{"4242", "1098765", "Anya", "anya", "127.0.0.1", "DO-NOT-LOG", first, second} {
		if strings.Contains(logs, secret) {
			t.Fatalf("the log carries %q:\n%s", secret, logs)
		}
	}
	if !strings.Contains(logs, "result=ok") || !strings.Contains(logs, "result=linked") {
		t.Fatalf("the log has no results:\n%s", logs)
	}
}

func TestTheCallbackRefusesWhatIsNotItsOwn(t *testing.T) {
	s := setup(t)
	s.google.SignInAs(oidctest.Identity{Sub: "g-1"})
	noFollow := func(b *browser) {
		b.c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	// begin starts a sign-in and stops at the provider's redirect back.
	begin := func(b *browser) *url.URL {
		noFollow(b)
		resp, err := b.c.Get(b.s.srv.URL + startURL("google"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		resp, err = b.c.Get(resp.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		back, _ := url.Parse(resp.Header.Get("Location"))
		b.c.CheckRedirect = nil
		return back
	}
	code := func(b *browser, to *url.URL) string { return b.open(to.String()) }

	t.Run("a swapped state", func(t *testing.T) {
		a, b := s.browser(), s.browser()
		backA, backB := begin(a), begin(b)
		swapped := *backA
		q := swapped.Query()
		q.Set("state", backB.Query().Get("state"))
		swapped.RawQuery = q.Encode()
		if got := code(a, &swapped); got != "failed" || a.cookie(account.SessionCookie) != "" {
			t.Fatalf("a swapped state = %q", got)
		}
	})
	t.Run("a replayed code", func(t *testing.T) {
		a := s.browser()
		back := begin(a)
		flow := a.cookie(account.FlowCookie)
		if got := code(a, back); got != "ok" {
			t.Fatalf("the first use = %q", got)
		}
		// The same callback, with the flow cookie put back: the provider has
		// used the code.
		thief := s.browser()
		thief.setCookie(account.FlowCookie, flow)
		if got := code(thief, back); got != "failed" || thief.cookie(account.SessionCookie) != "" {
			t.Fatalf("a replayed code = %q", got)
		}
	})
	t.Run("someone else's callback", func(t *testing.T) {
		// A victim who opens the attacker's callback has no flow for it.
		back := begin(s.browser())
		victim := s.browser()
		if got := code(victim, back); got != "expired" || victim.cookie(account.SessionCookie) != "" {
			t.Fatalf("someone else's callback = %q", got)
		}
	})
	t.Run("a forged flow", func(t *testing.T) {
		a := s.browser()
		back := begin(a)
		flow := a.cookie(account.FlowCookie)
		body, sig, _ := strings.Cut(flow, ".")
		a.setCookie(account.FlowCookie, body+"x."+sig)
		if got := code(a, back); got != "expired" {
			t.Fatalf("a forged flow = %q", got)
		}
	})
	t.Run("a flow older than ten minutes", func(t *testing.T) {
		a := s.browser()
		back := begin(a)
		s.move(11 * time.Minute)
		defer s.move(-11 * time.Minute)
		if got := code(a, back); got != "expired" {
			t.Fatalf("a late callback = %q", got)
		}
	})
	t.Run("the session changed on the way", func(t *testing.T) {
		a := s.browser()
		back := begin(a)
		a.setCookie(account.SessionCookie, strings.Repeat("A", 43))
		if got := code(a, back); got != "expired" {
			t.Fatalf("a callback into another session = %q", got)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		s.google.Cancel()
		defer s.google.SignInAs(oidctest.Identity{Sub: "g-1"})
		if got := s.browser().open(startURL("google")); got != "cancelled" {
			t.Fatalf("cancel = %q", got)
		}
	})
	t.Run("a provider that is away", func(t *testing.T) {
		svc := account.New(account.Config{Store: s.store, Key: []byte("k"), Now: s.now})
		rr := httptest.NewRecorder()
		svc.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/auth/google/start?return_to=/x", nil))
		if loc := rr.Header().Get("Location"); rr.Code != http.StatusSeeOther || loc != "/x?auth=unavailable" {
			t.Fatalf("no keys = %d %q", rr.Code, loc)
		}
	})
	t.Run("return_to stays on the site", func(t *testing.T) {
		s.move(time.Minute) // past the limiter's window
		a := s.browser()
		noFollow(a)
		for _, bad := range []string{"https://evil.test/", "//evil.test", "/\\evil.test", "javascript:alert(1)"} {
			resp, err := a.c.Get(s.srv.URL + "/auth/google/start?return_to=" + url.QueryEscape(bad))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			resp, err = a.c.Get(resp.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			resp, err = a.c.Get(resp.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if loc := resp.Header.Get("Location"); loc != "/?auth=ok" {
				t.Fatalf("return_to %q went to %q", bad, loc)
			}
		}
	})
}

func TestALinkIsThePagesOwn(t *testing.T) {
	s := setup(t)
	s.google.SignInAs(oidctest.Identity{Sub: "g-1"})
	b := s.browser()
	b.open(startURL("google"))
	for name, header := range map[string][]string{
		"a form":     {"Content-Type", "application/x-www-form-urlencoded"},
		"cross-site": {"Sec-Fetch-Site", "cross-site"},
	} {
		if status, _ := b.send("POST", "/auth/telegram/link", `{}`, header...); status != http.StatusBadRequest {
			t.Fatalf("%s = %d", name, status)
		}
	}
	// Without a session there is nothing to link to.
	if status, _ := s.browser().send("POST", "/auth/telegram/link", `{}`); status != http.StatusUnauthorized {
		t.Fatalf("a link without a session = %d", status)
	}
	if status, _ := b.send("POST", "/auth/google/link", `{}`); status != http.StatusConflict {
		t.Fatalf("a second Google = %d", status)
	}
}

func TestMergeAndTaken(t *testing.T) {
	s := setup(t)
	// Boris signed in with Telegram once and has nothing else.
	boris := oidctest.Identity{ID: 77, Name: "Boris", Username: "boris"}
	s.telegram.SignInAs(boris)
	old := s.browser()
	old.open(startURL("telegram"))

	// Now with Google, then links that Telegram: the two accounts are one.
	s.google.SignInAs(oidctest.Identity{Sub: "g-boris"})
	b := s.browser()
	b.open(startURL("google"))
	if code := b.link("telegram"); code != "merged" {
		t.Fatalf("link = %q", code)
	}
	if old.me() != nil {
		t.Fatal("the merged account's session still opens")
	}
	if n := s.countOf(`SELECT count(*) FROM maply.account`); n != 1 {
		t.Fatalf("%d accounts after the merge", n)
	}

	// Vera has both; Gleb's Google cannot take her Telegram.
	vera := oidctest.Identity{ID: 88, Name: "Vera"}
	s.telegram.SignInAs(vera)
	v := s.browser()
	v.open(startURL("telegram"))
	s.google.SignInAs(oidctest.Identity{Sub: "g-vera"})
	v.link("google")
	s.google.SignInAs(oidctest.Identity{Sub: "g-gleb"})
	g := s.browser()
	g.open(startURL("google"))
	if code := g.link("telegram"); code != "taken" {
		t.Fatalf("Vera's Telegram for Gleb = %q", code)
	}
}

func TestUnlinkAndDelete(t *testing.T) {
	s := setup(t)
	s.google.SignInAs(oidctest.Identity{Sub: "g-1"})
	b := s.browser()
	b.open(startURL("google"))
	if status, out := b.send("DELETE", "/api/account/identity/google", "{}"); status != http.StatusConflict || out["error"] != "last_identity" {
		t.Fatalf("unlink the only sign-in = %d %v", status, out)
	}
	s.telegram.SignInAs(anya)
	b.link("telegram")
	// A browser signed in by Google, as someone who linked it would be.
	intruder := s.browser()
	intruder.open(startURL("google"))
	if status, _ := b.send("DELETE", "/api/account/identity/google", "{}"); status != http.StatusOK {
		t.Fatalf("unlink Google = %d", status)
	}
	if intruder.me() != nil {
		t.Fatal("a session opened by the unlinked Google still opens the account")
	}
	if acc := b.me(); acc == nil || acc["google"] != false || acc["telegram"] == nil {
		t.Fatalf("the browser that unlinked = %v", acc)
	}

	// Delete needs the word, from the page.
	if status, _ := b.send("DELETE", "/api/account", `{}`); status != http.StatusBadRequest {
		t.Fatalf("delete without confirm = %d", status)
	}
	if status, _ := b.send("DELETE", "/api/account", `{"confirm":"delete"}`, "Sec-Fetch-Site", "cross-site"); status != http.StatusBadRequest {
		t.Fatalf("delete from another site = %d", status)
	}
	if status, _ := b.send("DELETE", "/api/account", `{"confirm":"delete"}`); status != http.StatusOK {
		t.Fatalf("delete = %d", status)
	}
	if b.me() != nil || s.countOf(`SELECT count(*) FROM maply.account`) != 0 || s.countOf(`SELECT count(*) FROM maply.session`) != 0 {
		t.Fatal("the account outlived its deletion")
	}
}

func TestASessionLastsNinetyDaysFromTheSignIn(t *testing.T) {
	s := setup(t)
	s.google.SignInAs(oidctest.Identity{Sub: "g-1"})
	b := s.browser()
	b.open(startURL("google"))
	token := b.cookie(account.SessionCookie)
	// Used every day, it still ends 90 days after the sign-in. Another
	// browser carries the token, so the jar's own expiry is no part of it.
	other := s.browser()
	other.setCookie(account.SessionCookie, token)
	for range 89 {
		s.move(24 * time.Hour)
		if other.me() == nil {
			t.Fatal("the session ended early")
		}
	}
	s.move(24*time.Hour + 31*time.Second)
	if other.me() != nil {
		t.Fatal("the session was extended by use")
	}
}

func TestTheGuestJoinsANewAccountOrWaits(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO maply.place (id, status, source, source_id, license, kind, lat, lon, image_key, country_iso, continent, difficulty, reviewed_at)
		OVERRIDING SYSTEM VALUE VALUES (1, 'used', 'mapillary', 'm1', 'CC-BY-SA-4.0', 'photo', 50.45, 30.52, 'k1', 'UA', 'europe', 2, now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO maply.round (date, day_no, place_id) VALUES ('2026-11-01', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	guest := func(b *browser, id string) {
		if _, err := s.pool.Exec(ctx, `INSERT INTO maply.site_player (id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, `INSERT INTO maply.site_guess (date, player, lat, lon, answered_at, deadline_at) VALUES ('2026-11-01', $1, 1, 2, '2026-11-01 09:00Z', '2026-11-02 05:00Z')`, id); err != nil {
			t.Fatal(err)
		}
		b.setCookie(account.GuestCookie, id)
	}
	s.google.SignInAs(oidctest.Identity{Sub: "g-1"})
	first := s.browser()
	guest(first, "11111111-1111-4111-8111-111111111111")
	first.open(startURL("google"))
	if seen := first.seen(); seen["player"] != "11111111-1111-4111-8111-111111111111" || first.cookie(account.GuestCookie) != "" {
		t.Fatalf("the new account's player = %v, guest cookie %q", seen, first.cookie(account.GuestCookie))
	}
	// The same account in another browser with its own guest: asked first.
	second := s.browser()
	guest(second, "22222222-2222-4222-8222-222222222222")
	second.open(startURL("google"))
	_, me := second.send("GET", "/api/me", "")
	if g, _ := me["guest"].(map[string]any); g == nil || g["answers"] != float64(1) {
		t.Fatalf("me = %v: the page should ask about one answer", me)
	}
	if status, out := second.send("POST", "/api/account/adopt", `{"take":true}`); status != http.StatusOK || out["answers"] != float64(1) {
		t.Fatalf("adopt = %d %v", status, out)
	}
	if second.cookie(account.GuestCookie) != "" {
		t.Fatal("the guest cookie stayed after the answer")
	}
	// A guest cookie of a player an account has opens nothing.
	stray := s.browser()
	stray.setCookie(account.GuestCookie, "22222222-2222-4222-8222-222222222222")
	if seen := stray.seen(); seen["player"] != "" {
		t.Fatalf("an old guest cookie opened the account's player: %v", seen)
	}
}

// initData signs a Mini App launch the way Telegram does.
func initData(userID int64, name string) string {
	fields := map[string]string{
		"auth_date": strconv.FormatInt(time.Now().Unix(), 10),
		"user":      fmt.Sprintf(`{"id":%d,"first_name":%q,"username":"anya","language_code":"uk"}`, userID, name),
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	values := url.Values{}
	for i, k := range keys {
		lines[i] = k + "=" + fields[k]
		values.Set(k, fields[k])
	}
	m := hmac.New(sha256.New, []byte("WebAppData"))
	m.Write([]byte(botToken))
	h := hmac.New(sha256.New, m.Sum(nil))
	h.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(h.Sum(nil)))
	return values.Encode()
}

func TestTheMiniAppLinksGoogleWithATicket(t *testing.T) {
	s := setup(t)
	app := s.browser()
	tma := "tma " + initData(4242, "Anya")
	status, out := app.send("POST", "/api/account/ticket", "{}", "Authorization", tma)
	link, _ := out["url"].(string)
	if status != http.StatusOK || !strings.HasPrefix(link, s.srv.URL+"/#link=") {
		t.Fatalf("ticket = %d %v", status, out)
	}
	ticket := strings.TrimPrefix(link, s.srv.URL+"/#link=")
	if n := s.countOf(`SELECT count(*) FROM core.maply_presence WHERE telegram_user_id = 4242`); n != 0 {
		t.Fatal("a ticket wrote presence")
	}

	// The phone's browser says whose account it is before going anywhere.
	phone := s.browser()
	status, who := phone.send("POST", "/auth/ticket", `{"ticket":"`+ticket+`"}`)
	if status != http.StatusOK || who["name"] != "Anya" || who["username"] != "anya" {
		t.Fatalf("whose ticket = %d %v", status, who)
	}
	status, out = phone.send("POST", "/auth/ticket/confirm", `{"ticket":"`+ticket+`"}`)
	if status != http.StatusOK {
		t.Fatalf("confirm = %d %v", status, out)
	}
	s.google.SignInAs(oidctest.Identity{Sub: "g-anya"})
	if code := phone.open(out["url"].(string)); code != "linked" {
		t.Fatalf("the ticket's callback = %q", code)
	}
	if acc := phone.me(); acc == nil || acc["google"] != true || acc["telegram"] == nil || acc["via"] != "google" {
		t.Fatalf("the phone's browser = %v", acc)
	}
	// Good once.
	if status, _ := s.browser().send("POST", "/auth/ticket/confirm", `{"ticket":"`+ticket+`"}`); status != http.StatusGone {
		t.Fatalf("a used ticket = %d", status)
	}
	// The Mini App sees the link, and a second ticket is refused.
	if status, out := app.send("GET", "/api/me", "", "Authorization", tma); status != http.StatusOK || out["account"].(map[string]any)["google"] != true {
		t.Fatalf("the Mini App's me = %d %v", status, out)
	}
	if status, _ := app.send("POST", "/api/account/ticket", "{}", "Authorization", tma); status != http.StatusConflict {
		t.Fatalf("a ticket with Google linked = %d", status)
	}
	if status, _ := app.send("POST", "/api/account/ticket", "{}", "Authorization", "tma forged"); status != http.StatusUnauthorized {
		t.Fatalf("a forged launch = %d", status)
	}
	// A ticket lasts five minutes.
	if status, _ := app.send("DELETE", "/api/account/identity/google", "{}", "Authorization", tma); status != http.StatusOK {
		t.Fatalf("unlink from the Mini App = %d", status)
	}
	_, out = app.send("POST", "/api/account/ticket", "{}", "Authorization", tma)
	late := strings.TrimPrefix(out["url"].(string), s.srv.URL+"/#link=")
	s.move(6 * time.Minute)
	if status, _ := s.browser().send("POST", "/auth/ticket", `{"ticket":"`+late+`"}`); status != http.StatusGone {
		t.Fatalf("a ticket after six minutes = %d", status)
	}
}

func TestPrivacySpeaksEveryLanguage(t *testing.T) {
	s := setup(t)
	resp, err := s.srv.Client().Get(s.srv.URL + "/privacy?lang=uk")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `lang="uk"`) || resp.Header.Get("Content-Security-Policy") == "" ||
		strings.Contains(string(body), "[acct.") {
		t.Fatalf("privacy = %d %s", resp.StatusCode, body)
	}
}
