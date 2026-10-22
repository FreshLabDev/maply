-- SPDX-License-Identifier: Apache-2.0
-- LOCAL-DEV ONLY. Minimal core.person/core.chat/core.presence/core.touch plus the
-- maply schema. Production gets all of this from the core repository
-- (migrations 001 and 018).
CREATE SCHEMA IF NOT EXISTS core;
CREATE SCHEMA IF NOT EXISTS maply;

CREATE TABLE IF NOT EXISTS core.person (
  telegram_user_id bigint PRIMARY KEY,
  username text, first_name text, last_name text,
  is_bot boolean NOT NULL DEFAULT false,
  tg_language_code text,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS core.chat (
  chat_id bigint PRIMARY KEY,
  type text, title text, username text,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

-- chat_id 0 is the DM, as in core.
CREATE TABLE IF NOT EXISTS core.presence (
  telegram_user_id bigint NOT NULL REFERENCES core.person(telegram_user_id),
  bot text NOT NULL,
  chat_id bigint NOT NULL DEFAULT 0,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  event_count bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (telegram_user_id, bot, chat_id)
);

CREATE OR REPLACE VIEW core.maply_presence AS
  SELECT telegram_user_id, chat_id, first_seen_at, last_seen_at
    FROM core.presence
   WHERE bot = 'maply';

CREATE OR REPLACE FUNCTION core.touch(
  p_bot text, p_user_id bigint,
  p_username text DEFAULT NULL, p_first_name text DEFAULT NULL, p_last_name text DEFAULT NULL,
  p_tg_lang text DEFAULT NULL, p_chat_id bigint DEFAULT NULL, p_chat_type text DEFAULT NULL,
  p_chat_title text DEFAULT NULL, p_chat_uname text DEFAULT NULL, p_is_bot boolean DEFAULT false,
  p_at timestamptz DEFAULT now()
) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO core.person AS pe (telegram_user_id, username, first_name, last_name, is_bot, tg_language_code, last_seen_at, updated_at)
  VALUES (p_user_id, p_username, p_first_name, p_last_name, p_is_bot, p_tg_lang, p_at, p_at)
  ON CONFLICT (telegram_user_id) DO UPDATE SET
    username = COALESCE(EXCLUDED.username, pe.username),
    first_name = COALESCE(EXCLUDED.first_name, pe.first_name),
    last_name = COALESCE(EXCLUDED.last_name, pe.last_name),
    is_bot = EXCLUDED.is_bot,
    tg_language_code = COALESCE(EXCLUDED.tg_language_code, pe.tg_language_code),
    last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at;
  IF p_chat_id IS NOT NULL AND p_chat_id <> 0 THEN
    INSERT INTO core.chat AS ch (chat_id, type, title, username, last_seen_at, updated_at)
    VALUES (p_chat_id, p_chat_type, p_chat_title, p_chat_uname, p_at, p_at)
    ON CONFLICT (chat_id) DO UPDATE SET
      type = COALESCE(EXCLUDED.type, ch.type),
      title = COALESCE(EXCLUDED.title, ch.title),
      username = COALESCE(EXCLUDED.username, ch.username),
      last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at;
  END IF;
  INSERT INTO core.presence AS pr (telegram_user_id, bot, chat_id, first_seen_at, last_seen_at, event_count)
  VALUES (p_user_id, p_bot, COALESCE(p_chat_id, 0), p_at, p_at, 1)
  ON CONFLICT (telegram_user_id, bot, chat_id) DO UPDATE SET
    last_seen_at = GREATEST(pr.last_seen_at, EXCLUDED.last_seen_at),
    event_count = pr.event_count + 1;
  IF p_tg_lang IS NOT NULL AND btrim(p_tg_lang) <> '' THEN
    INSERT INTO core.language_observation AS o (source_bot, scope, subject_id, language, source)
    VALUES (p_bot, 'user', p_user_id, lower(split_part(btrim(p_tg_lang), '-', 1)), 'client')
    ON CONFLICT (source_bot, scope, subject_id) DO UPDATE SET
      language = CASE WHEN o.source = 'manual' THEN o.language ELSE EXCLUDED.language END,
      source   = CASE WHEN o.source = 'manual' THEN o.source ELSE 'client' END,
      observed_at = clock_timestamp();
  END IF;
END $$;

-- Local stand-in for the core language hub, with the real signatures
-- (core migration 001): observations per bot, scope and subject, manual over
-- client, and a person's or a chat's language read with either preferred.
DO $$ BEGIN CREATE TYPE core.pref_scope AS ENUM ('user', 'chat'); EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN CREATE TYPE core.lang_source AS ENUM ('manual', 'auto', 'client', 'default'); EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- The first version of this stand-in had no scopes.
DROP FUNCTION IF EXISTS core.set_language(text, text, bigint, text, text);
DROP FUNCTION IF EXISTS core.clear_language(text, text, bigint);
DROP FUNCTION IF EXISTS core.effective_language(bigint, bigint, text);
DROP TABLE IF EXISTS core.user_language;

CREATE TABLE IF NOT EXISTS core.language_observation (
  source_bot text NOT NULL,
  scope core.pref_scope NOT NULL,
  subject_id bigint NOT NULL,
  language text NOT NULL,
  source core.lang_source NOT NULL,
  observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (source_bot, scope, subject_id)
);

CREATE OR REPLACE FUNCTION core.set_language(
  p_bot text, p_scope core.pref_scope, p_subject bigint, p_lang text, p_source core.lang_source
) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  IF p_lang IS NULL OR btrim(p_lang) = '' THEN
    RETURN;
  END IF;
  INSERT INTO core.language_observation (source_bot, scope, subject_id, language, source)
  VALUES (p_bot, p_scope, p_subject, lower(split_part(btrim(p_lang), '-', 1)), p_source)
  ON CONFLICT (source_bot, scope, subject_id) DO UPDATE SET
    language = EXCLUDED.language, source = EXCLUDED.source, observed_at = clock_timestamp();
END $$;

-- Dropping the manual observation has to leave the client hint behind, or
-- "follow Telegram" would only mean "forget everything": the real hub keeps
-- every bot's observations side by side, while this stand-in only knows
-- maply, so it rebuilds the client row from the language_code core.touch
-- recorded.
CREATE OR REPLACE FUNCTION core.clear_language(
  p_bot text, p_scope core.pref_scope, p_subject bigint
) RETURNS void LANGUAGE plpgsql AS $$
DECLARE v_client text;
BEGIN
  DELETE FROM core.language_observation WHERE source_bot = p_bot AND scope = p_scope AND subject_id = p_subject;
  IF p_scope = 'user' THEN
    SELECT lower(split_part(btrim(pe.tg_language_code), '-', 1)) INTO v_client
    FROM core.person pe
    WHERE pe.telegram_user_id = p_subject AND btrim(coalesce(pe.tg_language_code, '')) <> '';
    IF v_client IS NOT NULL THEN
      INSERT INTO core.language_observation (source_bot, scope, subject_id, language, source)
      VALUES (p_bot, 'user', p_subject, v_client, 'client');
    END IF;
  END IF;
END $$;

CREATE OR REPLACE FUNCTION core.effective_language(
  p_user bigint, p_chat bigint DEFAULT NULL, p_prefer core.pref_scope DEFAULT 'user'
) RETURNS text LANGUAGE sql STABLE AS $$
  SELECT l.language
  FROM core.language_observation l
  WHERE (l.scope = 'user' AND l.subject_id = p_user)
     OR (l.scope = 'chat' AND l.subject_id = p_chat AND p_chat IS NOT NULL)
  ORDER BY (l.scope = p_prefer) DESC, (l.source = 'manual') DESC, l.observed_at DESC
  LIMIT 1;
$$;
