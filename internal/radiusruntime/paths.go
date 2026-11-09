package radiusruntime

import (
	"errors"
	"os"
	"path/filepath"
)

// pathPolicy is private injection for portable filesystem tests. Production
// always uses Lstat and the kernel UID; installation JSON cannot override it.
type pathPolicy struct {
	lstat func(string) (os.FileInfo, error)
	owner func(os.FileInfo) (uint32, uint32, bool)
}

func nativePaths() pathPolicy       { return pathPolicy{os.Lstat, ownership} }
func (h hostOps) paths() pathPolicy { return pathPolicy{h.lstat, h.ownership} }
func safePath(path string) error    { return nativePaths().check(path) }

// check permits a missing leaf (new state files/directories), never a missing
// ancestor. Root-owned leaves under a replaceable parent are not trusted.
func (p pathPolicy) check(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrInvalid
	}
	info, err := p.lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrDenied
	}
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return ErrDenied
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err = p.lstat(parent)
		if err != nil || !info.IsDir() || !p.protected(info) {
			return ErrDenied
		}
		if parent == string(os.PathSeparator) {
			return nil
		}
	}
}

func (p pathPolicy) protected(info os.FileInfo) bool {
	uid, _, ok := p.owner(info)
	return ok && uid == 0 && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0022 == 0
}

func (p pathPolicy) regular(path string) (os.FileInfo, error) {
	if err := p.check(path); err != nil {
		return nil, err
	}
	info, err := p.lstat(path)
	if err != nil || !info.Mode().IsRegular() || !p.protected(info) {
		return nil, ErrDenied
	}
	return info, nil
}

func (p pathPolicy) directory(path string) error {
	if err := p.check(path); err != nil {
		return err
	}
	info, err := p.lstat(path)
	if err != nil || !info.IsDir() || !p.protected(info) {
		return ErrDenied
	}
	return nil
}

func (s *Supervisor) safePath(path string) error { return s.host.paths().check(path) }
