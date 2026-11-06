-- SPDX-License-Identifier: Apache-2.0
-- A place a day has had stays the day's whatever its status says. 019 kept
-- the pool of a place in status 'used', which a hand edit can set back to
-- 'approved'; a round naming the place is the day itself, so it counts too.
-- Only the trigger's function changes: no row is read or written here.
CREATE OR REPLACE FUNCTION place_pool_kept() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.pool IS DISTINCT FROM OLD.pool
     AND (OLD.pool = 'free' OR OLD.status = 'used'
          OR EXISTS (SELECT 1 FROM round WHERE place_id = OLD.id)) THEN
    RAISE EXCEPTION 'place %: pool % cannot change', OLD.id, OLD.pool;
  END IF;
  RETURN NEW;
END $$;
