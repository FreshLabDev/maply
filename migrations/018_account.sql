-- SPDX-License-Identifier: Apache-2.0
-- Accounts on the site: a sign-in with Telegram, with Google, or both. They
-- live in maply, not in core: no other bot of the family has a website.
--
-- An account holds at most one Telegram sign-in and one Google sign-in, and
-- always at least one: the last one cannot be unlinked. Google gives only
-- `sub`, the account's lasting number; no email, name or photo is asked for.
CREATE TABLE IF NOT EXISTS account (
  id               UUID PRIMARY KEY,
  telegram_user_id BIGINT UNIQUE REFERENCES core.person(telegram_user_id),
  google_sub       TEXT UNIQUE CHECK (char_length(google_sub) BETWEEN 1 AND 255),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (telegram_user_id IS NOT NULL OR google_sub IS NOT NULL)
);

-- The site players an account has taken in: the guest of the browser it was
-- made in, guests it adopted later and the players of accounts merged into
-- it. Their closed days stay as they were. Deleting the account leaves them
-- as guests.
ALTER TABLE site_player ADD COLUMN IF NOT EXISTS account_id UUID REFERENCES account(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS site_player_account_idx ON site_player (account_id) WHERE account_id IS NOT NULL;

-- The player new site answers of the account go to while it has no Telegram.
-- With Telegram the site answers as the bot's player, into guess.
ALTER TABLE account ADD COLUMN IF NOT EXISTS player UUID REFERENCES site_player(id) ON DELETE SET NULL;

-- A browser's session: the cookie holds 256 random bits, this table only
-- their SHA-256. A session lasts 90 days from the sign-in and is not
-- extended by use.
CREATE TABLE IF NOT EXISTS session (
  token_hash BYTEA PRIMARY KEY CHECK (length(token_hash) = 32),
  account_id UUID NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  via        TEXT NOT NULL CHECK (via IN ('telegram', 'google')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS session_account_idx ON session (account_id);
CREATE INDEX IF NOT EXISTS session_expiry_idx ON session (expires_at);

-- A ticket the Mini App hands to an outside browser to link Google: Google
-- refuses to sign in inside Telegram. Five minutes, good once.
CREATE TABLE IF NOT EXISTS link_ticket (
  token_hash BYTEA PRIMARY KEY CHECK (length(token_hash) = 32),
  account_id UUID NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  used_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS link_ticket_expiry_idx ON link_ticket (expires_at);
