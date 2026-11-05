// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/FreshLabDev/maply/internal/thumb"
)

// The days page's part of the stand: the strips of the closed days, drawn in
// the background as the service draws them, so a day closed by moving the
// clock gets its strip a few seconds later.
func init() {
	Register(Extension{
		Name: "days",
		Mount: func(e *Env, mux *http.ServeMux) {
			s := &thumb.Service{Store: e.Store, ImagesDir: e.ImagesDir, Log: e.Log}
			go s.Run(context.Background(), 5*time.Second)
			// /dev/days/redraw drops every strip; the next pass draws them
			// again under new keys.
			mux.HandleFunc("POST /dev/days/redraw", func(w http.ResponseWriter, r *http.Request) {
				if _, err := e.Pool.Exec(r.Context(), `DELETE FROM maply.day_thumb`); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				e.back(w, r, "Кадры удалены, сервис нарисует их за несколько секунд")
			})
		},
		Panel: func(e *Env, r *http.Request) template.HTML {
			var kept, drawn int
			if err := e.Pool.QueryRow(r.Context(), `
				SELECT (SELECT count(*) FROM maply.day_closed), (SELECT count(*) FROM maply.day_thumb)`).Scan(&kept, &drawn); err != nil {
				return template.HTML(template.HTMLEscapeString(err.Error()))
			}
			return template.HTML(fmt.Sprintf(`<h2>Дни</h2>
<p class="muted">Кадров нарисовано: %d из %d закрытых дней.</p>
<form method="post" action="/dev/days/redraw"><button>Нарисовать кадры заново</button></form>`, drawn, kept))
		},
	})
}
