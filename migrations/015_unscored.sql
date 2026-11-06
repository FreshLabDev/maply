-- SPDX-License-Identifier: Apache-2.0
-- Every result poll scores what is due first, and looks for the day's
-- answers that have no score yet. Almost none have: a Telegram answer is
-- scored when it comes in. Without these the search read every answer of
-- the day.
CREATE INDEX IF NOT EXISTS guess_unscored_idx ON guess (date) WHERE km IS NULL;
CREATE INDEX IF NOT EXISTS site_guess_unscored_idx ON site_guess (date) WHERE km IS NULL;
