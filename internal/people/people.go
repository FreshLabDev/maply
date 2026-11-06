// SPDX-License-Identifier: Apache-2.0

// Package people is how maply shows a player: every table, list and profile
// of the app carries a Person, the API fills in all of them in one place
// (api.Server.dress) and the app draws all of them with one function (face
// in web/app/app.js). A name, a face and "Anonymous" look the same on every
// screen because nothing else builds them.
package people

// Person is a player as a row shows them.
type Person struct {
	// Name is the Telegram first name, a site player's typed name, or
	// "Anonymous" for one who hid themselves; empty while Hidden, and for a
	// site guest without a name, whom the app calls "Guest".
	Name string `json:"name,omitempty"`
	// Photo is the address of the kept Telegram photo, /face/<key>.jpg.
	Photo string `json:"photo,omitempty"`
	// Who is the handle of the mini profile, next to a Telegram name only.
	Who string `json:"who,omitempty"`
	Me  bool   `json:"me,omitempty"`
	// Site is a player of the site's game rather than a Telegram one.
	Site bool `json:"site,omitempty"`
	// Group is a player of the chat the view is opened from: a hidden
	// player keeps their name there, as their groups do.
	Group bool `json:"group,omitempty"`
	// Anon is a player who hid themselves from this viewer ("Hide me").
	Anon bool `json:"anon,omitempty"`
	// Hidden is a row whose name is not out yet: no name, face or handle.
	Hidden bool `json:"hidden,omitempty"`
	// ID is the Telegram player the API fills the rest in by; never sent.
	ID int64 `json:"-"`
}

// Telegram is a Telegram player by their id and name.
func Telegram(id int64, name string, me bool) Person {
	return Person{ID: id, Name: name, Me: me}
}

// SitePlayer is a player of the site's game under the name they typed.
func SitePlayer(name string, me bool) Person {
	return Person{Name: name, Site: true, Me: me}
}

// Unnamed is a row whose name is not out yet.
func Unnamed(me bool) Person {
	return Person{Hidden: true, Me: me}
}
