// SPDX-License-Identifier: Apache-2.0
package main

import (
	"strings"
	"testing"
)

func TestCheckHTMLRefusesWhatTelegramRefuses(t *testing.T) {
	ok := []string{
		"<b>maply #31</b> · Где это?",
		`<blockquote expandable>Рио &amp; <i>факт</i></blockquote>`,
		`<a href="https://t.me/maplyin_bot/play?startapp=x">play</a> &lt;3 &#128512;`,
		`<span class="tg-spoiler">тайна</span> 2 > 1`,
	}
	for _, s := range ok {
		if err := checkHTML(s); err != nil {
			t.Errorf("checkHTML(%q) = %v, want nil", s, err)
		}
	}
	bad := map[string]string{
		"<div>x</div>":             "Unsupported start tag",
		"<b>x":                     "Can't find end tag",
		"<b><i>x</b></i>":          "Unmatched end tag",
		"Tom & Jerry":              "Unsupported HTML entity",
		"&nbsp;":                   "Unsupported HTML entity",
		"a < b":                    "can't parse entities",
		`<span class="x">y</span>`: "tg-spoiler",
	}
	for s, want := range bad {
		err := checkHTML(s)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("checkHTML(%q) = %v, want an error with %q", s, err, want)
		}
	}
}

func TestPlainAndRender(t *testing.T) {
	s := `<b>Аня</b> &amp; <a href="javascript:alert(1)">ссылка</a>`
	if got := plainOf(s); got != "Аня & ссылка" {
		t.Errorf("plainOf = %q", got)
	}
	got := string(renderHTML(s))
	if strings.Contains(got, "javascript") || !strings.Contains(got, `href="#"`) {
		t.Errorf("renderHTML kept a script link: %s", got)
	}
	if got := string(renderHTML("<b>x")); strings.Contains(got, "<b>") {
		t.Errorf("renderHTML of broken HTML = %s, want it escaped", got)
	}
}
