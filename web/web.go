// SPDX-License-Identifier: Apache-2.0

// Package web serves the Mini App: plain HTML, CSS and JavaScript embedded in
// the binary, with MapLibre, Pannellum and Sofia Sans vendored beside them.
// Only telegram-web-app.js and the map tiles come from outside; unpkg,
// jsdelivr and Google Fonts are unreliable from Russia.
//
// The app's words are not in its files: /texts/<lang>.json carries the "app."
// keys of internal/i18n and the names of the time zones, one file per
// language.
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/FreshLabDev/maply/internal/i18n"
	"github.com/FreshLabDev/maply/internal/round"
)

//go:generate go run gen_land.go -step 4 -o app/land.js

//go:embed app
var embedded embed.FS

// TileHost serves the vector tiles and the map glyphs.
const TileHost = "https://tiles.openfreemap.org"

// CSP names every host the page may reach, so a script that starts calling
// somewhere new fails in the browser before it ships. MapLibre runs its
// workers from blob: URLs; style attributes come from innerHTML templates. The
// RTL text plugin compiles WebAssembly, which 'wasm-unsafe-eval' allows without
// allowing eval.
const CSP = "default-src 'none'; " +
	"script-src 'self' 'wasm-unsafe-eval' https://telegram.org; " +
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
	".json":  "application/json; charset=utf-8",
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
		h.add("/"+name, data, strings.HasPrefix(name, "vendor/"))
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, lang := range i18n.Codes() {
		data, err := Texts(lang)
		if err != nil {
			return nil, err
		}
		h.add("/texts/"+lang+".json", data, false)
	}
	// Cloudflare caches .js and .css and tells browsers to keep them for
	// hours, whatever Cache-Control says; index.html it does not cache. So
	// index.html names each of the app's own files with its hash: a deploy is
	// never a new page on an old script.
	if idx, ok := h.files["/index.html"]; ok {
		data := localRef.ReplaceAllFunc(idx.data, func(m []byte) []byte {
			sm := localRef.FindSubmatch(m)
			f, ok := h.files["/"+string(sm[2])]
			if !ok || f.immutable {
				return m
			}
			return []byte(string(sm[1]) + `="` + string(sm[2]) + "?v=" + strings.Trim(f.etag, `"`) + `"`)
		})
		h.add("/index.html", data, false)
	}
	return h, nil
}

// localRef is a script or stylesheet index.html loads from the service.
var localRef = regexp.MustCompile(`(src|href)="([^":?#]+\.(?:js|css))"`)

func (h *handler) add(name string, data []byte, immutable bool) {
	sum := sha256.Sum256(data)
	ctype, ok := contentTypes[path.Ext(name)]
	if !ok {
		ctype = "application/octet-stream"
	}
	h.files[name] = file{
		data:      data,
		etag:      `"` + hex.EncodeToString(sum[:12]) + `"`,
		ctype:     ctype,
		immutable: immutable,
	}
}

// Texts is what the app reads in lang: its strings without the "app." prefix,
// and the name of every zone a chat can be in.
func Texts(lang string) ([]byte, error) {
	t := map[string]string{}
	for key, text := range i18n.WithPrefix(lang, "app.") {
		t[strings.TrimPrefix(key, "app.")] = text
	}
	zones := map[string]string{}
	for _, name := range round.ZoneChoices {
		zones[name] = i18n.T(lang, "zone."+name)
	}
	return json.Marshal(map[string]any{"lang": lang, "t": t, "zone": zones})
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
