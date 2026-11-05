// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FreshLabDev/maply/internal/free"
)

// Sign-in gives records. An account with Google alone plays free on the site
// as its own site player: it keeps records, makes a party, shares the site's
// link, and a second account joins by it; it takes a Telegram person's
// challenge. A guest plays alone, keeps nothing and is asked to sign in for
// the rest. Nothing of it reaches core.
func TestFreeWithAGoogleAccount(t *testing.T) {
	hs := freeSetup(t)
	ctx := context.Background()
	hs.srv.AppURL = "https://maply.test"
	lena, ivan := strings.Repeat("l", 43), strings.Repeat("i", 43)
	hs.googleSignIn("g-lena", lena)
	hs.googleSignIn("g-ivan", ivan)
	// Everyone in core but Oksana, whom the Mini App records.
	people := func() int {
		var n int
		if err := hs.pool.QueryRow(ctx, `SELECT count(*) FROM core.person WHERE telegram_user_id <> 1`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := people()
	type home struct {
		free.Home
		Keeps bool `json:"keeps"`
	}

	// The screen says who keeps records: an account, even before its first
	// game, and a Telegram person; not a guest.
	if h := decode[home](t, hs.siteExpect("GET", "/api/free", lena, "", 200)); !h.Keeps || h.Open != nil {
		t.Fatalf("lena's screen: %+v", h)
	}
	if h := decode[home](t, hs.expect("GET", "/api/free", "", "", 200)); h.Keeps {
		t.Fatalf("a guest's screen: %+v", h)
	}
	if h := decode[home](t, hs.expect("GET", "/api/free", initData(1, "Oksana", "uk", ""), "", 200)); !h.Keeps {
		t.Fatalf("a Telegram person's screen: %+v", h)
	}

	// Her first game makes her account's player; the session she asks with
	// still remembers the account without one, and her game is hers.
	g := decode[free.SoloView](t, hs.siteExpect("POST", "/api/free/solo", lena, `{"mode":"classic","seconds":0,"rounds":3}`, 200))
	path := fmt.Sprintf("/api/free/solo/%d", g.ID)
	for n := 1; n <= 3; n++ {
		hs.siteExpect("GET", path, lena, "", 200)
		hs.siteExpect("POST", path+"/start", lena, fmt.Sprintf(`{"n":%d}`, n), 200)
		g = decode[free.SoloView](t, hs.siteExpect("POST", path+"/guess", lena, fmt.Sprintf(`{"n":%d,"lat":48.85,"lon":2.35}`, n), 200))
	}
	if g.Final == nil || g.Final.Best != nil {
		t.Fatalf("lena's final: %+v", g.Final)
	}
	var player string
	if err := hs.pool.QueryRow(ctx, `
		SELECT r.player::text FROM maply.free_result r
		  JOIN maply.site_player p ON p.id = r.player JOIN maply.account a ON a.id = p.account_id
		 WHERE a.google_sub = 'g-lena' AND r.user_id IS NULL AND r.game = $1`, g.ID).Scan(&player); err != nil {
		t.Fatalf("lena's line: %v", err)
	}
	if h := decode[home](t, hs.siteExpect("GET", "/api/free", lena, "", 200)); len(h.Best) != 1 {
		t.Fatalf("lena's records: %+v", h)
	}

	// Her party: her name is the site player's, "Guest" without one; the
	// invitation is the site's own address.
	lobby := decode[free.PartyView](t, hs.siteExpect("POST", "/api/party", lena, `{"mode":"classic","seconds":60,"rounds":3}`, 200))
	if len(lobby.Members) != 1 || lobby.Members[0].Name != "Guest" {
		t.Fatalf("lena's lobby: %+v", lobby.Members)
	}
	inv := decode[map[string]string](t, hs.siteExpect("POST", "/api/party/"+lobby.Code+"/invite", lena, "", 200))
	if inv["id"] != "" || inv["link"] != "https://maply.test/party/"+lobby.Code {
		t.Fatalf("lena's invitation: %+v", inv)
	}
	// Ivan opens /party/<code> on the site and joins as his account.
	j := decode[free.PartyView](t, hs.siteExpect("POST", "/api/party/"+lobby.Code+"/join?lang=uk", ivan, "", 200))
	if len(j.Members) != 2 || j.Me != 2 || j.Members[1].Name != "Гість" {
		t.Fatalf("ivan joins: %+v", j)
	}
	if v := decode[free.PartyView](t, hs.siteExpect("GET", "/api/party/"+lobby.Code, lena, "", 200)); v.Me != 1 || len(v.Members) != 2 {
		t.Fatalf("lena's poll: %+v", v)
	}
	hs.siteExpect("GET", "/api/party/"+lobby.Code, lena, "", http.StatusBadRequest, "Sec-Fetch-Site", "cross-site")
	hs.siteExpect("POST", "/api/party/"+lobby.Code+"/start", lena, fmt.Sprintf(`{"version":%d}`, lobby.Version), 200)

	// A guest: alone, no line, and "sign in" for friends and challenges.
	guest := func(method, path, cookie, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, req)
		return rr
	}
	rr := guest("POST", "/api/free/solo", "", `{"mode":"classic","seconds":0,"rounds":3}`)
	if rr.Code != 200 {
		t.Fatalf("guest solo: %d %s", rr.Code, rr.Body)
	}
	jar := rr.Result().Cookies()[0]
	cookie := jar.Name + "=" + jar.Value
	gg := decode[free.SoloView](t, rr.Body.String())
	for n := 1; n <= 3; n++ {
		guest("POST", fmt.Sprintf("/api/free/solo/%d/start", gg.ID), cookie, fmt.Sprintf(`{"n":%d}`, n))
		if rr := guest("POST", fmt.Sprintf("/api/free/solo/%d/guess", gg.ID), cookie, fmt.Sprintf(`{"n":%d,"lat":1,"lon":1}`, n)); rr.Code != 200 {
			t.Fatalf("guest guess: %d %s", rr.Code, rr.Body)
		}
	}
	var lines int
	if err := hs.pool.QueryRow(ctx, `SELECT count(*) FROM maply.free_result WHERE game = $1 OR player = $2::uuid`, gg.ID, jar.Value).Scan(&lines); err != nil || lines != 0 {
		t.Fatalf("guest lines: %d %v", lines, err)
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/party"},
		{"POST", "/api/party/" + lobby.Code + "/join"},
		{"GET", "/api/party/" + lobby.Code},
		{"POST", fmt.Sprintf("/api/free/solo/%d/challenge", gg.ID)},
	} {
		if rr := guest(c.method, c.path, cookie, `{"mode":"classic","seconds":60,"rounds":3}`); rr.Code != 403 || !strings.Contains(rr.Body.String(), "sign_in") {
			t.Fatalf("guest %s %s: %d %s", c.method, c.path, rr.Code, rr.Body)
		}
	}

	// Oksana's challenge from the Mini App, taken by Lena on the site.
	oksana := initData(1, "Oksana", "uk", "")
	o := decode[free.SoloView](t, hs.expect("POST", "/api/free/solo", oksana, `{"mode":"classic","seconds":30,"rounds":3}`, 200))
	opath := fmt.Sprintf("/api/free/solo/%d", o.ID)
	for n := 1; n <= 3; n++ {
		hs.expect("POST", opath+"/start", oksana, fmt.Sprintf(`{"n":%d}`, n), 200)
		hs.expect("POST", opath+"/guess", oksana, fmt.Sprintf(`{"n":%d,"lat":10,"lon":10}`, n), 200)
	}
	code := decode[map[string]string](t, hs.expect("POST", opath+"/challenge", oksana, "", 200))["code"]
	if inv := decode[map[string]string](t, hs.expect("POST", "/api/free/challenge/"+code+"/invite", oksana, "", 200)); inv["link"] != "https://t.me/maplyin_bot/play?startapp=c"+code {
		t.Fatalf("the Mini App's invitation: %+v", inv)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/free/challenge/" + code},
		{"POST", "/api/free/challenge/" + code + "/play"},
	} {
		if rr := guest(c.method, c.path, cookie, ""); rr.Code != 403 || !strings.Contains(rr.Body.String(), "sign_in") || strings.Contains(rr.Body.String(), "Oksana") {
			t.Fatalf("guest %s %s: %d %s", c.method, c.path, rr.Code, rr.Body)
		}
	}
	// Ivan looks before taking it: no invitation of his to send yet.
	if c := decode[free.ChallengeView](t, hs.siteExpect("GET", "/api/free/challenge/"+code, ivan, "", 200)); c.By != "Oksana" || c.Mine != nil {
		t.Fatalf("ivan looks: %+v", c)
	}
	hs.siteExpect("POST", "/api/free/challenge/"+code+"/invite", ivan, "", 403)
	a := decode[free.SoloView](t, hs.siteExpect("POST", "/api/free/challenge/"+code+"/play", lena, "", 200))
	if a.Challenge != code || a.Next != 1 {
		t.Fatalf("lena's attempt: %+v", a)
	}
	c := decode[free.ChallengeView](t, hs.siteExpect("GET", "/api/free/challenge/"+code, lena, "", 200))
	if c.Mine == nil || *c.Mine != a.ID || len(c.Lines) != 2 || !c.Lines[1].Me || c.Lines[1].Name != "Guest" {
		t.Fatalf("lena in the challenge: %+v", c)
	}
	if inv := decode[map[string]string](t, hs.siteExpect("POST", "/api/free/challenge/"+code+"/invite", lena, "", 200)); inv["link"] != "https://maply.test/play?c="+code {
		t.Fatalf("the site's challenge link: %+v", inv)
	}
	if c := decode[free.ChallengeView](t, hs.expect("GET", "/api/free/challenge/"+code, oksana, "", 200)); len(c.Lines) != 2 || !c.Lines[0].Me {
		t.Fatalf("oksana sees lena: %+v", c)
	}
	if people() != before {
		t.Fatal("an account with Google alone wrote core")
	}
}
