// SPDX-License-Identifier: Apache-2.0
package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/free"
)

// The free play's leaderboards: each player's best on a board, over the last
// week or all time. Free play has no place in the day and nothing to reveal,
// so the names are the world table's after the day: a player who hid
// themselves is "Anonymous" to everyone outside their own groups, and has no
// handle there. An account without Telegram is named as on the site,
// by its site player, and has no mini profile, so no handle either.

// TopStore is what the leaderboards read. The store has it.
type TopStore interface {
	Top(ctx context.Context, b db.TopBoard, since time.Time, limit int, me db.FreeWho) (db.TopTable, error)
	TopPlaces(ctx context.Context, me db.FreeWho, since time.Time) ([]db.TopPlace, error)
}

var _ TopStore = (*db.Store)(nil)

// topKind is a board the page has a tab for and the settings it keeps a
// table of; the first of each is the one it opens on.
type topKind struct {
	mode    string
	seconds []int
	rounds  []int
}

// topKinds are the boards. A blitz is a classic game at 30 s a place, as the
// free game names it; a classic one has a longer timer. Without a timer a
// game is on no board.
var topKinds = map[string]topKind{
	"streak":  {mode: free.Streak, seconds: []int{30, 60}},
	"timed":   {mode: "timed", seconds: []int{180}},
	"blitz":   {mode: free.Classic, seconds: []int{30}, rounds: []int{5, 3, 10}},
	"classic": {mode: free.Classic, seconds: []int{60, 90, 120}, rounds: []int{5, 3, 10}},
}

// topShown is how many rows of a board the page gets, the viewer's besides.
const topShown = 100

// topWeek is the period "week": the last seven days, the same everywhere.
const topWeek = 7 * 24 * time.Hour

// topName is the board a setting is on.
func topName(b db.TopBoard) string {
	switch {
	case b.Mode == free.Classic && b.Seconds == 30:
		return "blitz"
	case b.Mode == free.Classic:
		return "classic"
	}
	return b.Mode
}

// topBoardOf reads a board from the query: its name and settings, a missing
// setting the board's first.
func topBoardOf(name, seconds, rounds string) (db.TopBoard, bool) {
	k, ok := topKinds[name]
	if !ok {
		return db.TopBoard{}, false
	}
	pick := func(raw string, of []int) (int, bool) {
		if len(of) == 0 {
			return 0, raw == "" || raw == "0"
		}
		if raw == "" {
			return of[0], true
		}
		n, err := strconv.Atoi(raw)
		return n, err == nil && slices.Contains(of, n)
	}
	b := db.TopBoard{Mode: k.mode}
	var okS, okR bool
	b.Seconds, okS = pick(seconds, k.seconds)
	b.Rounds, okR = pick(rounds, k.rounds)
	return b, okS && okR
}

type topView struct {
	Board   string `json:"board"`
	Seconds int    `json:"seconds"`
	Rounds  int    `json:"rounds,omitempty"`
	Period  string `json:"period"`
	// SecondsOf and RoundsOf are the settings the board has tables for.
	SecondsOf []int `json:"seconds_of"`
	RoundsOf  []int `json:"rounds_of,omitempty"`
	Total     int   `json:"total"`
	// Guest is a viewer who keeps no free results, so is on no board.
	Guest bool `json:"guest,omitempty"`
	// Rows are the first ones and the viewer's wherever it stands.
	Rows []topLine `json:"rows"`
}

type topLine struct {
	Rank  int    `json:"rank"`
	Name  string `json:"name"`
	Score int    `json:"score"`
	// Who is the handle for the mini profile, next to a Telegram player's
	// name the viewer may see.
	Who string `json:"who,omitempty"`
	Me  bool   `json:"me,omitempty"`
	// Anon is a player who hid themselves, "Anonymous" here.
	Anon bool `json:"anon,omitempty"`
	// Photo is a kept Telegram photo, given as in the mini profile.
	Photo string `json:"photo,omitempty"`
}

// freeWho is the viewer in free play: the Telegram person, signed in on the
// site with Telegram too, or an account without Telegram. A guest keeps no
// results.
func (v profileViewer) freeWho() db.FreeWho {
	switch {
	case v.user != 0:
		return db.FreeWho{UserID: v.user}
	case v.account != "":
		return db.FreeWho{Account: v.account}
	}
	return db.FreeWho{}
}

func (v profileViewer) ownsTop(r db.TopRow) bool {
	me := v.freeWho()
	return (me.UserID != 0 && r.UserID == me.UserID) || (me.Account != "" && r.Account == me.Account)
}

// top is one board: GET /api/top?board=classic&seconds=60&rounds=5&period=week.
func (s *Server) top(w http.ResponseWriter, r *http.Request) {
	ts, ok := s.Store.(TopStore)
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
	board := q.Get("board")
	if board == "" {
		board = "streak"
	}
	b, ok := topBoardOf(board, q.Get("seconds"), q.Get("rounds"))
	period := q.Get("period")
	if period == "" {
		period = "all"
	}
	if !ok || period != "all" && period != "week" {
		s.fail(w, r, errBadRequest)
		return
	}
	var since time.Time
	if period == "week" {
		since = s.Now().Add(-topWeek)
	}
	ctx := r.Context()
	t, err := ts.Top(ctx, b, since, topShown, v.freeWho())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var ids []int64
	for _, row := range t.Rows {
		if row.UserID != 0 && row.UserID != v.user {
			ids = append(ids, row.UserID)
		}
	}
	hidden, err := s.maskHidden(ctx, v.chatID, v.user, ids)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	photos := s.topPhotos(ctx, t.Rows, hidden)
	k := topKinds[board]
	out := topView{Board: board, Seconds: b.Seconds, Rounds: b.Rounds, Period: period,
		SecondsOf: k.seconds, RoundsOf: k.rounds, Total: t.Total, Guest: v.freeWho() == (db.FreeWho{}), Rows: make([]topLine, 0, len(t.Rows))}
	handles := s.handles()
	scope := "top:" + board + ":" + strconv.Itoa(b.Seconds) + ":" + strconv.Itoa(b.Rounds) + ":" + period + ":" + strconv.FormatInt(v.chatID, 10)
	for _, row := range t.Rows {
		l := topLine{Rank: row.Rank, Name: row.Name, Score: row.Score, Me: v.ownsTop(row)}
		switch {
		case hidden[row.UserID]:
			l.Name, l.Anon = anonName(v.lang), true
		case row.UserID != 0:
			l.Who = handles.Of(row.UserID, scope)
			l.Photo = photoURL(photos[row.UserID])
		}
		out.Rows = append(out.Rows, l)
	}
	writeJSON(w, http.StatusOK, out)
}

// topPhotos are the rows' Telegram photos: anyone's who is named here, never
// one of a player who is "Anonymous". Kept photos only, so a board waits for
// Telegram on nothing; the missing and the old ones are asked for after.
func (s *Server) topPhotos(ctx context.Context, rows []db.TopRow, hidden map[int64]bool) map[int64][]byte {
	var ids []int64
	for _, r := range rows {
		if r.UserID != 0 && !hidden[r.UserID] {
			ids = append(ids, r.UserID)
		}
	}
	return s.faces(ctx, ids)
}

type topPlace struct {
	Board   string `json:"board"`
	Seconds int    `json:"seconds"`
	Rounds  int    `json:"rounds,omitempty"`
	Rank    int    `json:"rank"`
	Of      int    `json:"of"`
	Score   int    `json:"score"`
}

// topPlaces is the viewer's place on every board they are on, all time. A
// failure leaves them out rather than the profile.
func (s *Server) topPlaces(ctx context.Context, v profileViewer) []topPlace {
	ts, ok := s.Store.(TopStore)
	me := v.freeWho()
	if !ok || me == (db.FreeWho{}) {
		return nil
	}
	places, err := ts.TopPlaces(ctx, me, time.Time{})
	if err != nil {
		s.Log.Warn("top places not read", "error", err)
		return nil
	}
	var out []topPlace
	for _, p := range places {
		name := topName(p.Board)
		k := topKinds[name]
		rounds := len(k.rounds) == 0 && p.Board.Rounds == 0 || slices.Contains(k.rounds, p.Board.Rounds)
		if !rounds || !slices.Contains(k.seconds, p.Board.Seconds) {
			continue
		}
		out = append(out, topPlace{Board: name, Seconds: p.Board.Seconds, Rounds: p.Board.Rounds, Rank: p.Rank, Of: p.Total, Score: p.Score})
	}
	return out
}
