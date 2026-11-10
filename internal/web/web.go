// Package web embeds the static Web UI served by `odm serve --listen`.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var content embed.FS

// FS returns the UI rooted at its index.html.
func FS() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
