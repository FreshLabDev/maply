-- SPDX-License-Identifier: Apache-2.0
-- A player's profile photo for the pins and tables of the results: a small
-- square JPEG, or NULL when the person has none the bot may see. checked_at
-- is when Telegram was last asked, so a changed photo shows within a day.
CREATE TABLE IF NOT EXISTS avatar (
  user_id        BIGINT PRIMARY KEY,
  file_unique_id TEXT,
  jpeg           BYTEA,
  checked_at     TIMESTAMPTZ NOT NULL
);
