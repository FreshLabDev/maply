-- SPDX-License-Identifier: Apache-2.0
-- A Telegram player's own choices about their profile. A player without a
-- row has the defaults: named in the site's tables, a mini profile open to
-- anyone who sees the name, and no public page.
CREATE TABLE IF NOT EXISTS profile_setting (
  user_id     BIGINT PRIMARY KEY REFERENCES core.person(telegram_user_id),
  -- The site's tables call a hidden player "Telegram player", and their mini
  -- profile is closed. A group's own table still names them to the group.
  hide_me     BOOLEAN NOT NULL DEFAULT false,
  -- The public page /p/<slug> is on while the slug is set. The slug is
  -- random, so a page cannot be found by a player's id or name, and a new
  -- one takes the place of the old: the old link stops working.
  public_slug TEXT UNIQUE CHECK (public_slug ~ '^[A-Za-z0-9_-]{22}$'),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A site player's profile reads their answers by player, not by date.
CREATE INDEX IF NOT EXISTS site_guess_player_idx ON site_guess (player, date);
