// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/tg"
)

type Store struct {
	pool *pgxpool.Pool

	// answers keeps the places already read. A place in a round does not
	// change, and parsing its city's outline was most of every result poll's
	// work. A place edited by hand shows within answerFor.
	answersMu sync.Mutex
	answers   map[int64]keptAnswer
}

func Connect(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "maply"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// coreNeeds are the core functions maply calls that came after its first
// release, with the core migration that brought each one. A build that
// reaches a database without one fails every request that calls it, so the
// service refuses to start instead.
var coreNeeds = []struct{ fn, migration string }{
	{"core.manual_language(core.pref_scope,bigint)", "core 020 (manual_language)"},
	{"core.touch_person(text,bigint,text,text,text,text,boolean,timestamptz)", "core 021 (touch_person)"},
}

// CheckCore makes sure maply may call every core function it needs.
func (s *Store) CheckCore(ctx context.Context) error {
	for _, n := range coreNeeds {
		var ok bool
		err := s.pool.QueryRow(ctx, `SELECT to_regprocedure($1) IS NOT NULL AND has_function_privilege(to_regprocedure($1), 'EXECUTE')`, n.fn).Scan(&ok)
		if err != nil {
			return fmt.Errorf("check %s: %w", n.fn, err)
		}
		if !ok {
			return fmt.Errorf("%s is missing or not granted to maply: apply %s to the core database first", n.fn, n.migration)
		}
	}
	return nil
}

// Touch records the person, and the chat when it is not a DM, in core. It is
// also how core.presence learns which groups a person plays in, which the
// answer window depends on.
func (s *Store) Touch(ctx context.Context, user tg.User, chat *tg.Chat) error {
	var chatID, chatType, chatTitle, chatUname any
	if chat != nil && chat.Type != "private" && chat.ID != 0 {
		chatID = chat.ID
		chatType = chat.Type
		chatTitle = nullString(chat.Title)
		chatUname = nullString(chat.Username)
	}
	_, err := s.pool.Exec(ctx,
		`SELECT core.touch($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		"maply", user.ID, nullString(user.Username), nullString(user.FirstName),
		nullString(user.LastName), nullString(user.LanguageCode), chatID, chatType, chatTitle, chatUname, user.IsBot)
	return err
}

// TouchPerson records the person in core and nothing about where they were
// seen. Use it for a person met outside the bot's chats: a website sign-in
// with Telegram, or a guest who joins a game with friends from a link. Touch
// would count them as talking to maply in a DM, which moves their answer
// window and puts them on the morning DM post.
func (s *Store) TouchPerson(ctx context.Context, user tg.User) error {
	_, err := s.pool.Exec(ctx,
		`SELECT core.touch_person($1,$2,$3,$4,$5,$6,$7)`,
		"maply", user.ID, nullString(user.Username), nullString(user.FirstName),
		nullString(user.LastName), nullString(user.LanguageCode), user.IsBot)
	return err
}

func (s *Store) Offset(ctx context.Context) (int64, error) {
	var off int64
	err := s.pool.QueryRow(ctx, `SELECT telegram_offset FROM runtime_state WHERE singleton`).Scan(&off)
	return off, err
}

func (s *Store) AdvanceOffset(ctx context.Context, offset int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE runtime_state SET telegram_offset=GREATEST(telegram_offset,$1), updated_at=now() WHERE singleton`, offset)
	return err
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
