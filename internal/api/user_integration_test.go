// SPDX-License-Identifier: Apache-2.0
//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FreshLabDev/maply/internal/avatar"
	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/free"
	"github.com/FreshLabDev/maply/internal/round"
)

// pngOf is a w x h picture, as a PNG.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 8), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func photoJSON(raw []byte) string {
	return `{"photo":"` + base64.StdEncoding.EncodeToString(raw) + `"}`
}

// editResponse is what a change answers: the editor and the person as
// every list now shows them.
type editResponse struct {
	Edit   editView `json:"edit"`
	Person rowView  `json:"person"`
}

// visit is a request from a browser that has not signed in.
func (hs *harness) visit(method, path, body string) (int, string) {
	hs.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

var faceURL = regexp.MustCompile(`^/face/[0-9a-f]{32}\.jpg$`)

// face fetches a photo by its address, and says its picture's size.
func (hs *harness) face(path string) (int, image.Config) {
	hs.t.Helper()
	rr := httptest.NewRecorder()
	hs.h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
	if rr.Code != http.StatusOK {
		return rr.Code, image.Config{}
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/jpeg" {
		hs.t.Fatalf("%s is %s", path, ct)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(rr.Body.Bytes()))
	if err != nil {
		hs.t.Fatalf("%s is not a JPEG: %v", path, err)
	}
	return rr.Code, cfg
}

// A profile at its username: the owner gets the whole profile, anyone else
// what a stranger may see, a site visitor who has not signed in no photo,
// and a username its owner left says where they are now. An account without
// Telegram has one too.
func TestUserPageForOwnerStrangerAndGuest(t *testing.T) {
	hs := setup(t)
	store := hs.srv.Store.(*db.Store)
	hs.srv.Avatars = &avatar.Service{Store: store}
	hs.srv.Stills = &stillsSeen{}
	const lucia = "3f6b1a2c-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	hs.seedPast(2, "EG", "africa", pastAnswer{user: 1, km: 400, points: 2500, bonus: 300}, pastAnswer{player: lucia, km: 10, points: 4900, bonus: 1500})
	hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500}, pastAnswer{player: lucia, km: 700, points: 2000, bonus: 300})
	oksana, ivan := initData(1, "Oksana", "uk", hs.payload(kyivGroup)), initData(2, "Ivan", "ru", "")
	hs.setUsername(oksana, "oksana_k")
	// A Telegram photo for her, so that her own one has something to
	// replace.
	if err := store.SetAvatar(context.Background(), 1, db.Avatar{UniqueID: "u1", JPEG: []byte{0xff, 0xd8, 1}, CheckedAt: hs.now}); err != nil {
		t.Fatal(err)
	}
	guest := func(path string) (int, string) { return hs.visit("GET", path, "") }

	// The account with Google alone: its profile begins when it first looks.
	token, _ := hs.signInGoogle("g-lucia", lucia)
	first := decode[ownView](t, hs.siteExpect("GET", "/api/profile", token, "", http.StatusOK))
	if first.Edit == nil || !first.Edit.Ask || first.Edit.Telegram != "" || !regexp.MustCompile(`^[a-z]+\d+$`).MatchString(first.Edit.Username) {
		t.Fatalf("her first look = %+v", first.Edit)
	}
	asked := decode[editResponse](t, hs.siteExpect("POST", "/api/profile/edit", token, `{"username":"Lucia_G","name":"Lucía","asked":true}`, http.StatusOK))
	if asked.Edit.Username != "lucia_g" || asked.Edit.Name != "Lucía" || asked.Edit.Ask || asked.Person.Name != "Lucía" || asked.Person.U != "lucia_g" || !asked.Person.Me {
		t.Fatalf("her edit = %+v", asked)
	}
	var siteName string
	if err := hs.pool.QueryRow(context.Background(), `SELECT name FROM maply.site_player WHERE id = $1`, lucia).Scan(&siteName); err != nil || siteName != "Lucía" {
		t.Fatalf("the site player's name = %q %v", siteName, err)
	}

	// The owner at her address is /api/profile, edit included.
	if own, page := hs.siteExpect("GET", "/api/profile", token, "", http.StatusOK), hs.siteExpect("GET", "/api/u/lucia_g", token, "", http.StatusOK); own != page || !strings.Contains(page, `"edit"`) {
		t.Fatalf("her address and her profile differ:\n%s\n%s", page, own)
	}
	// A Telegram player at hers, from the Mini App.
	if own, page := hs.expect("GET", "/api/profile", oksana, "", http.StatusOK), hs.expect("GET", "/api/u/oksana_k", oksana, "", http.StatusOK); own != page {
		t.Fatalf("her address and her profile differ:\n%s\n%s", page, own)
	}

	// Everyone else gets the same stranger's view: days over everywhere,
	// the history, the free play; no edit, no settings, no percentile.
	check := func(who, body string, name, u string) userView {
		t.Helper()
		v := decode[userView](t, body)
		if v.Name != name || v.U != u || v.Me || v.Ranked.Days != 2 || len(v.Ranked.History) != 2 || v.Ranked.Percentile != nil || v.Duel != nil || v.Free == nil {
			t.Fatalf("%s sees %+v", who, v)
		}
		for _, leak := range []string{`"edit"`, `"settings"`, `"guest"`, `"ask"`, `"account"`, `"player"`, `"telegram"`, "hide_me", hs.date.String()} {
			if strings.Contains(body, leak) {
				t.Fatalf("%s sees %s: %s", who, leak, body)
			}
		}
		return v
	}
	code, body := hs.do("GET", "/api/u/lucia_g", ivan, "")
	if code != http.StatusOK {
		t.Fatalf("Ivan at lucia_g = %d %s", code, body)
	}
	check("Ivan", body, "Lucía", "lucia_g")
	code, body = guest("/api/u/lucia_g")
	if code != http.StatusOK {
		t.Fatalf("a visitor at lucia_g = %d %s", code, body)
	}
	check("a visitor", body, "Lucía", "lucia_g")
	check("Ivan", hs.expect("GET", "/api/u/oksana_k", ivan, "", http.StatusOK), "Oksana", "oksana_k")

	// Photos: a signed-in viewer sees the Telegram one, a visitor none.
	tgPhoto := decode[userView](t, hs.expect("GET", "/api/u/oksana_k", ivan, "", http.StatusOK)).Photo
	if !faceURL.MatchString(tgPhoto) {
		t.Fatalf("Telegram's photo = %q", tgPhoto)
	}
	if _, body := guest("/api/u/oksana_k"); strings.Contains(body, `"photo"`) {
		t.Fatalf("a visitor sees a photo: %s", body)
	}
	own := decode[editResponse](t, hs.expect("POST", "/api/profile/photo", oksana, photoJSON(pngOf(t, 60, 40)), http.StatusOK))
	if !own.Edit.Photo || !faceURL.MatchString(own.Person.Photo) || own.Person.Photo == tgPhoto {
		t.Fatalf("her photo = %+v", own)
	}
	if got := decode[userView](t, hs.expect("GET", "/api/u/oksana_k", ivan, "", http.StatusOK)); got.Photo != own.Person.Photo {
		t.Fatalf("Ivan sees %q, not her own %q", got.Photo, own.Person.Photo)
	}
	if _, body := guest("/api/u/oksana_k"); strings.Contains(body, `"photo"`) || strings.Contains(body, own.Person.Photo) {
		t.Fatalf("a visitor sees her own photo: %s", body)
	}
	// Signed in on the site, a visitor is a viewer: the site account's own
	// photo for the stranger's view of Lucia too.
	lucP := decode[editResponse](t, hs.siteExpect("POST", "/api/profile/photo", token, photoJSON(pngOf(t, 40, 40)), http.StatusOK))
	if !faceURL.MatchString(lucP.Person.Photo) {
		t.Fatalf("Lucia's photo = %+v", lucP)
	}
	if got := decode[userView](t, hs.expect("GET", "/api/u/lucia_g", ivan, "", http.StatusOK)); got.Photo != lucP.Person.Photo {
		t.Fatalf("Ivan sees %q, not hers %q", got.Photo, lucP.Person.Photo)
	}
	if _, body := guest("/api/u/lucia_g"); strings.Contains(body, `"photo"`) {
		t.Fatalf("a visitor sees an account's photo: %s", body)
	}
	// Opened by the site, signed in with Telegram or with Google alone,
	// as by the Mini App.
	oksanaSite := hs.signIn(1, "Oksana")
	if got := decode[userView](t, hs.siteExpect("GET", "/api/u/lucia_g", oksanaSite, "", http.StatusOK)); got.Photo != lucP.Person.Photo {
		t.Fatalf("a signed-in site viewer sees %q", got.Photo)
	}
	if got := decode[userView](t, hs.siteExpect("GET", "/api/u/oksana_k", token, "", http.StatusOK)); got.Photo != own.Person.Photo || got.Name != "Oksana" {
		t.Fatalf("an account without Telegram sees %+v", got)
	}

	// The picture of a link is a Telegram player's: an account has none.
	if code, _ := guest("/api/u/lucia_g/card.png"); code != http.StatusNotFound {
		t.Fatalf("the card of an account without Telegram = %d", code)
	}

	// A username she left says where she is now, to anyone, and is not a
	// page of its own.
	hs.setUsername(oksana, "oksana_new")
	const moved = `{"moved":"oksana_new"}`
	for who, got := range map[string]string{
		"Ivan":    hs.expect("GET", "/api/u/oksana_k", ivan, "", http.StatusOK),
		"herself": hs.expect("GET", "/api/u/oksana_k", oksana, "", http.StatusOK),
		"visitor": func() string { _, b := guest("/api/u/oksana_k"); return b }(),
	} {
		if strings.TrimSpace(got) != moved {
			t.Fatalf("%s at the old username: %s", who, got)
		}
	}
	// The case does not matter; what was never a username is not found.
	hs.expect("GET", "/api/u/OKSANA_NEW", ivan, "", http.StatusOK)
	hs.expect("GET", "/api/u/never_was", ivan, "", http.StatusNotFound)
	if code, body := guest("/api/u/nobody"); code != http.StatusNotFound || !strings.Contains(body, `"error":"not_found"`) {
		t.Fatalf("nobody = %d %s", code, body)
	}
	// Someone else takes the name she left: it is theirs now.
	hs.setUsername(ivan, "oksana_k")
	if v := decode[userView](t, hs.expect("GET", "/api/u/oksana_k", oksana, "", http.StatusOK)); v.Moved != "" || v.Name != "Ivan" {
		t.Fatalf("a name someone took = %+v", v)
	}
}

// The username field of the editor: checked as it is typed, then saved.
func TestUsernameCheckAndEdit(t *testing.T) {
	hs := setup(t)
	hs.person(1, "Oksana")
	hs.person(2, "Ivan")
	oksana, ivan := initData(1, "Oksana", "uk", ""), initData(2, "Ivan", "ru", "")
	hs.setUsername(ivan, "ivan_i")

	type check struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	ask := func(auth, name string) check {
		t.Helper()
		return decode[check](t, hs.expect("GET", "/api/username?name="+name, auth, "", http.StatusOK))
	}
	for name, want := range map[string]check{
		"oksana_free":  {OK: true},
		"@Oksana_Free": {OK: true},
		"ivan_i":       {Error: "username_taken"},
		"IVAN_I":       {Error: "username_taken"},
		"ab":           {Error: "username_form"},
		"9lives":       {Error: "username_form"},
		"no%20good":    {Error: "username_form"},
		"admin":        {Error: "username_form"},
		"":             {Error: "username_form"},
	} {
		if got := ask(oksana, name); got != want {
			t.Errorf("check %q = %+v, want %+v", name, got, want)
		}
	}
	// Her own is hers to keep.
	cur := decode[ownView](t, hs.expect("GET", "/api/profile", oksana, "", http.StatusOK)).Edit.Username
	if got := ask(oksana, cur); !got.OK {
		t.Errorf("her own username: %+v", got)
	}
	// Not for a visitor who has not signed in.
	if code, body := hs.visit("GET", "/api/username?name=oksana_free", ""); code != http.StatusUnauthorized {
		t.Fatalf("a visitor checks a username: %d %s", code, body)
	}

	// Saving: the form, somebody's, then hers.
	for body, want := range map[string]struct {
		code int
		err  string
	}{
		`{"username":"x"}`:         {http.StatusBadRequest, "username_form"},
		`{"username":"has space"}`: {http.StatusBadRequest, "username_form"},
		`{"username":"support"}`:   {http.StatusBadRequest, "username_form"},
		`{"username":""}`:          {http.StatusBadRequest, "username_form"},
		`{"username":"ivan_i"}`:    {http.StatusConflict, "username_taken"},
		`{"username":"@IVAN_I"}`:   {http.StatusConflict, "username_taken"},
		`{"nope":1}`:               {http.StatusBadRequest, "bad_request"},
		`not json`:                 {http.StatusBadRequest, "bad_request"},
		`{"username":5}`:           {http.StatusBadRequest, "bad_request"},
	} {
		got := hs.expect("POST", "/api/profile/edit", oksana, body, want.code)
		if !strings.Contains(got, `"error":"`+want.err+`"`) {
			t.Errorf("edit %s = %s, want %s", body, got, want.err)
		}
	}
	if now := decode[ownView](t, hs.expect("GET", "/api/profile", oksana, "", http.StatusOK)).Edit.Username; now != cur {
		t.Fatalf("a refused username changed it to %q", now)
	}
	done := decode[editResponse](t, hs.expect("POST", "/api/profile/edit", oksana, `{"username":"@Oksana_Free"}`, http.StatusOK))
	if done.Edit.Username != "oksana_free" || done.Person.U != "oksana_free" || done.Edit.Telegram != "Oksana" {
		t.Fatalf("saved = %+v", done)
	}
	// An empty change changes nothing.
	if same := decode[editResponse](t, hs.expect("POST", "/api/profile/edit", oksana, `{}`, http.StatusOK)); same.Edit.Username != "oksana_free" {
		t.Fatalf("empty edit = %+v", same)
	}
	// The one she left is free for Ivan now, and leads to her until he takes it.
	if got := ask(ivan, cur); !got.OK {
		t.Fatalf("her old username for Ivan: %+v", got)
	}
	if v := decode[userView](t, hs.expect("GET", "/api/u/"+cur, ivan, "", http.StatusOK)); v.Moved != "oksana_free" {
		t.Fatalf("her old username = %+v", v)
	}
	hs.setUsername(ivan, cur)
	if v := decode[userView](t, hs.expect("GET", "/api/u/"+cur, oksana, "", http.StatusOK)); v.Moved != "" || v.Name != "Ivan" {
		t.Fatalf("her old username after Ivan took it = %+v", v)
	}
	// Posts without a sign-in do not edit anything.
	hs.expect("POST", "/api/profile/edit", "", `{"name":"x"}`, http.StatusUnsupportedMediaType)
	if code, body := hs.visit("POST", "/api/profile/edit", `{"name":"x"}`); code != http.StatusUnauthorized {
		t.Fatalf("a visitor edits: %d %s", code, body)
	}
}

// A name of one's own shows in every table, the world's and a free party;
// removing it brings back Telegram's, and for an account with Google alone
// "Guest".
func TestOwnNameShowsInTables(t *testing.T) {
	hs := setup(t)
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	const lucia = "3f6b1a2c-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	day := hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500}, pastAnswer{user: 2, km: 2000, points: 900},
		pastAnswer{player: lucia, km: 700, points: 2000, bonus: 300})
	oksana, ivan := initData(1, "Oksana", "uk", ""), initData(2, "Ivan", "ru", "")
	token, _ := hs.signInGoogle("g-lucia", lucia)
	// Lucia is "Guest" until she picks one.
	if err := hs.srv.Store.(*db.Store).SitePlayer(context.Background(), lucia, nil); err != nil {
		t.Fatal(err)
	}
	table := func() string { return hs.expect("GET", "/api/day?date="+day.String(), ivan, "", http.StatusOK) }
	if row := rowIn(t, table(), "Oksana"); row.U == "" {
		t.Fatalf("her row before = %+v", row)
	}

	set := decode[editResponse](t, hs.expect("POST", "/api/profile/edit", oksana, `{"name":"  Ксюша​  "}`, http.StatusOK))
	if set.Edit.Name != "Ксюша" || set.Edit.Telegram != "Oksana" || set.Person.Name != "Ксюша" || !set.Person.Me {
		t.Fatalf("her name = %+v", set)
	}
	body := table()
	if rowIn(t, body, "Oksana").U != "" || rowIn(t, body, "Ксюша").U == "" || strings.Contains(body, "Oksana") {
		t.Fatalf("the table after her name: %s", body)
	}
	if own := decode[ownView](t, hs.expect("GET", "/api/profile", oksana, "", http.StatusOK)); own.Name != "Ксюша" || own.Edit.Name != "Ксюша" || own.Edit.Telegram != "Oksana" {
		t.Fatalf("her profile = %+v", own)
	}
	if u := decode[userView](t, hs.expect("GET", "/api/u/"+rowIn(t, body, "Ксюша").U, ivan, "", http.StatusOK)); u.Name != "Ксюша" {
		t.Fatalf("her profile for Ivan = %+v", u)
	}

	// Too long is cut, as a site name is; blank removes it.
	long := decode[editResponse](t, hs.expect("POST", "/api/profile/edit", oksana, `{"name":"`+strings.Repeat("Ж", 40)+`"}`, http.StatusOK))
	if n := utf8.RuneCountInString(long.Edit.Name); n == 0 || n > db.NameLimit {
		t.Fatalf("a long name = %q", long.Edit.Name)
	}
	back := decode[editResponse](t, hs.expect("POST", "/api/profile/edit", oksana, `{"name":""}`, http.StatusOK))
	if back.Edit.Name != "" || back.Person.Name != "Oksana" {
		t.Fatalf("her name removed = %+v", back)
	}
	if rowIn(t, table(), "Oksana").U == "" {
		t.Fatalf("the table after removing: %s", table())
	}
	hs.expect("POST", "/api/profile/edit", oksana, `{"name":"Ксюша"}`, http.StatusOK)
	hs.expect("POST", "/api/profile/edit", oksana, `{"name":"   "}`, http.StatusOK)
	if rowIn(t, table(), "Oksana").U == "" {
		t.Fatalf("a blank name did not remove it: %s", table())
	}

	// Lucia's, from the site: the name her site rows have.
	hs.siteExpect("GET", "/api/profile", token, "", http.StatusOK)
	hs.siteExpect("POST", "/api/profile/edit", token, `{"name":"Lucía"}`, http.StatusOK)
	if row := rowIn(t, table(), "Lucía"); row.U == "" {
		t.Fatalf("her row = %+v in %s", row, table())
	}
	if code, body := hs.site("GET", "/api/profile", token, ""); code != http.StatusOK || !strings.Contains(body, `"name":"Lucía"`) {
		t.Fatalf("her profile = %d %s", code, body)
	}
	hs.siteExpect("POST", "/api/profile/edit", token, `{"name":""}`, http.StatusOK)
	var siteName *string
	if err := hs.pool.QueryRow(context.Background(), `SELECT name FROM maply.site_player WHERE id = $1`, lucia).Scan(&siteName); err != nil || siteName != nil {
		t.Fatalf("her site name after removing = %v %v, want NULL", siteName, err)
	}
	if strings.Contains(table(), "Lucía") {
		t.Fatalf("her name is still in the table: %s", table())
	}
}

// A party shows a player's own name and photo, in the lobby and after.
func TestPartyShowsOwnNameAndPhoto(t *testing.T) {
	hs := freeSetup(t)
	hs.srv.Avatars = &avatar.Service{Store: hs.srv.Store.(*db.Store)}
	hs.person(1, "Oksana")
	hs.person(7, "Vera")
	oksana, vera := initData(1, "Oksana", "en", ""), initData(7, "Vera", "en", "")
	set := decode[editResponse](t, hs.expect("POST", "/api/profile/edit", vera, `{"name":"Vera K","username":"vera_k"}`, http.StatusOK))
	photo := decode[editResponse](t, hs.expect("POST", "/api/profile/photo", vera, photoJSON(pngOf(t, 30, 30)), http.StatusOK))
	if set.Person.Name != "Vera K" || !faceURL.MatchString(photo.Person.Photo) {
		t.Fatalf("setup: %+v %+v", set, photo)
	}
	lobby := decode[free.PartyView](t, hs.expect("POST", "/api/party", oksana, `{"mode":"classic","seconds":30,"rounds":3}`, 200))
	v := decode[free.PartyView](t, hs.expect("POST", "/api/party/"+lobby.Code+"/join", initData(7, "Vera", "en", "p"+lobby.Code), "", 200))
	if len(v.Members) != 2 || v.Members[1].Name != "Vera K" || v.Members[1].Photo != photo.Person.Photo || v.Members[1].U != "vera_k" || v.Members[0].Name != "Oksana" || v.Members[0].Photo != "" {
		t.Fatalf("members: %+v", v.Members)
	}
	// Taken off, her Telegram name is back.
	hs.expect("POST", "/api/profile/edit", vera, `{"name":""}`, http.StatusOK)
	hs.expect("POST", "/api/profile/photo", vera, `{"remove":true}`, http.StatusOK)
	poll := decode[free.PartyView](t, hs.expect("GET", "/api/party/"+lobby.Code, oksana, "", 200))
	if len(poll.Members) != 2 || poll.Members[1].Name != "Vera" || poll.Members[1].Photo != "" {
		t.Fatalf("polled members: %+v", poll.Members)
	}
}

// A photo of one's own: any picture maply can read becomes a square JPEG at
// an address of its own, in every table; anything else is refused, and
// removing it brings Telegram's back.
func TestOwnPhoto(t *testing.T) {
	hs := setup(t)
	store := hs.srv.Store.(*db.Store)
	hs.srv.Avatars = &avatar.Service{Store: store}
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	day := hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500}, pastAnswer{user: 2, km: 2000, points: 900})
	oksana, ivan := initData(1, "Oksana", "uk", ""), initData(2, "Ivan", "ru", "")
	telegram := []byte{0xff, 0xd8, 0x01}
	if err := store.SetAvatar(context.Background(), 1, db.Avatar{UniqueID: "u1", JPEG: telegram, CheckedAt: hs.now}); err != nil {
		t.Fatal(err)
	}
	photoOf := func(name string) string {
		t.Helper()
		return rowIn(t, hs.expect("GET", "/api/day?date="+day.String(), ivan, "", http.StatusOK), name).Photo
	}
	tgPhoto := photoOf("Oksana")
	if !faceURL.MatchString(tgPhoto) {
		t.Fatalf("Telegram's photo in the table = %q", tgPhoto)
	}
	if code, _ := hs.visit("GET", tgPhoto, ""); code != http.StatusOK {
		t.Fatalf("Telegram's address = %d", code)
	}

	// Things that are not a picture maply takes.
	var gifBuf bytes.Buffer
	if err := gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"garbage":       photoJSON([]byte("this is not a picture at all")),
		"not base64":    `{"photo":"%%%%"}`,
		"too wide":      photoJSON(pngOf(t, 5000, 10)),
		"too tall":      photoJSON(pngOf(t, 10, 4097)),
		"gif":           photoJSON(gifBuf.Bytes()),
		"truncated PNG": photoJSON(pngOf(t, 64, 64)[:100]),
	} {
		if got := hs.expect("POST", "/api/profile/photo", oksana, body, http.StatusBadRequest); !strings.Contains(got, `"error":"photo"`) {
			t.Errorf("%s: %s", name, got)
		}
	}
	for name, body := range map[string]string{
		"nothing":       `{}`,
		"both":          `{"photo":"AAAA","remove":true}`,
		"unknown field": `{"photo":"AAAA","name":"x"}`,
		"not JSON":      `photo`,
	} {
		if got := hs.expect("POST", "/api/profile/photo", oksana, body, http.StatusBadRequest); !strings.Contains(got, `"error":"bad_request"`) {
			t.Errorf("%s: %s", name, got)
		}
	}
	if got := photoOf("Oksana"); got != tgPhoto {
		t.Fatalf("a refused photo changed the table: %q", got)
	}

	// A real one, wide: cut square, 256 px, a new address.
	set := decode[editResponse](t, hs.expect("POST", "/api/profile/photo", oksana, photoJSON(pngOf(t, 64, 32)), http.StatusOK))
	if !set.Edit.Photo || !faceURL.MatchString(set.Person.Photo) || set.Person.Photo == tgPhoto || !set.Person.Me {
		t.Fatalf("photo set = %+v", set)
	}
	if got := photoOf("Oksana"); got != set.Person.Photo {
		t.Fatalf("her row has %q, want %q", got, set.Person.Photo)
	}
	if code, cfg := hs.face(set.Person.Photo); code != http.StatusOK || cfg.Width != avatar.OwnSide || cfg.Height != avatar.OwnSide {
		t.Fatalf("her photo at its address: %d %+v", code, cfg)
	}
	if own := decode[ownView](t, hs.expect("GET", "/api/profile", oksana, "", http.StatusOK)); own.Photo != set.Person.Photo || !own.Edit.Photo {
		t.Fatalf("her profile = %+v %+v", own, own.Edit)
	}
	// Another photo, another address; the first is gone.
	next := decode[editResponse](t, hs.expect("POST", "/api/profile/photo", oksana, photoJSON(pngOf(t, 40, 40)), http.StatusOK))
	if next.Person.Photo == set.Person.Photo || !faceURL.MatchString(next.Person.Photo) {
		t.Fatalf("second photo = %q after %q", next.Person.Photo, set.Person.Photo)
	}
	if code, _ := hs.face(set.Person.Photo); code != http.StatusNotFound {
		t.Fatalf("the replaced photo is served: %d", code)
	}
	if code, _ := hs.face(next.Person.Photo); code != http.StatusOK {
		t.Fatalf("the new photo: %d", code)
	}
	// A visitor who has not signed in sees no photo in the table.
	code, body := hs.visit("GET", "/api/day?date="+day.String(), "")
	if row := rowIn(t, body, "Oksana"); code != http.StatusOK || row.Photo != "" || row.U == "" {
		t.Fatalf("a visitor's row = %d %+v", code, row)
	}

	// Removed: Telegram's again, and the own address shows nothing.
	gone := decode[editResponse](t, hs.expect("POST", "/api/profile/photo", oksana, `{"remove":true}`, http.StatusOK))
	if gone.Edit.Photo || gone.Person.Photo != tgPhoto {
		t.Fatalf("photo removed = %+v, want back to %q", gone, tgPhoto)
	}
	if got := photoOf("Oksana"); got != tgPhoto {
		t.Fatalf("her row after removing: %q", got)
	}
	if code, _ := hs.face(next.Person.Photo); code != http.StatusNotFound {
		t.Fatalf("the removed photo is served: %d", code)
	}
	// Removing what is not there is fine.
	hs.expect("POST", "/api/profile/photo", oksana, `{"remove":true}`, http.StatusOK)

	// Ivan has no Telegram photo: his own, then none.
	if photoOf("Ivan") != "" {
		t.Fatal("Ivan has a photo from nowhere")
	}
	hs.expect("POST", "/api/profile/photo", ivan, photoJSON(pngOf(t, 20, 20)), http.StatusOK)
	if !faceURL.MatchString(photoOf("Ivan")) {
		t.Fatalf("Ivan's own photo = %q", photoOf("Ivan"))
	}
	hs.expect("POST", "/api/profile/photo", ivan, `{"remove":true}`, http.StatusOK)
	if photoOf("Ivan") != "" {
		t.Fatal("Ivan's photo is still there")
	}
	// Not for a visitor.
	if code, body := hs.visit("POST", "/api/profile/photo", `{"remove":true}`); code != http.StatusUnauthorized {
		t.Fatalf("a visitor removes a photo: %d %s", code, body)
	}
}

// The usernames in the boards and tables: the same one on every screen, and
// none on a player who hid themselves, except for them and their group.
func TestUsernamesInTablesAndBoards(t *testing.T) {
	hs := setup(t)
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	hs.person(3, "Lena", minskGroup)
	day := hs.seedPast(1, "JP", "asia", pastAnswer{user: 1, km: 5, points: 4950, bonus: 1500}, pastAnswer{user: 2, km: 2000, points: 900},
		pastAnswer{user: 3, km: 100, points: 3000, bonus: 300})
	oksana, ivan, lena := initData(1, "Oksana", "uk", ""), initData(2, "Ivan", "en", ""), initData(3, "Lena", "be", hs.payload(minskGroup))
	hs.setUsername(oksana, "oksana_k")
	hs.setUsername(ivan, "ivan_i")
	hs.setUsername(lena, "lena_l")
	hs.freeGame(1, "solo", "streak", 30, 0, 5, hs.now.Add(-time.Hour))
	hs.freeGame(2, "solo", "streak", 30, 0, 4, hs.now.Add(-time.Hour))

	body := hs.expect("GET", "/api/day?date="+day.String(), ivan, "", http.StatusOK)
	for name, want := range map[string]string{"Oksana": "oksana_k", "Ivan": "ivan_i", "Lena": "lena_l"} {
		if got := rowIn(t, body, name).U; got != want {
			t.Errorf("%s in the day's table is %q, want %q", name, got, want)
		}
	}
	top := decode[topBody](t, hs.expect("GET", "/api/top?board=streak&seconds=30", ivan, "", http.StatusOK))
	if top.line() != "1 Oksana 5*, 2 Ivan 4*<" || top.Rows[0].U != "oksana_k" || top.Rows[1].U != "ivan_i" {
		t.Fatalf("board = %s %+v", top.line(), top.Rows)
	}
	// She hides: no username in either for someone outside her groups,
	// while her own groups and she herself still see it.
	hs.expect("POST", "/api/profile/settings", lena, `{"hide_me":true}`, http.StatusOK)
	if row := rowIn(t, hs.expect("GET", "/api/day?date="+day.String(), ivan, "", http.StatusOK), "Anonymous"); row.U != "" || row.Photo != "" || !row.Anon {
		t.Fatalf("hidden Lena for Ivan = %+v", row)
	}
	if row := rowIn(t, hs.expect("GET", "/api/day?date="+day.String(), lena, "", http.StatusOK), "Lena"); row.U != "lena_l" || !row.Me {
		t.Fatalf("Lena for herself = %+v", row)
	}
}

// A site row of a hidden player's account keeps the name the site gave it
// and nothing that finds the person: no username, no photo, except for the
// person and, for the photo, their group.
func TestHiddenPlayersSiteRow(t *testing.T) {
	hs := setup(t)
	store := hs.srv.Store.(*db.Store)
	hs.srv.Avatars = &avatar.Service{Store: store}
	const guest = "33333333-3333-4333-8333-333333333333"
	hs.person(1, "Oksana", kyivGroup)
	hs.person(2, "Ivan", kyivGroup)
	hs.person(3, "Alesia", minskGroup)
	day := hs.seedPast(1, "JP", "asia", pastAnswer{player: guest, km: 700, points: 2000, bonus: 300})
	home := "Home"
	if err := store.SitePlayer(context.Background(), guest, &home); err != nil {
		t.Fatal(err)
	}
	oksana, ivan, alesia := initData(1, "Oksana", "uk", ""), initData(2, "Ivan", "en", ""), initData(3, "Alesia", "en", "")
	oksanaSite := hs.signIn(1, "Oksana")
	acc, ok, err := store.AccountOf(context.Background(), db.Identity{Kind: db.KindTelegram, TelegramUserID: 1})
	if err != nil || !ok {
		t.Fatalf("account %v %v", ok, err)
	}
	if _, err := store.AdoptGuest(context.Background(), acc.ID, guest, db.Rules{Open: func(round.Date) bool { return false }}); err != nil {
		t.Fatal(err)
	}
	hs.setUsername(oksana, "oksana_k")
	hs.expect("POST", "/api/profile/photo", oksana, photoJSON(pngOf(t, 30, 30)), http.StatusOK)
	rowFor := func(auth string) rowView {
		t.Helper()
		path := "/api/day?date=" + day.String() + "&lang=en"
		if auth == "" {
			code, body := hs.visit("GET", path, "")
			if code != http.StatusOK {
				t.Fatalf("visitor: %d %s", code, body)
			}
			return rowIn(t, body, "Home")
		}
		return rowIn(t, hs.expect("GET", path, auth, "", http.StatusOK), "Home")
	}

	// Known, the row leads to the person.
	if row := rowFor(ivan); row.U != "oksana_k" || row.Photo == "" {
		t.Fatalf("the row for Ivan = %+v", row)
	}
	if row := rowFor(""); row.U != "oksana_k" || row.Photo != "" {
		t.Fatalf("the row for a visitor = %+v", row)
	}
	hs.expect("POST", "/api/profile/settings", oksana, `{"hide_me":true}`, http.StatusOK)
	// Alesia shares no group with her; Ivan is in hers.
	for who, auth := range map[string]string{"a visitor": "", "Alesia": alesia} {
		if row := rowFor(auth); row.Name != "Home" || row.U != "" || row.Photo != "" || row.Anon {
			t.Errorf("the row for %s = %+v", who, row)
		}
	}
	// Her group-mate sees the row's name and photo, but her profile is closed
	// to him, so no username.
	if row := rowFor(ivan); row.Name != "Home" || row.U != "" || row.Photo == "" {
		t.Errorf("the row for her group-mate = %+v", row)
	}
	// She sees hers, and her own profile, on the site she is signed in on.
	body := hs.siteExpect("GET", "/api/day?date="+day.String()+"&lang=en", oksanaSite, "", http.StatusOK)
	if row := rowIn(t, body, "Home"); row.U != "oksana_k" || !row.Me || row.Photo == "" {
		t.Errorf("the row for herself on the site = %+v", row)
	}
}

// The same row in the Mini App is not marked as hers (the Mini App does not
// know the site players of an account), but the viewer is the person: a
// hidden player's profile opens to them, so the row may lead to it. Found by
// writing the tests of the profile rewrite; people.go dress takes "closed"
// from !p.Me, not from the viewer's own id.
func TestHiddenPlayersOwnSiteRowInTheMiniApp(t *testing.T) {
	hs := setup(t)
	store := hs.srv.Store.(*db.Store)
	const guest = "33333333-3333-4333-8333-333333333333"
	hs.person(1, "Oksana", kyivGroup)
	day := hs.seedPast(1, "JP", "asia", pastAnswer{player: guest, km: 700, points: 2000, bonus: 300})
	home := "Home"
	if err := store.SitePlayer(context.Background(), guest, &home); err != nil {
		t.Fatal(err)
	}
	oksana := initData(1, "Oksana", "uk", "")
	hs.signIn(1, "Oksana")
	acc, _, err := store.AccountOf(context.Background(), db.Identity{Kind: db.KindTelegram, TelegramUserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdoptGuest(context.Background(), acc.ID, guest, db.Rules{Open: func(round.Date) bool { return false }}); err != nil {
		t.Fatal(err)
	}
	hs.setUsername(oksana, "oksana_k")
	hs.expect("POST", "/api/profile/settings", oksana, `{"hide_me":true}`, http.StatusOK)
	body := hs.expect("GET", "/api/day?date="+day.String()+"&lang=en", oksana, "", http.StatusOK)
	if row := rowIn(t, body, "Home"); row.U != "oksana_k" {
		t.Fatalf("her own row in the Mini App = %+v", row)
	}
}
