// SPDX-License-Identifier: Apache-2.0
package auth

import (
	"context"
	"sync"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/round"
)

type ChatMemberGetter interface {
	GetChatMember(ctx context.Context, chatID, userID int64) (tg.ChatMember, error)
}

// Members answers whether a person is in a group, asking Telegram once per
// person, group and date. A valid payload only proves which chat a post came
// from; it is forwardable, so membership is what keeps a stranger out.
type Members struct {
	tg    ChatMemberGetter
	mu    sync.Mutex
	date  round.Date
	cache map[[2]int64]bool
}

func NewMembers(client ChatMemberGetter) *Members {
	return &Members{tg: client, cache: map[[2]int64]bool{}}
}

// IsMember is cached for the day: someone who leaves a group in the afternoon
// may still answer that day's round, which is the same answer they could have
// given in the morning. Errors are not cached.
func (m *Members) IsMember(ctx context.Context, d round.Date, chatID, userID int64) (bool, error) {
	key := [2]int64{chatID, userID}
	m.mu.Lock()
	if m.date != d {
		// A new day empties the cache, which also keeps it from growing.
		m.date, m.cache = d, map[[2]int64]bool{}
	}
	if ok, hit := m.cache[key]; hit {
		m.mu.Unlock()
		return ok, nil
	}
	m.mu.Unlock()

	member, err := m.tg.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return false, err
	}
	ok := isMember(member.Status)
	m.mu.Lock()
	if m.date == d {
		m.cache[key] = ok
	}
	m.mu.Unlock()
	return ok, nil
}

// isMember counts a restricted person in: they may be muted, but they are
// still in the group and see its posts.
func isMember(status string) bool {
	switch status {
	case "creator", "administrator", "member", "restricted":
		return true
	default:
		return false
	}
}
