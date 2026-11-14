package environment

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	contractenv "github.com/opensoha/soha/contracts/agent/environment"
)

type PersonalMCPStdioCommand struct {
	Run   func(context.Context, *os.File, io.Writer) PersonalMCPStdioStatus
	Abort func()
}

type PersonalMCPStdioStatus string

const (
	PersonalMCPStdioClosed      PersonalMCPStdioStatus = "closed"
	PersonalMCPStdioStopped     PersonalMCPStdioStatus = "stopped"
	PersonalMCPStdioUnavailable PersonalMCPStdioStatus = "unavailable"
	PersonalMCPStdioUnknown     PersonalMCPStdioStatus = "unknown"
)

// ReservePersonalMCPStdio takes the existing Docker command fence before
// upgrade. Only Run can dispatch a command; Abort releases an unused fence.
func (e *DockerExecutor) ReservePersonalMCPStdio(ctx context.Context, original contractenv.Lease, argv []string) (PersonalMCPStdioCommand, error) {
	var command PersonalMCPStdioCommand
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "execute", Lease: original, Argv: argv}
	if request.Validate() != nil {
		return command, errors.New("invalid personal MCP stdio command")
	}
	ctx, cancel := context.WithDeadline(ctx, original.ExpiresAt)
	defer cancel()
	lease := personalDockerLease(original)
	admit, stopAdmission := context.WithTimeout(ctx, defaultOperationTimeout)
	handle, found, err := e.personalHandle(admit, lease)
	stopAdmission()
	if err != nil {
		return command, err
	}
	if !found {
		return command, ErrLeaseNotFound
	}
	fence, err := e.acquireCommandFence(ctx, handle)
	if err != nil {
		return command, err
	}
	var consumed atomic.Bool
	command.Abort = func() {
		if consumed.CompareAndSwap(false, true) {
			cleanup, stopCleanup := context.WithTimeout(context.Background(), defaultOperationTimeout)
			defer stopCleanup()
			_ = e.removeCommandFence(cleanup, fence)
		}
	}
	command.Run = func(ctx context.Context, stdin *os.File, stdout io.Writer) PersonalMCPStdioStatus {
		if stdin == nil || stdout == nil || !consumed.CompareAndSwap(false, true) {
			return PersonalMCPStdioUnknown
		}
		return e.runPersonalMCPStdio(ctx, lease, fence, argv, stdin, stdout)
	}
	return command, nil
}

func (e *DockerExecutor) runPersonalMCPStdio(ctx context.Context, lease Lease, fence dockerCommandFence, argv []string, stdin *os.File, stdout io.Writer) (status PersonalMCPStdioStatus) {
	ctx, cancel := context.WithDeadline(ctx, lease.ExpiresAt)
	defer cancel()
	dispatchAttempted, supervisorOnly := false, false
	defer func() {
		status = e.finishPersonalMCPStdio(fence, dispatchAttempted, supervisorOnly)
	}()
	handle := fence.Parent
	admit, stopAdmission := context.WithTimeout(ctx, defaultOperationTimeout)
	defer stopAdmission()
	if _, err := e.activeLease(admit, handle); err != nil {
		return
	}
	if _, err := e.volumeUsage(admit, lease); err != nil {
		return
	}
	if err := e.requireOnlySupervisor(admit, handle); err != nil {
		return
	}
	workspace, err := e.taskWorkspace(admit, handle, fence.CallID)
	if err != nil {
		return
	}
	stopAdmission()
	args := append([]string{"exec", "--interactive", "--user", "1000:1000", "--workdir", workspace, handle.ID}, argv...)
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // Only fixed Docker options and operator-selected sandbox argv are used.
	cmd.Stdin, cmd.Stdout, cmd.WaitDelay = stdin, stdout, time.Second
	dispatchAttempted = true
	if err := cmd.Run(); err != nil {
		return
	}
	verify, stopVerify := context.WithTimeout(ctx, defaultOperationTimeout)
	defer stopVerify()
	supervisorOnly = e.requireOnlySupervisor(verify, handle) == nil
	return
}

func (e *DockerExecutor) finishPersonalMCPStdio(fence dockerCommandFence, dispatchAttempted, supervisorOnly bool) PersonalMCPStdioStatus {
	cleanup, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
	defer cancel()
	if !dispatchAttempted || supervisorOnly {
		// A lost remove receipt may mean another command already owns the fence.
		if e.removeCommandFence(cleanup, fence) != nil {
			return PersonalMCPStdioUnknown
		}
		if !dispatchAttempted {
			return PersonalMCPStdioUnavailable
		}
		return PersonalMCPStdioClosed
	}
	// A lost CLI or child receipt leaves the durable fence in place until the
	// exact parent stops. Recovery can retry that cleanup after expiration.
	if e.Stop(cleanup, fence.Parent) != nil {
		return PersonalMCPStdioUnknown
	}
	return PersonalMCPStdioStopped
}
