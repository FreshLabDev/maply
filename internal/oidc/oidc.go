// SPDX-License-Identifier: Apache-2.0

// Package oidc signs a browser in with an OpenID Connect provider through
// the server: the authorization code flow with PKCE S256, a state and a
// nonce on every sign-in, and the id_token checked here. maply uses it for
// Telegram (oauth.telegram.org) and Google; neither gets a script on the page.
//
// The caller keeps the state, the nonce and the verifier between the two
// legs and compares the state itself; Exchange checks the nonce, because
// go-oidc's Verify does not.
package oidc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config is one provider's client.
type Config struct {
	// Issuer is the provider's issuer; its discovery document is read on the
	// first sign-in.
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is this site's callback, registered with the provider.
	RedirectURL string
	Scopes      []string
	// AuthParams go into the authorization request as they are, such as
	// Google's prompt=select_account.
	AuthParams map[string]string
	// HTTP makes the calls to the provider. It is copied and never follows a
	// redirect: the client secret travels in a header.
	HTTP *http.Client
	Now  func() time.Time
}

// Provider is a configured client. Its methods are safe for concurrent use.
type Provider struct {
	cfg  Config
	http *http.Client
	now  func() time.Time

	mu   sync.Mutex
	disc *discovered
}

type discovered struct {
	oauth    *oauth2.Config
	verifier *gooidc.IDTokenVerifier
}

// New builds the client. It makes no network call.
func New(cfg Config) *Provider {
	hc := &http.Client{Timeout: 10 * time.Second}
	if cfg.HTTP != nil {
		c := *cfg.HTTP
		hc = &c
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Provider{cfg: cfg, http: hc, now: now}
}

// Error is a failed step with a code fit for a log line. Err may hold more,
// but never a token, a code or the provider's error description.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// The codes of Error.
const (
	// CodeUnavailable is a provider whose discovery document did not come.
	CodeUnavailable = "unavailable"
	// CodeExchange is a token endpoint that refused the code: used, expired,
	// a verifier that does not match, a client it does not know.
	CodeExchange = "exchange"
	// CodeToken is an id_token that is missing or does not verify: the
	// signature, the algorithm, the issuer, the audience or the expiry.
	CodeToken = "token"
	// CodeNonce is an id_token whose nonce is not this sign-in's.
	CodeNonce = "nonce"
)

func fail(code string, err error) error { return &Error{Code: code, Err: err} }

// discover reads the discovery document once, on the first sign-in, and
// keeps the result. A failure is not kept: the next attempt asks again.
func (p *Provider) discover(ctx context.Context) (*discovered, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disc != nil {
		return p.disc, nil
	}
	prov, err := gooidc.NewProvider(gooidc.ClientContext(ctx, p.http), p.cfg.Issuer)
	if err != nil {
		return nil, fail(CodeUnavailable, fmt.Errorf("discover: %w", err))
	}
	ep := prov.Endpoint()
	// Said outright: auto-detection would try the body first and, on a
	// refusal, the header, which is a second request carrying the secret.
	ep.AuthStyle = oauth2.AuthStyleInHeader
	p.disc = &discovered{
		oauth: &oauth2.Config{
			ClientID: p.cfg.ClientID, ClientSecret: p.cfg.ClientSecret,
			Endpoint: ep, RedirectURL: p.cfg.RedirectURL, Scopes: p.cfg.Scopes,
		},
		// The discovery document may list algorithms this verifier would
		// take; the list is cut to RS256, which both providers sign with.
		// The context only carries the HTTP client for the key set.
		verifier: prov.VerifierContext(gooidc.ClientContext(context.Background(), p.http), &gooidc.Config{
			ClientID:             p.cfg.ClientID,
			SupportedSigningAlgs: []string{gooidc.RS256},
			Now:                  p.now,
		}),
	}
	return p.disc, nil
}

// NewVerifier is a fresh PKCE verifier.
func NewVerifier() string { return oauth2.GenerateVerifier() }

// AuthURL is where the browser goes to sign in: the provider's authorization
// endpoint with the state, the nonce and the S256 challenge of verifier.
func (p *Provider) AuthURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	d, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), gooidc.Nonce(nonce)}
	for k, v := range p.cfg.AuthParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}
	return d.oauth.AuthCodeURL(state, opts...), nil
}

// Token is a verified id_token.
type Token struct {
	// Subject is the `sub` claim.
	Subject string
	claims  json.RawMessage
}

// Claims decodes the id_token's claims into v.
func (t *Token) Claims(v any) error { return json.Unmarshal(t.claims, v) }

// Exchange trades the code, with the PKCE verifier and the client
// authenticated in a header, for tokens, verifies the id_token and checks
// that it carries nonce.
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (*Token, error) {
	d, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	ctx = gooidc.ClientContext(ctx, p.http)
	tok, err := d.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		// An oauth2.RetrieveError carries the provider's body; its status and
		// error code are all that is kept.
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			status := ""
			if re.Response != nil {
				status = re.Response.Status
			}
			err = fmt.Errorf("the token endpoint refused the code: %s %s", status, re.ErrorCode)
		}
		return nil, fail(CodeExchange, err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, fail(CodeToken, errors.New("no id_token in the token response"))
	}
	idt, err := d.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fail(CodeToken, err)
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		return nil, fail(CodeNonce, errors.New("nonce does not match"))
	}
	var claims json.RawMessage
	if err := idt.Claims(&claims); err != nil {
		return nil, fail(CodeToken, err)
	}
	return &Token{Subject: idt.Subject, claims: claims}, nil
}
