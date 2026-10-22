// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
)

// locationOf reads a location the person sent. A live location is an answer
// too: its first point is where they meant.
func locationOf(m *tg.Message) (geo.Point, bool) {
	if len(m.Raw) == 0 {
		return geo.Point{}, false
	}
	var raw struct {
		Location *struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"location"`
	}
	if err := json.Unmarshal(m.Raw, &raw); err != nil || raw.Location == nil {
		return geo.Point{}, false
	}
	p := geo.Point{Lat: raw.Location.Latitude, Lon: raw.Location.Longitude}
	return p, p.Valid()
}

// onLocation takes a location in a DM as the day's answer, for when the Mini
// App does not open. The rules are the Mini App's: one answer a day, and the
// window closes at the earliest reveal among the person's chats.
func (b *Bot) onLocation(ctx context.Context, m *tg.Message, p geo.Point) error {
	raw, err := b.personLang(ctx, *m.From)
	if err != nil {
		return err
	}
	lang := i18n.Resolve(raw)
	zone, err := b.store.ChatZone(ctx, m.From.ID)
	if err != nil {
		return err
	}
	loc := b.opts.Zones.For(zone, raw)
	now := b.opts.Now()
	d := round.DateIn(now, loc)
	reply := func(key string) error {
		_, err := b.tg.SendText(ctx, m.Chat.ID, i18n.T(lang, key, "time", revealTime()), nil)
		return err
	}
	if _, ok, err := b.store.RoundOn(ctx, d); err != nil {
		return err
	} else if !ok {
		return reply("loc.closed")
	}
	deadline, err := b.deadline(ctx, m.From.ID, d, loc)
	if err != nil {
		return err
	}
	if now.After(deadline) {
		return reply("loc.closed")
	}
	err = b.store.InsertGuess(ctx, db.NewGuess{
		Date: d, UserID: m.From.ID, Context: "dm", Pin: p, AnsweredAt: now, Deadline: deadline,
	})
	if errors.Is(err, db.ErrAlreadyAnswered) {
		return reply("loc.already")
	}
	if err != nil {
		return err
	}
	return reply("loc.accepted")
}

// deadline is the earliest reveal among the DM and the person's groups, as
// /api/guess counts it.
func (b *Bot) deadline(ctx context.Context, userID int64, d round.Date, own *time.Location) (time.Time, error) {
	contexts, err := b.store.Contexts(ctx, userID)
	if err != nil {
		return time.Time{}, err
	}
	locs := []*time.Location{own}
	for _, c := range contexts {
		locs = append(locs, b.opts.Zones.For(c.Zone, c.Lang))
	}
	return round.Deadline(d, locs)
}
