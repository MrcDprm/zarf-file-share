package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func webServer(t *testing.T) *Server {
	t.Helper()
	files := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>send</title>")},
		"d.html":               {Data: []byte("<!doctype html><title>receive</title>")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"assets/main-abc.js":   {Data: []byte("console.log(1)")},
		"assets/font.woff2":    {Data: []byte("wOF2")},
		"assets/.hidden":       {Data: []byte("no")},
		".gitkeep":             {Data: nil},
		"assets/notes.unknown": {Data: []byte("?")},
	}
	s, _ := newTestServer(t, func(c *Config) { c.Web = files })
	return s
}

func get(t *testing.T, s *Server, path string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Result(), string(body)
}

func TestPages(t *testing.T) {
	s := webServer(t)
	cases := []struct {
		path, want, contentType, cache string
	}{
		{"/", "send", "text/html; charset=utf-8", "no-cache"},
		{"/d/AAAAAAAAAAAAAAAAAAAAAA", "receive", "text/html; charset=utf-8", "no-cache"},
		{"/favicon.svg", "<svg/>", "image/svg+xml", "no-cache"},
		{"/assets/main-abc.js", "console.log(1)", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"},
		{"/assets/font.woff2", "wOF2", "font/woff2", "public, max-age=31536000, immutable"},
		{"/assets/notes.unknown", "?", "application/octet-stream", "public, max-age=31536000, immutable"},
	}
	for _, c := range cases {
		res, body := get(t, s, c.path)
		if res.StatusCode != 200 || !strings.Contains(body, c.want) {
			t.Errorf("%s: %d %q", c.path, res.StatusCode, body)
			continue
		}
		if got := res.Header.Get("Content-Type"); got != c.contentType {
			t.Errorf("%s: Content-Type %q", c.path, got)
		}
		if got := res.Header.Get("Cache-Control"); got != c.cache {
			t.Errorf("%s: Cache-Control %q", c.path, got)
		}
		if got := res.Header.Get("Content-Security-Policy"); got != pageCSP {
			t.Errorf("%s: CSP %q", c.path, got)
		}
	}
}

func TestPagesServeNothingElse(t *testing.T) {
	s := webServer(t)
	for _, p := range []string{"/.gitkeep", "/assets/.hidden", "/assets/missing.js", "/index.html", "/d.html", "/d/x/y", "/other"} {
		if res, _ := get(t, s, p); res.StatusCode == 200 {
			t.Errorf("%s: served", p)
		}
	}
	res, _ := get(t, s, "/")
	if res.Header.Get("X-Frame-Options") != "DENY" || res.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Error("security headers missing on a page")
	}
}

func TestWithoutWebOnlyTheAPI(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if res, _ := get(t, s, "/"); res.StatusCode != 404 {
		t.Errorf("/ without a built interface: %d", res.StatusCode)
	}
	res, _ := get(t, s, "/api/v1/files/AAAAAAAAAAAAAAAAAAAAAA")
	if res.StatusCode != 404 || res.Header.Get("Content-Security-Policy") != apiCSP {
		t.Errorf("API answer: %d, CSP %q", res.StatusCode, res.Header.Get("Content-Security-Policy"))
	}
}
