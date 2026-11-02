// SPDX-License-Identifier: Apache-2.0
package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

// clientReport is a script error in the Mini App or the site, as the page
// caught it.
type clientReport struct {
	Message string `json:"message"`
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Stack   string `json:"stack"`
	Version string `json:"version"`
	Page    string `json:"page"`
}

// clientLog writes a page's script error to the service log, where nobody
// would otherwise see it. It takes no sign-in, so it trusts nothing: every
// field is cut short, and a URL loses its fragment, which in the Mini App is
// Telegram's signed launch data.
func (s *Server) clientLog(w http.ResponseWriter, r *http.Request) {
	if !s.logLimit.allowIP(clientIP(r)) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var rep clientReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&rep); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.Log.Warn("client error",
		"message", clean(rep.Message, 300), "source", clean(rep.Source, 200),
		"line", rep.Line, "column", rep.Column, "stack", clean(rep.Stack, 2000),
		"version", clean(rep.Version, 40), "page", clean(rep.Page, 20),
		"agent", clean(r.UserAgent(), 200))
	w.WriteHeader(http.StatusNoContent)
}

var fragment = regexp.MustCompile(`#[^\s)]*`)

// clean drops URL fragments and control characters and cuts s to n runes.
func clean(s string, n int) string {
	s = fragment.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}
