// SPDX-License-Identifier: Apache-2.0

// Package auth decides who is asking: it verifies the Mini App's initData,
// signs and checks the startapp payload of a group post, and confirms that a
// person is in the group the payload names.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MaxInitDataAge is how old auth_date may be. Telegram signs initData when the
// Mini App opens, so a day covers a tab left open since the morning post.
const MaxInitDataAge = 24 * time.Hour

// clockSkew tolerates an auth_date slightly ahead of this server's clock.
const clockSkew = time.Minute

var (
	ErrBadSignature = errors.New("initData signature does not match")
	ErrStale        = errors.New("initData is too old")
	ErrMalformed    = errors.New("initData is malformed")
)

type User struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
	IsBot        bool   `json:"is_bot,omitempty"`
}

// InitData is what a verified Mini App launch tells the server.
type InitData struct {
	User       User
	AuthDate   time.Time
	StartParam string
	// ChatType is "private", "group", "supergroup", "channel" or "sender" when
	// the Mini App was opened from a chat; Telegram does not say which chat.
	ChatType     string
	ChatInstance string
}

// Verifier checks initData against the bot token.
type Verifier struct {
	secret []byte
	now    func() time.Time
}

func NewVerifier(botToken string) *Verifier {
	return &Verifier{secret: mac([]byte("WebAppData"), []byte(botToken)), now: time.Now}
}

// Verify checks the hash of a raw initData query string and returns its
// contents. The data-check-string is every field but hash, sorted by key, as
// key=value lines; the key is HMAC-SHA256("WebAppData", token).
func (v *Verifier) Verify(raw string) (InitData, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return InitData{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	keys := make([]string, 0, len(values))
	for k, vs := range values {
		// A repeated field would let two parsers disagree on what was signed.
		if len(vs) != 1 {
			return InitData{}, fmt.Errorf("%w: field %q appears %d times", ErrMalformed, k, len(vs))
		}
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	got, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(got) != sha256.Size {
		return InitData{}, ErrBadSignature
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + values.Get(k)
	}
	if !hmac.Equal(got, mac(v.secret, []byte(strings.Join(lines, "\n")))) {
		return InitData{}, ErrBadSignature
	}

	// Only signed data is read past this point.
	unix, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return InitData{}, fmt.Errorf("%w: auth_date", ErrMalformed)
	}
	authDate := time.Unix(unix, 0)
	now := v.now()
	if now.Sub(authDate) > MaxInitDataAge || authDate.Sub(now) > clockSkew {
		return InitData{}, ErrStale
	}
	var user User
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID == 0 {
		return InitData{}, fmt.Errorf("%w: user", ErrMalformed)
	}
	return InitData{
		User:         user,
		AuthDate:     authDate,
		StartParam:   values.Get("start_param"),
		ChatType:     values.Get("chat_type"),
		ChatInstance: values.Get("chat_instance"),
	}, nil
}

func mac(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}
