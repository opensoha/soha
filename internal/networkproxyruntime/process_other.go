//go:build !linux && !windows

package networkproxyruntime

import (
	"os"
	"os/exec"
	"syscall"
)

func configureOwnedEngineProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
