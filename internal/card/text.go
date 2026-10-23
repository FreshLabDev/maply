// SPDX-License-Identifier: Apache-2.0

package card

import (
	"fmt"
	"html"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
)

// Place is "Lisbon, Portugal" in lang, or whatever of the two the place has.
func Place(lang string, a db.Answer) string {
	place := localized(a.City, lang)
	if country := countryName(a.CountryISO, lang); country != "" {
		if place != "" {
			place += ", "
		}
		place += country
	}
	return place
}

// Head is "maply #12 · Lisbon, Portugal" and the fact under it, as the HTML
// Telegram takes in a caption.
func Head(lang string, dayNo int, a db.Answer) string {
	head := fmt.Sprintf("<b>maply #%d</b>", dayNo)
	if place := Place(lang, a); place != "" {
		head += " · " + html.EscapeString(place)
	}
	if fact := localized(a.Fact, lang); fact != "" {
		head += "\n<i>" + html.EscapeString(fact) + "</i>"
	}
	return head
}

func localized(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	return m[i18n.DefaultLang]
}

func countryName(iso, lang string) string {
	region, err := language.ParseRegion(iso)
	if err != nil {
		return ""
	}
	return display.Regions(language.Make(lang)).Name(region)
}
