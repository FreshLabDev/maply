// SPDX-License-Identifier: Apache-2.0

package card

import (
	"testing"

	"github.com/FreshLabDev/maply/internal/db"
)

func TestCountryNameIsLocalized(t *testing.T) {
	if got := countryName("PT", "ru"); got != "Португалия" {
		t.Fatalf("country = %q", got)
	}
}

func TestHeadNamesTheDayThePlaceAndTheFact(t *testing.T) {
	a := db.Answer{
		CountryISO: "PT",
		City:       map[string]string{"en": "Lisbon", "ru": "Лиссабон"},
		Fact:       map[string]string{"en": "Trams <and> hills"},
	}
	got := Head("ru", 12, a)
	want := "<b>maply #12</b> · Лиссабон, Португалия\n<i>Trams &lt;and&gt; hills</i>"
	if got != want {
		t.Fatalf("Head = %q, want %q", got, want)
	}
	if got := Place("en", db.Answer{}); got != "" {
		t.Fatalf("a place with nothing is %q", got)
	}
}
