// SPDX-License-Identifier: Apache-2.0
package profile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
)

// Handles name a Telegram player in the app without their id: a name in a
// table carries the player's handle, and a tap on it asks for the mini
// profile by the handle. A handle is the id, a scope and a check of both,
// encrypted as one AES block under a key only the server has, so it says
// nothing about the id and cannot be made up for another one.
//
// A handle goes out only next to a name the viewer may already see: a row
// without a name has none, so it cannot tie unnamed rows to a person before
// the reveal. The scope is the table it stands in, a day and a view, so the
// same person's handles in two tables differ: they cannot be matched to
// gather one player's days, which the mini profile does not show either.
type Handles struct {
	block cipher.Block
	check []byte
}

// NewHandles keys the handles. key is at least 32 bytes; the same key gives
// the same handles across restarts, so a page left open keeps working.
func NewHandles(key []byte) *Handles {
	if len(key) < 32 {
		panic("profile: handle key shorter than 32 bytes")
	}
	// The cipher and the check each have their own key from the one given.
	sub := func(label string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(label))
		return m.Sum(nil)
	}
	block, err := aes.NewCipher(sub("cipher"))
	if err != nil {
		panic(err)
	}
	return &Handles{block: block, check: sub("check")}
}

// RandomHandles is for tests and a server without a key.
func RandomHandles() *Handles {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return NewHandles(key)
}

func (h *Handles) mac(plain []byte) []byte {
	m := hmac.New(sha256.New, h.check)
	m.Write(plain[:10])
	return m.Sum(nil)[:6]
}

// Of is userID's handle in the table scope names.
func (h *Handles) Of(userID int64, scope string) string {
	var plain, out [16]byte
	binary.BigEndian.PutUint64(plain[:8], uint64(userID))
	sum := sha256.Sum256([]byte(scope))
	copy(plain[8:10], sum[:2])
	copy(plain[10:], h.mac(plain[:]))
	h.block.Encrypt(out[:], plain[:])
	return base64.RawURLEncoding.EncodeToString(out[:])
}

// User is the player a handle names, in whichever table; false for anything
// not made by Of.
func (h *Handles) User(handle string) (int64, bool) {
	if len(handle) != 22 {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(handle)
	if err != nil || len(raw) != 16 {
		return 0, false
	}
	var plain [16]byte
	h.block.Decrypt(plain[:], raw)
	id := binary.BigEndian.Uint64(plain[:8])
	if !hmac.Equal(plain[10:], h.mac(plain[:])) || int64(id) <= 0 {
		return 0, false
	}
	return int64(id), true
}

// NewSlug is a fresh random name for a public page.
func NewSlug() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
