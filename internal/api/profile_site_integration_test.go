// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/account"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

// signInGoogle gives the site player guest a Google account and a browser
// session for it, and says the cookie's value and the account.
func (hs *harness) signInGoogle(sub, guest string) (string, string) {
	hs.t.Helper()
	store := hs.srv.Store.(*db.Store)
	if hs.srv.Accounts == nil {
		hs.srv.Accounts = account.New(account.Config{
			Store: store, Key: []byte("flow"), InitData: hs.srv.InitData, Zones: hs.srv.Zones,
			AppURL: "https://maply.test", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Now: func() time.Time { return hs.now },
		})
	}
	ctx := context.Background()
	acc, _, err := store.SignIn(ctx, db.Identity{Kind: db.KindGoogle, GoogleSub: sub}, guest, db.Rules{})
	if err != nil {
		hs.t.Fatal(err)
	}
	token := strings.Repeat("g", 43)
	if err := store.CreateSession(ctx, db.Session{Hash: sha256.Sum256([]byte(token)), AccountID: acc.ID, Via: db.KindGoogle,
		Created: hs.now, Expires: hs.now.Add(account.SessionLife)}); err != nil {
		hs.t.Fatal(err)
	}
	return token, acc.ID
}

// Signed in on the site with Telegram, the profile is the Telegram
// player's, as in the bot: every scored day, today's too, the settings, and
// the mini profile knows its owner. Its writes take only maply's own JSON
// and leave core alone.
func TestProfileOfATelegramAccountOnTheSite(t *testing.T) {
	hs := setup(t)
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	d2 := hs.seedPast(2, "EG", "africa", pastAnswer{user: 1, km: 400, points: 2500, bonus: 300}, pastAnswer{user: 2, km: 100, points: 3800, bonus: 800})
	hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500})
	oksanaDM := initData(1, "Oksana", "uk", "")
	hs.expect("GET", "/api/today", oksanaDM, "", http.StatusOK)
	hs.expect("POST", "/api/guess", oksanaDM, `{"lat":40,"lon":-3}`, http.StatusOK)
	oksana := hs.signIn(1, "Oksana")
	presence := func() int {
		var n int
		if err := hs.pool.QueryRow(context.Background(), `SELECT count(*) FROM core.maply_presence WHERE telegram_user_id = 1`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := presence()

	own := decode[ownView](t, hs.siteExpect("GET", "/api/profile?lang=uk", oksana, "", http.StatusOK))
	if own.Name != "Oksana" || own.Guest || own.Settings == nil || own.Ranked.Days != 3 || own.Ranked.Percentile == nil {
		t.Fatalf("her profile on the site = %+v", own)
	}
	if mini := hs.siteExpect("GET", "/api/profile/who?id="+hs.srv.handles().Of(1, "day:x"), oksana, "", http.StatusOK); !strings.Contains(mini, `"me":true`) {
		t.Fatalf("her own handle = %s", mini)
	}

	// Another site's page cannot ask, read or write: the session is a cookie.
	hs.siteExpect("GET", "/api/profile", oksana, "", http.StatusBadRequest, "Sec-Fetch-Site", "cross-site")
	hs.siteExpect("POST", "/api/profile/settings", oksana, `{"hide_me":true}`, http.StatusBadRequest, "Content-Type", "text/plain")
	hs.siteExpect("POST", "/api/profile/public", oksana, `{"on":true}`, http.StatusBadRequest, "Sec-Fetch-Site", "same-site")

	hs.siteExpect("POST", "/api/profile/settings", oksana, `{"hide_me":true}`, http.StatusOK)
	pub := decode[ownView](t, `{"settings":`+hs.siteExpect("POST", "/api/profile/public", oksana, `{"on":true}`, http.StatusOK)+`}`)
	if !strings.HasPrefix(pub.Settings.Public, "/p/") || !pub.Settings.HideMe {
		t.Fatalf("settings from the site = %+v", pub.Settings)
	}
	// The bot's profile has the same settings: one player.
	if inApp := decode[ownView](t, hs.expect("GET", "/api/profile", oksanaDM, "", http.StatusOK)); !inApp.Settings.HideMe || inApp.Settings.Public != pub.Settings.Public {
		t.Fatalf("settings in the Mini App = %+v", inApp.Settings)
	}
	if after := presence(); after != before {
		t.Fatalf("the site's profile wrote presence: %d, then %d", before, after)
	}
	// Hidden, she is "Anonymous" on the site's day page, except to a
	// group-mate signed in with Telegram.
	ivan := hs.signIn(2, "Ivan")
	if body := hs.siteExpect("GET", "/api/day?date="+d2.String()+"&lang=en", ivan, "", http.StatusOK); !strings.Contains(body, "Oksana") {
		t.Fatalf("her group-mate on the site lost her name: %s", body)
	}
	req := httptest.NewRequest("GET", "/api/day?date="+d2.String()+"&lang=en", nil)
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "Oksana") || !strings.Contains(rr.Body.String(), "Anonymous") {
		t.Fatalf("a guest sees the day = %d %s", rr.Code, rr.Body.String())
	}
}

// Signed in with Google alone, the profile is the account's site game over
// every browser it has taken in: days over everywhere, one row a day, the
// account's name, no settings. A cookie of a player the account took in is
// a new guest.
func TestProfileOfAGoogleAccount(t *testing.T) {
	hs := setup(t)
	const (
		home = "11111111-1111-4111-8111-111111111111"
		work = "22222222-2222-4222-8222-222222222222"
	)
	hs.seedPast(2, "EG", "africa", pastAnswer{player: home, km: 400, points: 2500, bonus: 300})
	d1 := hs.seedPast(1, "JP", "asia", pastAnswer{player: home, km: 5, points: 4950, bonus: 1500})
	// The other browser answered the same day later, before the two met in
	// the account: a day over everywhere keeps both rows.
	ctx := context.Background()
	if _, err := hs.pool.Exec(ctx, `INSERT INTO maply.site_player (id) VALUES ($1)`, work); err != nil {
		t.Fatal(err)
	}
	at := d1.Reveal(time.UTC).Add(-time.Hour)
	if _, err := hs.pool.Exec(ctx, `
		INSERT INTO maply.site_guess (date, player, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus)
		VALUES ($1, $2, 11, 21, $3, $4, 3000, 100, 0)`, d1.Time(), work, at, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := hs.pool.Exec(ctx, `UPDATE maply.site_player SET name = 'Lena' WHERE id = $1`, home); err != nil {
		t.Fatal(err)
	}
	lena, acc := hs.signInGoogle("g-lena", home)
	store := hs.srv.Store.(*db.Store)
	if _, err := store.AdoptGuest(ctx, acc, work, db.Rules{Open: func(round.Date) bool { return false }}); err != nil {
		t.Fatal(err)
	}

	type siteView struct {
		ownView
		Site bool `json:"site"`
	}
	got := decode[siteView](t, hs.siteExpect("GET", "/api/profile?lang=en", lena, "", http.StatusOK))
	if !got.Site || got.Guest || got.Name != "Lena" || got.Settings != nil ||
		got.Ranked.Days != 2 || got.Ranked.Points != 2500+4950 || len(got.Ranked.History) != 2 {
		t.Fatalf("the account's profile = %+v", got)
	}
	hs.siteExpect("POST", "/api/profile/settings", lena, `{"hide_me":true}`, http.StatusUnauthorized)
	hs.siteExpect("POST", "/api/profile/public", lena, `{"on":true}`, http.StatusUnauthorized)

	// The work browser's old cookie no longer opens those days.
	req := httptest.NewRequest("GET", "/api/profile", nil)
	req.AddCookie(&http.Cookie{Name: playerCookie, Value: work})
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	if g := decode[siteView](t, rr.Body.String()); rr.Code != http.StatusOK || !g.Guest || g.Ranked.Days != 0 {
		t.Fatalf("a cookie the account took in = %d %s", rr.Code, rr.Body.String())
	}
}

// A Telegram account keeps the days its guest played on the site before the
// sign-in: a closed day stays a site row, and the profile counts it on a day
// with no Telegram answer, in the bot, on the site and for others alike.
func TestATelegramAccountKeepsItsGuestDays(t *testing.T) {
	hs := setup(t)
	const guest = "33333333-3333-4333-8333-333333333333"
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	hs.seedPast(3, "FR", "europe", pastAnswer{player: guest, km: 50, points: 4000, bonus: 800})
	hs.seedPast(2, "EG", "africa", pastAnswer{user: 1, km: 400, points: 2500, bonus: 300}, pastAnswer{player: guest, km: 10, points: 4900, bonus: 1500})
	hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500, speed: 200})
	oksana := hs.signIn(1, "Oksana")
	ctx := context.Background()
	store := hs.srv.Store.(*db.Store)
	acc, ok, err := store.AccountOf(ctx, db.Identity{Kind: db.KindTelegram, TelegramUserID: 1})
	if err != nil || !ok {
		t.Fatalf("account %v %v", ok, err)
	}
	if _, err := store.AdoptGuest(ctx, acc.ID, guest, db.Rules{Open: func(round.Date) bool { return false }}); err != nil {
		t.Fatal(err)
	}

	// Three days: the guest's alone, the Telegram answer where both played,
	// and the Telegram one with its speed.
	want := 4000 + 2500 + 4950 + 200
	inApp := decode[ownView](t, hs.expect("GET", "/api/profile", initData(1, "Oksana", "uk", ""), "", http.StatusOK))
	if inApp.Ranked.Days != 3 || inApp.Ranked.Points != want || len(inApp.Ranked.History) != 3 || inApp.Ranked.Percentile == nil {
		t.Fatalf("in the Mini App = %+v", inApp.Ranked)
	}
	onSite := decode[ownView](t, hs.siteExpect("GET", "/api/profile?lang=uk", oksana, "", http.StatusOK))
	if onSite.Ranked.Days != 3 || onSite.Ranked.Points != want {
		t.Fatalf("on the site = %+v", onSite.Ranked)
	}
	mini := decode[miniView](t, hs.expect("GET", "/api/profile/who?id="+hs.srv.handles().Of(1, "day:x"), initData(2, "Ivan", "ru", ""), "", http.StatusOK))
	if mini.Days != 3 {
		t.Fatalf("her mini profile = %+v", mini)
	}
}

// A guest's answer that a Telegram account took in stays off the person's
// own profile until the day is over everywhere: before that its place
// would tell them where to pin in a chat whose reveal is ahead. A row the
// guest gave after the first reveal never counts. Found by the days
// session's review.
func TestATakenGuestAnswerWaitsForTheDayToClose(t *testing.T) {
	hs := setup(t)
	ctx := context.Background()
	store := hs.srv.Store.(*db.Store)
	// Aleś speaks Belarusian (DM on Minsk, 17:00 UTC) and plays only in the
	// Kyiv group (18:00 UTC). He never wrote to the bot.
	hs.srv.Members.(members)[[2]int64{kyivGroup, 7}] = true
	inGroup := initData(7, "Aleś", "be", hs.payload(kyivGroup))
	hs.now = time.Date(2026, time.December, 10, 9, 0, 0, 0, time.UTC)
	hs.expect("GET", "/api/today", inGroup, "", http.StatusOK)

	send := func(method, path, cookie, session, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: playerCookie, Value: cookie})
		}
		if session != "" {
			req.AddCookie(&http.Cookie{Name: account.SessionCookie, Value: session})
		}
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, req)
		return rr
	}
	// 09:00: a guest answers blindly on the site, on Minsk's clock.
	rr := send("POST", "/api/site/guess?tz=Europe/Minsk", "", "", `{"lat":10,"lon":10}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("guest guess = %d %s", rr.Code, rr.Body.String())
	}
	var guest string
	for _, c := range rr.Result().Cookies() {
		if c.Name == playerCookie {
			guest = c.Value
		}
	}

	// 17:05: the site row is past its deadline and scored by any poll.
	hs.now = time.Date(2026, time.December, 10, 17, 5, 0, 0, time.UTC)
	rd, _, err := store.RoundOn(ctx, hs.date)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := store.Answer(ctx, rd.PlaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := hs.srv.scoreDay(ctx, hs.date, answer, hs.now); err != nil {
		t.Fatal(err)
	}
	// He signs in on the site with Telegram and takes the guest's answers.
	session := hs.signIn(7, "Aleś")
	if rr := send("POST", "/api/account/adopt", guest, session, `{"take":true}`); rr.Code != http.StatusOK {
		t.Fatalf("adopt = %d %s", rr.Code, rr.Body.String())
	}
	place := func() bool {
		t.Helper()
		rr := send("GET", "/api/profile?lang=be", "", session, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("profile = %d %s", rr.Code, rr.Body.String())
		}
		return strings.Contains(rr.Body.String(), "Львоў") || strings.Contains(rr.Body.String(), `"UA"`)
	}
	if place() {
		t.Fatal("the own profile names today's place while the Kyiv group still takes answers")
	}
	// In the Mini App too, from the group.
	if _, body := hs.do("GET", "/api/profile", inGroup, ""); strings.Contains(body, `"UA"`) {
		t.Fatalf("the Mini App's profile names today's place: %s", body)
	}

	// Over everywhere, the guest's day is his.
	hs.now = hs.srv.Zones.LastReveal(hs.date).Add(time.Minute)
	if !place() {
		t.Error("the day over everywhere left out the answer his account took in")
	}
	// A guest's answer given after the first reveal never is.
	if _, err := hs.pool.Exec(ctx, `UPDATE maply.site_guess SET late = true WHERE date = $1`, hs.date.Time()); err != nil {
		t.Fatal(err)
	}
	if place() {
		t.Error("a late site answer counts on the Telegram person's profile")
	}
}
