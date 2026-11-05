// SPDX-License-Identifier: Apache-2.0
package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/round"
)

const token = "123456:test-token"

// sign builds initData the way Telegram does, for a given token.
func sign(t *testing.T, botToken string, fields map[string]string) string {
	t.Helper()
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
	secret := mac([]byte("WebAppData"), []byte(botToken))
	values.Set("hash", hex.EncodeToString(mac(secret, []byte(strings.Join(lines, "\n")))))
	return values.Encode()
}

var now = time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)

func fields(authDate time.Time) map[string]string {
	return map[string]string{
		"auth_date":     strconv.FormatInt(authDate.Unix(), 10),
		"user":          `{"id":42,"first_name":"Олеся","language_code":"uk"}`,
		"start_param":   "abc",
		"chat_type":     "supergroup",
		"chat_instance": "-77",
		"signature":     "ed25519-signature-is-signed-too",
	}
}

func verifier() *Verifier {
	v := NewVerifier(token)
	v.now = func() time.Time { return now }
	return v
}

func TestVerifyAcceptsTelegramSignedData(t *testing.T) {
	got, err := verifier().Verify(sign(t, token, fields(now.Add(-time.Hour))))
	if err != nil {
		t.Fatal(err)
	}
	if got.User.ID != 42 || got.User.FirstName != "Олеся" || got.StartParam != "abc" || got.ChatType != "supergroup" {
		t.Fatalf("got %+v", got)
	}
}

func TestVerifyRejectsForgery(t *testing.T) {
	good := sign(t, token, fields(now.Add(-time.Hour)))
	values, _ := url.ParseQuery(good)
	values.Set("user", `{"id":43,"first_name":"Someone else"}`)
	tampered := values.Encode()

	for name, raw := range map[string]string{
		"another bot's token": sign(t, "654321:other", fields(now.Add(-time.Hour))),
		"edited user":         tampered,
		"no hash":             strings.ReplaceAll(good, "hash=", "nohash="),
		"short hash":          url.Values{"auth_date": {"1"}, "hash": {"abcd"}}.Encode(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier().Verify(raw); !errors.Is(err, ErrBadSignature) {
				t.Fatalf("err = %v, want ErrBadSignature", err)
			}
		})
	}
}

func TestVerifyRejectsRepeatedField(t *testing.T) {
	raw := sign(t, token, fields(now)) + "&user=" + url.QueryEscape(`{"id":43}`)
	if _, err := verifier().Verify(raw); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestVerifyRejectsStaleAuthDate(t *testing.T) {
	for name, at := range map[string]time.Time{
		"older than a day":    now.Add(-MaxInitDataAge - time.Second),
		"from the far future": now.Add(time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier().Verify(sign(t, token, fields(at))); !errors.Is(err, ErrStale) {
				t.Fatalf("err = %v, want ErrStale", err)
			}
		})
	}
	if _, err := verifier().Verify(sign(t, token, fields(now.Add(-MaxInitDataAge+time.Minute)))); err != nil {
		t.Fatalf("a day-old launch is refused: %v", err)
	}
}

func TestPayloadRoundTrip(t *testing.T) {
	s := NewSigner(token)
	want := Payload{Date: round.NewDate(2026, time.November, 2), ChatID: -1001234567890}
	raw, err := s.Sign(want)
	if err != nil {
		t.Fatal(err)
	}
	// Telegram allows A-Z, a-z, 0-9, _ and - in startapp, up to 512 characters;
	// the plan keeps it within 64.
	if len(raw) > 64 || strings.Trim(raw, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
		t.Fatalf("payload %q is not a valid startapp value", raw)
	}
	got, err := s.Verify(raw)
	if err != nil || got != want {
		t.Fatalf("Verify = %+v, %v; want %+v", got, err, want)
	}
}

func TestPayloadForAnotherChatIsRejected(t *testing.T) {
	s := NewSigner(token)
	raw, err := s.Sign(Payload{Date: round.NewDate(2026, time.November, 2), ChatID: -1001})
	if err != nil {
		t.Fatal(err)
	}
	buf, _ := base64.RawURLEncoding.DecodeString(raw)
	// Point the signed payload at another chat, and at another day.
	otherChat := append([]byte(nil), buf...)
	otherChat[10] ^= 0x02
	otherDay := append([]byte(nil), buf...)
	otherDay[2]++
	for name, forged := range map[string]string{
		"another chat":        base64.RawURLEncoding.EncodeToString(otherChat),
		"another day":         base64.RawURLEncoding.EncodeToString(otherDay),
		"signed by other bot": mustSign(t, NewSigner("654321:other"), Payload{Date: round.NewDate(2026, time.November, 2), ChatID: -1001}),
		"empty":               "",
		"not base64":          "!!!",
		"truncated":           raw[:20],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Verify(forged); !errors.Is(err, ErrBadPayload) {
				t.Fatalf("Verify(%q) err = %v, want ErrBadPayload", forged, err)
			}
		})
	}
}

func mustSign(t *testing.T, s *Signer, p Payload) string {
	t.Helper()
	raw, err := s.Sign(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPayloadOutsideTheEpochIsRefused(t *testing.T) {
	if _, err := NewSigner(token).Sign(Payload{Date: round.NewDate(2025, time.December, 31)}); !errors.Is(err, ErrBadPayload) {
		t.Fatalf("err = %v", err)
	}
}

type fakeMembers struct {
	status map[int64]string
	gone   map[int64]bool // restricted and no longer in the group
	calls  int
	err    error
}

func (f *fakeMembers) CallGet(_ context.Context, method string, values url.Values, out any) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	userID, _ := strconv.ParseInt(values.Get("user_id"), 10, 64)
	if method != "getChatMember" || values.Get("chat_id") != "-1001" {
		return fmt.Errorf("unexpected %s %v", method, values)
	}
	// The answer as Telegram sends it, through the same decoding.
	member := map[string]any{"status": f.status[userID]}
	if f.status[userID] == "restricted" {
		member["is_member"] = !f.gone[userID]
	}
	raw, _ := json.Marshal(map[string]any{"ok": true, "result": member})
	return json.Unmarshal(raw, out)
}

func TestMembersAreCachedForTheDay(t *testing.T) {
	ctx := context.Background()
	fake := &fakeMembers{
		status: map[int64]string{1: "member", 2: "left", 3: "restricted", 4: "kicked", 5: "restricted"},
		gone:   map[int64]bool{5: true},
	}
	m := NewMembers(fake)
	clock := time.Date(2026, time.November, 2, 9, 0, 0, 0, time.UTC)
	m.Now = func() time.Time { return clock }
	day := round.NewDate(2026, time.November, 2)
	for user, want := range map[int64]bool{1: true, 2: false, 3: true, 4: false, 5: false} {
		got, err := m.IsMember(ctx, day, -1001, user)
		if err != nil || got != want {
			t.Fatalf("IsMember(%d) = %v, %v; want %v", user, got, err, want)
		}
	}
	if _, err := m.IsMember(ctx, day, -1001, 1); err != nil || fake.calls != 5 {
		t.Fatalf("second ask on the same day called Telegram again: %d calls", fake.calls)
	}
	// A refusal stands for a minute: someone who joins later plays that day.
	fake.status[2] = "member"
	if ok, _ := m.IsMember(ctx, day, -1001, 2); ok || fake.calls != 5 {
		t.Fatalf("a refusal was asked again at once: %v, %d calls", ok, fake.calls)
	}
	clock = clock.Add(2 * time.Minute)
	if ok, _ := m.IsMember(ctx, day, -1001, 2); !ok || fake.calls != 6 {
		t.Fatalf("a refusal outlived its minute: %v, %d calls", ok, fake.calls)
	}
	if _, err := m.IsMember(ctx, day.AddDays(1), -1001, 1); err != nil || fake.calls != 7 {
		t.Fatalf("a new day reused yesterday's answer: %d calls", fake.calls)
	}
	// Around midnight UTC some groups are on the new date and some on the
	// old: asking for both does not empty the other's answers.
	for range 3 {
		m.IsMember(ctx, day, -1001, 1)
		m.IsMember(ctx, day.AddDays(1), -1001, 1)
	}
	if fake.calls != 7 {
		t.Fatalf("two dates in turn called Telegram again: %d calls", fake.calls)
	}

	fake.err = errors.New("telegram down")
	if _, err := m.IsMember(ctx, day.AddDays(1), -1001, 9); err == nil {
		t.Fatal("an error became a cached answer")
	}
	fake.err = nil
	fake.status[9] = "member"
	if ok, err := m.IsMember(ctx, day.AddDays(1), -1001, 9); err != nil || !ok {
		t.Fatalf("after the error: %v, %v", ok, err)
	}
}

func TestCardTokenAndStartappPayloadDoNotOpenEachOther(t *testing.T) {
	s := NewSigner(token)
	want := Payload{Date: round.NewDate(2026, time.November, 2), ChatID: -1001234567890}
	card, err := s.SignCard(want)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.VerifyCard(card); err != nil || got != want {
		t.Fatalf("VerifyCard = %+v, %v; want %+v", got, err, want)
	}
	start, err := s.Sign(want)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.VerifyCard(start); !errors.Is(err, ErrBadPayload) {
		t.Fatalf("a startapp payload opened a card: %v", err)
	}
	if _, err := s.Verify(card); !errors.Is(err, ErrBadPayload) {
		t.Fatalf("a card token opened a round: %v", err)
	}
}
