// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"fmt"
	"html/template"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/FreshLabDev/tg"
	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// The leaderboards' part of the stand: made-up results on every board of
// /top, over the last week and before it, so the tables have rows to look
// at. The games have no places: a board reads only the results, and a game
// without rounds is how a reseed tells the made-up ones from those played on
// the stand.
func init() {
	Register(Extension{Name: "top", Seed: seedTop, Mount: mountTop, Panel: topPanel})
}

// topPeople are the boards' players besides the stand's own. Strength puts
// a player on the boards, from 0 (last) to 1 (first); the owner is in the
// middle so their own row stands among others. They play the closed days
// too, from a DM, so a row opens a mini profile with numbers, and each who
// does not hide has a public page under topSlug.
var topPeople = []struct {
	ID       int64
	Name     string
	Lang     string
	Strength float64
	Hidden   bool
}{
	{2001, "Sofía", "es", 0.93, false},
	{2002, "Lukas", "de", 0.81, false},
	{2003, "Chloé", "fr", 0.62, false},
	{2004, "Giulia", "it", 0.97, true},
	{2005, "Kasia", "pl", 0.44, false},
	{2006, "Jiří", "cs", 0.35, false},
	{2007, "Elif", "tr", 0.71, false},
	{2008, "Astrid", "sv", 0.56, false},
	{2009, "Алесь", "be", 0.27, false},
	{2010, "Núria", "ca", 0.66, false},
	{2011, "王芳", "zh", 0.86, false},
	{2012, "さくら", "ja", 0.48, false},
	{2013, "ليلى", "ar", 0.39, false},
	{2014, "Олег", "uk", 0.18, false},
	{2015, "Mateo", "es", 0.09, false},
	{2016, "Тарас", "uk", 0.76, false},
	{2017, "Hannah", "en", 0.31, false},
}

// topStand are the stand's own players on the boards. Лена hides herself:
// she plays the days too and is in none of Аня's groups, so the day's
// world table, the days page and /top all show her as "Anonymous" to Аня.
var topStand = []struct {
	ID       int64
	Strength float64
	Hidden   bool
}{
	{1001, 0.52, false},
	{1002, 0.42, false},
	{1003, 0.79, false},
	{1006, 0.89, false},
	{1007, 0.6, true},
	{1008, 0.23, false},
}

// topGoogle are accounts with Google alone, each with its own site player.
var topGoogle = []struct {
	Sub, Player, Name string
	Strength          float64
}{
	{"dev-top-google-1", "0d5e0000-0000-4000-8000-00000000a001", "Noah", 0.69},
	{"dev-top-google-2", "0d5e0000-0000-4000-8000-00000000a002", "Ayşe", 0.38},
	{"dev-top-google-3", "0d5e0000-0000-4000-8000-00000000a003", "Margarida", 0.14},
}

// topCode is the made-up party's code.
const topCode = "TP5Q8M2K"

// topBoards are /top's boards and settings, as internal/api/top.go has
// them. lo and hi are the scores of the last and the first player.
var topBoards = []struct {
	mode            string
	seconds, rounds int
	lo, hi          float64
}{
	{"streak", 30, 0, 1, 23},
	{"streak", 60, 0, 2, 31},
	{"timed", 180, 0, 2, 24},
	{"classic", 30, 3, 900, 4300},
	{"classic", 30, 5, 900, 4300},
	{"classic", 30, 10, 900, 4300},
	{"classic", 60, 3, 1200, 4700},
	{"classic", 60, 5, 1200, 4700},
	{"classic", 60, 10, 1200, 4700},
	{"classic", 90, 3, 1300, 4800},
	{"classic", 90, 5, 1300, 4800},
	{"classic", 90, 10, 1300, 4800},
	{"classic", 120, 3, 1400, 4900},
	{"classic", 120, 5, 1400, 4900},
	{"classic", 120, 10, 1400, 4900},
}

// topPlayer is one row maker: a Telegram person or a Google account's site
// player.
type topPlayer struct {
	userID   int64
	player   string
	name     string
	strength float64
	owner    bool
}

func seedTop(ctx context.Context, e *Env) error {
	var all []topPlayer
	for _, p := range topPeople {
		if err := e.Store.TouchPerson(ctx, tg.User{ID: p.ID, FirstName: p.Name, LanguageCode: p.Lang}); err != nil {
			return err
		}
		if p.Hidden {
			if err := e.Store.SetHideMe(ctx, p.ID, true); err != nil {
				return err
			}
		}
		all = append(all, topPlayer{userID: p.ID, name: p.Name, strength: p.Strength})
		if !p.Hidden {
			if err := e.Store.SetPublicSlug(ctx, p.ID, topSlug(p.ID)); err != nil {
				return err
			}
		}
	}
	for _, s := range topStand {
		p, ok := e.Player(s.ID)
		if !ok {
			continue
		}
		if s.Hidden {
			if err := e.Store.SetHideMe(ctx, p.ID, true); err != nil {
				return err
			}
		}
		all = append(all, topPlayer{userID: p.ID, name: p.Name, strength: s.Strength, owner: p.Owner})
	}
	now := e.Clock.Now()
	for _, g := range topGoogle {
		if _, err := e.Pool.Exec(ctx, `INSERT INTO maply.site_player (id, name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`, g.Player, g.Name); err != nil {
			return fmt.Errorf("google player %s: %w", g.Name, err)
		}
		rules := db.Rules{Open: func(round.Date) bool { return false }, Now: now}
		if _, _, err := e.Store.SignIn(ctx, db.Identity{Kind: db.KindGoogle, GoogleSub: g.Sub}, g.Player, rules); err != nil {
			return fmt.Errorf("google account %s: %w", g.Name, err)
		}
		all = append(all, topPlayer{player: g.Player, name: g.Name, strength: g.Strength})
	}
	if err := writeTop(ctx, e, all, now); err != nil {
		return err
	}
	return topDays(ctx, e, now)
}

// topSlug is a made-up player's public page: fixed, so the stand's panel can
// link it. A real slug is random (profile.NewSlug).
func topSlug(id int64) string { return fmt.Sprintf("devtop-%015d", id) }

// topDays answers the days over in every zone for topPeople, each from their
// DM before their deadline, as seedDays does for the stand's own players,
// and keeps every such day's world places again with them in, as the final
// world job would have. Today stays as the stand has it. Played again, it
// finds the answers there and keeps the same places.
func topDays(ctx context.Context, e *Env, now time.Time) error {
	rows, err := e.Pool.Query(ctx, `SELECT date FROM maply.day_closed ORDER BY date`)
	if err != nil {
		return err
	}
	var dates []round.Date
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		dates = append(dates, round.DateIn(t, time.UTC))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(2026, 12))
	played := 0
	for _, d := range dates {
		r, err := e.Store.EnsureRound(ctx, d)
		if err != nil {
			return err
		}
		answer, err := e.Store.Answer(ctx, r.PlaceID)
		if err != nil {
			return err
		}
		for _, p := range topPeople {
			// The stronger play more days, and miss by less.
			if rng.Float64() > 0.35+0.5*p.Strength {
				continue
			}
			who := Player{ID: p.ID, Name: p.Name, Lang: p.Lang, Skill: 150 + 2600*(1-p.Strength)}
			ok, err := seedGuess(ctx, e, rng, d, who, answer.Point, now)
			if err != nil {
				return fmt.Errorf("day %s of %s: %w", d, p.Name, err)
			}
			if ok {
				played++
			}
		}
		place := scoring.Place{Point: answer.Point, Country: answer.CountryISO, Continent: answer.Continent, City: answer.CityShape}
		if _, err := e.Store.ScoreDue(ctx, d, now, func(pt geo.Point) scoring.Score {
			return scoring.Judge(pt, place, e.Countries)
		}); err != nil {
			return err
		}
		b := &pgx.Batch{}
		b.Queue(`DELETE FROM maply.day_place WHERE date = $1`, d.Time())
		b.Queue(`INSERT INTO maply.day_place (date, user_id, player, km, points, rank, n)
			SELECT date, user_id, player, km, points, rank, n FROM maply.day_rank WHERE date = $1`, d.Time())
		if err := e.Pool.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("keep day %s: %w", d, err)
		}
	}
	e.Log.Info("leaderboards' players on the closed days", "days", len(dates), "answers", played)
	return nil
}

// writeTop plays the made-up games: on each board a player plays one to
// three times, more often the stronger they are, some games this week and
// some before it. The owner plays every board and at least once this week.
func writeTop(ctx context.Context, e *Env, all []topPlayer, now time.Time) error {
	rnd := rand.New(rand.NewPCG(2026, 11))
	b := &pgx.Batch{}
	games := 0
	for _, p := range all {
		for _, bd := range topBoards {
			if !p.owner && rnd.Float64() > 0.5+0.45*p.strength {
				continue
			}
			for k := range 1 + rnd.IntN(3) {
				var age time.Duration
				if p.owner && k == 0 || rnd.Float64() < 0.4 {
					age = time.Duration(1+rnd.IntN(6*24)) * time.Hour
				} else {
					age = time.Duration(8*24+rnd.IntN(70*24)) * time.Hour
				}
				// Older games are a little worse: a player gets better.
				f := (0.82 + 0.22*rnd.Float64()) * (1 - float64(age)/float64(400*24*time.Hour))
				topGame(b, p, bd.mode, bd.seconds, bd.rounds, bd.lo+(bd.hi-bd.lo)*p.strength*f, rnd, now.Add(-age))
				games++
			}
		}
	}
	// One party, which counts on its board as a solo game does.
	party, err := topParty(e, all, now.Add(-30*time.Hour))
	if err != nil {
		return err
	}
	for _, q := range party {
		b.Queue(q.sql, q.args...)
	}
	if err := e.Pool.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("top games: %w", err)
	}
	e.Log.Info("leaderboards seeded", "players", len(all), "games", games+1)
	return nil
}

type topQuery struct {
	sql  string
	args []any
}

// topResult is a finished game's line, worked out from its score: the
// streak, the countries against the clock, or the points of a classic.
type topResult struct {
	points, played, answered, countries int
	km                                  float64
	kmRounds                            []float64
	timeMs                              int64
	streak                              *int
}

func topLine(mode string, seconds, rounds int, score float64, rnd *rand.Rand) topResult {
	var r topResult
	switch mode {
	case "streak":
		n := max(0, int(math.Round(score)))
		r.streak, r.played, r.answered, r.countries = &n, n+1, n+1, n
	case "timed":
		n := max(1, int(math.Round(score)))
		r.played, r.answered, r.countries = n+1+rnd.IntN(4), n+1, n
	default:
		per := min(5200, max(200, score))
		r.points = int(math.Round(per * float64(rounds)))
		r.played, r.answered = rounds, rounds
		r.countries = min(rounds, int(math.Round(float64(rounds)*per/5200+0.3)))
	}
	for range r.played {
		r.kmRounds = append(r.kmRounds, math.Round(40+rnd.Float64()*2400))
	}
	for _, km := range r.kmRounds {
		r.km += km
	}
	if mode == "timed" {
		r.timeMs = 180_000
	} else {
		r.timeMs = int64(r.played) * int64(seconds) * int64(400+rnd.IntN(500))
	}
	return r
}

func topGame(b *pgx.Batch, p topPlayer, mode string, seconds, rounds int, score float64, rnd *rand.Rand, at time.Time) {
	r := topLine(mode, seconds, rounds, score, rnd)
	start := at.Add(-time.Duration(r.timeMs) * time.Millisecond)
	user, player := topWho(p)
	b.Queue(`
		WITH g AS (
		  INSERT INTO maply.free_game (kind, mode, seconds, rounds, status, created_at, started_at, finished_at)
		  VALUES ('solo', $1, $2, $3, 'done', $4, $4, $5) RETURNING id),
		m AS (
		  INSERT INTO maply.free_member (game, m, user_id, player, name, joined_at, last_seen_at)
		  SELECT id, 1, $6, $7, $8, $4, $5 FROM g RETURNING game)
		INSERT INTO maply.free_result (game, m, user_id, player, kind, mode, seconds, rounds, points, points_base, km, km_rounds,
		                               time_ms, played, answered, countries, from_round, streak, finished_at)
		SELECT game, 1, $6, $7, 'solo', $1, $2, $3, $9, $10, $11, $12, $13, $14, $15, $16, 1, $17, $5 FROM m`,
		mode, seconds, nullRounds(rounds), start, at, user, player, p.name,
		r.points, r.points*95/100, r.km, r.kmRounds, r.timeMs, r.played, r.answered, r.countries, r.streak)
}

// topParty is a party of the owner and two others on classic, 60 seconds
// and five places.
func topParty(e *Env, all []topPlayer, at time.Time) ([]topQuery, error) {
	var who []topPlayer
	for _, p := range all {
		if p.owner || p.userID == 1002 || p.userID == 1006 {
			who = append(who, p)
		}
	}
	if len(who) == 0 {
		return nil, nil
	}
	rnd := rand.New(rand.NewPCG(7, 3))
	start := at.Add(-6 * time.Minute)
	q := []topQuery{{`
		INSERT INTO maply.free_game (code, kind, mode, seconds, rounds, status, created_at, started_at, finished_at)
		VALUES ($1, 'party', 'classic', 60, 5, 'done', $2, $2, $3)`, []any{topCode, start.Add(-2 * time.Minute), at}}}
	lines := make([]topResult, len(who))
	for i, p := range who {
		lines[i] = topLine("classic", 60, 5, 1200+3500*p.strength*(0.9+0.15*rnd.Float64()), rnd)
	}
	for i, p := range who {
		place := 1
		for _, l := range lines {
			if l.points > lines[i].points {
				place++
			}
		}
		r := lines[i]
		user, player := topWho(p)
		q = append(q,
			topQuery{`
				INSERT INTO maply.free_member (game, m, user_id, player, name, joined_at, last_seen_at)
				SELECT id, $2, $3, $4, $5, $6, $7 FROM maply.free_game WHERE code = $1`,
				[]any{topCode, i + 1, user, player, p.name, start.Add(-time.Minute), at}},
			topQuery{`
				INSERT INTO maply.free_result (game, m, user_id, player, kind, mode, seconds, rounds, points, points_base, km, km_rounds,
				                               time_ms, played, answered, countries, from_round, place, players, beaten, finished_at)
				SELECT id, $2, $3, $4, 'party', 'classic', 60, 5, $5, $6, $7, $8, $9, 5, 5, $10, 1, $11, $12, $13, $14
				  FROM maply.free_game WHERE code = $1`,
				[]any{topCode, i + 1, user, player, r.points, r.points * 95 / 100, r.km, r.kmRounds, r.timeMs, r.countries,
					place, len(who), len(who) - place, at}})
	}
	return q, nil
}

func topWho(p topPlayer) (user, player any) {
	if p.player != "" {
		return nil, p.player
	}
	return p.userID, nil
}

func nullRounds(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func mountTop(e *Env, mux *http.ServeMux) {
	// POST /dev/top/seed plays the made-up games again, up to the stand's
	// clock as it stands: the games played on the stand stay.
	mux.HandleFunc("POST /dev/top/seed", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if _, err := e.Pool.Exec(ctx, `
			DELETE FROM maply.free_game g
			 WHERE g.status = 'done' AND (g.code = $1 OR g.kind = 'solo')
			   AND NOT EXISTS (SELECT 1 FROM maply.free_round fr WHERE fr.game = g.id)`, topCode); err != nil {
			e.back(w, r, "Таблицы: "+err.Error())
			return
		}
		if err := seedTop(ctx, e); err != nil {
			e.back(w, r, "Таблицы: "+err.Error())
			return
		}
		e.back(w, r, "Таблицы засеяны заново")
	})
}

func topPanel(e *Env, r *http.Request) template.HTML {
	var b strings.Builder
	b.WriteString(`<section><h2>Таблицы</h2><p>`)
	owner := e.Players[0]
	for _, p := range e.Players {
		if p.Owner {
			owner = p
		}
	}
	for _, l := range []struct{ text, platform string }{{"телефон", "ios"}, {"Telegram Desktop", "tdesktop"}} {
		fmt.Fprintf(&b, `<a class="btn" href="/dev/open?user=%d&path=/top&platform=%s">%s: %s</a> `,
			owner.ID, l.platform, template.HTMLEscapeString(owner.Name), template.HTMLEscapeString(l.text))
	}
	b.WriteString(`<a class="btn" href="/top">Сайт</a></p>`)
	var players, games int
	if err := e.Pool.QueryRow(r.Context(), `
		SELECT count(DISTINCT COALESCE(user_id::text, player::text)), count(*)
		  FROM maply.free_result WHERE seconds IS NOT NULL AND mode <> 'archive' AND kind <> 'challenge'`).Scan(&players, &games); err != nil {
		fmt.Fprintf(&b, `<p>%s</p>`, template.HTMLEscapeString(err.Error()))
	} else {
		fmt.Fprintf(&b, `<p>На досках: игроков %d, игр %d. Скрыты Giulia и Лена: у Ани и гостя они «Аноним». У аккаунтов Noah, Ayşe и Margarida только Google: у их строк нет мини-профиля.</p>`, players, games)
	}
	// The made-up players play the closed days too: a row opens a mini profile with numbers, and these are their
	// public pages.
	b.WriteString(`<p>Публичные страницы игроков таблиц: `)
	sep := ""
	for _, p := range topPeople {
		if !p.Hidden {
			fmt.Fprintf(&b, `%s<a href="/p/%s">%s</a>`, sep, topSlug(p.ID), template.HTMLEscapeString(p.Name))
			sep = " · "
		}
	}
	b.WriteString(`</p>`)
	b.WriteString(`<form method="post" action="/dev/top/seed"><button class="btn">Засеять таблицы заново</button></form></section>`)
	return template.HTML(b.String())
}
