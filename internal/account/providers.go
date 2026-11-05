// SPDX-License-Identifier: Apache-2.0
package account

import (
	"strings"

	"github.com/FreshLabDev/maply/internal/db"
	"github.com/FreshLabDev/maply/internal/oidc"
)

// The providers' issuers.
const (
	TelegramIssuer = "https://oauth.telegram.org"
	GoogleIssuer   = "https://accounts.google.com"
)

// CallbackURL is where a provider sends the browser back on the site at
// appURL; it is registered with the provider as it is.
func CallbackURL(appURL, provider string) string {
	return strings.TrimRight(appURL, "/") + "/auth/" + provider + "/callback"
}

// Telegram is Telegram's sign-in for the site at appURL. The client id is
// the bot's id. `profile` brings the id, the name and the username; the
// photo it also brings is not read.
func Telegram(issuer, appURL, clientID, secret string) *oidc.Provider {
	return oidc.New(oidc.Config{
		Issuer: issuer, ClientID: clientID, ClientSecret: secret,
		RedirectURL: CallbackURL(appURL, db.KindTelegram), Scopes: []string{"openid", "profile"},
	})
}

// Google is Google's sign-in for the site at appURL. It asks for `openid`
// alone: the account is the `sub`, and Google sends no name, address or
// photo. select_account lets a shared computer pick the right person.
func Google(issuer, appURL, clientID, secret string) *oidc.Provider {
	return oidc.New(oidc.Config{
		Issuer: issuer, ClientID: clientID, ClientSecret: secret,
		RedirectURL: CallbackURL(appURL, db.KindGoogle), Scopes: []string{"openid"},
		AuthParams: map[string]string{"prompt": "select_account"},
	})
}
