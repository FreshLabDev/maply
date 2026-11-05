// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/FreshLabDev/maply/internal/account"
	"github.com/FreshLabDev/maply/internal/oidc/oidctest"
)

// Sign-in on the stand: fake Telegram and Google providers served by the
// stand itself, each with a page to pick who signs in. The site's sign-in
// goes through them exactly as through the real ones, code, PKCE and all.
//
// The session cookie is __Host- and Secure: browsers keep it on
// http://127.0.0.1 and http://localhost, so open the stand at the address it
// prints, the one the providers send the browser back to.

const accountsKey = "maply-dev-accounts"

func init() {
	Register(Extension{Name: "accounts", Mount: mountAccounts, Panel: accountsPanel})
}

// googlePeople are Google accounts of the stand: two of its players and
// someone who plays on the site only.
var googlePeople = []oidctest.Identity{
	{Sub: "dev-google-1001", Name: "Аня"},
	{Sub: "dev-google-1002", Name: "Боря"},
	{Sub: "dev-google-9001", Name: "Only Google"},
}

func mountAccounts(e *Env, mux *http.ServeMux) {
	base := "http://" + e.Addr
	telegram := oidctest.New(base+"/dev/accounts/telegram", "123456", "dev-telegram-secret", true)
	google := oidctest.New(base+"/dev/accounts/google", "dev-google-client", "dev-google-secret", false)
	var people []oidctest.Identity
	for _, p := range e.Players {
		people = append(people, oidctest.Identity{ID: p.ID, Name: p.Name, Username: p.Username})
	}
	telegram.SetPeople(people)
	google.SetPeople(googlePeople)
	mux.Handle("/dev/accounts/telegram/", telegram)
	mux.Handle("/dev/accounts/google/", google)

	svc := account.New(account.Config{
		Store: e.Store, Key: e.Signer.Key(accountsKey), InitData: e.API.InitData, Zones: e.Zones,
		AppURL: base, Log: e.Log, Now: e.Clock.Now,
	})
	svc.SetTelegram(account.Telegram(telegram.Issuer, base, telegram.ClientID, telegram.ClientSecret))
	svc.SetGoogle(account.Google(google.Issuer, base, google.ClientID, google.ClientSecret))
	// The game's handler reads Accounts on every request.
	e.API.Accounts = svc
	mux.Handle("/auth/", svc.Middleware(svc.Handler()))
	mux.Handle("GET /privacy", svc.Privacy())
	mux.HandleFunc("GET /dev/accounts/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(accountsList(r.Context(), e)))
	})
}

// accountsList is the stand's accounts as plain lines, for a look without psql.
func accountsList(ctx context.Context, e *Env) string {
	rows, err := e.Pool.Query(ctx, `
		SELECT a.id::text, COALESCE(a.telegram_user_id::text, '-'), COALESCE(a.google_sub, '-'),
		       (SELECT count(*) FROM maply.session s WHERE s.account_id = a.id),
		       (SELECT count(*) FROM maply.site_player p WHERE p.account_id = a.id)
		  FROM maply.account a ORDER BY a.created_at`)
	if err != nil {
		return err.Error()
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("account\ttelegram\tgoogle\tsessions\tsite players\n")
	for rows.Next() {
		var id, tgID, sub string
		var sessions, players int
		if err := rows.Scan(&id, &tgID, &sub, &sessions, &players); err != nil {
			return err.Error()
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%d\t%d\n", id, tgID, sub, sessions, players)
	}
	return b.String()
}

func accountsPanel(e *Env, r *http.Request) template.HTML {
	var n int
	_ = e.Pool.QueryRow(r.Context(), `SELECT count(*) FROM maply.account`).Scan(&n)
	return template.HTML(fmt.Sprintf(`<section><h2>Вход на сайте</h2>
<p>Telegram и Google поддельные: «Войти» на сайте ведёт на страницу выбора, кто входит. Аккаунтов: %d.</p>
<p><a class="btn" href="/">Открыть сайт</a> <a class="btn" href="/dev/accounts/list">Аккаунты</a> <a class="btn" href="/privacy">/privacy</a></p></section>`, n))
}
