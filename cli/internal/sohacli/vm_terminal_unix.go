//go:build !windows

package sohacli

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

func readVMTerminalFile(ctx context.Context, file *os.File, buffer []byte) (int, error) {
	descriptor := file.Fd()
	if descriptor > math.MaxInt32 {
		return 0, fmt.Errorf("terminal file descriptor is out of range")
	}
	fd := int(descriptor)
	for ctx.Err() == nil {
		poll := []unix.PollFd{{Fd: int32(descriptor), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		n, err = unix.Read(fd, buffer)
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if n == 0 && err == nil {
			err = io.EOF
		}
		return n, err
	}
	return 0, ctx.Err()
}
