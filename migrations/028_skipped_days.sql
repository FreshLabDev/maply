-- SPDX-License-Identifier: Apache-2.0
-- A day nobody answered anywhere is skipped, not kept: nobody saw the place,
-- so it is not shown and goes back to the queue. The round stays (its day
-- number appeared in posts, and numbers are never reused), but it counts as
-- no round to every reader (db.Store.RoundOn and the queries that join round).
ALTER TABLE round ADD COLUMN IF NOT EXISTS skipped_at TIMESTAMPTZ;

-- The place can be drawn again for a later date while its skipped round
-- still names it.
ALTER TABLE round DROP CONSTRAINT IF EXISTS round_place_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS round_place_live_idx ON round (place_id) WHERE skipped_at IS NULL;

-- A place that came back is not drawn for a date before this one. Its pool is
-- never touched: a place of a day stays the day's pool (place_pool_kept).
ALTER TABLE place ADD COLUMN IF NOT EXISTS back_after DATE;

-- A chat whose result waits: the world had no answer at its reveal, and the
-- result goes out, or the day is skipped, when the day closes everywhere.
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS held BOOLEAN NOT NULL DEFAULT false;

-- Days kept before this with no answer at all, which the final world job kept
-- like any other: over everywhere (two days on in UTC, as in 016), nobody in
-- guess or site_guess. They lose their kept rows (day_place and day_thumb go
-- with day_closed) and their places return for a month.
WITH nobody AS (
  SELECT r.date FROM round r
   WHERE r.skipped_at IS NULL
     AND r.date <= (now() AT TIME ZONE 'UTC')::date - 2
     AND NOT EXISTS (SELECT 1 FROM guess g WHERE g.date = r.date)
     AND NOT EXISTS (SELECT 1 FROM site_guess g WHERE g.date = r.date)),
unkept AS (
  DELETE FROM day_closed c USING nobody n WHERE c.date = n.date RETURNING c.date),
skipped AS (
  UPDATE round r SET skipped_at = now() FROM nobody n WHERE r.date = n.date RETURNING r.place_id)
UPDATE place p SET status = 'approved', back_after = (now() AT TIME ZONE 'UTC')::date + 30
  FROM skipped s WHERE p.id = s.place_id;
