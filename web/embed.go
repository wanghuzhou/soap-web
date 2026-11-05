// Package web bundles the frontend assets into the binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:assets
var assets embed.FS

// FS returns the frontend as an http.FileSystem rooted at assets/.
func FS() http.FileSystem {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}
