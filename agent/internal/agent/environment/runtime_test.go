package environment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeLocalExecutor struct {
	mu          sync.Mutex
	starts      int
	stops       int
	inspect     ResourceUsage
	artifactRef []string
	start       func(context.Context, LocalRequest) (Handle, error)
}

func (f *fakeLocalExecutor) Start(ctx context.Context, request LocalRequest) (Handle, error) {
	f.mu.Lock()
	f.starts++
	f.mu.Unlock()
	if f.start != nil {
		return f.start(ctx, request)
	}
	return Handle{ID: request.Lease.ID + "-handle"}, nil
}

func (f *fakeLocalExecutor) Inspect(context.Context, Handle) (ResourceUsage, []string, error) {
	return f.inspect, append([]string(nil), f.artifactRef...), nil
}

func (f *fakeLocalExecutor) Stop(context.Context, Handle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}

type fakeKubernetesPort struct{ created bool }

func (f *fakeKubernetesPort) Create(_ context.Context, request KubernetesRequest) (Handle, error) {
	f.created = true
	return Handle{ID: request.Lease.ID + "-pod", Ref: request.Spec.Namespace + "/" + request.Spec.Name}, nil
}

func (*fakeKubernetesPort) Inspect(context.Context, Handle) (ResourceUsage, []string, error) {
	return ResourceUsage{}, nil, nil
}

func (*fakeKubernetesPort) Delete(context.Context, Handle) error { return nil }

func TestRuntimeManagerLocalLifecycleAndIdempotentRelease(t *testing.T) {
	root := t.TempDir()
	executor := &fakeLocalExecutor{inspect: ResourceUsage{DiskBytes: 128}, artifactRef: []string{"artifact:one"}}
	backend, err := NewLocalBackend(BackendLocalProcess, executor)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewRuntimeManager(RuntimeOptions{AllowedWorkspaceRoot: root}, backend)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease := validTestLease(now, "lease-1")
	handle, err := manager.Acquire(t.Context(), lease, Spec{
		Backend: BackendLocalProcess, Name: "run-one", Command: "codex",
		Args: []string{"exec", "task"}, Workspace: filepath.Join(root, "run-one"),
	}, now)
	if err != nil || handle.Backend != BackendLocalProcess {
		t.Fatalf("Acquire() handle = %#v, error = %v", handle, err)
	}
	snapshot, err := manager.Inspect(t.Context(), lease.ID, now)
	if err != nil || snapshot.ResourceUsage.DiskBytes != 128 || len(snapshot.ArtifactRefs) != 1 {
		t.Fatalf("Inspect() snapshot = %#v, error = %v", snapshot, err)
	}
	first, err := manager.Release(t.Context(), lease.ID, now)
	if err != nil || first.ReleasedAt.IsZero() {
		t.Fatalf("Release() lease = %#v, error = %v", first, err)
	}
	second, err := manager.Release(t.Context(), lease.ID, now.Add(time.Second))
	if err != nil || second.ReleasedAt != first.ReleasedAt || executor.stops != 1 {
		t.Fatalf("idempotent Release() lease = %#v, stops = %d, error = %v", second, executor.stops, err)
	}
}

func TestRuntimeManagerRejectsWorkspaceEscapeBeforeBackendCall(t *testing.T) {
	root := t.TempDir()
	executor := &fakeLocalExecutor{}
	backend, _ := NewLocalBackend(BackendLocalProcess, executor)
	manager, err := NewRuntimeManager(RuntimeOptions{AllowedWorkspaceRoot: root}, backend)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = manager.Acquire(t.Context(), validTestLease(now, "lease-escape"), Spec{
		Backend: BackendLocalProcess, Name: "escape", Command: "codex",
		Workspace: filepath.Join(root, "..", "outside"),
	}, now)
	if err == nil || executor.starts != 0 {
		t.Fatalf("Acquire() error = %v, starts = %d", err, executor.starts)
	}
}

func TestRuntimeManagerRejectsWorkspaceSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside-link")); err != nil {
		t.Fatal(err)
	}
	executor := &fakeLocalExecutor{}
	backend, _ := NewLocalBackend(BackendLocalProcess, executor)
	manager, err := NewRuntimeManager(RuntimeOptions{AllowedWorkspaceRoot: root}, backend)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = manager.Acquire(t.Context(), validTestLease(now, "lease-symlink"), Spec{
		Backend: BackendLocalProcess, Name: "symlink", Command: "codex",
		Workspace: filepath.Join(root, "outside-link", "run"),
	}, now)
	if err == nil || (!strings.Contains(err.Error(), "symbolic links") && !strings.Contains(err.Error(), "allowed root")) || executor.starts != 0 {
		t.Fatalf("Acquire() error = %v, starts = %d", err, executor.starts)
	}
}

func TestRuntimeManagerBoundsBackendOperationTimeout(t *testing.T) {
	root := t.TempDir()
	executor := &fakeLocalExecutor{start: func(ctx context.Context, _ LocalRequest) (Handle, error) {
		<-ctx.Done()
		return Handle{}, ctx.Err()
	}}
	backend, _ := NewLocalBackend(BackendLocalProcess, executor)
	manager, err := NewRuntimeManager(RuntimeOptions{AllowedWorkspaceRoot: root, OperationTimeout: 20 * time.Millisecond}, backend)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = manager.Acquire(t.Context(), validTestLease(now, "lease-timeout"), Spec{
		Backend: BackendLocalProcess, Name: "timeout", Command: "codex", Workspace: filepath.Join(root, "timeout"),
	}, now)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire() error = %v, want context deadline", err)
	}
}

func TestRuntimeManagerGCBatchLimitAndKubernetesAdapter(t *testing.T) {
	root := t.TempDir()
	port := &fakeKubernetesPort{}
	backend, err := NewKubernetesBackend(port)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewRuntimeManager(RuntimeOptions{AllowedWorkspaceRoot: root, GCBatchLimit: 1}, backend)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, id := range []string{"lease-a", "lease-b"} {
		lease := validTestLease(now, id)
		lease.ExpiresAt = now.Add(time.Millisecond)
		_, err = manager.Acquire(t.Context(), lease, Spec{
			Backend: BackendKubernetes, Name: id, Namespace: "agents", Image: "ghcr.io/opensoha/agent:v1",
			Workspace: filepath.Join(root, id),
		}, now)
		if err != nil {
			t.Fatalf("Acquire(%s) error = %v", id, err)
		}
	}
	released, err := manager.GC(t.Context(), now.Add(time.Second))
	if err != nil || len(released) != 1 || released[0].ID != "lease-a" || !port.created {
		t.Fatalf("GC() = %#v, created = %v, error = %v", released, port.created, err)
	}
}

func validTestLease(now time.Time, id string) Lease {
	return Lease{
		ID: id, RunID: "run-1", OwnerID: "runner-1", ScopeHash: "sha256:scope", Mode: "restricted-write",
		ExpiresAt: now.Add(time.Minute), Quota: Quota{DiskBytes: 1024, MemoryBytes: 2048, CPUUnits: 100, MaxProcesses: 2},
	}
}
