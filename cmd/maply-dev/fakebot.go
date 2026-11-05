// SPDX-License-Identifier: Apache-2.0
package main

// A Bot API server inside the stand. The real bot and the real scheduler talk
// to it as they would to api.telegram.org; the /dev/tg page shows its chats
// and turns the players' messages and button presses into updates. Nothing
// leaves the machine: there is no Telegram behind it.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/avatar"
	"github.com/FreshLabDev/maply/internal/bot"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/profile"
	"github.com/FreshLabDev/maply/internal/schedule"
	"github.com/FreshLabDev/maply/internal/telegram"
	"github.com/FreshLabDev/maply/internal/world"
)

// The fake bot's own identity, matching BotToken.
const (
	botID       int64 = 123456
	botUsername       = "maplyin_bot"
	botName           = "maply"
)

func init() {
	Register(Extension{
		Name:  "tg",
		Mount: func(e *Env, mux *http.ServeMux) { fakeTG.mount(e, mux) },
		// A fresh stand starts with empty chats.
		Seed:  func(context.Context, *Env) error { fakeTG.reset(); return nil },
		Start: func(ctx context.Context, e *Env) (func(), error) { return fakeTG.run(ctx, e) },
		Panel: fakeTG.panel,
	})
}

var fakeTG = &fakeTelegram{}

type fakeTelegram struct {
	e *Env

	mu sync.Mutex
	// wake is closed and replaced when an update is queued; changed, when
	// anything the page shows changes.
	wake, changed chan struct{}
	version       int64
	nextUpdate    int64
	updates       []fakeUpdate
	chats         map[int64]*fakeChat
	files         map[string]fakeFile
	fileSeq       int
	commands      map[string][]string // language ("" is the default) to commands
	descriptions  map[string]string
	menu          *fakeButton // the default menu button
	calls         []fakeCall
	toasts        []fakeToast
	callbacks     map[string]int64 // callback id to the person who pressed
	prepared      map[string]fakePrepared
	seq           int
}

type fakeUpdate struct {
	ID  int64
	Raw json.RawMessage
}

type fakeChat struct {
	ID     int64      `json:"id"`
	Type   string     `json:"type"`
	Title  string     `json:"title"`
	Msgs   []*fakeMsg `json:"-"`
	next   int64
	Pinned []int64     `json:"pinned"`
	Menu   *fakeButton `json:"menu,omitempty"`
}

type fakeMsg struct {
	ID      int64          `json:"id"`
	FromBot bool           `json:"from_bot"`
	FromID  int64          `json:"from_id"`
	From    string         `json:"from"`
	Date    int64          `json:"date"`
	Body    string         `json:"body"` // Telegram HTML; a player's text is escaped
	Media   string         `json:"media,omitempty"`
	File    string         `json:"file,omitempty"`
	Buttons [][]fakeButton `json:"buttons,omitempty"`
	Edits   int            `json:"edits"`
	Thread  int            `json:"thread,omitempty"`
	Via     bool           `json:"via,omitempty"`
	// Location is a player's location message.
	Location *geo.Point `json:"location,omitempty"`
}

type fakeButton struct {
	Text   string `json:"text"`
	Data   string `json:"data,omitempty"`
	URL    string `json:"url,omitempty"`
	WebApp string `json:"web_app,omitempty"`
	Style  string `json:"style,omitempty"`
}

type fakeFile struct {
	Path string
	Kind string // photo, animation
}

type fakeCall struct {
	At     time.Time `json:"at"`
	Method string    `json:"method"`
	Chat   int64     `json:"chat,omitempty"`
	Error  string    `json:"error,omitempty"`
}

type fakeToast struct {
	Seq   int    `json:"seq"`
	User  int64  `json:"user"`
	Text  string `json:"text"`
	Alert bool   `json:"alert"`
}

// fakePrepared is a savePreparedInlineMessage result, for the Mini App's
// shareMessage.
type fakePrepared struct {
	User   int64
	Result map[string]any
}

// apiError is Telegram's refusal, with its code and words, so tg's helpers
// such as IsMessageNotModified read it as they read the real one.
type apiError struct {
	Code int
	Desc string
}

func (a *apiError) Error() string { return a.Desc }

func badRequest(desc string) error { return &apiError{400, "Bad Request: " + desc} }

func (f *fakeTelegram) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = nil
	f.chats = map[int64]*fakeChat{}
	f.toasts = nil
	f.calls = nil
	f.callbacks = map[string]int64{}
	f.prepared = map[string]fakePrepared{}
	f.touchLocked()
}

func (f *fakeTelegram) init(e *Env) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.e != nil {
		return
	}
	f.e = e
	f.wake = make(chan struct{})
	f.changed = make(chan struct{})
	f.chats = map[int64]*fakeChat{}
	f.files = map[string]fakeFile{}
	f.commands = map[string][]string{}
	f.descriptions = map[string]string{}
	f.callbacks = map[string]int64{}
	f.prepared = map[string]fakePrepared{}
	// Update ids go on from what the bot last saw, or it would skip them.
	var offset int64
	_ = e.Pool.QueryRow(context.Background(), `SELECT telegram_offset FROM maply.runtime_state WHERE singleton`).Scan(&offset)
	f.nextUpdate = max(offset, 1)
}

// touchLocked tells the page something changed. f.mu is held.
func (f *fakeTelegram) touchLocked() {
	f.version++
	if f.changed != nil {
		close(f.changed)
	}
	f.changed = make(chan struct{})
}

func (f *fakeTelegram) chatLocked(id int64) *fakeChat {
	if c, ok := f.chats[id]; ok {
		return c
	}
	c := &fakeChat{ID: id, Type: "private", next: 1}
	if id < 0 {
		c.Type = "supergroup"
		c.Title = fmt.Sprintf("group %d", id)
		if g, ok := f.e.Group(id); ok {
			c.Title = g.Title
		}
	} else if p, ok := f.e.Player(id); ok {
		c.Title = p.Name
	}
	f.chats[id] = c
	return c
}

func (c *fakeChat) find(id int64) (int, *fakeMsg) {
	for i, m := range c.Msgs {
		if m.ID == id {
			return i, m
		}
	}
	return -1, nil
}

func (c *fakeChat) wire() map[string]any {
	w := map[string]any{"id": c.ID, "type": c.Type}
	if c.Type == "private" {
		w["first_name"] = c.Title
	} else {
		w["title"] = c.Title
	}
	return w
}

func botUser() map[string]any {
	return map[string]any{"id": botID, "is_bot": true, "first_name": botName, "username": botUsername}
}

func playerUser(p Player) map[string]any {
	u := map[string]any{"id": p.ID, "is_bot": false, "first_name": p.Name, "language_code": p.Lang}
	if p.Username != "" {
		u["username"] = p.Username
	}
	return u
}

// wire is a message as the Bot API returns it.
func (f *fakeTelegram) wire(c *fakeChat, m *fakeMsg) map[string]any {
	w := map[string]any{"message_id": m.ID, "date": m.Date, "chat": c.wire()}
	if m.FromBot {
		w["from"] = botUser()
	} else if p, ok := f.e.Player(m.FromID); ok {
		w["from"] = playerUser(p)
	}
	if m.Thread != 0 {
		w["message_thread_id"] = m.Thread
	}
	text := m.Body
	if m.FromBot {
		text = plainOf(m.Body)
	}
	switch m.Media {
	case "":
		w["text"] = text
	case "photo":
		w["caption"] = text
		w["photo"] = []map[string]any{
			{"file_id": m.File + "_s", "file_unique_id": m.File + "s", "width": 320, "height": 180},
			{"file_id": m.File, "file_unique_id": m.File + "u", "width": 1280, "height": 720},
		}
	case "animation":
		w["caption"] = text
		file := map[string]any{"file_id": m.File, "file_unique_id": m.File + "u", "file_name": "maply.mp4", "mime_type": "video/mp4"}
		w["animation"] = file
		w["document"] = file
	}
	if m.Location != nil {
		w["location"] = map[string]any{"latitude": m.Location.Lat, "longitude": m.Location.Lon}
	}
	if len(m.Buttons) > 0 {
		w["reply_markup"] = map[string]any{"inline_keyboard": wireButtons(m.Buttons)}
	}
	if m.Edits > 0 {
		w["edit_date"] = m.Date
	}
	return w
}

func wireButtons(rows [][]fakeButton) [][]map[string]any {
	out := make([][]map[string]any, len(rows))
	for i, row := range rows {
		for _, b := range row {
			w := map[string]any{"text": b.Text}
			switch {
			case b.Data != "":
				w["callback_data"] = b.Data
			case b.URL != "":
				w["url"] = b.URL
			case b.WebApp != "":
				w["web_app"] = map[string]any{"url": b.WebApp}
			}
			if b.Style != "" {
				w["style"] = b.Style
			}
			out[i] = append(out[i], w)
		}
	}
	return out
}

// ---------- requests ----------

// params are a request's parameters, from JSON, a form or the query; values
// of a form are text and JSON inside, as Telegram reads them.
type params map[string]any

func readParams(r *http.Request) (params, map[string][]byte, error) {
	p := params{}
	files := map[string][]byte{}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch ct {
	case "application/json":
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		if err := dec.Decode(&p); err != nil && !errors.Is(err, io.EOF) {
			return nil, nil, err
		}
	case "multipart/form-data":
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			return nil, nil, err
		}
		for k, v := range r.MultipartForm.Value {
			if len(v) > 0 {
				p[k] = v[0]
			}
		}
		for k, fh := range r.MultipartForm.File {
			if len(fh) == 0 {
				continue
			}
			fd, err := fh[0].Open()
			if err != nil {
				return nil, nil, err
			}
			data, err := io.ReadAll(fd)
			fd.Close()
			if err != nil {
				return nil, nil, err
			}
			files[k] = data
		}
	default:
		if err := r.ParseForm(); err != nil {
			return nil, nil, err
		}
		for k, v := range r.Form {
			if len(v) > 0 {
				p[k] = v[0]
			}
		}
	}
	return p, files, nil
}

func (p params) str(k string) string {
	switch v := p[k].(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func (p params) int(k string) int64 {
	n, _ := strconv.ParseInt(p.str(k), 10, 64)
	return n
}

// obj reads a JSON parameter, sent as an object or as text.
func (p params) obj(k string, out any) error {
	v, ok := p[k]
	if !ok || v == nil {
		return nil
	}
	var raw []byte
	if s, isStr := v.(string); isStr {
		raw = []byte(s)
	} else {
		raw, _ = json.Marshal(v)
	}
	return json.Unmarshal(raw, out)
}

func (p params) buttons() ([][]fakeButton, error) {
	var mk struct {
		Rows [][]struct {
			Text         string `json:"text"`
			CallbackData string `json:"callback_data"`
			URL          string `json:"url"`
			WebApp       *struct {
				URL string `json:"url"`
			} `json:"web_app"`
			Style string `json:"style"`
		} `json:"inline_keyboard"`
	}
	if err := p.obj("reply_markup", &mk); err != nil {
		return nil, badRequest("can't parse reply keyboard markup JSON object")
	}
	var out [][]fakeButton
	for _, row := range mk.Rows {
		var r []fakeButton
		for _, b := range row {
			fb := fakeButton{Text: b.Text, Data: b.CallbackData, URL: b.URL, Style: b.Style}
			if b.WebApp != nil {
				fb.WebApp = b.WebApp.URL
			}
			if len(fb.Data) > 64 {
				return nil, badRequest("BUTTON_DATA_INVALID")
			}
			r = append(r, fb)
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeTelegram) mount(e *Env, mux *http.ServeMux) {
	f.init(e)
	mux.HandleFunc("/tgapi/", f.serveAPI)
	f.mountPage(e, mux)
}

func (f *fakeTelegram) serveAPI(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/tgapi/")
	token, method, ok := strings.Cut(strings.TrimPrefix(rest, "bot"), "/")
	if !strings.HasPrefix(rest, "bot") || !ok {
		writeAPI(w, nil, &apiError{404, "Not Found"})
		return
	}
	if token != BotToken {
		writeAPI(w, nil, &apiError{401, "Unauthorized"})
		return
	}
	p, files, err := readParams(r)
	if err != nil {
		writeAPI(w, nil, badRequest("can't parse request: "+err.Error()))
		return
	}
	name := strings.ToLower(method)
	if name == "getupdates" {
		f.getUpdates(w, r, p)
		return
	}
	res, err := f.call(name, p, files)
	if name != "getme" {
		f.logCall(method, p.int("chat_id"), err)
	}
	writeAPI(w, res, err)
}

func writeAPI(w http.ResponseWriter, res any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		var a *apiError
		if !errors.As(err, &a) {
			a = &apiError{500, "Internal Server Error: " + err.Error()}
		}
		w.WriteHeader(a.Code)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": a.Code, "description": a.Desc})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": res})
}

func (f *fakeTelegram) logCall(method string, chat int64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := fakeCall{At: time.Now(), Method: method, Chat: chat}
	if err != nil {
		c.Error = err.Error()
	}
	f.calls = append(f.calls, c)
	if len(f.calls) > 200 {
		f.calls = f.calls[len(f.calls)-200:]
	}
	f.touchLocked()
}

func (f *fakeTelegram) getUpdates(w http.ResponseWriter, r *http.Request, p params) {
	offset := p.int("offset")
	timeout := time.Duration(min(max(p.int("timeout"), 0), 50)) * time.Second
	deadline := time.Now().Add(timeout)
	for {
		f.mu.Lock()
		f.updates = slices.DeleteFunc(f.updates, func(u fakeUpdate) bool { return u.ID < offset })
		if len(f.updates) > 0 {
			out := make([]json.RawMessage, len(f.updates))
			for i, u := range f.updates {
				out[i] = u.Raw
			}
			f.mu.Unlock()
			writeAPI(w, out, nil)
			return
		}
		wake := f.wake
		f.mu.Unlock()
		left := time.Until(deadline)
		if left <= 0 {
			writeAPI(w, []json.RawMessage{}, nil)
			return
		}
		select {
		case <-wake:
		case <-time.After(left):
		case <-r.Context().Done():
			return
		}
	}
}

func (f *fakeTelegram) call(method string, p params, files map[string][]byte) (any, error) {
	switch method {
	case "getme":
		return map[string]any{"id": botID, "is_bot": true, "first_name": botName, "username": botUsername,
			"can_join_groups": true, "supports_inline_queries": true}, nil
	case "deletewebhook", "sendchataction", "logout", "close", "setmessagereaction", "setmydefaultadministratorrights":
		return true, nil
	case "setmycommands":
		return f.setCommands(p)
	case "setmydescription", "setmyshortdescription":
		field := "description"
		if method == "setmyshortdescription" {
			field = "short_description"
		}
		f.mu.Lock()
		f.descriptions[method+"/"+p.str("language_code")] = p.str(field)
		f.mu.Unlock()
		return true, nil
	case "getmydescription":
		f.mu.Lock()
		defer f.mu.Unlock()
		return map[string]any{"description": f.descriptions["setmydescription/"+p.str("language_code")]}, nil
	case "getmyshortdescription":
		f.mu.Lock()
		defer f.mu.Unlock()
		return map[string]any{"short_description": f.descriptions["setmyshortdescription/"+p.str("language_code")]}, nil
	case "setchatmenubutton":
		return f.setMenu(p)
	case "answercallbackquery":
		return f.answerCallback(p)
	case "sendmessage":
		return f.send(p, files, "")
	case "sendphoto":
		return f.send(p, files, "photo")
	case "sendanimation":
		return f.send(p, files, "animation")
	case "editmessagetext", "editmessagecaption", "editmessagereplymarkup", "editmessagemedia":
		return f.edit(method, p, files)
	case "pinchatmessage", "unpinchatmessage", "unpinallchatmessages":
		return f.pin(method, p)
	case "deletemessage":
		return f.delete(p)
	case "getchatmember":
		return f.member(p)
	case "getchat":
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.chatLocked(p.int("chat_id")).wire(), nil
	case "getchatmembercount":
		n := 0
		for _, pl := range f.e.Players {
			if pl.In(p.int("chat_id")) {
				n++
			}
		}
		return n + 1, nil
	case "getuserprofilephotos":
		return map[string]any{"total_count": 0, "photos": []any{}}, nil
	case "savepreparedinlinemessage":
		return f.savePrepared(p)
	}
	f.e.Log.Warn("fake telegram: method the stand does not know, answered true", "method", method)
	return true, nil
}

func (f *fakeTelegram) setCommands(p params) (any, error) {
	var cmds []struct {
		Command     string `json:"command"`
		Description string `json:"description"`
	}
	if err := p.obj("commands", &cmds); err != nil {
		return nil, badRequest("can't parse commands")
	}
	var scope struct {
		Type string `json:"type"`
	}
	_ = p.obj("scope", &scope)
	key := p.str("language_code")
	if scope.Type != "" && scope.Type != "default" {
		key = scope.Type + "/" + key
	}
	list := make([]string, len(cmds))
	for i, c := range cmds {
		list[i] = "/" + c.Command + " " + c.Description
	}
	f.mu.Lock()
	f.commands[key] = list
	f.touchLocked()
	f.mu.Unlock()
	return true, nil
}

func (f *fakeTelegram) setMenu(p params) (any, error) {
	var mb struct {
		Type   string `json:"type"`
		Text   string `json:"text"`
		WebApp *struct {
			URL string `json:"url"`
		} `json:"web_app"`
	}
	if err := p.obj("menu_button", &mb); err != nil {
		return nil, badRequest("can't parse menu button")
	}
	var b *fakeButton
	if mb.Type == "web_app" && mb.WebApp != nil {
		b = &fakeButton{Text: mb.Text, WebApp: mb.WebApp.URL}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if id := p.int("chat_id"); id != 0 {
		f.chatLocked(id).Menu = b
	} else {
		f.menu = b
	}
	f.touchLocked()
	return true, nil
}

func (f *fakeTelegram) answerCallback(p params) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	user, ok := f.callbacks[p.str("callback_query_id")]
	if !ok {
		return nil, badRequest("query is too old and response timeout expired or query ID is invalid")
	}
	delete(f.callbacks, p.str("callback_query_id"))
	if text := p.str("text"); text != "" {
		f.seq++
		f.toasts = append(f.toasts, fakeToast{Seq: f.seq, User: user, Text: text, Alert: p.str("show_alert") == "true"})
		if len(f.toasts) > 50 {
			f.toasts = f.toasts[len(f.toasts)-50:]
		}
		f.touchLocked()
	}
	return true, nil
}

// file finds a file a request names by id, or keeps the one it uploads.
func (f *fakeTelegram) fileLocked(ref string, upload []byte, kind string) (string, error) {
	if upload != nil {
		f.fileSeq++
		ext := ".png"
		if kind == "animation" {
			ext = ".mp4"
		} else if len(upload) > 2 && upload[0] == 0xFF && upload[1] == 0xD8 {
			ext = ".jpg"
		}
		id := fmt.Sprintf("F%s%04d", kind[:2], f.fileSeq)
		dir := filepath.Join(f.e.StateDir, "tg-files")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		path := filepath.Join(dir, id+ext)
		if err := os.WriteFile(path, upload, 0o644); err != nil {
			return "", err
		}
		f.files[id] = fakeFile{Path: path, Kind: kind}
		return id, nil
	}
	ref = strings.TrimSuffix(ref, "_s")
	if _, ok := f.files[ref]; !ok {
		return "", badRequest("wrong file identifier/HTTP URL specified")
	}
	return ref, nil
}

func (f *fakeTelegram) send(p params, files map[string][]byte, media string) (any, error) {
	chatID := p.int("chat_id")
	if chatID == 0 {
		return nil, badRequest("chat_id is empty")
	}
	buttons, err := p.buttons()
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.chatLocked(chatID)
	m := &fakeMsg{FromBot: true, FromID: botID, From: botName, Date: f.e.Clock.Now().Unix(), Buttons: buttons,
		Thread: int(p.int("message_thread_id")), Media: media}
	switch media {
	case "":
		m.Body = p.str("text")
		if strings.TrimSpace(m.Body) == "" {
			return nil, badRequest("message text is empty")
		}
		if n := len([]rune(plainOf(m.Body))); n > 4096 {
			return nil, badRequest("message is too long")
		}
	default:
		m.Body = p.str("caption")
		if n := len([]rune(plainOf(m.Body))); n > 1024 {
			return nil, badRequest("message caption is too long")
		}
		id, err := f.fileLocked(p.str(media), files[media], media)
		if err != nil {
			return nil, err
		}
		m.File = id
	}
	if err := checkHTML(m.Body); err != nil {
		return nil, err
	}
	m.ID = c.next
	c.next++
	c.Msgs = append(c.Msgs, m)
	f.touchLocked()
	return f.wire(c, m), nil
}

func (f *fakeTelegram) edit(method string, p params, files map[string][]byte) (any, error) {
	if p.str("inline_message_id") != "" {
		return true, nil
	}
	buttons, err := p.buttons()
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.chats[p.int("chat_id")]
	if !ok {
		return nil, badRequest("chat not found")
	}
	_, m := c.find(p.int("message_id"))
	if m == nil {
		return nil, badRequest("message to edit not found")
	}
	if !m.FromBot {
		return nil, badRequest("message can't be edited")
	}
	next := *m
	next.Buttons = buttons
	switch method {
	case "editmessagetext":
		if m.Media != "" {
			return nil, badRequest("there is no text in the message to edit")
		}
		next.Body = p.str("text")
	case "editmessagecaption":
		if m.Media == "" {
			return nil, badRequest("there is no caption in the message to edit")
		}
		next.Body = p.str("caption")
	case "editmessagereplymarkup":
	case "editmessagemedia":
		var in struct {
			Type    string `json:"type"`
			Media   string `json:"media"`
			Caption string `json:"caption"`
		}
		if err := p.obj("media", &in); err != nil {
			return nil, badRequest("can't parse InputMedia JSON object")
		}
		var upload []byte
		if name, ok := strings.CutPrefix(in.Media, "attach://"); ok {
			if upload = files[name]; upload == nil {
				return nil, badRequest("wrong file identifier/HTTP URL specified")
			}
		}
		id, err := f.fileLocked(in.Media, upload, in.Type)
		if err != nil {
			return nil, err
		}
		next.Media, next.File, next.Body = in.Type, id, in.Caption
	}
	if err := checkHTML(next.Body); err != nil {
		return nil, err
	}
	if next.Body == m.Body && next.File == m.File && sameButtons(next.Buttons, m.Buttons) {
		return nil, badRequest("message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message")
	}
	next.Edits++
	*m = next
	f.touchLocked()
	return f.wire(c, m), nil
}

func sameButtons(a, b [][]fakeButton) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !slices.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func (f *fakeTelegram) pin(method string, p params) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.chats[p.int("chat_id")]
	if !ok {
		return nil, badRequest("chat not found")
	}
	id := p.int("message_id")
	switch method {
	case "pinchatmessage":
		if _, m := c.find(id); m == nil {
			return nil, badRequest("message to pin not found")
		}
		c.Pinned = append(slices.DeleteFunc(c.Pinned, func(x int64) bool { return x == id }), id)
	case "unpinchatmessage":
		if id == 0 && len(c.Pinned) > 0 {
			id = c.Pinned[len(c.Pinned)-1]
		}
		c.Pinned = slices.DeleteFunc(c.Pinned, func(x int64) bool { return x == id })
	default:
		c.Pinned = nil
	}
	f.touchLocked()
	return true, nil
}

func (f *fakeTelegram) delete(p params) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.chats[p.int("chat_id")]
	if !ok {
		return nil, badRequest("chat not found")
	}
	i, _ := c.find(p.int("message_id"))
	if i < 0 {
		return nil, badRequest("message to delete not found")
	}
	c.Msgs = slices.Delete(c.Msgs, i, i+1)
	id := p.int("message_id")
	c.Pinned = slices.DeleteFunc(c.Pinned, func(x int64) bool { return x == id })
	f.touchLocked()
	return true, nil
}

func (f *fakeTelegram) member(p params) (any, error) {
	chat, user := p.int("chat_id"), p.int("user_id")
	if user == botID {
		return map[string]any{"status": "administrator", "user": botUser(), "can_pin_messages": true,
			"can_delete_messages": true, "can_manage_chat": true}, nil
	}
	pl, ok := f.e.Player(user)
	if !ok {
		return nil, badRequest("user not found")
	}
	status := "left"
	switch {
	case chat == user:
		status = "member"
	case pl.In(chat) && pl.Owner:
		status = "creator"
	case pl.In(chat):
		status = "member"
	}
	return map[string]any{"status": status, "user": playerUser(pl)}, nil
}

func (f *fakeTelegram) savePrepared(p params) (any, error) {
	var res map[string]any
	if err := p.obj("result", &res); err != nil || res == nil {
		return nil, badRequest("can't parse inline query result")
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	f.mu.Lock()
	f.prepared[id] = fakePrepared{User: p.int("user_id"), Result: res}
	f.mu.Unlock()
	return map[string]any{"id": id, "expiration_date": time.Now().Add(24 * time.Hour).Unix()}, nil
}

// ---------- updates from the page ----------

func (f *fakeTelegram) pushLocked(kind string, body any) {
	raw, _ := json.Marshal(map[string]any{"update_id": f.nextUpdate, kind: body})
	f.updates = append(f.updates, fakeUpdate{ID: f.nextUpdate, Raw: raw})
	f.nextUpdate++
	close(f.wake)
	f.wake = make(chan struct{})
}

// Say posts p's message into chat and gives it to the bot.
func (f *fakeTelegram) Say(p Player, chat int64, text string, loc *geo.Point) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.chatLocked(chat)
	m := &fakeMsg{ID: c.next, FromID: p.ID, From: p.Name, Date: f.e.Clock.Now().Unix(), Body: escapeHTML(text), Location: loc}
	c.next++
	c.Msgs = append(c.Msgs, m)
	w := f.wire(c, m)
	w["text"] = text
	if loc != nil {
		delete(w, "text")
	}
	if strings.HasPrefix(text, "/") {
		cmd, _, _ := strings.Cut(text, " ")
		w["entities"] = []map[string]any{{"type": "bot_command", "offset": 0, "length": len([]rune(cmd))}}
	}
	f.pushLocked("message", w)
	f.touchLocked()
}

// Press gives the bot p's tap on a callback button.
func (f *fakeTelegram) Press(p Player, chat, msg int64, data string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.chats[chat]
	if !ok {
		return errors.New("no such chat")
	}
	_, m := c.find(msg)
	if m == nil {
		return errors.New("no such message")
	}
	f.seq++
	id := fmt.Sprintf("cb%d", f.seq)
	f.callbacks[id] = p.ID
	f.pushLocked("callback_query", map[string]any{
		"id": id, "from": playerUser(p), "message": f.wire(c, m),
		"chat_instance": strconv.FormatInt(p.ID*7+chat, 10), "data": data,
	})
	return nil
}

// Membership tells the bot it was added to a group or removed from it.
func (f *fakeTelegram) Membership(p Player, chat int64, in bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.chatLocked(chat)
	from, to := "left", "administrator"
	if !in {
		from, to = to, "left"
	}
	f.pushLocked("my_chat_member", map[string]any{
		"chat": c.wire(), "from": playerUser(p), "date": f.e.Clock.Now().Unix(),
		"old_chat_member": map[string]any{"status": from, "user": botUser()},
		"new_chat_member": map[string]any{"status": to, "user": botUser()},
	})
	note := "добавил(а) бота"
	if !in {
		note = "убрал(а) бота"
	}
	c.Msgs = append(c.Msgs, &fakeMsg{ID: c.next, FromID: p.ID, From: p.Name, Date: f.e.Clock.Now().Unix(), Body: "<i>" + note + "</i>"})
	c.next++
	f.touchLocked()
}

// SharePrepared posts a prepared message into chat as p, via the bot.
func (f *fakeTelegram) SharePrepared(p Player, chat int64, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.prepared[id]
	if !ok {
		return errors.New("prepared message expired")
	}
	if pr.User != p.ID {
		return errors.New("prepared message belongs to another person")
	}
	c := f.chatLocked(chat)
	m := &fakeMsg{ID: c.next, FromID: p.ID, From: p.Name, Date: f.e.Clock.Now().Unix(), Via: true}
	r := pr.Result
	str := func(k string) string { s, _ := r[k].(string); return s }
	switch str("type") {
	case "photo":
		m.Body, m.Media = str("caption"), "photo-url"
		m.File = str("photo_url")
	case "mpeg4_gif":
		m.Body, m.Media, m.File = str("caption"), "animation", str("mpeg4_file_id")
	default:
		if in, ok := r["input_message_content"].(map[string]any); ok {
			m.Body, _ = in["message_text"].(string)
		}
	}
	if mk, ok := r["reply_markup"]; ok {
		raw, _ := json.Marshal(map[string]any{"reply_markup": mk})
		var pp params
		_ = json.Unmarshal(raw, &pp)
		m.Buttons, _ = pp.buttons()
	}
	c.next++
	c.Msgs = append(c.Msgs, m)
	f.touchLocked()
	return nil
}

// ---------- the bot and the scheduler ----------

// standWeather stands in for Open-Meteo, so the places' coordinates stay on
// the machine: the offset from the longitude and a made-up temperature.
type standWeather struct{}

func (standWeather) Now(_ context.Context, p geo.Point) (time.Duration, *float64, error) {
	off := time.Duration(math.Round(p.Lon/15)) * time.Hour
	t := math.Round((29-math.Abs(p.Lat)*0.5)*10) / 10
	return off, &t, nil
}

// run starts the real bot and scheduler against the fake server and returns
// what stops them.
func (f *fakeTelegram) run(ctx context.Context, e *Env) (func(), error) {
	if !e.Bot {
		return func() {}, nil
	}
	base := "http://" + e.Addr + "/tgapi"
	appURL := "http://" + e.Addr + "/"
	log := e.Log.With("part", "bot")
	client := tg.New(BotToken,
		tg.WithAPIBase(base),
		tg.WithAllowedUpdates("message", "callback_query", "my_chat_member"),
		tg.WithLogger(log),
	)
	ctx, cancel := context.WithCancel(ctx)
	if _, err := client.Preflight(ctx, tg.Needs{Wait: 5 * time.Second}); err != nil {
		cancel()
		return nil, err
	}
	tgc := telegram.New(client)
	tgc.Upload = &telegram.Uploader{Base: base, Token: BotToken}
	avatars := &avatar.Service{Store: e.Store, TG: tgc, Log: log, Now: e.Clock.Now}
	opts := bot.Options{Username: botUsername, Version: "dev", AppURL: appURL, Zones: e.Zones, Now: e.Clock.Now}
	sched := &schedule.Scheduler{
		Store: e.Store, TG: tgc, Weather: standWeather{}, Payloads: e.Signer, Zones: e.Zones,
		Countries: e.Countries, ImagesDir: e.ImagesDir, Username: botUsername, AppURL: appURL,
		Avatars: avatars, Log: log, Now: e.Clock.Now, Tick: 3 * time.Second,
	}
	var done sync.WaitGroup
	if ffmpeg, err := exec.LookPath("ffmpeg"); err == nil {
		profiles := &profile.Service{Store: e.Store, Zones: e.Zones, FFmpeg: ffmpeg, Log: log, Now: e.Clock.Now,
			Dir: filepath.Join(e.StateDir, "profiles")}
		opts.Profiles = profiles
		done.Add(1)
		go func() { defer done.Done(); profiles.Run(ctx, 10*time.Minute) }()
		sched.Anim = &schedule.Animator{FFmpeg: ffmpeg}
		sched.World = &world.Service{Store: e.Store, Zones: e.Zones, FFmpeg: ffmpeg, Avatars: avatars, Log: log, Now: e.Clock.Now}
		opts.World = sched
	} else {
		log.Warn("ffmpeg not found: posts go out as photos and text")
	}
	b := bot.New(e.Store, tgc, log, opts)
	done.Add(2)
	go func() {
		defer done.Done()
		if err := b.Run(ctx); err != nil {
			log.Error("bot stopped", "error", err)
		}
	}()
	go func() {
		defer done.Done()
		if err := sched.Run(ctx); err != nil {
			log.Error("scheduler stopped", "error", err)
		}
	}()
	log.Info("bot and scheduler on the fake Telegram", "chats", "http://"+e.Addr+"/dev/tg")
	return func() {
		cancel()
		done.Wait()
	}, nil
}
