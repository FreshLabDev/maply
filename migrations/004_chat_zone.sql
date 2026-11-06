-- SPDX-License-Identifier: Apache-2.0
-- The reveal is 20:00 in the chat's own zone. A group admin picks it, a person
-- picks their DM's; NULL follows the default of the chat's language, which the
-- service resolves (internal/round).
ALTER TABLE chat_settings ADD COLUMN IF NOT EXISTS timezone TEXT;

-- The language is core's: shared with every bot in the family through
-- core.set_language, so maply keeps no copy that could disagree with it.
ALTER TABLE chat_settings DROP COLUMN IF EXISTS lang;
