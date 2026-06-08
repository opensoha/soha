//go:build embedassets

package staticassets

import (
	"embed"
	"io/fs"
)

//go:embed all:web/dist
var embeddedWebAssets embed.FS

func DefaultWebFS(_ string) (fs.FS, error) {
	return fs.Sub(embeddedWebAssets, "web/dist")
}
