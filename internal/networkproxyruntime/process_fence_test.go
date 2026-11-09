//go:build linux || darwin

package networkproxyruntime

import (
	"bufio"
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestOwnedStopRequiresProcessGroupGone(t *testing.T) {
	t.Run("released anchor cannot signal", testReleasedProcessGroup)
	t.Run("live anchor reaps TERM-resistant member", testAnchoredProcessGroup)
}

func startTestProcessGroup(t *testing.T) (leader, worker *exec.Cmd) {
	t.Helper()
	leader = exec.Command("/bin/sleep", "30")
	// Both children are directly owned by the test, so it can reap both.
	leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := leader.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = leader.Process.Kill() })
	worker = exec.Command("/bin/sh", "-c", `trap '' TERM; echo ready; exec /bin/sleep 30`)
	worker.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: leader.Process.Pid}
	pipe, err := worker.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Process.Kill() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(pipe).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "ready\n" {
			t.Fatal("worker never entered the owned process group")
		}
	case <-time.After(time.Second):
		t.Fatal("worker readiness timed out")
	}
	return leader, worker
}

func testReleasedProcessGroup(t *testing.T) {
	leader, worker := startTestProcessGroup(t)
	defer worker.Wait()
	defer worker.Process.Kill()
	if err := leader.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = leader.Wait()
	e := &engine{process: leader, processFence: &ownedProcessFence{command: leader, reaping: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := e.stopOwned(ctx); err == nil || e.process != leader {
		t.Fatal("leader-only exit became successful stop or lost the proof target")
	}
	if err := worker.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("stop signalled a process group after releasing its identity anchor", err)
	}
	if err := worker.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = worker.Wait()
	confirm, confirmCancel := context.WithTimeout(context.Background(), time.Second)
	defer confirmCancel()
	if err := e.stopOwned(confirm); err != nil || e.process != nil {
		t.Fatal("empty owned process group did not confirm stop", err)
	}
}

func testAnchoredProcessGroup(t *testing.T) {
	leader, worker := startTestProcessGroup(t)
	fence := &ownedProcessFence{command: leader}
	wait := make(chan error, 1)
	go func() { wait <- fence.wait() }()
	workerDone := make(chan error, 1)
	go func() { workerDone <- worker.Wait() }()
	e := &engine{process: leader, processFence: fence, wait: wait}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := e.stopOwned(ctx); err != nil || e.process != nil {
		t.Fatal("owned group did not stop", err)
	}
	select {
	case err := <-workerDone:
		if err == nil {
			t.Fatal("fixture worker did not receive a terminating signal")
		}
	case <-ctx.Done():
		t.Fatal("worker survived group stop")
	}
}
