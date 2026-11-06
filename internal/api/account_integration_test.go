// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/account"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

// signIn gives userID a Telegram account and a browser session for it, as a
// sign-in on the site would, and says the cookie's value.
func (hs *harness) signIn(userID int64, name string) string {
	hs.t.Helper()
	if hs.srv.Accounts == nil {
		store := hs.srv.Store.(*db.Store)
		hs.srv.Accounts = account.New(account.Config{
			Store: store, Key: []byte("flow"), InitData: hs.srv.InitData, Zones: hs.srv.Zones,
			AppURL: "https://maply.test", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Now: func() time.Time { return hs.now },
		})
	}
	ctx := context.Background()
	store := hs.srv.Store.(*db.Store)
	if err := store.TouchPerson(ctx, tg.User{ID: userID, FirstName: name}); err != nil {
		hs.t.Fatal(err)
	}
	acc, _, err := store.SignIn(ctx, db.Identity{Kind: db.KindTelegram, TelegramUserID: userID}, "", db.Rules{
		Open:     func(round.Date) bool { return true },
		Deadline: func(context.Context, int64, round.Date) (time.Time, error) { return hs.now, nil },
	})
	if err != nil {
		hs.t.Fatal(err)
	}
	token := strings.Repeat(string(rune('a'+userID)), 43)
	if err := store.CreateSession(ctx, db.Session{Hash: sha256.Sum256([]byte(token)), AccountID: acc.ID, Via: db.KindTelegram,
		Created: hs.now, Expires: hs.now.Add(account.SessionLife)}); err != nil {
		hs.t.Fatal(err)
	}
	return token
}

// site is a request from the site's own page with the session cookie.
func (hs *harness) site(method, path, session, body string, header ...string) (int, string) {
	hs.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	req.AddCookie(&http.Cookie{Name: account.SessionCookie, Value: session})
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

func (hs *harness) siteExpect(method, path, session, body string, want int, header ...string) string {
	hs.t.Helper()
	status, got := hs.site(method, path, session, body, header...)
	if status != want {
		hs.t.Fatalf("%s %s = %d %s, want %d", method, path, status, got, want)
	}
	return got
}

// A browser signed in with Telegram is the person's Telegram player: one
// answer a day with the bot, the window of every chat they play in, the
// place at once after answering, the names at the usual time, and nothing
// before answering.
func TestATelegramAccountPlaysOnTheSite(t *testing.T) {
	hs := setup(t)
	hs.srv.BotUsername = "maplyin_bot"
	ivan := initData(2, "Ivan", "ru", hs.payload(moscowGroup))
	oksanaMoscow := initData(1, "Oksana", "uk", hs.payload(moscowGroup))
	oksanaDM := initData(1, "Oksana", "uk", "")
	pavelMoscow := initData(6, "Pavel", "ru", hs.payload(moscowGroup))

	// Morning: Oksana and Pavel open the Mini App from the Moscow group, so
	// its 20:00 (17:00 UTC) bounds them. Ivan answers there.
	hs.expect("GET", "/api/today", oksanaMoscow, "", http.StatusOK)
	hs.expect("GET", "/api/today", pavelMoscow, "", http.StatusOK)
	hs.expect("POST", "/api/guess", ivan, `{"lat":55.75,"lon":37.6}`, http.StatusOK)
	oksana := hs.signIn(1, "Oksana")
	pavel := hs.signIn(6, "Pavel")
	presence := func(userID int64) int {
		var n int
		if err := hs.pool.QueryRow(context.Background(), `SELECT count(*) FROM core.maply_presence WHERE telegram_user_id = $1 AND chat_id = 0`, userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Her screen on the site is her DM's: Kyiv, which Ukrainian means,
	// with the bot to play in.
	hs.now = time.Date(2026, time.December, 10, 10, 0, 0, 0, time.UTC)
	body := hs.siteExpect("GET", "/api/today?lang=uk", oksana, "", http.StatusOK)
	var td todayResponse
	if err := json.Unmarshal([]byte(body), &td); err != nil {
		t.Fatal(err)
	}
	if leaks(body) || td.Zone != "Europe/Kyiv" || !td.Deadline.Equal(time.Date(2026, time.December, 10, 17, 0, 0, 0, time.UTC)) || td.Bot != "maplyin_bot" || td.Answer != nil || td.Lang != "uk" {
		t.Fatalf("today on the site = %s", body)
	}
	// Nothing about the place before answering, and no answer from another
	// site's page.
	if body := hs.siteExpect("GET", "/api/result", oksana, "", http.StatusForbidden); leaks(body) || !strings.Contains(body, "not_answered") {
		t.Fatalf("result before answering = %s", body)
	}
	hs.siteExpect("POST", "/api/guess", oksana, `{"lat":49.8,"lon":24}`, http.StatusBadRequest, "Content-Type", "text/plain")
	hs.siteExpect("POST", "/api/guess", oksana, `{"lat":49.8,"lon":24}`, http.StatusBadRequest, "Sec-Fetch-Site", "cross-site")
	hs.siteExpect("GET", "/api/today", "not-a-session", "", http.StatusUnauthorized)
	// A link from another site carries the Lax cookie; the GET would fix
	// the DM's zone from its ?tz= and ?lang=.
	// So does one opened from an app or a letter ("none").
	for _, from := range []string{"cross-site", "same-site", "none"} {
		hs.siteExpect("GET", "/api/today?tz=Pacific/Honolulu&lang=en", oksana, "", http.StatusBadRequest, "Sec-Fetch-Site", from)
		hs.siteExpect("GET", "/api/profile?lang=ja", oksana, "", http.StatusBadRequest, "Sec-Fetch-Site", from)
	}
	// A browser without Fetch Metadata: the link is a navigation, which
	// asks for HTML; maply's script asks for anything.
	navigation := "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	hs.siteExpect("GET", "/api/today?tz=Pacific/Honolulu&lang=en", oksana, "", http.StatusBadRequest, "Sec-Fetch-Site", "", "Accept", navigation)
	hs.siteExpect("GET", "/api/profile", oksana, "", http.StatusBadRequest, "Sec-Fetch-Site", "", "Accept", navigation)
	if zone, err := hs.srv.Store.ChatZone(context.Background(), 1); err != nil || zone == "Pacific/Honolulu" {
		t.Fatal("a link from another site set her DM's zone")
	}

	// She answers on the site: scored at once, with the speed bonus, the
	// place shown, Ivan without his name.
	body = hs.siteExpect("POST", "/api/guess", oksana, `{"lat":49.8,"lon":24}`, http.StatusOK)
	var res resultResponse
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	kyivReveal := time.Date(2026, time.December, 10, 18, 0, 0, 0, time.UTC)
	if res.Phase != "live" || res.Place.Lat != placeLat || res.OpensAt == nil || !res.OpensAt.Equal(kyivReveal) || len(res.Board) != 2 {
		t.Fatalf("her answer on the site = %s", body)
	}
	if strings.Contains(body, "Ivan") {
		t.Fatalf("Ivan is named before the reveal: %s", body)
	}
	var context_ string
	var deadline time.Time
	var speed int
	if err := hs.pool.QueryRow(context.Background(), `SELECT context, deadline_at, pts_speed FROM maply.guess WHERE user_id = 1`).Scan(&context_, &deadline, &speed); err != nil {
		t.Fatal(err)
	}
	if context_ != "site" || !deadline.Equal(time.Date(2026, time.December, 10, 17, 0, 0, 0, time.UTC)) || speed <= 0 {
		t.Fatalf("her answer: context %q, deadline %v, speed %d", context_, deadline, speed)
	}
	// None of this was a DM with the bot, or the morning post would go to her.
	if presence(1) != 0 {
		t.Fatal("the site wrote DM presence")
	}
	// One answer a day: the bot has it, and the site's own game refuses her.
	hs.expect("POST", "/api/guess", oksanaDM, `{"lat":10,"lon":10}`, http.StatusConflict)
	if _, body := hs.site("POST", "/api/site/guess", oksana, `{"lat":10,"lon":10}`); !strings.Contains(body, `"telegram"`) {
		t.Fatalf("a site answer from a Telegram account = %s", body)
	}
	if body := hs.siteExpect("GET", "/api/today?lang=uk", oksana, "", http.StatusOK); !strings.Contains(body, `"answer":{"lat":49.8,"lon":24}`) {
		t.Fatalf("today after answering = %s", body)
	}
	// The Mini App sees the same answer.
	if body := hs.expect("GET", "/api/result", oksanaDM, "", http.StatusOK); !strings.Contains(body, `"phase":"live"`) {
		t.Fatalf("her DM after answering on the site = %s", body)
	}

	// 17:30 UTC: past 20:00 in Moscow. Pavel's DM is on Kyiv, but he plays
	// in the Moscow group, so the site is closed to him too.
	hs.now = time.Date(2026, time.December, 10, 17, 30, 0, 0, time.UTC)
	if body := hs.siteExpect("POST", "/api/guess", pavel, `{"lat":50,"lon":30}`, http.StatusConflict); !strings.Contains(body, "closed") {
		t.Fatalf("Pavel's late answer = %s", body)
	}
	// Without an answer, the place waits for the last reveal anywhere: the
	// site cannot tie him to a chat whose reveal has passed.
	hs.now = time.Date(2026, time.December, 10, 18, 30, 0, 0, time.UTC)
	if body := hs.siteExpect("GET", "/api/result", pavel, "", http.StatusForbidden); leaks(body) || !strings.Contains(body, "2026-12-11T04:00:00Z") {
		t.Fatalf("Pavel's result after his reveals = %s", body)
	}
	// Past her reveal she still sees the others of the world unnamed: Ivan's
	// group is not hers, and the day is not over everywhere.
	if body := hs.siteExpect("GET", "/api/result", oksana, "", http.StatusOK); strings.Contains(body, "Ivan") {
		t.Fatalf("Ivan is named before the day closed: %s", body)
	}

	// The day is over in every zone: names for everyone, the place for
	// Pavel too, the day asked by its date.
	hs.now = time.Date(2026, time.December, 11, 4, 0, 0, 0, time.UTC)
	if body := hs.siteExpect("GET", "/api/result?date=2026-12-10", oksana, "", http.StatusOK); !strings.Contains(body, "Ivan") {
		t.Fatalf("names after the day closed = %s", body)
	}
	hs.siteExpect("GET", "/api/result?date=2026-12-10", pavel, "", http.StatusOK)

	if presence(6) != 0 {
		t.Fatal("the site wrote Pavel's DM presence")
	}
	// Sharing is the Mini App's.
	hs.siteExpect("POST", "/api/share", oksana, `{}`, http.StatusServiceUnavailable)
}

// A guest cookie that names a player an account has taken in opens
// nothing, also on a request that carries some Authorization: the site's
// game and free play take such a request for a new guest, never for the
// account's player.
func TestACookieTheAccountTookInOpensNothing(t *testing.T) {
	hs := freeSetup(t)
	hs.now = time.Date(2026, time.December, 10, 11, 0, 0, 0, time.UTC)
	send := func(method, path, cookie, body string, header ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: playerCookie, Value: cookie})
		}
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, req)
		return rr
	}
	cookieOf := func(rr *httptest.ResponseRecorder) string {
		for _, c := range rr.Result().Cookies() {
			if c.Name == playerCookie {
				return c.Value
			}
		}
		return ""
	}
	// A guest answers the day and starts a free game, then signs in with
	// Google there: the account takes the guest in.
	rr := send("POST", "/api/site/guess?tz=Europe/Kyiv", "", `{"lat":49.9,"lon":24.1}`)
	guest := cookieOf(rr)
	if rr.Code != http.StatusOK || guest == "" {
		t.Fatalf("guest answer = %d %s", rr.Code, rr.Body)
	}
	rr = send("POST", "/api/free/solo", guest, `{"mode":"classic","seconds":0,"rounds":3}`)
	var game struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &game); rr.Code != http.StatusOK || err != nil || game.ID == 0 {
		t.Fatalf("guest solo = %d %s", rr.Code, rr.Body)
	}
	hs.signInGoogle("g-took", guest)

	for _, auth := range []string{"", "Basic eDp5", "tma nonsense"} {
		if rr := send("GET", fmt.Sprintf("/api/free/solo/%d", game.ID), guest, "", "Authorization", auth); rr.Code == http.StatusOK {
			t.Errorf("Authorization %q: the account's game opened by the old cookie: %s", auth, rr.Body)
		}
		// The site's game: the old cookie is a new guest, whose answer is
		// its own, not a second one of the account's player.
		rr := send("POST", "/api/site/guess?tz=Europe/Kyiv", guest, `{"lat":10,"lon":10}`, "Authorization", auth)
		if fresh := cookieOf(rr); rr.Code != http.StatusOK || fresh == "" || fresh == guest {
			t.Errorf("Authorization %q: an answer by the old cookie = %d %s, cookie %q", auth, rr.Code, rr.Body, fresh)
		}
	}
	var n int
	if err := hs.pool.QueryRow(context.Background(), `SELECT count(*) FROM maply.site_guess WHERE player = $1`, guest).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the account's player has %d answers, %v", n, err)
	}
}
