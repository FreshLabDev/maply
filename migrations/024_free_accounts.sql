-- SPDX-License-Identifier: Apache-2.0
-- Sign-in gives records. An account with Google alone plays free as its own
-- site player, account.player, and keeps records and plays with friends as a
-- Telegram person does; with Telegram it plays as the person. A guest plays
-- alone and keeps nothing, as before.

-- A finished game's line is a Telegram person's (user_id) or an account's
-- site player's (player). The store writes a player's line only while the
-- player belongs to an account, so a guest's game leaves no line. A
-- profile reads an account's lines by every site player it has taken in,
-- and by its Telegram person when it has one.
ALTER TABLE free_result ADD COLUMN IF NOT EXISTS player UUID REFERENCES site_player(id) ON DELETE CASCADE;
ALTER TABLE free_result ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE free_result ADD CONSTRAINT free_result_who CHECK ((user_id IS NULL) <> (player IS NULL));
CREATE INDEX IF NOT EXISTS free_result_player_idx ON free_result (player, mode, seconds, rounds)
  WHERE player IS NOT NULL;

-- The tables of the best read one set of settings since a moment: the best
-- line of each player over a week, or over all time. Only games on a timer
-- are ranked, so a place cannot be looked up at leisure. rounds is NULL for
-- a streak and against the clock, so it is matched with IS NULL there.
CREATE INDEX IF NOT EXISTS free_result_top_idx ON free_result (mode, seconds, rounds, finished_at)
  WHERE seconds IS NOT NULL AND mode <> 'archive';

-- An attempt at a challenge is a Telegram person's or an account's site
-- player's, once per challenge each. The attempt is its game, which is the
-- key now that user_id may be NULL. A unique pair with a NULL in it never
-- clashes, so each of the two constraints holds only its own kind.
ALTER TABLE free_attempt DROP CONSTRAINT IF EXISTS free_attempt_pkey;
ALTER TABLE free_attempt DROP CONSTRAINT IF EXISTS free_attempt_game_key;
ALTER TABLE free_attempt ADD COLUMN IF NOT EXISTS player UUID REFERENCES site_player(id) ON DELETE CASCADE;
ALTER TABLE free_attempt ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE free_attempt
  ADD CONSTRAINT free_attempt_pkey PRIMARY KEY (game),
  ADD CONSTRAINT free_attempt_who CHECK ((user_id IS NULL) <> (player IS NULL)),
  ADD CONSTRAINT free_attempt_user_key UNIQUE (challenge, user_id),
  ADD CONSTRAINT free_attempt_player_key UNIQUE (challenge, player);
CREATE INDEX IF NOT EXISTS free_attempt_player_idx ON free_attempt (player) WHERE player IS NOT NULL;
