-- SPDX-License-Identifier: Apache-2.0
-- The file_id of the animation a chat's result went out as. Sharing the
-- result from the Mini App sends that same animation; a result that went out
-- as a picture leaves it NULL and is shared as the card.
ALTER TABLE chat_round ADD COLUMN IF NOT EXISTS result_file_id TEXT;
