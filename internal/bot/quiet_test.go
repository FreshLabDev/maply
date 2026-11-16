// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/schedule"
)

// Pause and Resume are an admin's: a member's press is refused on the
// press itself, whatever the callback says, and changes nothing.
func TestOnlyAnAdminPausesOrResumes(t *testing.T) {
	b, store, client := testBot(t)
	ctx := context.Background()
	refused := i18n.T("ru", "msg.admin_only")

	store.chatLang[groupID] = "en"
	store.asked[groupID] = true
	b.handle(ctx, press(group, member, schedule.QuietPause))
	if store.paused[groupID] || len(client.alerts) != 1 || client.alerts[0] != refused || len(client.edits) != 0 {
		t.Fatalf("a member paused: paused %v, alerts %v, edits %d", store.paused[groupID], client.alerts, len(client.edits))
	}
	// Keep playing is anyone's.
	b.handle(ctx, press(group, member, schedule.QuietKeep))
	if store.asked[groupID] || len(client.edits) != 1 || client.edits[0].text != i18n.T("en", "quiet.kept") || len(client.edits[0].markup) != 0 {
		t.Fatalf("keep playing: asked %v, edits %+v", store.asked[groupID], client.edits)
	}
	b.handle(ctx, press(group, admin, schedule.QuietPause))
	if !store.paused[groupID] || len(client.edits) != 2 || client.edits[1].text != i18n.T("en", "quiet.paused") {
		t.Fatalf("an admin's pause: paused %v, edits %+v", store.paused[groupID], client.edits)
	}

	// /start in the paused group says so; Resume is on the admin's panel only.
	b.handle(ctx, command(group, member, "/start"))
	text, markup := client.sends[len(client.sends)-1].text, client.sends[len(client.sends)-1].markup
	if !strings.Contains(text, i18n.T("en", "home.paused")) || has(markup, actResume) {
		t.Fatalf("member's /start in a paused group = %q, %+v", text, markup)
	}
	b.handle(ctx, command(group, admin, "/start"))
	if markup := client.sends[len(client.sends)-1].markup; !has(markup, actResume) {
		t.Fatalf("admin's /start in a paused group has no Resume: %+v", markup)
	}

	// A member forging Resume into their own panel is refused.
	b.handle(ctx, press(group, member, cb(userID, actResume)))
	if !store.paused[groupID] || len(client.alerts) != 2 || client.alerts[1] != refused {
		t.Fatalf("a member resumed: paused %v, alerts %v", store.paused[groupID], client.alerts)
	}
	b.handle(ctx, press(group, admin, cb(adminID, actResume)))
	if store.paused[groupID] {
		t.Fatal("an admin's Resume left the group paused")
	}
	if last := client.edits[len(client.edits)-1]; !strings.Contains(last.text, i18n.T("en", "home.resumed", "post", "07:00")) || has(last.markup, actResume) {
		t.Fatalf("after Resume the panel = %q, %+v", last.text, last.markup)
	}
}
