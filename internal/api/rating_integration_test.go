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

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

type ratingBody struct {
	Period  string   `json:"period"`
	Circle  string   `json:"circle"`
	Sort    string   `json:"sort"`
	Dates   int      `json:"dates"`
	Total   int      `json:"total"`
	Guest   bool     `json:"guest"`
	Circles []string `json:"circles"`
	Rows    []struct {
		Rank   int     `json:"rank"`
		Name   string  `json:"name"`
		U      string  `json:"u"`
		Me     bool    `json:"me"`
		Anon   bool    `json:"anon"`
		Score  int     `json:"score"`
		Days   int     `json:"days"`
		Avg    int     `json:"avg"`
		Km     float64 `json:"km"`
		League string  `json:"league"`
	} `json:"rows"`
	Cards map[string]struct {
		Name string  `json:"name"`
		N    int     `json:"n"`
		Km   float64 `json:"km"`
		Date string  `json:"date"`
	} `json:"cards"`
	Me *struct {
		Score    int    `json:"score"`
		Days     int    `json:"days"`
		Avg      int    `json:"avg"`
		League   string `json:"league"`
		Next     string `json:"next"`
		NextFrom int    `json:"next_from"`
		NeedDays int    `json:"need_days"`
	} `json:"me"`
}

// line is the table as "rank name score" lines, "*" after a username, "?"
// on an anonymous row and "<" on the viewer's own.
func (b ratingBody) line() string {
	var out []string
	for _, r := range b.Rows {
		s := fmt.Sprintf("%d %s %d", r.Rank, r.Name, r.Score)
		if r.U != "" {
			s += "*"
		}
		if r.Anon {
			s += "?"
		}
		if r.Me {
			s += "<"
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

// The rating counts the days over everywhere of the people with a profile:
// Telegram players, with the site days of their account where they have no
// Telegram answer and gave it before the reveal, and accounts without
// Telegram. A guest of the site is in none. The circles narrow it to the
// viewer's groups or the people they played with; the leagues go by the
// average day from five days on.
func TestRating(t *testing.T) {
	hs := setup(t)
	const (
		home  = "11111111-1111-4111-8111-111111111111"
		vanya = "33333333-3333-4333-8333-333333333333"
		stray = "44444444-4444-4444-8444-444444444444"
	)
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	hs.person(3, "Lena", minskGroup)
	hs.person(5, "Dmitry", minskGroup)
	ctx := context.Background()
	for id, name := range map[string]string{home: "Marta", vanya: "Vanya", stray: "Stray"} {
		if _, err := hs.pool.Exec(ctx, `INSERT INTO maply.site_player (id, name) VALUES ($1, $2)`, id, name); err != nil {
			t.Fatal(err)
		}
	}
	marta, _ := hs.signInGoogle("g-marta", home)
	store := hs.srv.Store.(*db.Store)
	ivanAcc, _, err := store.SignIn(ctx, db.Identity{Kind: db.KindGoogle, GoogleSub: "g-ivan"}, vanya, db.Rules{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hs.pool.Exec(ctx, `UPDATE maply.account SET telegram_user_id = 2 WHERE id = $1`, ivanAcc.ID); err != nil {
		t.Fatal(err)
	}
	oksana, lena, dmitry := initData(1, "Oksana", "en", ""), initData(3, "Lena", "en", ""), initData(5, "Dmitry", "en", "")
	hs.expect("POST", "/api/profile/settings", lena, `{"hide_me":true}`, http.StatusOK)

	// Eight days, the eighth the oldest. Oksana plays all of them at 3000,
	// Lena the newest five at 4300; Ivan two in Telegram at 4500 and one
	// on the site at 4000, his site answer of a day he played in Telegram
	// and a late one left out; Marta two on the site; a guest one.
	dates := map[int]round.Date{}
	for n := 1; n <= 8; n++ {
		as := []pastAnswer{{user: 1, km: 100, points: 3000}}
		if n <= 5 {
			as = append(as, pastAnswer{user: 3, km: 5, points: 4300, bonus: 800})
		}
		switch n {
		case 1:
			as = append(as, pastAnswer{user: 2, km: 10, points: 4500, bonus: 1500}, pastAnswer{player: vanya, km: 1, points: 5000, bonus: 1500},
				pastAnswer{player: stray, km: 1, points: 5000, bonus: 1500})
		case 2:
			as = append(as, pastAnswer{user: 2, km: 10, points: 4500, bonus: 1500}, pastAnswer{player: home, km: 300, points: 2000})
		case 3:
			as = append(as, pastAnswer{player: vanya, km: 20, points: 4000, bonus: 800}, pastAnswer{player: home, km: 300, points: 2000})
		case 4:
			as = append(as, pastAnswer{player: vanya, km: 1, points: 5000, bonus: 1500})
		}
		dates[n] = hs.seedPast(n, "UA", "europe", as...)
	}
	if _, err := hs.pool.Exec(ctx, `UPDATE maply.site_guess SET late = true WHERE player = $1 AND date = $2`, vanya, dates[4].Time()); err != nil {
		t.Fatal(err)
	}
	// Oksana played a party with Lena.
	hs.freeGame(1, "party", "classic", 30, 5, 1000, hs.now.Add(-24*time.Hour))
	if _, err := hs.pool.Exec(ctx, `
		INSERT INTO maply.free_member (game, m, user_id, name, joined_at, last_seen_at)
		SELECT max(id), 2, 3, 'Lena', now(), now() FROM maply.free_game`); err != nil {
		t.Fatal(err)
	}

	get := func(auth, query string) ratingBody {
		t.Helper()
		return decode[ratingBody](t, hs.expect("GET", "/api/rating?"+query, auth, "", http.StatusOK))
	}
	check := func(b ratingBody, want string) {
		t.Helper()
		if b.line() != want || b.Total != len(b.Rows) {
			t.Fatalf("%s %s %s: %d [%s], want [%s]", b.Period, b.Circle, b.Sort, b.Total, b.line(), want)
		}
	}

	// The month by default: every day counts while there are fewer than
	// twenty. Lena is "Anonymous" to Oksana, who shares no group with her.
	month := get(oksana, "")
	check(month, "1 Oksana 24000*<, 2 Anonymous 21500?, 3 Ivan 13000*, 4 Marta 4000")
	if month.Period != "30" || month.Circle != "world" || month.Sort != "score" || month.Dates != 8 ||
		fmt.Sprint(month.Circles) != "[world groups played]" {
		t.Fatalf("the month = %+v", month)
	}
	if r := month.Rows[0]; r.Days != 8 || r.Avg != 3000 || r.Km != 100 || r.League != "traveler" {
		t.Fatalf("Oksana's line = %+v", r)
	}
	if r := month.Rows[2]; r.Days != 3 || r.League != "novice" {
		t.Fatalf("Ivan's line = %+v", r)
	}
	if m := month.Me; m == nil || m.League != "traveler" || m.Next != "navigator" || m.NextFrom != 3200 || m.NeedDays != 0 {
		t.Fatalf("Oksana's own = %+v", month.Me)
	}
	c := month.Cards
	if c["active"].Name != "Oksana" || c["active"].N != 8 || c["accurate"].Name != "Anonymous" || c["accurate"].Km != 5 ||
		c["day"].Name != "Ivan" || c["day"].N != 4500 || c["day"].Date != dates[2].String() || c["streak"].Name != "Oksana" || c["streak"].N != 8 {
		t.Fatalf("the cards = %+v", c)
	}

	// The week is the newest seven days.
	check(get(oksana, "period=7"), "1 Anonymous 21500?, 2 Oksana 21000*<, 3 Ivan 13000*, 4 Marta 4000")
	check(get(oksana, "period=all"), "1 Oksana 24000*<, 2 Anonymous 21500?, 3 Ivan 13000*, 4 Marta 4000")

	// Her groups, and the people she played with.
	check(get(oksana, "circle=groups"), "1 Oksana 24000*<, 2 Ivan 13000*")
	check(get(oksana, "circle=played"), "1 Oksana 24000*<, 2 Anonymous 21500?")

	// Leagues, and the other orders: one lucky day is not an average.
	check(get(oksana, "league=novice"), "1 Ivan 13000*, 2 Marta 4000")
	check(get(oksana, "league=cartographer"), "1 Anonymous 21500?")
	check(get(oksana, "sort=avg"), "1 Anonymous 21500?, 2 Oksana 24000*<, 3 Ivan 13000*, 4 Marta 4000")
	check(get(oksana, "sort=days"), "1 Oksana 24000*<, 2 Anonymous 21500?, 3 Ivan 13000*, 4 Marta 4000")
	check(get(oksana, "sort=km"), "1 Anonymous 21500?, 2 Oksana 24000*<, 3 Ivan 13000*, 4 Marta 4000")

	// Dmitry shares a group with Lena: her name, but no username.
	if r := get(dmitry, "league=cartographer").Rows[0]; r.Name != "Lena" || r.U != "" || r.Anon {
		t.Fatalf("Lena for her group-mate = %+v", r)
	}
	if r := get(lena, "league=cartographer").Rows[0]; r.Name != "Lena" || !r.Me {
		t.Fatalf("Lena for herself = %+v", r)
	}

	// Marta's account has no Telegram: no groups, and five days to go.
	m := decode[ratingBody](t, hs.siteExpect("GET", "/api/rating?lang=en", marta, "", http.StatusOK))
	if fmt.Sprint(m.Circles) != "[world played]" || m.Me == nil || m.Me.League != "novice" || m.Me.NeedDays != 3 || m.Me.Next != "" {
		t.Fatalf("Marta's rating = %+v", m)
	}
	if !strings.Contains(m.line(), "4 Marta 4000<") {
		t.Fatalf("Marta's row = [%s]", m.line())
	}

	// A guest of the site sees the table and is in none.
	req := httptest.NewRequest("GET", "/api/rating?lang=en", nil)
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	g := decode[ratingBody](t, rr.Body.String())
	if rr.Code != http.StatusOK || !g.Guest || g.Me != nil || fmt.Sprint(g.Circles) != "[world]" ||
		g.line() != "1 Oksana 24000*, 2 Anonymous 21500?, 3 Ivan 13000*, 4 Marta 4000" {
		t.Fatalf("a guest sees %d %s", rr.Code, rr.Body.String())
	}

	for _, bad := range []string{"period=14", "sort=name", "league=king", "circle=city"} {
		hs.expect("GET", "/api/rating?"+bad, oksana, "", http.StatusBadRequest)
	}
}
