// SPDX-License-Identifier: Apache-2.0
package telegram

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/FreshLabDev/tg"
)

type request struct {
	method string
	body   map[string]any
	upload map[string]string // multipart: field -> value or file name
}

type fakeBotAPI struct {
	mu       sync.Mutex
	requests []request
	results  map[string]string
}

func (f *fakeBotAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	req := request{method: method}
	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/form-data" {
		req.upload = map[string]string{}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			if part.FileName() != "" {
				req.upload[part.FormName()] = "file:" + part.FileName()
			} else {
				req.upload[part.FormName()] = string(data)
			}
		}
	} else {
		_ = json.NewDecoder(r.Body).Decode(&req.body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	result, ok := f.results[method]
	if !ok {
		result = "true"
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"ok":true,"result":`+result+`}`)
}

func client(t *testing.T) (*Client, *fakeBotAPI) {
	t.Helper()
	fake := &fakeBotAPI{results: map[string]string{
		"sendPhoto":                 `{"message_id":7,"chat":{"id":-100,"type":"supergroup"},"date":1,"photo":[{"file_id":"F1","file_unique_id":"U1","width":1,"height":1}]}`,
		"editMessageReplyMarkup":    `{"message_id":7,"chat":{"id":-100,"type":"supergroup"},"date":1}`,
		"savePreparedInlineMessage": `{"id":"prep-1","expiration_date":1900000000}`,
		"sendMessage":               `{"message_id":9,"chat":{"id":42,"type":"private"},"date":1,"text":"maply"}`,
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return New(tg.New("123:abc", tg.WithAPIBase(srv.URL))), fake
}

func TestWebAppButtonByFileID(t *testing.T) {
	c, fake := client(t)
	markup := Markup{{{Text: "Guess", WebApp: "https://maply.ws04.cc/"}}}
	if _, err := c.SendPhoto(context.Background(), 42, Photo{FileID: "F1"}, "<b>maply #1</b>", markup); err != nil {
		t.Fatal(err)
	}
	req := fake.requests[0]
	if req.method != "sendPhoto" || req.body["photo"] != "F1" || req.body["parse_mode"] != "HTML" {
		t.Fatalf("request = %+v", req)
	}
	button := req.body["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)[0].(map[string]any)
	if button["web_app"].(map[string]any)["url"] != "https://maply.ws04.cc/" || button["url"] != nil {
		t.Fatalf("button = %v", button)
	}
}

func TestUploadThenButtons(t *testing.T) {
	c, fake := client(t)
	path := filepath.Join(t.TempDir(), "Zk3qT9xWm2LpV7aN.jpg")
	if err := os.WriteFile(path, []byte("jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	markup := Markup{{{Text: "Guess", URL: "https://t.me/maplyin_bot/play?startapp=x"}}}
	msg, err := c.SendPhoto(context.Background(), -100, Photo{Path: path}, "caption", markup)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Photo) == 0 || msg.Photo[0].FileID != "F1" {
		t.Fatalf("message = %+v", msg)
	}
	if len(fake.requests) != 2 || fake.requests[0].method != "sendPhoto" || fake.requests[1].method != "editMessageReplyMarkup" {
		t.Fatalf("requests = %+v", fake.requests)
	}
	// Telegram records the upload's name; the key stays on our disk.
	if got := fake.requests[0].upload["photo"]; got != "file:maply.jpg" {
		t.Fatalf("uploaded as %q", got)
	}
}

func TestMarkupNeedsExactlyOneAction(t *testing.T) {
	for _, m := range []Markup{
		{{{Text: "none"}}},
		{{{Text: "two", URL: "https://a", WebApp: "https://b"}}},
	} {
		if _, err := json.Marshal(m); err == nil {
			t.Fatalf("markup %+v marshalled", m)
		}
	}
}

func TestPinIsSilent(t *testing.T) {
	c, fake := client(t)
	ctx := context.Background()
	if err := c.Pin(ctx, -100, 7); err != nil {
		t.Fatal(err)
	}
	if err := c.Unpin(ctx, -100, 7); err != nil {
		t.Fatal(err)
	}
	if fake.requests[0].method != "pinChatMessage" || fake.requests[0].body["disable_notification"] != true {
		t.Fatalf("pin = %+v", fake.requests[0])
	}
	if fake.requests[1].method != "unpinChatMessage" || fake.requests[1].body["message_id"] != float64(7) {
		t.Fatalf("unpin = %+v", fake.requests[1])
	}
}

func TestMenuButtonAndPreparedMessage(t *testing.T) {
	c, fake := client(t)
	ctx := context.Background()
	if err := c.SetMenuButton(ctx, 0, "Play", "https://maply.ws04.cc/"); err != nil {
		t.Fatal(err)
	}
	if _, has := fake.requests[0].body["chat_id"]; has {
		t.Fatal("the default menu button named a chat")
	}
	got, err := c.SavePreparedPhoto(ctx, 42, PreparedPhoto{ID: "r1", PhotoURL: "https://maply.ws04.cc/card/1.png", ThumbURL: "https://maply.ws04.cc/card/1.png"})
	if err != nil || got.ID != "prep-1" {
		t.Fatalf("prepared = %+v, %v", got, err)
	}
	if res := fake.requests[1].body["result"].(map[string]any); res["type"] != "photo" || fake.requests[1].body["user_id"] != float64(42) {
		t.Fatalf("savePreparedInlineMessage = %+v", fake.requests[1].body)
	}
}

func TestTextPanelCarriesStyleAndWebApp(t *testing.T) {
	c, fake := client(t)
	ctx := context.Background()
	markup := Markup{{{Text: "Play", WebApp: "https://maply.ws04.cc/", Style: tg.StylePrimary}, {Text: "Help", CallbackData: "m:42:help"}}}
	msg, err := c.SendText(ctx, 42, "<b>maply</b>", markup)
	if err != nil || msg.MessageID != 9 {
		t.Fatalf("message = %+v, %v", msg, err)
	}
	if err := c.EditText(ctx, 42, 9, "<b>Help</b>", nil); err != nil {
		t.Fatal(err)
	}
	send := fake.requests[0]
	if send.method != "sendMessage" || send.body["parse_mode"] != "HTML" {
		t.Fatalf("send = %+v", send)
	}
	row := send.body["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)
	if play := row[0].(map[string]any); play["style"] != tg.StylePrimary || play["web_app"] == nil {
		t.Fatalf("play = %v", play)
	}
	if help := row[1].(map[string]any); help["style"] != nil || help["callback_data"] != "m:42:help" {
		t.Fatalf("help = %v", help)
	}
	if edit := fake.requests[1]; edit.method != "editMessageText" || edit.body["reply_markup"] != nil || edit.body["message_id"] != float64(9) {
		t.Fatalf("edit = %+v", edit)
	}
}
