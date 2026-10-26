// SPDX-License-Identifier: Apache-2.0

package i18n

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// FormatKm is a distance as the language writes numbers: a decimal under ten
// kilometres, whole kilometres from there.
func FormatKm(lang string, km float64) string {
	if km < 10 {
		return message.NewPrinter(language.Make(lang)).Sprintf("%.1f", km)
	}
	return FormatNumber(lang, int(km+0.5))
}

// FormatNumber groups digits the way lang does.
func FormatNumber(lang string, n int) string {
	return message.NewPrinter(language.Make(lang)).Sprint(n)
}

// FormatDistance is a distance with its unit: metres under a kilometre, so a
// pin a few steps off reads "2 m" and not "0.0 km".
func FormatDistance(lang string, km float64) string {
	if km < 1 {
		return T(lang, "unit.m", "m", FormatNumber(lang, int(km*1000+0.5)))
	}
	return T(lang, "unit.km", "km", FormatKm(lang, km))
}
