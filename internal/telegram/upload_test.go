// SPDX-License-Identifier: Apache-2.0
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FreshLabDev/tg"
)

// The statistics picture takes a text panel's place: uploaded once, with its
// caption and buttons in the same request, and by file_id after that.
func TestEditAnimationUploadsThenNamesTheFile(t *testing.T) {
	var got struct {
		path, media, markup, name string
		data                      []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		got.media, got.markup = r.FormValue("media"), r.FormValue("reply_markup")
		f, h, err := r.FormFile("animation")
		if err != nil {
			t.Error(err)
		} else {
			got.name = h.Filename
			got.data, _ = io.ReadAll(f)
		}
		io.WriteString(w, `{"ok":true,"result":{"message_id":5,"chat":{"id":42,"type":"private"},"date":1,"caption":"s","animation":{"file_id":"P1","file_unique_id":"U"},"document":{"file_id":"P1","file_unique_id":"U"}}}`)
	}))
	defer srv.Close()
	c := New(tg.New("123:abc", tg.WithAPIBase(srv.URL)))
	c.Upload = &Uploader{Base: srv.URL, Token: "123:abc"}
	markup := Markup{{{Text: "Back", CallbackData: "m:42:home"}}}
	msg, err := c.EditAnimation(context.Background(), 42, 5, Animation{MP4: []byte("mp4"), Width: 1200, Height: 630, Duration: 24}, "<b>Stats</b>", markup)
	if err != nil || msg.Document == nil || msg.Document.FileID != "P1" {
		t.Fatalf("message %+v, %v", msg, err)
	}
	var media map[string]any
	if err := json.Unmarshal([]byte(got.media), &media); err != nil {
		t.Fatal(err)
	}
	if got.path != "/bot123:abc/editMessageMedia" || media["type"] != "animation" || media["media"] != "attach://animation" ||
		media["parse_mode"] != "HTML" || media["width"] != float64(1200) || string(got.data) != "mp4" || got.name != "maply.mp4" {
		t.Fatalf("request %+v, media %v", got, media)
	}
	if !strings.Contains(got.markup, `"callback_data":"m:42:home"`) {
		t.Fatalf("markup %s", got.markup)
	}

	fake := &fakeBotAPI{results: map[string]string{"editMessageMedia": `{"message_id":5,"chat":{"id":42,"type":"private"},"date":1,"document":{"file_id":"P1","file_unique_id":"U"}}`}}
	api := httptest.NewServer(fake)
	defer api.Close()
	c = New(tg.New("123:abc", tg.WithAPIBase(api.URL)))
	if _, err := c.EditAnimation(context.Background(), 42, 5, Animation{FileID: "P1"}, "<b>Stats</b>", markup); err != nil {
		t.Fatal(err)
	}
	if m := fake.requests[0].body["media"].(map[string]any); m["media"] != "P1" || m["type"] != "animation" {
		t.Fatalf("by file_id: %v", m)
	}
}

// A refusal is tg's error, so the callers' checks work on it, and no error
// carries the token.
func TestUploadErrorsKeepTheTokenOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`)
	}))
	c := New(tg.New("123:abc"))
	c.Upload = &Uploader{Base: srv.URL, Token: "123:abc"}
	_, err := c.EditAnimation(context.Background(), 42, 5, Animation{MP4: []byte("x")}, "", nil)
	var apiErr *tg.APIError
	if !errors.As(err, &apiErr) || !tg.IsMessageNotModified(err) {
		t.Fatalf("error %v", err)
	}
	srv.Close()
	_, err = c.EditAnimation(context.Background(), 42, 5, Animation{MP4: []byte("x")}, "", nil)
	if err == nil || strings.Contains(err.Error(), "123:abc") {
		t.Fatalf("error %v", err)
	}
}
