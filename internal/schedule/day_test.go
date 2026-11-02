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

func scored(id int64, name string, km float64, pts int) db.Guess {
	bonus := 0
	return db.Guess{UserID: id, FirstName: name, Km: &km, PtsKm: &pts, PtsBonus: &bonus}
}

// The post names who answered in the alphabet's order, never the answers',
// and the best distances without names.
func TestAnsweredBlockHidesWhoIsWho(t *testing.T) {
	got := answeredBlock("ru", []db.Guess{
		scored(1, "Вика", 501, 1000), scored(2, "аня", 276, 3000), scored(3, "Боря", 290, 2900), scored(4, "Гена", 1200, 500),
	})
	want := "<b>Ответили · 4</b>\nаня, Боря, Вика, Гена\n<b>Лучшие пока</b>\n276 км · 290 км · 501 км"
	if got != want {
		t.Fatalf("block = %q, want %q", got, want)
	}
	var many []db.Guess
	for i := 0; i < nameLimit+5; i++ {
		many = append(many, db.Guess{UserID: int64(i), FirstName: "A"})
	}
	if got := answeredBlock("en", many); !strings.Contains(got, "+5") || strings.Contains(got, "<b>Best") {
		t.Fatalf("answered = %q", got)
	}
	if got := answeredBlock("ru", nil); got != "Ответов пока нет." {
		t.Fatalf("nobody = %q", got)
	}
}

func TestPostCaptionSetsYesterdayApart(t *testing.T) {
	got := postCaption("ru", 42, "Вчера первое место в мире: Ким, 12 км.", "<b>Ответили · 1</b>\nАня", true)
	if !strings.HasPrefix(got, "<b>maply #42</b> · ") || !strings.Contains(got, "\n\n<b>Ответили · 1</b>\nАня\n\n<blockquote>Вчера") || !strings.HasSuffix(got, "</blockquote>") {
		t.Fatalf("post = %q", got)
	}
	if dm := postCaption("ru", 42, "", "", false); strings.Contains(dm, "\n\n") {
		t.Fatalf("dm post = %q", dm)
	}
}

func TestTableEscapesNamesAndShowsMedalsAndStreaks(t *testing.T) {
	lines := table("en", []db.Guess{scored(1, "<i>x", 12.4, 3500), scored(2, "Bo", 40, 3000), scored(3, "Cy", 90, 2000), scored(4, "Di", 900, 900), {UserID: 5, FirstName: "Ed"}}, map[int64]int{1: 4})
	want := []string{"🥇 &lt;i&gt;x · 12 km · 3,500 🔥4", "🥈 Bo · 40 km · 3,000", "🥉 Cy · 90 km · 2,000", "4. Di · 900 km · 900", "5. Ed · …"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("table = %q", lines)
	}
}

// The world line ranks the group's players; the final one names the world's
// first unless it is one of them.
func TestWorldGroupLine(t *testing.T) {
	w := db.World{Entries: []db.Entry{{UserID: 9, Name: "Kim", Km: 12, Rank: 1}, {UserID: 2, Rank: 2}, {UserID: 7, Rank: 3}, {UserID: 1, Rank: 4}}}
	guesses := []db.Guess{scored(1, "Аня", 300, 2000), scored(2, "Боря", 50, 3000), {UserID: 3, FirstName: "Вика"}}
	if got := worldGroupLine("ru", w, guesses, false); got != "🌍 В мире сейчас: Боря #2, Аня #4 из 4." {
		t.Fatalf("now = %q", got)
	}
	if got := worldGroupLine("ru", w, guesses, true); got != "🌍 Итог дня в мире: Боря #2, Аня #4 из 4. Первое место: Kim, 12 км." {
		t.Fatalf("final = %q", got)
	}
	if got := worldGroupLine("ru", w, []db.Guess{scored(9, "Kim", 12, 3400)}, true); strings.Contains(got, "Первое место") {
		t.Fatalf("final with the first in the group = %q", got)
	}
	if got := worldGroupLine("ru", db.World{}, guesses, false); got != "" {
		t.Fatalf("empty world = %q", got)
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
