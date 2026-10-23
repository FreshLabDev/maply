-- SPDX-License-Identifier: Apache-2.0
-- The world ranking: everyone who answered a day, from Telegram or the site,
-- ranked one by one. Groups are not ranked against each other.

-- A site player has no Telegram account. The browser keeps a random id in a
-- cookie; the name is optional and the ranking shows "Guest" without one.
CREATE TABLE IF NOT EXISTS site_player (
  id         UUID PRIMARY KEY,
  name       TEXT CHECK (char_length(name) BETWEEN 1 AND 24),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One answer a day per site player, scored like a Telegram answer once its
-- deadline has passed. There is no speed bonus: the browser names its own
-- zone, so the deadline it is counted from is the player's word.
CREATE TABLE IF NOT EXISTS site_guess (
  date        DATE NOT NULL REFERENCES round(date),
  player      UUID NOT NULL REFERENCES site_player(id),
  lat         DOUBLE PRECISION NOT NULL CHECK (lat BETWEEN -90 AND 90),
  lon         DOUBLE PRECISION NOT NULL CHECK (lon BETWEEN -180 AND 180),
  answered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deadline_at TIMESTAMPTZ NOT NULL,
  km          DOUBLE PRECISION CHECK (km >= 0),
  pts_km      INTEGER CHECK (pts_km BETWEEN 0 AND 3500),
  pts_bonus   INTEGER CHECK (pts_bonus IN (0, 300, 800, 1500)),
  PRIMARY KEY (date, player),
  CHECK (answered_at <= deadline_at)
);

-- Every scored answer of every day, both kinds, with its total. A day's
-- ranking is this ordered by points, the earlier answer first on a tie.
CREATE OR REPLACE VIEW day_score AS
  SELECT date, user_id, NULL::uuid AS player, km, pts_bonus,
         pts_km + pts_bonus + COALESCE(pts_speed, 0) AS points, answered_at
    FROM guess
   WHERE km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL
  UNION ALL
  SELECT date, NULL, player, km, pts_bonus, pts_km + pts_bonus, answered_at
    FROM site_guess
   WHERE km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL;

-- The look back at yesterday's final world ranking that the morning post
-- carried. A redraw keeps it, and a post that went out before yesterday closed
-- everywhere leaves it empty so the 20:00 result carries it instead.
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS yesterday TEXT;
