// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"github.com/FreshLabDev/tg"
)

// A persona is a player's profile in maply (migration 029): the address
// /u/<username> and the name and photo they chose over Telegram's. Every
// Telegram person maply meets gets one when it is touched; a site account
// without Telegram gets one at its first sign-in.

// Persona is a player's profile address and their own choices.
type Persona struct {
	ID        string
	UserID    int64
	AccountID string
	Username  string
	// UsernameBy is UsernameTelegram, UsernameMaply or UsernameOwn.
	UsernameBy string
	// Name and Face are the person's own; empty is Telegram's.
	Name string
	Face string
	// Asked is whether the person was asked to choose once already.
	Asked bool
}

const (
	UsernameTelegram = "telegram"
	UsernameMaply    = "maply"
	UsernameOwn      = "own"
)

var (
	// ErrUsername is a username that may not be: its form, or a word maply
	// keeps for itself.
	ErrUsername = errors.New("username not allowed")
	// ErrUsernameTaken is a username another person has.
	ErrUsernameTaken = errors.New("username taken")
	// ErrNoPersona is a profile that is not there.
	ErrNoPersona = errors.New("no such persona")
)

var usernameForm = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

// reservedNames are words a person cannot take: they would read as maply
// itself, or as one of its pages.
var reservedNames = map[string]bool{
	"admin": true, "administrator": true, "anonymous": true, "api": true, "bot": true, "days": true,
	"guest": true, "help": true, "maply": true, "maplyin": true, "maplyin_bot": true, "me": true,
	"moderator": true, "null": true, "official": true, "owner": true, "party": true, "play": true,
	"privacy": true, "profile": true, "root": true, "settings": true, "support": true, "system": true,
	"telegram": true, "google": true, "today": true, "top": true, "undefined": true,
}

// CleanUsername is name as a username: without a leading @, lower case. It
// fails with ErrUsername when the form is wrong or the word is maply's.
func CleanUsername(name string) (string, error) {
	u := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "@"))
	if !usernameForm.MatchString(u) || reservedNames[u] {
		return "", ErrUsername
	}
	return u, nil
}

// cyrillic writes the Cyrillic letters of the languages maply speaks in
// Latin ones, for a username made from a name.
var cyrillic = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'ґ': "g", 'д': "d", 'е': "e", 'є': "ye", 'ё': "yo",
	'ж': "zh", 'з': "z", 'и': "i", 'і': "i", 'ї': "yi", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ў': "u", 'ф': "f",
	'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e",
	'ю': "yu", 'я': "ya",
}

// usernameBase is the Latin letters of a name, at most 20 of them, or
// "player" when it has fewer than three.
func usernameBase(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if lat, ok := cyrillic[r]; ok {
			b.WriteString(lat)
			continue
		}
		// é is e and an accent: the letter stays.
		for _, c := range norm.NFD.String(string(r)) {
			if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' && b.Len() > 0 {
				b.WriteRune(c)
			}
		}
	}
	base := b.String()
	if len(base) > 20 {
		base = base[:20]
	}
	if len(base) < 3 || reservedNames[base] {
		return "player"
	}
	return base
}

// madeUp is a username for someone without a Telegram one: their name's
// letters and a number, so that it rarely takes a real Telegram username.
func madeUp(name string, try int) string {
	digits := int64(10000)
	if try > 5 {
		digits = 1000000
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(digits))
	return fmt.Sprintf("%s%d", usernameBase(name), n.Int64())
}

// telegramUsername is the Telegram username as maply's, "" when it cannot
// be one.
func telegramUsername(u string) string {
	clean, err := CleanUsername(u)
	if err != nil {
		return ""
	}
	return clean
}

// personaCols is the columns scanPersona reads, of the table named alias
// ("" for none).
func personaCols(alias string) string {
	if alias != "" {
		alias += "."
	}
	return strings.ReplaceAll(`@id::text, COALESCE(@user_id, 0), COALESCE(@account_id::text, ''), @username, @username_by,
	COALESCE(@name, ''), COALESCE(@face, ''), @asked_at IS NOT NULL`, "@", alias)
}

var personaColumns = personaCols("")

func scanPersona(row pgx.Row) (Persona, error) {
	var p Persona
	err := row.Scan(&p.ID, &p.UserID, &p.AccountID, &p.Username, &p.UsernameBy, &p.Name, &p.Face, &p.Asked)
	return p, err
}

func (s *Store) personaWhere(ctx context.Context, q querier, where string, arg any) (Persona, bool, error) {
	p, err := scanPersona(q.QueryRow(ctx, `SELECT `+personaColumns+` FROM persona WHERE `+where, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return Persona{}, false, nil
	}
	if err != nil {
		return Persona{}, false, fmt.Errorf("persona: %w", err)
	}
	return p, true, nil
}

// PersonaOfUser is a Telegram person's profile.
func (s *Store) PersonaOfUser(ctx context.Context, userID int64) (Persona, bool, error) {
	return s.personaWhere(ctx, s.pool, `user_id = $1`, userID)
}

// PersonaOfAccount is the profile of a site account: its Telegram person's
// when it has Telegram, else its own.
func (s *Store) PersonaOfAccount(ctx context.Context, accountID string) (Persona, bool, error) {
	if !uuidShape(accountID) {
		return Persona{}, false, nil
	}
	return s.personaWhere(ctx, s.pool, `user_id = (SELECT telegram_user_id FROM account WHERE id = $1::uuid)
		OR account_id = $1::uuid ORDER BY user_id IS NULL LIMIT 1`, accountID)
}

// PersonaByUsername is the profile at /u/<username>. A username its owner
// left still finds them: moved is then true and p.Username the new one.
func (s *Store) PersonaByUsername(ctx context.Context, username string) (p Persona, moved, ok bool, err error) {
	u, err := CleanUsername(username)
	if err != nil {
		return Persona{}, false, false, nil
	}
	if p, ok, err = s.personaWhere(ctx, s.pool, `username = $1`, u); err != nil || ok {
		return p, false, ok, err
	}
	p, ok, err = s.personaWhere(ctx, s.pool, `id = (SELECT persona FROM persona_was WHERE username = $1)`, u)
	return p, ok, ok, err
}

// Personas is the profiles of a page's players, by what each row knows of
// its player.
type Personas struct {
	ByUser    map[int64]Persona
	ByPlayer  map[string]Persona
	ByAccount map[string]Persona
}

// PersonasOf is the profiles of the Telegram people ids, the site players
// players and the accounts accounts. A site player's is its account's, if
// an account holds it, and an account's is its Telegram person's when it has
// Telegram.
func (s *Store) PersonasOf(ctx context.Context, ids []int64, players, accounts []string) (Personas, error) {
	out := Personas{ByUser: map[int64]Persona{}, ByPlayer: map[string]Persona{}, ByAccount: map[string]Persona{}}
	if len(ids) > 0 {
		rows, err := s.pool.Query(ctx, `SELECT `+personaColumns+` FROM persona WHERE user_id = ANY($1::bigint[])`, ids)
		if err != nil {
			return Personas{}, fmt.Errorf("personas: %w", err)
		}
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Persona, error) { return scanPersona(row) })
		if err != nil {
			return Personas{}, fmt.Errorf("personas: %w", err)
		}
		for _, p := range list {
			out.ByUser[p.UserID] = p
		}
	}
	keyed := func(what string, keys []string, join string, into map[string]Persona) error {
		var keep []string
		for _, k := range keys {
			if uuidShape(k) {
				keep = append(keep, k)
			}
		}
		if len(keep) == 0 {
			return nil
		}
		rows, err := s.pool.Query(ctx, `
			SELECT k.id::text, `+personaCols("pa")+`
			  FROM unnest($1::uuid[]) k(id)
			  `+join+`
			  JOIN persona pa ON pa.user_id = a.telegram_user_id OR pa.account_id = a.id
			 -- A Telegram person's profile wins over one an account kept.
			 ORDER BY pa.user_id IS NOT NULL`, keep)
		if err != nil {
			return fmt.Errorf("personas of %s: %w", what, err)
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var p Persona
			if err := rows.Scan(&k, &p.ID, &p.UserID, &p.AccountID, &p.Username, &p.UsernameBy, &p.Name, &p.Face, &p.Asked); err != nil {
				return fmt.Errorf("personas of %s: %w", what, err)
			}
			into[k] = p
		}
		return rows.Err()
	}
	if err := keyed("players", players, `JOIN site_player sp ON sp.id = k.id JOIN account a ON a.id = sp.account_id`, out.ByPlayer); err != nil {
		return Personas{}, err
	}
	if err := keyed("accounts", accounts, `JOIN account a ON a.id = k.id`, out.ByAccount); err != nil {
		return Personas{}, err
	}
	return out, nil
}

// keepPersona makes sure a Telegram person maply has met has a profile, and
// that a username taken from Telegram follows theirs: called with every
// touch, it reads the database only when the Telegram username differs from
// the one this process saw last.
func (s *Store) keepPersona(ctx context.Context, user tg.User) error {
	if user.ID == 0 || user.IsBot {
		return nil
	}
	seen := strings.ToLower(user.Username)
	s.personaMu.Lock()
	if s.personaSeen == nil {
		s.personaSeen = map[int64]string{}
	}
	last, ok := s.personaSeen[user.ID]
	s.personaMu.Unlock()
	if ok && last == seen {
		return nil
	}
	if err := s.EnsurePersona(ctx, user.ID, user.Username, user.FirstName); err != nil {
		return err
	}
	s.personaMu.Lock()
	if len(s.personaSeen) > 50000 {
		clear(s.personaSeen)
	}
	s.personaSeen[user.ID] = seen
	s.personaMu.Unlock()
	return nil
}

// EnsurePersona gives the Telegram person userID a profile if they have
// none, under their Telegram username when it is free, else under one made
// from their name. A username that came from Telegram, or was made up,
// follows a new Telegram username; one the person typed stays.
func (s *Store) EnsurePersona(ctx context.Context, userID int64, tgUsername, firstName string) error {
	want := telegramUsername(tgUsername)
	p, ok, err := s.PersonaOfUser(ctx, userID)
	if err != nil {
		return err
	}
	if ok {
		if want == "" || want == p.Username || p.UsernameBy == UsernameOwn {
			return nil
		}
		err := s.renamePersona(ctx, p.ID, want, UsernameTelegram)
		if errors.Is(err, ErrUsernameTaken) {
			return nil
		}
		return err
	}
	for try := range 12 {
		name, by := want, UsernameTelegram
		if want == "" || try > 0 {
			name, by = madeUp(firstName, try), UsernameMaply
		}
		created, err := s.insertPersona(ctx, `user_id`, userID, name, by)
		if err != nil || created {
			return err
		}
		if _, ok, err := s.PersonaOfUser(ctx, userID); err != nil || ok {
			// Made at the same time by another request.
			return err
		}
	}
	return fmt.Errorf("persona of %d: no free username", userID)
}

// EnsureAccountPersona gives a site account without Telegram a profile, and
// returns it; an account with Telegram has its person's.
func (s *Store) EnsureAccountPersona(ctx context.Context, accountID string, name string) (Persona, error) {
	if p, ok, err := s.PersonaOfAccount(ctx, accountID); err != nil || ok {
		return p, err
	}
	for try := range 12 {
		created, err := s.insertPersona(ctx, `account_id`, accountID, madeUp(name, try), UsernameMaply)
		if err != nil {
			return Persona{}, err
		}
		if p, ok, err := s.PersonaOfAccount(ctx, accountID); err != nil || ok {
			return p, err
		}
		if created {
			// An account with Telegram has its person's profile; one that
			// got Telegram meanwhile is checked again.
			continue
		}
	}
	return Persona{}, fmt.Errorf("persona of account: no free username")
}

// insertPersona adds a profile keyed by col = key. created is false when the
// username or the key was someone's already.
func (s *Store) insertPersona(ctx context.Context, col string, key any, username, by string) (created bool, err error) {
	if _, err := CleanUsername(username); err != nil {
		return false, nil
	}
	cast := "$1::bigint"
	if col == "account_id" {
		cast = "$1::uuid"
	}
	tag, err := s.pool.Exec(ctx, `
		WITH freed AS (DELETE FROM persona_was WHERE username = $2
		                AND NOT EXISTS (SELECT 1 FROM persona WHERE `+col+` = `+cast+`))
		INSERT INTO persona (`+col+`, username, username_by) VALUES (`+cast+`, $2, $3)
		ON CONFLICT DO NOTHING`, key, username, by)
	if err != nil {
		return false, fmt.Errorf("new persona: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// SetUsername gives the profile id the username the person typed. The old
// one keeps leading to them until someone takes it.
func (s *Store) SetUsername(ctx context.Context, id, username string) error {
	u, err := CleanUsername(username)
	if err != nil {
		return err
	}
	return s.renamePersona(ctx, id, u, UsernameOwn)
}

func (s *Store) renamePersona(ctx context.Context, id, username, by string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var old string
	if err := tx.QueryRow(ctx, `SELECT username FROM persona WHERE id = $1::uuid FOR UPDATE`, id).Scan(&old); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPersona
		}
		return fmt.Errorf("rename persona: %w", err)
	}
	if old == username {
		if _, err := tx.Exec(ctx, `UPDATE persona SET username_by = $2, updated_at = now() WHERE id = $1::uuid`, id, by); err != nil {
			return fmt.Errorf("rename persona: %w", err)
		}
		return tx.Commit(ctx)
	}
	// A username its owner left is free; one they left themselves comes back.
	if _, err := tx.Exec(ctx, `DELETE FROM persona_was WHERE username = $1`, username); err != nil {
		return fmt.Errorf("rename persona: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE persona SET username = $2, username_by = $3, updated_at = now()
		 WHERE id = $1::uuid AND NOT EXISTS (SELECT 1 FROM persona WHERE username = $2)`, id, username, by)
	if err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return ErrUsernameTaken
		}
		return fmt.Errorf("rename persona: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUsernameTaken
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO persona_was (username, persona) VALUES ($1, $2::uuid)
		ON CONFLICT (username) DO UPDATE SET persona = EXCLUDED.persona, left_at = now()`, old, id); err != nil {
		return fmt.Errorf("rename persona: %w", err)
	}
	return tx.Commit(ctx)
}

// UsernameFree is whether username may be the profile id's: its own, or
// nobody's but a name someone left.
func (s *Store) UsernameFree(ctx context.Context, id, username string) (bool, error) {
	u, err := CleanUsername(username)
	if err != nil {
		return false, nil
	}
	var free bool
	err = s.pool.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM persona WHERE username = $1 AND id::text <> $2)`, u, id).Scan(&free)
	if err != nil {
		return false, fmt.Errorf("username free: %w", err)
	}
	return free, nil
}

// SetOwnName gives the profile id the person's own name, or takes it away
// with "". The name is the one CleanName keeps. A site account's players
// carry it into the site's tables; without one they are "Guest" again, or a
// Telegram person's first name.
func (s *Store) SetOwnName(ctx context.Context, id, name string) error {
	name = CleanName(name)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var p Persona
	if p, err = scanPersona(tx.QueryRow(ctx, `
		UPDATE persona SET name = NULLIF($2, ''), updated_at = now() WHERE id = $1::uuid
		RETURNING `+personaColumns, id, name)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPersona
		}
		return fmt.Errorf("own name: %w", err)
	}
	// The site's own rows of the account show the same name.
	shown := any(nil)
	if name != "" {
		shown = name
	} else if p.UserID != 0 {
		var first string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(first_name, '') FROM core.person WHERE telegram_user_id = $1`, p.UserID).Scan(&first); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("own name: %w", err)
		}
		if first = CleanName(first); first != "" {
			shown = first
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE site_player SET name = $3
		 WHERE account_id IN (SELECT id FROM account WHERE ($1::bigint <> 0 AND telegram_user_id = $1) OR id::text = $2)`,
		p.UserID, p.AccountID, shown); err != nil {
		return fmt.Errorf("own name: site players: %w", err)
	}
	return tx.Commit(ctx)
}

// SetOwnPhoto gives the profile id the person's own photo, a square JPEG,
// under a new address; nil takes it away and Telegram's shows again.
func (s *Store) SetOwnPhoto(ctx context.Context, id string, jpeg []byte) (face string, err error) {
	var tag interface{ RowsAffected() int64 }
	if jpeg == nil {
		tag, err = s.pool.Exec(ctx, `UPDATE persona SET photo = NULL, face = NULL, updated_at = now() WHERE id = $1::uuid`, id)
	} else {
		face = newFace()
		tag, err = s.pool.Exec(ctx, `UPDATE persona SET photo = $2, face = $3, updated_at = now() WHERE id = $1::uuid`, id, jpeg, face)
	}
	if err != nil {
		return "", fmt.Errorf("own photo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNoPersona
	}
	return face, nil
}

// TelegramNameOf is the first name Telegram gives the person, under any
// name of their own.
func (s *Store) TelegramNameOf(ctx context.Context, userID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(first_name, '') FROM core.person WHERE telegram_user_id = $1`, userID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("telegram name of %d: %w", userID, err)
	}
	return name, nil
}

// MarkAsked records that the person was asked for their name, username and
// photo, whatever they answered.
func (s *Store) MarkAsked(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE persona SET asked_at = COALESCE(asked_at, now()) WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("mark asked: %w", err)
	}
	return nil
}

// OwnPhotos is the people's own photos, for the bot's pictures.
func (s *Store) OwnPhotos(ctx context.Context, ids []int64) (map[int64][]byte, error) {
	out := map[int64][]byte{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id, photo FROM persona WHERE user_id = ANY($1::bigint[]) AND photo IS NOT NULL`, ids)
	if err != nil {
		return nil, fmt.Errorf("own photos: %w", err)
	}
	return collectMap(rows, out, "own photos")
}

// mergePersona moves the profile of the site account accountID onto the
// Telegram person userID's, once the account has Telegram or is merged into
// an account that has: the Telegram person's own choices win, the account's
// fill what they left empty, and a username the person typed for the account
// is kept over one that only came from Telegram. The other username keeps
// leading to them.
func (s *Store) mergePersona(ctx context.Context, tx pgx.Tx, accountID string, userID int64) error {
	from, ok, err := s.personaWhere(ctx, tx, `account_id = $1::uuid FOR UPDATE`, accountID)
	if err != nil || !ok {
		return err
	}
	to, ok, err := s.personaWhere(ctx, tx, `user_id = $1 FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	if !ok {
		_, err := tx.Exec(ctx, `UPDATE persona SET account_id = NULL, user_id = $2, updated_at = now() WHERE id = $1::uuid`, from.ID, userID)
		if err != nil {
			return fmt.Errorf("merge persona: %w", err)
		}
		return nil
	}
	var photo []byte
	if err := tx.QueryRow(ctx, `SELECT photo FROM persona WHERE id = $1::uuid`, from.ID).Scan(&photo); err != nil {
		return fmt.Errorf("merge persona: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_was SET persona = $2::uuid WHERE persona = $1::uuid`, from.ID, to.ID); err != nil {
		return fmt.Errorf("merge persona: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM persona WHERE id = $1::uuid`, from.ID); err != nil {
		return fmt.Errorf("merge persona: %w", err)
	}
	username, by, left := to.Username, to.UsernameBy, from.Username
	if from.UsernameBy == UsernameOwn && to.UsernameBy != UsernameOwn {
		username, by, left = from.Username, UsernameOwn, to.Username
	}
	if _, err := tx.Exec(ctx, `
		UPDATE persona SET username = $2, username_by = $3,
		       name = COALESCE(name, NULLIF($4, '')),
		       photo = CASE WHEN face IS NULL THEN $5 ELSE photo END,
		       face = COALESCE(face, NULLIF($6, '')),
		       asked_at = CASE WHEN $7 THEN COALESCE(asked_at, now()) ELSE asked_at END,
		       updated_at = now()
		 WHERE id = $1::uuid`, to.ID, username, by, from.Name, photo, from.Face, from.Asked); err != nil {
		return fmt.Errorf("merge persona: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO persona_was (username, persona) VALUES ($1, $2::uuid)
		ON CONFLICT (username) DO UPDATE SET persona = EXCLUDED.persona, left_at = now()`, left, to.ID); err != nil {
		return fmt.Errorf("merge persona: %w", err)
	}
	return nil
}

// BackfillPersonas gives a profile to every Telegram person who has played
// maply and has none: the people met before profiles existed. It is cheap
// once done and runs at start.
func (s *Store) BackfillPersonas(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pe.telegram_user_id, COALESCE(pe.username, ''), COALESCE(pe.first_name, '')
		  FROM core.person pe
		 WHERE NOT EXISTS (SELECT 1 FROM persona pa WHERE pa.user_id = pe.telegram_user_id)
		   AND (EXISTS (SELECT 1 FROM guess g WHERE g.user_id = pe.telegram_user_id)
		     OR EXISTS (SELECT 1 FROM account a WHERE a.telegram_user_id = pe.telegram_user_id)
		     OR EXISTS (SELECT 1 FROM free_member m WHERE m.user_id = pe.telegram_user_id)
		     OR EXISTS (SELECT 1 FROM core.maply_presence p WHERE p.telegram_user_id = pe.telegram_user_id))
		 ORDER BY pe.telegram_user_id`)
	if err != nil {
		return 0, fmt.Errorf("backfill personas: %w", err)
	}
	type who struct {
		id          int64
		uname, name string
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (who, error) {
		var w who
		err := row.Scan(&w.id, &w.uname, &w.name)
		return w, err
	})
	if err != nil {
		return 0, fmt.Errorf("backfill personas: %w", err)
	}
	for _, w := range list {
		if err := s.EnsurePersona(ctx, w.id, w.uname, w.name); err != nil {
			return 0, err
		}
	}
	return len(list), nil
}

// newFace is a new photo address: 32 hex digits.
func newFace() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
