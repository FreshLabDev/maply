-- SPDX-License-Identifier: Apache-2.0
-- The bonus for answering early, filled at the reveal with the rest of the
-- score. Answers scored before it existed keep NULL and count as 0.
ALTER TABLE guess ADD COLUMN IF NOT EXISTS pts_speed INTEGER CHECK (pts_speed BETWEEN 0 AND 250);
