//go:build linux

package radiusruntime

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func peerCredentials(conn net.Conn) (uint32, int, error) {
	socket, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, ErrDenied
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var credential *unix.Ucred
	var readErr error
	if err = raw.Control(func(fd uintptr) {
		credential, readErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if readErr != nil {
		return 0, 0, readErr
	}
	return credential.Uid, int(credential.Pid), nil
}
func processStartToken(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	index := strings.LastIndex(string(b), ") ")
	if index < 0 {
		return "", ErrInvalid
	}
	fields := strings.Fields(string(b)[index+2:])
	if len(fields) < 20 {
		return "", ErrInvalid
	}
	return fields[19], nil
}
func configureChild(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
func lockOwner(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func ownership(info os.FileInfo) (uint32, uint32, bool) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return s.Uid, s.Gid, true
}
func supportedHost() bool { return os.Geteuid() == 0 }
