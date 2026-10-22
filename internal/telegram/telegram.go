// SPDX-License-Identifier: Apache-2.0

// Package telegram adds to tg what maply needs and tg does not type yet: the
// web_app button, pinning, the chat menu button and prepared inline messages.
// Everything goes through tg's Client.Call, so it shares tg's transport,
// retries and token redaction. When tg learns a method, it moves out of here.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/FreshLabDev/tg"
)

type Client struct {
	tg *tg.Client
}

func New(client *tg.Client) *Client {
	return &Client{tg: client}
}

// Button is one inline button: a callback, a link, or a Mini App to open.
// Exactly one of CallbackData, URL and WebApp is set.
type Button struct {
	Text         string
	CallbackData string
	URL          string
	WebApp       string
}

type Markup [][]Button

func (m Markup) MarshalJSON() ([]byte, error) {
	type webApp struct {
		URL string `json:"url"`
	}
	type button struct {
		Text         string  `json:"text"`
		CallbackData string  `json:"callback_data,omitempty"`
		URL          string  `json:"url,omitempty"`
		WebApp       *webApp `json:"web_app,omitempty"`
	}
	rows := make([][]button, len(m))
	for i, row := range m {
		rows[i] = make([]button, len(row))
		for j, b := range row {
			set := 0
			for _, v := range []string{b.CallbackData, b.URL, b.WebApp} {
				if v != "" {
					set++
				}
			}
			if set != 1 {
				return nil, fmt.Errorf("button %q needs exactly one action, has %d", b.Text, set)
			}
			rows[i][j] = button{Text: b.Text, CallbackData: b.CallbackData, URL: b.URL}
			if b.WebApp != "" {
				rows[i][j].WebApp = &webApp{URL: b.WebApp}
			}
		}
	}
	return json.Marshal(map[string]any{"inline_keyboard": rows})
}

type envelope[T any] struct {
	OK     bool   `json:"ok"`
	Result T      `json:"result"`
	Desc   string `json:"description,omitempty"`
}

func call[T any](ctx context.Context, c *Client, method string, req map[string]any) (T, error) {
	var out envelope[T]
	if err := c.tg.Call(ctx, method, req, &out); err != nil {
		var zero T
		return zero, err
	}
	if !out.OK {
		var zero T
		return zero, fmt.Errorf("telegram %s returned ok=false: %s", method, out.Desc)
	}
	return out.Result, nil
}

// Photo is either a file Telegram already has or a local file to upload.
type Photo struct {
	FileID string
	Path   string
}

// SendPhoto posts a photo with an HTML caption and buttons. A file_id goes in
// one call. An upload goes through tg, which cannot carry a web_app button
// yet, so the buttons follow in an edit; the returned message holds the
// file_id to reuse for every other chat.
func (c *Client) SendPhoto(ctx context.Context, chatID int64, photo Photo, caption string, markup Markup) (tg.Message, error) {
	if photo.FileID != "" {
		req := map[string]any{"chat_id": chatID, "photo": photo.FileID, "caption": caption, "parse_mode": "HTML"}
		if markup != nil {
			req["reply_markup"] = markup
		}
		return call[tg.Message](ctx, c, "sendPhoto", req)
	}
	data, err := os.ReadFile(photo.Path)
	if err != nil {
		return tg.Message{}, fmt.Errorf("read photo: %w", err)
	}
	// The upload's name is what Telegram records; the key is random already,
	// but a fixed name keeps even that out of Telegram.
	msg, err := c.tg.SendPhoto(ctx, chatID, tg.InputFileUpload{Filename: "maply" + filepath.Ext(photo.Path), Data: bytes.NewReader(data)},
		tg.CaptionOptions{Caption: caption})
	if err != nil || markup == nil {
		return msg, err
	}
	if err := c.EditMarkup(ctx, chatID, msg.MessageID, markup); err != nil {
		return msg, fmt.Errorf("photo sent without buttons: %w", err)
	}
	return msg, nil
}

func (c *Client) EditMarkup(ctx context.Context, chatID, messageID int64, markup Markup) error {
	_, err := call[json.RawMessage](ctx, c, "editMessageReplyMarkup", map[string]any{
		"chat_id": chatID, "message_id": messageID, "reply_markup": markup,
	})
	return err
}

// EditCaption replaces a photo's caption and keeps its buttons.
func (c *Client) EditCaption(ctx context.Context, chatID, messageID int64, caption string, markup Markup) error {
	req := map[string]any{"chat_id": chatID, "message_id": messageID, "caption": caption, "parse_mode": "HTML"}
	if markup != nil {
		req["reply_markup"] = markup
	}
	_, err := call[json.RawMessage](ctx, c, "editMessageCaption", req)
	return err
}

// Pin pins without a notification: the morning post is already a message,
// and a pin that buzzes would be a fourth one.
func (c *Client) Pin(ctx context.Context, chatID, messageID int64) error {
	_, err := call[bool](ctx, c, "pinChatMessage", map[string]any{
		"chat_id": chatID, "message_id": messageID, "disable_notification": true,
	})
	return err
}

func (c *Client) Unpin(ctx context.Context, chatID, messageID int64) error {
	_, err := call[bool](ctx, c, "unpinChatMessage", map[string]any{
		"chat_id": chatID, "message_id": messageID,
	})
	return err
}

// SetMenuButton makes the button next to the input field open the Mini App.
// A zero chatID sets the default for every DM.
func (c *Client) SetMenuButton(ctx context.Context, chatID int64, text, url string) error {
	req := map[string]any{"menu_button": map[string]any{
		"type": "web_app", "text": text, "web_app": map[string]string{"url": url},
	}}
	if chatID != 0 {
		req["chat_id"] = chatID
	}
	_, err := call[bool](ctx, c, "setChatMenuButton", req)
	return err
}

// PreparedPhoto is a photo result for shareMessage: the Mini App hands its id
// to Telegram, and the person picks where to send it.
type PreparedPhoto struct {
	ID       string
	PhotoURL string
	ThumbURL string
	Caption  string
	Markup   Markup
}

type PreparedMessage struct {
	ID             string `json:"id"`
	ExpirationDate int64  `json:"expiration_date"`
}

func (c *Client) SavePreparedPhoto(ctx context.Context, userID int64, p PreparedPhoto) (PreparedMessage, error) {
	result := map[string]any{
		"type": "photo", "id": p.ID, "photo_url": p.PhotoURL, "thumbnail_url": p.ThumbURL,
		"caption": p.Caption, "parse_mode": "HTML",
	}
	if p.Markup != nil {
		result["reply_markup"] = p.Markup
	}
	return call[PreparedMessage](ctx, c, "savePreparedInlineMessage", map[string]any{
		"user_id": userID, "result": result,
		"allow_user_chats": true, "allow_group_chats": true,
	})
}
