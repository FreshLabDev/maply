// SPDX-License-Identifier: Apache-2.0
//go:build integration

package free_test

import (
	"slices"
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/round"
)

// closedRules settles an account over days that are all over.
var closedRules = db.Rules{Open: func(round.Date) bool { return false }}

// accountOf is the account a site player belongs to.
func (h *harness) accountOf(w free.Who) string {
	h.t.Helper()
	var id string
	if err := h.pool.QueryRow(h.ctx, `SELECT account_id::text FROM maply.site_player WHERE id = $1`, w.Player).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

// linkTelegram links the person w to account, as the site's link does.
func (h *harness) linkTelegram(account string, w free.Who, name string) {
	h.t.Helper()
	if _, err := h.pool.Exec(h.ctx, `SELECT core.touch('maply', $1, NULL, $2)`, w.UserID, name); err != nil {
		h.t.Fatal(err)
	}
	if res, err := h.store.Link(h.ctx, account, db.Identity{Kind: db.KindTelegram, TelegramUserID: w.UserID}, closedRules); err != nil || res != db.Linked {
		h.t.Fatalf("link %d: %v %v", w.UserID, res, err)
	}
}

// siteAnswer is a site player's answer on closed day d.
func (h *harness) siteAnswer(w free.Who, d round.Date) {
	h.t.Helper()
	at := d.Reveal(time.UTC).Add(-time.Hour)
	if _, err := h.pool.Exec(h.ctx, `
		INSERT INTO maply.site_guess (date, player, lat, lon, answered_at, deadline_at, km, pts_km, pts_bonus)
		VALUES ($1, $2, 0, 0, $3, $4, 3000, 100, 0)`, d.Time(), w.Player, at, at.Add(time.Hour)); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) stateOf(w free.Who, n int) string {
	h.t.Helper()
	days, err := h.svc.ArchiveOf(h.ctx, w)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, d := range days {
		if d.N == n {
			return d.State
		}
	}
	return "none"
}

// candidates is the places game id may get next with the days closed by
// closed.
func (h *harness) candidates(id int64, closed round.Date) []int64 {
	h.t.Helper()
	cands, _, err := h.store.FreeCandidates(h.ctx, id, closed.Time())
	if err != nil {
		h.t.Fatal(err)
	}
	out := []int64{}
	for _, c := range cands {
		out = append(out, c.PlaceID)
	}
	return out
}

// A person is every player their account holds: a day answered as a guest
// before signing in, or taken from the archive as the account's site player
// before Telegram was linked, is theirs in the archive and in the places
// their games may get.
func TestThePersonIsEveryPlayerOfTheirAccount(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	byGuest := h.addPlace("by-guest", "used", spot{"SE", "europe", 59.3293, 18.0686})
	taken := h.addPlace("taken", "used", spot{"FI", "europe", 60.1699, 24.9384})
	open := h.addPlace("open", "used", spot{"NO", "europe", 59.9139, 10.7522})
	d1, d2, d3 := round.NewDate(2026, time.October, 20), round.NewDate(2026, time.October, 21), round.NewDate(2026, time.October, 22)
	h.day(1, d1, byGuest, true)
	h.day(2, d2, taken, true)
	h.day(3, d3, open, true)
	far := round.NewDate(2026, time.December, 31)

	// Ann answered day 1 as a guest of the site, then signed in with
	// Telegram in that browser: the account took the guest in.
	guest := h.guestOf()
	h.siteAnswer(guest, d1)
	if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'Ann')`, ann.UserID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.store.SignIn(ctx, db.Identity{Kind: db.KindTelegram, TelegramUserID: ann.UserID}, guest.Player, closedRules); err != nil {
		t.Fatal(err)
	}
	if got := h.stateOf(ann, 1); got != free.DayAnswered {
		t.Fatalf("ann's day 1: %s", got)
	}
	if _, err := h.svc.NewArchive(ctx, ann, "Ann", 1, "en"); !is(err, free.ErrDayAnswered) {
		t.Fatalf("ann takes day 1 again: %v", err)
	}
	if known, err := h.store.FreeKnown(ctx, ann, byGuest); err != nil || !known {
		t.Fatalf("ann knows day 1's place: %v %v", known, err)
	}
	a, err := h.svc.NewSolo(ctx, ann, "Ann", free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.candidates(a.ID, far); slices.Contains(got, byGuest) || !slices.Contains(got, taken) || !slices.Contains(got, open) {
		t.Fatalf("ann's places: %v", got)
	}

	// Lena, with Google alone, takes day 2 and leaves it for later, then
	// links Telegram: as the person she has had its place.
	lena := h.googler("g-lena", "Lena")
	if _, err := h.svc.NewArchive(ctx, lena, "Lena", 2, "en"); err != nil {
		t.Fatal(err)
	}
	l, err := h.svc.NewSolo(ctx, lena, "Lena", free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	// A day taken and not answered never comes back as a repeat with the
	// answer, however many places she plays.
	if got := h.candidates(l.ID, far); slices.Contains(got, taken) || !slices.Contains(got, open) {
		t.Fatalf("lena's places: %v", got)
	}
	h.linkTelegram(h.accountOf(lena), bob, "Bob")
	if got := h.stateOf(bob, 2); got != free.DaySeen {
		t.Fatalf("bob's day 2: %s", got)
	}
	if _, err := h.svc.NewArchive(ctx, bob, "Bob", 2, "en"); !is(err, free.ErrDaySeen) {
		t.Fatalf("bob takes day 2 again: %v", err)
	}
	b, err := h.svc.NewSolo(ctx, bob, "Bob", free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.candidates(b.ID, far); slices.Contains(got, taken) || !slices.Contains(got, open) {
		t.Fatalf("bob's places: %v", got)
	}
	if known, err := h.store.FreeKnown(ctx, bob, taken); err != nil || !known {
		t.Fatalf("bob knows day 2's place: %v %v", known, err)
	}
	// Day 3 is open to both, and a new guest knows nothing.
	if got := h.stateOf(bob, 3); got != free.DayOpen {
		t.Fatalf("bob's day 3: %s", got)
	}
	if got := h.stateOf(h.guestOf(), 1); got != free.DayOpen {
		t.Fatalf("a new guest's day 1: %s", got)
	}
}

// A row in day_closed written before the day is over everywhere opens
// nothing: the archive and the places of free play check the clock.
func TestAClosedDayWaitsForTheClock(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	today := round.NewDate(2026, time.November, 5)
	place := h.addPlace("today", "used", spot{"SE", "europe", 59.3293, 18.0686})
	h.day(7, today, place, true)
	if got := h.stateOf(ann, 7); got != "none" {
		t.Fatalf("today in the archive at noon: %s", got)
	}
	if _, err := h.svc.NewArchive(ctx, ann, "Ann", 7, "en"); !is(err, free.ErrNotFound) {
		t.Fatalf("today taken at noon: %v", err)
	}
	a, err := h.svc.NewSolo(ctx, ann, "Ann", free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.candidates(a.ID, today.AddDays(-1)); slices.Contains(got, place) {
		t.Fatalf("today among the places: %v", got)
	}
	// The next day at noon UTC it is over in every zone.
	h.jump(time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC))
	if got := h.stateOf(ann, 7); got != free.DayOpen {
		t.Fatalf("the day after: %s", got)
	}
	if got := h.candidates(a.ID, today); !slices.Contains(got, place) {
		t.Fatalf("the day after, the places: %v", got)
	}
}

// A challenge is played once a person: an attempt made as the account's
// site player before Telegram was linked is the person's.
func TestAChallengeIsOnceAPerson(t *testing.T) {
	h := setup(t)
	ctx := h.ctx
	if _, err := h.pool.Exec(ctx, `SELECT core.touch('maply', $1, NULL, 'Ann')`, ann.UserID); err != nil {
		t.Fatal(err)
	}
	set := free.Settings{Mode: free.Classic, Seconds: 60, Rounds: 3}
	a, err := h.svc.NewSolo(ctx, ann, "Ann", set)
	if err != nil {
		t.Fatal(err)
	}
	h.playSolo(ann, a.ID, 3, false)
	code, err := h.svc.MakeChallenge(ctx, ann, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	lena := h.googler("g-lena", "Lena")
	lv, err := h.svc.TakeChallenge(ctx, lena, "Lena", code, "en")
	if err != nil {
		t.Fatal(err)
	}
	h.playSolo(lena, lv.ID, 3, false)
	h.linkTelegram(h.accountOf(lena), bob, "Bob")
	if _, err := h.svc.TakeChallenge(ctx, bob, "Bob", code, "en"); !is(err, free.ErrChallengeTaken) {
		t.Fatalf("bob takes it again: %v", err)
	}
	cv, err := h.svc.ChallengeOf(ctx, bob, code)
	if err != nil || cv.Mine != nil || !cv.In() || len(cv.Lines) != 2 {
		t.Fatalf("bob sees it: %+v %v", cv, err)
	}
	for _, l := range cv.Lines {
		if l.Me != (l.Name == "Lena") {
			t.Fatalf("bob's line: %+v", cv.Lines)
		}
	}
	// The store holds it too, whatever the service saw first.
	c, err := h.store.FreeChallenge(ctx, code, bob)
	if err != nil {
		t.Fatal(err)
	}
	now := h.clock()
	if _, err := h.store.TakeFreeChallenge(ctx, c.ID, free.NewGame{Kind: free.Challenge, Settings: set, Status: free.StatusPlaying, At: now,
		Host: free.Member{M: 1, Who: bob, Name: "Bob", JoinedAt: now, FromRound: 1, LastSeen: now}}); !is(err, free.ErrChallengeTaken) {
		t.Fatalf("the store takes it again: %v", err)
	}
	// Lena's own attempt is hers still, and Ann's line is not Bob's.
	if cv, err := h.svc.ChallengeOf(ctx, ann, code); err != nil || cv.Mine == nil || !cv.In() {
		t.Fatalf("ann sees it: %+v %v", cv, err)
	}
}
