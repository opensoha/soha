//go:build linux

package networkproxyruntime

import (
	"os/exec"
	"syscall"
)

// Linux also terminates the child if its owning runtime exits unexpectedly. The
// authenticated bridge independently closes traffic on every platform.
func configureOwnedEngineProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
