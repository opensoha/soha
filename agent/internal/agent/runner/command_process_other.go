//go:build !unix

package runner

import "os/exec"

func configureCommandCancellation(_ *exec.Cmd) {}
