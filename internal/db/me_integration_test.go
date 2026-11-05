// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"testing"
)

// The profile reads free play's results by contract 5: the columns of
// free_result as migration 019 makes them.
func TestFreeGamesReadFreeResult(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	// The store's own tests leave free play alone: this one starts and ends
	// without it.
	exec(t, store, `TRUNCATE maply.free_game CASCADE`)
	t.Cleanup(func() { exec(t, store, `TRUNCATE maply.free_game CASCADE`) })
	// free-game's migration 024 gives free_result its player; until it
	// lands, the test does.
	exec(t, store, `ALTER TABLE maply.free_result ADD COLUMN IF NOT EXISTS player UUID REFERENCES maply.site_player(id) ON DELETE CASCADE`)
	exec(t, store, `ALTER TABLE maply.free_result ALTER COLUMN user_id DROP NOT NULL`)
	if games, err := store.FreeGames(ctx, FreeWho{UserID: 7}); err != nil || len(games) != 0 {
		t.Fatalf("before free play: %v, %v", games, err)
	}
	exec(t, store, `INSERT INTO maply.free_game (id, code, kind, mode, seconds, rounds, status, created_at, started_at, finished_at)
	                OVERRIDING SYSTEM VALUE
	                VALUES (1, 'ABCDEFGH', 'duel', 'classic', 30, 3, 'done', '2026-11-01 09:50Z', '2026-11-01 09:55Z', '2026-11-01 10:00Z'),
	                       (2, NULL, 'solo', 'streak', 30, NULL, 'done', '2026-11-02 09:50Z', NULL, '2026-11-02 10:00Z'),
	                       (3, NULL, 'solo', 'classic', NULL, 5, 'done', '2026-11-02 09:50Z', NULL, '2026-11-02 10:00Z')`)
	exec(t, store, `INSERT INTO maply.free_member (game, m, user_id, name, joined_at, last_seen_at)
	                VALUES (1, 1, 7, 'Pin', '2026-11-01 09:50Z', '2026-11-01 10:00Z'),
	                       (1, 2, 8, 'Ana', '2026-11-01 09:51Z', '2026-11-01 10:00Z'),
	                       (2, 1, 7, 'Pin', '2026-11-02 09:50Z', '2026-11-02 10:00Z'),
	                       (3, 1, 8, 'Ana', '2026-11-02 09:50Z', '2026-11-02 10:00Z')`)
	exec(t, store, `INSERT INTO maply.free_result (game, m, user_id, kind, mode, seconds, rounds, points, points_base, km, km_rounds,
	                                              time_ms, played, answered, countries, from_round, place, players, beaten, streak, finished_at)
	                VALUES (1, 1, 7, 'duel', 'classic', 30, 3, 9000, 8000, 20400, '{100,300}', 50000, 3, 2, 1, 1, 1, 2, 1, NULL, '2026-11-01 10:00Z'),
	                       (2, 1, 7, 'solo', 'streak', NULL, NULL, 4000, 3800, 900, '{900}', 20000, 2, 1, 1, 1, NULL, NULL, NULL, 1, '2026-11-02 10:00Z'),
	                       (3, 1, 8, 'solo', 'classic', NULL, 5, 1, 1, 1, '{}', 1, 5, 0, 0, 1, NULL, NULL, NULL, NULL, '2026-11-02 10:00Z')`)
	games, err := store.FreeGames(ctx, FreeWho{UserID: 7})
	if err != nil || len(games) != 2 {
		t.Fatalf("games %+v, %v", games, err)
	}
	d, s := games[0], games[1]
	if d.Kind != "duel" || d.Seconds != 30 || d.Rounds != 3 || d.Played != 3 || d.Base != 8000 || len(d.KmRounds) != 2 || d.Beaten != 1 || d.Place != 1 || d.Players != 2 {
		t.Fatalf("duel %+v", d)
	}
	if s.Mode != "streak" || s.Seconds != 0 || s.Rounds != 0 || s.Streak != 1 || s.Place != 0 {
		t.Fatalf("streak %+v", s)
	}
}
