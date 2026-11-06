-- SPDX-License-Identifier: Apache-2.0
-- Where the centre of the frame points, in degrees clockwise from north
-- (Mapillary's computed_compass_angle). The Mini App's compass needs it; a
-- direction alone does not locate a place, so it is sent before the reveal.
ALTER TABLE place ADD COLUMN IF NOT EXISTS heading REAL
  CHECK (heading IS NULL OR (heading >= 0 AND heading < 360));
