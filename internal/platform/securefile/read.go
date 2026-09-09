package securefile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Read opens a bounded regular file through an anchored directory. Symlinks
// may point within that directory (as Kubernetes Secret volumes require), but
// cannot escape it. Private files may be group-readable, but not writable by
// the group or accessible by other users.
func Read(path string, maxBytes int64, private bool) ([]byte, error) {
	if strings.TrimSpace(path) == "" || maxBytes < 1 {
		return nil, fmt.Errorf("secure file path and positive size limit are required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve secure file path: %w", err)
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, fmt.Errorf("open secure file directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	return ReadRoot(root, filepath.Base(absolute), maxBytes, private)
}

// ReadRoot reads a file relative to an already anchored directory.
func ReadRoot(root *os.Root, name string, maxBytes int64, private bool) ([]byte, error) {
	if root == nil || strings.TrimSpace(name) == "" || maxBytes < 1 {
		return nil, fmt.Errorf("secure file root, name, and positive size limit are required")
	}
	if private {
		info, err := root.Stat(".")
		if err != nil || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
			return nil, fmt.Errorf("private file directory must not be group- or world-writable")
		}
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("secure file must be a regular file")
	}
	if private && info.Mode().Perm()&0o027 != 0 {
		return nil, fmt.Errorf("private file must not be group-writable or accessible by other users")
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("secure file exceeds %d bytes", maxBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("secure file exceeds %d bytes", maxBytes)
	}
	return raw, nil
}
