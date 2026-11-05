// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/FreshLabDev/maply/internal/geo"
	"github.com/FreshLabDev/maply/internal/scoring"
)

// A place of the free game must never become a place of the day, and the
// free game must not show the spot of a future day. So a batch for one pool
// stays clear of the other pool's places: the SQL this command wrote for it
// and the candidates of its batches, where a spare frame may still be used.

// avoidList is the other pool's places.
type avoidList struct {
	ids    map[string]bool
	points []geo.Point
	km     float64
}

var insertRE = regexp.MustCompile(`INSERT INTO maply\.place \(([^)]*)\)\s*VALUES \(`)

// readAvoid reads the places in maply-places' SQL files (*.sql) and in
// harvest's candidates.json files.
func readAvoid(paths []string, km float64) (*avoidList, error) {
	a := &avoidList{ids: map[string]bool{}, km: km}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(path, ".sql") {
			var cands []candidate
			if err := json.Unmarshal(raw, &cands); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			for _, c := range cands {
				a.add(c.SourceID, c.Lat, c.Lon)
			}
			continue
		}
		sql := string(raw)
		for _, m := range insertRE.FindAllStringSubmatchIndex(sql, -1) {
			cols := strings.Split(sql[m[2]:m[3]], ",")
			vals, err := sqlTuple(sql[m[1]:])
			if err != nil || len(vals) != len(cols) {
				return nil, fmt.Errorf("%s: an INSERT this command did not write", path)
			}
			row := map[string]string{}
			for i, c := range cols {
				row[strings.TrimSpace(c)] = vals[i]
			}
			lat, err1 := strconv.ParseFloat(row["lat"], 64)
			lon, err2 := strconv.ParseFloat(row["lon"], 64)
			if row["source_id"] == "" || err1 != nil || err2 != nil {
				return nil, fmt.Errorf("%s: a place without source_id, lat or lon", path)
			}
			a.add(row["source_id"], lat, lon)
		}
	}
	return a, nil
}

func (a *avoidList) add(id string, lat, lon float64) {
	a.ids[id] = true
	a.points = append(a.points, geo.Point{Lat: lat, Lon: lon})
}

// why says why c may not go in, or "" when it may.
func (a *avoidList) why(c candidate) string {
	if a == nil {
		return ""
	}
	if a.ids[c.SourceID] {
		return "the same frame is in the other pool"
	}
	p := geo.Point{Lat: c.Lat, Lon: c.Lon}
	for _, q := range a.points {
		if d := scoring.DistanceKm(p, q); d < a.km {
			return fmt.Sprintf("%.1f km from a place of the other pool", d)
		}
	}
	return ""
}

// sqlTuple splits the values of the tuple s starts inside, up to its closing
// parenthesis, and unquotes the string literals.
func sqlTuple(s string) ([]string, error) {
	var vals []string
	var cur strings.Builder
	depth, quoted := 0, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quoted && ch == '\'' && i+1 < len(s) && s[i+1] == '\'':
			cur.WriteByte('\'')
			i++
		case quoted && ch == '\'':
			quoted = false
		case quoted:
			cur.WriteByte(ch)
		case ch == '\'':
			quoted = true
		case ch == '(':
			depth++
			cur.WriteByte(ch)
		case ch == ')' && depth == 0:
			return append(vals, strings.TrimSpace(cur.String())), nil
		case ch == ')':
			depth--
			cur.WriteByte(ch)
		case ch == ',' && depth == 0:
			vals = append(vals, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	return nil, fmt.Errorf("unterminated VALUES")
}

// oneName is the city's name for the free pool, which has no curator to
// write names in sixteen languages: the English one where OSM has it, else
// the local one Nominatim gives (see cityOf and namesOf).
func oneName(names map[string]string) map[string]string {
	if names["en"] == "" {
		return nil
	}
	return map[string]string{"en": names["en"]}
}

// sharpenRow is a place's line of sharpen.csv, the input of -sharpen.
func sharpenRow(ref string, pl *place) []string {
	kind := "photo"
	if pl.c.IsPano {
		kind = "panorama"
	}
	return []string{ref, kind, pl.c.SourceID, pl.key}
}

func writeCSV(path string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := csv.NewWriter(f).WriteAll(append([][]string{{"id", "kind", "source_id", "image_key"}}, rows...)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
