// SPDX-License-Identifier: Apache-2.0

package card

import (
	"fmt"
	"html"
	"strings"

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

// Quoted is the share's head: "maply #12" with the place and its fact set
// apart in a quote, so a forwarded card reads as the day's answer and then
// the sender's line.
func Quoted(lang string, dayNo int, a db.Answer) string {
	head := fmt.Sprintf("<b>maply #%d</b>", dayNo)
	var quote []string
	if place := Place(lang, a); place != "" {
		quote = append(quote, html.EscapeString(place))
	}
	if fact := localized(a.Fact, lang); fact != "" {
		quote = append(quote, "<i>"+html.EscapeString(fact)+"</i>")
	}
	if len(quote) > 0 {
		head += "\n<blockquote>" + strings.Join(quote, "\n") + "</blockquote>"
	}
	return head
}

// City is the place's city in lang, or in the default language.
func City(lang string, a db.Answer) string { return localized(a.City, lang) }

// Country is the place's country in lang.
func Country(lang string, a db.Answer) string { return countryName(a.CountryISO, lang) }

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
