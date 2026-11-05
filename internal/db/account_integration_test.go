// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/round"
)

// The day the account tests play: the round of seedRound, 2026-11-01.
var accountDay = round.NewDate(2026, time.November, 1)

const (
	guestA = "11111111-1111-4111-8111-111111111111"
	guestB = "22222222-2222-4222-8222-222222222222"
	guestC = "33333333-3333-4333-8333-333333333333"
)

func telegramID(id int64) Identity { return Identity{Kind: KindTelegram, TelegramUserID: id} }
func googleID(sub string) Identity { return Identity{Kind: KindGoogle, GoogleSub: sub} }

// openRules keep accountDay open; deadline is the person's Telegram window,
// still open an hour before it.
func openRules(deadline time.Time) Rules {
	return Rules{
		Now:      deadline.Add(-time.Hour),
		Open:     func(round.Date) bool { return true },
		Deadline: func(context.Context, int64, round.Date) (time.Time, error) { return deadline, nil },
	}
}

var closedRules = Rules{Open: func(round.Date) bool { return false }}

func person(t *testing.T, s *Store, id int64, name string) {
	t.Helper()
	if err := s.TouchPerson(context.Background(), tg.User{ID: id, FirstName: name}); err != nil {
		t.Fatal(err)
	}
}

// siteAnswer is a site player's answer of accountDay at hh:mm UTC.
func siteAnswer(t *testing.T, s *Store, player string, hh, mm int) {
	t.Helper()
	exec(t, s, `INSERT INTO maply.site_player (id) VALUES ($1) ON CONFLICT DO NOTHING`, player)
	at := time.Date(2026, time.November, 1, hh, mm, 0, 0, time.UTC)
	exec(t, s, `INSERT INTO maply.site_guess (date, player, lat, lon, answered_at, deadline_at) VALUES ('2026-11-01', $1, 10, 20, $2, '2026-11-02 05:00Z')`, player, at)
}

func countOf(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestSignInMakesOneAccountAndTakesInTheGuest(t *testing.T) {
	s := testStore(t)
	seedRound(t, s)
	ctx := context.Background()
	siteAnswer(t, s, guestA, 9, 0)

	acc, created, err := s.SignIn(ctx, googleID("g-1"), guestA, closedRules)
	if err != nil || !created || acc.Player != guestA || acc.GoogleSub != "g-1" {
		t.Fatalf("first sign-in = %+v %v %v", acc, created, err)
	}
	// The same sign-in again is the same account, and takes in nobody.
	siteAnswer(t, s, guestB, 9, 5)
	again, created, err := s.SignIn(ctx, googleID("g-1"), guestB, closedRules)
	if err != nil || created || again.ID != acc.ID {
		t.Fatalf("second sign-in = %+v %v %v", again, created, err)
	}
	if n, ok, err := s.GuestOf(ctx, guestB); err != nil || !ok || n != 1 {
		t.Fatalf("guest B = %d %v %v; it should wait for the person to say", n, ok, err)
	}
	if _, ok, _ := s.GuestOf(ctx, guestA); ok {
		t.Fatal("guest A is still a guest after its account took it in")
	}
	if owned, err := s.PlayerOwned(ctx, guestA); err != nil || !owned {
		t.Fatalf("guest A owned = %v %v", owned, err)
	}
	// A player an account has is no other account's to take in.
	other, _, err := s.SignIn(ctx, googleID("g-2"), guestA, closedRules)
	if err != nil || other.Player != "" {
		t.Fatalf("another account took in A: %+v %v", other, err)
	}

	// Ten browsers at once make one account.
	person(t, s, 42, "Anya")
	var wg sync.WaitGroup
	ids := make([]string, 10)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, _, err := s.SignIn(ctx, telegramID(42), "", closedRules)
			if err != nil {
				t.Error(err)
			}
			ids[i] = a.ID
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("parallel sign-ins made %v", ids)
		}
	}
	if n := countOf(t, s, `SELECT count(*) FROM maply.account WHERE telegram_user_id = 42`); n != 1 {
		t.Fatalf("%d accounts for one Telegram", n)
	}
}

func TestLinkMergesOrRefuses(t *testing.T) {
	s := testStore(t)
	seedRound(t, s)
	ctx := context.Background()
	for _, p := range []struct {
		id   int64
		name string
	}{{1, "Anya"}, {2, "Boris"}, {3, "Vera"}} {
		person(t, s, p.id, p.name)
	}
	a, _, _ := s.SignIn(ctx, googleID("g-a"), "", closedRules)

	// Nobody's Telegram joins A.
	if res, err := s.Link(ctx, a.ID, telegramID(1), closedRules); err != nil || res != Linked {
		t.Fatalf("link = %v %v", res, err)
	}
	if res, err := s.Link(ctx, a.ID, telegramID(2), closedRules); err != nil || res != AlreadyLinked {
		t.Fatalf("a second Telegram = %v %v", res, err)
	}

	// B has only Telegram 2 and a guest's history: linking 2 to C merges B in.
	siteAnswer(t, s, guestB, 8, 0)
	b, _, _ := s.SignIn(ctx, telegramID(2), guestB, closedRules)
	if err := s.CreateSession(ctx, Session{Hash: [32]byte{2}, AccountID: b.ID, Via: KindTelegram, Created: time.Now(), Expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	c, _, _ := s.SignIn(ctx, googleID("g-c"), "", closedRules)
	if res, err := s.Link(ctx, c.ID, telegramID(2), closedRules); err != nil || res != Merged {
		t.Fatalf("merge = %v %v", res, err)
	}
	if _, ok, _ := s.AccountByID(ctx, b.ID); ok {
		t.Fatal("B is still there after the merge")
	}
	if _, ok, _ := s.LookupSession(ctx, [32]byte{2}, time.Now()); ok {
		t.Fatal("B's session outlived B")
	}
	merged, _, _ := s.AccountByID(ctx, c.ID)
	if merged.TelegramUserID != 2 || merged.GoogleSub != "g-c" || merged.Player != guestB {
		t.Fatalf("C after the merge = %+v", merged)
	}
	if players, _ := s.AccountPlayers(ctx, c.ID); len(players) != 1 || players[0] != guestB {
		t.Fatalf("C's players = %v", players)
	}

	// D has Telegram 3 and Google: its Telegram is not E's to take.
	d, _, _ := s.SignIn(ctx, telegramID(3), "", closedRules)
	if res, err := s.Link(ctx, d.ID, googleID("g-d"), closedRules); err != nil || res != Linked {
		t.Fatalf("link Google to D = %v %v", res, err)
	}
	e, _, _ := s.SignIn(ctx, googleID("g-e"), "", closedRules)
	if res, err := s.Link(ctx, e.ID, telegramID(3), closedRules); err != nil || res != Taken {
		t.Fatalf("D's Telegram for E = %v %v", res, err)
	}
	if res, err := s.Link(ctx, e.ID, googleID("g-d"), closedRules); err != nil || res != AlreadyLinked {
		t.Fatalf("a second Google = %v %v", res, err)
	}
	if res, err := s.Link(ctx, newUUID(), googleID("g-x"), closedRules); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("a link to a gone account = %v %v", res, err)
	}
}

func TestUnlinkKeepsTheLastSignIn(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 1, "Anya")
	a, _, _ := s.SignIn(ctx, googleID("g-a"), "", closedRules)
	if err := s.Unlink(ctx, a.ID, KindGoogle); !errors.Is(err, ErrLastIdentity) {
		t.Fatalf("unlink the only sign-in = %v", err)
	}
	if err := s.Unlink(ctx, a.ID, KindTelegram); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("unlink what is not there = %v", err)
	}
	if _, err := s.Link(ctx, a.ID, telegramID(1), closedRules); err != nil {
		t.Fatal(err)
	}
	if err := s.Unlink(ctx, a.ID, KindGoogle); err != nil {
		t.Fatal(err)
	}
	if acc, _, _ := s.AccountByID(ctx, a.ID); acc.GoogleSub != "" || acc.TelegramUserID != 1 {
		t.Fatalf("after unlinking Google = %+v", acc)
	}
	// The table holds it too, whatever the code does.
	rejects(t, s, checkViolation, `UPDATE maply.account SET telegram_user_id = NULL WHERE id = $1`, a.ID)
}

func TestSettleKeepsOneAnswerInAnOpenDay(t *testing.T) {
	ctx := context.Background()
	deadline := time.Date(2026, time.November, 1, 17, 0, 0, 0, time.UTC)

	t.Run("a closed day is never rewritten", func(t *testing.T) {
		s := testStore(t)
		seedRound(t, s)
		person(t, s, 1, "Anya")
		siteAnswer(t, s, guestA, 9, 0)
		siteAnswer(t, s, guestB, 8, 0)
		a, _, _ := s.SignIn(ctx, googleID("g-a"), guestA, closedRules)
		if _, err := s.AdoptGuest(ctx, a.ID, guestB, closedRules); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Link(ctx, a.ID, telegramID(1), closedRules); err != nil {
			t.Fatal(err)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.site_guess`); n != 2 {
			t.Fatalf("%d site answers left in a closed day, want both", n)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.guess`); n != 0 {
			t.Fatalf("a closed day gained a Telegram answer")
		}
		// day_closed closes it as well, whatever the rules say.
		exec(t, s, `INSERT INTO maply.day_closed (date) VALUES ('2026-11-01')`)
		if _, err := s.AdoptGuest(ctx, a.ID, guestC, openRules(deadline)); err != nil {
			t.Fatal(err)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.site_guess`); n != 2 {
			t.Fatalf("a day in day_closed was rewritten")
		}
	})

	t.Run("the earliest site answer stays", func(t *testing.T) {
		s := testStore(t)
		seedRound(t, s)
		siteAnswer(t, s, guestA, 9, 0)
		siteAnswer(t, s, guestB, 8, 0)
		a, _, _ := s.SignIn(ctx, googleID("g-a"), guestA, openRules(deadline))
		if n, err := s.AdoptGuest(ctx, a.ID, guestB, openRules(deadline)); err != nil || n != 1 {
			t.Fatalf("adopt = %d %v", n, err)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.site_guess WHERE player = $1`, guestB); n != 1 {
			t.Fatal("the earlier answer, B's, is gone")
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.site_guess`); n != 1 {
			t.Fatal("two answers of one account in an open day")
		}
	})

	t.Run("an answer before the deadline becomes the Telegram answer", func(t *testing.T) {
		s := testStore(t)
		seedRound(t, s)
		person(t, s, 1, "Anya")
		siteAnswer(t, s, guestA, 16, 59)
		a, _, _ := s.SignIn(ctx, googleID("g-a"), guestA, openRules(deadline))
		if res, err := s.Link(ctx, a.ID, telegramID(1), openRules(deadline)); err != nil || res != Linked {
			t.Fatalf("link = %v %v", res, err)
		}
		var context string
		var dl time.Time
		if err := s.pool.QueryRow(ctx, `SELECT context, deadline_at FROM maply.guess WHERE user_id = 1`).Scan(&context, &dl); err != nil {
			t.Fatal(err)
		}
		if context != "site" || !dl.Equal(deadline) {
			t.Fatalf("moved answer: context %s, deadline %v", context, dl)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.site_guess`); n != 0 {
			t.Fatal("the site row stayed beside the Telegram one")
		}
	})

	t.Run("a Telegram answer already there wins", func(t *testing.T) {
		s := testStore(t)
		seedRound(t, s)
		person(t, s, 1, "Anya")
		exec(t, s, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
		            VALUES ('2026-11-01', 1, 'dm', 1, 2, '2026-11-01 12:00Z', '2026-11-01 17:00Z')`)
		siteAnswer(t, s, guestA, 8, 0)
		a, _, _ := s.SignIn(ctx, googleID("g-a"), guestA, openRules(deadline))
		if _, err := s.Link(ctx, a.ID, telegramID(1), openRules(deadline)); err != nil {
			t.Fatal(err)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.guess WHERE user_id = 1 AND context = 'dm' AND lat = 1`); n != 1 {
			t.Fatal("the Telegram answer changed")
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.site_guess`); n != 0 {
			t.Fatal("the site answer stayed beside the Telegram one")
		}
	})

	// After the deadline the person may have seen the place: a guest's
	// answer from before it, picked then, would be a Telegram answer
	// chosen knowing where the place is.
	t.Run("an answer merged after the deadline stays a site row", func(t *testing.T) {
		s := testStore(t)
		seedRound(t, s)
		person(t, s, 1, "Anya")
		siteAnswer(t, s, guestA, 9, 0)
		late := openRules(deadline)
		late.Now = deadline.Add(5 * time.Minute)
		a, _, err := s.SignIn(ctx, telegramID(1), guestA, late)
		if err != nil || a.TelegramUserID != 1 {
			t.Fatalf("account = %+v %v", a, err)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.guess`); n != 0 {
			t.Fatal("a guest's answer became a Telegram one after the deadline")
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.site_guess WHERE player = $1`, guestA); n != 1 {
			t.Fatal("the site answer is gone")
		}
		// Adopting another guest later is no different.
		siteAnswer(t, s, guestB, 8, 0)
		if _, err := s.AdoptGuest(ctx, a.ID, guestB, late); err != nil {
			t.Fatal(err)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.guess`); n != 0 {
			t.Fatal("an adopted guest's answer became a Telegram one after the deadline")
		}
	})

	t.Run("an answer after the deadline stays a site row", func(t *testing.T) {
		s := testStore(t)
		seedRound(t, s)
		person(t, s, 1, "Anya")
		siteAnswer(t, s, guestA, 17, 30)
		a, _, _ := s.SignIn(ctx, telegramID(1), guestA, openRules(deadline))
		if a.TelegramUserID != 1 {
			t.Fatalf("account = %+v", a)
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.site_guess`); n != 1 {
			t.Fatal("a late site answer was dropped")
		}
		if n := countOf(t, s, `SELECT count(*) FROM maply.guess`); n != 0 {
			t.Fatal("a late site answer became a Telegram one")
		}
	})
}

func TestDeleteAccountLeavesGuests(t *testing.T) {
	s := testStore(t)
	seedRound(t, s)
	ctx := context.Background()
	person(t, s, 1, "Anya")
	siteAnswer(t, s, guestA, 9, 0)
	a, _, _ := s.SignIn(ctx, googleID("g-a"), guestA, closedRules)
	if _, err := s.AccountSitePlayer(ctx, a.ID, guestC, ptr("Anya")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(ctx, a.ID, telegramID(1), closedRules); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.CreateSession(ctx, Session{Hash: [32]byte{1}, AccountID: a.ID, Via: KindGoogle, Created: now, Expires: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTicket(ctx, [32]byte{9}, a.ID, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := s.AccountView(ctx, a.ID); !ok || v.Name != "Anya" || v.TelegramName != "Anya" {
		t.Fatalf("view = %+v", v)
	}
	if err := s.DeleteAccount(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, a.ID); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("delete twice = %v", err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM maply.account`:                                               0,
		`SELECT count(*) FROM maply.session`:                                               0,
		`SELECT count(*) FROM maply.link_ticket`:                                           0,
		`SELECT count(*) FROM maply.site_player WHERE account_id IS NULL AND name IS NULL`: 1,
		`SELECT count(*) FROM maply.site_guess`:                                            1,
		`SELECT count(*) FROM core.person WHERE telegram_user_id = 1`:                      1,
	} {
		if n := countOf(t, s, q); n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}
}

func TestSessionsAndTickets(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _, _ := s.SignIn(ctx, googleID("g-a"), "", closedRules)
	now := time.Now()
	for i, via := range []string{KindGoogle, KindGoogle, KindTelegram} {
		if err := s.CreateSession(ctx, Session{Hash: [32]byte{byte(i + 1)}, AccountID: a.ID, Via: via, Created: now, Expires: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if sa, ok, err := s.LookupSession(ctx, [32]byte{1}, now); err != nil || !ok || sa.AccountID != a.ID || sa.Account.GoogleSub != "g-a" || sa.Via != KindGoogle {
		t.Fatalf("lookup = %+v %v %v", sa, ok, err)
	}
	if _, ok, _ := s.LookupSession(ctx, [32]byte{1}, now.Add(2*time.Hour)); ok {
		t.Fatal("an expired session opened")
	}
	if n, err := s.DeleteSessions(ctx, a.ID, KindGoogle); err != nil || n != 2 {
		t.Fatalf("sign out of Google's sessions = %d %v", n, err)
	}
	if _, ok, _ := s.LookupSession(ctx, [32]byte{3}, now); !ok {
		t.Fatal("the Telegram session went with Google's")
	}
	if err := s.DeleteExpired(ctx, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := countOf(t, s, `SELECT count(*) FROM maply.session`); n != 0 {
		t.Fatal("the sweep left an expired session")
	}

	if err := s.CreateTicket(ctx, [32]byte{7}, a.ID, now, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := s.PeekTicket(ctx, [32]byte{7}, now); err != nil || !ok || id != a.ID {
		t.Fatalf("peek = %s %v %v", id, ok, err)
	}
	if _, ok, _ := s.PeekTicket(ctx, [32]byte{7}, now.Add(6*time.Minute)); ok {
		t.Fatal("an expired ticket is still shown")
	}
	// Good once, also when two browsers race for it.
	var wg sync.WaitGroup
	var mu sync.Mutex
	used := 0
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok, err := s.UseTicket(ctx, [32]byte{7}, now); err == nil && ok {
				mu.Lock()
				used++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if used != 1 {
		t.Fatalf("the ticket was used %d times", used)
	}
	if _, ok, _ := s.PeekTicket(ctx, [32]byte{7}, now); ok {
		t.Fatal("a used ticket is still shown")
	}
}

// An account with Telegram answers as its Telegram player: a site answer
// for it is refused, held under the account's row against a link at the
// same moment.
func TestAnAccountWithTelegramGivesNoSiteAnswer(t *testing.T) {
	s := testStore(t)
	seedRound(t, s)
	ctx := context.Background()
	person(t, s, 1, "Anya")
	a, _, err := s.SignIn(ctx, googleID("g-a"), "", closedRules)
	if err != nil {
		t.Fatal(err)
	}
	player, err := s.AccountSitePlayer(ctx, a.ID, guestA, nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC)
	g := NewSiteGuess{Date: accountDay, Player: player, AnsweredAt: at, Deadline: at.Add(20 * time.Hour)}
	if _, err := s.Link(ctx, a.ID, telegramID(1), closedRules); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAccountSiteGuess(ctx, a.ID, g); !errors.Is(err, ErrTelegramAccount) {
		t.Fatalf("a site answer for a Telegram account = %v", err)
	}
	// Unlinked, it plays the site's game again, once.
	if err := s.Unlink(ctx, a.ID, KindTelegram); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAccountSiteGuess(ctx, a.ID, g); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAccountSiteGuess(ctx, a.ID, g); !errors.Is(err, ErrAlreadyAnswered) {
		t.Fatalf("a second site answer = %v", err)
	}
}

// Unlinking ends the sessions the sign-in opened and the account's tickets
// in one go.
func TestUnlinkEndsItsSessionsAndTickets(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 1, "Anya")
	a, _, _ := s.SignIn(ctx, telegramID(1), "", closedRules)
	if _, err := s.Link(ctx, a.ID, googleID("g-a"), closedRules); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, via := range []string{KindGoogle, KindTelegram} {
		if err := s.CreateSession(ctx, Session{Hash: [32]byte{byte(i + 1)}, AccountID: a.ID, Via: via, Created: now, Expires: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateTicket(ctx, [32]byte{9}, a.ID, now, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if held, err := s.TicketHeld(ctx, [32]byte{9}, a.ID); err != nil || !held {
		t.Fatalf("held = %v %v", held, err)
	}
	if err := s.Unlink(ctx, a.ID, KindGoogle); err != nil {
		t.Fatal(err)
	}
	if n := countOf(t, s, `SELECT count(*) FROM maply.session WHERE via = 'google'`); n != 0 {
		t.Fatal("a session of the unlinked Google lives")
	}
	if n := countOf(t, s, `SELECT count(*) FROM maply.session WHERE via = 'telegram'`); n != 1 {
		t.Fatal("the Telegram session went too")
	}
	if held, _ := s.TicketHeld(ctx, [32]byte{9}, a.ID); held {
		t.Fatal("the ticket outlived the unlink")
	}
}
