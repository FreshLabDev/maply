// SPDX-License-Identifier: Apache-2.0
package account

import (
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/i18n"
)

func TestReturnToStaysAPathOnTheSite(t *testing.T) {
	for raw, want := range map[string]string{
		"":                             "/",
		"/":                            "/",
		"/days?x=1":                    "/days?x=1",
		"/days?auth=ok&x=1#link=abc":   "/days?x=1",
		"//evil.test/":                 "/",
		"/\\evil.test":                 "/",
		"https://evil.test/":           "/",
		"javascript:alert(1)":          "/",
		"days":                         "/",
		"/a\r\nSet-Cookie: x=1":        "/",
		"/%2F%2Fevil.test":             "/%2F%2Fevil.test",
		"/" + strings.Repeat("a", 600): "/",
	} {
		if got := safeReturnTo(raw); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := withCode("/days?x=1", "ok"); got != "/days?auth=ok&x=1" {
		t.Errorf("withCode = %q", got)
	}
}

func TestAFlowIsSignedAndShortLived(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc := New(Config{Key: []byte("k1"), Now: func() time.Time { return now }})
	f := flow{Provider: "google", Intent: intentLogin, State: "s", Nonce: "n", Verifier: "v", ReturnTo: "/", At: now.Unix()}
	value, err := svc.sign(f)
	if err != nil {
		t.Fatal(err)
	}
	read := func(svc *Service, value string) error {
		r := httptest.NewRequest("GET", "/auth/google/callback", nil)
		r.AddCookie(&http.Cookie{Name: FlowCookie, Value: value})
		_, err := svc.readFlow(r)
		return err
	}
	if err := read(svc, value); err != nil {
		t.Fatalf("its own flow: %v", err)
	}
	body, sig, _ := strings.Cut(value, ".")
	var raw map[string]any
	b, _ := base64.RawURLEncoding.DecodeString(body)
	_ = json.Unmarshal(b, &raw)
	raw["a"] = "someone-else"
	changed, _ := json.Marshal(raw)
	for name, v := range map[string]string{
		"another key":    mustSign(t, New(Config{Key: []byte("k2"), Now: svc.cfg.Now}), f),
		"a changed body": base64.RawURLEncoding.EncodeToString(changed) + "." + sig,
		"no signature":   body,
		"empty":          "",
	} {
		if err := read(svc, v); err == nil {
			t.Errorf("%s: read", name)
		}
	}
	later := New(Config{Key: []byte("k1"), Now: func() time.Time { return now.Add(FlowLife + time.Second) }})
	if err := read(later, value); err == nil {
		t.Error("a flow older than its life was read")
	}
}

func mustSign(t *testing.T, svc *Service, f flow) string {
	t.Helper()
	v, err := svc.sign(f)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTelegramClaimsMakeAUser(t *testing.T) {
	for _, c := range []struct {
		name   string
		claims string
		known  bool
		first  string
		last   string
		lang   string
	}{
		{"given and family", `{"id":42,"given_name":"Anya","family_name":"Petrova","name":"Anya Petrova","locale":"uk-UA"}`, true, "Anya", "Petrova", "uk"},
		{"one word", `{"id":"42","name":"Anya"}`, true, "Anya", "", ""},
		{"two words, known", `{"id":42,"name":"Anya Petrova"}`, true, "", "", ""},
		{"two words, new", `{"id":42,"name":"Anya Petrova","locale":"pt_BR"}`, false, "Anya Petrova", "", "pt"},
	} {
		var tc telegramClaims
		if err := json.Unmarshal([]byte(c.claims), &tc); err != nil {
			t.Fatal(err)
		}
		u, err := tc.user(c.known)
		if err != nil || u.ID != 42 || u.FirstName != c.first || u.LastName != c.last || u.LanguageCode != c.lang {
			t.Errorf("%s: %+v %v", c.name, u, err)
		}
	}
	for _, bad := range []string{`{}`, `{"id":0}`, `{"id":-5}`, `{"id":"x"}`, `{"id":1.5}`} {
		var tc telegramClaims
		_ = json.Unmarshal([]byte(bad), &tc)
		if _, err := tc.user(true); err == nil {
			t.Errorf("%s gave a user", bad)
		}
	}
}

// The privacy page names the owner's Telegram for erasing answers, as a link,
// in every language, and its text stays text.
func TestPrivacyGivesTheOwnersTelegram(t *testing.T) {
	svc := New(Config{Key: []byte("k1")})
	for _, lang := range i18n.Codes() {
		rec := httptest.NewRecorder()
		svc.Privacy().ServeHTTP(rec, httptest.NewRequest("GET", "/privacy?lang="+lang, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || strings.Count(body, `<a href="https://t.me/amtiyo" dir="ltr">@amtiyo</a>`) != 1 ||
			strings.Contains(body, "{contact}") || strings.Contains(body, "github.com") || strings.Contains(body, "&lt;a") {
			t.Errorf("privacy in %s = %d, the contact is missing or broken", lang, rec.Code)
		}
		// Navigation is not CSP's to stop, and nothing here may name a target.
		if csp := rec.Header().Get("Content-Security-Policy"); strings.Contains(csp, "navigate-to") || strings.Contains(body, "target=") {
			t.Errorf("privacy in %s: CSP %q or a target on a link", lang, csp)
		}
	}
}

// The page covers what other players see of free play and the public link,
// in the words the app's own switches and tables use.
func TestPrivacyNamesTheAppsOwnWords(t *testing.T) {
	svc := New(Config{Key: []byte("k1")})
	for _, lang := range i18n.Codes() {
		rec := httptest.NewRecorder()
		svc.Privacy().ServeHTTP(rec, httptest.NewRequest("GET", "/privacy?lang="+lang, nil))
		body := html.UnescapeString(rec.Body.String())
		for _, key := range []string{"app.prof.hide_me", "app.prof.anon", "app.guest"} {
			if word := i18n.T(lang, key); !strings.Contains(body, word) {
				t.Errorf("privacy in %s does not say %q (%s)", lang, word, key)
			}
		}
		if !strings.Contains(body, "/p/…") {
			t.Errorf("privacy in %s does not name the public link", lang)
		}
	}
}

// A request that signs for itself is nobody on the site, whatever its
// cookies: the handlers would otherwise take the guest cookie's player as it
// is, even one an account has taken in, by sending any Authorization.
func TestARequestThatSignsForItselfIsNobodyOnTheSite(t *testing.T) {
	svc := New(Config{Key: []byte("k1")}) // no store: the middleware must not ask it
	for _, auth := range []string{"tma query_id=x", "Basic eDp5", "x"} {
		req := httptest.NewRequest("GET", "/api/site/today", nil)
		req.Header.Set("Authorization", auth)
		req.AddCookie(&http.Cookie{Name: GuestCookie, Value: "6f1c2a7e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"})
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: strings.Repeat("a", 43)})
		var got Viewer
		var ok bool
		svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok = ViewerFrom(r.Context())
		})).ServeHTTP(httptest.NewRecorder(), req)
		if !ok || got.Player != "" || got.SignedIn() || got.TelegramUserID != 0 {
			t.Errorf("Authorization %q: viewer %+v, ok %v", auth, got, ok)
		}
	}
}
