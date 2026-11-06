-- SPDX-License-Identifier: Apache-2.0
-- The picture of a person's statistics, once Telegram has it: the file_id of
-- the animation drawn for the numbers whose fingerprint is version, in one
-- language. New numbers make a new version, and the old file_id is replaced.
CREATE TABLE IF NOT EXISTS profile_card (
  user_id    BIGINT NOT NULL,
  lang       TEXT NOT NULL,
  version    TEXT NOT NULL,
  file_id    TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, lang)
);
