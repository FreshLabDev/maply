// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ChatLang is the group's language in core, empty when core has none. It asks
// for no member's language: a group's zone must not depend on who opened it.
func (s *Store) ChatLang(ctx context.Context, chatID int64) (string, error) {
	var lang *string
	if err := s.pool.QueryRow(ctx, `SELECT core.effective_language(0, $1, 'chat')`, chatID).Scan(&lang); err != nil {
		return "", fmt.Errorf("chat lang %d: %w", chatID, err)
	}
	if lang == nil {
		return "", nil
	}
	return *lang, nil
}

// SetUserLang records a language the person picked by hand. Manual outranks
// every automatic source in core, for every bot in the family.
func (s *Store) SetUserLang(ctx context.Context, userID int64, lang string) error {
	return s.setLang(ctx, "user", userID, lang)
}

// ManualLang is the language the person picked by hand in any bot of the
// family, empty when theirs comes from the Telegram app or from nowhere.
func (s *Store) ManualLang(ctx context.Context, userID int64) (string, error) {
	var lang *string
	if err := s.pool.QueryRow(ctx, `SELECT core.manual_language('user', $1)`, userID).Scan(&lang); err != nil {
		return "", fmt.Errorf("manual lang %d: %w", userID, err)
	}
	if lang == nil {
		return "", nil
	}
	return *lang, nil
}

// ClearUserLang withdraws maply's claim, so the next source in core decides.
func (s *Store) ClearUserLang(ctx context.Context, userID int64) error {
	return s.clearLang(ctx, "user", userID)
}

// SetChatLang records the language a group admin picked.
func (s *Store) SetChatLang(ctx context.Context, chatID int64, lang string) error {
	return s.setLang(ctx, "chat", chatID, lang)
}

// ClearChatLang withdraws maply's claim on the group's language.
func (s *Store) ClearChatLang(ctx context.Context, chatID int64) error {
	return s.clearLang(ctx, "chat", chatID)
}

func (s *Store) setLang(ctx context.Context, scope string, subject int64, lang string) error {
	if _, err := s.pool.Exec(ctx, `SELECT core.set_language('maply', $1::core.pref_scope, $2, $3, 'manual')`,
		scope, subject, lang); err != nil {
		return fmt.Errorf("set %s language of %d: %w", scope, subject, err)
	}
	return nil
}

func (s *Store) clearLang(ctx context.Context, scope string, subject int64) error {
	if _, err := s.pool.Exec(ctx, `SELECT core.clear_language('maply', $1::core.pref_scope, $2)`,
		scope, subject); err != nil {
		return fmt.Errorf("clear %s language of %d: %w", scope, subject, err)
	}
	return nil
}

// ChatZone is the zone a chat picked, empty when it has not. A DM is keyed by
// the person's id.
func (s *Store) ChatZone(ctx context.Context, chatID int64) (string, error) {
	var zone *string
	err := s.pool.QueryRow(ctx, `SELECT timezone FROM chat_settings WHERE chat_id = $1`, chatID).Scan(&zone)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("chat zone %d: %w", chatID, err)
	}
	if zone == nil {
		return "", nil
	}
	return *zone, nil
}

// ChatActive reports whether maply is still in the group: false once it was
// removed. A chat with no settings row yet counts as active.
func (s *Store) ChatActive(ctx context.Context, chatID int64) (bool, error) {
	var active bool
	err := s.pool.QueryRow(ctx, `SELECT active FROM chat_settings WHERE chat_id = $1`, chatID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("chat active %d: %w", chatID, err)
	}
	return active, nil
}

// SetChatZone stores the zone a chat picked. The caller checks it is one of
// round.ZoneChoices.
func (s *Store) SetChatZone(ctx context.Context, chatID int64, zone string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, timezone) VALUES ($1, $2, $3)
		ON CONFLICT (chat_id) DO UPDATE SET timezone = EXCLUDED.timezone, zone_auto = false, updated_at = now()`,
		chatID, kindOf(chatID), zone); err != nil {
		return fmt.Errorf("set zone of %d: %w", chatID, err)
	}
	return nil
}

// AdoptChatZone fixes a group's zone the first time maply sets up there, so
// a later change of the group's language does not move its reveal. A zone
// someone picked is kept.
func (s *Store) AdoptChatZone(ctx context.Context, chatID int64, zone string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, timezone) VALUES ($1, $2, $3)
		ON CONFLICT (chat_id) DO UPDATE SET timezone = COALESCE(chat_settings.timezone, EXCLUDED.timezone),
		                                    active = true, left_at = NULL, updated_at = now()`,
		chatID, kindOf(chatID), zone); err != nil {
		return fmt.Errorf("adopt zone of %d: %w", chatID, err)
	}
	return nil
}

// FixZone stores the zone a chat is in while it has none, so the reveal stops
// following the language. A DM's is marked as fixed this way: the phone's
// zone may replace it.
func (s *Store) FixZone(ctx context.Context, chatID int64, zone string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, timezone, zone_auto) VALUES ($1, $2, $3, $2 = 'dm')
		ON CONFLICT (chat_id) DO UPDATE SET timezone = EXCLUDED.timezone, zone_auto = EXCLUDED.zone_auto, updated_at = now()
		 WHERE chat_settings.timezone IS NULL`, chatID, kindOf(chatID), zone); err != nil {
		return fmt.Errorf("fix zone of %d: %w", chatID, err)
	}
	return nil
}

// AdoptDMZone sets a person's zone from their phone while they have none or
// only the one their language gave: a zone they picked stays. It reports
// whether it set one.
func (s *Store) AdoptDMZone(ctx context.Context, userID int64, zone string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, timezone) VALUES ($1, 'dm', $2)
		ON CONFLICT (chat_id) DO UPDATE SET timezone = EXCLUDED.timezone, zone_auto = false, updated_at = now()
		 WHERE chat_settings.timezone IS NULL OR chat_settings.zone_auto`, userID, zone)
	if err != nil {
		return false, fmt.Errorf("adopt zone of %d: %w", userID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// LeaveChat marks a group maply was removed from, or a DM whose person
// blocked it. The row stays: its zone is still the chat's if maply comes back,
// and left_at keeps the chat in the answer window of a day whose result it
// may already have shown.
func (s *Store) LeaveChat(ctx context.Context, chatID int64) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, active, left_at) VALUES ($1, $2, false, now())
		ON CONFLICT (chat_id) DO UPDATE SET active = false, updated_at = now(),
		       left_at = CASE WHEN chat_settings.active THEN now() ELSE COALESCE(chat_settings.left_at, now()) END`,
		chatID, kindOf(chatID)); err != nil {
		return fmt.Errorf("leave chat %d: %w", chatID, err)
	}
	return nil
}

// MigrateChat carries a group over to the supergroup Telegram made of it.
// The new id takes the old one's settings and the language picked by hand,
// and the old id is left as any chat maply was removed from. The day goes on
// in the new chat from the next planning pass; what went to the old one
// stays there, since its messages cannot be edited any more.
func (s *Store) MigrateChat(ctx context.Context, from, to int64) error {
	if from >= 0 || to >= 0 || from == to {
		return fmt.Errorf("migrate chat %d to %d: not two groups", from, to)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrate chat %d: %w", from, err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, post_time, intrigue, mentions, active, timezone, zone_auto)
		SELECT $2, 'group', post_time, intrigue, mentions, true, timezone, zone_auto
		  FROM chat_settings WHERE chat_id = $1
		ON CONFLICT (chat_id) DO UPDATE SET post_time = EXCLUDED.post_time, intrigue = EXCLUDED.intrigue,
		       mentions = EXCLUDED.mentions, timezone = COALESCE(EXCLUDED.timezone, chat_settings.timezone),
		       zone_auto = EXCLUDED.zone_auto, active = true, left_at = NULL, updated_at = now()`, from, to); err != nil {
		return fmt.Errorf("migrate chat %d settings: %w", from, err)
	}
	var lang *string
	if err := tx.QueryRow(ctx, `SELECT core.manual_language('chat', $1)`, from).Scan(&lang); err != nil {
		return fmt.Errorf("migrate chat %d language: %w", from, err)
	}
	if lang != nil {
		if _, err := tx.Exec(ctx, `SELECT core.set_language('maply', 'chat', $1, $2, 'manual')`, to, *lang); err != nil {
			return fmt.Errorf("migrate chat %d language: %w", from, err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE chat_settings SET active = false, updated_at = now(),
		       left_at = CASE WHEN active THEN now() ELSE COALESCE(left_at, now()) END
		 WHERE chat_id = $1`, from); err != nil {
		return fmt.Errorf("migrate chat %d: %w", from, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrate chat %d: %w", from, err)
	}
	return nil
}

// ReturnChat marks a chat active again: the person unblocked maply or wrote
// to it, or the group talks to it.
func (s *Store) ReturnChat(ctx context.Context, chatID int64) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE chat_settings SET active = true, left_at = NULL, updated_at = now() WHERE chat_id = $1 AND NOT active`, chatID); err != nil {
		return fmt.Errorf("return chat %d: %w", chatID, err)
	}
	return nil
}

func kindOf(chatID int64) string {
	if chatID > 0 {
		return "dm"
	}
	return "group"
}
