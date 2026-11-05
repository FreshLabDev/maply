-- SPDX-License-Identifier: Apache-2.0
-- More of the free game: a missed day played from the archive, a game against
-- the clock, and a challenge, where friends play the places of one game each
-- in their own time.
--
-- mode 'archive' is one closed day's place without a timer. mode 'timed' has
-- 180 seconds for the whole game and no number of places. kind 'challenge' is
-- one person's attempt at a challenge: played alone, as a solo game is, on the
-- places of the game the challenge was made from.

-- 019's checks have the names Postgres gave them; they go, and come back
-- whole and named below.
DO $$
DECLARE c record;
BEGIN
  FOR c IN SELECT conname, conrelid::regclass AS tbl FROM pg_constraint
            WHERE contype = 'c' AND conrelid IN ('free_game'::regclass, 'free_result'::regclass)
              AND conname NOT IN ('free_game_code_check', 'free_game_status_check', 'free_game_version_check')
  LOOP
    EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', c.tbl, c.conname);
  END LOOP;
END $$;

ALTER TABLE free_game
  ADD CONSTRAINT free_game_kind CHECK (kind IN ('solo', 'party', 'duel', 'challenge')),
  ADD CONSTRAINT free_game_mode CHECK (mode IN ('classic', 'streak', 'timed', 'archive')),
  ADD CONSTRAINT free_game_seconds CHECK (seconds IN (30, 60, 90, 120, 180)),
  ADD CONSTRAINT free_game_rounds CHECK (rounds IN (1, 3, 5, 10)),
  -- Only a solo classic and a day from the archive go without a timer.
  ADD CONSTRAINT free_game_timer CHECK (seconds IS NOT NULL OR (kind = 'solo' AND mode IN ('classic', 'archive'))),
  ADD CONSTRAINT free_game_endless CHECK ((mode IN ('streak', 'timed')) = (rounds IS NULL)),
  ADD CONSTRAINT free_game_streak CHECK (mode <> 'streak' OR (kind = 'solo' AND seconds IN (30, 60))),
  ADD CONSTRAINT free_game_timed CHECK ((mode = 'timed') = (seconds IS NOT DISTINCT FROM 180) AND (mode <> 'timed' OR kind = 'solo')),
  ADD CONSTRAINT free_game_archive CHECK ((mode = 'archive') = (rounds IS NOT DISTINCT FROM 1)
                                          AND (mode <> 'archive' OR (kind = 'solo' AND seconds IS NULL))),
  -- An attempt plays the places of a classic on a timer: without one, a
  -- friend could look the places up at leisure.
  ADD CONSTRAINT free_game_attempt CHECK (kind <> 'challenge' OR (mode = 'classic' AND seconds IS NOT NULL)),
  ADD CONSTRAINT free_game_code_kind CHECK ((kind IN ('party', 'duel')) = (code IS NOT NULL)),
  ADD CONSTRAINT free_game_started CHECK (status IN ('lobby', 'abandoned') OR kind IN ('solo', 'challenge') OR started_at IS NOT NULL);

ALTER TABLE free_result
  ADD CONSTRAINT free_result_kind CHECK (kind IN ('solo', 'party', 'duel', 'challenge')),
  ADD CONSTRAINT free_result_mode CHECK (mode IN ('classic', 'streak', 'timed', 'archive')),
  ADD CONSTRAINT free_result_streak CHECK ((mode = 'streak') = (streak IS NOT NULL)),
  ADD CONSTRAINT free_result_place CHECK ((kind IN ('party', 'duel')) = (place IS NOT NULL));

-- A day from the archive: once per person and day, and only a day over in
-- every zone that the person did not answer. The game is written with its
-- one place, so the place counts as seen from the moment it is taken.
CREATE TABLE IF NOT EXISTS free_archive (
  game    BIGINT PRIMARY KEY REFERENCES free_game(id) ON DELETE CASCADE,
  date    DATE NOT NULL REFERENCES round(date),
  user_id BIGINT,
  player  UUID,
  CHECK ((user_id IS NULL) <> (player IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS free_archive_user_idx ON free_archive (user_id, date) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS free_archive_player_idx ON free_archive (player, date) WHERE player IS NOT NULL;

-- A challenge is made from a finished solo classic on a timer, by its player.
-- Everyone who takes it plays those places in that order on those settings,
-- once: an attempt is a game of its own, with its own clock.
CREATE TABLE IF NOT EXISTS free_challenge (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code       TEXT NOT NULL UNIQUE CHECK (code ~ '^[0-9A-HJKMNP-TV-Z]{8}$'),
  game       BIGINT NOT NULL UNIQUE REFERENCES free_game(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL
);

-- Who has played a challenge, and in which game: the maker's is the game the
-- challenge was made from.
CREATE TABLE IF NOT EXISTS free_attempt (
  challenge BIGINT NOT NULL REFERENCES free_challenge(id) ON DELETE CASCADE,
  user_id   BIGINT NOT NULL,
  game      BIGINT NOT NULL UNIQUE REFERENCES free_game(id) ON DELETE CASCADE,
  PRIMARY KEY (challenge, user_id)
);
CREATE INDEX IF NOT EXISTS free_attempt_user_idx ON free_attempt (user_id);
