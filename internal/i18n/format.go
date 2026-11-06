// SPDX-License-Identifier: Apache-2.0

package i18n

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// numbers prints the way lang groups digits. Arabic keeps Western digits:
// places, times and day numbers in its texts are written with them, and
// "3 من ١٬٢٢٩" in one line reads as two scripts.
func numbers(lang string) *message.Printer {
	if lang == "ar" {
		return message.NewPrinter(language.Make("ar-u-nu-latn"))
	}
	return message.NewPrinter(language.Make(lang))
}

// FormatKm is a distance as the language writes numbers: a decimal under ten
// kilometres, whole kilometres from there.
func FormatKm(lang string, km float64) string {
	if km < 10 {
		return numbers(lang).Sprintf("%.1f", km)
	}
	return FormatNumber(lang, int(km+0.5))
}

// FormatNumber groups digits the way lang does.
func FormatNumber(lang string, n int) string {
	return numbers(lang).Sprint(n)
}

// FormatDistance is a distance with its unit: metres under a kilometre, so a
// pin a few steps off reads "2 m" and not "0.0 km".
func FormatDistance(lang string, km float64) string {
	if km < 1 {
		return T(lang, "unit.m", "m", FormatNumber(lang, int(km*1000+0.5)))
	}
	return T(lang, "unit.km", "km", FormatKm(lang, km))
}

// At is a time with the zone it is in, "20:00 (Kyiv)", so a reader in another
// zone does not take it for their own clock. zone is one of the zones maply
// offers.
func At(lang, clock, zone string) string {
	name := T(lang, "zone."+zone)
	if lang == "ja" || lang == "zh" {
		return clock + "（" + name + "）"
	}
	return clock + " (" + name + ")"
}
