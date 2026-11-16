-- SPDX-License-Identifier: Apache-2.0
-- A group that adds maply after its reveal has no day left to play until the
-- next morning, so it gets a warm-up: the newest day of the archive, closed in
-- every zone, played from the group's welcome and summed up in the group an
-- hour later. A warm-up is a day of the free game's archive: every player's
-- game is their own free_archive game, outside the day's tables, the world,
-- the profiles, the streaks and the ratings. warmup_game only says which of
-- those games were started from the group's welcome.
--
-- One warm-up a group, ever: a group that removes maply and adds it back
-- gets a welcome, not a second warm-up.
CREATE TABLE IF NOT EXISTS warmup (
  chat_id       BIGINT PRIMARY KEY CHECK (chat_id < 0),
  day           DATE NOT NULL REFERENCES round(date),
  result_at     TIMESTAMPTZ NOT NULL,
  result_msg_id BIGINT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS warmup_game (
  chat_id BIGINT NOT NULL REFERENCES warmup(chat_id) ON DELETE CASCADE,
  game    BIGINT NOT NULL REFERENCES free_game(id) ON DELETE CASCADE,
  PRIMARY KEY (chat_id, game)
);

-- The warm-up's result is a job of its own, keyed by the group and the date
-- the group joined on.
ALTER TABLE job DROP CONSTRAINT IF EXISTS job_kind_check;
ALTER TABLE job ADD CONSTRAINT job_kind_check CHECK (kind IN ('post', 'pin', 'intrigue', 'result', 'world', 'warmup'));
