// SPDX-License-Identifier: Apache-2.0
package main

// /dev/tg: the fake Telegram's chats as a page. A player writes, taps the
// bot's buttons and opens the Mini App from them; the page draws what the
// bot sent, edited and pinned.

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
)

//go:embed chats.html
var chatsHTML string

func (f *fakeTelegram) mountPage(e *Env, mux *http.ServeMux) {
	mux.HandleFunc("GET /dev/tg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(chatsHTML))
	})
	mux.HandleFunc("GET /dev/tg/state", func(w http.ResponseWriter, r *http.Request) {
		chat, _ := strconv.ParseInt(r.URL.Query().Get("chat"), 10, 64)
		since, _ := strconv.ParseInt(r.URL.Query().Get("v"), 10, 64)
		f.mu.Lock()
		ch := f.changed
		v := f.version
		f.mu.Unlock()
		if since >= v {
			select {
			case <-ch:
			case <-time.After(25 * time.Second):
			case <-r.Context().Done():
				return
			}
		}
		writeJSON(w, f.state(r.Context(), chat))
	})
	mux.HandleFunc("GET /dev/tg/file/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		file, ok := f.files[r.PathValue("id")]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "max-age=86400")
		http.ServeFile(w, r, file.Path)
	})
	mux.HandleFunc("POST /dev/tg/say", func(w http.ResponseWriter, r *http.Request) {
		p, chat, err := f.actor(r)
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		text := strings.TrimSpace(r.FormValue("text"))
		if text == "" {
			writeJSON(w, action{Error: "пустое сообщение"})
			return
		}
		f.Say(p, chat, text, nil)
		writeJSON(w, action{})
	})
	// A location near today's place: km away, or at the player's own skill.
	mux.HandleFunc("POST /dev/tg/location", func(w http.ResponseWriter, r *http.Request) {
		p, chat, err := f.actor(r)
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		pt, err := f.nearToday(r.Context(), p, r.FormValue("km"))
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		f.Say(p, chat, "", &pt)
		writeJSON(w, action{})
	})
	mux.HandleFunc("POST /dev/tg/press", func(w http.ResponseWriter, r *http.Request) {
		p, chat, err := f.actor(r)
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		msg, _ := strconv.ParseInt(r.FormValue("msg"), 10, 64)
		row, _ := strconv.Atoi(r.FormValue("row"))
		col, _ := strconv.Atoi(r.FormValue("col"))
		b, err := f.button(chat, msg, row, col)
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		switch {
		case b.Data != "":
			if err := f.Press(p, chat, msg, b.Data); err != nil {
				writeJSON(w, action{Error: err.Error()})
				return
			}
			writeJSON(w, action{})
		case b.WebApp != "":
			writeJSON(w, f.webApp(p, chat, b.WebApp, r.FormValue("platform")))
		default:
			writeJSON(w, f.link(p, chat, b.URL, r.FormValue("platform")))
		}
	})
	// A link the page or the Mini App opens: t.me links act as Telegram's.
	mux.HandleFunc("POST /dev/tg/link", func(w http.ResponseWriter, r *http.Request) {
		p, chat, err := f.actor(r)
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		writeJSON(w, f.link(p, chat, r.FormValue("url"), r.FormValue("platform")))
	})
	// The menu button of a DM, or the Mini App opened from the chat's header.
	mux.HandleFunc("POST /dev/tg/app", func(w http.ResponseWriter, r *http.Request) {
		p, chat, err := f.actor(r)
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		if chat < 0 {
			writeJSON(w, f.openApp(p, chat, "supergroup", f.groupParam(chat), "/", r.FormValue("platform")))
			return
		}
		writeJSON(w, f.webApp(p, chat, "http://"+e.Addr+"/", r.FormValue("platform")))
	})
	mux.HandleFunc("GET /dev/tg/prepared", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		pr, ok := f.prepared[r.URL.Query().Get("id")]
		f.mu.Unlock()
		if !ok {
			writeJSON(w, action{Error: "prepared message expired"})
			return
		}
		writeJSON(w, map[string]any{"user": pr.User, "result": pr.Result})
	})
	mux.HandleFunc("POST /dev/tg/share", func(w http.ResponseWriter, r *http.Request) {
		p, chat, err := f.actor(r)
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		if err := f.SharePrepared(p, chat, r.FormValue("id")); err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		writeJSON(w, action{Switch: chat})
	})
	mux.HandleFunc("POST /dev/tg/member", func(w http.ResponseWriter, r *http.Request) {
		p, chat, err := f.actor(r)
		if err != nil {
			writeJSON(w, action{Error: err.Error()})
			return
		}
		if chat > 0 {
			writeJSON(w, action{Error: "только для групп"})
			return
		}
		f.Membership(p, chat, r.FormValue("in") == "1")
		writeJSON(w, action{})
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// action is what the page does after a tap.
type action struct {
	Error string `json:"error,omitempty"`
	// Open is a Mini App launch to show in the sheet.
	Open string `json:"open,omitempty"`
	// Switch is a chat to show.
	Switch int64 `json:"switch,omitempty"`
	// Share is text to post into a chat the person picks, as t.me/share does.
	Share string `json:"share,omitempty"`
	// External is a link outside Telegram, for a new tab.
	External string `json:"external,omitempty"`
	Note     string `json:"note,omitempty"`
}

// actor is the player the request acts as, and the chat.
func (f *fakeTelegram) actor(r *http.Request) (Player, int64, error) {
	id, _ := strconv.ParseInt(r.FormValue("user"), 10, 64)
	chat, _ := strconv.ParseInt(r.FormValue("chat"), 10, 64)
	p, ok := f.e.Player(id)
	if !ok {
		return Player{}, 0, errors.New("нет такого игрока")
	}
	if chat == 0 {
		chat = p.ID
	}
	if chat > 0 && chat != p.ID {
		return Player{}, 0, errors.New("в чужой личке писать нельзя")
	}
	if chat < 0 && !p.In(chat) {
		return Player{}, 0, fmt.Errorf("%s не в этой группе", p.Name)
	}
	return p, chat, nil
}

func (f *fakeTelegram) button(chat, msg int64, row, col int) (fakeButton, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.chats[chat]
	if !ok {
		return fakeButton{}, errors.New("нет такого чата")
	}
	_, m := c.find(msg)
	if m == nil || row < 0 || row >= len(m.Buttons) || col < 0 || col >= len(m.Buttons[row]) {
		return fakeButton{}, errors.New("кнопки уже нет")
	}
	return m.Buttons[row][col], nil
}

// groupParam is the start parameter the bot's own group link carries.
func (f *fakeTelegram) groupParam(chat int64) string {
	g, ok := f.e.Group(chat)
	if !ok {
		return ""
	}
	d := round.DateIn(f.e.Clock.Now(), f.e.Zones.For(g.Zone, ""))
	s, _ := f.e.Signer.Sign(auth.Payload{Date: d, ChatID: chat})
	return s
}

func (f *fakeTelegram) openApp(p Player, chat int64, chatType, startParam, path, platform string) action {
	return action{Open: f.e.LaunchWith(p, chat, chatType, startParam, platform, path)}
}

// webApp opens a web_app button: its path on the stand, as a DM launch.
func (f *fakeTelegram) webApp(p Player, chat int64, raw, platform string) action {
	u, err := url.Parse(raw)
	if err != nil {
		return action{Error: "плохой адрес кнопки: " + raw}
	}
	path := u.EscapedPath()
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return f.openApp(p, chat, "", "", path, platform)
}

// link does what Telegram does with a link: t.me links of the bot open the
// Mini App or the DM, t.me/share asks where to send, others leave Telegram.
func (f *fakeTelegram) link(p Player, chat int64, raw, platform string) action {
	u, err := url.Parse(raw)
	if err != nil {
		return action{Error: "плохая ссылка: " + raw}
	}
	q := u.Query()
	var path []string
	switch {
	case u.Scheme == "tg" && u.Host == "resolve":
		path = []string{q.Get("domain")}
		if a := q.Get("appname"); a != "" {
			path = append(path, a)
		}
	case u.Host == "t.me" || u.Host == "telegram.me" || u.Host == "www.t.me":
		path = strings.Split(strings.Trim(u.Path, "/"), "/")
	case u.Host == f.e.Addr:
		return f.webApp(p, chat, raw, platform)
	default:
		return action{External: raw}
	}
	if len(path) == 0 || path[0] == "" {
		return action{External: raw}
	}
	if path[0] == "share" {
		text := q.Get("text")
		if link := q.Get("url"); link != "" {
			text = strings.TrimSpace(link + "\n" + text)
		}
		return action{Share: text}
	}
	if !strings.EqualFold(path[0], botUsername) {
		return action{Note: "Ссылка на другой чат или бота: " + raw}
	}
	if len(path) >= 2 {
		chatType := "sender"
		if chat < 0 {
			chatType = "supergroup"
		}
		return f.openApp(p, chat, chatType, q.Get("startapp"), "/", platform)
	}
	switch {
	case q.Has("startgroup"):
		return action{Note: "В Telegram тут откроется выбор группы. На стенде бот уже в обеих группах; убрать и добавить его можно в шапке группы."}
	case q.Has("start"):
		text := "/start"
		if s := q.Get("start"); s != "" {
			text += " " + s
		}
		f.Say(p, p.ID, text, nil)
		return action{Switch: p.ID}
	}
	return action{Switch: p.ID}
}

func (f *fakeTelegram) nearToday(ctx context.Context, p Player, km string) (geo.Point, error) {
	d := round.DateIn(f.e.Clock.Now(), f.e.Zones.For(p.Zone, p.Lang))
	r, ok, err := f.e.Store.RoundOn(ctx, d)
	if err != nil || !ok {
		return geo.Point{}, errors.New("сегодняшнего дня ещё нет")
	}
	answer, err := f.e.Store.Answer(ctx, r.PlaceID)
	if err != nil {
		return geo.Point{}, err
	}
	rng := rand.New(rand.NewPCG(uint64(p.ID), uint64(time.Now().UnixNano())))
	if n, _ := strconv.ParseFloat(km, 64); n > 0 {
		return Destination(answer.Point, rng.Float64()*360, n), nil
	}
	return near(rng, answer.Point, p.Skill), nil
}

// ---------- the page's view ----------

type chatView struct {
	ID      int64   `json:"id"`
	Type    string  `json:"type"`
	Title   string  `json:"title"`
	Last    string  `json:"last"`
	LastAt  int64   `json:"last_at"`
	Count   int     `json:"count"`
	Members []int64 `json:"members"`
}

type msgView struct {
	*fakeMsg
	HTML    template.HTML `json:"html"`
	FileURL string        `json:"file_url,omitempty"`
	Pinned  bool          `json:"pinned"`
	// Time is when it was sent, on the stand's clock in the chat's zone.
	Time string `json:"time"`
}

type openChat struct {
	*fakeChat
	Msgs   []msgView   `json:"msgs"`
	Menu   *fakeButton `json:"menu,omitempty"`
	Pinned []msgView   `json:"pinned_msgs"`
}

type playerView struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Lang   string  `json:"lang"`
	Groups []int64 `json:"groups"`
	Owner  bool    `json:"owner"`
}

type tgState struct {
	Version  int64        `json:"version"`
	Now      string       `json:"now"`
	Shift    string       `json:"shift"`
	Bot      bool         `json:"bot"`
	Players  []playerView `json:"players"`
	Chats    []chatView   `json:"chats"`
	Chat     *openChat    `json:"chat,omitempty"`
	Commands []string     `json:"commands"`
	Toasts   []fakeToast  `json:"toasts"`
	Calls    []fakeCall   `json:"calls"`
}

func (f *fakeTelegram) state(_ context.Context, open int64) tgState {
	f.mu.Lock()
	defer f.mu.Unlock()
	loc := f.e.Zones.For(f.e.Groups[0].Zone, "")
	st := tgState{
		Version: f.version, Bot: f.e.Bot,
		Now:    f.e.Clock.Now().In(loc).Format("Mon 2 Jan 15:04 MST"),
		Shift:  f.e.Clock.Shifted().Round(time.Minute).String(),
		Toasts: slices.Clone(f.toasts),
	}
	if n := len(f.calls); n > 60 {
		st.Calls = slices.Clone(f.calls[n-60:])
	} else {
		st.Calls = slices.Clone(f.calls)
	}
	for _, p := range f.e.Players {
		st.Players = append(st.Players, playerView{ID: p.ID, Name: p.Name, Lang: p.Lang, Groups: p.Groups, Owner: p.Owner})
	}
	add := func(id int64) {
		c := f.chatLocked(id)
		v := chatView{ID: id, Type: c.Type, Title: c.Title, Count: len(c.Msgs)}
		if id < 0 {
			for _, p := range f.e.Players {
				if p.In(id) {
					v.Members = append(v.Members, p.ID)
				}
			}
		} else {
			v.Members = []int64{id}
		}
		if n := len(c.Msgs); n > 0 {
			m := c.Msgs[n-1]
			v.Last, v.LastAt = preview(m), m.Date
		}
		st.Chats = append(st.Chats, v)
	}
	for _, g := range f.e.Groups {
		add(g.ID)
	}
	for _, p := range f.e.Players {
		add(p.ID)
	}
	dms := st.Chats[len(f.e.Groups):]
	sort.SliceStable(dms, func(i, j int) bool { return dms[i].LastAt > dms[j].LastAt })
	// Empty lists stay lists for the page, never null.
	st.Toasts = append([]fakeToast{}, st.Toasts...)
	st.Calls = append([]fakeCall{}, st.Calls...)
	st.Commands = []string{}
	if open != 0 {
		c := f.chatLocked(open)
		oc := &openChat{fakeChat: c, Menu: c.Menu}
		if oc.Menu == nil && c.Type == "private" {
			oc.Menu = f.menu
		}
		loc := f.zoneOf(open)
		for _, m := range c.Msgs {
			oc.Msgs = append(oc.Msgs, f.view(m, slices.Contains(c.Pinned, m.ID), loc))
		}
		for i := len(c.Pinned) - 1; i >= 0; i-- {
			if _, m := c.find(c.Pinned[i]); m != nil {
				oc.Pinned = append(oc.Pinned, f.view(m, true, loc))
			}
		}
		st.Chat = oc
		key := ""
		if c.Type != "private" {
			key = "all_group_chats/"
		} else {
			key = "all_private_chats/"
		}
		lang := ""
		if p, ok := f.e.Player(open); ok {
			lang = p.Lang
		} else if g, ok := f.e.Group(open); ok {
			lang = g.Lang
		}
		for _, k := range []string{key + lang, key, lang, ""} {
			if cmds, ok := f.commands[k]; ok {
				st.Commands = cmds
				break
			}
		}
	}
	return st
}

// zoneOf is the chat's own time zone, for the times on the page.
func (f *fakeTelegram) zoneOf(chat int64) *time.Location {
	if g, ok := f.e.Group(chat); ok {
		return f.e.Zones.For(g.Zone, g.Lang)
	}
	if p, ok := f.e.Player(chat); ok {
		return f.e.Zones.For(p.Zone, p.Lang)
	}
	return time.UTC
}

func (f *fakeTelegram) view(m *fakeMsg, pinned bool, loc *time.Location) msgView {
	v := msgView{fakeMsg: m, HTML: renderHTML(m.Body), Pinned: pinned, Time: time.Unix(m.Date, 0).In(loc).Format("15:04")}
	switch m.Media {
	case "photo", "animation":
		if _, ok := f.files[m.File]; ok {
			v.FileURL = "/dev/tg/file/" + m.File
		}
	case "photo-url":
		v.FileURL = m.File
	}
	return v
}

func preview(m *fakeMsg) string {
	s := plainOf(m.Body)
	switch {
	case m.Location != nil:
		s = "📍 геопозиция"
	case m.Media == "animation" && s == "":
		s = "GIF"
	case strings.HasPrefix(m.Media, "photo") && s == "":
		s = "Фото"
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 70 {
		s = string(r[:70]) + "…"
	}
	if !m.FromBot {
		s = m.From + ": " + s
	}
	return s
}

// panel is the block on /dev.
func (f *fakeTelegram) panel(e *Env, _ *http.Request) template.HTML {
	f.mu.Lock()
	n := 0
	for _, c := range f.chats {
		n += len(c.Msgs)
	}
	f.mu.Unlock()
	state := "бот и планировщик работают на поддельном Telegram"
	if !e.Bot {
		state = "бот выключен флагом -bot=false"
	}
	return template.HTML(fmt.Sprintf(`<section><h2>Telegram</h2><p>%s. Сообщений в чатах: %d.</p>
<p><a class="btn" href="/dev/tg">Открыть чаты</a></p></section>`, template.HTMLEscapeString(state), n))
}
