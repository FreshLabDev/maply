-- SPDX-License-Identifier: Apache-2.0
-- A group where nobody plays is asked once whether to go on, and paused when
-- nobody answers the question. A quiet day is a day the group got its
-- morning post and nobody answered from the group (guess.chat_id); a
-- warm-up is free play and is no answer here.
--
-- quiet_since: days before it do not count towards the next question. Set by
--   "Keep playing" and by a resume.
-- quiet_asked_on, quiet_msg_id: the question is out and waits for a press;
--   cleared by a press, by an answer from the group and by the pause.
-- paused_at, paused_by: the group gets nothing from the scheduler. A paused
--   group stays active: it still has maply, and its members' answer window
--   still counts it.
-- resumed_at: a resumed group's day starts with the first morning post after
--   this moment.
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS quiet_since DATE;
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS quiet_asked_on DATE;
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS quiet_msg_id BIGINT;
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS paused_at TIMESTAMPTZ;
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS paused_by TEXT CHECK (paused_by IN ('admin', 'silence'));
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS resumed_at TIMESTAMPTZ;
