// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"strconv"
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
// and every distance, closest first, without names.
func TestAnsweredBlockHidesWhoIsWho(t *testing.T) {
	got := answeredBlock("ru", []db.Guess{
		scored(1, "Вика", 501, 1000), scored(2, "аня", 276, 3000), scored(3, "Боря", 290, 2900), scored(4, "Гена", 1200, 500),
	})
	want := "<b>Ответили · 4</b>\nаня, Боря, Вика, Гена\n<b>Расстояния</b>\n276 км · 290 км · 501 км · 1\u00a0200 км"
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
	// Thirty long names would not fit a caption: the list stops by length,
	// and the whole post, with yesterday, stays within Telegram's limit.
	var long []db.Guess
	for i := 0; i < nameLimit; i++ {
		km := float64(100 + i)
		long = append(long, db.Guess{UserID: int64(i), FirstName: strings.Repeat("Я", 30) + strconv.Itoa(i), Km: &km})
	}
	block := answeredBlock("ru", long)
	if !strings.Contains(block, "+") {
		t.Fatalf("long names = %q", block)
	}
	if post := postCaption("ru", 3, "20:00 (Киев)", strings.Repeat("вчера ", 100), block, true); visibleLen(post) > captionLimit || !strings.Contains(post, "Расстояния") || !strings.Contains(block, "100 км · 101 км") {
		t.Fatalf("post is %d long: %q", visibleLen(post), post)
	}
	// Many distances stop by length too, with how many are left.
	var crowd []db.Guess
	for i := 0; i < 60; i++ {
		crowd = append(crowd, scored(int64(i), "A", float64(1000+i), 100))
	}
	if got := answeredBlock("en", crowd); !strings.HasPrefix(got[strings.Index(got, "<b>Distances</b>"):], "<b>Distances</b>\n1,000 km · 1,001 km") || !strings.HasSuffix(got, " · +"+strconv.Itoa(60-strings.Count(got[strings.Index(got, "<b>Distances</b>"):], " km"))) {
		t.Fatalf("crowd = %q", got)
	}
	if got := answeredBlock("ru", nil); got != "Ответов пока нет." {
		t.Fatalf("nobody = %q", got)
	}
}

func TestPostCaptionSetsYesterdayApart(t *testing.T) {
	got := postCaption("ru", 42, "20:00 (Киев)", "Вчера первое место в мире: Ким, 12 км.", "<b>Ответили · 1</b>\nАня", true)
	if !strings.HasPrefix(got, "<b>maply #42</b> · ") || !strings.Contains(got, "\n\n<b>Ответили · 1</b>\nАня\n\n<blockquote>Вчера") || !strings.HasSuffix(got, "</blockquote>") {
		t.Fatalf("post = %q", got)
	}
	if dm := postCaption("ru", 42, "20:00 (Киев)", "", "", false); strings.Contains(dm, "\n\n") {
		t.Fatalf("dm post = %q", dm)
	}
}

func TestTableEscapesNamesAndShowsMedalsAndStreaks(t *testing.T) {
	lines := table("en", []db.Guess{scored(1, "<i>x", 12.4, 3500), scored(2, "Bo", 40, 3000), scored(3, "Cy", 90, 2000), scored(4, "Di", 900, 900), {UserID: 5, FirstName: "Ed"}}, map[int64]int{1: 4})
	want := []string{"🥇 &lt;i&gt;x · 12 km · <b>3,500</b> 🔥4", "🥈 Bo · 40 km · <b>3,000</b>", "🥉 Cy · 90 km · <b>2,000</b>", "4. Di · 900 km · <b>900</b>", "5. Ed · …"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("table = %q", lines)
	}
}

// The world line ranks the group's players; the final one names the world's
// first unless it is one of them.
func TestWorldGroupLine(t *testing.T) {
	w := db.World{Entries: []db.Entry{{UserID: 9, Name: "Kim", Km: 12, Rank: 1}, {UserID: 2, Rank: 2}, {UserID: 7, Rank: 3}, {UserID: 1, Rank: 4}}}
	guesses := []db.Guess{scored(1, "Аня", 300, 2000), scored(2, "Боря", 50, 3000), {UserID: 3, FirstName: "Вика"}}
	if got := worldGroupLine("ru", w, guesses, false); got != "<b>🌍 Места в мире пока, из 4</b>\nБоря #2 · Аня #4" {
		t.Fatalf("now = %q", got)
	}
	if got := worldGroupLine("ru", w, guesses, true); got != "<b>🌍 Итоговые места в мире из 4</b>\nБоря #2 · Аня #4\nПервое место в мире: Kim, 12 км." {
		t.Fatalf("final = %q", got)
	}
	if got := worldGroupLine("ru", w, []db.Guess{scored(9, "Kim", 12, 3400)}, true); strings.Contains(got, "Первое место") {
		t.Fatalf("final with the first in the group = %q", got)
	}
	if got := worldGroupLine("ru", db.World{}, guesses, false); got != "" {
		t.Fatalf("empty world = %q", got)
	}
	// What gives way in a full caption is the world's first, not the places.
	line, first := worldGroupParts("ru", w, guesses, true)
	if line != "<b>🌍 Итоговые места в мире из 4</b>\nБоря #2 · Аня #4" || first != "Первое место в мире: Kim, 12 км." {
		t.Fatalf("parts = %q, %q", line, first)
	}
	// A site player is a guest in a post, whatever name the browser gave.
	site := db.World{Entries: []db.Entry{{Player: "p", Name: "t.me/spam", Km: 1, Rank: 1}, {UserID: 2, Rank: 2}}}
	if _, first := worldGroupParts("ru", site, guesses, true); first != "Первое место в мире: Гость, 1,0 км." {
		t.Fatalf("site first = %q", first)
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
