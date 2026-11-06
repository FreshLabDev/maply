-- SPDX-License-Identifier: Apache-2.0
-- Every mode of the free game is played with friends: a country streak, a game
-- against the clock and a day from the archive, not the classic alone.
-- 020 pinned each of them to one player; the checks go and come back for
-- anyone but a challenge's attempt, which free_game_attempt keeps to a classic.
-- free_game_timer stays as it is: a party has a timer in every mode already.
--
-- A day from the archive played together is a party with its day on the game:
-- free_archive holds one person's game of a day, and a party has several.

ALTER TABLE free_game ADD COLUMN IF NOT EXISTS day DATE REFERENCES round(date);

ALTER TABLE free_game
  DROP CONSTRAINT IF EXISTS free_game_streak,
  DROP CONSTRAINT IF EXISTS free_game_timed,
  DROP CONSTRAINT IF EXISTS free_game_archive;

ALTER TABLE free_game
  ADD CONSTRAINT free_game_streak CHECK (mode <> 'streak' OR seconds IN (30, 60)),
  ADD CONSTRAINT free_game_timed CHECK ((mode = 'timed') = (seconds IS NOT DISTINCT FROM 180)),
  -- One place. A solo day has no timer; a party on a day has the host's pick
  -- of 60, 90 or 120 seconds, never none: one player who walked away would
  -- hold everyone.
  ADD CONSTRAINT free_game_archive CHECK ((mode = 'archive') = (rounds IS NOT DISTINCT FROM 1)
                                          AND (mode <> 'archive'
                                               OR (kind = 'solo' AND seconds IS NULL)
                                               OR (kind IN ('party', 'duel') AND seconds IN (60, 90, 120)))),
  -- The day is on the game of a party that plays one, and only there.
  ADD CONSTRAINT free_game_day CHECK ((mode = 'archive' AND kind IN ('party', 'duel')) = (day IS NOT NULL));
