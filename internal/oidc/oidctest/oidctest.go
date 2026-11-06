// SPDX-License-Identifier: Apache-2.0

// Package oidctest is a stand-in for oauth.telegram.org and Google's sign-in,
// for tests and for the local stand: the discovery document, the keys, an
// authorization endpoint, and a token endpoint as strict as the real ones are
// meant to be (the client authenticates in a header, the PKCE verifier has to
// match the challenge, a code is good once and only for its redirect URI).
//
// A sign-in is approved at once as the identity SignInAs set, or, with none
// set, the authorization endpoint shows a page to pick one of People or to
// cancel, which is what the stand shows its user.
package oidctest

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Identity is who signs in. Telegram's tokens carry ID, Name and Username,
// with an opaque Sub that is never the account; Google's carry Sub alone.
type Identity struct {
	ID       int64
	Name     string
	Username string
	Sub      string
}

// Label is how the picking page names the identity.
func (id Identity) Label() string {
	if id.ID != 0 {
		s := id.Name
		if id.Username != "" {
			s += " (@" + id.Username + ")"
		}
		return s + " · id " + strconv.FormatInt(id.ID, 10)
	}
	if id.Name != "" {
		return id.Name + " · sub " + id.Sub
	}
	return "sub " + id.Sub
}

type code struct {
	challenge, nonce, redirect string
	who                        Identity
	used                       bool
}

type pending struct {
	q url.Values
}

// Provider is one fake provider. Set the exported fields before it serves.
type Provider struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// Telegram shapes the claims as Telegram does.
	Telegram bool
	// Now is the clock of the tokens it issues.
	Now func() time.Time

	mu       sync.Mutex
	next     *Identity
	people   []Identity
	deny     bool
	down     bool
	codes    map[string]*code
	waiting  map[string]pending
	edit     func(map[string]any)
	sign     func(map[string]any) string
	tokens   int
	lastAuth url.Values
}

// New is a provider at issuer, which has to be where its handler is served:
// a path in issuer is the prefix of every endpoint.
func New(issuer, clientID, secret string, telegram bool) *Provider {
	return &Provider{Issuer: strings.TrimRight(issuer, "/"), ClientID: clientID, ClientSecret: secret, Telegram: telegram,
		codes: map[string]*code{}, waiting: map[string]pending{}}
}

// Start serves a new provider on a test server that closes with the test.
func Start(t testing.TB, clientID, secret string, telegram bool) *Provider {
	t.Helper()
	p := New("", clientID, secret, telegram)
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	p.Issuer = srv.URL
	return p
}

// SignInAs makes every sign-in from now on approve at once as id.
func (p *Provider) SignInAs(id Identity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next, p.deny = &id, false
}

// Cancel makes the sign-ins refuse, as a person who pressed Cancel would.
func (p *Provider) Cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deny = true
}

// SetPeople is the list the picking page offers; SignInAs wins over it.
func (p *Provider) SetPeople(people []Identity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.people = append([]Identity(nil), people...)
}

// Down makes the discovery document fail, as a provider that is away.
func (p *Provider) Down(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
}

// Edit changes the claims of the next tokens before they are signed.
func (p *Provider) Edit(f func(map[string]any)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.edit = f
}

// Sign replaces the signing of the next tokens.
func (p *Provider) Sign(f func(map[string]any) string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sign = f
}

// TokenRequests is how many requests reached the token endpoint.
func (p *Provider) TokenRequests() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokens
}

// LastAuthRequest is the query of the last authorization request.
func (p *Provider) LastAuthRequest() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastAuth
}

func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prefix := ""
	if u, err := url.Parse(p.Issuer); err == nil {
		prefix = strings.TrimRight(u.Path, "/")
	}
	path, ok := strings.CutPrefix(r.URL.Path, prefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "/.well-known/openid-configuration":
		p.serveDiscovery(w)
	case r.Method == http.MethodGet && path == "/jwks":
		p.serveKeys(w)
	case r.Method == http.MethodGet && path == "/auth":
		p.serveAuthorize(w, r)
	case r.Method == http.MethodGet && path == "/auth/pick":
		p.servePick(w, r)
	case r.Method == http.MethodPost && path == "/token":
		p.serveToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (p *Provider) serveDiscovery(w http.ResponseWriter) {
	p.mu.Lock()
	down := p.down
	p.mu.Unlock()
	if down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                   p.Issuer,
		"authorization_endpoint":   p.Issuer + "/auth",
		"token_endpoint":           p.Issuer + "/token",
		"jwks_uri":                 p.Issuer + "/jwks",
		"response_types_supported": []string{"code"},
		"subject_types_supported":  []string{"public"},
		// RS512 is listed so a test proves the client's own restriction to
		// RS256, not this document's: a client that believed it would take it.
		"id_token_signing_alg_values_supported": []string{"RS256", "RS512", "ES256", "EdDSA"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
		"scopes_supported":                      []string{"openid", "profile"},
	})
}

func (p *Provider) serveKeys(w http.ResponseWriter) {
	k := Key()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test-key",
		"n": b64(k.N.Bytes()), "e": b64(big.NewInt(int64(k.E)).Bytes()),
	}}})
}

func (p *Provider) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p.mu.Lock()
	p.lastAuth = q
	next, deny := p.next, p.deny
	p.mu.Unlock()
	if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		q.Get("redirect_uri") == "" || !strings.Contains(" "+q.Get("scope")+" ", " openid ") {
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	switch {
	case deny:
		p.back(w, r, q, nil)
	case next != nil:
		p.back(w, r, q, next)
	default:
		p.showPick(w, q)
	}
}

// back sends the browser to the redirect URI with a code for who, or with
// access_denied when who is nil.
func (p *Provider) back(w http.ResponseWriter, r *http.Request, q url.Values, who *Identity) {
	to, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	out := to.Query()
	out.Set("state", q.Get("state"))
	if who == nil {
		out.Set("error", "access_denied")
	} else {
		c := randHex()
		p.mu.Lock()
		p.codes[c] = &code{challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), redirect: q.Get("redirect_uri"), who: *who}
		p.mu.Unlock()
		out.Set("code", c)
	}
	to.RawQuery = out.Encode()
	http.Redirect(w, r, to.String(), http.StatusFound)
}

func (p *Provider) showPick(w http.ResponseWriter, q url.Values) {
	id := randHex()
	p.mu.Lock()
	p.waiting[id] = pending{q: q}
	people := append([]Identity(nil), p.people...)
	p.mu.Unlock()
	name := "Google"
	if p.Telegram {
		name = "Telegram"
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width">` +
		`<title>` + name + ` (fake)</title><style>body{font:16px system-ui;max-width:420px;margin:40px auto;padding:0 16px}` +
		`a{display:block;padding:12px 14px;margin:8px 0;border:1px solid #888;border-radius:10px;color:inherit;text-decoration:none}` +
		`a.no{border-style:dashed}</style><h1>` + name + ` sign-in (fake)</h1><p>Who signs in?</p>`)
	for i, who := range people {
		fmt.Fprintf(&b, `<a href="%s/auth/pick?req=%s&who=%d">%s</a>`, html.EscapeString(p.Issuer), id, i, html.EscapeString(who.Label()))
	}
	fmt.Fprintf(&b, `<a class="no" href="%s/auth/pick?req=%s&who=-1">Cancel</a>`, html.EscapeString(p.Issuer), id)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func (p *Provider) servePick(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p.mu.Lock()
	req, ok := p.waiting[q.Get("req")]
	delete(p.waiting, q.Get("req"))
	people := p.people
	p.mu.Unlock()
	if !ok {
		http.Error(w, "this sign-in is over", http.StatusBadRequest)
		return
	}
	n, err := strconv.Atoi(q.Get("who"))
	if err != nil || n >= len(people) {
		http.Error(w, "no such person", http.StatusBadRequest)
		return
	}
	if n < 0 {
		p.back(w, r, req.q, nil)
		return
	}
	who := people[n]
	p.back(w, r, req.q, &who)
}

func (p *Provider) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, secret, basic := r.BasicAuth()
	// RFC 6749 2.3.1: both are form-urlencoded before they are joined.
	id, _ = url.QueryUnescape(id)
	secret, _ = url.QueryUnescape(secret)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens++
	refuse := func(status int, e string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"error":%q,"error_description":"DO-NOT-LOG provider detail"}`, e)
	}
	c := p.codes[r.PostForm.Get("code")]
	switch {
	case !basic || id != p.ClientID || secret != p.ClientSecret || r.PostForm.Has("client_secret"):
		refuse(http.StatusUnauthorized, "invalid_client")
		return
	case r.PostForm.Get("grant_type") != "authorization_code" || c == nil || c.used:
		refuse(http.StatusBadRequest, "invalid_grant")
		return
	case r.PostForm.Get("redirect_uri") != c.redirect:
		refuse(http.StatusBadRequest, "invalid_grant")
		return
	}
	c.used = true
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if b64(sum[:]) != c.challenge {
		refuse(http.StatusBadRequest, "invalid_grant")
		return
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	claims := map[string]any{
		"iss": p.Issuer, "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": c.nonce,
	}
	if p.Telegram {
		sub := c.who.Sub
		if sub == "" {
			sub = "tg-opaque-" + strconv.FormatInt(c.who.ID*7919, 36)
		}
		claims["sub"], claims["id"], claims["name"], claims["preferred_username"] = sub, c.who.ID, c.who.Name, c.who.Username
	} else {
		claims["sub"] = c.who.Sub
	}
	if p.edit != nil {
		p.edit(claims)
	}
	token := SignRS256(claims)
	if p.sign != nil {
		token = p.sign(claims)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "access-token-DO-NOT-LOG", "token_type": "Bearer", "expires_in": 3600, "id_token": token,
	})
}

// Keys: the one every fake publishes, and one none does.

var (
	keyOnce       sync.Once
	theKey, other *rsa.PrivateKey
)

func keys() {
	keyOnce.Do(func() {
		var err error
		if theKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if other, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
}

// Key is the key the fakes publish.
func Key() *rsa.PrivateKey { keys(); return theKey }

// OtherKey is a key no fake publishes.
func OtherKey() *rsa.PrivateKey { keys(); return other }

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func randHex() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}

// JWT joins a header and claims and signs the result with sign.
func JWT(header, claims map[string]any, sign func(input string) []byte) string {
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	input := b64(h) + "." + b64(c)
	return input + "." + b64(sign(input))
}

func rsaSign(key *rsa.PrivateKey, h crypto.Hash) func(string) []byte {
	return func(input string) []byte {
		var sum []byte
		if h == crypto.SHA512 {
			s := sha512.Sum512([]byte(input))
			sum = s[:]
		} else {
			s := sha256.Sum256([]byte(input))
			sum = s[:]
		}
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, h, sum)
		if err != nil {
			panic(err)
		}
		return sig
	}
}

// SignRS256 is the token a provider would send: RS256 with the published key.
func SignRS256(claims map[string]any) string {
	return JWT(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test-key"}, claims, rsaSign(Key(), crypto.SHA256))
}

// SignOtherKey is RS256 with a key nobody publishes.
func SignOtherKey(claims map[string]any) string {
	return JWT(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test-key"}, claims, rsaSign(OtherKey(), crypto.SHA256))
}

// SignRS512 uses the published key and an algorithm the client does not take.
func SignRS512(claims map[string]any) string {
	return JWT(map[string]any{"alg": "RS512", "typ": "JWT", "kid": "test-key"}, claims, rsaSign(Key(), crypto.SHA512))
}

// SignHS256WithPublicKey is the old confusion: the public key, which anyone
// has, used as an HMAC secret.
func SignHS256WithPublicKey(claims map[string]any) string {
	return JWT(map[string]any{"alg": "HS256", "typ": "JWT", "kid": "test-key"}, claims, func(input string) []byte {
		m := hmac.New(sha256.New, Key().N.Bytes())
		m.Write([]byte(input))
		return m.Sum(nil)
	})
}

// SignNone is an unsigned token.
func SignNone(claims map[string]any) string {
	return JWT(map[string]any{"alg": "none", "typ": "JWT"}, claims, func(string) []byte { return nil })
}
