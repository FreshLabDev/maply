// SPDX-License-Identifier: Apache-2.0
package account

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// FlowCookie carries a sign-in between its two legs.
	FlowCookie = "__Host-maply_oauth"
	// FlowLife is how long a sign-in may take.
	FlowLife = 10 * time.Minute

	intentLogin = "login"
	intentLink  = "link"
)

// flow is a sign-in under way.
type flow struct {
	Provider string `json:"p"`
	Intent   string `json:"i"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
	At       int64  `json:"t"`
	// Account and Session are set for a link: the account it joins and the
	// hash of the session it started in, base64url. A ticket's link has no
	// session; Ticket is the hash of its ticket instead.
	Account string `json:"a,omitempty"`
	Session string `json:"h,omitempty"`
	Ticket  string `json:"k,omitempty"`
}

var errFlow = errors.New("no valid sign-in under way")

// sign is the cookie's value: the flow and its HMAC, base64url, dot apart.
func (s *Service) sign(f flow) (string, error) {
	raw, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + base64.RawURLEncoding.EncodeToString(s.mac(body)), nil
}

func (s *Service) mac(body string) []byte {
	m := hmac.New(sha256.New, s.cfg.Key)
	m.Write([]byte("maply-auth-flow-v1|"))
	m.Write([]byte(body))
	return m.Sum(nil)
}

// readFlow is the request's sign-in, checked: signed by this server, not
// older than FlowLife.
func (s *Service) readFlow(r *http.Request) (flow, error) {
	c, err := r.Cookie(FlowCookie)
	if err != nil || len(c.Value) > 4096 {
		return flow{}, errFlow
	}
	body, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return flow{}, errFlow
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, s.mac(body)) {
		return flow{}, errFlow
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return flow{}, errFlow
	}
	var f flow
	if err := json.Unmarshal(raw, &f); err != nil {
		return flow{}, errFlow
	}
	age := s.now().Sub(time.Unix(f.At, 0))
	if age < -time.Minute || age > FlowLife {
		return flow{}, errFlow
	}
	return f, nil
}

func (s *Service) setFlow(w http.ResponseWriter, f flow) error {
	v, err := s.sign(f)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: FlowCookie, Value: v, Path: "/", MaxAge: int(FlowLife.Seconds()),
		// Lax: the provider sends the browser back with a top-level GET,
		// which carries it.
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

// newToken is 256 random bits, base64url.
func newToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// tokenShape is what newToken makes.
var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func hashToken(t string) [32]byte { return sha256.Sum256([]byte(t)) }

func encodeHash(h [32]byte) string { return base64.RawURLEncoding.EncodeToString(h[:]) }

func decodeHash(s string) ([32]byte, bool) {
	var h [32]byte
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != len(h) {
		return h, false
	}
	copy(h[:], b)
	return h, true
}

func sameHash(a [32]byte, b string) bool {
	return subtle.ConstantTimeCompare([]byte(encodeHash(a)), []byte(b)) == 1
}

// sessionHash is the hash of the request's session token, if it carries one
// of the right shape.
func sessionHash(r *http.Request) ([32]byte, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || !tokenShape.MatchString(c.Value) {
		return [32]byte{}, false
	}
	return hashToken(c.Value), true
}

func (s *Service) setSession(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", Expires: expires.UTC(), MaxAge: int(expires.Sub(s.now()).Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// guestID is the browser's guest player from its cookie, empty without one.
func guestID(r *http.Request) string {
	c, err := r.Cookie(GuestCookie)
	if err != nil || !uuidPattern.MatchString(c.Value) {
		return ""
	}
	return c.Value
}

// safeReturnTo keeps a return address on this site: a path, with its query,
// that no browser reads as another host. Anything else is the home page.
func safeReturnTo(raw string) string {
	if raw == "" || len(raw) > 512 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	for _, c := range raw {
		// A backslash is a slash to some browsers; a control character can
		// end the header early.
		if c == '\\' || c < 0x20 || c == 0x7f {
			return "/"
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return "/"
	}
	// The fragment stays in the browser; the result code goes in the query.
	u.Fragment, u.RawFragment = "", ""
	q := u.Query()
	q.Del("auth")
	u.RawQuery = q.Encode()
	return u.String()
}

// withCode is return_to with the result of the sign-in in ?auth=.
func withCode(returnTo, code string) string {
	u, err := url.Parse(safeReturnTo(returnTo))
	if err != nil {
		return "/?auth=" + code
	}
	q := u.Query()
	q.Set("auth", code)
	u.RawQuery = q.Encode()
	return u.String()
}
