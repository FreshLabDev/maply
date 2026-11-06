// SPDX-License-Identifier: Apache-2.0
package oidc_test

import (
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/FreshLabDev/maply/internal/oidc"
	"github.com/FreshLabDev/maply/internal/oidc/oidctest"
)

const (
	clientID = "7000001"
	secret   = "client-secret-DO-NOT-LOG"
	redirect = "https://maply.test/auth/telegram/callback"
)

func provider(t *testing.T, telegram bool) (*oidctest.Provider, *oidc.Provider) {
	t.Helper()
	fake := oidctest.Start(t, clientID, secret, telegram)
	p := oidc.New(oidc.Config{
		Issuer: fake.Issuer, ClientID: clientID, ClientSecret: secret, RedirectURL: redirect,
		Scopes: []string{"openid", "profile"}, AuthParams: map[string]string{"prompt": "select_account"},
	})
	return fake, p
}

// signIn runs the browser's part: the authorization request, and the code
// the provider sends back.
func signIn(t *testing.T, p *oidc.Provider, state, nonce, verifier string) (code string, back url.Values) {
	t.Helper()
	to, err := p.AuthURL(context.Background(), state, nonce, verifier)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(to)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if !strings.HasPrefix(loc.String(), redirect) {
		t.Fatalf("redirected to %s", loc)
	}
	return loc.Query().Get("code"), loc.Query()
}

func TestTheAuthorizationRequest(t *testing.T) {
	fake, p := provider(t, true)
	fake.SignInAs(oidctest.Identity{ID: 42, Name: "Anya", Username: "anya"})
	verifier := oidc.NewVerifier()
	_, back := signIn(t, p, "state-1", "nonce-1", verifier)
	q := fake.LastAuthRequest()
	sum := sha256.Sum256([]byte(verifier))
	for k, want := range map[string]string{
		"client_id": clientID, "redirect_uri": redirect, "response_type": "code", "state": "state-1", "nonce": "nonce-1",
		"code_challenge_method": "S256", "code_challenge": base64.RawURLEncoding.EncodeToString(sum[:]),
		"scope": "openid profile", "prompt": "select_account",
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if q.Has("code_verifier") || q.Has("client_secret") {
		t.Fatalf("the authorization request carries a secret: %v", q)
	}
	if back.Get("state") != "state-1" {
		t.Fatalf("state came back as %q", back.Get("state"))
	}
}

func TestExchangeGivesTheClaims(t *testing.T) {
	fake, p := provider(t, true)
	fake.SignInAs(oidctest.Identity{ID: 42, Name: "Anya", Username: "anya"})
	v := oidc.NewVerifier()
	code, _ := signIn(t, p, "s", "n", v)
	tok, err := p.Exchange(context.Background(), code, v, "n")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		ID       int64  `json:"id"`
		Username string `json:"preferred_username"`
	}
	if err := tok.Claims(&c); err != nil || c.ID != 42 || c.Username != "anya" || tok.Subject == "" || tok.Subject == "42" {
		t.Fatalf("claims = %+v, sub %q, %v", c, tok.Subject, err)
	}
	// A code is good once.
	if _, err := p.Exchange(context.Background(), code, v, "n"); code2(err) != oidc.CodeExchange {
		t.Fatalf("a code used twice = %v", err)
	}
}

func code2(err error) string {
	var e *oidc.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestExchangeRefuses(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		edit func(map[string]any)
		sign func(map[string]any) string
		// verifier and nonce the callback presents, when not the sign-in's own
		verifier, nonce string
		want            string
	}{
		{name: "another nonce", nonce: "someone-else", want: oidc.CodeNonce},
		{name: "no nonce", edit: func(c map[string]any) { delete(c, "nonce") }, want: oidc.CodeNonce},
		{name: "another verifier", verifier: oidc.NewVerifier(), want: oidc.CodeExchange},
		{name: "another audience", edit: func(c map[string]any) { c["aud"] = "999" }, want: oidc.CodeToken},
		{name: "expired", edit: func(c map[string]any) { c["exp"] = now.Add(-time.Minute).Unix() }, want: oidc.CodeToken},
		{name: "another issuer", edit: func(c map[string]any) { c["iss"] = "https://evil.test" }, want: oidc.CodeToken},
		{name: "a key nobody publishes", sign: oidctest.SignOtherKey, want: oidc.CodeToken},
		{name: "RS512", sign: oidctest.SignRS512, want: oidc.CodeToken},
		{name: "HS256 with the public key", sign: oidctest.SignHS256WithPublicKey, want: oidc.CodeToken},
		{name: "unsigned", sign: oidctest.SignNone, want: oidc.CodeToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, p := provider(t, false)
			fake.SignInAs(oidctest.Identity{Sub: "1098765"})
			fake.Edit(tc.edit)
			if tc.sign != nil {
				fake.Sign(tc.sign)
			}
			v := oidc.NewVerifier()
			code, _ := signIn(t, p, "s", "n", v)
			if tc.verifier != "" {
				v = tc.verifier
			}
			nonce := "n"
			if tc.nonce != "" {
				nonce = tc.nonce
			}
			tok, err := p.Exchange(context.Background(), code, v, nonce)
			if code2(err) != tc.want || tok != nil {
				t.Fatalf("Exchange = %v, %v; want %s", tok, err, tc.want)
			}
			// What the error says never carries the provider's description.
			if strings.Contains(err.Error(), "DO-NOT-LOG") {
				t.Fatalf("the error carries the provider's body: %v", err)
			}
		})
	}
}

func TestTheSecretGoesInTheHeader(t *testing.T) {
	// The fake refuses a secret in the body and one missing from the header.
	fake, p := provider(t, false)
	fake.SignInAs(oidctest.Identity{Sub: "1"})
	v := oidc.NewVerifier()
	code, _ := signIn(t, p, "s", "n", v)
	if _, err := p.Exchange(context.Background(), code, v, "n"); err != nil {
		t.Fatal(err)
	}
	wrong := oidc.New(oidc.Config{Issuer: fake.Issuer, ClientID: clientID, ClientSecret: "wrong", RedirectURL: redirect, Scopes: []string{"openid"}})
	code, _ = signIn(t, wrong, "s", "n", v)
	if _, err := wrong.Exchange(context.Background(), code, v, "n"); code2(err) != oidc.CodeExchange {
		t.Fatalf("a wrong secret = %v", err)
	}
}

func TestDiscoveryFailureIsNotKept(t *testing.T) {
	fake, p := provider(t, false)
	fake.Down(true)
	if _, err := p.AuthURL(context.Background(), "s", "n", oidc.NewVerifier()); code2(err) != oidc.CodeUnavailable {
		t.Fatalf("AuthURL with the provider away = %v", err)
	}
	fake.Down(false)
	if _, err := p.AuthURL(context.Background(), "s", "n", oidc.NewVerifier()); err != nil {
		t.Fatalf("AuthURL once it is back = %v", err)
	}
}

// Google sometimes writes its issuer without the scheme. go-oidc takes that
// for Google's issuer alone; this holds the dependency to it.
func TestGoogleIssuerWithoutScheme(t *testing.T) {
	keys := &gooidc.StaticKeySet{PublicKeys: []crypto.PublicKey{oidctest.Key().Public()}}
	v := gooidc.NewVerifier("https://accounts.google.com", keys, &gooidc.Config{ClientID: "g-client", SupportedSigningAlgs: []string{gooidc.RS256}})
	now := time.Now()
	for _, iss := range []string{"https://accounts.google.com", "accounts.google.com"} {
		raw := oidctest.SignRS256(map[string]any{"iss": iss, "aud": "g-client", "sub": "1098765", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
		if _, err := v.Verify(context.Background(), raw); err != nil {
			t.Fatalf("iss %q: %v", iss, err)
		}
	}
	raw := oidctest.SignRS256(map[string]any{"iss": "evil.google.com", "aud": "g-client", "sub": "1", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	if _, err := v.Verify(context.Background(), raw); err == nil {
		t.Fatal("another issuer verified")
	}
}
