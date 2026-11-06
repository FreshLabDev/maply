// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

// The people someone played free play with: a party's, and a challenge's
// maker and everyone who took it, a site player as its account. A solo game
// is with no one.
func TestPlayedWith(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const (
		acc    = "55555555-5555-4555-8555-555555555555"
		player = "66666666-6666-4666-8666-666666666666"
		guest  = "77777777-7777-4777-8777-777777777777"
	)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := store.pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`TRUNCATE maply.free_game CASCADE`)
	t.Cleanup(func() { store.pool.Exec(context.Background(), `TRUNCATE maply.free_game CASCADE`) })
	exec(`INSERT INTO maply.site_player (id, name) VALUES ($1, 'Marta'), ($2, 'Guest')`, player, guest)
	exec(`INSERT INTO maply.account (id, google_sub, player) VALUES ($1, 'g-marta', $2)`, acc, player)
	exec(`UPDATE maply.site_player SET account_id = $1 WHERE id = $2`, acc, player)

	parties := 0
	game := func(kind string, members ...any) int64 {
		t.Helper()
		var id int64
		var code, seconds, started any
		if kind != "solo" {
			parties++
			code, seconds, started = fmt.Sprintf("PARTY%03d", parties), 30, "2026-10-01T10:00:00Z"
		}
		if err := store.pool.QueryRow(ctx, `
			INSERT INTO maply.free_game (code, kind, mode, seconds, rounds, status, created_at, started_at, finished_at)
			VALUES ($1, $2, 'classic', $3, 5, 'done', '2026-10-01T09:00:00Z', $4, '2026-10-01T11:00:00Z') RETURNING id`,
			code, kind, seconds, started).Scan(&id); err != nil {
			t.Fatal(err)
		}
		for i, m := range members {
			var user, pl any
			switch w := m.(type) {
			case int:
				user = int64(w)
			case string:
				pl = w
			}
			exec(`INSERT INTO maply.free_member (game, m, user_id, player, name, joined_at, last_seen_at)
			      VALUES ($1, $2, $3, $4, 'x', now(), now())`, id, i+1, user, pl)
		}
		return id
	}
	made := game("solo", 10)
	exec(`INSERT INTO maply.free_challenge (code, game, created_at) VALUES ('CHAMP001', $1, now())`, made)
	for _, who := range []any{11, player, guest} {
		g := game("solo", who)
		var user, pl any
		if id, ok := who.(int); ok {
			user = int64(id)
		} else {
			pl = who
		}
		exec(`INSERT INTO maply.free_attempt (challenge, user_id, player, game)
		      SELECT id, $1, $2, $3 FROM maply.free_challenge WHERE code = 'CHAMP001'`, user, pl, g)
	}
	game("party", 12, 13)
	game("solo", 14)

	check := func(who FreeWho, want string) {
		t.Helper()
		users, accounts, err := store.PlayedWith(ctx, who)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(users)
		slices.Sort(accounts)
		if got := fmt.Sprint(users, accounts); got != want {
			t.Fatalf("played with %+v: %s, want %s", who, got, want)
		}
	}
	check(FreeWho{UserID: 10}, "[10 11] ["+acc+"]")
	check(FreeWho{UserID: 11}, "[10 11] ["+acc+"]")
	check(FreeWho{Account: acc}, "[10 11] ["+acc+"]")
	check(FreeWho{UserID: 12}, "[12 13] []")
	check(FreeWho{UserID: 14}, "[14] []")
}
