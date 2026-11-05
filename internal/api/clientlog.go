// SPDX-License-Identifier: Apache-2.0
package api

import (
	"encoding/json"
	"mime"
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

// ownJSON reports whether a post without a sign-in came from maply's own
// pages: JSON only, since a page elsewhere can post a form or plain text
// without asking but not JSON, which needs a CORS preflight maply never
// grants; and not marked cross-site by a browser that says.
func ownJSON(r *http.Request) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		return false
	}
	return r.Header.Get("Sec-Fetch-Site") != "cross-site"
}

// clientLog writes a page's script error to the service log, where nobody
// would otherwise see it. It takes no sign-in, so it trusts nothing: every
// field is cut short, and a URL loses its fragment, which in the Mini App is
// Telegram's signed launch data.
func (s *Server) clientLog(w http.ResponseWriter, r *http.Request) {
	if !ownJSON(r) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	// Per address, and a ceiling for all of them: a phone's IPv6 addresses
	// change too easily to be the only key.
	if !s.logLimit.allowIP(clientIP(r)) || !s.logAll.allowKey("all") {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var rep clientReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&rep); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(rep.Message) == "" {
		w.WriteHeader(http.StatusNoContent)
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

// query is a URL's query string. The Mini App's page address carries
// tgWebAppStartParam, the signed payload naming the group.
var query = regexp.MustCompile(`\?[\w%.\-]+=[^\s)]*`)

// coordinate is a number with a fraction of three digits or more, the shape
// of a latitude or longitude. MapLibre puts a point in some of its messages,
// and a pin before the reveal is not for the log.
var coordinate = regexp.MustCompile(`-?\d{1,3}\.\d{3,}`)

// clean drops URL fragments and queries and control characters and cuts s
// to n runes.
func clean(s string, n int) string {
	s = fragment.ReplaceAllString(s, "")
	s = query.ReplaceAllString(s, "")
	s = coordinate.ReplaceAllString(s, "…")
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
