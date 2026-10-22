// SPDX-License-Identifier: Apache-2.0

// Package web serves the Mini App: plain HTML, CSS and JavaScript embedded in
// the binary, with MapLibre, Pannellum and Sofia Sans vendored beside them.
// Only telegram-web-app.js and the map tiles come from outside; unpkg,
// jsdelivr and Google Fonts are unreliable from Russia.
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed app
var embedded embed.FS

// TileHost serves the vector tiles and the map glyphs.
const TileHost = "https://tiles.openfreemap.org"

// CSP names every host the page may reach, so a script that starts calling
// somewhere new fails in the browser before it ships. MapLibre runs its
// workers from blob: URLs; style attributes come from innerHTML templates.
const CSP = "default-src 'none'; " +
	"script-src 'self' https://telegram.org; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self'; " +
	"connect-src 'self' " + TileHost + "; " +
	"worker-src blob:; child-src blob:; " +
	"base-uri 'none'; form-action 'none'"

var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8",
}

type file struct {
	data  []byte
	etag  string
	ctype string
	// immutable files sit under a versioned vendor path and never change.
	immutable bool
}

type handler struct {
	files map[string]file
}

// Handler serves the app at the root: "/" is index.html, anything not in the
// embedded tree is a 404.
func Handler() (http.Handler, error) {
	root, err := fs.Sub(embedded, "app")
	if err != nil {
		return nil, err
	}
	h := &handler{files: map[string]file{}}
	err = fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(root, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		ctype, ok := contentTypes[path.Ext(name)]
		if !ok {
			ctype = "application/octet-stream"
		}
		h.files["/"+name] = file{
			data:      data,
			etag:      `"` + hex.EncodeToString(sum[:12]) + `"`,
			ctype:     ctype,
			immutable: strings.HasPrefix(name, "vendor/"),
		}
		return nil
	})
	return h, err
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := path.Clean(r.URL.Path)
	if name == "/" {
		name = "/index.html"
	}
	f, ok := h.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", f.ctype)
	hd.Set("Content-Security-Policy", CSP)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Referrer-Policy", "no-referrer")
	hd.Set("ETag", f.etag)
	if f.immutable {
		hd.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// The app changes with every deploy: revalidate, and let the ETag
		// turn an unchanged file into a 304.
		hd.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.data))
}
