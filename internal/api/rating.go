// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/people"
	"github.com/FreshLabDev/maply/internal/rating"
	"github.com/FreshLabDev/maply/internal/round"
)

// The rating: the daily game over the days that are over everywhere, the
// main table of the page at /top (internal/rating has the rules). Its names
// are the world table's after the day: a player who hid themselves is
// "Anonymous" outside their own groups, and has no username.

// RatingStore is what the rating reads. The store has it.
type RatingStore interface {
	RatingDates(ctx context.Context, n int) ([]round.Date, error)
	RatingDays(ctx context.Context, from round.Date) ([]db.RatingDay, error)
	RatingNames(ctx context.Context, users []int64, accounts []string) (map[int64]string, map[string]string, error)
	GroupPeople(ctx context.Context, viewer, chatID int64) ([]int64, error)
	PlayedWith(ctx context.Context, who db.FreeWho) ([]int64, []string, error)
}

var _ RatingStore = (*db.Store)(nil)

// Circles, as the page names them: everyone, the people of the viewer's
// groups, and those they have played free play with.
const (
	circleWorld  = "world"
	circleGroups = "groups"
	circlePlayed = "played"
)

// ratingTTL is how long the days of a period are kept: a period changes
// once a day, when the next day is over everywhere, so a minute late is
// nothing, and a full page of readers ranks it once a minute.
const ratingTTL = time.Minute

type ratingKept struct {
	at    time.Time
	dates []round.Date
	days  []rating.Day
}

// ratingCache keeps each period's days.
type ratingCache struct {
	mu   sync.Mutex
	kept map[string]ratingKept
}

// ratingDays are the period's dates and every person's days in them, kept
// for ratingTTL.
func (s *Server) ratingDays(ctx context.Context, rs RatingStore, period string) ([]round.Date, []rating.Day, error) {
	now := s.Now()
	s.rating.mu.Lock()
	k, ok := s.rating.kept[period]
	s.rating.mu.Unlock()
	if ok && now.Sub(k.at) < ratingTTL {
		return k.dates, k.days, nil
	}
	dates, err := rs.RatingDates(ctx, rating.Window(period))
	if err != nil {
		return nil, nil, err
	}
	var days []rating.Day
	if len(dates) > 0 {
		rows, err := rs.RatingDays(ctx, dates[len(dates)-1])
		if err != nil {
			return nil, nil, err
		}
		days = make([]rating.Day, len(rows))
		for i, r := range rows {
			days[i] = rating.Day{Key: ratingKey(r.UserID, r.Account), Date: r.Date, Points: r.Points, Km: r.Km}
		}
	}
	s.rating.mu.Lock()
	if s.rating.kept == nil {
		s.rating.kept = map[string]ratingKept{}
	}
	s.rating.kept[period] = ratingKept{at: now, dates: dates, days: days}
	s.rating.mu.Unlock()
	return dates, days, nil
}

// ratingKey names a person in internal/rating: "u" and a Telegram id, or
// "a" and an account without Telegram.
func ratingKey(user int64, account string) string {
	if user != 0 {
		return "u" + strconv.FormatInt(user, 10)
	}
	return "a" + account
}

func ratingWho(key string) (int64, string) {
	if len(key) > 1 && key[0] == 'u' {
		id, _ := strconv.ParseInt(key[1:], 10, 64)
		return id, ""
	}
	return 0, key[1:]
}

type ratingView struct {
	Period string `json:"period"`
	Circle string `json:"circle"`
	League string `json:"league,omitempty"`
	Sort   string `json:"sort"`
	// Dates is how many days over everywhere the period has.
	Dates int `json:"dates"`
	// Total is how many people the table has, Rows its first ones and the
	// viewer's own wherever it stands.
	Total int         `json:"total"`
	Rows  []ratingRow `json:"rows"`
	Cards ratingCards `json:"cards"`
	// Me is the viewer over the period among everyone, whatever the
	// filters; nil for a guest and for one with no day in it.
	Me *ratingMe `json:"me,omitempty"`
	// Guest is a viewer without a profile, who is in no rating.
	Guest bool `json:"guest,omitempty"`
	// Circles are the ones the viewer may pick: a guest has only the world,
	// an account without Telegram no groups.
	Circles []string        `json:"circles"`
	Leagues []rating.League `json:"leagues"`
}

type ratingRow struct {
	Rank int `json:"rank"`
	people.Person
	Score  int     `json:"score"`
	Days   int     `json:"days"`
	Avg    int     `json:"avg"`
	Km     float64 `json:"km"`
	League string  `json:"league"`
}

// ratingCard is one of the four: the person and what they stand out by.
type ratingCard struct {
	people.Person
	// N is the days for the most active and the streak, the points of the
	// best day; Km is the most accurate one's median miss.
	N    int     `json:"n,omitempty"`
	Km   float64 `json:"km,omitempty"`
	Date string  `json:"date,omitempty"`
}

type ratingCards struct {
	Active   *ratingCard `json:"active,omitempty"`
	Accurate *ratingCard `json:"accurate,omitempty"`
	Day      *ratingCard `json:"day,omitempty"`
	Streak   *ratingCard `json:"streak,omitempty"`
}

type ratingMe struct {
	Score  int    `json:"score"`
	Days   int    `json:"days"`
	Avg    int    `json:"avg"`
	League string `json:"league"`
	// Next is the league above and the average it starts at; for a new
	// person NeedDays is how many more days put them in one.
	Next     string `json:"next,omitempty"`
	NextFrom int    `json:"next_from,omitempty"`
	NeedDays int    `json:"need_days,omitempty"`
}

// ratingPage is GET /api/rating?period=30&circle=world&league=&sort=score.
func (s *Server) ratingPage(w http.ResponseWriter, r *http.Request) {
	rs, ok := s.Store.(RatingStore)
	if !ok {
		s.fail(w, r, errUnavailable)
		return
	}
	v, err := s.profileViewer(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	pick := func(key, def string) string {
		if x := q.Get(key); x != "" {
			return x
		}
		return def
	}
	period, circle, league, by := pick("period", rating.Month), pick("circle", circleWorld), q.Get("league"), pick("sort", rating.ByScore)
	if !rating.Known(period) || !rating.KnownSort(by) || league != "" && !rating.KnownLeague(league) ||
		circle != circleWorld && circle != circleGroups && circle != circlePlayed {
		s.fail(w, r, errBadRequest)
		return
	}
	ctx := r.Context()
	dates, days, err := s.ratingDays(ctx, rs, period)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	lines := rating.Lines(period, dates, days)
	me := v.freeWho()
	mine := ""
	if me != (db.FreeWho{}) {
		mine = ratingKey(me.UserID, me.Account)
	}
	out := ratingView{Period: period, Circle: circle, League: league, Sort: by, Dates: len(dates),
		Guest: mine == "", Leagues: rating.Ladder, Rows: []ratingRow{}, Circles: []string{circleWorld}}
	if me.UserID != 0 {
		out.Circles = append(out.Circles, circleGroups)
	}
	if mine != "" {
		out.Circles = append(out.Circles, circlePlayed)
	}
	for _, l := range lines {
		if l.Key == mine {
			m := &ratingMe{Score: l.Score, Days: l.Days, Avg: l.Avg, League: l.League}
			m.Next, m.NextFrom = rating.Next(l.League)
			if l.League == rating.Novice {
				m.NeedDays = rating.MinDays - l.Days
			}
			out.Me = m
		}
	}

	// The circle, then the league.
	if circle != circleWorld {
		in := map[string]bool{}
		switch {
		case mine == "":
		case circle == circleGroups:
			ids, err := rs.GroupPeople(ctx, me.UserID, 0)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			for _, id := range ids {
				in[ratingKey(id, "")] = true
			}
		default:
			ids, accounts, err := rs.PlayedWith(ctx, me)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			for _, id := range ids {
				in[ratingKey(id, "")] = true
			}
			for _, a := range accounts {
				in[ratingKey(0, a)] = true
			}
		}
		lines = keep(lines, func(l rating.Line) bool { return in[l.Key] })
	}
	if league != "" {
		lines = keep(lines, func(l rating.Line) bool { return l.League == league })
	}
	ranked := rating.Rank(lines, by)
	out.Total = len(ranked)
	cards := rating.CardsOf(lines)

	byKey := make(map[string]rating.Line, len(lines))
	for _, l := range lines {
		byKey[l.Key] = l
	}
	// Who each shown key is, filled in below as the viewer may see them.
	var keys []string
	var who []*people.Person
	for i, x := range ranked {
		if i >= topShown && x.Key != mine {
			continue
		}
		out.Rows = append(out.Rows, ratingRow{Rank: x.Rank, Score: x.Score, Days: x.Days, Avg: x.Avg, Km: x.Km, League: x.League})
		keys = append(keys, x.Key)
	}
	for i := range out.Rows {
		who = append(who, &out.Rows[i].Person)
	}
	card := func(key string, set func(*ratingCard, rating.Line)) *ratingCard {
		if key == "" {
			return nil
		}
		c := &ratingCard{}
		set(c, byKey[key])
		keys, who = append(keys, key), append(who, &c.Person)
		return c
	}
	out.Cards = ratingCards{
		Active:   card(cards.Active, func(c *ratingCard, l rating.Line) { c.N = l.Days }),
		Accurate: card(cards.Accurate, func(c *ratingCard, l rating.Line) { c.Km = l.Km }),
		Day:      card(cards.Day, func(c *ratingCard, l rating.Line) { c.N, c.Date = l.Best, l.BestDate.String() }),
		Streak:   card(cards.Streak, func(c *ratingCard, l rating.Line) { c.N = l.Streak }),
	}
	if err := s.ratingPeople(ctx, rs, v, mine, keys, who); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ratingPeople fills in who[i], the person keys[i] names: their name as the
// store has it, then dressed as the viewer may see them.
func (s *Server) ratingPeople(ctx context.Context, rs RatingStore, v profileViewer, mine string, keys []string, who []*people.Person) error {
	var users []int64
	var accounts []string
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		if id, a := ratingWho(k); id != 0 {
			users = append(users, id)
		} else {
			accounts = append(accounts, a)
		}
	}
	byUser, byAccount, err := rs.RatingNames(ctx, users, accounts)
	if err != nil {
		return err
	}
	for i, k := range keys {
		if id, a := ratingWho(k); id != 0 {
			*who[i] = people.Telegram(id, byUser[id], k == mine)
		} else {
			*who[i] = people.SitePlayer("", byAccount[a], k == mine)
			who[i].Account = a
		}
	}
	return s.dress(ctx, dressAsk{chatID: v.chatID, viewer: v.user, lang: v.lang, guest: v.guest()}, who...)
}

// keep is the lines ok says yes to.
func keep(lines []rating.Line, ok func(rating.Line) bool) []rating.Line {
	out := lines[:0:0]
	for _, l := range lines {
		if ok(l) {
			out = append(out, l)
		}
	}
	return out
}
