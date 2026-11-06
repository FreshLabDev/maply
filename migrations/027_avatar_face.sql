-- SPDX-License-Identifier: Apache-2.0
-- A kept photo has an address of its own, /face/<face>.jpg: a table names
-- its people's photos instead of carrying them, and a browser keeps each one.
-- A new photo gets a new face, so an address never changes what it shows.
ALTER TABLE avatar ADD COLUMN IF NOT EXISTS face TEXT;
UPDATE avatar SET face = replace(gen_random_uuid()::text, '-', '')
 WHERE jpeg IS NOT NULL AND face IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS avatar_face ON avatar (face);
