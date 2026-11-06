-- SPDX-License-Identifier: Apache-2.0
-- A Telegram player sees the place the moment they answer, from the first
-- morning post on, and a new browser is a new site player. So until the site
-- has sign-in, every site answer ranks after every Telegram answer: a site
-- row never takes a Telegram player's place, first or any other. Among
-- themselves site answers keep the usual order. The late mark stays on the
-- rows; the ranking no longer needs it.
CREATE OR REPLACE VIEW day_rank AS
  SELECT s.date, s.user_id, s.player, s.km, s.pts_bonus, s.points, s.answered_at,
         s.pts_speed, s.lat, s.lon, s.late,
         row_number() OVER (PARTITION BY s.date
                            ORDER BY (s.user_id IS NULL), s.points DESC, s.km,
                                     s.answered_at, s.user_id, s.player) AS rank,
         count(*) OVER (PARTITION BY s.date) AS n
    FROM day_score s;
