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
//go:generate go run gen_profland.go -step 2 -o app/profland.js

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
	"base-uri 'none'; form-action 'none'; " +
	// Only Telegram's web clients frame the app: a page elsewhere cannot lay
	// it under its own buttons and have the guess pressed through.
	"frame-ancestors 'self' https://web.telegram.org https://*.telegram.org"

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

// Pages are the addresses the app draws itself. The server answers each with
// index.html, and router.js picks the screen from the path. "*" stands for one
// path segment: a day, a party code, a player. A screen that needs a new
// address adds it here.
var Pages = []string{"/days", "/days/*", "/me", "/play", "/party/*", "/p/*"}

// segment is what "*" in Pages matches: short, and nothing a file name or a
// path trick could need.
var segment = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// isPage says whether a cleaned path is one of Pages.
func isPage(name string) bool {
	got := strings.Split(strings.TrimPrefix(name, "/"), "/")
	for _, p := range Pages {
		want := strings.Split(strings.TrimPrefix(p, "/"), "/")
		if len(want) != len(got) {
			continue
		}
		ok := true
		for i, w := range want {
			if w == "*" {
				ok = ok && segment.MatchString(got[i])
			} else {
				ok = ok && w == got[i]
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// Handler serves the app at the root: "/" and every address in Pages is
// index.html, anything else not in the embedded tree is a 404.
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
			f, ok := h.files["/"+strings.TrimPrefix(string(sm[2]), "/")]
			if !ok || f.immutable {
				return m
			}
			return []byte(string(sm[1]) + `="` + string(sm[2]) + "?v=" + strings.Trim(f.etag, `"`) + `"`)
		})
		h.add("/index.html", data, false)
	}
	return h, nil
}

// localRef is a script or stylesheet index.html loads from the service. The
// page is served on every address in Pages, so it names them from the root.
var localRef = regexp.MustCompile(`(src|href)="([^":?#]+\.(?:js|css))"`)

// host is a Host header fit to name the preview's address with.
var host = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}(:[0-9]{1,5})?$`)

// publicPage is index.html for a player's public page /p/<slug>: kept out of
// search engines, with the player's picture for a link's preview, which
// needs an absolute address. slug is a path segment isPage accepted.
func publicPage(f file, slug string, r *http.Request) file {
	meta := `<meta name="robots" content="noindex, nofollow">`
	if host.MatchString(r.Host) {
		scheme := "https"
		if h, _, _ := strings.Cut(r.Host, ":"); h == "127.0.0.1" || h == "localhost" {
			scheme = "http"
		}
		meta += `<meta property="og:image" content="` + scheme + "://" + r.Host + "/api/p/" + slug + `/card.png">` +
			`<meta name="twitter:card" content="summary_large_image">`
	}
	data := bytes.Replace(f.data, []byte("</head>"), []byte(meta+"</head>"), 1)
	sum := sha256.Sum256(data)
	return file{data: data, etag: `"` + hex.EncodeToString(sum[:12]) + `"`, ctype: f.ctype}
}

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
// the units the bot writes distances with, and the name of every zone a chat
// can be in.
func Texts(lang string) ([]byte, error) {
	t := map[string]string{}
	for key, text := range i18n.WithPrefix(lang, "app.") {
		t[strings.TrimPrefix(key, "app.")] = text
	}
	for key, text := range i18n.WithPrefix(lang, "unit.") {
		t[key] = text
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
	page := name
	if name == "/" || isPage(name) {
		name = "/index.html"
	}
	f, ok := h.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if slug, ok := strings.CutPrefix(page, "/p/"); ok && name == "/index.html" {
		f = publicPage(f, slug, r)
	}
	hd := w.Header()
	if strings.HasPrefix(page, "/p/") {
		hd.Set("X-Robots-Tag", "noindex, nofollow")
	}
	hd.Set("Content-Type", f.ctype)
	hd.Set("Content-Security-Policy", CSP)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Referrer-Policy", "no-referrer")
	// The site is only ever HTTPS behind Cloudflare. Without subdomains: the
	// rest of ws04.cc is not this app's to decide.
	hd.Set("Strict-Transport-Security", "max-age=31536000")
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
