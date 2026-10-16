package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// pageCSP lets the pages run only this server's own scripts, styles and fonts, and talk only
// to this server. A file name or anything else from a sender can never turn into code: the
// pages write text only, and Trusted Types makes the browser refuse HTML built from strings.
const pageCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; " +
	"connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; " +
	"require-trusted-types-for 'script'; trusted-types 'none'"

// apiCSP is for everything else: a JSON answer has nothing to load.
const apiCSP = "default-src 'none'; frame-ancestors 'none'"

var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
}

// webRoutes serves the built interface: "/" sends a file, "/d/{id}" receives one, "/r/{rid}"
// uploads to a request.
func (s *Server) webRoutes() {
	s.mux.HandleFunc("GET /{$}", s.serveFile("index.html", "no-cache"))
	s.mux.HandleFunc("GET /d/{id}", s.serveFile("d.html", "no-cache"))
	s.mux.HandleFunc("GET /r/{rid}", s.serveFile("r.html", "no-cache"))
	s.mux.HandleFunc("GET /favicon.svg", s.serveFile("favicon.svg", "no-cache"))
	s.mux.HandleFunc("GET /assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		// Asset names carry a hash of their content, so a browser may keep them for good.
		s.serveFile("assets/"+r.PathValue("name"), "public, max-age=31536000, immutable")(w, r)
	})
}

func (s *Server) serveFile(name, cacheControl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(s.cfg.Web, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ct, ok := contentTypes[path.Ext(name)]
		if !ok {
			ct = "application/octet-stream"
		}
		h := w.Header()
		h.Set("Content-Type", ct)
		h.Set("Cache-Control", cacheControl)
		h.Set("Content-Security-Policy", pageCSP)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}
}
