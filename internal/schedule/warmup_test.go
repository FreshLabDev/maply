// SPDX-License-Identifier: Apache-2.0
package schedule

import (
	"strings"
	"testing"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
)

func TestWarmupCaption(t *testing.T) {
	km, pts, bonus := 120.0, 3000, 800
	answer := db.Answer{CountryISO: "PT", City: map[string]string{"en": "Lisbon", "ru": "Лиссабон"}}
	got := warmupCaption("ru", 41, answer, []db.Guess{{UserID: 1, FirstName: "Оксана", Km: &km, PtsKm: &pts, PtsBonus: &bonus}},
		"07:00", "20:00 (Киев)")
	for _, want := range []string{
		"<b>maply #41</b> · " + i18n.T("ru", "warmup.title"),
		"Лиссабон",
		"🥇 Оксана",
		i18n.T("ru", "warmup.after", "post", "07:00", "time", "20:00 (Киев)"),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("caption = %q, missing %q", got, want)
		}
	}
}
