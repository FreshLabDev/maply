// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// The free game on the stand: its service on the stand's clock, a free pool
// to play from, and bot players who join a party by its code, poll as the app
// does and answer near the place after a while.

func init() {
	Register(Extension{
		Name:  "free",
		Mount: freeStand.mount,
		Seed:  freeStand.seed,
		Start: freeStand.start,
		Panel: freeStand.panel,
	})
}

var freeStand = &standFree{}

type standFree struct {
	e *Env

	mu sync.Mutex
	// ctx ends with the stand or before a wipe; bots are the codes and
	// players a bot plays for.
	ctx  context.Context
	bots map[string]map[int64]bool
	done sync.WaitGroup
}

func (f *standFree) mount(e *Env, mux *http.ServeMux) {
	f.e = e
	e.API.Free = &free.Service{Store: e.Store, Countries: e.Countries, ImageKey: e.Signer.Key("maply-free-img-v1"),
		Zones: e.Zones, Log: e.Log.With("part", "free"), Now: e.Clock.Now}
	e.API.People = e.Store
	// The invitation is prepared on the stand's fake Bot API, which the
	// chats page shares with shareMessage; the client only calls it when a
	// player asks for an invitation.
	e.API.Invites = telegram.New(tg.New(BotToken, tg.WithAPIBase("http://"+e.Addr+"/tgapi"), tg.WithLogger(e.Log.With("part", "free-invites"))))
	// /dev/free/bots?code=K7QF2MXA&n=3 puts n bot players into that party.
	mux.HandleFunc("POST /dev/free/bots", func(w http.ResponseWriter, r *http.Request) {
		code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
		n, _ := strconv.Atoi(r.FormValue("n"))
		e.back(w, r, f.addBots(code, max(1, min(n, 7))))
	})
	// /dev/free/invite?user=1002&code=K7QF2MXA opens the invitation as that
	// person, as the button under the prepared message does; with
	// &challenge=1 the invitation is a challenge's.
	mux.HandleFunc("GET /dev/free/invite", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		id, _ := strconv.ParseInt(q.Get("user"), 10, 64)
		p, ok := e.Player(id)
		code := strings.ToUpper(q.Get("code"))
		if !ok || !free.ValidCode(code) {
			http.Error(w, "no such player or code", http.StatusBadRequest)
			return
		}
		prefix := free.InvitePrefix
		if q.Get("challenge") != "" {
			prefix = free.ChallengePrefix
		}
		http.Redirect(w, r, e.LaunchWith(p, 0, "sender", prefix+code, q.Get("platform"), "/"), http.StatusFound)
	})
}

func (f *standFree) start(ctx context.Context, e *Env) (func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	f.mu.Lock()
	f.ctx, f.bots = ctx, map[string]map[int64]bool{}
	f.mu.Unlock()
	f.done.Add(1)
	go func() { defer f.done.Done(); e.API.Free.Run(ctx, time.Minute) }()
	return func() { cancel(); f.done.Wait() }, nil
}

// seed makes the free pool: the daily queue keeps ten places, the rest of
// the unused ones play free, and drawn places fill a pool still too small.
// A curation folder of free places (out-free) loads as free by itself.
func (f *standFree) seed(ctx context.Context, e *Env) error {
	if _, err := e.Pool.Exec(ctx, `
		UPDATE maply.place SET pool = 'free'
		WHERE id IN (
			SELECT id FROM maply.place p
			WHERE status = 'approved' AND pool = 'daily'
			  AND NOT EXISTS (SELECT 1 FROM maply.round r WHERE r.place_id = p.id)
			ORDER BY difficulty, id OFFSET 10)`); err != nil {
		return fmt.Errorf("free pool: %w", err)
	}
	var have int
	if err := e.Pool.QueryRow(ctx, `SELECT count(*) FROM maply.place WHERE status = 'approved' AND pool = 'free'`).Scan(&have); err != nil {
		return err
	}
	for i, s := range freeCapitals {
		if have >= 24 {
			break
		}
		key := fmt.Sprintf("DevFree%02d%011d", i, i*7907)
		city := fmt.Sprintf(`{"en":%q}`, s.city)
		tag, err := e.Pool.Exec(ctx, `
			INSERT INTO maply.place (pool, status, source, source_id, source_url, license, kind, lat, lon, image_key,
			                         country_iso, continent, city, difficulty, reviewed_at, author, heading)
			VALUES ('free', 'approved', 'mapillary', $1, 'https://example.com', 'CC-BY-SA-4.0', 'panorama', $2, $3, $4,
			        $5, $6, $7, $8, now(), 'maply-dev', 0)
			ON CONFLICT (source, source_id) DO NOTHING`,
			fmt.Sprint(950000+i), s.lat, s.lon, key, s.iso, s.continent, city, 1+i%3)
		if err != nil {
			return fmt.Errorf("free place %s: %w", s.city, err)
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		if err := pano(filepath.Join(e.ImagesDir, key), 40+i); err != nil {
			return err
		}
		have++
	}
	e.Log.Info("free pool", "places", have)
	return nil
}

var freeCapitals = []struct {
	city, iso, continent string
	lat, lon             float64
}{
	{"Reykjavík", "IS", "europe", 64.1466, -21.9426}, {"Lisbon", "PT", "europe", 38.7223, -9.1393},
	{"Oslo", "NO", "europe", 59.9139, 10.7522}, {"Athens", "GR", "europe", 37.9838, 23.7275},
	{"Cairo", "EG", "africa", 30.0444, 31.2357}, {"Nairobi", "KE", "africa", -1.2921, 36.8219},
	{"Dakar", "SN", "africa", 14.7167, -17.4677}, {"Cape Town", "ZA", "africa", -33.9249, 18.4241},
	{"Tokyo", "JP", "asia", 35.6762, 139.6503}, {"Hanoi", "VN", "asia", 21.0278, 105.8342},
	{"Kathmandu", "NP", "asia", 27.7172, 85.3240}, {"Tbilisi", "GE", "asia", 41.7151, 44.8271},
	{"Ulaanbaatar", "MN", "asia", 47.8864, 106.9057}, {"Manila", "PH", "asia", 14.5995, 120.9842},
	{"Lima", "PE", "south_america", -12.0464, -77.0428}, {"Buenos Aires", "AR", "south_america", -34.6037, -58.3816},
	{"Bogotá", "CO", "south_america", 4.7110, -74.0721}, {"Santiago", "CL", "south_america", -33.4489, -70.6693},
	{"Mexico City", "MX", "north_america", 19.4326, -99.1332}, {"Ottawa", "CA", "north_america", 45.4215, -75.6972},
	{"Havana", "CU", "north_america", 23.1136, -82.3666}, {"Anchorage", "US", "north_america", 61.2181, -149.9003},
	{"Wellington", "NZ", "oceania", -41.2865, 174.7762}, {"Hobart", "AU", "oceania", -42.8821, 147.3272},
}

// addBots starts up to n bot players in the party, none of them the stand's
// owner, none already playing there.
func (f *standFree) addBots(code string, n int) string {
	if !free.ValidCode(code) {
		return "Код партии: восемь знаков, например K7QF2MXA."
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ctx == nil {
		return "Стенд ещё не запущен."
	}
	if f.bots[code] == nil {
		f.bots[code] = map[int64]bool{}
	}
	var names []string
	for _, p := range f.e.Players {
		if len(names) == n {
			break
		}
		if p.Owner || f.bots[code][p.ID] {
			continue
		}
		f.bots[code][p.ID] = true
		names = append(names, p.Name)
		f.done.Add(1)
		go func(p Player) {
			defer f.done.Done()
			f.play(f.ctx, p, code)
			f.mu.Lock()
			delete(f.bots[code], p.ID)
			f.mu.Unlock()
		}(p)
	}
	if len(names) == 0 {
		return "Свободных ботов нет: все уже в этой партии."
	}
	return "В партию " + code + " идут: " + strings.Join(names, ", ") + "."
}

// botView is the part of a poll's answer a bot needs.
type botView struct {
	V        int64     `json:"v"`
	Now      time.Time `json:"now"`
	Code     string    `json:"code"`
	Status   string    `json:"status"`
	Settings struct {
		Seconds int `json:"seconds"`
	} `json:"settings"`
	Me      int `json:"me"`
	Members []struct {
		M    int `json:"m"`
		From int `json:"from"`
	} `json:"members"`
	Rounds []struct {
		N        int        `json:"n"`
		StartsAt time.Time  `json:"starts_at"`
		ClosedAt *time.Time `json:"closed_at"`
		Answered []int      `json:"answered"`
	} `json:"rounds"`
	Next string `json:"next"`
}

// play is one bot in one party: it joins, polls every two seconds, answers
// each place at a moment of its own near the place at its skill, now and
// then lets one pass, and follows a rematch.
func (f *standFree) play(ctx context.Context, p Player, code string) {
	rng := rand.New(rand.NewPCG(uint64(p.ID), uint64(time.Now().UnixNano())))
	log := f.e.Log.With("part", "free-bot", "bot", p.Name, "code", code)
	var v botView
	join := func() bool {
		status, body := f.call(p, http.MethodPost, "/api/party/"+code+"/join", map[string]any{})
		if status != http.StatusOK {
			log.Info("bot could not join", "status", status, "body", strings.TrimSpace(string(body)))
			return false
		}
		v = botView{}
		return json.Unmarshal(body, &v) == nil
	}
	if !join() {
		return
	}
	at := map[int]time.Time{}
	idle := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		status, body := f.call(p, http.MethodGet, "/api/party/"+code+"?v="+strconv.FormatInt(v.V, 10), nil)
		if status == http.StatusNotFound || status == http.StatusForbidden {
			return
		}
		if status != http.StatusOK {
			continue
		}
		var got botView
		if json.Unmarshal(body, &got) != nil {
			continue
		}
		if got.Code != "" {
			v = got
		} else {
			v.Now = got.Now
		}
		switch v.Status {
		case free.StatusAbandoned:
			return
		case free.StatusDone:
			if v.Next != "" {
				code = v.Next
				at = map[int]time.Time{}
				if !join() {
					return
				}
				continue
			}
			if idle.IsZero() {
				idle = time.Now()
			}
			if time.Since(idle) > 5*time.Minute {
				return
			}
			continue
		case free.StatusPlaying:
		default:
			continue
		}
		if len(v.Rounds) == 0 {
			continue
		}
		r := v.Rounds[len(v.Rounds)-1]
		from := 1
		for _, m := range v.Members {
			if m.M == v.Me {
				from = m.From
			}
		}
		if r.ClosedAt != nil || v.Now.Before(r.StartsAt) || from > r.N || contains(r.Answered, v.Me) {
			continue
		}
		if at[r.N].IsZero() {
			share := 0.2 + 0.65*rng.Float64()
			if rng.IntN(12) == 0 {
				// Now and then a bot lets the time run out.
				share = 99
			}
			at[r.N] = r.StartsAt.Add(time.Duration(share * float64(v.Settings.Seconds) * float64(time.Second)))
		}
		if v.Now.Before(at[r.N]) {
			continue
		}
		truth, err := f.truth(ctx, code, r.N)
		if err != nil {
			log.Warn("bot cannot find the place", "error", err)
			continue
		}
		pin := near(rng, truth, p.Skill)
		status, body = f.call(p, http.MethodPost, "/api/party/"+code+"/guess", map[string]any{"n": r.N, "lat": pin.Lat, "lon": pin.Lon})
		if status != http.StatusOK {
			log.Info("bot answer refused", "status", status, "body", strings.TrimSpace(string(body)))
			at[r.N] = v.Now.Add(time.Hour)
			continue
		}
		_ = json.Unmarshal(body, &v)
	}
}

func contains(s []int, x int) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// truth is where place n of the party is. A bot reads it from the database:
// it is the stand's helper, not a player, and no API gives it away.
func (f *standFree) truth(ctx context.Context, code string, n int) (geo.Point, error) {
	var p geo.Point
	err := f.e.Pool.QueryRow(ctx, `
		SELECT pl.lat, pl.lon FROM maply.free_round r
		JOIN maply.free_game g ON g.id = r.game
		JOIN maply.place pl ON pl.id = r.place_id
		WHERE g.code = $1 AND r.n = $2`, code, n).Scan(&p.Lat, &p.Lon)
	return p, err
}

// call is a request to the real API as p from their DM, through the stand's
// own handler.
func (f *standFree) call(p Player, method, path string, body any) (int, []byte) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "tma "+f.e.SignLaunch(p, 0, "sender", ""))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	f.e.Handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// panel is the free game's block on /dev: ways in, the open parties and
// their bots.
func (f *standFree) panel(e *Env, r *http.Request) template.HTML {
	var b strings.Builder
	b.WriteString(`<section><h2>Свободная игра</h2><p>`)
	owner := e.Players[0]
	for _, p := range e.Players {
		if p.Owner {
			owner = p
		}
	}
	for _, l := range []struct{ text, platform string }{{"телефон", "ios"}, {"Telegram Desktop", "tdesktop"}} {
		fmt.Fprintf(&b, `<a class="btn" href="/dev/open?user=%d&path=/play&platform=%s">%s: %s</a> `,
			owner.ID, l.platform, template.HTMLEscapeString(owner.Name), template.HTMLEscapeString(l.text))
	}
	b.WriteString(`<a class="btn" href="/play">Сайт: гость</a></p>`)
	rows, err := e.Pool.Query(r.Context(), `
		SELECT g.code, g.status, g.mode, g.seconds, g.rounds,
		       (SELECT count(*) FROM maply.free_member m WHERE m.game = g.id AND m.left_at IS NULL)
		FROM maply.free_game g
		WHERE g.kind <> 'solo' AND g.status IN ('lobby', 'playing')
		ORDER BY g.created_at DESC LIMIT 6`)
	if err != nil {
		fmt.Fprintf(&b, `<p>%s</p></section>`, template.HTMLEscapeString(err.Error()))
		return template.HTML(b.String())
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var code, status, mode string
		var seconds, rounds *int
		var members int
		if rows.Scan(&code, &status, &mode, &seconds, &rounds, &members) != nil {
			continue
		}
		n++
		set := mode
		if seconds != nil && rounds != nil {
			set = fmt.Sprintf("%d с · %d мест", *seconds, *rounds)
		}
		fmt.Fprintf(&b, `<form method="post" action="/dev/free/bots"><b>%s</b> · %s · %s · игроков: %d
<input type="hidden" name="code" value="%s"><select name="n"><option>1</option><option>2</option><option selected>3</option><option>5</option><option>7</option></select>
<button class="btn">Добавить ботов</button>`, code, template.HTMLEscapeString(status), template.HTMLEscapeString(set), members, code)
		for _, p := range e.Players[1:3] {
			fmt.Fprintf(&b, ` <a href="/dev/free/invite?user=%d&code=%s">приглашение как %s</a>`, p.ID, url.QueryEscape(code), template.HTMLEscapeString(p.Name))
		}
		b.WriteString(`</form>`)
	}
	if n == 0 {
		b.WriteString(`<p>Открытых партий нет. Создай лобби в игре: «С друзьями».</p>`)
	}
	b.WriteString(`<form method="post" action="/dev/free/bots">Код <input name="code" size="10" placeholder="K7QF2MXA"> ботов <input name="n" value="3" size="2"> <button class="btn">Позвать</button></form></section>`)
	return template.HTML(b.String())
}
