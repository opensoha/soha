//go:build unix

package pluginrelease

import (
	"io/fs"
	"os"
	"syscall"
)

func protectedDirectory(info fs.FileInfo, private bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	const special = fs.ModeSetuid | fs.ModeSetgid
	if !ok || !info.IsDir() || info.Mode()&special != 0 {
		return false
	}
	current := int64(os.Geteuid())
	if private {
		return int64(stat.Uid) == current && info.Mode().Perm() == 0o700 && info.Mode()&fs.ModeSticky == 0
	}
	// A sticky administrator-owned ancestor (for example /tmp) protects
	// children against replacement by other users. Every child is rechecked.
	return (stat.Uid == 0 || int64(stat.Uid) == current) &&
		(info.Mode().Perm()&0o022 == 0 || (stat.Uid == 0 && info.Mode()&fs.ModeSticky != 0))
}

func protectedFile(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0o400 &&
		info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) == 0 &&
		int64(stat.Uid) == int64(os.Geteuid()) && stat.Nlink == 1
}
