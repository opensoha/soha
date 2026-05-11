//go:build !embedassets

package web

import (
	"io/fs"
	"os"
)

func StaticFS() (fs.FS, error) {
	diskFS := os.DirFS("web/dist")
	if _, err := fs.Stat(diskFS, "."); err != nil {
		return nil, err
	}
	return diskFS, nil
}
