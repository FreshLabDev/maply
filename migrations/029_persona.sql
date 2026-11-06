-- SPDX-License-Identifier: Apache-2.0
-- A player's face in maply: one profile address, /u/<username>, and the name
-- and photo they chose over Telegram's. A Telegram person has one by their
-- id, whether or not they ever signed in on the site; a site account without
-- Telegram has one by the account. Linking Telegram to such an account moves
-- what it chose onto the Telegram person's, and deletes the account's.
--
-- username_by says where the username came from: 'telegram' follows the
-- person's Telegram username, 'maply' was made up for someone without one,
-- 'own' was typed by the person and never changes by itself.
--
-- name and photo are the person's own. NULL is Telegram's first name and
-- Telegram's photo (avatar), which come back as soon as these are removed.
-- photo is a square JPEG of photoSide pixels with an address of its own,
-- /face/<face>.jpg, a new one each time, as a Telegram photo has.
CREATE TABLE IF NOT EXISTS persona (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      BIGINT UNIQUE REFERENCES core.person(telegram_user_id),
  account_id   UUID UNIQUE REFERENCES account(id) ON DELETE CASCADE,
  username     TEXT NOT NULL CHECK (username ~ '^[a-z][a-z0-9_]{2,31}$'),
  username_by  TEXT NOT NULL CHECK (username_by IN ('telegram', 'maply', 'own')),
  name         TEXT CHECK (char_length(name) BETWEEN 1 AND 24),
  photo        BYTEA,
  face         TEXT CHECK (face ~ '^[0-9a-f]{32}$'),
  -- When the person was first asked for a name, a username and a photo: a
  -- site account without Telegram is, once, after its first sign-in.
  asked_at     TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((user_id IS NULL) <> (account_id IS NULL)),
  CHECK ((photo IS NULL) = (face IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS persona_username ON persona (username);
CREATE UNIQUE INDEX IF NOT EXISTS persona_face ON persona (face);

-- A username a person left keeps leading to them until someone takes it.
CREATE TABLE IF NOT EXISTS persona_was (
  username TEXT PRIMARY KEY CHECK (username ~ '^[a-z][a-z0-9_]{2,31}$'),
  persona  UUID NOT NULL REFERENCES persona(id) ON DELETE CASCADE,
  left_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS persona_was_persona_idx ON persona_was (persona);

-- core.person as maply shows it: the person's own name over Telegram's.
-- Every query that names a Telegram player reads it instead of core.person.
CREATE OR REPLACE VIEW person_shown AS
SELECT pe.telegram_user_id, COALESCE(pa.name, pe.first_name) AS first_name,
       CASE WHEN pa.name IS NULL THEN pe.last_name END AS last_name,
       pe.username, pe.is_bot, pa.username AS maply_username
  FROM core.person pe
  LEFT JOIN persona pa ON pa.user_id = pe.telegram_user_id;
