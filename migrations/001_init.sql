-- SPDX-License-Identifier: Apache-2.0
-- Tables in search_path=maply. Identity stays in core.*.

CREATE TABLE IF NOT EXISTS runtime_state (
  singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
  telegram_offset BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO runtime_state(singleton) VALUES (true) ON CONFLICT DO NOTHING;
