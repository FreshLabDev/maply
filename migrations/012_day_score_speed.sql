-- SPDX-License-Identifier: Apache-2.0
-- The day's ranking also carries the speed bonus, so the Mini App can draw
-- every row of the world's table in parts. A new column goes at the end:
-- CREATE OR REPLACE VIEW keeps the old ones in place.
CREATE OR REPLACE VIEW day_score AS
  SELECT date, user_id, NULL::uuid AS player, km, pts_bonus,
         pts_km + pts_bonus + COALESCE(pts_speed, 0) AS points, answered_at,
         COALESCE(pts_speed, 0) AS pts_speed
    FROM guess
   WHERE km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL
  UNION ALL
  SELECT date, NULL, player, km, pts_bonus, pts_km + pts_bonus, answered_at, 0
    FROM site_guess
   WHERE km IS NOT NULL AND pts_km IS NOT NULL AND pts_bonus IS NOT NULL;
