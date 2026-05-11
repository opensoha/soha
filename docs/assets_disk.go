//go:build !embedassets

package docs

import (
	"io/fs"
	"os"
)

func StaticFS() (fs.FS, error) {
	diskFS := os.DirFS("docs/build")
	if _, err := fs.Stat(diskFS, "."); err != nil {
		return nil, err
	}
	return diskFS, nil
}
