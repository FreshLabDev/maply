// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

// pastDay is a closed day before the harness's: its own place, and answers
// already scored, kept as over everywhere.
type pastAnswer struct {
	user   int64
	player string
	km     float64
	points int // without the speed bonus
	bonus  int
	speed  int
}

func (hs *harness) seedPast(n int, country, continent string, answers ...pastAnswer) round.Date {
	hs.t.Helper()
	ctx := context.Background()
	d := hs.date.AddDays(-n)
	var placeID int64
	if err := hs.pool.QueryRow(ctx, `
		INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon, image_key,
		                         country_iso, continent, city, difficulty, reviewed_at)
		VALUES ('used', 'mapillary', $1, 'CC-BY-SA-4.0', 'photo', 10, 20, $2, $3, $4, '{"en":"Past","ru":"Прошлое"}', 3, now())
		RETURNING id`, "past-"+d.String(), "pastkey-"+d.String(), country, continent).Scan(&placeID); err != nil {
		hs.t.Fatal(err)
	}
	if _, err := hs.pool.Exec(ctx, `INSERT INTO maply.round (date, day_no, place_id) VALUES ($1, $2, $3)`, d.Time(), 100-n, placeID); err != nil {
		hs.t.Fatal(err)
	}
	at := d.Reveal(time.UTC).Add(-6 * time.Hour)
	for _, a := range answers {
		if a.user != 0 {
			if _, err := hs.pool.Exec(ctx, `
				INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus, pts_speed)
				VALUES ($1, $2, 'dm', 11, 21, $3, $4, $5, $6, $7, $8)`,
				d.Time(), a.user, at, at.Add(time.Hour), a.km, a.points-a.bonus, a.bonus, a.speed); err != nil {
				hs.t.Fatal(err)
			}
			continue
		}
		if _, err := hs.pool.Exec(ctx, `INSERT INTO maply.site_player (id) VALUES ($1) ON CONFLICT DO NOTHING`, a.player); err != nil {
			hs.t.Fatal(err)
		}
		if _, err := hs.pool.Exec(ctx, `
			INSERT INTO maply.site_guess (date, player, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus)
			VALUES ($1, $2, 11, 21, $3, $4, $5, $6, $7)`,
			d.Time(), a.player, at, at.Add(time.Hour), a.km, a.points-a.bonus, a.bonus); err != nil {
			hs.t.Fatal(err)
		}
	}
	if err := hs.srv.Store.(*db.Store).KeepDay(ctx, d); err != nil {
		hs.t.Fatal(err)
	}
	return d
}

func (hs *harness) person(id int64, name string, chats ...int64) {
	hs.t.Helper()
	ctx := context.Background()
	if _, err := hs.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, $2)`, id, name); err != nil {
		hs.t.Fatal(err)
	}
	for _, c := range chats {
		if _, err := hs.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, $2, NULL, NULL, $3, 'supergroup')`, id, name, c); err != nil {
			hs.t.Fatal(err)
		}
	}
}

type ownView struct {
	Name     string `json:"name"`
	Guest    bool   `json:"guest"`
	Settings *struct {
		HideMe bool   `json:"hide_me"`
		Public string `json:"public"`
	} `json:"settings"`
	Ranked struct {
		Days       int  `json:"days"`
		Points     int  `json:"points"`
		Avg30      *int `json:"avg30"`
		Streak     int  `json:"streak"`
		Percentile *int `json:"percentile"`
		History    []struct {
			Day    int    `json:"day"`
			Date   string `json:"date"`
			Points int    `json:"points"`
			Final  bool   `json:"final"`
			Rank   int    `json:"rank"`
			City   string `json:"city"`
		} `json:"history"`
	} `json:"ranked"`
	Free *struct {
		Games int `json:"games"`
	} `json:"free"`
}

type miniView struct {
	Me     bool   `json:"me"`
	Name   string `json:"name"`
	Days   int    `json:"days"`
	Streak int    `json:"streak"`
	Avg30  *int   `json:"avg30"`
	Photo  string `json:"photo"`
	Duel   *struct {
		Closer int `json:"closer"`
		Of     int `json:"of"`
	} `json:"duel"`
	Error string `json:"error"`
}

// whoIn is the handle next to name in a day's ranking.
func whoIn(t *testing.T, body, name string) string {
	t.Helper()
	var d struct {
		Ranking []struct {
			Name string `json:"name"`
			Who  string `json:"who"`
		} `json:"ranking"`
	}
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	for _, r := range d.Ranking {
		if r.Name == name {
			return r.Who
		}
	}
	return ""
}

// The profile's visibility rules, against the real store: what the player,
// another player in a group with them, a player outside it and a site
// visitor each get, before and after today's reveal, and with "hide me".
func TestProfileVisibility(t *testing.T) {
	hs := setup(t)
	// 09:00 UTC on 10 December: the 9th is over everywhere, the 10th is
	// being guessed in every zone.
	hs.person(1, "Oksana", kyivGroup, moscowGroup)
	hs.person(2, "Ivan", moscowGroup)
	hs.person(3, "Alesia", minskGroup)
	const lucia = "3f6b1a2c-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	hs.seedPast(3, "FR", "europe", pastAnswer{user: 1, km: 50, points: 4000, bonus: 800, speed: 200}, pastAnswer{user: 2, km: 900, points: 1500, bonus: 300})
	hs.seedPast(2, "EG", "africa", pastAnswer{user: 1, km: 400, points: 2500, bonus: 300}, pastAnswer{user: 2, km: 100, points: 3800, bonus: 800},
		pastAnswer{player: lucia, km: 10, points: 4900, bonus: 1500})
	yesterday := hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500}, pastAnswer{user: 2, km: 2000, points: 900},
		pastAnswer{player: lucia, km: 700, points: 2000, bonus: 300})

	oksanaDM := initData(1, "Oksana", "uk", "")
	oksanaMoscow := initData(1, "Oksana", "uk", hs.payload(moscowGroup))
	ivanMoscow := initData(2, "Ivan", "ru", hs.payload(moscowGroup))
	alesia := initData(3, "Alesia", "be", hs.payload(minskGroup))
	cookie := &http.Cookie{Name: playerCookie, Value: lucia}
	site := func(path string) (int, string) {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		hs.h.ServeHTTP(rr, req)
		return rr.Code, rr.Body.String()
	}

	// Oksana answers today from the DM, far from Lviv.
	hs.expect("GET", "/api/today", oksanaDM, "", http.StatusOK)
	hs.expect("POST", "/api/guess", oksanaDM, `{"lat":40,"lon":-3}`, http.StatusOK)

	own := decode[ownView](t, hs.expect("GET", "/api/profile", oksanaDM, "", http.StatusOK))
	if own.Name != "Oksana" || own.Guest || own.Settings == nil || own.Settings.HideMe {
		t.Fatalf("own profile = %+v", own)
	}
	// Her own profile has today: she saw the place when she answered. Its
	// world place waits for the day to close.
	if own.Ranked.Days != 4 || len(own.Ranked.History) != 4 || own.Ranked.History[0].Date != hs.date.String() ||
		own.Ranked.History[0].Final || own.Ranked.History[0].Rank != 0 || own.Ranked.Streak != 4 {
		t.Fatalf("own ranked = %+v", own.Ranked)
	}
	// Three days for the percentile: she beat Ivan (100), then neither (0),
	// then both (100).
	if own.Ranked.Percentile == nil || *own.Ranked.Percentile != 67 {
		t.Fatalf("percentile = %v", own.Ranked.Percentile)
	}
	if own.Ranked.History[1].City != "Past" || own.Ranked.History[1].Rank != 1 {
		t.Fatalf("yesterday in her history = %+v", own.Ranked.History[1])
	}
	// Her name in yesterday's table carries a handle: a tap opens her profile.
	// Another table gives her another one.
	handle := whoIn(t, hs.expect("GET", "/api/day?date="+yesterday.String(), ivanMoscow, "", http.StatusOK), "Oksana")
	if other := whoIn(t, hs.expect("GET", "/api/day?date="+yesterday.AddDays(-1).String(), ivanMoscow, "", http.StatusOK), "Oksana"); handle == "" || other == "" || other == handle {
		t.Fatalf("handles %q and %q", handle, other)
	}
	// Her own name opens her whole profile, so the mini answer says only that.
	if self := decode[miniView](t, hs.expect("GET", "/api/profile/who?id="+handle, oksanaDM, "", http.StatusOK)); !self.Me || self.Name != "" || self.Days != 0 {
		t.Fatalf("her own mini = %+v", self)
	}
	// The free tab is empty until free play has results.
	if own.Free == nil || own.Free.Games != 0 {
		t.Fatalf("free = %+v", own.Free)
	}

	// Ivan taps her name in the Moscow group: the days over everywhere only,
	// so today's answer is nowhere in it, and the head to head stops at
	// yesterday until the group's own reveal: she was closer than him on two
	// of their three days.
	mini := decode[miniView](t, hs.expect("GET", "/api/profile/who?id="+handle, ivanMoscow, "", http.StatusOK))
	if mini.Name != "Oksana" || mini.Days != 3 || mini.Streak != 3 || mini.Avg30 == nil || *mini.Avg30 != (4200+2500+4950)/3 {
		t.Fatalf("Ivan's view of Oksana = %+v", mini)
	}
	if mini.Duel == nil || mini.Duel.Of != 3 || mini.Duel.Closer != 2 {
		t.Fatalf("head to head = %+v", mini.Duel)
	}
	for _, leak := range []string{`"lat"`, `"history"`, hs.date.String(), `"handle"`, "telegram_user_id", `"id"`} {
		if body := hs.expect("GET", "/api/profile/who?id="+handle, ivanMoscow, "", http.StatusOK); strings.Contains(body, leak) {
			t.Fatalf("the mini profile carries %s: %s", leak, body)
		}
	}
	// At 16:00 UTC tomorrow has begun in the east. Ivan has not answered
	// today, and his streak still runs from yesterday: it is as of his date.
	morning := hs.now
	hs.now = time.Date(2026, time.December, 10, 16, 0, 0, 0, time.UTC)
	if ivan := decode[ownView](t, hs.expect("GET", "/api/profile", initData(2, "Ivan", "ru", ""), "", http.StatusOK)); ivan.Ranked.Streak != 3 {
		t.Fatalf("Ivan's own streak = %d, want 3", ivan.Ranked.Streak)
	}
	hs.now = morning
	// Ivan answers too; after the Moscow reveal today counts in the head to head.
	hs.expect("GET", "/api/today", ivanMoscow, "", http.StatusOK)
	hs.expect("POST", "/api/guess", ivanMoscow, `{"lat":49.8,"lon":24}`, http.StatusOK)
	hs.now = time.Date(2026, time.December, 10, 17, 5, 0, 0, time.UTC)
	mini = decode[miniView](t, hs.expect("GET", "/api/profile/who?id="+handle, ivanMoscow, "", http.StatusOK))
	if mini.Duel == nil || mini.Duel.Of != 4 || mini.Duel.Closer != 2 || mini.Days != 3 {
		t.Fatalf("after the Moscow reveal = %+v", mini)
	}

	// Alesia shares no group with Oksana: the numbers, no head to head.
	mini = decode[miniView](t, hs.expect("GET", "/api/profile/who?id="+handle, alesia, "", http.StatusOK))
	if mini.Days != 3 || mini.Duel != nil || mini.Photo != "" {
		t.Fatalf("Alesia's view of Oksana = %+v", mini)
	}
	// A site visitor sees the same numbers.
	code, body := site("/api/profile/who?id=" + handle)
	if code != http.StatusOK || !strings.Contains(body, `"days":3`) || strings.Contains(body, `"duel"`) {
		t.Fatalf("site view of Oksana = %d %s", code, body)
	}
	// A made-up handle reads as hidden.
	hs.expect("GET", "/api/profile/who?id=AAAAAAAAAAAAAAAAAAAAAA", ivanMoscow, "", http.StatusNotFound)

	// The site's own profile: days over everywhere only.
	code, body = site("/api/profile?lang=ru")
	guest := decode[ownView](t, body)
	if code != http.StatusOK || !guest.Guest || guest.Settings != nil || guest.Ranked.Days != 2 || guest.Ranked.History[0].City != "Прошлое" {
		t.Fatalf("site profile = %d %s", code, body)
	}

	// The world's table of yesterday names her and carries her handle.
	code, body = site("/api/day?date=" + yesterday.String())
	if code != http.StatusOK || !strings.Contains(body, `"name":"Oksana"`) {
		t.Fatalf("yesterday = %d %s", code, body)
	}
	if whoIn(t, body, "Oksana") == "" {
		t.Fatalf("no handle on her name: %s", body)
	}

	// She hides herself.
	set := hs.expect("POST", "/api/profile/settings", oksanaMoscow, `{"hide_me":true}`, http.StatusOK)
	if !strings.Contains(set, `"hide_me":true`) {
		t.Fatalf("settings = %s", set)
	}
	hs.expect("GET", "/api/profile/who?id="+handle, alesia, "", http.StatusNotFound)
	if code, _ := site("/api/profile/who?id=" + handle); code != http.StatusNotFound {
		t.Fatalf("a hidden profile opens on the site: %d", code)
	}
	code, body = site("/api/day?date=" + yesterday.String() + "&lang=en")
	if code != http.StatusOK || strings.Contains(body, "Oksana") || whoIn(t, body, "Telegram player") != "" || !strings.Contains(body, "Telegram player") {
		t.Fatalf("yesterday with her hidden = %s", body)
	}
	// Her group still sees her, and she sees herself.
	if body := hs.expect("GET", "/api/day?date="+yesterday.String(), ivanMoscow, "", http.StatusOK); !strings.Contains(body, "Oksana") {
		t.Fatalf("her group lost her name: %s", body)
	}
	// Ivan shares the Moscow group with her, so from his DM too.
	if body := hs.expect("GET", "/api/day?date="+yesterday.String(), initData(2, "Ivan", "ru", ""), "", http.StatusOK); !strings.Contains(body, "Oksana") {
		t.Fatalf("her group-mate lost her name in his DM: %s", body)
	}
	if body := hs.expect("GET", "/api/day?date="+yesterday.String(), alesia, "", http.StatusOK); strings.Contains(body, "Oksana") {
		t.Fatalf("another group sees her name: %s", body)
	}
	if body := hs.expect("GET", "/api/day?date="+yesterday.String(), oksanaDM, "", http.StatusOK); !strings.Contains(body, "Oksana") {
		t.Fatalf("she does not see herself: %s", body)
	}
	if self := decode[miniView](t, hs.expect("GET", "/api/profile/who?id="+handle, oksanaDM, "", http.StatusOK)); !self.Me {
		t.Fatalf("her own name closed for her: %+v", self)
	}
	hs.expect("POST", "/api/profile/settings", oksanaDM, `{"hide_me":"yes"}`, http.StatusBadRequest)
	if code, _ := site("/api/profile/settings"); code != http.StatusMethodNotAllowed {
		t.Fatalf("settings by GET = %d", code)
	}
}

// Before the reveal the rows of others carry no handle: a handle goes only
// next to a name.
func TestNoHandleBeforeTheReveal(t *testing.T) {
	hs := setup(t)
	hs.person(1, "Oksana", kyivGroup)
	hs.person(5, "Dmitry", kyivGroup)
	oksana := initData(1, "Oksana", "uk", hs.payload(kyivGroup))
	dmitry := initData(5, "Dmitry", "ru", hs.payload(kyivGroup))
	hs.expect("GET", "/api/today", oksana, "", http.StatusOK)
	hs.expect("GET", "/api/today", dmitry, "", http.StatusOK)
	hs.expect("POST", "/api/guess", dmitry, `{"lat":48,"lon":30}`, http.StatusOK)
	live := decode[resultResponse](t, hs.expect("POST", "/api/guess", oksana, `{"lat":50,"lon":30}`, http.StatusOK))
	for _, l := range live.Table {
		if !l.Me && (l.Who != "" || l.Name != "") {
			t.Fatalf("an unnamed line has a handle: %+v", l)
		}
	}
	for _, l := range live.Board {
		if !l.Me && l.Who != "" {
			t.Fatalf("an unnamed row has a handle: %+v", l)
		}
	}
	// At the Kyiv reveal the group's names open, and their handles with them.
	hs.now = time.Date(2026, time.December, 10, 18, 1, 0, 0, time.UTC)
	final := decode[resultResponse](t, hs.expect("GET", "/api/result", oksana, "", http.StatusOK))
	for _, l := range final.Table {
		if l.Name == "" || l.Who == "" {
			t.Fatalf("a named line without its handle: %+v", l)
		}
	}
}
