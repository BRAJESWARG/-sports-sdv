// Package webui serves the embedded single-page chatbot UI.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web
var files embed.FS

// Handler returns an http.Handler that serves the embedded web/ directory.
// Because the assets are embedded, it works regardless of the process CWD.
func Handler() http.Handler {
	sub, err := fs.Sub(files, "web")
	if err != nil {
		panic(err) // embedded FS is known at build time; this cannot fail
	}
	return http.FileServer(http.FS(sub))
}
