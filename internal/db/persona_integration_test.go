// SPDX-License-Identifier: Apache-2.0
//go:build integration

package db

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// touchAs meets a Telegram person the way the bot does.
func touchAs(t *testing.T, s *Store, id int64, username, first string) Persona {
	t.Helper()
	ctx := context.Background()
	if err := s.Touch(ctx, tg.User{ID: id, Username: username, FirstName: first}, nil); err != nil {
		t.Fatal(err)
	}
	return personaOf(t, s, id)
}

func personaOf(t *testing.T, s *Store, id int64) Persona {
	t.Helper()
	p, ok, err := s.PersonaOfUser(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("persona of %d: %v %v", id, ok, err)
	}
	return p
}

// googleAccount makes a site account with Google alone and its profile.
func googleAccount(t *testing.T, s *Store, sub, name string) (string, Persona) {
	t.Helper()
	ctx := context.Background()
	acc, _, err := s.SignIn(ctx, googleID(sub), "", closedRules)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.EnsureAccountPersona(ctx, acc.ID, name)
	if err != nil {
		t.Fatal(err)
	}
	return acc.ID, p
}

func shownName(t *testing.T, s *Store, id int64) (first string, last *string) {
	t.Helper()
	err := s.pool.QueryRow(context.Background(),
		`SELECT first_name, last_name FROM person_shown WHERE telegram_user_id = $1`, id).Scan(&first, &last)
	if err != nil {
		t.Fatal(err)
	}
	return first, last
}

func movedTo(t *testing.T, s *Store, old string) (string, bool) {
	t.Helper()
	p, moved, ok, err := s.PersonaByUsername(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return "", false
	}
	if !moved {
		t.Fatalf("%q is somebody's own name, not an old one: %+v", old, p)
	}
	return p.Username, true
}

func TestCleanUsername(t *testing.T) {
	for in, want := range map[string]string{"@Oksana_UA": "oksana_ua", "  abc ": "abc", "a_1": "a_1"} {
		if got, err := CleanUsername(in); err != nil || got != want {
			t.Errorf("CleanUsername(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ab", "9lives", "_abc", "has space", "dash-ed", "Оксана", strings.Repeat("a", 33),
		"admin", "Maply", "@telegram", "support"} {
		if got, err := CleanUsername(bad); !errors.Is(err, ErrUsername) {
			t.Errorf("CleanUsername(%q) = %q, %v; want ErrUsername", bad, got, err)
		}
	}
	if _, err := CleanUsername(strings.Repeat("a", 32)); err != nil {
		t.Errorf("32 letters: %v", err)
	}
}

// A person maply meets gets their Telegram username when it is free, else
// one made from their name: its Latin letters and digits.
func TestPersonaUsernameAtFirstSight(t *testing.T) {
	s := testStore(t)

	p := touchAs(t, s, 10, "Oksana_UA", "Oksana")
	if p.Username != "oksana_ua" || p.UsernameBy != UsernameTelegram || p.UserID != 10 || p.Asked || p.Name != "" || p.Face != "" {
		t.Fatalf("a Telegram username is taken over: %+v", p)
	}
	// The same Telegram username cannot be had twice (it cannot be at
	// Telegram, but a username may come from an old name).
	taken := touchAs(t, s, 11, "oksana_ua", "Oksana")
	if taken.UsernameBy != UsernameMaply || !regexp.MustCompile(`^oksana\d{1,6}$`).MatchString(taken.Username) {
		t.Fatalf("a taken username: %+v", taken)
	}

	for i, c := range []struct {
		name  string
		user  string
		first string
		want  string
	}{
		{"none", "", "Оксана", `^oksana\d+$`},
		{"cyrillic", "", "Їжак Щука", `^yizhakshchuka\d+$`},
		{"accent", "", "José", `^jose\d+$`},
		{"too short", "ab", "Zo", `^player\d+$`},
		{"starts with a digit", "9lives", "9lives", `^lives\d+$`},
		{"reserved", "admin", "Admin", `^player\d+$`},
		{"no letters", "", "🙂", `^player\d+$`},
		{"long name", "", strings.Repeat("Z", 40), `^z{20}\d+$`},
	} {
		id := int64(100 + i)
		p := touchAs(t, s, id, c.user, c.first)
		if p.UsernameBy != UsernameMaply || !regexp.MustCompile(c.want).MatchString(p.Username) {
			t.Errorf("%s: %+v, want %s", c.name, p, c.want)
		}
		if _, err := CleanUsername(p.Username); err != nil {
			t.Errorf("%s: %q is not a username: %v", c.name, p.Username, err)
		}
	}
	// Only people: a bot has no profile, and a touch twice makes one.
	if err := s.Touch(context.Background(), tg.User{ID: 900, FirstName: "Bot", IsBot: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.PersonaOfUser(context.Background(), 900); err != nil || ok {
		t.Fatalf("a bot has a profile: %v %v", ok, err)
	}
	touchAs(t, s, 10, "Oksana_UA", "Oksana")
	if n := countOf(t, s, `SELECT count(*) FROM maply.persona WHERE user_id = 10`); n != 1 {
		t.Fatalf("%d profiles for one person", n)
	}
}

// A username from Telegram, or made up, follows the person's Telegram
// username; one they typed never changes by itself. The old one keeps
// leading to them.
func TestPersonaFollowsTelegram(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	p := touchAs(t, s, 20, "first_name", "Anya")
	touchAs(t, s, 20, "first_name", "Anya") // same again: nothing to do
	p = touchAs(t, s, 20, "Second_Name", "Anya")
	if p.Username != "second_name" || p.UsernameBy != UsernameTelegram {
		t.Fatalf("after a Telegram rename: %+v", p)
	}
	if now, ok := movedTo(t, s, "first_name"); !ok || now != "second_name" {
		t.Fatalf("the old name leads to %q %v", now, ok)
	}

	// Made up while there was none; Telegram's own comes later.
	m := touchAs(t, s, 21, "", "Boris")
	if m.UsernameBy != UsernameMaply {
		t.Fatalf("made up: %+v", m)
	}
	m = touchAs(t, s, 21, "boris_tg", "Boris")
	if m.Username != "boris_tg" || m.UsernameBy != UsernameTelegram {
		t.Fatalf("a made-up username gives way to Telegram's: %+v", m)
	}

	// Typed by the person: Telegram's changes do not move it.
	o := touchAs(t, s, 22, "carl_tg", "Carl")
	if err := s.SetUsername(ctx, o.ID, "carl_own"); err != nil {
		t.Fatal(err)
	}
	o = touchAs(t, s, 22, "carl_tg2", "Carl")
	if o.Username != "carl_own" || o.UsernameBy != UsernameOwn {
		t.Fatalf("a typed username followed Telegram: %+v", o)
	}
	if now, ok := movedTo(t, s, "carl_tg"); !ok || now != "carl_own" {
		t.Fatalf("carl_tg leads to %q %v", now, ok)
	}

	// A Telegram username somebody else has is not taken, and no username
	// from Telegram changes nothing.
	d := touchAs(t, s, 23, "dina_tg", "Dina")
	if again := touchAs(t, s, 23, "second_name", "Dina"); again.Username != d.Username || again.UsernameBy != UsernameTelegram {
		t.Fatalf("a taken username moved her: %+v", again)
	}
	if again := touchAs(t, s, 23, "", "Dina"); again.Username != "dina_tg" {
		t.Fatalf("no username at Telegram moved her: %+v", again)
	}
	// A bad one is no change either.
	if again := touchAs(t, s, 23, "ab", "Dina"); again.Username != "dina_tg" {
		t.Fatalf("a username that cannot be one moved her: %+v", again)
	}
}

// Typing a username: the form, a word maply keeps, somebody's, and the
// names people left.
func TestSetUsername(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := touchAs(t, s, 30, "alpha", "Alpha")
	b := touchAs(t, s, 31, "bravo", "Bravo")

	for _, bad := range []string{"", "x", "Bad Name", "admin", "9x9"} {
		if err := s.SetUsername(ctx, a.ID, bad); !errors.Is(err, ErrUsername) {
			t.Errorf("SetUsername(%q) = %v, want ErrUsername", bad, err)
		}
	}
	if err := s.SetUsername(ctx, a.ID, "@Bravo"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("somebody's username: %v", err)
	}
	if got := personaOf(t, s, 30); got.Username != "alpha" {
		t.Fatalf("a refused name changed it: %+v", got)
	}
	if err := s.SetUsername(ctx, "00000000-0000-4000-8000-000000000000", "nobody_here"); !errors.Is(err, ErrNoPersona) {
		t.Fatalf("unknown profile: %v", err)
	}
	// Her own again is no change.
	if err := s.SetUsername(ctx, a.ID, "alpha"); err != nil {
		t.Fatal(err)
	}
	if got := personaOf(t, s, 30); got.UsernameBy != UsernameOwn {
		t.Fatalf("typed her own: %+v", got)
	}

	if free, err := s.UsernameFree(ctx, a.ID, "bravo"); err != nil || free {
		t.Fatalf("bravo is free: %v %v", free, err)
	}
	if free, err := s.UsernameFree(ctx, a.ID, "alpha"); err != nil || !free {
		t.Fatalf("her own is not free to her: %v %v", free, err)
	}
	if free, err := s.UsernameFree(ctx, a.ID, "no good"); err != nil || free {
		t.Fatalf("a bad form is free: %v %v", free, err)
	}

	// Alpha becomes alpha2: alpha leads to her.
	if err := s.SetUsername(ctx, a.ID, "alpha2"); err != nil {
		t.Fatal(err)
	}
	if now, ok := movedTo(t, s, "alpha"); !ok || now != "alpha2" {
		t.Fatalf("alpha leads to %q %v", now, ok)
	}
	if p, moved, ok, err := s.PersonaByUsername(ctx, "ALPHA2"); err != nil || !ok || moved || p.ID != a.ID {
		t.Fatalf("her new name: %+v %v %v %v", p, moved, ok, err)
	}
	// An old name is free to anyone, and to its owner.
	if free, err := s.UsernameFree(ctx, b.ID, "alpha"); err != nil || !free {
		t.Fatalf("an old name is not free: %v %v", free, err)
	}
	// Bravo takes it: it is his, and leads to her no more.
	if err := s.SetUsername(ctx, b.ID, "alpha"); err != nil {
		t.Fatal(err)
	}
	if p, moved, ok, err := s.PersonaByUsername(ctx, "alpha"); err != nil || !ok || moved || p.ID != b.ID {
		t.Fatalf("alpha after Bravo took it: %+v %v %v %v", p, moved, ok, err)
	}
	if n := countOf(t, s, `SELECT count(*) FROM maply.persona_was WHERE username = 'alpha'`); n != 0 {
		t.Fatalf("alpha is both Bravo's and an old name: %d", n)
	}
	// His old bravo leads to him; she cannot take alpha now, but takes
	// bravo, and the old name of hers she goes back to is hers again.
	if now, ok := movedTo(t, s, "bravo"); !ok || now != "alpha" {
		t.Fatalf("bravo leads to %q %v", now, ok)
	}
	if err := s.SetUsername(ctx, a.ID, "alpha"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("alpha is Bravo's: %v", err)
	}
	if err := s.SetUsername(ctx, a.ID, "bravo"); err != nil {
		t.Fatal(err)
	}
	if p, moved, ok, _ := s.PersonaByUsername(ctx, "bravo"); !ok || moved || p.ID != a.ID {
		t.Fatalf("bravo after she took it: %+v %v %v", p, moved, ok)
	}
	// Nothing that was never a username leads anywhere.
	if _, _, ok, err := s.PersonaByUsername(ctx, "nobody_here"); err != nil || ok {
		t.Fatalf("nobody_here: %v %v", ok, err)
	}
	if _, _, ok, err := s.PersonaByUsername(ctx, "no good"); err != nil || ok {
		t.Fatalf("a bad form: %v %v", ok, err)
	}
	// The name Telegram gives her next does not matter: she typed hers.
	touchAs(t, s, 30, "alpha_new", "Alpha")
	if got := personaOf(t, s, 30); got.Username != "bravo" {
		t.Fatalf("a typed username moved: %+v", got)
	}
}

// A person's own name shows over Telegram's wherever a name is read, in the
// site's rows of their account too; removing it brings Telegram's back.
func TestOwnNameShowsEverywhere(t *testing.T) {
	s := testStore(t)
	seedRound(t, s)
	ctx := context.Background()
	d := round.NewDate(2026, 11, 1)
	p := personaOf(t, s, 7)

	early := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	closes := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	if err := s.InsertGuess(ctx, NewGuess{Date: d, UserID: 7, Context: "dm", Pin: geo.Point{Lat: 48, Lon: 2}, AnsweredAt: early, Deadline: closes}); err != nil {
		t.Fatal(err)
	}
	kyiv := scoring.Place{Point: geo.Point{Lat: 50.45, Lon: 30.52}, Country: "UA", Continent: "europe"}
	if _, err := s.ScoreDue(ctx, d, closes, func(p geo.Point) scoring.Score { return scoring.Judge(p, kyiv, nil) }); err != nil {
		t.Fatal(err)
	}
	worldName := func() string {
		w, err := s.World(ctx, d)
		if err != nil || len(w.Entries) != 1 {
			t.Fatalf("world: %+v %v", w, err)
		}
		return w.Entries[0].Name
	}
	// The account of the Telegram person has a site player of its own.
	acc, _, err := s.SignIn(ctx, telegramID(7), "", closedRules)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountSitePlayer(ctx, acc.ID, guestA, ptr("Pin")); err != nil {
		t.Fatal(err)
	}
	sitePlayerName := func() *string {
		var n *string
		if err := s.pool.QueryRow(ctx, `SELECT name FROM site_player WHERE account_id = $1`, acc.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if got := worldName(); got != "Pin" {
		t.Fatalf("Telegram's name: %q", got)
	}
	// Plain and short: CleanName tidies what was typed.
	if err := s.SetOwnName(ctx, p.ID, "  Ксю​ \n Pin  "); err != nil {
		t.Fatal(err)
	}
	if got := worldName(); got != "Ксю Pin" {
		t.Fatalf("own name in the world: %q", got)
	}
	if first, last := shownName(t, s, 7); first != "Ксю Pin" || last != nil {
		t.Fatalf("person_shown: %q %v", first, last)
	}
	if got := personaOf(t, s, 7); got.Name != "Ксю Pin" {
		t.Fatalf("persona: %+v", got)
	}
	if n := sitePlayerName(); n == nil || *n != "Ксю Pin" {
		t.Fatalf("site player: %v", n)
	}
	if tn, err := s.TelegramNameOf(ctx, 7); err != nil || tn != "Pin" {
		t.Fatalf("Telegram's name is gone: %q %v", tn, err)
	}

	// Removed: Telegram's first name is back everywhere.
	if err := s.SetOwnName(ctx, p.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := worldName(); got != "Pin" {
		t.Fatalf("after removing: %q", got)
	}
	if first, _ := shownName(t, s, 7); first != "Pin" {
		t.Fatalf("person_shown after removing: %q", first)
	}
	if n := sitePlayerName(); n == nil || *n != "Pin" {
		t.Fatalf("site player after removing: %v", n)
	}
	if err := s.SetOwnName(ctx, "00000000-0000-4000-8000-000000000000", "x"); !errors.Is(err, ErrNoPersona) {
		t.Fatalf("unknown profile: %v", err)
	}
}

// Telegram's last name goes with Telegram's first: a name of one's own is
// the whole name.
func TestOwnNameHidesTheLastName(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.Touch(ctx, tg.User{ID: 15, FirstName: "Oksana", LastName: "Bondar", Username: "oksana15"}, nil); err != nil {
		t.Fatal(err)
	}
	p := personaOf(t, s, 15)
	if first, last := shownName(t, s, 15); first != "Oksana" || last == nil || *last != "Bondar" {
		t.Fatalf("Telegram's: %q %v", first, last)
	}
	if err := s.SetOwnName(ctx, p.ID, "Ksu"); err != nil {
		t.Fatal(err)
	}
	if first, last := shownName(t, s, 15); first != "Ksu" || last != nil {
		t.Fatalf("own: %q %v", first, last)
	}
}

// For an account with Google alone the name is "Guest" without one of its
// own: removing it clears the site's rows, not a Telegram name back.
func TestOwnNameOfAGoogleAccount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	acc, p := googleAccount(t, s, "g-lena", "Lena")
	if p.AccountID != acc || p.UserID != 0 || p.UsernameBy != UsernameMaply || !regexp.MustCompile(`^lena\d+$`).MatchString(p.Username) {
		t.Fatalf("her profile: %+v", p)
	}
	// Asking again gives the same profile.
	if again, err := s.EnsureAccountPersona(ctx, acc, "Other"); err != nil || again.ID != p.ID {
		t.Fatalf("again: %+v %v", again, err)
	}
	if got, ok, err := s.PersonaOfAccount(ctx, acc); err != nil || !ok || got.ID != p.ID {
		t.Fatalf("of account: %+v %v %v", got, ok, err)
	}
	if _, ok, err := s.PersonaOfAccount(ctx, "not-an-id"); err != nil || ok {
		t.Fatalf("a bad id: %v %v", ok, err)
	}
	if _, err := s.AccountSitePlayer(ctx, acc, guestA, ptr("Lenka")); err != nil {
		t.Fatal(err)
	}
	name := func() *string {
		var n *string
		if err := s.pool.QueryRow(ctx, `SELECT name FROM site_player WHERE id = $1`, guestA).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := s.SetOwnName(ctx, p.ID, "Lena K"); err != nil {
		t.Fatal(err)
	}
	if n := name(); n == nil || *n != "Lena K" {
		t.Fatalf("site player: %v", n)
	}
	if err := s.SetOwnName(ctx, p.ID, ""); err != nil {
		t.Fatal(err)
	}
	if n := name(); n != nil {
		t.Fatalf("site player after removing: %q, want NULL", *n)
	}
	if got, _, _ := s.PersonaOfAccount(ctx, acc); got.Name != "" {
		t.Fatalf("persona keeps %q", got.Name)
	}
}

// An own photo shows over Telegram's by its own address, and removing it
// brings Telegram's back.
func TestOwnPhotoOverTelegrams(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := touchAs(t, s, 40, "photo_man", "Pho")
	other := touchAs(t, s, 41, "no_photo", "Nope")
	at := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	if err := s.SetAvatar(ctx, 40, Avatar{UniqueID: "u1", JPEG: []byte{1}, CheckedAt: at}); err != nil {
		t.Fatal(err)
	}
	tgFace := func() string { a, _, _ := s.AvatarOf(ctx, 40); return a.Face }()
	if faces, _ := s.FacesOf(ctx, []int64{40, 41}); len(faces) != 1 || faces[40] != tgFace {
		t.Fatalf("Telegram's face: %v", faces)
	}

	face, err := s.SetOwnPhoto(ctx, p.ID, []byte{9, 9})
	if err != nil || len(face) != 32 || face == tgFace {
		t.Fatalf("own photo: %q %v", face, err)
	}
	if got := personaOf(t, s, 40); got.Face != face {
		t.Fatalf("persona face %q", got.Face)
	}
	if faces, err := s.FacesOf(ctx, []int64{40, 41}); err != nil || len(faces) != 1 || faces[40] != face {
		t.Fatalf("FacesOf = %v %v", faces, err)
	}
	if photos, err := s.AvatarsOf(ctx, []int64{40, 41}); err != nil || len(photos) != 1 || !bytes.Equal(photos[40], []byte{9, 9}) {
		t.Fatalf("AvatarsOf = %v %v", photos, err)
	}
	if b, ok, err := s.FaceJPEG(ctx, face); err != nil || !ok || !bytes.Equal(b, []byte{9, 9}) {
		t.Fatalf("FaceJPEG = %v %v %v", b, ok, err)
	}
	// Telegram's keeps its own address: a link to it made earlier still works.
	if b, ok, _ := s.FaceJPEG(ctx, tgFace); !ok || !bytes.Equal(b, []byte{1}) {
		t.Fatalf("Telegram's address: %v %v", b, ok)
	}
	own, err := s.OwnPhotos(ctx, []int64{40, 41})
	if err != nil || len(own) != 1 || !bytes.Equal(own[40], []byte{9, 9}) {
		t.Fatalf("OwnPhotos = %v %v", own, err)
	}

	// Another photo, another address; the old one shows nothing.
	next, err := s.SetOwnPhoto(ctx, p.ID, []byte{8})
	if err != nil || next == face {
		t.Fatalf("second photo: %q %v", next, err)
	}
	if _, ok, _ := s.FaceJPEG(ctx, face); ok {
		t.Fatal("the replaced photo is still served")
	}

	// Removed: Telegram's again.
	if got, err := s.SetOwnPhoto(ctx, p.ID, nil); err != nil || got != "" {
		t.Fatalf("remove: %q %v", got, err)
	}
	if faces, _ := s.FacesOf(ctx, []int64{40}); faces[40] != tgFace {
		t.Fatalf("after removing: %v", faces)
	}
	if photos, _ := s.AvatarsOf(ctx, []int64{40}); !bytes.Equal(photos[40], []byte{1}) {
		t.Fatalf("after removing: %v", photos)
	}
	if _, ok, _ := s.FaceJPEG(ctx, next); ok {
		t.Fatal("the removed photo is still served")
	}
	if own, _ := s.OwnPhotos(ctx, []int64{40}); len(own) != 0 {
		t.Fatalf("OwnPhotos after removing: %v", own)
	}

	// A person with no Telegram photo: the own one alone, then none.
	if _, err := s.SetOwnPhoto(ctx, other.ID, []byte{7}); err != nil {
		t.Fatal(err)
	}
	if faces, _ := s.FacesOf(ctx, []int64{41}); len(faces) != 1 {
		t.Fatalf("own photo alone: %v", faces)
	}
	if _, err := s.SetOwnPhoto(ctx, other.ID, nil); err != nil {
		t.Fatal(err)
	}
	if faces, _ := s.FacesOf(ctx, []int64{41}); len(faces) != 0 {
		t.Fatalf("no photo at all: %v", faces)
	}
	if _, err := s.SetOwnPhoto(ctx, "00000000-0000-4000-8000-000000000000", []byte{1}); !errors.Is(err, ErrNoPersona) {
		t.Fatalf("unknown profile: %v", err)
	}
}

// PersonasOf finds a profile by whichever key a row has.
func TestPersonasOf(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tgP := touchAs(t, s, 50, "tg_fifty", "Fifty")
	acc, gp := googleAccount(t, s, "g-ps", "Gee")
	if _, err := s.AccountSitePlayer(ctx, acc, guestA, ptr("Gee")); err != nil {
		t.Fatal(err)
	}
	// A Telegram account's site player shows its person's profile.
	person(t, s, 51, "Fiftyone")
	tAcc, _, err := s.SignIn(ctx, telegramID(51), "", closedRules)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountSitePlayer(ctx, tAcc.ID, guestB, ptr("Fiftyone")); err != nil {
		t.Fatal(err)
	}
	tp := personaOf(t, s, 51)

	got, err := s.PersonasOf(ctx, []int64{50, 51, 999}, []string{guestA, guestB, "junk", guestC}, []string{acc, tAcc.ID, "junk"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ByUser[50].ID != tgP.ID || got.ByUser[51].ID != tp.ID || len(got.ByUser) != 2 {
		t.Fatalf("by user: %+v", got.ByUser)
	}
	if got.ByPlayer[guestA].ID != gp.ID || got.ByPlayer[guestB].ID != tp.ID || len(got.ByPlayer) != 2 {
		t.Fatalf("by player: %+v", got.ByPlayer)
	}
	if got.ByAccount[acc].ID != gp.ID || got.ByAccount[tAcc.ID].ID != tp.ID || len(got.ByAccount) != 2 {
		t.Fatalf("by account: %+v", got.ByAccount)
	}
	if empty, err := s.PersonasOf(ctx, nil, nil, nil); err != nil || len(empty.ByUser)+len(empty.ByPlayer)+len(empty.ByAccount) != 0 {
		t.Fatalf("nothing asked: %+v %v", empty, err)
	}
}

// A site account that gets Telegram has its person's profile from then on:
// the choices it made go over, its username too when the person did not type
// one, and the name it left keeps leading to the person.
func TestLinkingTelegramMovesTheProfile(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// 1. A Google account with a username, name and photo of its own links
	// a Telegram person who follows their Telegram username.
	acc, g := googleAccount(t, s, "g-1", "Lena")
	madeUpName := g.Username
	if err := s.SetUsername(ctx, g.ID, "lena_own"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOwnName(ctx, g.ID, "Lena Own"); err != nil {
		t.Fatal(err)
	}
	gFace, err := s.SetOwnPhoto(ctx, g.ID, []byte{5, 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAsked(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	tp := touchAs(t, s, 60, "lena_tg", "Lenochka")
	if res, err := s.Link(ctx, acc, telegramID(60), closedRules); err != nil || res != Linked {
		t.Fatalf("link: %v %v", res, err)
	}
	got := personaOf(t, s, 60)
	if got.ID != tp.ID || got.Username != "lena_own" || got.UsernameBy != UsernameOwn || got.Name != "Lena Own" || got.Face != gFace || !got.Asked {
		t.Fatalf("after linking: %+v", got)
	}
	if b, ok, _ := s.FaceJPEG(ctx, gFace); !ok || !bytes.Equal(b, []byte{5, 5}) {
		t.Fatalf("her photo: %v %v", b, ok)
	}
	if n := countOf(t, s, `SELECT count(*) FROM maply.persona WHERE account_id = $1`, acc); n != 0 {
		t.Fatalf("the account's own profile is still there: %d", n)
	}
	// The account now has its person's profile, and both old names lead there.
	if of, ok, err := s.PersonaOfAccount(ctx, acc); err != nil || !ok || of.ID != tp.ID {
		t.Fatalf("PersonaOfAccount: %+v %v %v", of, ok, err)
	}
	for _, old := range []string{"lena_tg", madeUpName} {
		if now, ok := movedTo(t, s, old); !ok || now != "lena_own" {
			t.Fatalf("%q leads to %q %v", old, now, ok)
		}
	}

	// 2. The Telegram person's own choices win; the account's fill what
	// they left empty.
	acc2, g2 := googleAccount(t, s, "g-2", "Gala")
	if err := s.SetUsername(ctx, g2.ID, "gala_own"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOwnName(ctx, g2.ID, "Gala"); err != nil {
		t.Fatal(err)
	}
	g2Face, err := s.SetOwnPhoto(ctx, g2.ID, []byte{6})
	if err != nil {
		t.Fatal(err)
	}
	tp2 := touchAs(t, s, 61, "gala_tg", "Galina")
	if err := s.SetUsername(ctx, tp2.ID, "galina_own"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOwnName(ctx, tp2.ID, "Galina"); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Link(ctx, acc2, telegramID(61), closedRules); err != nil || res != Linked {
		t.Fatalf("link: %v %v", res, err)
	}
	got = personaOf(t, s, 61)
	if got.Username != "galina_own" || got.Name != "Galina" || got.Face != g2Face {
		t.Fatalf("their choices lost: %+v", got)
	}
	if got.Asked {
		t.Fatalf("asked from nowhere: %+v", got)
	}
	if now, ok := movedTo(t, s, "gala_own"); !ok || now != "galina_own" {
		t.Fatalf("gala_own leads to %q %v", now, ok)
	}
	// With a photo of theirs, theirs stays too.
	acc3, g3 := googleAccount(t, s, "g-3", "Gus")
	if _, err := s.SetOwnPhoto(ctx, g3.ID, []byte{3}); err != nil {
		t.Fatal(err)
	}
	tp3 := touchAs(t, s, 62, "gus_tg", "Gus")
	mine, err := s.SetOwnPhoto(ctx, tp3.ID, []byte{4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(ctx, acc3, telegramID(62), closedRules); err != nil {
		t.Fatal(err)
	}
	if got := personaOf(t, s, 62); got.Face != mine {
		t.Fatalf("their photo lost: %+v", got)
	}
	if photos, _ := s.AvatarsOf(ctx, []int64{62}); !bytes.Equal(photos[62], []byte{4}) {
		t.Fatalf("photo bytes: %v", photos)
	}

	// 3. A username made up for the account gives way to the one Telegram
	// gives the person.
	acc4, g4 := googleAccount(t, s, "g-4", "Hana")
	tp4 := touchAs(t, s, 63, "hana_tg", "Hana")
	if _, err := s.Link(ctx, acc4, telegramID(63), closedRules); err != nil {
		t.Fatal(err)
	}
	got = personaOf(t, s, 63)
	if got.Username != "hana_tg" || got.UsernameBy != UsernameTelegram || got.ID != tp4.ID {
		t.Fatalf("a made-up username won over Telegram's: %+v", got)
	}
	if now, ok := movedTo(t, s, g4.Username); !ok || now != "hana_tg" {
		t.Fatalf("%q leads to %q %v", g4.Username, now, ok)
	}

	// 4. A person core knows but maply never touched has no profile yet:
	// the account's becomes theirs as it is.
	acc5, g5 := googleAccount(t, s, "g-5", "Ira")
	if err := s.SetOwnName(ctx, g5.ID, "Ira"); err != nil {
		t.Fatal(err)
	}
	exec(t, s, `SELECT core.touch_person('maply', 64, NULL, 'Irina')`)
	if _, ok, _ := s.PersonaOfUser(ctx, 64); ok {
		t.Fatal("the raw core touch made a profile")
	}
	if _, err := s.Link(ctx, acc5, telegramID(64), closedRules); err != nil {
		t.Fatal(err)
	}
	got = personaOf(t, s, 64)
	if got.ID != g5.ID || got.Username != g5.Username || got.Name != "Ira" || got.AccountID != "" {
		t.Fatalf("the account's profile became the person's: %+v", got)
	}

	// 5. An account that never had a profile just links.
	acc6, _, err := s.SignIn(ctx, googleID("g-6"), "", closedRules)
	if err != nil {
		t.Fatal(err)
	}
	person(t, s, 65, "Jo")
	if res, err := s.Link(ctx, acc6.ID, telegramID(65), closedRules); err != nil || res != Linked {
		t.Fatalf("link without a profile: %v %v", res, err)
	}
	if got := personaOf(t, s, 65); got.AccountID != "" {
		t.Fatalf("%+v", got)
	}
}

// Merging a Google account into a Telegram account moves the profile of the
// Google account before it goes with the account it is deleted with.
func TestMergingAGoogleAccountKeepsItsProfile(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tp := touchAs(t, s, 70, "tele_seventy", "Seventy")
	tAcc, _, err := s.SignIn(ctx, telegramID(70), "", closedRules)
	if err != nil {
		t.Fatal(err)
	}
	gAcc, g := googleAccount(t, s, "g-merge", "Gerda")
	if err := s.SetUsername(ctx, g.ID, "gerda_own"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOwnName(ctx, g.ID, "Gerda"); err != nil {
		t.Fatal(err)
	}
	face, err := s.SetOwnPhoto(ctx, g.ID, []byte{2})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.Link(ctx, tAcc.ID, googleID("g-merge"), closedRules)
	if err != nil || res != Merged {
		t.Fatalf("link = %v %v, want a merge", res, err)
	}
	if _, ok, _ := s.AccountByID(ctx, gAcc); ok {
		t.Fatal("the merged account is still there")
	}
	got := personaOf(t, s, 70)
	if got.ID != tp.ID || got.Username != "gerda_own" || got.UsernameBy != UsernameOwn || got.Name != "Gerda" || got.Face != face {
		t.Fatalf("the Google account's profile was lost: %+v", got)
	}
	if n := countOf(t, s, `SELECT count(*) FROM maply.persona WHERE account_id IS NOT NULL`); n != 0 {
		t.Fatalf("%d profiles of accounts left", n)
	}
	for _, old := range []string{"tele_seventy", g.Username} {
		if now, ok := movedTo(t, s, old); !ok || now != "gerda_own" {
			t.Fatalf("%q leads to %q %v", old, now, ok)
		}
	}
	if of, ok, err := s.PersonaOfAccount(ctx, tAcc.ID); err != nil || !ok || of.ID != tp.ID {
		t.Fatalf("PersonaOfAccount: %+v %v %v", of, ok, err)
	}
}

// People met before profiles existed get one at start, once.
func TestBackfillPersonas(t *testing.T) {
	s := testStore(t)
	seedRound(t, s)
	ctx := context.Background()
	// Seven was touched by the store; 80 played, 81 only exists in core.
	exec(t, s, `SELECT core.touch_person('maply', 80, 'Old_Timer', 'Old')`)
	exec(t, s, `SELECT core.touch_person('maply', 81, NULL, 'Silent')`)
	exec(t, s, `INSERT INTO maply.guess (date, user_id, context, lat, lon, answered_at, deadline_at)
	            VALUES ('2026-11-01', 80, 'dm', 48.85, 2.35, '2026-11-01 10:00+03', '2026-11-01 20:00+03')`)
	n, err := s.BackfillPersonas(ctx)
	if err != nil || n != 1 {
		t.Fatalf("backfill = %d %v", n, err)
	}
	if p := personaOf(t, s, 80); p.Username != "old_timer" || p.UsernameBy != UsernameTelegram {
		t.Fatalf("backfilled: %+v", p)
	}
	if _, ok, _ := s.PersonaOfUser(ctx, 81); ok {
		t.Fatal("a person who never played got a profile")
	}
	if n, err := s.BackfillPersonas(ctx); err != nil || n != 0 {
		t.Fatalf("second backfill = %d %v", n, err)
	}
}

// A Google account that links a Telegram sign-in an account of its own
// already has merges with it: the profile still ends up the person's, once.
func TestLinkingTelegramOfAnotherAccountKeepsOneProfile(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tp := touchAs(t, s, 90, "tele_ninety", "Ninety")
	tAcc, _, err := s.SignIn(ctx, telegramID(90), "", closedRules)
	if err != nil {
		t.Fatal(err)
	}
	gAcc, g := googleAccount(t, s, "g-ninety", "Gina")
	if err := s.SetUsername(ctx, g.ID, "gina_own"); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Link(ctx, gAcc, telegramID(90), closedRules); err != nil || res != Merged {
		t.Fatalf("link = %v %v, want a merge", res, err)
	}
	if _, ok, _ := s.AccountByID(ctx, tAcc.ID); ok {
		t.Fatal("the Telegram account is still there")
	}
	got := personaOf(t, s, 90)
	if got.ID != tp.ID || got.Username != "gina_own" {
		t.Fatalf("after the merge: %+v", got)
	}
	if n := countOf(t, s, `SELECT count(*) FROM maply.persona`); n != 1 {
		t.Fatalf("%d profiles for one person", n)
	}
	if of, ok, _ := s.PersonaOfAccount(ctx, gAcc); !ok || of.ID != tp.ID {
		t.Fatalf("PersonaOfAccount: %+v %v", of, ok)
	}
}
