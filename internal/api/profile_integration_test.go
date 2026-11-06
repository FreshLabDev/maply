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
		VALUES ('used', 'mapillary', $1, 'CC-BY-SA-4.0', 'panorama', 10, 20, $2, $3, $4, '{"en":"Past","ru":"Прошлое"}', 3, now())
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
	// The profile, with a username made up from the name: what the bot
	// gives anyone it meets.
	if err := hs.srv.Store.(*db.Store).EnsurePersona(ctx, id, "", name); err != nil {
		hs.t.Fatal(err)
	}
}

// editView is what the owner may change of how maply shows them.
type editView struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Photo    bool   `json:"photo"`
	Telegram string `json:"telegram"`
	Ask      bool   `json:"ask"`
}

type ownView struct {
	Name  string `json:"name"`
	Photo string `json:"photo"`
	U     string `json:"u"`
	Guest bool   `json:"guest"`
	// Settings has hide_me only: the public page is gone.
	Settings *struct {
		HideMe bool `json:"hide_me"`
	} `json:"settings"`
	Edit   *editView `json:"edit"`
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

// userView is a profile at /api/u/<username> as someone else sees it.
type userView struct {
	Name  string `json:"name"`
	Photo string `json:"photo"`
	U     string `json:"u"`
	Me    bool   `json:"me"`
	// Moved is the new username of a username its owner left.
	Moved  string `json:"moved"`
	Ranked struct {
		Days       int  `json:"days"`
		Points     int  `json:"points"`
		Avg30      *int `json:"avg30"`
		Streak     int  `json:"streak"`
		Percentile *int `json:"percentile"`
		History    []struct {
			Date  string `json:"date"`
			City  string `json:"city"`
			Final bool   `json:"final"`
		} `json:"history"`
	} `json:"ranked"`
	Free *struct {
		Games int `json:"games"`
	} `json:"free"`
	Duel *struct {
		Closer int `json:"closer"`
		Of     int `json:"of"`
	} `json:"duel"`
	Error string `json:"error"`
}

// rowView is a player's row in a table of a day: the name, the username
// that opens the profile and the photo.
type rowView struct {
	Name  string `json:"name"`
	U     string `json:"u"`
	Photo string `json:"photo"`
	Anon  bool   `json:"anon"`
	Me    bool   `json:"me"`
}

// rowIn is the row of name in a day's ranking.
func rowIn(t *testing.T, body, name string) rowView {
	t.Helper()
	var d struct {
		Ranking []rowView `json:"ranking"`
	}
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	for _, r := range d.Ranking {
		if r.Name == name {
			return r
		}
	}
	return rowView{}
}

// setUsername types a username for the viewer, from the Mini App.
func (hs *harness) setUsername(auth, username string) {
	hs.t.Helper()
	hs.expect("POST", "/api/profile/edit", auth, `{"username":"`+username+`"}`, http.StatusOK)
}

// The profile's visibility rules, against the real store: what the player,
// another player in a group with them, a player outside it and a site
// visitor each get at /api/u/<username>, before and after today's reveal,
// and with "hide me".
func TestProfileVisibility(t *testing.T) {
	hs := setup(t)
	hs.srv.Avatars = &photosAsked{}
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
	hs.setUsername(oksanaDM, "oksana_k")

	// Oksana answers today from the DM, far from Lviv.
	hs.expect("GET", "/api/today", oksanaDM, "", http.StatusOK)
	hs.expect("POST", "/api/guess", oksanaDM, `{"lat":40,"lon":-3}`, http.StatusOK)

	ownBody := hs.expect("GET", "/api/profile", oksanaDM, "", http.StatusOK)
	own := decode[ownView](t, ownBody)
	if own.Name != "Oksana" || own.U != "oksana_k" || own.Guest || own.Settings == nil || own.Settings.HideMe ||
		own.Edit == nil || own.Edit.Username != "oksana_k" || own.Edit.Telegram != "Oksana" || own.Edit.Name != "" || own.Edit.Photo || own.Edit.Ask {
		t.Fatalf("own profile = %+v %+v", own, own.Edit)
	}
	if strings.Contains(ownBody, `"public"`) {
		t.Fatalf("the public page is gone: %s", ownBody)
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
	// Her own address opens the same whole profile, edit and all.
	if self := hs.expect("GET", "/api/u/oksana_k", oksanaDM, "", http.StatusOK); self != ownBody {
		t.Fatalf("her own address differs from her profile:\n%s\n%s", self, ownBody)
	}
	// Her name in a table carries the username that opens her profile, the
	// same in every table.
	day := hs.expect("GET", "/api/day?date="+yesterday.String(), ivanMoscow, "", http.StatusOK)
	if row := rowIn(t, day, "Oksana"); row.U != "oksana_k" || row.Photo != "/face/"+faceKey(1)+".jpg" {
		t.Fatalf("her row = %+v", row)
	}
	if other := rowIn(t, hs.expect("GET", "/api/day?date="+yesterday.AddDays(-1).String(), ivanMoscow, "", http.StatusOK), "Oksana"); other.U != "oksana_k" {
		t.Fatalf("her row in another table = %+v", other)
	}
	// The free tab is empty until free play has results.
	if own.Free == nil || own.Free.Games != 0 {
		t.Fatalf("free = %+v", own.Free)
	}

	// Ivan opens her profile from the Moscow group: the days over
	// everywhere only, so today's answer is nowhere in it, and the head to
	// head stops at yesterday until the group's own reveal: she was closer
	// than him on two of their three days.
	view := func(auth string) (int, string) {
		t.Helper()
		return hs.do("GET", "/api/u/oksana_k", auth, "")
	}
	code, body := view(ivanMoscow)
	if code != http.StatusOK {
		t.Fatalf("Ivan's view = %d %s", code, body)
	}
	u := decode[userView](t, body)
	r := u.Ranked
	if u.Name != "Oksana" || u.U != "oksana_k" || u.Photo != "/face/"+faceKey(1)+".jpg" || u.Me ||
		r.Days != 3 || r.Streak != 3 || r.Avg30 == nil || *r.Avg30 != (4200+2500+4950)/3 || r.Points != 4200+2500+4950 {
		t.Fatalf("Ivan's view of Oksana = %+v", u)
	}
	// Closed days with their places, no percentile: its line speaks to her.
	if len(r.History) != 3 || r.Percentile != nil {
		t.Fatalf("history %+v, percentile %v", r.History, r.Percentile)
	}
	for _, h := range r.History {
		if !h.Final || h.Date == hs.date.String() {
			t.Fatalf("a day still open in a stranger's view: %+v", h)
		}
	}
	if u.Duel == nil || u.Duel.Of != 3 || u.Duel.Closer != 2 {
		t.Fatalf("head to head = %+v", u.Duel)
	}
	if u.Free == nil || u.Free.Games != 0 {
		t.Fatalf("free of a stranger's view = %+v", u.Free)
	}
	// Nothing of her own, of today (her pin there was 40, -3), or that finds
	// her in Telegram. The pins of the closed days are in the history.
	for _, leak := range []string{`"lat":40`, `"lon":-3`, hs.date.String(), `"percentile":`, `"settings"`, `"edit"`, `"hide_me"`, `"telegram"`, `"guest"`,
		"telegram_user_id", `"id"`, `"account"`, `"player"`, `"who"`, `"handle"`, `"public"`, "Lviv", "Львов", `"UA"`} {
		if leak == `"percentile":` {
			// The key is there, null.
			if !strings.Contains(body, `"percentile":null`) {
				t.Fatalf("a stranger gets a percentile: %s", body)
			}
			continue
		}
		if strings.Contains(body, leak) {
			t.Fatalf("a stranger's view carries %s: %s", leak, body)
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
	code, body = view(ivanMoscow)
	if u = decode[userView](t, body); code != http.StatusOK || u.Duel == nil || u.Duel.Of != 4 || u.Duel.Closer != 2 || u.Ranked.Days != 3 {
		t.Fatalf("after the Moscow reveal = %d %+v", code, u)
	}
	// From the DM there is no group to speak for: no head to head.
	if _, body = view(initData(2, "Ivan", "ru", "")); decode[userView](t, body).Duel != nil {
		t.Fatalf("a duel from a DM: %s", body)
	}

	// Alesia shares no group with Oksana: the numbers and the photo of a
	// signed-in viewer, no head to head.
	code, body = view(alesia)
	if u = decode[userView](t, body); code != http.StatusOK || u.Ranked.Days != 3 || u.Duel != nil || u.Photo == "" {
		t.Fatalf("Alesia's view of Oksana = %d %+v", code, u)
	}
	// A site visitor sees the same numbers, with no photo and no duel.
	code, body = site("/api/u/oksana_k")
	u = decode[userView](t, body)
	if code != http.StatusOK || u.Ranked.Days != 3 || u.Duel != nil || u.Photo != "" || u.Name != "Oksana" || u.U != "oksana_k" || strings.Contains(body, `"photo"`) {
		t.Fatalf("site view of Oksana = %d %s", code, body)
	}
	// A made-up username reads as one that is not there.
	hs.expect("GET", "/api/u/nobody_here", ivanMoscow, "", http.StatusNotFound)
	hs.expect("GET", "/api/u/%20", ivanMoscow, "", http.StatusNotFound)
	// The address is for people, not search.
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/u/oksana_k", nil))
	if rr.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("headers = %v", rr.Header())
	}

	// The site's own profile: days over everywhere only.
	code, body = site("/api/profile?lang=ru")
	guest := decode[ownView](t, body)
	if code != http.StatusOK || !guest.Guest || guest.Settings != nil || guest.Edit != nil || guest.Ranked.Days != 2 || guest.Ranked.History[0].City != "Прошлое" {
		t.Fatalf("site profile = %d %s", code, body)
	}

	// The world's table of yesterday names her and carries her username.
	code, body = site("/api/day?date=" + yesterday.String())
	if code != http.StatusOK || !strings.Contains(body, `"name":"Oksana"`) {
		t.Fatalf("yesterday = %d %s", code, body)
	}
	if row := rowIn(t, body, "Oksana"); row.U != "oksana_k" || row.Photo != "" {
		t.Fatalf("her row for a visitor = %+v", row)
	}

	// She hides herself.
	set := hs.expect("POST", "/api/profile/settings", oksanaMoscow, `{"hide_me":true}`, http.StatusOK)
	if !strings.Contains(set, `"hide_me":true`) {
		t.Fatalf("settings = %s", set)
	}
	for who, auth := range map[string]string{"Alesia": alesia, "Ivan": ivanMoscow} {
		if code, body := view(auth); code != http.StatusNotFound || !strings.Contains(body, `"error":"not_found"`) {
			t.Fatalf("%s opens a hidden profile: %d %s", who, code, body)
		}
	}
	if code, body := site("/api/u/oksana_k"); code != http.StatusNotFound || !strings.Contains(body, `"error":"not_found"`) {
		t.Fatalf("a hidden profile opens on the site: %d %s", code, body)
	}
	// An old address of hers is as closed.
	hs.expect("POST", "/api/profile/edit", oksanaDM, `{"username":"oksana_new"}`, http.StatusOK)
	hs.expect("GET", "/api/u/oksana_k", alesia, "", http.StatusNotFound)
	hs.expect("GET", "/api/u/oksana_new", alesia, "", http.StatusNotFound)
	code, body = site("/api/day?date=" + yesterday.String() + "&lang=en")
	if row := rowIn(t, body, "Anonymous"); code != http.StatusOK || strings.Contains(body, "Oksana") || row.U != "" || row.Photo != "" || !row.Anon {
		t.Fatalf("yesterday with her hidden = %d %s", code, body)
	}
	if code != http.StatusOK || strings.Contains(body, "oksana") {
		t.Fatalf("her username is in the table: %s", body)
	}
	// Her group still sees her name and photo, but no username: her profile
	// is closed to them as to everyone, so no row leads to it. She sees
	// herself, and hers.
	if row := rowIn(t, hs.expect("GET", "/api/day?date="+yesterday.String(), ivanMoscow, "", http.StatusOK), "Oksana"); row.Name == "" || row.U != "" || row.Photo == "" {
		t.Fatalf("her group lost her or sees her username: %+v", row)
	}
	// Ivan shares the Moscow group with her, so from his DM too.
	if row := rowIn(t, hs.expect("GET", "/api/day?date="+yesterday.String(), initData(2, "Ivan", "ru", ""), "", http.StatusOK), "Oksana"); row.Name == "" || row.U != "" {
		t.Fatalf("her group-mate lost her in his DM: %+v", row)
	}
	if body := hs.expect("GET", "/api/day?date="+yesterday.String(), alesia, "", http.StatusOK); strings.Contains(body, "Oksana") || strings.Contains(body, "oksana") {
		t.Fatalf("another group sees her: %s", body)
	}
	if row := rowIn(t, hs.expect("GET", "/api/day?date="+yesterday.String(), oksanaDM, "", http.StatusOK), "Oksana"); row.U != "oksana_new" || !row.Me {
		t.Fatalf("she does not see herself: %+v", row)
	}
	// And the profile is hers to open, whatever the address she is at.
	if self := decode[ownView](t, hs.expect("GET", "/api/u/oksana_new", oksanaDM, "", http.StatusOK)); self.Edit == nil || self.Edit.Username != "oksana_new" || self.Settings == nil || !self.Settings.HideMe {
		t.Fatalf("her own hidden profile = %+v", self)
	}
	if moved := decode[userView](t, hs.expect("GET", "/api/u/oksana_k", oksanaDM, "", http.StatusOK)); moved.Moved != "oksana_new" {
		t.Fatalf("her old address for her = %+v", moved)
	}
	// Showing herself brings it back.
	hs.expect("POST", "/api/profile/settings", oksanaDM, `{"hide_me":false}`, http.StatusOK)
	if u := decode[userView](t, hs.expect("GET", "/api/u/oksana_new", alesia, "", http.StatusOK)); u.Name != "Oksana" {
		t.Fatalf("shown again = %+v", u)
	}
	hs.expect("POST", "/api/profile/settings", oksanaDM, `{"hide_me":"yes"}`, http.StatusBadRequest)
	if code, _ := site("/api/profile/settings"); code != http.StatusMethodNotAllowed {
		t.Fatalf("settings by GET = %d", code)
	}
}

// Before the reveal the rows of others carry no username: it goes only
// next to a name.
func TestNoUsernameBeforeTheReveal(t *testing.T) {
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
		if !l.Me && (l.U != "" || l.Name != "" || l.Photo != "") {
			t.Fatalf("an unnamed line has a username: %+v", l)
		}
	}
	for _, l := range live.Board {
		if !l.Me && (l.U != "" || l.Photo != "") {
			t.Fatalf("an unnamed row has a username: %+v", l)
		}
	}
	// At the Kyiv reveal the group's names open, and their usernames with them.
	hs.now = time.Date(2026, time.December, 10, 18, 1, 0, 0, time.UTC)
	final := decode[resultResponse](t, hs.expect("GET", "/api/result", oksana, "", http.StatusOK))
	for _, l := range final.Table {
		if l.Name == "" || l.U == "" {
			t.Fatalf("a named line without its username: %+v", l)
		}
	}
}
