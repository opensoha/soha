//go:build darwin || linux

package runtimeinfo

import (
	"os"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"golang.org/x/sys/unix"
)

func processCPUTime() (time.Duration, error) {
	var usage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &usage); err != nil {
		return 0, err
	}
	return timevalDuration(usage.Utime) + timevalDuration(usage.Stime), nil
}

func timevalDuration(value unix.Timeval) time.Duration {
	return time.Duration(value.Sec)*time.Second + time.Duration(value.Usec)*time.Microsecond
}

func diskUsage() sohaapi.RuntimeDiskUsage {
	path, err := os.Getwd()
	if err != nil {
		return sohaapi.RuntimeDiskUsage{}
	}
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return sohaapi.RuntimeDiskUsage{Path: path}
	}
	total := int64(stats.Blocks) * int64(stats.Bsize)
	available := int64(stats.Bavail) * int64(stats.Bsize)
	free := int64(stats.Bfree) * int64(stats.Bsize)
	used := max(0, total-free)
	return sohaapi.RuntimeDiskUsage{
		Available:      true,
		Path:           path,
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		UsagePercent:   percent(used, total),
	}
}
