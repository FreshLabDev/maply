// SPDX-License-Identifier: Apache-2.0

package profile

import (
	"bytes"
	"context"
	"image/png"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

// stillsKept bounds the stills kept in memory: one is a few hundred
// kilobytes, and a link shared in a chat is fetched by every preview at once.
const stillsKept = 32

type stillKey struct {
	user int64
	lang string
	upTo round.Date
}

// Still is the person's picture as one PNG frame, for the preview of their
// public page. It goes to anyone with the link, so it counts only the days
// over in every zone, and its globe has no pins: a stranger's view of a
// profile has none. ErrNothing when no such day is scored.
func (s *Service) Still(ctx context.Context, userID int64, lang string) ([]byte, error) {
	upTo := LatestClosed(s.Zones, s.now())
	k := stillKey{userID, lang, upTo}
	s.stillMu.Lock()
	b, ok := s.stills[k]
	s.stillMu.Unlock()
	if ok {
		return b, nil
	}
	data, err := s.Store.ProfileOf(ctx, userID, upTo, recent+1)
	if err != nil {
		return nil, err
	}
	p := publicScene(data, lang, upTo)
	if p == nil {
		return nil, ErrNothing
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, anim.Still(p, 0)); err != nil {
		return nil, err
	}
	s.stillMu.Lock()
	if s.stills == nil || len(s.stills) >= stillsKept {
		s.stills = map[stillKey][]byte{}
	}
	s.stills[k] = buf.Bytes()
	s.stillMu.Unlock()
	return buf.Bytes(), nil
}

// Kept is the person's picture if Still has drawn it today, else nil.
func (s *Service) Kept(userID int64, lang string) []byte {
	k := stillKey{userID, lang, LatestClosed(s.Zones, s.now())}
	s.stillMu.Lock()
	defer s.stillMu.Unlock()
	return s.stills[k]
}

// publicScene is the profile picture as a stranger may see it: the days up
// to upTo, without the pins.
func publicScene(data db.Profile, lang string, upTo round.Date) *anim.Profile {
	p := build(data, lang, upTo, func(d round.Date) bool { return !upTo.Before(d) })
	if p != nil {
		p.Pins = nil
	}
	return p
}
