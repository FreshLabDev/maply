// SPDX-License-Identifier: Apache-2.0

// Package telegram adds to tg what maply needs and tg does not type yet: the
// web_app button, pinning, the chat menu button and prepared inline messages.
// Everything but uploads goes through tg's Client.Call, so it shares tg's
// transport, retries and token redaction; uploads go through Uploader, which
// carries the buttons in the same request. When tg learns a method, it moves
// out of here.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/FreshLabDev/tg"
)

// Client is tg's client with maply's additions on top: every method tg has is
// reachable through the embedded value, and a method here with the same name
// wins.
type Client struct {
	*tg.Client
	// Upload carries the uploads tg has no method for; nil leaves them out.
	Upload *Uploader
}

func New(client *tg.Client) *Client {
	return &Client{Client: client}
}

// Button is one inline button: a callback, a link, or a Mini App to open.
// Exactly one of CallbackData, URL and WebApp is set. Style is tg's: empty,
// tg.StylePrimary, tg.StyleSuccess or tg.StyleDanger.
type Button struct {
	Text         string
	CallbackData string
	URL          string
	WebApp       string
	Style        string
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
		Style        string  `json:"style,omitempty"`
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
			rows[i][j] = button{Text: b.Text, CallbackData: b.CallbackData, URL: b.URL, Style: b.Style}
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
	if err := c.Call(ctx, method, req, &out); err != nil {
		var zero T
		return zero, err
	}
	if !out.OK {
		var zero T
		return zero, fmt.Errorf("telegram %s returned ok=false: %s", method, out.Desc)
	}
	return out.Result, nil
}

// Photo is a file Telegram already has, a local file to upload, or PNG bytes
// to upload (a drawn card).
type Photo struct {
	FileID string
	Path   string
	PNG    []byte
}

// SendPhoto posts a photo with an HTML caption and buttons, always in one
// request: a message sent first and given its buttons after would, when the
// second call failed, be sent again by the job's retry. The returned message
// of an upload holds the file_id to reuse for every other chat.
func (c *Client) SendPhoto(ctx context.Context, chatID int64, photo Photo, caption string, markup Markup) (tg.Message, error) {
	if photo.FileID != "" {
		req := map[string]any{"chat_id": chatID, "photo": photo.FileID, "caption": caption, "parse_mode": "HTML"}
		if markup != nil {
			req["reply_markup"] = markup
		}
		return call[tg.Message](ctx, c, "sendPhoto", req)
	}
	data, ext := photo.PNG, ".png"
	if data == nil {
		var err error
		if data, err = os.ReadFile(photo.Path); err != nil {
			return tg.Message{}, fmt.Errorf("read photo: %w", err)
		}
		ext = filepath.Ext(photo.Path)
	}
	fields := map[string]string{"chat_id": fmt.Sprint(chatID), "caption": caption, "parse_mode": "HTML"}
	if err := withMarkup(fields, markup); err != nil {
		return tg.Message{}, err
	}
	// The upload's name is what Telegram records; the key is random already,
	// but a fixed name keeps even that out of Telegram.
	return c.upload(ctx, "sendPhoto", fields, "photo", "maply"+ext, data)
}

// Animation is a silent MP4 that Telegram plays as a GIF: a file it already
// has, or bytes to upload.
type Animation struct {
	FileID                  string
	MP4                     []byte
	Width, Height, Duration int
}

// SendAnimation posts an animation with an HTML caption and buttons in one
// request, as SendPhoto does. The returned message's Document holds the
// file_id to send to every other chat.
func (c *Client) SendAnimation(ctx context.Context, chatID int64, a Animation, caption string, markup Markup) (tg.Message, error) {
	if a.FileID != "" {
		req := map[string]any{"chat_id": chatID, "animation": a.FileID, "caption": caption, "parse_mode": "HTML"}
		if a.Width > 0 {
			req["width"], req["height"], req["duration"] = a.Width, a.Height, a.Duration
		}
		if markup != nil {
			req["reply_markup"] = markup
		}
		return call[tg.Message](ctx, c, "sendAnimation", req)
	}
	fields := map[string]string{"chat_id": fmt.Sprint(chatID), "caption": caption, "parse_mode": "HTML"}
	if a.Width > 0 {
		fields["width"], fields["height"], fields["duration"] = fmt.Sprint(a.Width), fmt.Sprint(a.Height), fmt.Sprint(a.Duration)
	}
	if err := withMarkup(fields, markup); err != nil {
		return tg.Message{}, err
	}
	return c.upload(ctx, "sendAnimation", fields, "animation", "maply.mp4", a.MP4)
}

// SendText posts an HTML message without a link preview. It exists beside
// tg's SendMessage because tg's keyboard has no web_app button.
func (c *Client) SendText(ctx context.Context, chatID int64, text string, markup Markup) (tg.Message, error) {
	req := map[string]any{
		"chat_id": chatID, "text": text, "parse_mode": "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true},
	}
	if markup != nil {
		req["reply_markup"] = markup
	}
	return call[tg.Message](ctx, c, "sendMessage", req)
}

// EditText replaces an HTML message and its buttons.
func (c *Client) EditText(ctx context.Context, chatID, messageID int64, text string, markup Markup) error {
	req := map[string]any{
		"chat_id": chatID, "message_id": messageID, "text": text, "parse_mode": "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true},
	}
	if markup != nil {
		req["reply_markup"] = markup
	}
	_, err := call[json.RawMessage](ctx, c, "editMessageText", req)
	return err
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

// SetDescriptions writes the bot's profile texts for one language, empty
// being the default for every language without its own. Each is read first
// and written only when it differs: Telegram limits how often they change,
// and a restart should not spend that.
func (c *Client) SetDescriptions(ctx context.Context, lang, description, short string) error {
	for _, d := range []struct{ get, set, field, text string }{
		{"getMyDescription", "setMyDescription", "description", description},
		{"getMyShortDescription", "setMyShortDescription", "short_description", short},
	} {
		q := url.Values{}
		if lang != "" {
			q.Set("language_code", lang)
		}
		var cur envelope[map[string]string]
		if err := c.CallGet(ctx, d.get, q, &cur); err == nil && cur.OK && cur.Result[d.field] == d.text {
			continue
		}
		req := map[string]any{d.field: d.text}
		if lang != "" {
			req["language_code"] = lang
		}
		if _, err := call[bool](ctx, c, d.set, req); err != nil {
			return err
		}
	}
	return nil
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

// PreparedAnimation is an animation Telegram already has, by its file_id,
// for shareMessage.
type PreparedAnimation struct {
	ID      string
	FileID  string
	Caption string
	Markup  Markup
}

func (c *Client) SavePreparedAnimation(ctx context.Context, userID int64, a PreparedAnimation) (PreparedMessage, error) {
	result := map[string]any{
		"type": "mpeg4_gif", "id": a.ID, "mpeg4_file_id": a.FileID,
		"caption": a.Caption, "parse_mode": "HTML",
	}
	if a.Markup != nil {
		result["reply_markup"] = a.Markup
	}
	return call[PreparedMessage](ctx, c, "savePreparedInlineMessage", map[string]any{
		"user_id": userID, "result": result,
		"allow_user_chats": true, "allow_group_chats": true,
	})
}

// PreparedText is a text message for shareMessage: a title for the picker
// and the message itself.
type PreparedText struct {
	ID          string
	Title       string
	Description string
	Text        string
	Markup      Markup
}

func (c *Client) SavePreparedText(ctx context.Context, userID int64, t PreparedText) (PreparedMessage, error) {
	result := map[string]any{
		"type": "article", "id": t.ID, "title": t.Title, "description": t.Description,
		"input_message_content": map[string]any{
			"message_text": t.Text, "parse_mode": "HTML",
			"link_preview_options": map[string]any{"is_disabled": true},
		},
	}
	if t.Markup != nil {
		result["reply_markup"] = t.Markup
	}
	return call[PreparedMessage](ctx, c, "savePreparedInlineMessage", map[string]any{
		"user_id": userID, "result": result,
		"allow_user_chats": true, "allow_group_chats": true,
	})
}
