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
	if errors.Is(err, pgx.ErrNoRows) || zone == nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("chat zone %d: %w", chatID, err)
	}
	return *zone, nil
}

// SetChatZone stores the zone a chat picked. The caller checks it is one of
// round.ZoneChoices.
func (s *Store) SetChatZone(ctx context.Context, chatID int64, zone string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, timezone) VALUES ($1, $2, $3)
		ON CONFLICT (chat_id) DO UPDATE SET timezone = EXCLUDED.timezone, updated_at = now()`,
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
		                                    active = true, updated_at = now()`,
		chatID, kindOf(chatID), zone); err != nil {
		return fmt.Errorf("adopt zone of %d: %w", chatID, err)
	}
	return nil
}

// LeaveChat marks a group maply was removed from, or a DM whose person
// blocked it. The row stays: its zone is still the chat's if maply comes back.
func (s *Store) LeaveChat(ctx context.Context, chatID int64) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, active) VALUES ($1, $2, false)
		ON CONFLICT (chat_id) DO UPDATE SET active = false, updated_at = now()`, chatID, kindOf(chatID)); err != nil {
		return fmt.Errorf("leave chat %d: %w", chatID, err)
	}
	return nil
}

// ReturnChat marks a chat active again: the person unblocked maply or wrote
// to it, or the group talks to it.
func (s *Store) ReturnChat(ctx context.Context, chatID int64) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE chat_settings SET active = true, updated_at = now() WHERE chat_id = $1 AND NOT active`, chatID); err != nil {
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
