//go:build windows

package networkproxyruntime

import (
	"errors"
	"os"
	"os/exec"
)

func configureOwnedEngineProcess(_ *exec.Cmd)            {}
func signalOwnedProcess(command *exec.Cmd, _ bool) error { return command.Process.Kill() }
func signalAnchoredGroup(command *exec.Cmd, force bool) error {
	return signalOwnedProcess(command, force)
}
func ownedProcessGroupAbsent(_ *exec.Cmd) (bool, error) {
	return false, errors.New("owned process group proof unavailable")
}

func approvedBinaryOwner(_ os.FileInfo) bool { return false }
