//go:build linux

package runtimeinfo

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func networkCounters() (int64, int64, bool) {
	file, err := os.Open("/proc/self/net/dev")
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()

	var rxBytes int64
	var txBytes int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		_, values, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(values)
		if len(fields) < 9 {
			continue
		}
		rx, rxErr := strconv.ParseInt(fields[0], 10, 64)
		tx, txErr := strconv.ParseInt(fields[8], 10, 64)
		if rxErr == nil && txErr == nil {
			rxBytes += rx
			txBytes += tx
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, false
	}
	return rxBytes, txBytes, true
}
