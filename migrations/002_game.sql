-- SPDX-License-Identifier: Apache-2.0
-- The game: places, rounds, answers, per-chat state and the send queue.
-- Streaks, seasons and rankings are computed from guess, never stored.

-- A place moves pending -> approved -> used, or pending -> rejected. Source
-- IDs and coordinates never leave the server before the reveal; image_key is
-- the only name a client sees.
CREATE TABLE IF NOT EXISTS place (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  status      TEXT NOT NULL DEFAULT 'pending'
              CHECK (status IN ('pending', 'approved', 'rejected', 'used')),
  source      TEXT NOT NULL CHECK (source IN ('mapillary', 'panoramax')),
  source_id   TEXT NOT NULL,
  source_url  TEXT,
  author      TEXT,
  license     TEXT NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('photo', 'panorama')),
  lat         DOUBLE PRECISION NOT NULL CHECK (lat BETWEEN -90 AND 90),
  lon         DOUBLE PRECISION NOT NULL CHECK (lon BETWEEN -180 AND 180),
  image_key   TEXT UNIQUE,
  detail_key  TEXT UNIQUE,
  country_iso TEXT CHECK (country_iso ~ '^[A-Z]{2}$'),
  continent   TEXT CHECK (continent IN ('africa', 'antarctica', 'asia', 'europe',
                                        'north_america', 'oceania', 'south_america')),
  -- Names and the fact are per interface language: {"ru": …, "uk": …, "be": …}.
  city        JSONB CHECK (city IS NULL OR jsonb_typeof(city) = 'object'),
  city_poly   JSONB CHECK (city_poly IS NULL OR jsonb_typeof(city_poly) = 'object'),
  fact        JSONB CHECK (fact IS NULL OR jsonb_typeof(fact) = 'object'),
  difficulty  SMALLINT CHECK (difficulty BETWEEN 1 AND 5),
  reviewed_by BIGINT,
  reviewed_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source, source_id),
  -- Nothing reaches a round without its picture and what scoring needs.
  CHECK (status NOT IN ('approved', 'used')
         OR (image_key IS NOT NULL AND country_iso IS NOT NULL AND continent IS NOT NULL
             AND difficulty IS NOT NULL AND reviewed_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS place_queue_idx ON place (difficulty, id) WHERE status = 'approved';

-- One place per calendar date for everyone.
CREATE TABLE IF NOT EXISTS round (
  date       DATE PRIMARY KEY,
  day_no     INTEGER NOT NULL UNIQUE CHECK (day_no > 0),
  place_id   BIGINT NOT NULL UNIQUE REFERENCES place(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A DM and a group are both a chat here: the scheduler treats them alike.
-- A DM's chat_id is the person's id, and core.chat does not hold DMs, so
-- there is no foreign key.
CREATE TABLE IF NOT EXISTS chat_settings (
  chat_id    BIGINT PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('dm', 'group')),
  -- NULL follows core.effective_language.
  lang       TEXT CHECK (lang IN ('ru', 'uk', 'be')),
  -- The reveal is fixed at 20:00; only the morning post moves.
  post_time  TIME NOT NULL DEFAULT '07:00' CHECK (post_time >= '05:00' AND post_time <= '19:00'),
  intrigue   BOOLEAN NOT NULL DEFAULT true,
  mentions   BOOLEAN NOT NULL DEFAULT true,
  -- false once the bot is removed or blocked; the row stays for the history.
  active     BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (kind = 'group' OR chat_id > 0),
  CHECK (kind = 'dm' OR chat_id < 0)
);

-- One answer per person per day, whichever chat it came from. deadline_at is
-- the earliest reveal among the person's chats when the answer was accepted.
CREATE TABLE IF NOT EXISTS guess (
  date        DATE NOT NULL REFERENCES round(date),
  user_id     BIGINT NOT NULL REFERENCES core.person(telegram_user_id),
  context     TEXT NOT NULL CHECK (context IN ('dm', 'group', 'site')),
  chat_id     BIGINT,
  lat         DOUBLE PRECISION NOT NULL CHECK (lat BETWEEN -90 AND 90),
  lon         DOUBLE PRECISION NOT NULL CHECK (lon BETWEEN -180 AND 180),
  answered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deadline_at TIMESTAMPTZ NOT NULL,
  -- Filled at the reveal, not before: nothing about distance exists earlier.
  km          DOUBLE PRECISION CHECK (km >= 0),
  pts_km      INTEGER CHECK (pts_km BETWEEN 0 AND 3500),
  pts_bonus   INTEGER CHECK (pts_bonus IN (0, 300, 800, 1500)),
  clean       BOOLEAN NOT NULL DEFAULT false,
  PRIMARY KEY (date, user_id),
  CHECK (answered_at <= deadline_at),
  CHECK ((context = 'group') = (chat_id IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS guess_user_idx ON guess (user_id, date DESC);

-- What has already happened in a chat on a date. The scheduler reads it after
-- a restart instead of guessing.
CREATE TABLE IF NOT EXISTS chat_round (
  chat_id       BIGINT NOT NULL REFERENCES chat_settings(chat_id),
  date          DATE NOT NULL REFERENCES round(date),
  post_msg_id   BIGINT,
  pinned        BOOLEAN NOT NULL DEFAULT false,
  intrigue_at   TIMESTAMPTZ,
  intrigue_sent BOOLEAN NOT NULL DEFAULT false,
  result_msg_id BIGINT,
  PRIMARY KEY (chat_id, date)
);

-- Scheduled sends. key is the idempotency key, for example
-- "post:2026-10-23:-1001234": a restart re-reads the queue and a key that is
-- already done is never sent again.
CREATE TABLE IF NOT EXISTS job (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  key          TEXT NOT NULL UNIQUE,
  kind         TEXT NOT NULL CHECK (kind IN ('post', 'pin', 'intrigue', 'result')),
  chat_id      BIGINT NOT NULL,
  date         DATE NOT NULL,
  run_at       TIMESTAMPTZ NOT NULL,
  status       TEXT NOT NULL DEFAULT 'pending'
               CHECK (status IN ('pending', 'running', 'done', 'failed', 'skipped')),
  attempts     INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  locked_until TIMESTAMPTZ,
  last_error   TEXT,
  done_at      TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS job_due_idx ON job (run_at) WHERE status IN ('pending', 'running');
