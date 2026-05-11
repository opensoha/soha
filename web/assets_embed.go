//go:build embedassets

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedAssets embed.FS

func StaticFS() (fs.FS, error) {
	return fs.Sub(embeddedAssets, "dist")
}
