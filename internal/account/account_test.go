// SPDX-License-Identifier: Apache-2.0
package account

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
