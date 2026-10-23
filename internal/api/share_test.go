// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/card"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

const shareToken = "123456:share-test"

var shareDay = round.NewDate(2026, time.November, 2)

// shareStore answers only what /api/share asks of the store.
type shareStore struct {
	Store
	scored int
}

func (s *shareStore) Touch(context.Context, tg.User, *tg.Chat) error  { return nil }
func (s *shareStore) UserLang(context.Context, int64) (string, error) { return "ru", nil }
func (s *shareStore) ChatLang(context.Context, int64) (string, error) { return "ru", nil }
func (s *shareStore) ChatZone(context.Context, int64) (string, error) { return "", nil }
func (s *shareStore) Contexts(context.Context, int64) ([]db.Context, error) {
	return nil, nil
}
func (s *shareStore) RoundOn(context.Context, round.Date) (db.Round, bool, error) {
	return db.Round{Date: shareDay, DayNo: 12, PlaceID: 1}, true, nil
}
func (s *shareStore) Answer(context.Context, int64) (db.Answer, error) {
	return db.Answer{Point: geo.Point{Lat: 38.7, Lon: -9.1}, CountryISO: "PT", Continent: "europe"}, nil
}
func (s *shareStore) ScoreDue(context.Context, round.Date, time.Time, func(geo.Point) scoring.Score) (int, error) {
	s.scored++
	return 0, nil
}

type shareMembers struct{}

func (shareMembers) IsMember(context.Context, round.Date, int64, int64) (bool, error) {
	return true, nil
}

type fakeSharer struct {
	can   bool
	err   error
	calls []card.Share
}

func (f *fakeSharer) CanShare() bool { return f.can }
func (f *fakeSharer) Prepare(_ context.Context, sh card.Share) (string, error) {
	f.calls = append(f.calls, sh)
	return "prepared-1", f.err
}

func shareServer(t *testing.T, now time.Time, sharer Sharer) (http.Handler, *shareStore) {
	t.Helper()
	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	store := &shareStore{}
	s := &Server{
		Store: store, InitData: auth.NewVerifier(shareToken), Payloads: auth.NewSigner(shareToken),
		Members: shareMembers{}, Zones: zones, Shares: sharer,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now },
	}
	return s.Handler(), store
}

// signedLaunch signs a launch the way Telegram does.
func signedLaunch(userID int64, startParam string) string {
	fields := map[string]string{
		"auth_date": strconv.FormatInt(time.Now().Unix(), 10),
		"user":      fmt.Sprintf(`{"id":%d,"first_name":"Mia","language_code":"ru"}`, userID),
	}
	if startParam != "" {
		fields["start_param"] = startParam
		fields["chat_type"] = "supergroup"
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
	mac := func(key, msg []byte) []byte {
		h := hmac.New(sha256.New, key)
		h.Write(msg)
		return h.Sum(nil)
	}
	values.Set("hash", hex.EncodeToString(mac(mac([]byte("WebAppData"), []byte(shareToken)), []byte(strings.Join(lines, "\n")))))
	return values.Encode()
}

func postShare(h http.Handler, launch string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/share", nil)
	if launch != "" {
		req.Header.Set("Authorization", "tma "+launch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func answerOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return out
}

// 18:30 UTC on 2 November is 20:30 in Kyiv, where Russian defaults.
var afterKyivReveal = time.Date(2026, time.November, 2, 18, 30, 0, 0, time.UTC)

func TestShareFromADMPreparesThePersonsOwnCard(t *testing.T) {
	sharer := &fakeSharer{can: true}
	h, store := shareServer(t, afterKyivReveal, sharer)
	rec := postShare(h, signedLaunch(777, ""))
	if rec.Code != http.StatusOK || answerOf(t, rec)["id"] != "prepared-1" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(sharer.calls) != 1 || sharer.calls[0] != (card.Share{UserID: 777, ChatID: 777, Date: shareDay}) {
		t.Fatalf("Prepare got %+v", sharer.calls)
	}
	if store.scored != 1 {
		t.Fatalf("the day was scored %d times", store.scored)
	}
}

func TestShareFromAGroupPreparesThatGroupsCard(t *testing.T) {
	sharer := &fakeSharer{can: true}
	h, _ := shareServer(t, afterKyivReveal, sharer)
	start, err := auth.NewSigner(shareToken).Sign(auth.Payload{Date: shareDay, ChatID: -1001234})
	if err != nil {
		t.Fatal(err)
	}
	if rec := postShare(h, signedLaunch(777, start)); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(sharer.calls) != 1 || sharer.calls[0] != (card.Share{UserID: 777, ChatID: -1001234, Date: shareDay}) {
		t.Fatalf("Prepare got %+v", sharer.calls)
	}
}

func TestShareSaysNothingBeforeTheReveal(t *testing.T) {
	sharer := &fakeSharer{can: true}
	h, store := shareServer(t, afterKyivReveal.Add(-time.Hour), sharer)
	rec := postShare(h, signedLaunch(777, ""))
	if rec.Code != http.StatusForbidden || answerOf(t, rec)["error"] != "not_revealed" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(sharer.calls) != 0 || store.scored != 0 {
		t.Fatalf("a card was prepared before the reveal: %+v", sharer.calls)
	}
}

func TestShareNeedsTelegramsSignature(t *testing.T) {
	sharer := &fakeSharer{can: true}
	h, _ := shareServer(t, afterKyivReveal, sharer)
	if rec := postShare(h, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no launch: %d", rec.Code)
	}
	if rec := postShare(h, strings.Replace(signedLaunch(777, ""), "Mia", "Eve", 1)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged launch: %d", rec.Code)
	}
	if len(sharer.calls) != 0 {
		t.Fatal("a forged launch prepared a card")
	}
}

func TestShareIsUnavailableWithoutCardsOrWhenTheyFail(t *testing.T) {
	for name, sharer := range map[string]Sharer{
		"not configured": nil,
		"cannot share":   &fakeSharer{can: false},
		"render failed":  &fakeSharer{can: true, err: errors.New("renderer timed out: http://card-renderer:9222")},
	} {
		h, _ := shareServer(t, afterKyivReveal, sharer)
		rec := postShare(h, signedLaunch(777, ""))
		if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "card-renderer") {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body)
		}
	}
}

func TestShareIsRateLimitedPerPerson(t *testing.T) {
	h, _ := shareServer(t, afterKyivReveal, &fakeSharer{can: true})
	for i := 1; i <= 6; i++ {
		rec := postShare(h, signedLaunch(777, ""))
		want := http.StatusOK
		if i > 5 {
			want = http.StatusTooManyRequests
		}
		if rec.Code != want {
			t.Fatalf("share %d: %d", i, rec.Code)
		}
	}
	if rec := postShare(h, signedLaunch(778, "")); rec.Code != http.StatusOK {
		t.Fatalf("another person was limited: %d", rec.Code)
	}
}
