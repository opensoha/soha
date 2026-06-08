//go:build !embedassets

package staticassets

import "io/fs"

func DefaultWebFS(dir string) (fs.FS, error) {
	return DiskFS(dir)
}
