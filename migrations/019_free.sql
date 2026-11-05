-- SPDX-License-Identifier: Apache-2.0
-- The free game: alone or with friends, as often as one likes, outside the
-- day's ranking. Its tables are its own, so the ranked statistics never read
-- a free answer by mistake: they are apart by construction, not by a filter.

-- A place is for the days or for the free game. The day's place is picked
-- from 'daily' only, so the queue of future days does not change by a row,
-- and the free game never shows a future day's place. A place that may have
-- been shown in the free game never becomes a day's place, and a day's place
-- that has been played stays the day's.
ALTER TABLE place ADD COLUMN IF NOT EXISTS pool TEXT NOT NULL DEFAULT 'daily'
  CHECK (pool IN ('daily', 'free'));

CREATE OR REPLACE FUNCTION place_pool_kept() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.pool IS DISTINCT FROM OLD.pool AND (OLD.pool = 'free' OR OLD.status = 'used') THEN
    RAISE EXCEPTION 'place %: pool % cannot change', OLD.id, OLD.pool;
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS place_pool_kept ON place;
CREATE TRIGGER place_pool_kept BEFORE UPDATE OF pool ON place
  FOR EACH ROW EXECUTE FUNCTION place_pool_kept();

CREATE OR REPLACE FUNCTION round_place_daily() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM place WHERE id = NEW.place_id AND pool = 'daily') THEN
    RAISE EXCEPTION 'place % is not a day''s place', NEW.place_id;
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS round_place_daily ON round;
CREATE TRIGGER round_place_daily BEFORE INSERT OR UPDATE OF place_id ON round
  FOR EACH ROW EXECUTE FUNCTION round_place_daily();

DROP INDEX IF EXISTS place_queue_idx;
CREATE INDEX IF NOT EXISTS place_daily_queue_idx ON place (difficulty, id)
  WHERE status = 'approved' AND pool = 'daily';
CREATE INDEX IF NOT EXISTS place_free_idx ON place (id)
  WHERE status = 'approved' AND pool = 'free';

-- A game. kind is who plays: one person, or a party, which is a duel when two
-- started it. mode is the rules. seconds is the time for a place, NULL for no
-- timer, which only a solo classic has; rounds is NULL for a streak, which
-- goes on while the country is right. version is the settings' version: a
-- start names the one it was shown, so a party never begins on settings
-- nobody saw. A party's code is the invitation's.
CREATE TABLE IF NOT EXISTS free_game (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code        TEXT UNIQUE CHECK (code ~ '^[0-9A-HJKMNP-TV-Z]{8}$'),
  kind        TEXT NOT NULL CHECK (kind IN ('solo', 'party', 'duel')),
  mode        TEXT NOT NULL CHECK (mode IN ('classic', 'streak')),
  seconds     SMALLINT CHECK (seconds IN (30, 60, 90, 120)),
  rounds      SMALLINT CHECK (rounds IN (3, 5, 10)),
  host        SMALLINT NOT NULL DEFAULT 1,
  status      TEXT NOT NULL CHECK (status IN ('lobby', 'playing', 'done', 'abandoned')),
  version     INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  next_id     BIGINT REFERENCES free_game(id),
  created_at  TIMESTAMPTZ NOT NULL,
  started_at  TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  CHECK (seconds IS NOT NULL OR (kind = 'solo' AND mode = 'classic')),
  CHECK ((mode = 'streak') = (rounds IS NULL)),
  CHECK (mode <> 'streak' OR (kind = 'solo' AND seconds IN (30, 60))),
  CHECK ((kind = 'solo') = (code IS NULL)),
  CHECK (status IN ('lobby', 'abandoned') OR kind = 'solo' OR started_at IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS free_game_open_idx ON free_game (status, created_at)
  WHERE status IN ('lobby', 'playing');

-- Who plays a game: a Telegram person, or a site player playing alone. name is
-- the first name as the launch gave it, so a party shows its players without
-- reading core. from_round is the first place a late player plays.
CREATE TABLE IF NOT EXISTS free_member (
  game         BIGINT NOT NULL REFERENCES free_game(id) ON DELETE CASCADE,
  -- At most eight are in a party at once; a number is never handed out
  -- twice, so someone who left and the one who took their seat stay apart.
  m            SMALLINT NOT NULL CHECK (m BETWEEN 1 AND 32),
  user_id      BIGINT,
  player       UUID,
  name         TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
  joined_at    TIMESTAMPTZ NOT NULL,
  from_round   SMALLINT NOT NULL DEFAULT 1 CHECK (from_round >= 1),
  last_seen_at TIMESTAMPTZ NOT NULL,
  left_at      TIMESTAMPTZ,
  PRIMARY KEY (game, m),
  CHECK ((user_id IS NULL) <> (player IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS free_member_user_idx ON free_member (user_id, game) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS free_member_player_idx ON free_member (player, game) WHERE player IS NOT NULL;

-- A place of a game. A solo round is written when the player starts it, and
-- that is when its place counts as seen; a party's next round is written when
-- the one before closes, with the moment it begins. ends_at is NULL without a
-- timer. repeat marks a place one of the players has seen before.
CREATE TABLE IF NOT EXISTS free_round (
  game      BIGINT NOT NULL REFERENCES free_game(id) ON DELETE CASCADE,
  n         SMALLINT NOT NULL CHECK (n >= 1),
  place_id  BIGINT NOT NULL REFERENCES place(id),
  repeat    BOOLEAN NOT NULL DEFAULT false,
  starts_at TIMESTAMPTZ NOT NULL,
  ends_at   TIMESTAMPTZ,
  closed_at TIMESTAMPTZ,
  PRIMARY KEY (game, n),
  UNIQUE (game, place_id),
  CHECK (ends_at IS NULL OR ends_at > starts_at)
);
CREATE INDEX IF NOT EXISTS free_round_place_idx ON free_round (place_id);

-- One answer per player and place, scored when it comes in by the judge of
-- the day's game. No pin is a place given up or run out of time: 0 points.
-- auto is a pin the app sent itself at zero; it gets no speed bonus.
CREATE TABLE IF NOT EXISTS free_guess (
  game        BIGINT NOT NULL,
  n           SMALLINT NOT NULL,
  m           SMALLINT NOT NULL,
  lat         DOUBLE PRECISION CHECK (lat BETWEEN -90 AND 90),
  lon         DOUBLE PRECISION CHECK (lon BETWEEN -180 AND 180),
  answered_at TIMESTAMPTZ NOT NULL,
  auto        BOOLEAN NOT NULL DEFAULT false,
  km          DOUBLE PRECISION CHECK (km >= 0),
  pts_km      INTEGER NOT NULL DEFAULT 0 CHECK (pts_km BETWEEN 0 AND 3500),
  pts_bonus   INTEGER NOT NULL DEFAULT 0 CHECK (pts_bonus IN (0, 300, 800, 1500)),
  pts_speed   INTEGER NOT NULL DEFAULT 0 CHECK (pts_speed BETWEEN 0 AND 200),
  country_ok  BOOLEAN NOT NULL DEFAULT false,
  time_ms     INTEGER NOT NULL CHECK (time_ms >= 0),
  PRIMARY KEY (game, n, m),
  FOREIGN KEY (game, n) REFERENCES free_round(game, n) ON DELETE CASCADE,
  FOREIGN KEY (game, m) REFERENCES free_member(game, m) ON DELETE CASCADE,
  CHECK ((lat IS NULL) = (lon IS NULL) AND (lat IS NULL) = (km IS NULL)),
  CHECK (lat IS NOT NULL OR (pts_km = 0 AND pts_bonus = 0 AND pts_speed = 0 AND NOT auto))
);

-- A finished game's line for each Telegram player, with the settings it was
-- played on: the profile reads only this. points has the speed bonus,
-- points_base does not, so the average for a place compares across timers.
-- km_rounds holds the distance of every place answered with a pin, for the
-- typical miss; km is their sum. place and players are the standing in a
-- party, beaten how many of the others finished below; NULL alone. streak is
-- a streak game's length.
CREATE TABLE IF NOT EXISTS free_result (
  game        BIGINT NOT NULL REFERENCES free_game(id) ON DELETE CASCADE,
  m           SMALLINT NOT NULL,
  user_id     BIGINT NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('solo', 'party', 'duel')),
  mode        TEXT NOT NULL CHECK (mode IN ('classic', 'streak')),
  seconds     SMALLINT,
  rounds      SMALLINT,
  points      INTEGER NOT NULL,
  points_base INTEGER NOT NULL,
  km          DOUBLE PRECISION NOT NULL,
  km_rounds   DOUBLE PRECISION[] NOT NULL,
  time_ms     BIGINT NOT NULL,
  played      SMALLINT NOT NULL,
  answered    SMALLINT NOT NULL,
  countries   SMALLINT NOT NULL,
  from_round  SMALLINT NOT NULL,
  place       SMALLINT,
  players     SMALLINT,
  beaten      SMALLINT,
  streak      SMALLINT,
  finished_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (game, m),
  FOREIGN KEY (game, m) REFERENCES free_member(game, m) ON DELETE CASCADE,
  CHECK ((mode = 'streak') = (streak IS NOT NULL)),
  CHECK ((kind = 'solo') = (place IS NULL))
);
CREATE INDEX IF NOT EXISTS free_result_user_idx ON free_result (user_id, mode, seconds, rounds);
