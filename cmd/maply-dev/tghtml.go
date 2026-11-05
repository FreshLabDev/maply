// SPDX-License-Identifier: Apache-2.0
package main

// Telegram's HTML: the fake server refuses what Telegram refuses, so a
// message the bot could not send in Telegram fails on the stand too, and the
// /dev/tg page draws what it accepted.

import (
	"fmt"
	"html"
	"html/template"
	"strings"
)

var tgTags = map[string]string{
	"b": "b", "strong": "b", "i": "i", "em": "i", "u": "u", "ins": "u", "s": "s", "strike": "s", "del": "s",
	"code": "code", "pre": "pre", "blockquote": "blockquote", "tg-spoiler": "spoiler", "span": "spoiler",
	"a": "a", "tg-emoji": "emoji",
}

type tgToken struct {
	text  string // text as written, entities included
	tag   string // the tag's name, empty for text
	close bool
	href  string
}

func tokenizeHTML(s string) ([]tgToken, error) {
	var out []tgToken
	var stack []string
	for i := 0; i < len(s); {
		switch s[i] {
		case '<':
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				return nil, badRequest(fmt.Sprintf("can't parse entities: Unclosed start tag at byte offset %d", i))
			}
			raw := s[i+1 : i+end]
			tok := tgToken{}
			if strings.HasPrefix(raw, "/") {
				tok.close = true
				raw = raw[1:]
			}
			name, attrs, _ := strings.Cut(strings.TrimSpace(raw), " ")
			name = strings.ToLower(name)
			if _, ok := tgTags[name]; !ok {
				return nil, badRequest(fmt.Sprintf("can't parse entities: Unsupported start tag \"%s\" at byte offset %d", name, i))
			}
			tok.tag = name
			if !tok.close {
				switch name {
				case "a":
					tok.href = attr(attrs, "href")
				case "span":
					if attr(attrs, "class") != "tg-spoiler" {
						return nil, badRequest(fmt.Sprintf("can't parse entities: Tag \"span\" must have class \"tg-spoiler\" at byte offset %d", i))
					}
				}
				stack = append(stack, name)
			} else {
				if len(stack) == 0 || stack[len(stack)-1] != name {
					return nil, badRequest(fmt.Sprintf("can't parse entities: Unmatched end tag at byte offset %d, expected \"</%s>\", found \"</%s>\"", i, last(stack), name))
				}
				stack = stack[:len(stack)-1]
			}
			out = append(out, tok)
			i += end + 1
		case '&':
			semi := strings.IndexByte(s[i:], ';')
			ent := ""
			if semi > 0 {
				ent = s[i+1 : i+semi]
			}
			ok := ent == "lt" || ent == "gt" || ent == "amp" || ent == "quot"
			if !ok && strings.HasPrefix(ent, "#") && len(ent) > 1 {
				ok = strings.Trim(ent[1:], "0123456789xXabcdefABCDEF") == ""
			}
			if !ok {
				return nil, badRequest(fmt.Sprintf("can't parse entities: Unsupported HTML entity at byte offset %d", i))
			}
			out = append(out, tgToken{text: s[i : i+semi+1]})
			i += semi + 1
		default:
			j := strings.IndexAny(s[i:], "<&")
			if j < 0 {
				j = len(s) - i
			}
			out = append(out, tgToken{text: s[i : i+j]})
			i += j
		}
	}
	if len(stack) > 0 {
		return nil, badRequest(fmt.Sprintf("can't parse entities: Can't find end tag corresponding to start tag \"%s\"", last(stack)))
	}
	return out, nil
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func attr(attrs, name string) string {
	for _, q := range []string{`"`, `'`} {
		k := name + "=" + q
		if i := strings.Index(attrs, k); i >= 0 {
			rest := attrs[i+len(k):]
			if j := strings.Index(rest, q); j >= 0 {
				return html.UnescapeString(rest[:j])
			}
		}
	}
	return ""
}

// checkHTML refuses what Telegram's parse_mode HTML refuses.
func checkHTML(s string) error {
	_, err := tokenizeHTML(s)
	return err
}

// plainOf is the text Telegram shows, without tags and entities.
func plainOf(s string) string {
	toks, err := tokenizeHTML(s)
	if err != nil {
		return s
	}
	var b strings.Builder
	for _, t := range toks {
		if t.tag == "" {
			b.WriteString(html.UnescapeString(t.text))
		}
	}
	return b.String()
}

func escapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// renderHTML is a message for the page: Telegram's tags as plain HTML, links
// only to http, https and tg.
func renderHTML(s string) template.HTML {
	toks, err := tokenizeHTML(s)
	if err != nil {
		return template.HTML(escapeHTML(s))
	}
	var b strings.Builder
	for _, t := range toks {
		if t.tag == "" {
			// Entities are Telegram's four and numbers: safe as they are.
			b.WriteString(strings.ReplaceAll(t.text, ">", "&gt;"))
			continue
		}
		kind := tgTags[t.tag]
		switch kind {
		case "emoji":
			continue
		case "spoiler":
			if t.close {
				b.WriteString("</span>")
			} else {
				b.WriteString(`<span class="spoiler">`)
			}
		case "a":
			if t.close {
				b.WriteString("</a>")
				continue
			}
			href := t.href
			low := strings.ToLower(href)
			if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") && !strings.HasPrefix(low, "tg://") {
				href = "#"
			}
			b.WriteString(`<a href="` + html.EscapeString(href) + `" target="_blank" rel="noopener">`)
		default:
			if t.close {
				b.WriteString("</" + kind + ">")
			} else {
				b.WriteString("<" + kind + ">")
			}
		}
	}
	return template.HTML(b.String())
}
