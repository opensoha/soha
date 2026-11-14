package networkproxyruntime

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// Wait is the sole reaper. It closes the group-signal fence before reaping the
// leader, while its PID still anchors the private session group against reuse.
type ownedProcessFence struct {
	command *exec.Cmd
	mu      sync.Mutex
	reaping bool
}

func (f *ownedProcessFence) signal(force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reaping {
		return nil
	}
	return signalAnchoredGroup(f.command, force)
}

func (f *ownedProcessFence) wait() error {
	exitErr := waitOwnedProcessExit(f.command.Process.Pid)
	f.mu.Lock()
	// A leader exiting on TERM does not prove its workers also stopped.
	// If the platform cannot monitor exit, terminate while still anchored.
	_ = signalAnchoredGroup(f.command, true)
	f.reaping = true
	f.mu.Unlock()
	return errors.Join(exitErr, f.command.Wait())
}

func awaitOwnedGroup(ctx context.Context, command *exec.Cmd) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		absent, err := ownedProcessGroupAbsent(command)
		if absent || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(errors.New("owned process group still present"), ctx.Err())
		case <-ticker.C:
		}
	}
}

func runOwnedCommand(ctx context.Context, command *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	fence := &ownedProcessFence{command: command}
	done := make(chan error, 1)
	go func() { done <- fence.wait() }()
	select {
	case err := <-done:
		if err != nil {
			return err
		}
		return awaitOwnedGroup(ctx, command)
	case <-ctx.Done():
		_ = fence.signal(true)
		<-done
		return ctx.Err()
	}
}
