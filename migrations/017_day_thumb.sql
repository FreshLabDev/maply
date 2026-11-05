-- SPDX-License-Identifier: Apache-2.0
-- The days page shows each closed day as a strip of its own panorama: the
-- band from 8° below the horizon to 22° above it, without the ground and the
-- camera's operator, facing thumb_yaw.
--
-- thumb_yaw is in degrees clockwise from the centre of the panorama image,
-- as the viewer counts its yaw. The centre often faces a wall, so the
-- curation picks the direction; NULL lets the drawing pick one itself. It is
-- never sent to a client.
ALTER TABLE place ADD COLUMN IF NOT EXISTS thumb_yaw REAL
  CHECK (thumb_yaw IS NULL OR (thumb_yaw >= 0 AND thumb_yaw < 360));

-- The strip is drawn once a day is over in every zone and kept, and served
-- under a random key. It exists only for a day in day_closed: the key goes
-- out with the day's city and country, and a strip of a place still in play
-- somewhere, or still in the queue, would be a key to it. A day taken out of
-- day_closed by hand loses its strip with it and gets a new key when it is
-- kept again.
CREATE TABLE IF NOT EXISTS day_thumb (
  date    DATE PRIMARY KEY REFERENCES day_closed(date) ON DELETE CASCADE,
  key     TEXT NOT NULL UNIQUE CHECK (key ~ '^[A-Za-z0-9]{20}$'),
  -- The version of the drawing, so a new one can be told from the old.
  recipe  SMALLINT NOT NULL,
  -- JPEGs: small for a row of the list, wide for the day's page.
  small   BYTEA NOT NULL,
  wide    BYTEA NOT NULL,
  made_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
