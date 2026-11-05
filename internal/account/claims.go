// SPDX-License-Identifier: Apache-2.0
package account

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/FreshLabDev/tg"
)

// telegramClaims are the parts of Telegram's id_token maply reads. The
// account is `id`, the user id the bot and the Mini App know; `sub` is
// another number. The photo is not read: the bot keeps its own.
type telegramClaims struct {
	ID                json.RawMessage `json:"id"`
	Name              string          `json:"name"`
	GivenName         string          `json:"given_name"`
	FamilyName        string          `json:"family_name"`
	PreferredUsername string          `json:"preferred_username"`
	Locale            string          `json:"locale"`
}

// user is the person for core.touch_person. `id` is a number in Telegram's
// tokens; a string of digits is read too, anything else is no account.
// known says core has the person already: then a name of several words is
// left out, since "Mary Ann" and "Smith" cannot be told apart and a wrong
// split would overwrite what the bot wrote to the family's core.person.
func (c telegramClaims) user(known bool) (tg.User, error) {
	idText := strings.TrimSpace(string(c.ID))
	if s, err := strconv.Unquote(idText); err == nil {
		idText = s
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		return tg.User{}, errors.New("the id_token has no usable id claim")
	}
	u := tg.User{ID: id, Username: strings.TrimSpace(c.PreferredUsername), LanguageCode: language(c.Locale)}
	name := strings.TrimSpace(c.Name)
	switch {
	case c.GivenName != "" || c.FamilyName != "":
		u.FirstName, u.LastName = strings.TrimSpace(c.GivenName), strings.TrimSpace(c.FamilyName)
	case name != "" && !strings.ContainsAny(name, " \t\n"):
		u.FirstName = name
	case name != "" && !known:
		// Someone the bot has never seen gets the whole name as the first
		// one; the bot writes it properly at their first message.
		u.FirstName = name
	}
	return u, nil
}

// language cuts a locale such as "en-US" to the letters core takes, or "".
func language(locale string) string {
	l, _, _ := strings.Cut(strings.ReplaceAll(locale, "_", "-"), "-")
	l = strings.ToLower(l)
	if len(l) < 2 || len(l) > 3 {
		return ""
	}
	for _, r := range l {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return l
}
