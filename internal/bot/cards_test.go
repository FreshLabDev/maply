// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/profile"
)

type fakeProfiles struct {
	ready bool
	card  profile.Card
	err   error
	kept  []string
	draws int
}

func (f *fakeProfiles) Ready(context.Context, int64, string) (profile.Card, bool, error) {
	return f.card, f.ready, f.err
}

func (f *fakeProfiles) Draw(context.Context, int64, string, int) (profile.Card, error) {
	f.draws++
	return f.card, f.err
}

func (f *fakeProfiles) Keep(_ context.Context, _ int64, _, version, fileID string) error {
	f.kept = append(f.kept, version+":"+fileID)
	return nil
}

func statsBot(t *testing.T, p *fakeProfiles) (*Bot, *fakeTelegram) {
	t.Helper()
	b, store, client := testBot(t)
	store.stats = db.Stats{Days: 3, Points: 9000, Best: 4000}
	b.opts.Profiles = p
	return b, client
}

// A picture drawn ahead goes on the panel at once, and its file_id is kept
// for the next press.
func TestStatsShowsThePictureWhenItIsReady(t *testing.T) {
	p := &fakeProfiles{ready: true, card: profile.Card{Version: "v1", MP4: []byte("mp4")}}
	b, client := statsBot(t, p)
	b.handle(context.Background(), press(private(member), member, cb(userID, actStats)))
	if len(client.anims) != 1 || len(client.edits) != 0 || client.anims[0].messageID != 100 {
		t.Fatalf("animations %+v, edits %+v", client.anims, client.edits)
	}
	if len(p.kept) != 1 || p.kept[0] != "v1:uploaded" || p.draws != 0 {
		t.Fatalf("kept %v, draws %d", p.kept, p.draws)
	}
}

// Without a picture the numbers come at once as text, and the picture takes
// their place when it is drawn.
func TestStatsTextFirstThenThePicture(t *testing.T) {
	p := &fakeProfiles{card: profile.Card{Version: "v2", MP4: []byte("mp4")}}
	b, client := statsBot(t, p)
	client.animated = make(chan struct{}, 1)
	b.handle(context.Background(), press(private(member), member, cb(userID, actStats)))
	if len(client.edits) != 1 {
		t.Fatalf("edits %+v", client.edits)
	}
	select {
	case <-client.animated:
	case <-time.After(5 * time.Second):
		t.Fatal("the picture never came")
	}
	if client.anims[0].messageID != 100 || client.anims[0].text != client.edits[0].text {
		t.Fatalf("animation %+v after %+v", client.anims[0], client.edits[0])
	}
}

// Back from the picture brings the panel back as a new message, since a
// picture cannot turn into text, and the picture goes.
func TestBackFromThePictureSendsThePanelAgain(t *testing.T) {
	p := &fakeProfiles{ready: true, card: profile.Card{Version: "v1", FileID: "kept"}}
	b, client := statsBot(t, p)
	ctx := context.Background()
	b.handle(ctx, press(private(member), member, cb(userID, actStats)))
	back := press(private(member), member, cb(userID, actHome))
	back.Callback.Message = tg.Message{MessageID: 100, Chat: private(member), Caption: "stats", Document: &tg.Document{FileID: "kept"}}
	b.handle(ctx, back)
	if len(client.sends) != 1 || len(client.deleted) != 1 || client.deleted[0] != 100 {
		t.Fatalf("sends %+v, deleted %v", client.sends, client.deleted)
	}
	if len(p.kept) != 0 {
		t.Fatalf("a file_id already kept was kept again: %v", p.kept)
	}
}

// The result is an animation too; a forged Close against it is ignored.
func TestForgedCloseLeavesOtherAnimations(t *testing.T) {
	b, client := statsBot(t, &fakeProfiles{})
	c := press(group, member, cb(userID, actClose))
	c.Callback.Message = tg.Message{MessageID: 7, Chat: group, Caption: "maply #1", Document: &tg.Document{FileID: "result"}}
	b.handle(context.Background(), c)
	if len(client.deleted) != 0 {
		t.Fatal("a forged Close deleted the result")
	}
}

func TestStatsWithNothingToDrawStayText(t *testing.T) {
	b, client := statsBot(t, &fakeProfiles{err: profile.ErrNothing})
	b.handle(context.Background(), press(private(member), member, cb(userID, actStats)))
	if len(client.edits) != 1 || len(client.anims) != 0 {
		t.Fatalf("edits %d, animations %d", len(client.edits), len(client.anims))
	}
}
