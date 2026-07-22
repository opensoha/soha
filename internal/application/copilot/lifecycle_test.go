package copilot

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLifecycleIsIdempotentAndRestartable(t *testing.T) {
	service := &Service{}
	ctx, cancel := context.WithCancel(context.Background())

	service.Start(ctx)
	service.Start(ctx)
	if !service.Running() {
		t.Fatal("Running() = false after Start()")
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if service.Running() {
		t.Fatal("Running() = true after Stop()")
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}

	service.Start(ctx)
	if !service.Running() {
		t.Fatal("Running() = false after restart")
	}
	cancel()
	waitForCopilotRunning(t, service, false)
}

func TestLifecycleConcurrentStartStop(t *testing.T) {
	service := &Service{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls sync.WaitGroup
	for i := 0; i < 64; i++ {
		calls.Add(1)
		go func(start bool) {
			defer calls.Done()
			if start {
				service.Start(ctx)
				return
			}
			_ = service.Stop(context.Background())
		}(i%2 == 0)
	}
	calls.Wait()
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("final Stop() error = %v", err)
	}
	if service.Running() {
		t.Fatal("Running() = true after final Stop()")
	}
}

func waitForCopilotRunning(t *testing.T, service *Service, want bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for service.Running() != want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := service.Running(); got != want {
		t.Fatalf("Running() = %v, want %v", got, want)
	}
}
