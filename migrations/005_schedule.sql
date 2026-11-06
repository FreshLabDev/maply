-- SPDX-License-Identifier: Apache-2.0
-- What the scheduler needs to send each message exactly once and to redraw
-- the morning post no more than once a minute.

-- Telegram's id for the day's photo after the first upload: every other chat
-- gets the same file without a second upload.
ALTER TABLE round ADD COLUMN IF NOT EXISTS photo_file_id TEXT;

-- The who-answered line last written into the post and when, so an edit
-- happens only when the line changed and at most once a minute.
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS answered TEXT;
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS answered_at TIMESTAMPTZ;
-- Which intrigue went out, so tomorrow's is a different one.
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS intrigue_kind TEXT;

-- The admins are told once that the post is better pinned.
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS pin_warned BOOLEAN NOT NULL DEFAULT false;
