// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"context"
	"testing"
)

// The profile reads free play's results by contract 5: the columns of
// free_result as migration 019 of the free game makes them. Until that
// migration is here, the test makes a table with those columns.
func TestFreeGamesReadFreeResult(t *testing.T) {
	store := testStore(t)
	seedRound(t, store)
	ctx := context.Background()
	if games, err := store.FreeGames(ctx, 7); err != nil || games != nil {
		t.Fatalf("before free play: %v, %v", games, err)
	}
	var exists bool
	if err := store.pool.QueryRow(ctx, `SELECT to_regclass('free_result') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Skip("free_result is the free game's own now; its tests cover it")
	}
	exec(t, store, `CREATE TABLE maply.free_result (
	  game BIGINT, m SMALLINT, user_id BIGINT NOT NULL, kind TEXT NOT NULL, mode TEXT NOT NULL,
	  seconds SMALLINT, rounds SMALLINT, points INTEGER NOT NULL, points_base INTEGER NOT NULL,
	  km DOUBLE PRECISION NOT NULL, km_rounds DOUBLE PRECISION[] NOT NULL, time_ms BIGINT NOT NULL,
	  played SMALLINT NOT NULL, answered SMALLINT NOT NULL, countries SMALLINT NOT NULL, from_round SMALLINT NOT NULL,
	  place SMALLINT, players SMALLINT, beaten SMALLINT, streak SMALLINT, finished_at TIMESTAMPTZ NOT NULL)`)
	t.Cleanup(func() { exec(t, store, `DROP TABLE maply.free_result`) })
	exec(t, store, `INSERT INTO maply.free_result (game, m, user_id, kind, mode, seconds, rounds, points, points_base, km, km_rounds,
	                                              time_ms, played, answered, countries, from_round, place, players, beaten, streak, finished_at)
	                VALUES (1, 1, 7, 'duel', 'classic', 30, 3, 9000, 8000, 20400, '{100,300}', 50000, 3, 2, 1, 1, 1, 2, 1, NULL, '2026-11-01 10:00Z'),
	                       (2, 1, 7, 'solo', 'streak', NULL, NULL, 4000, 3800, 900, '{900}', 20000, 2, 1, 1, 1, NULL, NULL, NULL, 1, '2026-11-02 10:00Z'),
	                       (3, 1, 8, 'solo', 'classic', NULL, 5, 1, 1, 1, '{}', 1, 5, 0, 0, 1, NULL, NULL, NULL, NULL, '2026-11-02 10:00Z')`)
	games, err := store.FreeGames(ctx, 7)
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
