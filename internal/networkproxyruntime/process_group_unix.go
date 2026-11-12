//go:build !windows

package networkproxyruntime

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func signalOwnedProcess(command *exec.Cmd, force bool) error {
	if force {
		return command.Process.Kill()
	}
	return command.Process.Signal(syscall.SIGTERM)
}

// Group signalling requires an unreaped leader. A vanished or changed group
// can only be probed after Wait, never signalled using its former number.
func signalAnchoredGroup(command *exec.Cmd, force bool) error {
	attrs := command.SysProcAttr
	if attrs != nil && (attrs.Setsid || attrs.Setpgid && attrs.Pgid == 0) {
		return signalOwnedProcessGroup(command, force)
	}
	return signalOwnedProcess(command, force)
}

func ownedProcessGroupAbsent(command *exec.Cmd) (bool, error) {
	err := syscall.Kill(-command.Process.Pid, 0)
	switch {
	case errors.Is(err, syscall.ESRCH):
		return true, nil
	case err == nil:
		return false, nil
	default:
		return false, os.NewSyscallError("probe owned process group", err)
	}
}
