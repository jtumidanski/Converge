// Package ui embeds the built frontend (apps/frontend → internal/ui/dist).
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the dist directory as the root of a filesystem.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("ui: dist directory missing from embed: " + err.Error())
	}
	return sub
}

// Present reports whether a built index.html is embedded.
func Present() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
