//go:build !linux

package networkproxyruntime

import "os/exec"

func configureOwnedEngineProcess(_ *exec.Cmd) {}
