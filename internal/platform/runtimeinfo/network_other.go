//go:build !linux

package runtimeinfo

func networkCounters() (int64, int64, bool) {
	return 0, 0, false
}
