// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/FreshLabDev/maply/internal/round"
)

// Accounts on the site (migrations/018_account.sql). An account has at most
// one Telegram sign-in and one Google sign-in, and at least one of them.

// The kinds of sign-in.
const (
	KindTelegram = "telegram"
	KindGoogle   = "google"
)

var (
	// ErrNoAccount is an account that is gone: deleted, or merged into
	// another one.
	ErrNoAccount = errors.New("no such account")
	// ErrLastIdentity is an unlink that would leave the account without a
	// way to sign in.
	ErrLastIdentity = errors.New("the last sign-in of an account cannot be unlinked")
	// ErrNotLinked is an unlink of a sign-in the account does not have.
	ErrNotLinked = errors.New("the account has no such sign-in")
	// ErrTelegramAccount is a site answer for an account with Telegram,
	// which answers as its Telegram player.
	ErrTelegramAccount = errors.New("the account answers as its Telegram player")
)

// Identity is one sign-in: a Telegram user id or a Google sub.
type Identity struct {
	Kind           string
	TelegramUserID int64
	GoogleSub      string
}

func (id Identity) column() string {
	if id.Kind == KindTelegram {
		return "telegram_user_id"
	}
	return "google_sub"
}

func (id Identity) key() any {
	if id.Kind == KindTelegram {
		return id.TelegramUserID
	}
	return id.GoogleSub
}

func (id Identity) valid() bool {
	switch id.Kind {
	case KindTelegram:
		return id.TelegramUserID > 0
	case KindGoogle:
		return id.GoogleSub != "" && len(id.GoogleSub) <= 255
	}
	return false
}

// Account is an account and its sign-ins. Player is where its site answers
// go while it has no Telegram, empty until the first one.
type Account struct {
	ID             string
	TelegramUserID int64
	GoogleSub      string
	Player         string
	CreatedAt      time.Time
}

// Has reports whether the account has a sign-in of kind.
func (a Account) Has(kind string) bool {
	if kind == KindTelegram {
		return a.TelegramUserID != 0
	}
	return a.GoogleSub != ""
}

const accountColumns = `a.id::text, COALESCE(a.telegram_user_id, 0), COALESCE(a.google_sub, ''), COALESCE(a.player::text, ''), a.created_at`

func scanAccount(row pgx.Row) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.TelegramUserID, &a.GoogleSub, &a.Player, &a.CreatedAt)
	return a, err
}

func (s *Store) accountWhere(ctx context.Context, q querier, where string, arg any, lock bool) (Account, bool, error) {
	sql := `SELECT ` + accountColumns + ` FROM account a WHERE ` + where
	if lock {
		sql += ` FOR UPDATE`
	}
	a, err := scanAccount(q.QueryRow(ctx, sql, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, fmt.Errorf("account: %w", err)
	}
	return a, true, nil
}

// AccountByID is the account with id, false when there is none.
func (s *Store) AccountByID(ctx context.Context, id string) (Account, bool, error) {
	if !uuidShape(id) {
		return Account{}, false, nil
	}
	return s.accountWhere(ctx, s.pool, `a.id = $1::uuid`, id, false)
}

// AccountOf is the account a sign-in belongs to, false when none.
func (s *Store) AccountOf(ctx context.Context, id Identity) (Account, bool, error) {
	if !id.valid() {
		return Account{}, false, nil
	}
	return s.accountWhere(ctx, s.pool, `a.`+id.column()+` = $1`, id.key(), false)
}

// Rules decide what happens to the answers of an account that gains a
// Telegram sign-in or takes in a site player.
type Rules struct {
	// Open reports whether d is still being guessed somewhere. A day over in
	// every zone is never rewritten.
	Open func(d round.Date) bool
	// Deadline is when userID's answer window for d closes, from the site:
	// the zone of their DM counted with every chat they play in.
	Deadline func(ctx context.Context, userID int64, d round.Date) (time.Time, error)
	// Now is when the merge happens. A site answer becomes the Telegram one
	// only while the person's window is still open: after it they may have
	// seen the place, and would pick the best of several guests' answers.
	// Zero moves nothing.
	Now time.Time
}

// SignIn finds the account of id, or makes one. A new account takes in
// guest, the browser's site player, when no account has it yet; the guest's
// answers then follow the rules. An existing account takes in nobody: the
// site asks first. A Telegram sign-in needs the person in core already.
func (s *Store) SignIn(ctx context.Context, id Identity, guest string, rules Rules) (acc Account, created bool, err error) {
	if !id.valid() {
		return Account{}, false, fmt.Errorf("sign in: invalid %s identity", id.Kind)
	}
	for range 3 {
		if a, ok, err := s.AccountOf(ctx, id); err != nil || ok {
			return a, false, err
		}
		acc, err = s.newAccount(ctx, id, guest, rules)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Someone else made it first; it is theirs to find now.
			continue
		}
		return acc, err == nil, err
	}
	return Account{}, false, fmt.Errorf("sign in: the account kept changing")
}

func (s *Store) newAccount(ctx context.Context, id Identity, guest string, rules Rules) (Account, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	accID := newUUID()
	if _, err := tx.Exec(ctx, `INSERT INTO account (id, `+id.column()+`) VALUES ($1, $2)`, accID, id.key()); err != nil {
		return Account{}, err
	}
	if uuidShape(guest) {
		if _, err := s.adopt(ctx, tx, accID, guest); err != nil {
			return Account{}, err
		}
	}
	if err := s.settle(ctx, tx, accID, rules); err != nil {
		return Account{}, err
	}
	acc, _, err := s.accountWhere(ctx, tx, `a.id = $1::uuid`, accID, false)
	if err != nil {
		return Account{}, err
	}
	return acc, tx.Commit(ctx)
}

// LinkResult is what a link did.
type LinkResult string

const (
	// Linked: the sign-in was nobody's and is the account's now.
	Linked LinkResult = "linked"
	// Merged: the sign-in was another account's, which had nothing else;
	// that account is now part of this one.
	Merged LinkResult = "merged"
	// AlreadyLinked: the account has a sign-in of this kind already.
	AlreadyLinked LinkResult = "already_linked"
	// Taken: the sign-in is another account's, and both have a sign-in of
	// the other kind. Signing in with that one and unlinking it comes first.
	Taken LinkResult = "taken"
	// Elsewhere: the sign-in is another account's, and this link may not
	// merge (LinkOnly).
	Elsewhere LinkResult = "elsewhere"
)

// Link adds id to the account. When id is another account's and that account
// has no other sign-in, the two merge: its site players and their history
// come over, and it is gone. A Telegram sign-in needs the person in core.
func (s *Store) Link(ctx context.Context, accountID string, id Identity, rules Rules) (LinkResult, error) {
	return s.link(ctx, accountID, id, true, rules)
}

// LinkOnly is Link without the merge: a sign-in that is another account's
// is Elsewhere. The Mini App's ticket links this way, since the browser
// that proves the sign-in may not be the person the ticket was made for.
func (s *Store) LinkOnly(ctx context.Context, accountID string, id Identity, rules Rules) (LinkResult, error) {
	return s.link(ctx, accountID, id, false, rules)
}

func (s *Store) link(ctx context.Context, accountID string, id Identity, merge bool, rules Rules) (LinkResult, error) {
	if !id.valid() || !uuidShape(accountID) {
		return "", fmt.Errorf("link: invalid %s identity", id.Kind)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var other *string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM account WHERE `+id.column()+` = $1`, id.key()).Scan(&other); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("link: %w", err)
	}
	// Both rows are locked in the order of their ids, so two merges of the
	// same pair the other way round wait for each other instead of locking up.
	ids := []string{accountID}
	if other != nil && *other != accountID {
		ids = append(ids, *other)
	}
	rows, err := tx.Query(ctx, `SELECT `+accountColumns+` FROM account a WHERE a.id = ANY($1::uuid[]) ORDER BY a.id FOR UPDATE`, ids)
	if err != nil {
		return "", fmt.Errorf("link: lock: %w", err)
	}
	locked, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Account, error) { return scanAccount(row) })
	if err != nil {
		return "", fmt.Errorf("link: lock: %w", err)
	}
	var a, b *Account
	for i := range locked {
		if locked[i].ID == accountID {
			a = &locked[i]
		} else {
			b = &locked[i]
		}
	}
	if a == nil {
		return "", ErrNoAccount
	}
	if a.Has(id.Kind) {
		return AlreadyLinked, nil
	}
	result := Linked
	if b != nil {
		// b has id; a has the other kind, since it has at least one sign-in.
		otherKind := KindGoogle
		if id.Kind == KindGoogle {
			otherKind = KindTelegram
		}
		if b.Has(otherKind) {
			return Taken, nil
		}
		if !merge {
			return Elsewhere, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE site_player SET account_id = $1 WHERE account_id = $2`, a.ID, b.ID); err != nil {
			return "", fmt.Errorf("link: move players: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE account SET player = COALESCE(player, $2::uuid) WHERE id = $1`, a.ID, nullUUID(b.Player)); err != nil {
			return "", fmt.Errorf("link: main player: %w", err)
		}
		// b without Telegram had a profile of its own, which goes over to
		// a's Telegram person before it would go with b.
		if b.TelegramUserID == 0 && a.TelegramUserID != 0 {
			if err := s.mergePersona(ctx, tx, b.ID, a.TelegramUserID); err != nil {
				return "", err
			}
		}
		// Its sessions and tickets go with it.
		if _, err := tx.Exec(ctx, `DELETE FROM account WHERE id = $1`, b.ID); err != nil {
			return "", fmt.Errorf("link: drop merged account: %w", err)
		}
		result = Merged
	}
	if _, err := tx.Exec(ctx, `UPDATE account SET `+id.column()+` = $2 WHERE id = $1`, a.ID, id.key()); err != nil {
		return "", fmt.Errorf("link: %w", err)
	}
	// An account that gets Telegram has its person's profile from now on,
	// with what it chose itself where the person chose nothing.
	if id.Kind == KindTelegram {
		if err := s.mergePersona(ctx, tx, a.ID, id.TelegramUserID); err != nil {
			return "", err
		}
	}
	if err := s.unifyNames(ctx, tx, a.ID); err != nil {
		return "", err
	}
	if err := s.settle(ctx, tx, a.ID, rules); err != nil {
		return "", err
	}
	return result, tx.Commit(ctx)
}

// Unlink removes the account's sign-in of kind. The last one stays. The
// sessions it opened and the account's tickets end with it, in the same
// transaction: a sign-in linked by someone else is unlinked to shut them
// out.
func (s *Store) Unlink(ctx context.Context, accountID, kind string) error {
	col := Identity{Kind: kind}.column()
	if kind != KindTelegram && kind != KindGoogle {
		return ErrNotLinked
	}
	a, ok, err := s.AccountByID(ctx, accountID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoAccount
	}
	if !a.Has(kind) {
		return ErrNotLinked
	}
	other := "google_sub"
	if kind == KindGoogle {
		other = "telegram_user_id"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE account SET `+col+` = NULL WHERE id = $1 AND `+other+` IS NOT NULL`, accountID)
	if err != nil {
		return fmt.Errorf("unlink: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLastIdentity
	}
	if _, err := tx.Exec(ctx, `DELETE FROM session WHERE account_id = $1 AND via = $2`, accountID, kind); err != nil {
		return fmt.Errorf("unlink: sessions: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM link_ticket WHERE account_id = $1`, accountID); err != nil {
		return fmt.Errorf("unlink: tickets: %w", err)
	}
	return tx.Commit(ctx)
}

// AdoptGuest takes the browser's site player into the account, when no
// account has it yet, and says how many answers came with it.
func (s *Store) AdoptGuest(ctx context.Context, accountID, guest string, rules Rules) (int, error) {
	if !uuidShape(guest) || !uuidShape(accountID) {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, ok, err := s.accountWhere(ctx, tx, `a.id = $1::uuid`, accountID, true); err != nil {
		return 0, err
	} else if !ok {
		return 0, ErrNoAccount
	}
	n, err := s.adopt(ctx, tx, accountID, guest)
	if err != nil {
		return 0, err
	}
	if err := s.unifyNames(ctx, tx, accountID); err != nil {
		return 0, err
	}
	if err := s.settle(ctx, tx, accountID, rules); err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

// adopt moves an unowned site player into the account and makes it the
// account's main one if it has none.
func (s *Store) adopt(ctx context.Context, tx pgx.Tx, accountID, guest string) (int, error) {
	tag, err := tx.Exec(ctx, `UPDATE site_player SET account_id = $1 WHERE id = $2 AND account_id IS NULL`, accountID, guest)
	if err != nil {
		return 0, fmt.Errorf("adopt guest: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE account SET player = COALESCE(player, $2::uuid) WHERE id = $1`, accountID, guest); err != nil {
		return 0, fmt.Errorf("adopt guest: %w", err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM site_guess WHERE player = $1`, guest).Scan(&n); err != nil {
		return 0, fmt.Errorf("adopt guest: %w", err)
	}
	return n, nil
}

// unifyNames gives every site player of the account one name: the main
// player's, or the first one any of them had.
func (s *Store) unifyNames(ctx context.Context, tx pgx.Tx, accountID string) error {
	if _, err := tx.Exec(ctx, `
		WITH n AS (
		  SELECT COALESCE(
		           (SELECT p.name FROM account a JOIN site_player p ON p.id = a.player WHERE a.id = $1),
		           (SELECT name FROM site_player WHERE account_id = $1 AND name IS NOT NULL ORDER BY created_at, id LIMIT 1)) AS name)
		UPDATE site_player sp SET name = n.name FROM n
		 WHERE sp.account_id = $1 AND n.name IS NOT NULL AND sp.name IS DISTINCT FROM n.name`, accountID); err != nil {
		return fmt.Errorf("unify names: %w", err)
	}
	return nil
}

// settle makes the account's open days hold one answer each. A day over in
// every zone is left as it is, even with two rows of one person in it. In an
// open day the earliest site answer stays. When the account has Telegram and
// that answer came before the Telegram deadline, it was given blind, and it
// becomes the Telegram answer; a Telegram answer already there wins, and the
// site's go. One after the deadline stays a site row, below every Telegram
// row: by then the person may have seen the place. It is marked late too,
// as an answer given then would be, so it never counts as the person's day:
// which of several browsers to sign in from may have been picked knowing
// the place.
func (s *Store) settle(ctx context.Context, tx pgx.Tx, accountID string, rules Rules) error {
	var tgID int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(telegram_user_id, 0) FROM account WHERE id = $1`, accountID).Scan(&tgID); err != nil {
		return fmt.Errorf("settle: %w", err)
	}
	if tgID != 0 {
		// An answer in a chat at this moment waits, then sees the rows this
		// one keeps; or it went in first, and the query below sees it.
		if err := lockPerson(ctx, tx, tgID); err != nil {
			return fmt.Errorf("settle: %w", err)
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT sg.date, sg.player::text, sg.lat, sg.lon, sg.answered_at
		  FROM site_guess sg JOIN site_player sp ON sp.id = sg.player
		 WHERE sp.account_id = $1
		   AND NOT EXISTS (SELECT 1 FROM day_closed c WHERE c.date = sg.date)
		 ORDER BY sg.date, sg.answered_at, sg.player
		   FOR UPDATE OF sg`, accountID)
	if err != nil {
		return fmt.Errorf("settle: %w", err)
	}
	type answer struct {
		date     time.Time
		player   string
		lat, lon float64
		at       time.Time
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (answer, error) {
		var x answer
		err := row.Scan(&x.date, &x.player, &x.lat, &x.lon, &x.at)
		return x, err
	})
	if err != nil {
		return fmt.Errorf("settle: %w", err)
	}
	drop := func(x answer) error {
		_, err := tx.Exec(ctx, `DELETE FROM site_guess WHERE date = $1 AND player = $2`, x.date, x.player)
		return err
	}
	for i, x := range list {
		d := round.DateIn(x.date, time.UTC)
		if rules.Open == nil || !rules.Open(d) {
			continue
		}
		if i > 0 && list[i-1].date.Equal(x.date) {
			// A later answer of the same day: the earliest one stays.
			if err := drop(x); err != nil {
				return fmt.Errorf("settle: drop a later answer: %w", err)
			}
			continue
		}
		if tgID == 0 || rules.Deadline == nil {
			continue
		}
		var answered bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM guess WHERE date = $1 AND user_id = $2)`, x.date, tgID).Scan(&answered); err != nil {
			return fmt.Errorf("settle: %w", err)
		}
		if !answered {
			deadline, err := rules.Deadline(ctx, tgID, d)
			if err != nil {
				return fmt.Errorf("settle: deadline: %w", err)
			}
			if x.at.After(deadline) || rules.Now.IsZero() || !rules.Now.Before(deadline) {
				if !rules.Now.IsZero() {
					if _, err := tx.Exec(ctx, `UPDATE site_guess SET late = true WHERE date = $1 AND player = $2 AND NOT late`, x.date, x.player); err != nil {
						return fmt.Errorf("settle: mark late: %w", err)
					}
				}
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO guess (date, user_id, context, chat_id, lat, lon, answered_at, deadline_at)
				VALUES ($1, $2, 'site', NULL, $3, $4, $5, $6)
				ON CONFLICT (date, user_id) DO NOTHING`, x.date, tgID, x.lat, x.lon, x.at, deadline); err != nil {
				return fmt.Errorf("settle: move to telegram: %w", err)
			}
		}
		if err := drop(x); err != nil {
			return fmt.Errorf("settle: drop the site row: %w", err)
		}
	}
	return nil
}

// DeleteAccount removes the account, its sign-ins, sessions and tickets. Its
// site players stay, without a name: their rows read "Guest" and keep their
// places in every ranking. A free game keeps the name its players had when
// they joined, so the account's seats there take guest, the word a nameless
// account plays under. Telegram answers stay too: they are the person's game
// in the bot.
func (s *Store) DeleteAccount(ctx context.Context, accountID, guest string) error {
	if !uuidShape(accountID) {
		return ErrNoAccount
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The row is held first, so a merge into it waits and its players are
	// renamed too, or the merge comes first and finds no account.
	var found bool
	if err := tx.QueryRow(ctx, `SELECT true FROM account WHERE id = $1 FOR UPDATE`, accountID).Scan(&found); errors.Is(err, pgx.ErrNoRows) {
		return ErrNoAccount
	} else if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	if guest == "" || len([]rune(guest)) > 64 {
		guest = "?"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE free_member SET name = $2
		 WHERE player IN (SELECT id FROM site_player WHERE account_id = $1) AND name <> $2`, accountID, guest); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE site_player SET name = NULL, account_id = NULL WHERE account_id = $1`, accountID); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM account WHERE id = $1`, accountID)
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoAccount
	}
	return tx.Commit(ctx)
}

// AccountPlayers is every site player the account has taken in, oldest first.
func (s *Store) AccountPlayers(ctx context.Context, accountID string) ([]string, error) {
	if !uuidShape(accountID) {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM site_player WHERE account_id = $1 ORDER BY created_at, id`, accountID)
	if err != nil {
		return nil, fmt.Errorf("account players: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// AccountSitePlayer is the account's main site player, made as id on the
// first site answer. A name sets the name of every site player of the
// account, a blank one clears it, nil leaves it.
func (s *Store) AccountSitePlayer(ctx context.Context, accountID, id string, name *string) (string, error) {
	var n any
	if name != nil {
		if clean := CleanName(*name); clean != "" {
			n = clean
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	a, ok, err := s.accountWhere(ctx, tx, `a.id = $1::uuid`, accountID, true)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoAccount
	}
	player := a.Player
	if player == "" {
		player = id
		if _, err := tx.Exec(ctx, `INSERT INTO site_player (id, name, account_id) VALUES ($1, $2, $3)`, player, n, accountID); err != nil {
			return "", fmt.Errorf("account site player: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE account SET player = $2 WHERE id = $1`, accountID, player); err != nil {
			return "", fmt.Errorf("account site player: %w", err)
		}
	}
	if name != nil {
		if _, err := tx.Exec(ctx, `UPDATE site_player SET name = $2 WHERE account_id = $1`, accountID, n); err != nil {
			return "", fmt.Errorf("account name: %w", err)
		}
	}
	return player, tx.Commit(ctx)
}

// SetAccountName names every site player of the account; blank is "Guest".
// An account without one keeps nothing: the name comes with its first answer.
func (s *Store) SetAccountName(ctx context.Context, accountID, name string) error {
	var n any
	if clean := CleanName(name); clean != "" {
		n = clean
	}
	if _, err := s.pool.Exec(ctx, `UPDATE site_player SET name = $2 WHERE account_id = $1`, accountID, n); err != nil {
		return fmt.Errorf("account name: %w", err)
	}
	return nil
}

// GuestOf is a site player no account has, with how many answers it has
// given. ok is false for a player that is unknown or is an account's.
func (s *Store) GuestOf(ctx context.Context, player string) (answers int, ok bool, err error) {
	if !uuidShape(player) {
		return 0, false, nil
	}
	err = s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM site_guess WHERE player = sp.id) FROM site_player sp
		 WHERE sp.id = $1 AND sp.account_id IS NULL`, player).Scan(&answers)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("guest: %w", err)
	}
	return answers, true, nil
}

// PersonKnown reports whether core has the Telegram user already.
func (s *Store) PersonKnown(ctx context.Context, userID int64) (bool, error) {
	var known bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM core.person WHERE telegram_user_id = $1)`, userID).Scan(&known); err != nil {
		return false, fmt.Errorf("person known: %w", err)
	}
	return known, nil
}

// PlayerOwned reports whether an account has taken the site player in: a
// guest cookie that points at one opens nothing.
func (s *Store) PlayerOwned(ctx context.Context, player string) (bool, error) {
	var owned bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM site_player WHERE id = $1 AND account_id IS NOT NULL)`, player).Scan(&owned)
	if err != nil {
		return false, fmt.Errorf("player owned: %w", err)
	}
	return owned, nil
}

// AccountView is what the account's page shows of it.
type AccountView struct {
	Account
	// TelegramName is the linked Telegram player's name as maply shows it,
	// their own one over Telegram's, and TelegramUsername Telegram's: for
	// the account's own page only.
	TelegramName     string
	TelegramUsername string
	// Name is what the site's rows of the account are called, empty for
	// "Guest".
	Name string
}

// AccountView reads the account for its own page.
func (s *Store) AccountView(ctx context.Context, accountID string) (AccountView, bool, error) {
	if !uuidShape(accountID) {
		return AccountView{}, false, nil
	}
	var v AccountView
	err := s.pool.QueryRow(ctx, `
		SELECT `+accountColumns+`,
		       btrim(COALESCE(pe.first_name, '') || ' ' || COALESCE(pe.last_name, '')), COALESCE(pe.username, ''),
		       COALESCE((SELECT p.name FROM site_player p WHERE p.id = a.player), '')
		  FROM account a
		  LEFT JOIN person_shown pe ON pe.telegram_user_id = a.telegram_user_id
		 WHERE a.id = $1`, accountID).
		Scan(&v.ID, &v.TelegramUserID, &v.GoogleSub, &v.Player, &v.CreatedAt, &v.TelegramName, &v.TelegramUsername, &v.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountView{}, false, nil
	}
	if err != nil {
		return AccountView{}, false, fmt.Errorf("account view: %w", err)
	}
	return v, true, nil
}

// Session is a browser's session row. Hash is the SHA-256 of the secret the
// browser holds; the secret itself is kept nowhere.
type Session struct {
	Hash      [32]byte
	AccountID string
	Via       string
	Created   time.Time
	Expires   time.Time
}

// SessionAccount is a live session with the account it opens.
type SessionAccount struct {
	Session
	Account Account
}

// CreateSession stores a session.
func (s *Store) CreateSession(ctx context.Context, x Session) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO session (token_hash, account_id, via, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		x.Hash[:], x.AccountID, x.Via, x.Created, x.Expires); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// LookupSession finds the session with this hash that has not expired at now.
func (s *Store) LookupSession(ctx context.Context, hash [32]byte, now time.Time) (SessionAccount, bool, error) {
	x := SessionAccount{Session: Session{Hash: hash}}
	err := s.pool.QueryRow(ctx, `
		SELECT s.account_id::text, s.via, s.created_at, s.expires_at, `+accountColumns+`
		  FROM session s JOIN account a ON a.id = s.account_id
		 WHERE s.token_hash = $1 AND s.expires_at > $2`, hash[:], now).
		Scan(&x.AccountID, &x.Via, &x.Created, &x.Expires,
			&x.Account.ID, &x.Account.TelegramUserID, &x.Account.GoogleSub, &x.Account.Player, &x.Account.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionAccount{}, false, nil
	}
	if err != nil {
		return SessionAccount{}, false, fmt.Errorf("session: %w", err)
	}
	return x, true, nil
}

// DeleteSession ends one session; one that is gone is no error.
func (s *Store) DeleteSession(ctx context.Context, hash [32]byte) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM session WHERE token_hash = $1`, hash[:]); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteSessions ends every session of the account, or with via set only
// those started by that sign-in, and says how many there were.
func (s *Store) DeleteSessions(ctx context.Context, accountID, via string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM session WHERE account_id = $1 AND ($2 = '' OR via = $2)`, accountID, via)
	if err != nil {
		return 0, fmt.Errorf("delete sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteExpired removes the sessions and tickets that expired before.
func (s *Store) DeleteExpired(ctx context.Context, before time.Time) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM session WHERE expires_at < $1`, before); err != nil {
		return fmt.Errorf("sweep sessions: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM link_ticket WHERE expires_at < $1`, before); err != nil {
		return fmt.Errorf("sweep tickets: %w", err)
	}
	return nil
}

// CreateTicket stores a link ticket for the account.
func (s *Store) CreateTicket(ctx context.Context, hash [32]byte, accountID string, now, expires time.Time) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO link_ticket (token_hash, account_id, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
		hash[:], accountID, now, expires); err != nil {
		return fmt.Errorf("create ticket: %w", err)
	}
	return nil
}

// PeekTicket is the account of a ticket that is unused and live at now,
// without using it.
func (s *Store) PeekTicket(ctx context.Context, hash [32]byte, now time.Time) (string, bool, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT account_id::text FROM link_ticket WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2`, hash[:], now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("ticket: %w", err)
	}
	return id, true, nil
}

// TicketHeld reports whether the account still has the ticket, used or
// not. A sign-out everywhere and an unlink drop the account's tickets, and
// with them a link the ticket started.
func (s *Store) TicketHeld(ctx context.Context, hash [32]byte, accountID string) (bool, error) {
	if !uuidShape(accountID) {
		return false, nil
	}
	var held bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM link_ticket WHERE token_hash = $1 AND account_id = $2)`, hash[:], accountID).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("ticket: %w", err)
	}
	return held, nil
}

// DeleteTickets drops every ticket of the account.
func (s *Store) DeleteTickets(ctx context.Context, accountID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM link_ticket WHERE account_id = $1`, accountID); err != nil {
		return fmt.Errorf("delete tickets: %w", err)
	}
	return nil
}

// UseTicket uses a live ticket once and says whose account it was for.
func (s *Store) UseTicket(ctx context.Context, hash [32]byte, now time.Time) (string, bool, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		UPDATE link_ticket SET used_at = $2
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING account_id::text`, hash[:], now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("use ticket: %w", err)
	}
	return id, true, nil
}

// AccountSiteGuessOf is SiteGuessOf for every player of an account: its answer of
// d from any of them, the earliest when there are two.
func (s *Store) AccountSiteGuessOf(ctx context.Context, d round.Date, accountID string) (SiteGuess, bool, error) {
	var g SiteGuess
	err := s.pool.QueryRow(ctx, `
		SELECT sg.lat, sg.lon, sg.km, sg.pts_km, sg.pts_bonus
		  FROM site_guess sg JOIN site_player sp ON sp.id = sg.player
		 WHERE sg.date = $1 AND sp.account_id = $2
		 ORDER BY sg.answered_at LIMIT 1`, d.Time(), accountID).
		Scan(&g.Pin.Lat, &g.Pin.Lon, &g.Km, &g.PtsKm, &g.PtsBonus)
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteGuess{}, false, nil
	}
	if err != nil {
		return SiteGuess{}, false, fmt.Errorf("account site guess: %w", err)
	}
	return g, true, nil
}

// InsertAccountSiteGuess is InsertSiteGuess for a site player of the
// account. The account's row is held while the answer goes in: a link of
// Telegram at the same moment either comes first, and the answer is
// refused, or waits and settles it. An account with Telegram answers as its
// Telegram player, never as a site player beside it.
func (s *Store) InsertAccountSiteGuess(ctx context.Context, accountID string, g NewSiteGuess) error {
	if !uuidShape(accountID) {
		return ErrNoAccount
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var telegram bool
	err = tx.QueryRow(ctx, `SELECT telegram_user_id IS NOT NULL FROM account WHERE id = $1 FOR SHARE`, accountID).Scan(&telegram)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoAccount
	}
	if err != nil {
		return fmt.Errorf("account site guess: %w", err)
	}
	if telegram {
		return ErrTelegramAccount
	}
	var answered bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM site_guess sg JOIN site_player sp ON sp.id = sg.player
		                WHERE sg.date = $1 AND sp.account_id = $2)`, g.Date.Time(), accountID).Scan(&answered); err != nil {
		return fmt.Errorf("account site guess: %w", err)
	}
	if answered {
		return ErrAlreadyAnswered
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO site_guess (date, player, lat, lon, answered_at, deadline_at, late)
		SELECT $1, $2, $3, $4, $5, $6, $7 FROM site_player WHERE id = $2 AND account_id = $8
		ON CONFLICT (date, player) DO NOTHING`,
		g.Date.Time(), g.Player, g.Pin.Lat, g.Pin.Lon, g.AnsweredAt, g.Deadline, g.Late, accountID)
	if err != nil {
		return fmt.Errorf("account site guess: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyAnswered
	}
	return tx.Commit(ctx)
}

func uuidShape(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
				return false
			}
		}
	}
	return true
}

func nullUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// newUUID is a random version 4 UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
