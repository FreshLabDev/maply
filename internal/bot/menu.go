// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/FreshLabDev/tg"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
	"github.com/FreshLabDev/maply/internal/telegram"
)

// The menu repeats voicy's and quoto's: callbacks "m:<owner>:<action>", the
// same glyphs for a choice, one panel shape, Close only in a group.
const menuPrefix = "m"

const (
	toggleOn  = "◉ "
	toggleOff = "◎ "
)

const (
	productName = "maply"
	repoURL     = "https://github.com/FreshLabDev/maply"
	repoName    = "FreshLabDev/maply"
	licenseName = "Apache-2.0"
	adminURL    = "https://t.me/amtiyo"
	adminName   = "@amtiyo"
)

// Actions a panel button carries. The ones under a group's Settings are
// admin-only, and the check runs on every press: rights change.
const (
	actHome      = "home"
	actHelp      = "help"
	actAbout     = "about"
	actStats     = "stats"
	actLang      = "lang"
	actLangAuto  = "langauto"
	actSettings  = "set"
	actZone      = "zone"
	actGroupLang = "glang"
	actGroupAuto = "glangauto"
	actClose     = "close"
)

func adminOnly(action string) bool {
	head, _, _ := strings.Cut(action, ":")
	switch head {
	case actSettings, actZone, actGroupLang, actGroupAuto:
		return true
	}
	return false
}

func cb(owner int64, action string) string {
	return menuPrefix + ":" + strconv.FormatInt(owner, 10) + ":" + action
}

func parseMenuCB(data string) (owner int64, action string, ok bool) {
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 || parts[0] != menuPrefix || parts[2] == "" {
		return 0, "", false
	}
	owner, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || owner == 0 {
		return 0, "", false
	}
	return owner, parts[2], true
}

// scope is where the panel sits. In a direct chat the conversation is the
// panel. In a group it is one message in other people's feed, so it can be
// closed, and what it changes belongs to the whole group.
type scope int

const (
	scopePrivate scope = iota
	scopeGroup
)

func scopeOf(chat tg.Chat) scope {
	if chat.Type == "private" {
		return scopePrivate
	}
	return scopeGroup
}

// view is who a panel is drawn for: the language it is written in, the person
// whose buttons these are, and where it sits.
type view struct {
	lang  string
	owner int64
	sc    scope
}

func (v view) t(key string, pairs ...string) string { return i18n.T(v.lang, key, pairs...) }

func (v view) button(key, action string) telegram.Button {
	return telegram.Button{Text: v.t(key), CallbackData: cb(v.owner, action)}
}

// panel is the one shape every screen has: a bold title, an italic hint under
// it, then the substance in a quote. note sits on the title line; only About
// has one, the version.
func panel(title, note, hint, body string) string {
	s := "<b>" + title + "</b>"
	if note != "" {
		s += " · <i>" + note + "</i>"
	}
	if hint != "" {
		s += "\n<i>" + hint + "</i>"
	}
	if body != "" {
		s += "\n\n" + body
	}
	return s
}

func quote(inner string) string {
	if strings.TrimSpace(inner) == "" {
		return ""
	}
	return "<blockquote>" + inner + "</blockquote>"
}

func link(url, label string) string {
	return "<a href=\"" + url + "\">" + html.EscapeString(label) + "</a>"
}

func revealTime() string { return fmt.Sprintf("%02d:00", round.RevealHour) }

func toggleMark(on bool) string {
	if on {
		return toggleOn
	}
	return toggleOff
}

// selectable is one option of a set. The current one is state, not an action,
// so it is the one painted Success.
func selectable(text, data string, selected bool) telegram.Button {
	b := telegram.Button{Text: toggleMark(selected) + text, CallbackData: data}
	if selected {
		b.Style = tg.StyleSuccess
	}
	return b
}

func pairs(buttons []telegram.Button) telegram.Markup {
	rows := make(telegram.Markup, 0, (len(buttons)+1)/2)
	for i := 0; i < len(buttons); i += 2 {
		rows = append(rows, buttons[i:min(i+2, len(buttons))])
	}
	return rows
}

// navRow is Back, and Close only in a group: closing a direct chat's panel
// would leave the person looking at their own history.
func (v view) navRow(back string) []telegram.Button {
	row := []telegram.Button{v.button("btn.back", back)}
	if v.sc == scopeGroup {
		row = append(row, v.closeButton())
	}
	return row
}

func (v view) closeButton() telegram.Button {
	b := v.button("btn.close", actClose)
	b.Style = tg.StyleDanger
	return b
}

// home is two screens. The direct chat gets every tab: there the panel is the
// whole bot. A group gets what a group acts on, and Settings only for an
// admin, who is the only one who can change them.
func (v view) home(appURL string, admin bool) (string, telegram.Markup) {
	if v.sc == scopeGroup {
		text := panel(productName, "", v.t("home.hint"), quote(v.t("home.group", "time", revealTime())))
		var rows telegram.Markup
		if admin {
			rows = append(rows, []telegram.Button{v.button("btn.settings", actSettings)})
		}
		rows = append(rows,
			[]telegram.Button{v.button("btn.help", actHelp), v.button("btn.about", actAbout)},
			// Home is the top, so it has nothing to go back to; it still closes.
			[]telegram.Button{v.closeButton()},
		)
		return text, rows
	}
	text := panel(productName, "", v.t("home.hint"), quote(v.t("home.body", "time", revealTime())))
	var rows telegram.Markup
	if appURL != "" {
		// Playing is what the panel is opened for, so it is the one
		// highlighted button. Telegram opens a web_app button only in a
		// direct chat, which is why a group's home has none.
		rows = append(rows, []telegram.Button{{Text: v.t("btn.play"), WebApp: appURL, Style: tg.StylePrimary}})
	}
	rows = append(rows,
		[]telegram.Button{v.button("btn.language", actLang), v.button("btn.stats", actStats)},
		[]telegram.Button{v.button("btn.settings", actSettings), v.button("btn.help", actHelp)},
		[]telegram.Button{v.button("btn.about", actAbout)},
	)
	return text, rows
}

func (v view) help() (string, telegram.Markup) {
	return panel(v.t("help.title"), "", v.t("help.hint"), quote(v.t("help.body"))),
		telegram.Markup{v.navRow(actHome)}
}

func (v view) about(version string) (string, telegram.Markup) {
	rows := []string{
		v.t("about.photos") + " · " + link("https://www.mapillary.com", "Mapillary") + " · CC BY-SA 4.0",
		v.t("about.map") + " · " + link("https://openfreemap.org", "OpenFreeMap") + " · © " + link("https://www.openstreetmap.org/copyright", "OpenStreetMap"),
		v.t("about.borders") + " · " + link("https://www.naturalearthdata.com", "Natural Earth"),
		v.t("about.source") + " · " + link(repoURL, repoName) + " · " + licenseName,
		v.t("about.admin") + " · " + link(adminURL, adminName),
	}
	return panel(productName, html.EscapeString(version), v.t("about.tagline"), quote(strings.Join(rows, "\n"))),
		telegram.Markup{v.navRow(actHome)}
}

// languageGrid is the family's sixteen languages, two per row, with the
// current one marked.
func (v view) languageGrid(current, action string) telegram.Markup {
	buttons := make([]telegram.Button, len(i18n.LANGUAGE_OPTIONS))
	for i, opt := range i18n.LANGUAGE_OPTIONS {
		buttons[i] = selectable(opt.Label, cb(v.owner, action+":"+opt.Code), opt.Code == current)
	}
	return pairs(buttons)
}

// language is the person's interface language, shared with the sibling bots
// through core. Follow Telegram is the way back from a manual pick, which
// otherwise outranks every automatic source for ever.
func (v view) language() (string, telegram.Markup) {
	rows := v.languageGrid(v.lang, actLang)
	rows = append(rows, []telegram.Button{v.button("btn.follow_telegram", actLangAuto)}, v.navRow(actHome))
	return panel(v.t("lang.title"), "", v.t("lang.hint"), ""), rows
}

// groupLanguage is the group's own language, in which its posts and results
// come. It sits under Settings, so Back returns there.
func (v view) groupLanguage(current string) (string, telegram.Markup) {
	rows := v.languageGrid(current, actGroupLang)
	rows = append(rows, []telegram.Button{v.button("btn.lang_auto", actGroupAuto)}, v.navRow(actSettings))
	return panel(v.t("lang.title"), "", v.t("lang.group_hint"), ""), rows
}

// settings shows the zone the result follows. A group's also has its
// language; a person's language is on the home screen.
func (v view) settings(zone, groupLang string) (string, telegram.Markup) {
	hint := v.t("settings.hint")
	if v.sc == scopeGroup {
		hint = v.t("settings.group_hint")
	}
	zoneName := v.t("zone." + zone)
	text := panel(v.t("settings.title"), "", hint, quote(v.t("settings.reveal", "time", revealTime(), "zone", zoneName)))
	rows := telegram.Markup{{{Text: v.t("btn.zone", "zone", zoneName), CallbackData: cb(v.owner, actZone)}}}
	if v.sc == scopeGroup {
		label := v.t("btn.lang_auto")
		if groupLang != "" {
			label = i18n.LabelOf(groupLang)
		}
		rows = append(rows, []telegram.Button{{Text: v.t("btn.group_language", "language", label), CallbackData: cb(v.owner, actGroupLang)}})
	}
	return text, append(rows, v.navRow(actHome))
}

// zone lists the zones west to east with their offset today, so a summer-time
// switch shows on the button.
func (v view) zone(current string, zones round.Zones, now time.Time) (string, telegram.Markup) {
	buttons := make([]telegram.Button, len(round.ZoneChoices))
	for i, name := range round.ZoneChoices {
		label := v.t("zone."+name) + " · " + utcOffset(now.In(zones.For(name, "")))
		buttons[i] = selectable(label, cb(v.owner, actZone+":"+name), name == current)
	}
	text := panel(v.t("zone.title"), "", v.t("zone.hint", "time", revealTime()), quote(v.t("zone.body")))
	return text, append(pairs(buttons), v.navRow(actSettings))
}

// utcOffset is "UTC", "UTC+2", "UTC+5:30" or "UTC−3", with a real minus sign.
func utcOffset(t time.Time) string {
	_, secs := t.Zone()
	if secs == 0 {
		return "UTC"
	}
	sign := "+"
	if secs < 0 {
		sign, secs = "−", -secs
	}
	s := "UTC" + sign + strconv.Itoa(secs/3600)
	if m := secs % 3600 / 60; m != 0 {
		s += fmt.Sprintf(":%02d", m)
	}
	return s
}

func (v view) stats(st db.Stats) (string, telegram.Markup) {
	body := v.t("stats.empty")
	if st.Days > 0 {
		p := message.NewPrinter(language.Make(v.lang))
		lines := []string{
			v.t("stats.days", "n", p.Sprint(st.Days)),
			v.t("stats.points", "n", p.Sprint(st.Points)),
			v.t("stats.best", "n", p.Sprint(st.Best)),
		}
		if st.MedianKm != nil {
			lines = append(lines, v.t("stats.median", "km", p.Sprint(int(*st.MedianKm+0.5))))
		}
		body = strings.Join(lines, "\n")
	}
	return panel(v.t("stats.title"), "", v.t("stats.hint"), quote(body)), telegram.Markup{v.navRow(actHome)}
}
