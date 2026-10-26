// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"testing"

	"github.com/FreshLabDev/maply/internal/db"
)

func TestPointsLineNamesWhatWasRight(t *testing.T) {
	for _, tc := range []struct {
		km, bonus, speed int
		want             string
	}{
		{3500, 1500, 40, "3,500 for the distance, +1,500 for the city, country and continent, +40 for speed"},
		{2741, 800, 0, "2,741 for the distance, +800 for the country and continent"},
		{563, 300, 0, "563 for the distance, +300 for the continent"},
		{100, 0, 0, "100 for the distance"},
		{100, 500, 0, "100 for the distance, +500 bonus"},
	} {
		if got := pointsLine("en", tc.km, tc.bonus, tc.speed); got != tc.want {
			t.Errorf("pointsLine(%d, %d, %d) = %q, want %q", tc.km, tc.bonus, tc.speed, got, tc.want)
		}
	}
}

func TestComparedFindsARecordOrTheUsualMiss(t *testing.T) {
	ptr := func(n int) *int { return &n }
	day := func(points int) db.Guess { return db.Guess{PtsKm: ptr(points), PtsBonus: ptr(0)} }
	past := []db.Scored{{Km: 2728, Points: 863}, {Km: 222, Points: 4067}, {Km: 365, Points: 3651}}
	if got := compared("en", day(5040), past); got != "Your best day yet; the old record was 4,067." {
		t.Errorf("record = %q", got)
	}
	if got := compared("en", day(1000), past); got != "You usually miss by 365 km." {
		t.Errorf("usual = %q", got)
	}
	if got := compared("en", day(1000), past[:2]); got != "" {
		t.Errorf("two days make no usual miss, got %q", got)
	}
	if got := compared("en", day(1000), nil); got != "" {
		t.Errorf("the first day has nothing to compare, got %q", got)
	}
}
