// SPDX-License-Identifier: Apache-2.0
package free

import (
	"testing"
	"time"

	"github.com/FreshLabDev/maply/internal/geo"
)

var t0 = time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)

func TestSpeedIsAShareOfTheTime(t *testing.T) {
	// Half the time left and 3,000 for the distance gives +90 on every
	// timer, the plan's table.
	for _, sec := range []int{30, 60, 90, 120} {
		limit := time.Duration(sec) * time.Second
		ends := t0.Add(limit)
		if got := Speed(sec, ends, t0.Add(limit/2), 3000, false); got != 90 {
			t.Errorf("%d s, half the time: %d, want 90", sec, got)
		}
		// The first two seconds load the picture and cost nothing.
		if got := Speed(sec, ends, t0.Add(2*time.Second), 3500, false); got != MaxSpeed {
			t.Errorf("%d s, at 2 s: %d, want %d", sec, got, MaxSpeed)
		}
		if got := Speed(sec, ends, t0, 3500, false); got != MaxSpeed {
			t.Errorf("%d s, at once: %d, want %d", sec, got, MaxSpeed)
		}
		// The grace after the end and a pin sent at zero earn nothing.
		if got := Speed(sec, ends, ends.Add(time.Second), 3500, false); got != 0 {
			t.Errorf("%d s, in the grace: %d, want 0", sec, got)
		}
		if got := Speed(sec, ends, ends, 3500, false); got != 0 {
			t.Errorf("%d s, at the end: %d, want 0", sec, got)
		}
		if got := Speed(sec, ends, t0.Add(time.Second), 3500, true); got != 0 {
			t.Errorf("%d s, auto: %d, want 0", sec, got)
		}
		// A random click far away gets nothing for being quick.
		if got := Speed(sec, ends, t0, 20, false); got != 0 {
			t.Errorf("%d s, far: %d, want 0", sec, got)
		}
	}
	if got := Speed(0, t0, t0, 3500, false); got != 0 {
		t.Errorf("no timer: %d, want 0", got)
	}
	// The old idea, 200 × (1 − seconds/60), gave any blitz answer +100;
	// here a blitz answer at the end gets next to nothing.
	if got := Speed(30, t0.Add(30*time.Second), t0.Add(29*time.Second), 3500, false); got > 10 {
		t.Errorf("blitz at 29 s: %d", got)
	}
}

func TestSettings(t *testing.T) {
	for _, c := range []struct {
		s            Settings
		solo, party  bool
		key          string
		lengthMinute float64
	}{
		{Settings{Classic, 60, 5}, true, true, "classic/60/5", 6.25},
		{Settings{Classic, 30, 5}, true, true, "classic/30/5", 3.58},
		{Settings{Classic, 120, 10}, true, true, "classic/120/10", 22.5},
		{Settings{Classic, 0, 5}, true, false, "classic/0/5", 0},
		{Settings{Classic, 45, 5}, false, false, "", 0},
		{Settings{Classic, 60, 7}, false, false, "", 0},
		// A streak has no length: it goes on while someone is right.
		{Settings{Streak, 30, 0}, true, true, "streak/30", 0},
		{Settings{Streak, 60, 0}, true, true, "streak/60", 0},
		{Settings{Streak, 0, 0}, false, false, "", 0},
		{Settings{Streak, 90, 0}, false, false, "", 0},
		{Settings{Streak, 60, 5}, false, false, "", 0},
		{Settings{Timed, 180, 0}, true, true, "timed/180", 3.08},
		{Settings{Timed, 120, 0}, false, false, "", 0},
		{Settings{Timed, 180, 5}, false, false, "", 0},
		// A day from the archive is taken, not chosen, alone; together the
		// host picks a timer, never none.
		{Settings{Archive, 0, 1}, false, false, "", 0},
		{Settings{Archive, 60, 1}, false, true, "archive/60/1", 1.25},
		{Settings{Archive, 120, 1}, false, true, "archive/120/1", 2.25},
		{Settings{Archive, 30, 1}, false, false, "", 0},
		{Settings{Archive, 60, 3}, false, false, "", 0},
		{Settings{"region", 60, 5}, false, false, "", 0},
	} {
		if got := c.s.ValidSolo(); got != c.solo {
			t.Errorf("%+v solo = %v", c.s, got)
		}
		if got := c.s.ValidParty(); got != c.party {
			t.Errorf("%+v party = %v", c.s, got)
		}
		if c.key != "" && c.s.Key() != c.key {
			t.Errorf("%+v key = %q", c.s, c.s.Key())
		}
		if c.party {
			if got := Length(c.s).Minutes(); got < c.lengthMinute-0.01 || got > c.lengthMinute+0.01 {
				t.Errorf("%+v length = %.2f min, want %.2f", c.s, got, c.lengthMinute)
			}
		}
	}
	if RevealFor(30) != 8*time.Second || RevealFor(60) != 10*time.Second || RevealFor(120) != 10*time.Second {
		t.Error("reveal: 8 s for a blitz, 10 s otherwise")
	}
}

func pin(lat, lon float64) *geo.Point { return &geo.Point{Lat: lat, Lon: lon} }

func TestTiesGoToKilometresThenTime(t *testing.T) {
	set := Settings{Classic, 60, 2}
	members := []Member{{M: 1, FromRound: 1}, {M: 2, FromRound: 1}, {M: 3, FromRound: 1}, {M: 4, FromRound: 2}}
	rounds := []Round{{N: 1}, {N: 2}}
	guesses := []Guess{
		// 1 and 2 tie on points; 2 is closer in all.
		{N: 1, M: 1, Pin: pin(0, 0), PtsKm: 2000, Km: 600, TimeMs: 10_000},
		{N: 2, M: 1, Pin: pin(0, 0), PtsKm: 2000, Km: 600, TimeMs: 10_000},
		{N: 1, M: 2, Pin: pin(0, 0), PtsKm: 2500, Km: 500, TimeMs: 30_000},
		{N: 2, M: 2, Pin: pin(0, 0), PtsKm: 1500, Km: 600, TimeMs: 30_000},
		// 3 answered one place only, with the same points as 4 got from the
		// one place 4 was there for; silence counts the full time and the
		// farthest pin.
		{N: 1, M: 3, Pin: pin(0, 0), PtsKm: 1000, Km: 900, TimeMs: 5_000},
		{N: 2, M: 4, Pin: pin(0, 0), PtsKm: 1000, Km: 900, TimeMs: 5_000},
	}
	lines := Lines(set, members, rounds, guesses)
	order := []int{}
	for _, l := range lines {
		order = append(order, l.M)
	}
	if order[0] != 2 || order[1] != 1 {
		t.Fatalf("order = %v, want 2 before 1", order)
	}
	byM := map[int]Line{}
	for _, l := range lines {
		byM[l.M] = l
	}
	if byM[2].Rank != 1 || byM[1].Rank != 2 {
		t.Errorf("ranks = %d, %d", byM[2].Rank, byM[1].Rank)
	}
	// 3 and 4: same points, same kilometres (one pin and one miss each),
	// same time: one place shared.
	if byM[3].Rank != byM[4].Rank || byM[3].Rank != 3 {
		t.Errorf("3 and 4 share a place: %d, %d", byM[3].Rank, byM[4].Rank)
	}
	if byM[3].TimeMs != 5_000+60_000 {
		t.Errorf("a missed place counts the full time: %d", byM[3].TimeMs)
	}
	if byM[4].Played != 1 || byM[3].Played != 2 {
		t.Errorf("played: late %d, on time %d", byM[4].Played, byM[3].Played)
	}
	if byM[2].Beaten != 3 || byM[3].Beaten != 0 {
		t.Errorf("beaten: %d, %d", byM[2].Beaten, byM[3].Beaten)
	}
	// Time breaks a tie of points and kilometres.
	tie := Lines(set, members[:2], rounds[:1], []Guess{
		{N: 1, M: 1, Pin: pin(0, 0), PtsKm: 2000, Km: 600, TimeMs: 9_000},
		{N: 1, M: 2, Pin: pin(0, 0), PtsKm: 2000, Km: 600, TimeMs: 8_000},
	})
	if tie[0].M != 2 || tie[0].Rank != 1 || tie[1].Rank != 2 {
		t.Errorf("time tie: %+v", tie)
	}
}

func TestStreakAndBase(t *testing.T) {
	set := Settings{Streak, 30, 0}
	lines := Lines(set, []Member{{M: 1, FromRound: 1}}, []Round{{N: 1}, {N: 2}, {N: 3}}, []Guess{
		{N: 1, M: 1, Pin: pin(0, 0), PtsKm: 3000, PtsBonus: 800, PtsSpeed: 100, CountryOK: true, Km: 50},
		{N: 2, M: 1, Pin: pin(0, 0), PtsKm: 3000, PtsBonus: 800, PtsSpeed: 150, CountryOK: true, Km: 40},
		{N: 3, M: 1, Pin: pin(0, 0), PtsKm: 1000, PtsBonus: 300, CountryOK: false, Km: 2000},
	})
	l := lines[0]
	if l.Streak != 2 || l.Countries != 2 || l.Answered != 3 {
		t.Errorf("streak line: %+v", l)
	}
	if l.Base != 3800+3800+1300 || l.Points != l.Base+250 {
		t.Errorf("base %d points %d: the average leaves the speed out", l.Base, l.Points)
	}
	if len(l.KmRounds) != 3 || l.KmRounds[2] != 2000 {
		t.Errorf("km rounds: %v", l.KmRounds)
	}
}

func TestPickPrefersUnseenAndOneCountry(t *testing.T) {
	old, older := t0.Add(-time.Hour), t0.Add(-48*time.Hour)
	first := func(int) int { return 0 }
	cands := []Candidate{
		{PlaceID: 1, Country: "FR"},
		{PlaceID: 2, Country: "DE", SeenAt: &old},
		{PlaceID: 3, Country: "IT", SeenAt: &older},
		{PlaceID: 4, Country: "ES"},
	}
	used := map[string]bool{"FR": true}
	c, repeat, ok := Pick(cands, used, first)
	if !ok || c.PlaceID != 4 || repeat {
		t.Fatalf("unseen in a new country first: %+v %v", c, repeat)
	}
	used["ES"] = true
	// Unseen only in a used country: still before any repeat.
	c, repeat, _ = Pick(cands, used, first)
	if c.PlaceID != 1 && c.PlaceID != 4 || repeat {
		t.Fatalf("an unseen place in a used country before a repeat: %+v %v", c, repeat)
	}
	// Everything seen: the oldest in a new country, then the oldest at all.
	seen := []Candidate{cands[1], cands[2], {PlaceID: 5, Country: "FR", SeenAt: &older}}
	c, repeat, _ = Pick(seen, used, first)
	if c.PlaceID != 3 && c.PlaceID != 5 || !repeat {
		t.Fatalf("oldest repeat: %+v %v", c, repeat)
	}
	c, _, _ = Pick(seen, map[string]bool{"FR": true}, first)
	if c.PlaceID != 3 {
		t.Fatalf("oldest repeat in a new country: %+v", c)
	}
	used["DE"], used["IT"] = true, true
	c, repeat, _ = Pick(seen, used, first)
	if c.PlaceID != 3 && c.PlaceID != 5 || !repeat {
		t.Fatalf("oldest repeat when every country is used: %+v %v", c, repeat)
	}
	if _, _, ok := Pick(nil, used, first); ok {
		t.Fatal("nothing to pick")
	}
	// A closed day's place, public with its answer, waits for the free pool
	// to run out of new places, even in a new country; it still comes before
	// a repeat.
	days := []Candidate{{PlaceID: 6, Country: "JP", Day: true}, {PlaceID: 7, Country: "FR"}, {PlaceID: 8, Country: "KE", SeenAt: &older}}
	if c, repeat, _ := Pick(days, map[string]bool{"FR": true}, first); c.PlaceID != 7 || repeat {
		t.Fatalf("the pool before a closed day: %+v %v", c, repeat)
	}
	if c, repeat, _ := Pick(days[0:1:1], nil, first); c.PlaceID != 6 || repeat {
		t.Fatalf("a closed day when the pool is used up: %+v %v", c, repeat)
	}
	if c, repeat, _ := Pick([]Candidate{days[0], days[2]}, nil, first); c.PlaceID != 6 || repeat {
		t.Fatalf("a closed day before a repeat: %+v %v", c, repeat)
	}
}

func TestAwayAfterTwoMissedPlaces(t *testing.T) {
	closed := t0
	rounds := []Round{{N: 1, ClosedAt: &closed}, {N: 2, ClosedAt: &closed}, {N: 3, ClosedAt: &closed}, {N: 4}}
	answered := map[[2]int]bool{{1, 1}: true, {2, 2}: true, {3, 2}: true}
	ans := func(n, m int) bool { return answered[[2]int{n, m}] }
	if !away(Member{M: 1, FromRound: 1}, rounds, ans) {
		t.Error("missed 2 and 3: away")
	}
	if away(Member{M: 2, FromRound: 1}, rounds, ans) {
		t.Error("answered the last two: not away")
	}
	if away(Member{M: 3, FromRound: 3}, rounds, ans) {
		t.Error("joined for 3: one miss only")
	}
}

func TestCodes(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		c := newCode()
		if !ValidCode(c) || seen[c] {
			t.Fatalf("code %q", c)
		}
		seen[c] = true
	}
	for _, bad := range []string{"", "ABCDEFG", "ABCDEFGHI", "ABCDEFGI", "abcdefgh", "ABCD-FGH"} {
		if ValidCode(bad) {
			t.Errorf("%q passed", bad)
		}
	}
}

func TestImagePathIsSigned(t *testing.T) {
	s := &Service{ImageKey: []byte("k")}
	s.init()
	p := s.imagePath(42, 3)
	other := (&Service{ImageKey: []byte("other")}).imagePath(42, 3)
	if p == other || p == s.imagePath(42, 4) || p == s.imagePath(43, 3) {
		t.Fatal("paths must differ by key, game and place")
	}
	if len(p) > 64 {
		t.Errorf("path %q", p)
	}
}

func TestStreakPartyLinesStopAtTheMiss(t *testing.T) {
	set := Settings{Streak, 30, 0}
	members := []Member{{M: 1, FromRound: 1}, {M: 2, FromRound: 1}, {M: 3, FromRound: 2}}
	rounds := []Round{{N: 1}, {N: 2}, {N: 3}}
	right := func(n, m int, pts int) Guess {
		return Guess{N: n, M: m, Pin: pin(0, 0), PtsKm: pts, CountryOK: true, Km: 10}
	}
	lines := Lines(set, members, rounds, []Guess{
		right(1, 1, 3000), right(2, 1, 3000), right(3, 1, 3000),
		right(1, 2, 3000), {N: 2, M: 2, Pin: pin(0, 0), PtsKm: 100, Km: 900},
		// A pin after the miss is not part of the line.
		right(3, 2, 5000),
		right(2, 3, 3000),
	})
	byM := map[int]Line{}
	for _, l := range lines {
		byM[l.M] = l
	}
	if byM[1].Streak != 3 || byM[2].Streak != 1 || byM[3].Streak != 1 {
		t.Errorf("streaks: %d %d %d", byM[1].Streak, byM[2].Streak, byM[3].Streak)
	}
	if byM[2].Played != 2 || byM[2].Answered != 2 {
		t.Errorf("a line stops at its miss: %+v", byM[2])
	}
	// 2 and 3 tie on the streak; 2's points decide.
	if lines[0].M != 1 || lines[1].M != 2 || lines[2].M != 3 {
		t.Errorf("order: %d %d %d", lines[0].M, lines[1].M, lines[2].M)
	}
	// 3 came in at place 2, where it answered; place 3 went by unanswered.
	if byM[3].Played != 2 || byM[3].Answered != 1 {
		t.Errorf("a late player is counted from their place: %+v", byM[3])
	}
}

func TestTimedPartyRanksCountriesFirst(t *testing.T) {
	set := Settings{Timed, 180, 0}
	members := []Member{{M: 1, FromRound: 1}, {M: 2, FromRound: 1}}
	rounds := []Round{{N: 1}, {N: 2}, {N: 3}}
	lines := Lines(set, members, rounds, []Guess{
		// 1 is quick and far; 2 found more countries with fewer points.
		{N: 1, M: 1, Pin: pin(0, 0), PtsKm: 4000, CountryOK: true, Km: 5},
		{N: 2, M: 1, Pin: pin(0, 0), PtsKm: 4000, Km: 700},
		{N: 3, M: 1, Pin: pin(0, 0), PtsKm: 4000, Km: 700},
		{N: 1, M: 2, Pin: pin(0, 0), PtsKm: 2000, CountryOK: true, Km: 300},
		{N: 2, M: 2, Pin: pin(0, 0), PtsKm: 2000, CountryOK: true, Km: 300},
	})
	if lines[0].M != 2 || lines[0].Countries != 2 || lines[1].Countries != 1 {
		t.Fatalf("countries decide: %+v", lines)
	}
	// Place 3 was never reached by 2: it is not played, not a miss.
	if lines[0].Played != 2 || lines[1].Played != 3 {
		t.Errorf("played: %d %d", lines[0].Played, lines[1].Played)
	}
	if lines[0].Km != 600 {
		t.Errorf("a place not reached adds no kilometres: %v", lines[0].Km)
	}
}
