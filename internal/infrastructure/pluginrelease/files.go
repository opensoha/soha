package pluginrelease

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

var errPackageRoot = apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "plugin_repository_package_store_unavailable",
	"The protected local plugin package store is unavailable.", "受保护的本地插件制品目录不可用。")

func openPackageRoot(path string, create bool) (*os.Root, error) {
	volume := filepath.VolumeName(path)
	base := volume + string(filepath.Separator)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == base {
		return nil, errPackageRoot
	}
	directory, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	segments := strings.Split(strings.TrimPrefix(path, base), string(filepath.Separator))
	for i, name := range segments {
		private := i == len(segments)-1
		next, err := openDirectory(directory, name, create, private)
		closeErr := directory.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			_ = next.Close()
			return nil, closeErr
		}
		directory = next
	}
	return directory, nil
}

func privateChild(parent *os.Root, name string, create bool) (*os.Root, error) {
	if !filepath.IsLocal(name) || name == "." || filepath.Base(name) != name {
		return nil, errPackageRoot
	}
	return openDirectory(parent, name, create, true)
}

func openDirectory(parent *os.Root, name string, create, private bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) && create {
		if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if err := syncDirectory(parent, "."); err != nil {
			return nil, err
		}
		info, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !protectedDirectory(info, private) {
		return nil, errPackageRoot
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	current, err := child.Stat(".")
	if err != nil || !os.SameFile(info, current) || !protectedDirectory(current, private) {
		_ = child.Close()
		return nil, errPackageRoot
	}
	return child, nil
}

func syncDirectory(directory *os.Root, name string) error {
	file, err := directory.Open(name)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func writePrivateFile(directory *os.Root, name string, write func(io.Writer) error) error {
	file, err := directory.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := write(file); err != nil {
		return errors.Join(err, file.Close())
	}
	return errors.Join(file.Chmod(0o400), file.Sync(), file.Close())
}

func openPrivateFile(directory *os.Root, name string) (*os.File, error) {
	info, err := directory.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !protectedFile(info) {
		return nil, errPackageRoot
	}
	file, err := directory.Open(name)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) || !protectedFile(actual) {
		_ = file.Close()
		return nil, errPackageRoot
	}
	return file, nil
}

func readPrivateFile(directory *os.Root, name string, limit int64) ([]byte, error) {
	file, err := openPrivateFile(directory, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errArtifact
	}
	return raw, nil
}
