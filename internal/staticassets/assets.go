package staticassets

import (
	"io/fs"
	"os"
)

func DiskFS(dir string) (fs.FS, error) {
	diskFS := os.DirFS(dir)
	if _, err := fs.Stat(diskFS, "."); err != nil {
		return nil, err
	}
	return diskFS, nil
}
