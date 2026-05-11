//go:build embedassets

package docs

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var embeddedAssets embed.FS

func StaticFS() (fs.FS, error) {
	return fs.Sub(embeddedAssets, "build")
}
