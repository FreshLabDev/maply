// SPDX-License-Identifier: Apache-2.0
package auth

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/FreshLabDev/maply/internal/round"
)

// ChatMemberGetter is the Telegram client. getChatMember goes through its raw
// call because tg v0.1.0 drops is_member, which tells a muted member from a
// muted person who has left.
type ChatMemberGetter interface {
	CallGet(ctx context.Context, method string, values url.Values, out any) error
}

// chatMember is the part of getChatMember's answer maply reads.
type chatMember struct {
	Status string `json:"status"`
	// IsMember comes only with "restricted": false means the person has left.
	IsMember bool `json:"is_member"`
}

// Members answers whether a person is in a group, asking Telegram once per
// person, group and date. A valid payload only proves which chat a post came
// from; it is forwardable, so membership is what keeps a stranger out.
type Members struct {
	tg ChatMemberGetter
	// Now is the clock a refusal is timed by; nil is time.Now.
	Now func() time.Time
	mu  sync.Mutex
	// cache holds a few dates at once: around midnight UTC one group plays
	// the new date while another is still on the old one, and a cache of one
	// date would be emptied by every request in turn.
	cache map[round.Date]map[[2]int64]membership
}

// membership is one answer from Telegram: in the group, or not as of at.
type membership struct {
	ok bool
	at time.Time
}

const (
	// memberDates is how many dates the cache keeps.
	memberDates = 3
	// refusalTTL is how long a "not in the group" stands: someone who joins
	// in the afternoon plays that afternoon, while a stranger polling the
	// link still costs Telegram one question a minute.
	refusalTTL = time.Minute
)

func NewMembers(client ChatMemberGetter) *Members {
	return &Members{tg: client, cache: map[round.Date]map[[2]int64]membership{}}
}

func (m *Members) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// IsMember is cached for the day: someone who leaves a group in the afternoon
// may still answer that day's round, which is the same answer they could have
// given in the morning. A refusal is cached for a minute only, and errors are
// not cached.
func (m *Members) IsMember(ctx context.Context, d round.Date, chatID, userID int64) (bool, error) {
	key := [2]int64{chatID, userID}
	m.mu.Lock()
	day := m.cache[d]
	if day == nil {
		// A new date drops the oldest, which keeps the cache from growing.
		day = map[[2]int64]membership{}
		m.cache[d] = day
		for len(m.cache) > memberDates {
			oldest := d
			for k := range m.cache {
				if k.Before(oldest) {
					oldest = k
				}
			}
			delete(m.cache, oldest)
		}
	}
	if got, hit := day[key]; hit && (got.ok || m.now().Sub(got.at) < refusalTTL) {
		m.mu.Unlock()
		return got.ok, nil
	}
	m.mu.Unlock()

	member, err := m.get(ctx, chatID, userID)
	if err != nil {
		return false, err
	}
	ok := isMember(member)
	m.mu.Lock()
	if day := m.cache[d]; day != nil {
		day[key] = membership{ok: ok, at: m.now()}
	}
	m.mu.Unlock()
	return ok, nil
}

func (m *Members) get(ctx context.Context, chatID, userID int64) (chatMember, error) {
	values := url.Values{}
	values.Set("chat_id", strconv.FormatInt(chatID, 10))
	values.Set("user_id", strconv.FormatInt(userID, 10))
	var resp struct {
		OK     bool       `json:"ok"`
		Result chatMember `json:"result"`
	}
	if err := m.tg.CallGet(ctx, "getChatMember", values, &resp); err != nil {
		return chatMember{}, err
	}
	if !resp.OK {
		return chatMember{}, errors.New("telegram getChatMember returned ok=false")
	}
	return resp.Result, nil
}

// isMember counts a restricted person in while they are in the group: they
// may be muted, but they see its posts. Restricted and gone is not in.
func isMember(member chatMember) bool {
	switch member.Status {
	case "creator", "administrator", "member":
		return true
	case "restricted":
		return member.IsMember
	default:
		return false
	}
}
