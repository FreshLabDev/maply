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
	"time"

	"github.com/FreshLabDev/maply/internal/free"
)

func TestArchiveOverHTTP(t *testing.T) {
	hs := freeSetup(t)
	ivan := initData(2, "Ivan", "ru", "")
	oksana := initData(1, "Oksana", "uk", "")
	hs.expect("GET", "/api/today", ivan, "", 200)
	d := hs.seedPast(3, "JP", "asia", pastAnswer{user: 2, km: 50, points: 3400, speed: 200})
	type archive struct{ Days []free.ArchiveDay }
	// Only the day kept is in the archive: today's is in play.
	body := hs.expect("GET", "/api/free/archive", ivan, "", 200)
	if a := decode[archive](t, body); len(a.Days) != 1 || a.Days[0].N != 97 || a.Days[0].Date != d.String() || a.Days[0].State != free.DayAnswered {
		t.Fatalf("ivan's archive: %s", body)
	}
	if strings.Contains(body, "Past") || strings.Contains(body, "JP") {
		t.Fatalf("the archive names a place: %s", body)
	}
	if body := hs.expect("POST", "/api/free/archive", ivan, `{"n":97}`, 409); !strings.Contains(body, "day_answered") {
		t.Fatalf("ivan answered it: %s", body)
	}
	hs.expect("POST", "/api/free/archive", oksana, `{"n":96}`, 404)
	body = hs.expect("POST", "/api/free/archive", oksana, `{"n":97}`, 200)
	if strings.Contains(body, "Past") || strings.Contains(body, "JP") || strings.Contains(body, `"lat"`) {
		t.Fatalf("a day taken names its place before the answer: %s", body)
	}
	v := decode[free.SoloView](t, body)
	if v.Current == nil || v.Day == nil || v.Day.N != 97 {
		t.Fatalf("day taken: %+v", v)
	}
	// Ivan's answer counts as its distance and its bonus: 3400 without the
	// 200 for speed. Oksana's pin on the place, 3500, is worth more.
	got := decode[free.SoloView](t, hs.expect("POST", fmt.Sprintf("/api/free/solo/%d/guess", v.ID), oksana, `{"n":1,"lat":10,"lon":20}`, 200))
	if got.Final == nil || got.Final.DayRank != 1 || got.Final.DayOf != 2 || got.Done[0].Place.City != "Past" {
		t.Fatalf("oksana's day: %+v", got.Final)
	}
	if h := decode[free.Home](t, hs.expect("GET", "/api/free", oksana, "", 200)); h.Archive != 0 {
		t.Fatalf("nothing left in the archive: %+v", h)
	}
	// A browser with no cookie sees the day open, and taking it makes the
	// browser's player.
	if a := decode[archive](t, hs.expect("GET", "/api/free/archive", "", "", 200)); len(a.Days) != 1 || a.Days[0].State != free.DayOpen {
		t.Fatalf("a new browser: %+v", a)
	}
	req := httptest.NewRequest("POST", "/api/free/archive", strings.NewReader(`{"n":97}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	if rr.Code != 200 || len(rr.Result().Cookies()) != 1 {
		t.Fatalf("guest takes a day: %d %s", rr.Code, rr.Body)
	}
}

func TestChallengeOverHTTP(t *testing.T) {
	hs := freeSetup(t)
	ctx := context.Background()
	invites := &fakeInvites{}
	hs.srv.Invites = invites
	oksana := initData(1, "Oksana", "uk", "")
	ivan := initData(2, "Ivan", "ru", "")
	g := decode[free.SoloView](t, hs.expect("POST", "/api/free/solo", oksana, `{"mode":"classic","seconds":30,"rounds":3}`, 200))
	path := fmt.Sprintf("/api/free/solo/%d", g.ID)
	hs.expect("POST", path+"/challenge", oksana, "", 409)
	for n := 1; n <= 3; n++ {
		hs.expect("POST", path+"/start", oksana, fmt.Sprintf(`{"n":%d}`, n), 200)
		hs.expect("POST", path+"/guess", oksana, fmt.Sprintf(`{"n":%d,"lat":10,"lon":10}`, n), 200)
	}
	hs.expect("POST", path+"/challenge", ivan, "", 403)
	code := decode[map[string]string](t, hs.expect("POST", path+"/challenge", oksana, "", 200))["code"]
	if !free.ValidCode(code) {
		t.Fatalf("code %q", code)
	}
	link := "https://t.me/maplyin_bot/play?startapp=c" + code
	inv := decode[map[string]string](t, hs.expect("POST", "/api/free/challenge/"+code+"/invite", oksana, "", 200))
	if inv["link"] != link || !strings.HasPrefix(inv["id"], "prep-") || len(invites.sent) != 1 {
		t.Fatalf("invitation: %+v", inv)
	}
	if sent := invites.sent[0]; sent.Description != "Бліц · 3 місця · 30 с" || !strings.Contains(sent.Text, "Oksana") || sent.Markup[0][0].URL != link {
		t.Fatalf("invitation text: %+v", sent)
	}
	// Someone not in it does not send it on.
	hs.expect("POST", "/api/free/challenge/"+code+"/invite", ivan, "", 403)

	// Vera opens the invitation: she sees the challenge, takes it, and is in
	// core without a DM until she opens the day's game from it.
	vera := initData(7, "Vera", "be", "c"+code)
	body := hs.expect("GET", "/api/free/challenge/"+code, vera, "", 200)
	if s := freeLeaks(body); s != "" {
		t.Fatalf("the challenge says %q: %s", s, body)
	}
	if c := decode[free.ChallengeView](t, body); c.By != "Oksana" || c.Mine != nil || len(c.Lines) != 1 {
		t.Fatalf("challenge: %+v", c)
	}
	a := decode[free.SoloView](t, hs.expect("POST", "/api/free/challenge/"+code+"/play", vera, "", 200))
	if a.Challenge != code || a.Next != 1 {
		t.Fatalf("attempt: %+v", a)
	}
	inCore := func() (person, dm bool) {
		t.Helper()
		if err := hs.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM core.person WHERE telegram_user_id = 7),
			EXISTS (SELECT 1 FROM core.maply_presence WHERE telegram_user_id = 7 AND chat_id = 0)`).Scan(&person, &dm); err != nil {
			t.Fatal(err)
		}
		return person, dm
	}
	if person, dm := inCore(); !person || dm {
		t.Fatalf("vera after taking it: person %v, DM %v", person, dm)
	}
	hs.expect("GET", "/api/today", vera, "", 200)
	if _, dm := inCore(); !dm {
		t.Fatal("the day's game from a challenge plays as from the DM")
	}
	hs.expect("GET", "/api/today", initData(8, "Nobody", "en", "cZZZZZZZZ"), "", 200)
	hs.expect("GET", "/api/free/challenge/ZZZZZZZZ", vera, "", 404)
	hs.expect("GET", "/api/free/challenge/nope", vera, "", 404)
	// A guest of the site neither takes a challenge nor looks at it: the
	// table names the players.
	req := httptest.NewRequest("POST", "/api/free/challenge/"+code+"/play", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "sign_in") {
		t.Fatalf("guest: %d %s", rr.Code, rr.Body)
	}
	if body := hs.expect("GET", "/api/free/challenge/"+code, "", "", http.StatusForbidden); !strings.Contains(body, "sign_in") || strings.Contains(body, "Oksana") {
		t.Fatalf("guest looks: %s", body)
	}
	// Vera's first place begins only when she starts it, on her own clock.
	hs.now = hs.now.Add(time.Hour)
	r := decode[free.RoundView](t, hs.expect("POST", fmt.Sprintf("/api/free/solo/%d/start", a.ID), vera, `{"n":1}`, 200))
	if r.EndsAt == nil || !r.EndsAt.Equal(hs.now.Add(30*time.Second).UTC()) {
		t.Fatalf("vera's place 1: %+v", r)
	}
}
