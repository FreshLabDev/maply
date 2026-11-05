-- SPDX-License-Identifier: Apache-2.0
-- A chat's zone is fixed the first time maply plans a day for it, from its
-- language, so a later change of language does not move the reveal of a day
-- already shown. zone_auto marks a DM's zone fixed that way: the phone's zone
-- may still replace it, one the person picked may not.
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS zone_auto BOOLEAN NOT NULL DEFAULT false;

-- The group's 20:00 result as it went out, and its world line, so the
-- morning's edit swaps that one line for the final one instead of building
-- the caption again from the group as it is by then.
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS result_text TEXT;
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS result_world TEXT;

-- When maply left the chat. A chat left after its reveal still closes the
-- answer window of that day for its people: they may have seen the place in
-- its result before maply went. Chats left before this column existed take
-- the time of their last change, which is the leaving or later.
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS left_at TIMESTAMPTZ;
UPDATE chat_settings SET left_at = updated_at WHERE NOT active AND left_at IS NULL;

-- A site answer given after the day's first reveal: by then the place is out
-- in some chat, and a new browser is a new player, so such an answer does
-- not take the world's first place.
ALTER TABLE site_guess ADD COLUMN IF NOT EXISTS late BOOLEAN NOT NULL DEFAULT false;

-- New columns go at the end: CREATE OR REPLACE VIEW keeps the old in place.
CREATE OR REPLACE VIEW day_score AS
  SELECT date, user_id, NULL::uuid AS player, km, pts_bonus,
         pts_km + pts_bonus + COALESCE(pts_speed, 0) AS points, answered_at,
         COALESCE(pts_speed, 0) AS pts_speed, lat, lon, false AS late
    FROM guess
   WHERE km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL
  UNION ALL
  SELECT date, NULL, player, km, pts_bonus, pts_km + pts_bonus, answered_at, 0, lat, lon, late
    FROM site_guess
   WHERE km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL;

-- The one order of a day's ranking, for the world post, the Mini App, the
-- profile and the days page alike: more points, then the shorter distance,
-- then the earlier answer. First place goes to the best answer that is not a
-- late site one, when there is such an answer.
CREATE OR REPLACE VIEW day_rank AS
  SELECT s.date, s.user_id, s.player, s.km, s.pts_bonus, s.points, s.answered_at,
         s.pts_speed, s.lat, s.lon, s.late,
         row_number() OVER (PARTITION BY s.date
                            ORDER BY (NOT s.late AND s.best) DESC, s.points DESC, s.km,
                                     s.answered_at, s.user_id, s.player) AS rank,
         count(*) OVER (PARTITION BY s.date) AS n
    FROM (SELECT ds.*,
                 row_number() OVER (PARTITION BY ds.date, ds.late
                                    ORDER BY ds.points DESC, ds.km, ds.answered_at,
                                             ds.user_id, ds.player) = 1 AS best
            FROM day_score ds) s;
