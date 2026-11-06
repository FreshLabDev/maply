// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// Player is a fake Telegram user.
type Player struct {
	ID       int64
	Name     string
	Username string
	Lang     string
	// Zone is the zone of their DM with the bot; empty for someone who
	// plays only in groups.
	Zone   string
	Groups []int64
	// Owner is the player the stand is for: the seed leaves their answer of
	// today for them to give.
	Owner bool
	// Skill is the median miss of their seeded answers, in km.
	Skill float64
}

// Group is a fake Telegram group with maply in it.
type Group struct {
	ID    int64
	Title string
	Zone  string
	Lang  string
}

const (
	FriendsGroup int64 = -1001001
	TokyoGroup   int64 = -1001002
)

var groups = []Group{
	{ID: FriendsGroup, Title: "Карта друзей", Zone: "Europe/Kyiv", Lang: "ru"},
	{ID: TokyoGroup, Title: "Geo Tokyo", Zone: "Asia/Tokyo", Lang: "en"},
}

var players = []Player{
	{ID: 1001, Name: "Аня", Username: "anya", Lang: "ru", Zone: "Europe/Kyiv", Groups: []int64{FriendsGroup}, Owner: true, Skill: 600},
	{ID: 1002, Name: "Боря", Username: "borya", Lang: "ru", Zone: "Europe/Kyiv", Groups: []int64{FriendsGroup}, Skill: 900},
	{ID: 1003, Name: "Вика", Lang: "uk", Zone: "Europe/Kyiv", Groups: []int64{FriendsGroup}, Skill: 400},
	{ID: 1004, Name: "Гена", Username: "gena", Lang: "ru", Groups: []int64{FriendsGroup}, Skill: 1600},
	{ID: 1005, Name: "Дима", Lang: "be", Zone: "Europe/Minsk", Groups: []int64{FriendsGroup, TokyoGroup}, Skill: 1100},
	{ID: 1006, Name: "Kim", Username: "kim", Lang: "en", Zone: "Asia/Tokyo", Groups: []int64{TokyoGroup}, Skill: 250},
	{ID: 1007, Name: "Лена", Lang: "ru", Zone: "Europe/Kyiv", Skill: 1300},
	{ID: 1008, Name: "Marco 🦊", Username: "marco", Lang: "it", Zone: "Europe/Berlin", Groups: []int64{TokyoGroup}, Skill: 700},
}

// sitePlayers are browsers that played on the site without signing in.
var sitePlayers = []struct {
	ID, Name string
	Skill    float64
}{
	{ID: "0d5e0000-0000-4000-8000-000000000001", Name: "Ольга", Skill: 800},
	{ID: "0d5e0000-0000-4000-8000-000000000002", Name: "", Skill: 2000},
}

func (e *Env) Player(id int64) (Player, bool) {
	for _, p := range e.Players {
		if p.ID == id {
			return p, true
		}
	}
	return Player{}, false
}

func (e *Env) Group(id int64) (Group, bool) {
	for _, g := range e.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

func (p Player) User() tg.User {
	return tg.User{ID: p.ID, FirstName: p.Name, Username: p.Username, LanguageCode: p.Lang}
}

// seed wipes the stand and fills it again: places, people, the past days
// played and closed through the store, and today with the others' answers.
func seed(ctx context.Context, e *Env) error {
	start := time.Now()
	if err := wipe(ctx, e); err != nil {
		return err
	}
	n, err := seedPlaces(ctx, e)
	if err != nil {
		return err
	}
	if err := seedPeople(ctx, e); err != nil {
		return err
	}
	played, err := seedDays(ctx, e)
	if err != nil {
		return err
	}
	for _, x := range extensions {
		if x.Seed != nil {
			if err := x.Seed(ctx, e); err != nil {
				return fmt.Errorf("seed %s: %w", x.Name, err)
			}
		}
	}
	e.Log.Info("stand seeded", "places", n, "days", e.Days+1, "answers", played, "took", time.Since(start).Round(time.Millisecond))
	return nil
}

// wipe empties every table of the maply schema but the migration ledger, and
// the people and chats of the local core. New tables are wiped without
// being listed here.
func wipe(ctx context.Context, e *Env) error {
	var list *string
	if err := e.Pool.QueryRow(ctx, `
		SELECT string_agg(format('%I.%I', schemaname, tablename), ', ')
		  FROM pg_tables
		 WHERE schemaname = 'maply' AND tablename NOT IN ('schema_migrations', 'runtime_state')`).Scan(&list); err != nil {
		return err
	}
	stmts := []string{`TRUNCATE core.person, core.chat CASCADE`, `TRUNCATE core.language_observation`}
	if list != nil {
		stmts = append([]string{`TRUNCATE ` + *list + ` RESTART IDENTITY CASCADE`}, stmts...)
	}
	for _, s := range stmts {
		if _, err := e.Pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return os.RemoveAll(e.ImagesDir)
}

// seedPlaces loads the curation folders' places.sql and links their images,
// sharpened ones first; with no folder it makes synthetic places.
func seedPlaces(ctx context.Context, e *Env) (int, error) {
	if err := os.MkdirAll(e.ImagesDir, 0o755); err != nil {
		return 0, err
	}
	if len(e.Places) == 0 {
		return syntheticPlaces(ctx, e)
	}
	for _, dir := range e.Places {
		sql, err := os.ReadFile(filepath.Join(dir, "places.sql"))
		if err != nil {
			return 0, fmt.Errorf("places of %s: %w", dir, err)
		}
		if _, err := e.Pool.Exec(ctx, `SET search_path = maply, core; `+string(sql)+`; SET search_path = maply`); err != nil {
			return 0, fmt.Errorf("load %s/places.sql: %w", dir, err)
		}
		for _, sub := range []string{"sharp", "images"} {
			files, _ := os.ReadDir(filepath.Join(dir, sub))
			for _, f := range files {
				if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
					continue
				}
				dst := filepath.Join(e.ImagesDir, f.Name())
				if _, err := os.Lstat(dst); err == nil {
					continue
				}
				src, err := filepath.Abs(filepath.Join(dir, sub, f.Name()))
				if err != nil {
					return 0, err
				}
				if err := os.Symlink(src, dst); err != nil {
					return 0, err
				}
			}
		}
	}
	// A place whose picture is not here would be a broken day.
	rows, err := e.Pool.Query(ctx, `SELECT id, image_key FROM maply.place WHERE status = 'approved'`)
	if err != nil {
		return 0, err
	}
	var missing []int64
	n := 0
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			rows.Close()
			return 0, err
		}
		if _, err := os.Stat(filepath.Join(e.ImagesDir, key)); err != nil {
			missing = append(missing, id)
			continue
		}
		n++
	}
	rows.Close()
	if len(missing) > 0 {
		if _, err := e.Pool.Exec(ctx, `UPDATE maply.place SET status = 'rejected' WHERE id = ANY($1)`, missing); err != nil {
			return 0, err
		}
		e.Log.Warn("places without a picture left out", "count", len(missing))
	}
	return n, nil
}

type synthetic struct {
	city, iso, continent string
	lat, lon             float64
}

var syntheticList = []synthetic{
	{"Brașov", "RO", "europe", 45.6427, 25.5887}, {"Porto", "PT", "europe", 41.1496, -8.611},
	{"Kraków", "PL", "europe", 50.0614, 19.9366}, {"Tromsø", "NO", "europe", 69.6492, 18.9553},
	{"Sevilla", "ES", "europe", 37.3891, -5.9845}, {"Tallinn", "EE", "europe", 59.437, 24.7536},
	{"Split", "HR", "europe", 43.5081, 16.4402}, {"Reykjavík", "IS", "europe", 64.1466, -21.9426},
	{"Marrakesh", "MA", "africa", 31.6295, -7.9811}, {"Nairobi", "KE", "africa", -1.2921, 36.8219},
	{"Cape Town", "ZA", "africa", -33.9249, 18.4241}, {"Kampala", "UG", "africa", 0.3476, 32.5825},
	{"Kyoto", "JP", "asia", 35.0116, 135.7681}, {"Hanoi", "VN", "asia", 21.0278, 105.8342},
	{"Almaty", "KZ", "asia", 43.2389, 76.8897}, {"Tbilisi", "GE", "asia", 41.7151, 44.8271},
	{"Bangkok", "TH", "asia", 13.7563, 100.5018}, {"Ubud", "ID", "asia", -8.5069, 115.2625},
	{"Seattle", "US", "north_america", 47.6062, -122.3321}, {"Oaxaca", "MX", "north_america", 17.0732, -96.7266},
	{"Québec", "CA", "north_america", 46.8139, -71.208}, {"Cusco", "PE", "south_america", -13.5319, -71.9675},
	{"Valparaíso", "CL", "south_america", -33.0472, -71.6127}, {"Ushuaia", "AR", "south_america", -54.8019, -68.303},
	{"Hobart", "AU", "oceania", -42.8821, 147.3272}, {"Queenstown", "NZ", "oceania", -45.0312, 168.6626},
}

func syntheticPlaces(ctx context.Context, e *Env) (int, error) {
	for i, s := range syntheticList {
		key := fmt.Sprintf("DevPano%02d%011d", i, i*7919)
		city := fmt.Sprintf(`{"en":%q,"ru":%q}`, s.city, s.city)
		fact := fmt.Sprintf(`{"en":"A synthetic place for the local stand: %s.","ru":"Выдуманное место для локального стенда: %s."}`, s.city, s.city)
		if _, err := e.Pool.Exec(ctx, `
			INSERT INTO maply.place (status, source, source_id, source_url, license, kind, lat, lon, image_key,
			                         country_iso, continent, city, fact, difficulty, reviewed_at, author, heading)
			VALUES ('approved', 'mapillary', $1, 'https://example.com', 'CC-BY-SA-4.0', 'panorama', $2, $3, $4,
			        $5, $6, $7, $8, $9, now(), 'maply-dev', 0)`,
			fmt.Sprint(900000+i), s.lat, s.lon, key, s.iso, s.continent, city, fact, 1+i%5); err != nil {
			return 0, fmt.Errorf("synthetic place %s: %w", s.city, err)
		}
		if err := pano(filepath.Join(e.ImagesDir, key), i); err != nil {
			return 0, err
		}
	}
	return len(syntheticList), nil
}

// pano draws a 2:1 picture: a sky, a horizon and stripes in a colour of
// its own, so two synthetic days do not look the same.
func pano(path string, n int) error {
	img := image.NewRGBA(image.Rect(0, 0, 2048, 1024))
	hue := float64(n) * 0.6
	for y := 0; y < 1024; y++ {
		for x := 0; x < 2048; x++ {
			c := color.RGBA{uint8(110 + 30*math.Sin(hue)), 170, 220, 255}
			if y > 512 {
				g := uint8(90 + 40*math.Sin(float64(x)/40+hue))
				c = color.RGBA{uint8(60 + 50*math.Cos(hue)), g, 70, 255}
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 80})
}

// seedPeople puts the players in core as the bot would have met them: in
// their groups and, for those who have one, in their DM.
func seedPeople(ctx context.Context, e *Env) error {
	for _, g := range e.Groups {
		if err := e.Store.FixZone(ctx, g.ID, g.Zone); err != nil {
			return err
		}
		if err := e.Store.SetChatLang(ctx, g.ID, g.Lang); err != nil {
			return err
		}
	}
	for _, p := range e.Players {
		u := p.User()
		for _, gid := range p.Groups {
			g, _ := e.Group(gid)
			if err := e.Store.Touch(ctx, u, &tg.Chat{ID: g.ID, Type: "supergroup", Title: g.Title}); err != nil {
				return err
			}
		}
		if p.Zone != "" {
			if err := e.Store.Touch(ctx, u, nil); err != nil {
				return err
			}
			if err := e.Store.FixZone(ctx, p.ID, p.Zone); err != nil {
				return err
			}
		}
	}
	for _, s := range sitePlayers {
		name := s.Name
		if err := e.Store.SitePlayer(ctx, s.ID, &name); err != nil {
			return err
		}
	}
	return nil
}

// seedDays plays the past days and today. Every day is created by the store's
// own queue, answered before each person's own deadline, scored by the real
// judge, and kept once it is over in every zone, as the final world job does.
// A day nobody played is skipped instead, and its place goes back.
func seedDays(ctx context.Context, e *Env) (int, error) {
	now := e.Clock.Now()
	today := round.DateIn(now, e.Zones.For("Europe/Kyiv", ""))
	rng := rand.New(rand.NewPCG(42, uint64(today.DaysSince(round.NewDate(2026, time.January, 1)))))
	played := 0
	for back := e.Days; back >= 0; back-- {
		d := today.AddDays(-back)
		r, err := e.Store.EnsureRound(ctx, d)
		if errors.Is(err, db.ErrNoPlace) {
			e.Log.Warn("the place queue ran out", "date", d)
			break
		}
		if err != nil {
			return played, err
		}
		answer, err := e.Store.Answer(ctx, r.PlaceID)
		if err != nil {
			return played, err
		}
		// One day in nine nobody played; on the others most people did.
		nobody := back > 0 && back%9 == 4
		for _, p := range e.Players {
			if nobody || back == 0 && p.Owner {
				continue
			}
			if rng.Float64() < 0.22 {
				continue
			}
			ok, err := seedGuess(ctx, e, rng, d, p, answer.Point, now)
			if err != nil {
				return played, err
			}
			if ok {
				played++
			}
		}
		for _, s := range sitePlayers {
			if nobody || back == 0 || rng.Float64() < 0.4 {
				continue
			}
			at := d.Reveal(e.Zones.For("Europe/Kyiv", "")).Add(-time.Duration(1+rng.IntN(600)) * time.Minute)
			err := e.Store.InsertSiteGuess(ctx, db.NewSiteGuess{
				Date: d, Player: s.ID, Pin: near(rng, answer.Point, s.Skill),
				AnsweredAt: at, Deadline: e.Zones.LastReveal(d),
			})
			if err != nil && !errors.Is(err, db.ErrAlreadyAnswered) {
				return played, err
			}
			played++
		}
		place := scoring.Place{Point: answer.Point, Country: answer.CountryISO, Continent: answer.Continent, City: answer.CityShape}
		if _, err := e.Store.ScoreDue(ctx, d, now, func(pt geo.Point) scoring.Score {
			return scoring.Judge(pt, place, e.Countries)
		}); err != nil {
			return played, err
		}
		if now.After(e.Zones.LastReveal(d)) {
			// As the final world job does: a day nobody answered is skipped and
			// its place goes back, any other is kept.
			n, err := e.Store.DayAnswers(ctx, d)
			if err != nil {
				return played, err
			}
			if n == 0 {
				if _, err := e.Store.SkipDay(ctx, d, round.DateIn(now, time.UTC).AddDays(db.ReturnDays)); err != nil {
					return played, err
				}
				continue
			}
			if err := e.Store.KeepDay(ctx, d); err != nil {
				return played, err
			}
		}
	}
	return played, nil
}

// seedGuess answers d for p from one of their chats, before their deadline
// and, for today, before now.
func seedGuess(ctx context.Context, e *Env, rng *rand.Rand, d round.Date, p Player, place geo.Point, now time.Time) (bool, error) {
	kind, chat := "dm", int64(0)
	zone := p.Zone
	if len(p.Groups) > 0 && (p.Zone == "" || rng.Float64() < 0.7) {
		kind, chat = "group", p.Groups[rng.IntN(len(p.Groups))]
		g, _ := e.Group(chat)
		zone = g.Zone
	}
	loc := e.Zones.For(zone, p.Lang)
	deadline, err := e.Store.Deadline(ctx, e.Zones, p.ID, d, loc)
	if err != nil {
		return false, err
	}
	from := d.At(7, 0, loc)
	until := deadline.Add(-time.Minute)
	if until.After(now) {
		until = now.Add(-time.Minute)
	}
	if !until.After(from) {
		return false, nil
	}
	at := from.Add(time.Duration(rng.Int64N(int64(until.Sub(from)))))
	err = e.Store.InsertGuess(ctx, db.NewGuess{
		Date: d, UserID: p.ID, Context: kind, ChatID: chat,
		Pin: near(rng, place, p.Skill), AnsweredAt: at, Deadline: deadline,
	})
	if errors.Is(err, db.ErrAlreadyAnswered) {
		return false, nil
	}
	return err == nil, err
}

// near is a point at a random distance from place, with median km, in a
// random direction: sometimes a city away, sometimes another continent.
func near(rng *rand.Rand, place geo.Point, median float64) geo.Point {
	km := median * math.Exp(rng.NormFloat64()*1.1)
	km = math.Max(0.3, math.Min(km, 15000))
	return Destination(place, rng.Float64()*360, km)
}

// Destination is the point km away from p on the initial bearing deg.
func Destination(p geo.Point, deg, km float64) geo.Point {
	const r = 6371.0088
	lat1 := p.Lat * math.Pi / 180
	lon1 := p.Lon * math.Pi / 180
	brg := deg * math.Pi / 180
	ad := km / r
	lat2 := math.Asin(math.Sin(lat1)*math.Cos(ad) + math.Cos(lat1)*math.Sin(ad)*math.Cos(brg))
	lon2 := lon1 + math.Atan2(math.Sin(brg)*math.Sin(ad)*math.Cos(lat1), math.Cos(ad)-math.Sin(lat1)*math.Sin(lat2))
	lon := math.Mod(lon2*180/math.Pi+540, 360) - 180
	return geo.Point{Lat: lat2 * 180 / math.Pi, Lon: lon}
}
