-- SPDX-License-Identifier: Apache-2.0
-- The animation of a day's world ranking, once Telegram has it: the file_id
-- of the picture drawn for the answers whose fingerprint is version, in one
-- language. It is redrawn while answers still come in elsewhere; final is set
-- once the day has closed in every zone.
CREATE TABLE IF NOT EXISTS world_card (
  date       DATE NOT NULL,
  lang       TEXT NOT NULL,
  version    TEXT NOT NULL,
  file_id    TEXT NOT NULL,
  final      BOOLEAN NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (date, lang)
);

-- The job that puts the final world picture in the DM results of a day.
ALTER TABLE job DROP CONSTRAINT IF EXISTS job_kind_check;
ALTER TABLE job ADD CONSTRAINT job_kind_check CHECK (kind IN ('post', 'pin', 'intrigue', 'result', 'world'));

-- The version of the world picture a DM's result shows, so Refresh knows
-- whether there is anything newer.
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS world_version TEXT;
