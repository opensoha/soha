//go:build !windows

package sohacli

import (
	"fmt"
	"os"
	"path/filepath"
)

func requirePrivateConfigDirectory(dir string, created bool) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("profile config directory must be a regular directory")
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	home, err := os.UserHomeDir()
	if created || err == nil && filepath.Clean(dir) == filepath.Join(home, ".soha") {
		return os.Chmod(dir, 0o700) // #nosec G302 -- This is a private directory, which requires owner traversal.
	}
	return fmt.Errorf("profile config requires a private directory (0700), got %04o; choose a dedicated config directory", info.Mode().Perm())
}

func restrictPrivateFile(path string) error { return os.Chmod(path, 0o600) }

func atomicReplaceFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
