//go:build !unix

package pluginrelease

import "io/fs"

// This store requires Unix ownership and file mode semantics.
func protectedDirectory(fs.FileInfo, bool) bool { return false }

func protectedFile(fs.FileInfo) bool { return false }
