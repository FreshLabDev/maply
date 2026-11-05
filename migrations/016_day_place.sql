-- SPDX-License-Identifier: Apache-2.0
-- A day over in every zone no longer changes, so its ranking is kept once.
-- day_rank ranks a whole day even when one person's place is asked for, and
-- the profile asked for every day there has been, for every recent player,
-- every ten minutes. The final world job keeps a day once it has scored it.
--
-- An answer removed by hand from a kept day: delete the day from day_closed
-- (its places go with it), and the next final pass or read ranks it anew.
CREATE TABLE IF NOT EXISTS day_closed (
  date    DATE PRIMARY KEY,
  kept_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS day_place (
  date    DATE NOT NULL REFERENCES day_closed(date) ON DELETE CASCADE,
  user_id BIGINT,
  player  UUID,
  km      DOUBLE PRECISION NOT NULL,
  points  INTEGER NOT NULL,
  rank    INTEGER NOT NULL,
  n       INTEGER NOT NULL,
  CHECK ((user_id IS NULL) <> (player IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS day_place_user_idx ON day_place (user_id, date) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS day_place_player_idx ON day_place (player, date) WHERE player IS NOT NULL;
CREATE INDEX IF NOT EXISTS day_place_date_idx ON day_place (date);

-- The days before this are over everywhere two days on in UTC: the last
-- reveal of a date is in the morning of the next one.
WITH kept AS (
  INSERT INTO day_closed (date)
  SELECT date FROM round WHERE date <= (now() AT TIME ZONE 'UTC')::date - 2
  ON CONFLICT DO NOTHING
  RETURNING date)
INSERT INTO day_place (date, user_id, player, km, points, rank, n)
SELECT r.date, r.user_id, r.player, r.km, r.points, r.rank, r.n
  FROM day_rank r JOIN kept k ON k.date = r.date;
