// SPDX-License-Identifier: Apache-2.0
//go:build integration

package schedule

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/anim"
	"github.com/FreshLabDev/maply/internal/auth"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/world"
)

// A DM's result is the world picture with a Refresh button and a note while
// other zones still answer; Refresh draws the answers scored since, and once
// the day closes everywhere the final picture replaces it and the button goes.
func TestDMResultIsTheWorld(t *testing.T) {
	store, pool := testDB(t)
	ctx := context.Background()
	images := t.TempDir()
	if err := os.WriteFile(filepath.Join(images, "k1"), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `INSERT INTO maply.place (status, source, source_id, license, kind, lat, lon, image_key,
	                                       country_iso, continent, city, fact, difficulty, reviewed_at)
	               VALUES ('approved', 'mapillary', 'm1', 'CC-BY-SA-4.0', 'photo', 38.72, -9.14, 'k1', 'PT', 'europe',
	                       '{"en":"Lisbon"}', '{"en":"Built on seven hills."}', 2, now())`)
	const late = int64(10) // answers from far west, after the Kyiv result
	// Both are DMs with the bot.
	for _, u := range []tg.User{{ID: alone, FirstName: "Solo"}, {ID: late, FirstName: "West"}} {
		if err := store.Touch(ctx, u, nil); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, pool, `INSERT INTO maply.chat_settings (chat_id, kind, timezone) VALUES ($1, 'dm', 'Europe/Kyiv')`, alone)

	zones, err := round.LoadZones()
	if err != nil {
		t.Fatal(err)
	}
	countries, err := geo.World()
	if err != nil {
		t.Fatal(err)
	}
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	clock := time.Date(2026, 11, 2, 5, 0, 0, 0, kyiv)
	now := func() time.Time { return clock }
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	drawn := 0
	fake := &fakeTG{fail: map[int64]error{}}
	s := &Scheduler{
		Store: store, TG: fake, Weather: fakeWeather{}, Payloads: auth.NewSigner("123:abc"),
		Zones: zones, Countries: countries, ImagesDir: images, Username: "maplyin_bot",
		AppURL: "https://maply.example/", Log: log, Now: now,
		World: &world.Service{Store: store, Zones: zones, Log: log, Now: now,
			Draw: func(context.Context, anim.Scene) ([]byte, error) { drawn++; return []byte("mp4"), nil }},
	}
	d := round.DateIn(clock, kyiv)
	for ; clock.Before(d.At(20, 10, kyiv)); clock = clock.Add(10 * time.Minute) {
		if clock.Hour() == 10 && clock.Minute() == 0 {
			exec(t, pool, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
			               VALUES ($1, $2, 'dm', 40.4, -3.7, $3, $4)`, d.Time(), alone, clock, d.Reveal(kyiv))
		}
		s.Step(ctx)
	}

	// Both DMs get the one picture: drawn once, uploaded once, then sent by
	// the file_id Telegram gave it.
	res, other := fake.last("animation", alone), fake.last("animation", late)
	if first, second := other, res; drawn != 1 || first.upload == second.upload || (!first.upload && first.fileID == "") || (!second.upload && second.fileID == "") {
		t.Fatalf("drawn %d times, uploads %t %t, resent %q %q", drawn, first.upload, second.upload, first.fileID, second.fileID)
	}
	if b := res.buttons[0][0]; b.CallbackData != "wr:"+d.String() || b.Text != "Refresh" {
		t.Errorf("first button = %+v, want Refresh", b)
	}
	for _, want := range []string{"World place so far: 1 of 1.", "<i>Other time zones are still answering", "tomorrow at"} {
		if !strings.Contains(res.text, want) {
			t.Errorf("caption %q lacks %q", res.text, want)
		}
	}
	cr, err := store.ChatRound(ctx, alone, d)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := store.ResultAnimation(ctx, alone, d); id == "" {
		t.Error("the world picture's file id is not kept for sharing")
	}

	// Nothing new: the press says so and draws nothing.
	if r, err := s.RefreshWorld(ctx, alone, cr.ResultMsgID, d); err != nil || r != RefreshSame {
		t.Fatalf("refresh with nothing new = %v, %v", r, err)
	}
	if r, _ := s.RefreshWorld(ctx, alone, cr.ResultMsgID+1, d); r != RefreshGone {
		t.Errorf("refresh under another message = %v", r)
	}

	// Somebody further west answers; their time is up a few minutes later.
	clock = clock.Add(5 * time.Minute)
	exec(t, pool, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
	               VALUES ($1, $2, 'dm', 38.7, -9.1, $3, $3)`, d.Time(), late, clock)
	if r, err := s.RefreshWorld(ctx, alone, cr.ResultMsgID, d); err != nil || r != RefreshDrawing {
		t.Fatalf("refresh with a new answer = %v, %v", r, err)
	}
	waitFor(t, func() bool { return fake.count("edit_animation", alone) == 1 })
	edit := fake.last("edit_animation", alone)
	if !strings.Contains(edit.text, "World place so far: 2 of 2.") || edit.buttons[0][0].CallbackData == "" {
		t.Errorf("refreshed result = %+v", edit)
	}
	waitFor(t, func() bool {
		id, _ := store.ResultAnimation(ctx, alone, d)
		return strings.HasPrefix(id, "edited-")
	})
	if r, _ := s.RefreshWorld(ctx, alone, cr.ResultMsgID, d); r != RefreshSame {
		t.Errorf("second refresh = %v, want nothing new", r)
	}

	// The day closes everywhere: the final picture, no button, no note.
	clock = zones.LastReveal(d).Add(worldAfter + time.Minute)
	s.Step(ctx)
	if n := fake.count("edit_animation", alone); n != 2 {
		t.Fatalf("final pictures = %d, want 1", n-1)
	}
	final := fake.last("edit_animation", alone)
	if strings.Contains(final.text, "Other time zones") || !strings.Contains(final.text, "Your place: 2 of 2.") {
		t.Errorf("final caption = %q", final.text)
	}
	for _, row := range final.buttons {
		for _, b := range row {
			if b.CallbackData != "" {
				t.Errorf("final result still has %+v", b)
			}
		}
	}
	if r, _ := s.RefreshWorld(ctx, alone, cr.ResultMsgID, d); r != RefreshFinal {
		t.Errorf("refresh after the day closed = %v", r)
	}
	// The job ran once.
	s.Step(ctx)
	if n := fake.count("edit_animation", alone); n != 2 {
		t.Errorf("final picture put %d times", n-1)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
