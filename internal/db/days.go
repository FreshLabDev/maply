// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/FreshLabDev/maply/internal/round"
)

// DaysQuery asks for one page of the days list, newest first.
type DaysQuery struct {
	// Newest is the newest date to list: the newest begun in any zone.
	Newest round.Date
	// Before, when not zero, lists the dates before it: the cursor of the
	// page that comes after another.
	Before round.Date
	N      int
	// Final is the newest date over in every zone. A later date is open
	// somewhere, and its row carries nothing scored and nothing about its
	// place: an early zone's numbers would hint at it to a later one.
	Final round.Date
	// Seen is the newest date begun where the viewer is. An open date up to
	// it carries its puzzle's picture, which anyone playing it sees; a
	// later one would be tomorrow's picture before tomorrow.
	Seen round.Date
	// The viewer: a Telegram player, a site player, or neither.
	UserID int64
	Player string
	// ChatID is the group the app was opened from, 0 for none.
	ChatID int64
}

// DayRow is one date of the days list.
type DayRow struct {
	Date  round.Date
	DayNo int
	// Players is how many answers are scored, Pending how many wait for
	// their deadline; an open date has only their sum.
	Players int
	Pending int
	// The world's numbers, place and strip of a final date only.
	MedianKm   *float64
	BestKm     *float64
	CountryPct int
	Country    string
	City       map[string]string
	// Thumb is the key of the day's strip, set only for a final day kept
	// closed whose strip is drawn.
	Thumb string
	// Image and Kind are the puzzle's picture, for an open date only and
	// only up to Seen.
	Image string
	Kind  string
	// Played says the viewer has answered on the date.
	Played bool
	// Me is the viewer's score and place, on a final date only.
	Me *Mine
	// Group is the group's part of a final date.
	Group *ChatDay
}

// DaysPage is one page of the list.
type DaysPage struct {
	Days []DayRow
	// More says there are older dates past the last.
	More bool
	// Best is the viewer's best kept day, by points; zero without one.
	Best round.Date
}

// DaysPage reads a page of the days list in one query: the world's numbers,
// the viewer's own line, the group's and the strip of every date on it.
func (s *Store) DaysPage(ctx context.Context, q DaysQuery) (DaysPage, error) {
	if q.N <= 0 {
		return DaysPage{}, nil
	}
	var before, player any
	if !q.Before.IsZero() {
		before = q.Before.Time()
	}
	if q.Player != "" {
		player = q.Player
	}
	// A kept day is read as it was kept; a final day the job has not kept
	// yet is ranked live, as are the open ones, whose ranks are not sent.
	rows, err := s.pool.Query(ctx, `
		WITH page AS (
		  SELECT r.date, r.day_no, r.place_id, r.date <= $4::date AS final,
		         EXISTS (SELECT 1 FROM day_closed c WHERE c.date = r.date) AS kept
		    FROM round r
		   WHERE r.date <= $1::date AND ($2::date IS NULL OR r.date < $2::date)
		   ORDER BY r.date DESC
		   LIMIT $3::int + 1),
		world AS (
		  SELECT ds.date, count(*) AS players,
		         percentile_cont(0.5) WITHIN GROUP (ORDER BY ds.km) AS median_km,
		         min(ds.km) AS best_km,
		         round(100.0 * count(*) FILTER (WHERE ds.pts_bonus >= 800) / count(*))::int AS country_pct
		    FROM day_score ds
		   WHERE ds.date IN (SELECT date FROM page)
		   GROUP BY ds.date),
		waiting AS (
		  SELECT w.date, count(*) AS n
		    FROM (SELECT date FROM guess WHERE km IS NULL AND date IN (SELECT date FROM page)
		          UNION ALL
		          SELECT date FROM site_guess WHERE km IS NULL AND date IN (SELECT date FROM page)) w
		   GROUP BY w.date),
		mine AS (
		  SELECT dp.date, dp.km, dp.points, dp.rank
		    FROM day_place dp
		   WHERE dp.date IN (SELECT date FROM page WHERE final)
		     AND (dp.user_id = $5::bigint OR dp.player = $6::uuid)
		  UNION ALL
		  SELECT dr.date, dr.km, dr.points, dr.rank
		    FROM day_rank dr
		   WHERE dr.date IN (SELECT date FROM page WHERE final AND NOT kept)
		     AND (dr.user_id = $5::bigint OR dr.player = $6::uuid)),
		played AS (
		  SELECT date FROM guess WHERE user_id = $5::bigint AND date IN (SELECT date FROM page)
		  UNION
		  SELECT date FROM site_guess WHERE player = $6::uuid AND date IN (SELECT date FROM page)),
		members AS (
		  SELECT telegram_user_id AS id FROM core.maply_presence WHERE chat_id = $7::bigint AND $7::bigint <> 0),
		grp AS (
		  SELECT p.date,
		         count(g.user_id) AS answered,
		         (array_agg(COALESCE(pe.first_name, '') ORDER BY ds.points DESC, ds.km, ds.answered_at, ds.user_id)
		            FILTER (WHERE ds.points IS NOT NULL))[1] AS best_name,
		         (array_agg(ds.km ORDER BY ds.points DESC, ds.km, ds.answered_at, ds.user_id)
		            FILTER (WHERE ds.points IS NOT NULL))[1] AS best_km
		    FROM page p
		    LEFT JOIN guess g ON g.date = p.date AND g.user_id IN (SELECT id FROM members)
		    LEFT JOIN day_score ds ON ds.date = g.date AND ds.user_id = g.user_id
		    LEFT JOIN core.person pe ON pe.telegram_user_id = g.user_id
		   WHERE p.final AND $7::bigint <> 0
		   GROUP BY p.date),
		best AS (
		  SELECT dp.date FROM day_place dp
		   WHERE dp.user_id = $5::bigint OR dp.player = $6::uuid
		   ORDER BY dp.points DESC, dp.km, dp.date
		   LIMIT 1)
		SELECT p.date, p.day_no,
		       COALESCE(w.players, 0), COALESCE(wt.n, 0),
		       CASE WHEN p.final THEN w.median_km END,
		       CASE WHEN p.final THEN w.best_km END,
		       CASE WHEN p.final THEN COALESCE(w.country_pct, 0) ELSE 0 END,
		       CASE WHEN p.final THEN pl.country_iso END,
		       CASE WHEN p.final THEN pl.city END,
		       CASE WHEN p.final AND p.kept THEN dt.key END,
		       CASE WHEN NOT p.final AND p.date <= $8::date THEN pl.image_key END,
		       CASE WHEN NOT p.final AND p.date <= $8::date THEN pl.kind END,
		       pd.date IS NOT NULL,
		       m.km, m.points, m.rank,
		       g.date IS NOT NULL, (SELECT count(*) FROM members), COALESCE(g.answered, 0), g.best_name, g.best_km,
		       (SELECT date FROM best)
		  FROM page p
		  JOIN place pl ON pl.id = p.place_id
		  LEFT JOIN world w ON w.date = p.date
		  LEFT JOIN waiting wt ON wt.date = p.date
		  LEFT JOIN mine m ON m.date = p.date
		  LEFT JOIN played pd ON pd.date = p.date
		  LEFT JOIN grp g ON g.date = p.date
		  LEFT JOIN day_thumb dt ON dt.date = p.date
		 ORDER BY p.date DESC`,
		q.Newest.Time(), before, q.N, q.Final.Time(), q.UserID, player, q.ChatID, q.Seen.Time())
	if err != nil {
		return DaysPage{}, fmt.Errorf("days page: %w", err)
	}
	defer rows.Close()
	var page DaysPage
	for rows.Next() {
		var (
			x                 DayRow
			at                time.Time
			country, thumb    *string
			image, kind, name *string
			city              []byte
			km                *float64
			points, rank      *int
			inGroup           bool
			members, answered int
			groupKm           *float64
			best              *time.Time
		)
		if err := rows.Scan(&at, &x.DayNo, &x.Players, &x.Pending, &x.MedianKm, &x.BestKm, &x.CountryPct,
			&country, &city, &thumb, &image, &kind, &x.Played, &km, &points, &rank,
			&inGroup, &members, &answered, &name, &groupKm, &best); err != nil {
			return DaysPage{}, fmt.Errorf("days page: %w", err)
		}
		x.Date = round.DateIn(at, time.UTC)
		x.Country, x.Thumb, x.Image, x.Kind = str(country), str(thumb), str(image), str(kind)
		if err := decodeLangs(city, &x.City); err != nil {
			return DaysPage{}, fmt.Errorf("days page city %s: %w", x.Date, err)
		}
		if km != nil && points != nil && rank != nil {
			x.Me = &Mine{Km: *km, Points: *points, Rank: *rank}
		}
		if inGroup {
			x.Group = &ChatDay{Answered: answered, Members: members, BestName: str(name), BestKm: groupKm}
		}
		if best != nil {
			page.Best = round.DateIn(*best, time.UTC)
		}
		page.Days = append(page.Days, x)
	}
	if err := rows.Err(); err != nil {
		return DaysPage{}, fmt.Errorf("days page: %w", err)
	}
	if len(page.Days) > q.N {
		page.Days, page.More = page.Days[:q.N], true
	}
	return page, nil
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RoundNo returns the round numbered n, if there is one.
func (s *Store) RoundNo(ctx context.Context, n int) (Round, bool, error) {
	var (
		r  Round
		at time.Time
	)
	err := s.pool.QueryRow(ctx, `SELECT date, day_no, place_id FROM round WHERE day_no = $1`, n).Scan(&at, &r.DayNo, &r.PlaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Round{}, false, nil
	}
	if err != nil {
		return Round{}, false, fmt.Errorf("round #%d: %w", n, err)
	}
	r.Date = round.DateIn(at, time.UTC)
	return r, true, nil
}

// ClosedDay is what the page of a day over everywhere shows beside its
// ranking: its strip, its puzzle for the 360° view, everyone's pins and the
// days around it.
type ClosedDay struct {
	// Thumb is the strip's key; empty while the day is not kept or its strip
	// is not drawn yet.
	Thumb   string
	Image   string
	Kind    string
	Heading *float64
	// Pins are every scored answer's, without whose they are.
	Pins [][2]float64
	// Prev and Next are the numbers of the rounds before and after, 0 when
	// there is none; Next only up to newest.
	Prev, Next int
}

// ClosedDay reads the rest of d's page. The caller has made sure d is over
// in every zone.
func (s *Store) ClosedDay(ctx context.Context, d round.Date, newest round.Date) (ClosedDay, error) {
	var (
		c        ClosedDay
		thumb    *string
		heading  *float32
		lat, lon []float64
	)
	// The pins go in the order the answers came, which says nothing about
	// whose is whose next to the ranking's order by points.
	err := s.pool.QueryRow(ctx, `
		SELECT dt.key, p.image_key, p.kind, p.heading,
		       COALESCE((SELECT max(day_no) FROM round WHERE date < r.date), 0),
		       COALESCE((SELECT min(day_no) FROM round WHERE date > r.date AND date <= $2::date), 0),
		       COALESCE(pins.lat, '{}'), COALESCE(pins.lon, '{}')
		  FROM round r
		  JOIN place p ON p.id = r.place_id
		  LEFT JOIN day_thumb dt ON dt.date = r.date
		  LEFT JOIN LATERAL (
		    SELECT array_agg(ds.lat ORDER BY ds.answered_at, ds.user_id, ds.player) AS lat,
		           array_agg(ds.lon ORDER BY ds.answered_at, ds.user_id, ds.player) AS lon
		      FROM day_score ds WHERE ds.date = r.date) pins ON true
		 WHERE r.date = $1::date`, d.Time(), newest.Time()).
		Scan(&thumb, &c.Image, &c.Kind, &heading, &c.Prev, &c.Next, &lat, &lon)
	if err != nil {
		return ClosedDay{}, fmt.Errorf("closed day %s: %w", d, err)
	}
	c.Thumb = str(thumb)
	if heading != nil {
		h := float64(*heading)
		c.Heading = &h
	}
	c.Pins = make([][2]float64, 0, len(lat))
	for i := range min(len(lat), len(lon)) {
		c.Pins = append(c.Pins, [2]float64{lat[i], lon[i]})
	}
	return c, nil
}

// Undrawn is a kept day whose strip is missing, or was drawn by an older
// recipe than the one asked for.
type Undrawn struct {
	Date     round.Date
	ImageKey string
	Kind     string
	// Yaw is the place's thumb_yaw, nil when the curation picked none.
	Yaw *float64
}

// Undrawn lists the kept days without a strip of recipe, newest first.
func (s *Store) Undrawn(ctx context.Context, recipe int) ([]Undrawn, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.date, p.image_key, p.kind, p.thumb_yaw
		  FROM day_closed c
		  JOIN round r ON r.date = c.date
		  JOIN place p ON p.id = r.place_id
		  LEFT JOIN day_thumb dt ON dt.date = c.date
		 WHERE p.image_key IS NOT NULL AND (dt.date IS NULL OR dt.recipe < $1)
		 ORDER BY c.date DESC`, recipe)
	if err != nil {
		return nil, fmt.Errorf("undrawn days: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Undrawn, error) {
		var (
			u   Undrawn
			at  time.Time
			yaw *float32
		)
		err := row.Scan(&at, &u.ImageKey, &u.Kind, &yaw)
		u.Date = round.DateIn(at, time.UTC)
		if yaw != nil {
			v := float64(*yaw)
			u.Yaw = &v
		}
		return u, err
	})
}

// Thumb is a day's strip in both sizes.
type Thumb struct {
	Date   round.Date
	Key    string
	Recipe int
	Small  []byte
	Wide   []byte
}

// ErrNotKept means the day of a strip is not in day_closed (any more).
var ErrNotKept = errors.New("day not kept")

// PutThumb keeps a day's strip, replacing one of an older recipe. The day
// must be kept closed.
func (s *Store) PutThumb(ctx context.Context, t Thumb) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO day_thumb (date, key, recipe, small, wide) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (date) DO UPDATE SET key = EXCLUDED.key, recipe = EXCLUDED.recipe,
		       small = EXCLUDED.small, wide = EXCLUDED.wide, made_at = now()
		 WHERE day_thumb.recipe < EXCLUDED.recipe`,
		t.Date.Time(), t.Key, t.Recipe, t.Small, t.Wide)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrNotKept
	}
	if err != nil {
		return fmt.Errorf("put thumb %s: %w", t.Date, err)
	}
	return nil
}

// ThumbByKey returns the strip kept under key.
func (s *Store) ThumbByKey(ctx context.Context, key string) (Thumb, bool, error) {
	var (
		t  Thumb
		at time.Time
	)
	err := s.pool.QueryRow(ctx, `SELECT date, key, recipe, small, wide FROM day_thumb WHERE key = $1`, key).
		Scan(&at, &t.Key, &t.Recipe, &t.Small, &t.Wide)
	if errors.Is(err, pgx.ErrNoRows) {
		return Thumb{}, false, nil
	}
	if err != nil {
		return Thumb{}, false, fmt.Errorf("thumb: %w", err)
	}
	t.Date = round.DateIn(at, time.UTC)
	return t, true, nil
}
