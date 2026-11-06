// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/people"
	"github.com/FreshLabDev/maply/internal/profile"
	"github.com/FreshLabDev/maply/internal/round"
)

// The profile: a player's own numbers, and the mini profile anyone who sees
// a player's name may open by tapping it.
//
// Who sees what:
//   - The player in Telegram sees every scored day of theirs, today's too:
//     they saw the place when they answered. World places and percentiles
//     come only from days over everywhere.
//   - A site player sees only days over everywhere, as the site shows no
//     place before then.
//   - Anyone else sees a few numbers over the days over everywhere, so a
//     profile tells nobody what a player did on a day still being guessed.
//     The Telegram photo goes with the name, as on Telegram itself; the head
//     to head comes only inside a group both are in, over the days the
//     group's reveal has opened.
//   - A player who hid themselves has no mini profile, and the site's tables
//     call them "Telegram player".

// ProfileStore is what the profile reads and writes. The store has it.
type ProfileStore interface {
	PlayedDays(ctx context.Context, who db.Who, upTo, closed round.Date) ([]db.PlayedDay, error)
	Standings(ctx context.Context, who db.Who, dates []round.Date, closed round.Date) ([]db.Standing, error)
	LastRounds(ctx context.Context, upTo round.Date, n int) ([]db.RoundDay, error)
	SkippedDates(ctx context.Context, upTo round.Date) ([]round.Date, error)
	NameOf(ctx context.Context, userID int64) (string, error)
	ProfileSettingOf(ctx context.Context, userID int64) (db.ProfileSetting, error)
	SetHideMe(ctx context.Context, userID int64, on bool) error
	PublicOwner(ctx context.Context, slug string) (int64, bool, error)
	Hidden(ctx context.Context, ids []int64) (map[int64]bool, error)
	Present(ctx context.Context, chatID int64, ids []int64) (map[int64]bool, error)
	SharesGroup(ctx context.Context, viewer int64, ids []int64) (map[int64]bool, error)
	HeadToHead(ctx context.Context, me, other int64, upTo round.Date) (int, int, error)
	FreeGames(ctx context.Context, w db.FreeWho) ([]db.FreeGame, error)
	SeasonOf(ctx context.Context, first, last round.Date, chatID, userID int64) (db.SeasonRank, error)
	TouchPerson(ctx context.Context, user tg.User) error
}

// The store must have the profile's side: without it the site's tables
// would name players who hid themselves.
var _ ProfileStore = (*db.Store)(nil)

// profileStore is the store's profile side, nil when it has none.
func (s *Server) profileStore() ProfileStore {
	ps, _ := s.Store.(ProfileStore)
	return ps
}

// profileViewer is who opens a profile: a Telegram player from the Mini App,
// with the group it was opened from, or from the site signed in with
// Telegram; a site account without Telegram; or a guest by the cookie.
type profileViewer struct {
	user int64
	name string
	// person is the Mini App's Telegram user, for core.touch_person; empty
	// on the site, where the sign-in has already recorded the person.
	person  tg.User
	account string
	player  string
	chatID  int64
	zone    *time.Location
	lang    string
}

// guest is a viewer who has not signed in: neither the Mini App nor a site
// account.
func (v profileViewer) guest() bool { return v.user == 0 && v.account == "" }

func (s *Server) profileViewer(r *http.Request) (profileViewer, error) {
	if r.Header.Get("Authorization") != "" {
		p, err := s.resolveAs(r, false)
		if err != nil {
			return profileViewer{}, err
		}
		person := tg.User{ID: p.user.ID, FirstName: p.user.FirstName, LastName: p.user.LastName,
			Username: p.user.Username, LanguageCode: p.user.LanguageCode}
		return profileViewer{user: p.user.ID, name: p.user.FirstName, person: person, chatID: p.chatID, zone: p.zone, lang: p.lang}, nil
	}
	sv := s.siteViewer(r)
	if sv.TelegramUserID != 0 {
		// The site signed in with Telegram is that Telegram player, from
		// their DM: its zone and language, and only maply's own page may
		// ask, as the session is a cookie.
		p, err := s.resolveSite(r, sv)
		if err != nil {
			return profileViewer{}, err
		}
		return profileViewer{user: sv.TelegramUserID, zone: p.zone, lang: p.lang}, nil
	}
	// Anyone may read a profile without signing in: by address, and all of
	// them together.
	if !s.ipLimit.allowIP(clientIP(r)) || !s.profAll.allowKey("") {
		return profileViewer{}, errRateLimited
	}
	return profileViewer{account: sv.AccountID, player: sv.Player, lang: i18n.Resolve(r.URL.Query().Get("lang"))}, nil
}

func (s *Server) closedAt(now time.Time) func(round.Date) bool {
	return func(d round.Date) bool { return !s.Zones.LastReveal(d).After(now) }
}

// ownProfile is the viewer's own. Site is a profile of the site's game, a
// guest's or an account's without Telegram: days over everywhere, no speed
// bonus, no settings.
type ownProfile struct {
	people.Person
	// Guest is a site player known only by the cookie: clearing it loses the
	// history, so the page says to sign in.
	Guest    bool            `json:"guest,omitempty"`
	Ranked   profile.Ranked  `json:"ranked"`
	Free     *profile.Free   `json:"free"`
	Settings *profileSetting `json:"settings,omitempty"`
	// Season is the month's season so far, the world's and the group's.
	Season []seasonPlace `json:"season,omitempty"`
	// Top is the player's place on each board of free play they are on.
	Top []topPlace `json:"top,omitempty"`
	// Edit is what the player may change of how maply shows them: nil for
	// a guest, who has no profile of their own.
	Edit *profileEdit `json:"edit,omitempty"`
}

type seasonPlace struct {
	// Kind is "world" or "group".
	Kind   string `json:"kind"`
	Rank   int    `json:"rank"`
	Of     int    `json:"of"`
	Points int    `json:"points"`
}

// season is the player's place in the month's season: the world's, and the
// group's when the profile was opened from one. It counts the days over
// everywhere, as the month's end does, and only Telegram players have one.
// A failure leaves the tiles out rather than the profile.
func (s *Server) season(ctx context.Context, ps ProfileStore, v profileViewer, now time.Time) []seasonPlace {
	upTo := profile.LatestClosed(s.Zones, now)
	t := upTo.Time()
	first := round.NewDate(t.Year(), t.Month(), 1)
	type scope struct {
		kind string
		chat int64
	}
	scopes := []scope{{"world", 0}}
	if v.chatID < 0 {
		scopes = append(scopes, scope{"group", v.chatID})
	}
	var out []seasonPlace
	for _, sc := range scopes {
		r, err := ps.SeasonOf(ctx, first, upTo, sc.chat, v.user)
		if err != nil {
			s.Log.Warn("season not read", "error", err)
			return nil
		}
		if r.Rank > 0 {
			out = append(out, seasonPlace{Kind: sc.kind, Rank: r.Rank, Of: r.Of, Points: r.Points})
		}
	}
	return out
}

type profileSetting struct {
	HideMe bool `json:"hide_me"`
	// Zone is the person's own zone, the DM's: the morning post and the
	// result there, and the site, go by it. Zones is the choice, west to
	// east.
	Zone  string   `json:"zone,omitempty"`
	Zones []string `json:"zones,omitempty"`
}

// settings is the profile's settings of the Telegram player user.
func (s *Server) settings(ctx context.Context, ps ProfileStore, user int64) (*profileSetting, error) {
	set, err := ps.ProfileSettingOf(ctx, user)
	if err != nil {
		return nil, err
	}
	zone, err := s.ownZone(ctx, user)
	if err != nil {
		return nil, err
	}
	return &profileSetting{HideMe: set.HideMe, Zone: zone, Zones: round.ZoneChoices}, nil
}

// ownZone is the zone of user's DM: the one stored, else their language's,
// as the DM's first post fixes it.
func (s *Server) ownZone(ctx context.Context, user int64) (string, error) {
	zone, err := s.Store.ChatZone(ctx, user)
	if err != nil || round.IsChoice(zone) {
		return zone, err
	}
	lang, err := s.Store.UserLang(ctx, user)
	if err != nil {
		return "", err
	}
	return round.DefaultName(lang), nil
}

// Moving the own zone is refused as the bot's picker refuses it: a zone
// where today is still open after the place was out here, or one a date
// ahead before today's result.
var (
	errZoneTomorrow    = apiError{http.StatusConflict, "zone_tomorrow"}
	errZoneAfterResult = apiError{http.StatusConflict, "zone_after_result"}
)

// setZone moves user's own zone to name, one of round.ZoneChoices.
func (s *Server) setZone(ctx context.Context, user int64, name string) error {
	if !round.IsChoice(name) {
		return errBadRequest
	}
	cur, err := s.ownZone(ctx, user)
	if err != nil {
		return err
	}
	if cur == name {
		return nil
	}
	from, to, now := s.Zones.For(cur, ""), s.Zones.For(name, ""), s.Now()
	if round.Reopens(from, to, now) {
		return errZoneTomorrow
	}
	if round.Outruns(from, to, now) {
		return errZoneAfterResult
	}
	return s.Store.SetChatZone(ctx, user, name)
}

// rankedAsk is which numbers of whose a ranked tab holds: the days up to
// upTo, a streak and a 30-day window that end at the player's own date (zero
// is upTo), the history, and the percentile, which reads every answer of
// every closed day the player has.
type rankedAsk struct {
	who        db.Who
	upTo, ref  round.Date
	history    bool
	percentile bool
}

// ranked reads and works out a ranked tab.
func (s *Server) ranked(ctx context.Context, ps ProfileStore, ask rankedAsk, lang string) (profile.Ranked, error) {
	now := s.Now()
	closed := s.closedAt(now)
	// Site rows only of days over everywhere, whoever looks: a Telegram
	// person's own profile goes up to the newest date, and a guest's answer
	// their account took in would show the place of a day they can still
	// answer in a chat.
	days, err := ps.PlayedDays(ctx, ask.who, ask.upTo, profile.LatestClosed(s.Zones, now))
	if err != nil {
		return profile.Ranked{}, err
	}
	var standings []db.Standing
	if ask.percentile {
		var over []round.Date
		for _, d := range days {
			if closed(d.Date) {
				over = append(over, d.Date)
			}
		}
		if standings, err = ps.Standings(ctx, ask.who, over, profile.LatestClosed(s.Zones, now)); err != nil {
			return profile.Ranked{}, err
		}
	}
	ref := ask.ref
	if ref.IsZero() || ask.upTo.Before(ref) {
		ref = ask.upTo
	}
	// A day answered in a group whose date runs ahead of the player's own
	// is theirs too: the streak runs to it.
	if n := len(days); n > 0 && ref.Before(days[n-1].Date) {
		ref = days[n-1].Date
	}
	rounds, err := ps.LastRounds(ctx, ask.upTo, profile.SeriesDays)
	if err != nil {
		return profile.Ranked{}, err
	}
	skipped, err := ps.SkippedDates(ctx, ask.upTo)
	if err != nil {
		return profile.Ranked{}, err
	}
	return profile.Rank(profile.Input{Days: days, Standings: standings, Rounds: rounds, Skipped: skipped, Ref: ref,
		Closed: closed, Lang: lang, WithHistory: ask.history}), nil
}

// free reads the free tab. A failure there leaves the tab empty rather than
// the profile: free play is another part of the service.
func (s *Server) free(ctx context.Context, ps ProfileStore, w db.FreeWho) *profile.Free {
	games, err := ps.FreeGames(ctx, w)
	if err != nil {
		s.Log.Warn("free results not read", "error", err)
		return nil
	}
	f := profile.FreeOf(games)
	return &f
}

// profile is the viewer's own profile.
func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	ps := s.profileStore()
	if ps == nil {
		s.fail(w, r, errUnavailable)
		return
	}
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.ownProfileOf(r.Context(), ps, v)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ownProfileOf is the viewer's own profile, with what they may change of it.
func (s *Server) ownProfileOf(ctx context.Context, ps ProfileStore, v profileViewer) (ownProfile, error) {
	now := s.Now()
	out := ownProfile{}
	var err error
	switch {
	case v.user != 0:
		// Up to the newest date anywhere, as a group far east may be on it;
		// the streak as of the player's own date, else it would break each
		// afternoon in the west once Sydney's tomorrow began.
		ask := rankedAsk{who: db.Who{UserID: v.user}, upTo: s.Zones.Newest(now), history: true, percentile: true}
		if v.zone != nil {
			ask.ref = round.DateIn(now, v.zone)
		}
		if out.Ranked, err = s.ranked(ctx, ps, ask, v.lang); err != nil {
			return ownProfile{}, err
		}
		name, err := ps.NameOf(ctx, v.user)
		if err != nil {
			return ownProfile{}, err
		}
		if name == "" {
			name = v.name
		}
		out.Person = people.Telegram(v.user, name, true)
		if out.Settings, err = s.settings(ctx, ps, v.user); err != nil {
			return ownProfile{}, err
		}
		out.Free = s.free(ctx, ps, v.freeWho())
		out.Season = s.season(ctx, ps, v, now)
		out.Top = s.topPlaces(ctx, v)
	default:
		// A site player sees what the site shows: days over everywhere. An
		// account keeps its days across browsers; a guest has the cookie's.
		out.Person, out.Guest = people.SitePlayer(v.player, "", true), v.account == ""
		out.Person.Account = v.account
		upTo := profile.LatestClosed(s.Zones, now)
		who := db.Who{Account: v.account, Player: v.player}
		if v.account != "" {
			who.Player = ""
		}
		if who == (db.Who{}) {
			out.Ranked = profile.Rank(profile.Input{Ref: upTo, Closed: s.closedAt(now)})
			break
		}
		if out.Ranked, err = s.ranked(ctx, ps, rankedAsk{who: who, upTo: upTo, history: true, percentile: true}, v.lang); err != nil {
			return ownProfile{}, err
		}
		// An account without Telegram plays free as its site player and
		// keeps its games; a guest keeps none.
		if v.account != "" {
			out.Free = s.free(ctx, ps, v.freeWho())
		}
		out.Top = s.topPlaces(ctx, v)
		// An account's site players share one name: the main one's.
		if v.player != "" {
			if out.Name, err = s.Store.SiteName(ctx, v.player); err != nil {
				return ownProfile{}, err
			}
		}
	}
	if out.Edit, err = s.editOf(ctx, v, out.Name); err != nil {
		return ownProfile{}, err
	}
	if err := s.dress(ctx, dressAsk{viewer: v.user, lang: v.lang}, &out.Person); err != nil {
		return ownProfile{}, err
	}
	return out, nil
}

// faceWait bounds how long the app's own face waits for Telegram.
const faceWait = 5 * time.Second

// face is the viewer's own photo, for the face the app draws for them: the
// one they chose, or Telegram's, asked of Telegram now when it was not today
// so a new photo shows on the next open. An account without Telegram has
// only the one it chose; a guest none.
func (s *Server) face(w http.ResponseWriter, r *http.Request) {
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var out struct {
		Photo string `json:"photo,omitempty"`
	}
	if v.user != 0 && s.Avatars != nil {
		ctx, cancel := context.WithTimeout(r.Context(), faceWait)
		defer cancel()
		s.Avatars.Fetch(ctx, []int64{v.user})
		me := people.Telegram(v.user, v.name, true)
		if err := s.dress(r.Context(), dressAsk{viewer: v.user, lang: v.lang}, &me); err != nil {
			s.fail(w, r, err)
			return
		}
		out.Photo = me.Photo
	} else if v.account != "" {
		me := people.Person{Site: true, Me: true, Account: v.account}
		if err := s.dress(r.Context(), dressAsk{lang: v.lang}, &me); err != nil {
			s.fail(w, r, err)
			return
		}
		out.Photo = me.Photo
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

type headToHead struct {
	Closer int `json:"closer"`
	Of     int `json:"of"`
}

// profileSettings changes the player's own choices. Only a Telegram player
// has them: the site's players are not named by Telegram.
func (s *Server) profileSettings(w http.ResponseWriter, r *http.Request) {
	ps := s.profileStore()
	if ps == nil {
		s.fail(w, r, errUnavailable)
		return
	}
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if v.user == 0 {
		s.fail(w, r, errUnauthorized)
		return
	}
	var body struct {
		HideMe *bool   `json:"hide_me"`
		Zone   *string `json:"zone"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || (body.HideMe == nil) == (body.Zone == nil) {
		s.fail(w, r, errBadRequest)
		return
	}
	ctx := r.Context()
	if err := s.touchProfile(ctx, ps, v); err != nil {
		s.fail(w, r, err)
		return
	}
	if body.HideMe != nil {
		err = ps.SetHideMe(ctx, v.user, *body.HideMe)
	} else {
		err = s.setZone(ctx, v.user, *body.Zone)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	set, err := s.settings(ctx, ps, v.user)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// touchProfile records the person in core before a setting that refers to
// them. From the site the Telegram sign-in already has, and the site writes
// nothing to core.
func (s *Server) touchProfile(ctx context.Context, ps ProfileStore, v profileViewer) error {
	if v.person.ID == 0 {
		return nil
	}
	return ps.TouchPerson(ctx, v.person)
}

// anonName is what shared tables call a player who hid themselves.
func anonName(lang string) string { return i18n.T(lang, "app.prof.anon") }

// maskHidden is which of the Telegram players among ids asked to be hidden
// from a viewer: their own groups still see them, whether the viewer looks
// from the group (chatID), from a DM or from another group they share.
// viewer is the Telegram id, zero on the site.
func (s *Server) maskHidden(ctx context.Context, chatID, viewer int64, ids []int64) (map[int64]bool, error) {
	ps := s.profileStore()
	if ps == nil || len(ids) == 0 {
		return map[int64]bool{}, nil
	}
	hidden, err := ps.Hidden(ctx, ids)
	if err != nil || len(hidden) == 0 || chatID == 0 && viewer == 0 {
		return hidden, err
	}
	var asked []int64
	for id := range hidden {
		asked = append(asked, id)
	}
	in, err := ps.Present(ctx, chatID, asked)
	if err != nil {
		return nil, err
	}
	shared, err := ps.SharesGroup(ctx, viewer, asked)
	if err != nil {
		return nil, err
	}
	for id := range hidden {
		if in[id] || shared[id] {
			delete(hidden, id)
		}
	}
	return hidden, nil
}
