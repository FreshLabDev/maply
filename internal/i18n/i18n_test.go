// SPDX-License-Identifier: Apache-2.0
package i18n

import (
	"strings"
	"testing"
)

// A missing string shows as "[key]" to whoever is reading the panel, so every
// key must exist in every language maply offers to pick (AGENTS.md: a key
// missing in one language is a bug).
func TestEveryKeyCoversEveryLanguage(t *testing.T) {
	for _, key := range Keys() {
		for _, code := range Codes() {
			if strings.TrimSpace(translations[key][code]) == "" {
				t.Errorf("%s is missing %s", key, code)
			}
		}
	}
}

// A placeholder that survives into one language but not another renders a
// literal "{url}" to that user.
func TestPlaceholdersMatchAcrossLanguages(t *testing.T) {
	for _, key := range Keys() {
		want := placeholders(translations[key][DefaultLang])
		for _, code := range Codes() {
			if got := placeholders(translations[key][code]); got != want {
				t.Errorf("%s [%s] has placeholders %q, English has %q", key, code, got, want)
			}
		}
	}
}

func placeholders(text string) string {
	var found []string
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(text[open:], '}')
		if close < 0 {
			break
		}
		found = append(found, text[open:open+close+1])
		text = text[open+close+1:]
	}
	// Order does not matter across languages, only the set does.
	for i := 0; i < len(found); i++ {
		for j := i + 1; j < len(found); j++ {
			if found[j] < found[i] {
				found[i], found[j] = found[j], found[i]
			}
		}
	}
	return strings.Join(found, ",")
}

func TestResolveFallsBackToEnglish(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ru", "ru"},
		{"ru-RU", "ru"},
		{"uk", "uk"},
		{"be", "be"},
		{"zh_CN", "zh"},
		{"EN-gb", "en"},
		{"", "en"},
		{"kl", "en"},
		{"klingon", "en"},
	} {
		if got := Resolve(tc.in); got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Ukrainian and Belarusian are their own languages, not Russian with another
// flag.
func TestUkrainianAndBelarusianAreNotRussian(t *testing.T) {
	for _, code := range []string{"uk", "be"} {
		if Resolve(code) != code {
			t.Fatalf("%s resolved to %q", code, Resolve(code))
		}
		if T(code, "home.body") == T("ru", "home.body") {
			t.Fatalf("%s reuses the Russian text", code)
		}
	}
}

// The Mini App splits these into one line per item: an empty line would be a
// blank toast.
func TestListsHaveNoEmptyItems(t *testing.T) {
	for _, key := range []string{"app.loading", "app.on_water", "app.on_land", "app.moved", "app.logo"} {
		for _, code := range Codes() {
			for i, line := range strings.Split(translations[key][code], "\n") {
				if strings.TrimSpace(line) == "" {
					t.Errorf("%s [%s] line %d is empty", key, code, i+1)
				}
			}
		}
	}
}

func TestWithPrefix(t *testing.T) {
	app := WithPrefix("uk", "app.")
	if app["app.where"] != "Де це?" {
		t.Fatalf("app.where = %q", app["app.where"])
	}
	for key := range app {
		if !strings.HasPrefix(key, "app.") {
			t.Fatalf("WithPrefix returned %s", key)
		}
	}
	if _, ok := app["btn.play"]; ok {
		t.Fatal("a bot key leaked into the app's strings")
	}
}

func TestInterpolation(t *testing.T) {
	got := T("en", "stats.days", "n", "12")
	if got != "Days played: 12" {
		t.Fatalf("got %q", got)
	}
	if got := T("en", "no.such.key"); got != "[no.such.key]" {
		t.Fatalf("unknown key = %q", got)
	}
}

// Every option in the picker must be renderable, and every renderable language
// must be offered.
func TestPickerMatchesSupportedSet(t *testing.T) {
	for _, o := range LANGUAGE_OPTIONS {
		if !IsSupported(o.Code) {
			t.Errorf("%s is offered but not supported", o.Code)
		}
		if o.Label == "" || o.Label == o.Code {
			t.Errorf("%s has no native label", o.Code)
		}
	}
	if len(LANGUAGE_OPTIONS) != len(Codes()) {
		t.Fatal("the picker and the supported set disagree")
	}
}
