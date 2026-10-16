// Package web holds the built web interface (npm run build), so the server ships as one file.
package web

import (
	"embed"
	"io/fs"
)

// dist always holds .gitkeep (copied from public/ on every build), so the server compiles even
// before the interface is built.
//
//go:embed all:dist
var dist embed.FS

// Files returns the built interface, or nil when it has not been built: the server then
// offers only the API, which is all the command line tool needs.
func Files() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
