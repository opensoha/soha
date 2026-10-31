//go:build !windows

package sohacli

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func tryCredentialFileLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}
func unlockCredentialFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
