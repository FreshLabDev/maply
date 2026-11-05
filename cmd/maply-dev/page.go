// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/round"
)

//go:embed page.html
var pageHTML string

var page = template.Must(template.New("dev").Funcs(template.FuncMap{
	"join": strings.Join,
}).Parse(pageHTML))

// mountDev adds the stand's own handlers under /dev.
func mountDev(e *Env, mux *http.ServeMux) {
	mux.HandleFunc("GET /dev", func(w http.ResponseWriter, r *http.Request) { e.page(w, r) })
	mux.HandleFunc("GET /dev/{$}", func(w http.ResponseWriter, r *http.Request) { e.page(w, r) })
	// /dev/open?user=1001&chat=-1001001&platform=tdesktop opens the Mini App as
	// that person, from that group (or their DM without chat).
	mux.HandleFunc("GET /dev/open", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		id, _ := strconv.ParseInt(q.Get("user"), 10, 64)
		p, ok := e.Player(id)
		if !ok {
			http.Error(w, "no such player", http.StatusNotFound)
			return
		}
		chat, _ := strconv.ParseInt(q.Get("chat"), 10, 64)
		u, err := e.LaunchURL(p, chat, q.Get("platform"), q.Get("path"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, u, http.StatusFound)
	})
	// /dev/answer?user=1002&chat=-1001001&km=300 answers today for that
	// person through the real API, km from the place, or at their own skill.
	mux.HandleFunc("POST /dev/answer", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.FormValue("user"), 10, 64)
		chat, _ := strconv.ParseInt(r.FormValue("chat"), 10, 64)
		km, _ := strconv.ParseFloat(r.FormValue("km"), 64)
		code, body := e.Answer(r.Context(), id, chat, km)
		e.back(w, r, fmt.Sprintf("%d %s", code, body))
	})
	// /dev/answer-all?chat=-1001001: everyone in the group who has not
	// answered today does, the stand's owner excepted.
	mux.HandleFunc("POST /dev/answer-all", func(w http.ResponseWriter, r *http.Request) {
		chat, _ := strconv.ParseInt(r.FormValue("chat"), 10, 64)
		var out []string
		for _, p := range e.Players {
			if p.Owner || !p.In(chat) {
				continue
			}
			code, _ := e.Answer(r.Context(), p.ID, chat, 0)
			out = append(out, fmt.Sprintf("%s %d", p.Name, code))
		}
		e.back(w, r, strings.Join(out, ", "))
	})
	// /dev/clock?add=60 moves the clock by minutes; reset=1 brings it back;
	// to=reveal|morning|lastreveal goes to that moment of the group's day.
	mux.HandleFunc("POST /dev/clock", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.FormValue("reset") != "":
			e.Clock.Reset()
		case r.FormValue("add") != "":
			m, _ := strconv.Atoi(r.FormValue("add"))
			e.Clock.Move(time.Duration(m) * time.Minute)
		case r.FormValue("to") != "":
			loc := e.Zones.For(groups[0].Zone, "")
			d := round.DateIn(e.Clock.Now(), loc)
			var t time.Time
			switch r.FormValue("to") {
			case "reveal":
				t = d.Reveal(loc).Add(time.Second)
			case "before-reveal":
				t = d.Reveal(loc).Add(-2 * time.Minute)
			case "morning":
				t = d.AddDays(1).At(7, 0, loc).Add(time.Second)
			case "lastreveal":
				t = e.Zones.LastReveal(d).Add(time.Second)
			}
			if !t.IsZero() {
				e.Clock.Set(t)
			}
		}
		e.back(w, r, "now "+e.Clock.Now().In(e.Zones.For(groups[0].Zone, "")).Format("Mon 2 Jan 15:04 MST"))
	})
	// /dev/fresh wipes the stand and seeds it again at the current clock.
	mux.HandleFunc("POST /dev/fresh", func(w http.ResponseWriter, r *http.Request) {
		if err := seed(context.WithoutCancel(r.Context()), e); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		e.back(w, r, "seeded again")
	})
	// /dev/state is the stand at a glance for scripts and tests.
	mux.HandleFunc("GET /dev/state", func(w http.ResponseWriter, r *http.Request) {
		st, err := e.state(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
}

func (p Player) In(chat int64) bool {
	for _, g := range p.Groups {
		if g == chat {
			return true
		}
	}
	return false
}

// back returns to the /dev page with a note, or answers a script in text.
func (e *Env) back(w http.ResponseWriter, r *http.Request, note string) {
	if r.FormValue("json") != "" || !strings.Contains(r.Header.Get("Accept"), "text/html") {
		fmt.Fprintln(w, note)
		return
	}
	http.Redirect(w, r, "/dev?note="+url.QueryEscape(note), http.StatusSeeOther)
}

// InitData signs a launch as p, from chat when it is a group.
func (e *Env) InitData(p Player, chat int64) (string, error) {
	f := map[string]string{
		// The verifier checks auth_date against real time, not the stand's.
		"auth_date": strconv.FormatInt(time.Now().Unix(), 10),
		"user": fmt.Sprintf(`{"id":%d,"first_name":%q,"username":%q,"language_code":%q}`,
			p.ID, p.Name, p.Username, p.Lang),
		"chat_instance": strconv.FormatInt(p.ID*7+chat, 10),
	}
	if chat != 0 {
		g, ok := e.Group(chat)
		if !ok {
			return "", fmt.Errorf("no such group %d", chat)
		}
		d := round.DateIn(e.Clock.Now(), e.Zones.For(g.Zone, ""))
		payload, err := e.Signer.Sign(auth.Payload{Date: d, ChatID: chat})
		if err != nil {
			return "", err
		}
		f["start_param"] = payload
		f["chat_type"] = "supergroup"
	} else {
		f["chat_type"] = "sender"
	}
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	v := url.Values{}
	for i, k := range keys {
		lines[i] = k + "=" + f[k]
		v.Set(k, f[k])
	}
	mac := func(k, m []byte) []byte { h := hmac.New(sha256.New, k); h.Write(m); return h.Sum(nil) }
	v.Set("hash", hex.EncodeToString(mac(mac([]byte("WebAppData"), []byte(BotToken)), []byte(strings.Join(lines, "\n")))))
	return v.Encode(), nil
}

// LaunchURL is the Mini App as Telegram opens it for p: at path (default /),
// with the signed launch in the hash.
func (e *Env) LaunchURL(p Player, chat int64, platform, path string) (string, error) {
	data, err := e.InitData(p, chat)
	if err != nil {
		return "", err
	}
	if platform == "" {
		platform = "ios"
	}
	if path == "" || !strings.HasPrefix(path, "/") {
		path = "/"
	}
	return path + "#tgWebAppData=" + url.QueryEscape(data) + "&tgWebAppVersion=8.0&tgWebAppPlatform=" + url.QueryEscape(platform), nil
}

// Answer gives p's answer of today through the real API, from chat or the DM.
func (e *Env) Answer(ctx context.Context, user, chat int64, km float64) (int, string) {
	p, ok := e.Player(user)
	if !ok {
		return http.StatusNotFound, "no such player"
	}
	data, err := e.InitData(p, chat)
	if err != nil {
		return http.StatusBadRequest, err.Error()
	}
	g := groups[0]
	if chat != 0 {
		g, _ = e.Group(chat)
	}
	d := round.DateIn(e.Clock.Now(), e.Zones.For(g.Zone, ""))
	r, ok, err := e.Store.RoundOn(ctx, d)
	if err != nil || !ok {
		return http.StatusConflict, "no round today yet: open the Mini App once"
	}
	answer, err := e.Store.Answer(ctx, r.PlaceID)
	if err != nil {
		return http.StatusInternalServerError, err.Error()
	}
	rng := rand.New(rand.NewPCG(uint64(user), uint64(e.Clock.Now().UnixNano())))
	pin := near(rng, answer.Point, p.Skill)
	if km > 0 {
		pin = Destination(answer.Point, rng.Float64()*360, km)
	}
	body := fmt.Sprintf(`{"lat":%f,"lon":%f}`, pin.Lat, pin.Lon)
	req := httptest.NewRequest(http.MethodPost, "/api/guess", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "tma "+data)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = fmt.Sprintf("10.0.%d.%d:1000", user/256%256, user%256)
	rec := httptest.NewRecorder()
	e.Handler.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

type standState struct {
	Now      time.Time       `json:"now"`
	Shift    string          `json:"shift"`
	Groups   []groupState    `json:"groups"`
	Answered map[int64]bool  `json:"answered"`
	Days     int             `json:"days_kept"`
	Places   map[string]int  `json:"places"`
	Rows     []playerRow     `json:"-"`
	Extra    []template.HTML `json:"-"`
	Note     string          `json:"-"`
}

type link struct{ Label, URL string }

type playerRow struct {
	Player
	Answered bool
	Where    string
	Links    []link
	// AnswerChat is where the stand answers for them: their first group,
	// else their DM.
	AnswerChat int64
}

type groupState struct {
	Group
	Date     string `json:"date"`
	DayNo    int    `json:"day_no"`
	Reveal   string `json:"reveal"`
	Revealed bool   `json:"revealed"`
}

func (e *Env) state(ctx context.Context) (standState, error) {
	now := e.Clock.Now()
	st := standState{Now: now, Shift: e.Clock.Shifted().Round(time.Minute).String(), Answered: map[int64]bool{}, Places: map[string]int{}}
	for _, g := range e.Groups {
		loc := e.Zones.For(g.Zone, "")
		d := round.DateIn(now, loc)
		gs := groupState{Group: g, Date: d.String(), Reveal: d.Reveal(loc).In(loc).Format("15:04 MST"), Revealed: now.After(d.Reveal(loc))}
		if r, ok, err := e.Store.RoundOn(ctx, d); err == nil && ok {
			gs.DayNo = r.DayNo
		}
		st.Groups = append(st.Groups, gs)
	}
	d := round.DateIn(now, e.Zones.For(groups[0].Zone, ""))
	rows, err := e.Pool.Query(ctx, `SELECT user_id FROM maply.guess WHERE date = $1`, d.Time())
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return st, err
		}
		st.Answered[id] = true
	}
	rows.Close()
	if err := e.Pool.QueryRow(ctx, `SELECT count(*) FROM maply.day_closed`).Scan(&st.Days); err != nil {
		return st, err
	}
	prow, err := e.Pool.Query(ctx, `SELECT status, count(*) FROM maply.place GROUP BY status`)
	if err != nil {
		return st, err
	}
	for prow.Next() {
		var s string
		var n int
		if err := prow.Scan(&s, &n); err != nil {
			prow.Close()
			return st, err
		}
		st.Places[s] = n
	}
	prow.Close()
	return st, nil
}

func (e *Env) page(w http.ResponseWriter, r *http.Request) {
	st, err := e.state(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	st.Note = r.URL.Query().Get("note")
	for _, p := range e.Players {
		row := playerRow{Player: p, Answered: st.Answered[p.ID]}
		var where []string
		for _, gid := range p.Groups {
			g, _ := e.Group(gid)
			where = append(where, g.Title)
			row.Links = append(row.Links,
				link{g.Title, "/dev/open?user=" + strconv.FormatInt(p.ID, 10) + "&chat=" + strconv.FormatInt(gid, 10)},
				link{g.Title + " · Desktop", "/dev/open?platform=tdesktop&user=" + strconv.FormatInt(p.ID, 10) + "&chat=" + strconv.FormatInt(gid, 10)})
		}
		if p.Zone != "" {
			where = append(where, "личка "+p.Zone)
			row.Links = append(row.Links,
				link{"Личка", "/dev/open?user=" + strconv.FormatInt(p.ID, 10)},
				link{"Личка · Desktop", "/dev/open?platform=tdesktop&user=" + strconv.FormatInt(p.ID, 10)})
		}
		if len(p.Groups) > 0 {
			row.AnswerChat = p.Groups[0]
		}
		row.Where = strings.Join(where, ", ")
		st.Rows = append(st.Rows, row)
	}
	for _, x := range extensions {
		if x.Panel != nil {
			st.Extra = append(st.Extra, x.Panel(e, r))
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := page.Execute(w, st); err != nil {
		e.Log.Warn("dev page", "error", err)
	}
}
