//go:build linux

package networkproxyruntime

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// Linux also terminates the child if its owning runtime exits unexpectedly. The
// authenticated bridge independently closes traffic on every platform.
func configureOwnedEngineProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setsid: true}
}

func signalOwnedProcessGroup(command *exec.Cmd, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-command.Process.Pid, signal)
}

func approvedBinaryOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == 0 || int(stat.Uid) == os.Geteuid())
}

func waitOwnedProcessExit(pid int) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err != unix.EINTR {
			return err
		}
	}
}
