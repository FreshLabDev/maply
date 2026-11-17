// SPDX-License-Identifier: Apache-2.0
//go:build integration

package schedule

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asterfield-dev/maply/internal/i18n"
	"github.com/asterfield-dev/maply/internal/round"
)

// messages is what the group got on the date, by kind: sends only, not
// edits or pins.
func (st *standing) messages(from int) []sent {
	var out []sent
	for _, m := range st.fake.log[from:] {
		if m.chatID == group && (m.kind == "photo" || m.kind == "text" || m.kind == "animation") {
			out = append(out, m)
		}
	}
	return out
}

// day runs the scheduler through one Kyiv date, 05:00 to 23:40, every
// twenty minutes, and returns what the group got.
func (st *standing) day(t *testing.T, d round.Date) []sent {
	t.Helper()
	from := len(st.fake.log)
	for st.clock = d.At(5, 0, st.kyiv); st.clock.Before(d.At(23, 59, st.kyiv)); st.clock = st.clock.Add(20 * time.Minute) {
		st.s.Step(context.Background())
	}
	return st.messages(from)
}

// A group where nobody plays: seven quiet days bring the question once, in
// place of the eighth day's intrigue; three days without a press pause the
// group, and while paused it gets nothing; an admin's Resume in the middle
// of a day starts it again with the next morning's post.
func TestQuietGroupIsAskedPausedAndResumed(t *testing.T) {
	st := newStanding(t, 16)
	ctx := context.Background()
	ask := i18n.T("en", "quiet.ask")
	asks := func(ms []sent) int {
		n := 0
		for _, m := range ms {
			if m.text == ask {
				n++
			}
		}
		return n
	}

	// Seven quiet days: the post and an intrigue each, no result since
	// nobody answered anywhere, and no question.
	for i := range QuietDays {
		got := st.day(t, st.d.AddDays(i))
		if len(got) != 2 || asks(got) != 0 {
			t.Fatalf("quiet day %d: the group got %d messages (%d questions), want the post and the intrigue: %+v", i+1, len(got), asks(got), got)
		}
	}

	// The eighth: the question takes the intrigue's place.
	asked := st.d.AddDays(QuietDays)
	got := st.day(t, asked)
	if len(got) != 2 || asks(got) != 1 {
		t.Fatalf("day 8: %d messages, %d questions, want the post and the question: %+v", len(got), asks(got), got)
	}
	q := got[1]
	if len(q.buttons) != 1 || len(q.buttons[0]) != 2 || q.buttons[0][0].CallbackData != QuietKeep || q.buttons[0][1].CallbackData != QuietPause {
		t.Fatalf("question's buttons = %+v", q.buttons)
	}

	// Three more days without a press: the posts still come, the question
	// does not come again.
	for i := 1; i < QuietWait; i++ {
		got := st.day(t, asked.AddDays(i))
		if len(got) != 2 || asks(got) != 0 {
			t.Fatalf("day %d after the question: %d messages, %d questions: %+v", i, len(got), asks(got), got)
		}
	}
	// The third morning after it, the group is paused: no post, and the
	// question says how to turn maply back on.
	pausedOn := asked.AddDays(QuietWait)
	if got := st.day(t, pausedOn); len(got) != 0 {
		t.Fatalf("the day of the pause sent %+v", got)
	}
	if edit := st.fake.last("edit_text", group); edit.text != i18n.T("en", "quiet.paused") || len(edit.buttons) != 0 {
		t.Fatalf("the question became %q with %+v", edit.text, edit.buttons)
	}
	var by string
	if err := st.pool.QueryRow(ctx, `SELECT paused_by FROM maply.chat_settings WHERE chat_id = $1`, group).Scan(&by); err != nil || by != "silence" {
		t.Fatalf("paused_by = %q (%v)", by, err)
	}
	chats, err := st.store.ActiveChats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chats {
		if c.ID == group {
			t.Fatal("a paused group is an active chat")
		}
	}

	// Paused, nothing at all.
	calls := len(st.fake.log)
	resumedOn := pausedOn.AddDays(1)
	from := len(st.fake.log)
	for st.clock = resumedOn.At(5, 0, st.kyiv); st.clock.Before(resumedOn.At(12, 0, st.kyiv)); st.clock = st.clock.Add(20 * time.Minute) {
		st.s.Step(ctx)
	}
	if len(st.fake.log) != calls {
		t.Fatalf("a paused group got %+v", st.fake.log[calls:])
	}
	// An admin resumes at noon: today's post time is gone, so today stays
	// quiet, and the next morning's post goes out.
	if ok, err := st.store.ResumeGroup(ctx, group, st.clock); err != nil || !ok {
		t.Fatalf("resume = %v, %v", ok, err)
	}
	for ; st.clock.Before(resumedOn.At(23, 59, st.kyiv)); st.clock = st.clock.Add(20 * time.Minute) {
		st.s.Step(ctx)
	}
	if got := st.messages(from); len(got) != 0 {
		t.Fatalf("the day of the resume sent %+v", got)
	}
	got = st.day(t, resumedOn.AddDays(1))
	if len(got) != 2 || !strings.Contains(got[0].text, "maply #") || asks(got) != 0 {
		t.Fatalf("the morning after the resume: %+v, want the post and the intrigue", got)
	}
}

// One answer from the group ends the run of quiet days, and an answer
// after the question takes it back: no pause.
func TestAnAnswerKeepsAQuietGroupGoing(t *testing.T) {
	st := newStanding(t, 13)
	ctx := context.Background()
	for i := range QuietDays {
		st.day(t, st.d.AddDays(i))
	}
	asked := st.d.AddDays(QuietDays)
	if got := st.day(t, asked); len(got) != 2 || got[1].text != i18n.T("en", "quiet.ask") {
		t.Fatalf("day 8 = %+v, want the question", got)
	}
	// The next day someone answers from the group.
	next := asked.AddDays(1)
	st.clock = next.At(5, 0, st.kyiv)
	for ; st.clock.Before(next.At(10, 0, st.kyiv)); st.clock = st.clock.Add(20 * time.Minute) {
		st.s.Step(ctx)
	}
	exec(t, st.pool, `INSERT INTO maply.guess (date, user_id, context, chat_id, lat, lon, answered_at, deadline_at)
	                  VALUES ($1, $2, 'group', $3, 40.4, -3.7, $4, $5)`, next.Time(), player, group, st.clock, next.Reveal(st.kyiv))
	for ; st.clock.Before(next.At(23, 59, st.kyiv)); st.clock = st.clock.Add(20 * time.Minute) {
		st.s.Step(ctx)
	}
	for i := 2; i <= QuietWait+1; i++ {
		if got := st.day(t, asked.AddDays(i)); len(got) < 2 || !strings.Contains(got[0].text, "maply #") {
			t.Fatalf("day %d after the question: %+v, want the post", i, got)
		}
	}
	if paused, err := st.store.Paused(ctx, group); err != nil || paused {
		t.Fatalf("paused = %v, %v after an answer", paused, err)
	}
	if q, err := st.store.QuietOf(ctx, group, asked.AddDays(QuietWait+2)); err != nil || q.Asked || q.Days != QuietWait {
		t.Fatalf("quiet = %+v, %v", q, err)
	}
}
