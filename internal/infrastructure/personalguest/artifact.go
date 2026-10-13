package personalguest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func LoadArtifact(path string) ([]byte, error) {
	return readProtectedFile(path, 0022, 64<<20)
}

func readProtectedFile(path string, forbidden os.FileMode, maximum int64) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&forbidden != 0 || before.Size() < 1 || before.Size() > maximum {
		return nil, fmt.Errorf("personal guest file must be protected, regular and bounded")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm()&forbidden != 0 {
		return nil, fmt.Errorf("personal guest artifact changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(raw)) != before.Size() {
		return nil, fmt.Errorf("personal guest artifact changed while reading")
	}
	return raw, nil
}
