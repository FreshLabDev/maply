// SPDX-License-Identifier: Apache-2.0
package auth

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"

	"github.com/FreshLabDev/maply/internal/round"
)

// A group post links to t.me/<bot>/play?startapp=<payload>. Telegram hands the
// payload back as start_param, but anyone can edit a link, so the payload is
// signed: without the signature a player could open another chat's round.
//
// Layout, then base64url without padding (36 characters):
//
//	version  1 byte   payloadVersion
//	day      2 bytes  days since payloadEpoch, big-endian
//	chat     8 bytes  chat_id, big-endian two's complement
//	mac     16 bytes  HMAC-SHA256 of the above, truncated
//
// Posts already in chats carry this form. A new form gets a new version byte,
// and this one keeps verifying. Version 2 has the same layout and signs a
// result card's link: the startapp payload in a group post is visible to
// everyone who can read the post, so it must not open a card page.
const (
	payloadVersion = 1
	cardVersion    = 2
	payloadBody    = 1 + 2 + 8
	payloadMAC     = 16
)

var payloadEpoch = round.NewDate(2026, time.January, 1)

var ErrBadPayload = errors.New("startapp payload is invalid")

type Payload struct {
	Date   round.Date
	ChatID int64
}

// Signer signs and checks payloads. Its key is derived from the bot token
// under its own label, so it is never the initData key.
type Signer struct {
	key []byte
}

func NewSigner(botToken string) *Signer {
	return &Signer{key: mac([]byte("maply-startapp-v1"), []byte(botToken))}
}

// Sign makes the startapp payload of a group post.
func (s *Signer) Sign(p Payload) (string, error) { return s.sign(payloadVersion, p) }

// SignCard makes the token of a result card's link.
func (s *Signer) SignCard(p Payload) (string, error) { return s.sign(cardVersion, p) }

func (s *Signer) sign(version byte, p Payload) (string, error) {
	days := p.Date.DaysSince(payloadEpoch)
	if days < 0 || days > 0xFFFF {
		return "", ErrBadPayload
	}
	buf := make([]byte, payloadBody, payloadBody+payloadMAC)
	buf[0] = version
	binary.BigEndian.PutUint16(buf[1:3], uint16(days))
	binary.BigEndian.PutUint64(buf[3:11], uint64(p.ChatID))
	buf = append(buf, mac(s.key, buf)[:payloadMAC]...)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Signer) Verify(raw string) (Payload, error) { return s.verify(payloadVersion, raw) }

func (s *Signer) VerifyCard(raw string) (Payload, error) { return s.verify(cardVersion, raw) }

func (s *Signer) verify(version byte, raw string) (Payload, error) {
	buf, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(buf) != payloadBody+payloadMAC || buf[0] != version {
		return Payload{}, ErrBadPayload
	}
	if !hmac.Equal(buf[payloadBody:], mac(s.key, buf[:payloadBody])[:payloadMAC]) {
		return Payload{}, ErrBadPayload
	}
	return Payload{
		Date:   payloadEpoch.AddDays(int(binary.BigEndian.Uint16(buf[1:3]))),
		ChatID: int64(binary.BigEndian.Uint64(buf[3:11])),
	}, nil
}
