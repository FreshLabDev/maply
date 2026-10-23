// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/round"
)

func TestFitCaptionDropsLinesFromTheEnd(t *testing.T) {
	head := "<b>maply #3</b> · Lisbon"
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, strings.Repeat("я", 40))
	}
	got := fitCaption(head, lines)
	if visibleLen(got) > captionLimit {
		t.Fatalf("caption is %d long", visibleLen(got))
	}
	if !strings.HasPrefix(got, head) || strings.Count(got, "\n") < 20 {
		t.Fatalf("caption kept too little: %q", got)
	}
}

func TestVisibleLenSkipsTagsAndCountsUTF16(t *testing.T) {
	if n := visibleLen("<b>ab</b>🔥"); n != 4 {
		t.Fatalf("visibleLen = %d, want 4", n)
	}
}

func TestAnsweredTextCapsTheList(t *testing.T) {
	var names []string
	for i := 0; i < nameLimit+5; i++ {
		names = append(names, "A")
	}
	if got := answeredText("en", names); !strings.HasSuffix(got, "+5") {
		t.Fatalf("answered = %q", got)
	}
	if got := answeredText("ru", nil); got != "Ответов пока нет." {
		t.Fatalf("nobody = %q", got)
	}
}

func TestTableEscapesNamesAndShowsStreaks(t *testing.T) {
	km := 12.4
	pts, bonus := 3000, 500
	lines := table("en", []db.Guess{{UserID: 1, FirstName: "<i>x", Km: &km, PtsKm: &pts, PtsBonus: &bonus}}, map[int64]int{1: 4})
	if len(lines) != 1 || lines[0] != "1. &lt;i&gt;x — 12 km · 3,500 🔥4" {
		t.Fatalf("table = %q", lines)
	}
}

func TestIntrigueTimeIsStableAndInTheWindow(t *testing.T) {
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	d := round.NewDate(2026, 11, 2)
	for chat := int64(-100); chat < 100; chat++ {
		at := intrigueAt(chat, d, kyiv).In(kyiv)
		if at != intrigueAt(chat, d, kyiv).In(kyiv) || at.Hour() < 16 || at.Hour() >= 18 {
			t.Fatalf("intrigue for %d at %v", chat, at)
		}
	}
}
