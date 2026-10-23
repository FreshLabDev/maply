// SPDX-License-Identifier: Apache-2.0
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FreshLabDev/tg"
)

// Uploader posts a multipart request with one file. tg uploads only through
// its own typed methods, and none of them puts an animation in place of a
// message and hands the message back, so this does it beside tg. The token
// is in the request URL only: an error never quotes the URL.
type Uploader struct {
	Base  string // https://api.telegram.org
	Token string
	HTTP  *http.Client
}

func (u *Uploader) post(ctx context.Context, method string, fields map[string]string, field, name string, data []byte) (tg.Message, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return tg.Message{}, err
		}
	}
	part, err := w.CreateFormFile(field, name)
	if err != nil {
		return tg.Message{}, err
	}
	if _, err := part.Write(data); err != nil {
		return tg.Message{}, err
	}
	if err := w.Close(); err != nil {
		return tg.Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(u.Base, "/")+"/bot"+u.Token+"/"+method, &body)
	if err != nil {
		return tg.Message{}, fmt.Errorf("telegram %s: bad request", method)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	client := u.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return tg.Message{}, fmt.Errorf("telegram %s: %w", method, err)
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool       `json:"ok"`
		Result      tg.Message `json:"result"`
		ErrorCode   int        `json:"error_code"`
		Description string     `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return tg.Message{}, fmt.Errorf("telegram %s: %d with an unreadable body", method, resp.StatusCode)
	}
	if !out.OK {
		return tg.Message{}, &tg.APIError{Method: method, StatusCode: resp.StatusCode, ErrorCode: out.ErrorCode, Description: out.Description}
	}
	return out.Result, nil
}

// EditAnimation puts an animation in place of a message the bot sent, a text
// one included, with an HTML caption and buttons, and returns the message:
// its Document holds the file_id to send the same animation again.
func (c *Client) EditAnimation(ctx context.Context, chatID, messageID int64, a Animation, caption string, markup Markup) (tg.Message, error) {
	media := map[string]any{"type": "animation", "caption": caption, "parse_mode": "HTML"}
	if a.Width > 0 {
		media["width"], media["height"], media["duration"] = a.Width, a.Height, a.Duration
	}
	if a.FileID != "" {
		media["media"] = a.FileID
		req := map[string]any{"chat_id": chatID, "message_id": messageID, "media": media}
		if markup != nil {
			req["reply_markup"] = markup
		}
		return call[tg.Message](ctx, c, "editMessageMedia", req)
	}
	if c.Upload == nil {
		return tg.Message{}, errors.New("telegram editMessageMedia: no uploader")
	}
	media["media"] = "attach://animation"
	m, err := json.Marshal(media)
	if err != nil {
		return tg.Message{}, err
	}
	fields := map[string]string{"chat_id": fmt.Sprint(chatID), "message_id": fmt.Sprint(messageID), "media": string(m)}
	if markup != nil {
		k, err := json.Marshal(markup)
		if err != nil {
			return tg.Message{}, err
		}
		fields["reply_markup"] = string(k)
	}
	return c.Upload.post(ctx, "editMessageMedia", fields, "animation", "maply.mp4", a.MP4)
}
