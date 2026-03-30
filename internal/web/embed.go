package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the embedded frontend dist filesystem.
func Assets() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
