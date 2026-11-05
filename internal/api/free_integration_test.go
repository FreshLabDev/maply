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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/account"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// freePlaces are the free pool of the HTTP test: none of their numbers may
// show before an answer.
var freePlaces = []struct {
	key, iso, continent string
	lat, lon            float64
}{
	{"FreeKeyAaaaaaaaaa01", "FR", "europe", 48.8566, 2.3522},
	{"FreeKeyBbbbbbbbbb02", "JP", "asia", 35.6762, 139.6503},
	{"FreeKeyCccccccccc03", "PE", "south_america", -12.0464, -77.0428},
	{"FreeKeyDddddddddd04", "KE", "africa", -1.2921, 36.8219},
}

type fakeInvites struct {
	mu   sync.Mutex
	sent []telegram.PreparedText
}

func (f *fakeInvites) SavePreparedText(_ context.Context, _ int64, t telegram.PreparedText) (telegram.PreparedMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, t)
	return telegram.PreparedMessage{ID: "prep-" + t.ID}, nil
}

func freeSetup(t *testing.T) *harness {
	hs := setup(t)
	ctx := context.Background()
	if _, err := hs.pool.Exec(ctx, `TRUNCATE maply.free_game CASCADE`); err != nil {
		t.Fatal(err)
	}
	for i, p := range freePlaces {
		if _, err := hs.pool.Exec(ctx, `
			INSERT INTO maply.place (status, pool, source, source_id, license, kind, lat, lon, image_key,
			                         country_iso, continent, city, difficulty, reviewed_at)
			VALUES ('approved', 'free', 'panoramax', $1, 'CC-BY-SA-4.0', 'panorama', $2, $3, $4, $5, $6, '{"en":"Freetown"}', 2, now())`,
			fmt.Sprint("free-src-", i), p.lat, p.lon, p.key, p.iso, p.continent); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hs.srv.ImagesDir, p.key), []byte("\xff\xd8\xff\xe0 free "+p.iso), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	countries, err := geo.World()
	if err != nil {
		t.Fatal(err)
	}
	store := hs.srv.Store.(*db.Store)
	hs.srv.Countries = countries
	hs.srv.People = store
	hs.srv.BotUsername = "maplyin_bot"
	hs.srv.Free = &free.Service{Store: store, Countries: countries, ImageKey: []byte("free-test"),
		Now: func() time.Time { return hs.now }, Log: hs.srv.Log}
	return hs
}

// freeLeaks reports whether a response says where a free place is.
func freeLeaks(body string) string {
	for _, p := range freePlaces {
		for _, s := range []string{fmt.Sprint(p.lat), fmt.Sprint(p.lon), p.key, `"` + p.iso + `"`} {
			if strings.Contains(body, s) {
				return s
			}
		}
	}
	for _, s := range []string{"Freetown", "free-src-", "panoramax", `"lat"`} {
		if strings.Contains(body, s) {
			return s
		}
	}
	return ""
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return v
}

func TestFreeSoloOverHTTP(t *testing.T) {
	hs := freeSetup(t)
	oksana := initData(1, "Oksana", "uk", "")
	home := decode[free.Home](t, hs.expect("GET", "/api/free", oksana, "", 200))
	if home.Last != free.DefaultSolo || home.Open != nil {
		t.Fatalf("home: %+v", home)
	}
	hs.expect("POST", "/api/free/solo", oksana, `{"mode":"classic","seconds":45,"rounds":5}`, 400)
	g := decode[free.SoloView](t, hs.expect("POST", "/api/free/solo", oksana, `{"mode":"classic","seconds":30,"rounds":3}`, 200))
	path := fmt.Sprintf("/api/free/solo/%d", g.ID)
	// Someone else cannot read it.
	hs.expect("GET", path, initData(2, "Ivan", "ru", ""), "", 403)
	body := hs.expect("POST", path+"/start", oksana, `{"n":1}`, 200)
	if s := freeLeaks(body); s != "" {
		t.Fatalf("start says %q: %s", s, body)
	}
	r := decode[free.RoundView](t, body)
	// The picture comes under the round's address, from the place's file.
	status, img := hs.do("GET", r.Image, "", "")
	if status != 200 || !strings.HasPrefix(img, "\xff\xd8\xff\xe0 free ") {
		t.Fatalf("image %d %q", status, img)
	}
	if status, _ := hs.do("GET", strings.Replace(r.Image, "/img/", "/img/A", 1), "", ""); status != 404 {
		t.Fatalf("a forged image: %d", status)
	}
	// The answer comes with the place, and the place is the picture's.
	iso := strings.TrimPrefix(img, "\xff\xd8\xff\xe0 free ")
	var lat, lon float64
	for _, p := range freePlaces {
		if p.iso == iso {
			lat, lon = p.lat, p.lon
		}
	}
	hs.expect("POST", path+"/guess", oksana, `{"n":1,"lat":91,"lon":0}`, 400)
	got := decode[free.SoloView](t, hs.expect("POST", path+"/guess", oksana, fmt.Sprintf(`{"n":1,"lat":%f,"lon":%f}`, lat, lon), 200))
	if len(got.Done) != 1 || got.Done[0].Place.Country != iso || got.Done[0].Guess.Distance != 3500 {
		t.Fatalf("answer: %+v", got.Done)
	}
	hs.expect("POST", path+"/guess", oksana, fmt.Sprintf(`{"n":1,"lat":%f,"lon":%f}`, lat, lon), 409)
	// Reading the game again does not tell place 2 before it is started.
	body = hs.expect("GET", path, oksana, "", 200)
	if !strings.Contains(body, `"next":2`) || strings.Count(body, `"country":`) != 2 {
		t.Fatalf("game after one place: %s", body)
	}
}

func TestFreeSiteGuest(t *testing.T) {
	hs := freeSetup(t)
	post := func(path, ctype, cookie, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, req)
		return rr
	}
	if rr := post("/api/free/solo", "text/plain", "", `{}`); rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a form from elsewhere: %d", rr.Code)
	}
	rr := post("/api/free/solo", "application/json", "", `{"mode":"classic","seconds":0,"rounds":3}`)
	if rr.Code != 200 {
		t.Fatalf("guest solo: %d %s", rr.Code, rr.Body)
	}
	cookie := rr.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != playerCookie {
		t.Fatalf("cookie: %+v", cookie)
	}
	g := decode[free.SoloView](t, rr.Body.String())
	jar := cookie[0].Name + "=" + cookie[0].Value
	if rr := post(fmt.Sprintf("/api/free/solo/%d/start", g.ID), "application/json", jar, `{"n":1}`); rr.Code != 200 {
		t.Fatalf("guest start: %d %s", rr.Code, rr.Body)
	}
	// Without the cookie the game is not theirs.
	if rr := post(fmt.Sprintf("/api/free/solo/%d/guess", g.ID), "application/json", "", `{"n":1,"skip":true}`); rr.Code != 403 {
		t.Fatalf("guess without the cookie: %d", rr.Code)
	}
	if rr := post(fmt.Sprintf("/api/free/solo/%d/guess", g.ID), "application/json", jar, `{"n":1,"skip":true}`); rr.Code != 200 {
		t.Fatalf("guest gives up: %d %s", rr.Code, rr.Body)
	}
	if rr := post("/api/party", "application/json", jar, `{"mode":"classic","seconds":60,"rounds":5}`); rr.Code != 403 || !strings.Contains(rr.Body.String(), "telegram_only") {
		t.Fatalf("guest party: %d %s", rr.Code, rr.Body)
	}
}

func TestPartyOverHTTP(t *testing.T) {
	hs := freeSetup(t)
	ctx := context.Background()
	invites := &fakeInvites{}
	hs.srv.Invites = invites
	oksana := initData(1, "Oksana", "uk", "")
	lobby := decode[free.PartyView](t, hs.expect("POST", "/api/party", oksana, `{"mode":"classic","seconds":30,"rounds":3}`, 200))
	code := lobby.Code
	// The invitation: the settings in its caption, the lobby behind its
	// button.
	inv := decode[map[string]string](t, hs.expect("POST", "/api/party/"+code+"/invite", oksana, "", 200))
	link := "https://t.me/maplyin_bot/play?startapp=p" + code
	if inv["link"] != link || !strings.HasPrefix(inv["id"], "prep-") || len(invites.sent) != 1 {
		t.Fatalf("invitation: %+v", inv)
	}
	if sent := invites.sent[0]; sent.Description != "Бліц · 3 місця · 30 с" || !strings.Contains(sent.Text, "Oksana") ||
		sent.Markup[0][0].URL != link {
		t.Fatalf("invitation text: %+v", sent)
	}
	// Vera has never started the bot: she opens the link and joins, and
	// core has her without a DM with maply.
	vera := initData(7, "Vera", "be", "p"+code)
	hs.expect("POST", "/api/party/"+code+"/join", vera, "", 200)
	inCore := func() (person, dm bool) {
		t.Helper()
		if err := hs.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM core.person WHERE telegram_user_id = 7),
			EXISTS (SELECT 1 FROM core.maply_presence WHERE telegram_user_id = 7 AND chat_id = 0)`).Scan(&person, &dm); err != nil {
			t.Fatal(err)
		}
		return person, dm
	}
	if person, dm := inCore(); !person || dm {
		t.Fatalf("vera in core after joining: person %v, DM %v", person, dm)
	}
	// From the same launch she opens the day's game, which plays as from the
	// DM: the DM's reveal now closes her answers, or she could see the place
	// there and answer later from a group in a later zone.
	hs.expect("GET", "/api/today", vera, "", 200)
	if _, dm := inCore(); !dm {
		t.Fatal("the day's game from an invitation leaves no DM presence")
	}
	// A made-up invitation is not a group post either.
	hs.expect("GET", "/api/today", initData(8, "Nobody", "en", "pZZZZZZZZ"), "", 200)
	hs.expect("POST", "/api/party/ZZZZZZZZ/join", initData(8, "Nobody", "en", ""), "", 404)
	// Only the host starts, on the settings shown.
	hs.expect("POST", "/api/party/"+code+"/start", vera, fmt.Sprintf(`{"version":%d}`, lobby.Version), 403)
	hs.expect("POST", "/api/party/"+code+"/settings", oksana, `{"mode":"classic","seconds":60,"rounds":3}`, 200)
	hs.expect("POST", "/api/party/"+code+"/start", oksana, fmt.Sprintf(`{"version":%d}`, lobby.Version), 409)
	started := decode[free.PartyView](t, hs.expect("POST", "/api/party/"+code+"/start", oksana, fmt.Sprintf(`{"version":%d}`, lobby.Version+1), 200))
	// The poll at the same version is a few bytes with the clock.
	status, short := hs.do("GET", fmt.Sprintf("/api/party/%s?v=%d", code, started.V), vera, "")
	if status != 200 || len(short) > 64 || !strings.Contains(short, `"now"`) {
		t.Fatalf("short poll: %d %q", status, short)
	}
	// During the countdown the picture is not there yet, and the poll never
	// says where the place is.
	if status, _ := hs.do("GET", started.Rounds[0].Image, "", ""); status != 409 {
		t.Fatalf("picture in the countdown: %d", status)
	}
	hs.expect("POST", "/api/party/"+code+"/guess", vera, `{"n":1,"lat":10,"lon":10}`, 409)
	hs.now = hs.now.Add(free.Countdown + time.Second)
	hs.expect("POST", "/api/party/"+code+"/guess", vera, `{"n":1,"lat":10,"lon":10}`, 200)
	body := hs.expect("GET", "/api/party/"+code, oksana, "", 200)
	if s := freeLeaks(body); s != "" {
		t.Fatalf("poll after one answer says %q: %s", s, body)
	}
	hs.expect("POST", "/api/party/"+code+"/guess", vera, `{"n":1,"lat":10,"lon":10}`, 409)
	// Oksana answers last: the place closes and comes with every pin.
	body = hs.expect("POST", "/api/party/"+code+"/guess", oksana, `{"n":1,"lat":11,"lon":11,"auto":true}`, 200)
	v := decode[free.PartyView](t, body)
	if v.Rounds[0].Place == nil || len(v.Rounds[0].Guesses) != 2 {
		t.Fatalf("closed place: %+v", v.Rounds[0])
	}
	hs.expect("POST", "/api/party/"+code+"/leave", vera, "", 200)
	hs.expect("POST", "/api/party/"+code+"/rematch", oksana, "", 409)
}

// googleSignIn opens a session for an account with Google alone, under
// token. A second token for the same sub is a second browser.
func (hs *harness) googleSignIn(sub, token string) {
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
	acc, _, err := store.SignIn(ctx, db.Identity{Kind: db.KindGoogle, GoogleSub: sub}, "", db.Rules{
		Open:     func(round.Date) bool { return true },
		Deadline: func(context.Context, int64, round.Date) (time.Time, error) { return hs.now, nil },
	})
	if err != nil {
		hs.t.Fatal(err)
	}
	if err := store.CreateSession(ctx, db.Session{Hash: sha256.Sum256([]byte(token)), AccountID: acc.ID, Via: db.KindGoogle,
		Created: hs.now, Expires: hs.now.Add(account.SessionLife)}); err != nil {
		hs.t.Fatal(err)
	}
}

// A browser signed in with an account that has Telegram plays free as the
// person, alone and with friends, the same as the Mini App, and writes
// nothing to core. An account with Google alone plays alone as its site
// player, from any browser it signs in on.
func TestFreeOnTheSiteWithAnAccount(t *testing.T) {
	hs := freeSetup(t)
	ctx := context.Background()
	hs.srv.Invites = &fakeInvites{}
	oksana := hs.signIn(1, "Oksana")
	oksanaApp := initData(1, "Oksana", "uk", "")
	presence := func() int {
		var n int
		if err := hs.pool.QueryRow(ctx, `SELECT count(*) FROM core.maply_presence WHERE telegram_user_id = 1`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := presence()

	// A solo game begun on the site is hers in the Mini App.
	g := decode[free.SoloView](t, hs.siteExpect("POST", "/api/free/solo", oksana, `{"mode":"classic","seconds":0,"rounds":3}`, 200))
	home := decode[free.Home](t, hs.expect("GET", "/api/free", oksanaApp, "", 200))
	if home.Open == nil || *home.Open != g.ID {
		t.Fatalf("the site's game in the app: %+v", home)
	}
	for n := 1; n <= 3; n++ {
		hs.siteExpect("POST", fmt.Sprintf("/api/free/solo/%d/start", g.ID), oksana, fmt.Sprintf(`{"n":%d}`, n), 200)
		hs.siteExpect("POST", fmt.Sprintf("/api/free/solo/%d/guess", g.ID), oksana, fmt.Sprintf(`{"n":%d,"lat":48.85,"lon":2.35}`, n), 200)
	}
	var results int
	if err := hs.pool.QueryRow(ctx, `SELECT count(*) FROM maply.free_result WHERE user_id = 1`).Scan(&results); err != nil || results != 1 {
		t.Fatalf("her record from the site: %d %v", results, err)
	}
	if home := decode[free.Home](t, hs.siteExpect("GET", "/api/free", oksana, "", 200)); len(home.Best) != 1 {
		t.Fatalf("her records on the site: %+v", home)
	}

	// With friends: her lobby from the site, under her name from core; the
	// invitation is the link, since the site has no shareMessage.
	lobby := decode[free.PartyView](t, hs.siteExpect("POST", "/api/party", oksana, `{"mode":"classic","seconds":60,"rounds":3}`, 200))
	if lobby.Members[0].Name != "Oksana" {
		t.Fatalf("her name in the lobby: %+v", lobby.Members)
	}
	inv := decode[map[string]string](t, hs.siteExpect("POST", "/api/party/"+lobby.Code+"/invite", oksana, "", 200))
	if inv["id"] != "" || !strings.HasSuffix(inv["link"], "startapp=p"+lobby.Code) {
		t.Fatalf("the site's invitation: %+v", inv)
	}
	ivan := initData(2, "Ivan", "ru", "p"+lobby.Code)
	if v := decode[free.PartyView](t, hs.expect("POST", "/api/party/"+lobby.Code+"/join", ivan, "", 200)); len(v.Members) != 2 {
		t.Fatalf("ivan joins her: %+v", v.Members)
	}
	if v := decode[free.PartyView](t, hs.expect("GET", "/api/party/"+lobby.Code, oksanaApp, "", 200)); v.Me != 1 {
		t.Fatalf("the app sees her in the site's lobby: %+v", v)
	}
	// Only maply's own page asks with her cookie.
	hs.siteExpect("POST", "/api/free/solo", oksana, `{"mode":"classic","seconds":0,"rounds":3}`, http.StatusUnsupportedMediaType, "Content-Type", "text/plain")
	hs.siteExpect("GET", "/api/party/"+lobby.Code, oksana, "", http.StatusBadRequest, "Sec-Fetch-Site", "cross-site")
	if presence() != before {
		t.Fatal("free play on the site wrote her presence")
	}

	// Google alone: alone, as the account's site player, in any browser.
	hs.googleSignIn("google-sub-1", strings.Repeat("g", 43))
	hs.googleSignIn("google-sub-1", strings.Repeat("h", 43))
	first, second := strings.Repeat("g", 43), strings.Repeat("h", 43)
	if body := hs.siteExpect("POST", "/api/party", first, `{"mode":"classic","seconds":60,"rounds":3}`, http.StatusForbidden); !strings.Contains(body, "telegram_only") {
		t.Fatalf("google party: %s", body)
	}
	gg := decode[free.SoloView](t, hs.siteExpect("POST", "/api/free/solo", first, `{"mode":"classic","seconds":0,"rounds":3}`, 200))
	if home := decode[free.Home](t, hs.siteExpect("GET", "/api/free", second, "", 200)); home.Open == nil || *home.Open != gg.ID || len(home.Best) != 0 {
		t.Fatalf("the account's game in another browser: %+v", home)
	}
}
