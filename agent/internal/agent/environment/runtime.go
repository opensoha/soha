package environment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	BackendLocalProcess   = "local-process"
	BackendLocalContainer = "local-container"
	BackendKubernetes     = "kubernetes"

	defaultOperationTimeout = 30 * time.Second
	defaultGCBatchLimit     = 32
	maxEnvironmentArgs      = 64
	maxEnvironmentValues    = 128
)

var restrictedNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?$`)

type Spec struct {
	RequireExistingData bool              `json:"requireExistingData,omitempty"`
	Backend             string            `json:"backend"`
	Name                string            `json:"name"`
	Namespace           string            `json:"namespace,omitempty"`
	Image               string            `json:"image,omitempty"`
	Command             string            `json:"command,omitempty"`
	Args                []string          `json:"args,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	Workspace           string            `json:"workspace,omitempty"`
	ArtifactRefs        []string          `json:"artifactRefs,omitempty"`
}

type Handle struct {
	ID      string `json:"id"`
	Backend string `json:"backend"`
	Ref     string `json:"ref,omitempty"`
}

type Backend interface {
	Kind() string
	Prepare(ctx context.Context, lease Lease, spec Spec) (Handle, error)
	Inspect(ctx context.Context, handle Handle) (ResourceUsage, []string, error)
	Release(ctx context.Context, handle Handle) error
}

type LocalRequest struct {
	Lease Lease
	Spec  Spec
}

type LocalExecutor interface {
	Start(ctx context.Context, request LocalRequest) (Handle, error)
	Inspect(ctx context.Context, handle Handle) (ResourceUsage, []string, error)
	Stop(ctx context.Context, handle Handle) error
}

type KubernetesRequest struct {
	Lease Lease
	Spec  Spec
}

type KubernetesSandboxPort interface {
	Create(ctx context.Context, request KubernetesRequest) (Handle, error)
	Inspect(ctx context.Context, handle Handle) (ResourceUsage, []string, error)
	Delete(ctx context.Context, handle Handle) error
}

type LocalBackend struct {
	kind     string
	executor LocalExecutor
}

func NewLocalBackend(kind string, executor LocalExecutor) (*LocalBackend, error) {
	if kind != BackendLocalProcess && kind != BackendLocalContainer {
		return nil, fmt.Errorf("unsupported local environment backend %q", kind)
	}
	if executor == nil {
		return nil, fmt.Errorf("local environment executor is required")
	}
	return &LocalBackend{kind: kind, executor: executor}, nil
}

func (b *LocalBackend) Kind() string { return b.kind }

func (b *LocalBackend) Prepare(ctx context.Context, lease Lease, spec Spec) (Handle, error) {
	return b.executor.Start(ctx, LocalRequest{Lease: lease, Spec: cloneSpec(spec)})
}

func (b *LocalBackend) Inspect(ctx context.Context, handle Handle) (ResourceUsage, []string, error) {
	return b.executor.Inspect(ctx, handle)
}

func (b *LocalBackend) Release(ctx context.Context, handle Handle) error {
	return b.executor.Stop(ctx, handle)
}

type KubernetesBackend struct {
	port KubernetesSandboxPort
}

func NewKubernetesBackend(port KubernetesSandboxPort) (*KubernetesBackend, error) {
	if port == nil {
		return nil, fmt.Errorf("kubernetes sandbox port is required")
	}
	return &KubernetesBackend{port: port}, nil
}

func (b *KubernetesBackend) Kind() string { return BackendKubernetes }

func (b *KubernetesBackend) Prepare(ctx context.Context, lease Lease, spec Spec) (Handle, error) {
	return b.port.Create(ctx, KubernetesRequest{Lease: lease, Spec: cloneSpec(spec)})
}

func (b *KubernetesBackend) Inspect(ctx context.Context, handle Handle) (ResourceUsage, []string, error) {
	return b.port.Inspect(ctx, handle)
}

func (b *KubernetesBackend) Release(ctx context.Context, handle Handle) error {
	return b.port.Delete(ctx, handle)
}

type runtimeEntry struct {
	lease   Lease
	handle  Handle
	backend Backend
}

type RuntimeManager struct {
	mu               sync.RWMutex
	leases           *Manager
	backends         map[string]Backend
	active           map[string]runtimeEntry
	allowedRoot      string
	operationTimeout time.Duration
	gcBatchLimit     int
}

type RuntimeOptions struct {
	AllowedWorkspaceRoot string
	OperationTimeout     time.Duration
	GCBatchLimit         int
}

func NewRuntimeManager(options RuntimeOptions, backends ...Backend) (*RuntimeManager, error) {
	root, err := filepath.Abs(strings.TrimSpace(options.AllowedWorkspaceRoot))
	if err != nil || strings.TrimSpace(options.AllowedWorkspaceRoot) == "" {
		return nil, fmt.Errorf("allowed workspace root is required")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve allowed workspace root: %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return nil, fmt.Errorf("allowed workspace root must be an existing directory")
	}
	timeout := options.OperationTimeout
	if timeout <= 0 {
		timeout = defaultOperationTimeout
	}
	batch := options.GCBatchLimit
	if batch <= 0 {
		batch = defaultGCBatchLimit
	}
	manager := &RuntimeManager{
		leases: NewManager(), backends: make(map[string]Backend), active: make(map[string]runtimeEntry),
		allowedRoot: filepath.Clean(root), operationTimeout: timeout, gcBatchLimit: batch,
	}
	for _, backend := range backends {
		if backend == nil || strings.TrimSpace(backend.Kind()) == "" {
			return nil, fmt.Errorf("environment backend kind is required")
		}
		if _, exists := manager.backends[backend.Kind()]; exists {
			return nil, fmt.Errorf("duplicate environment backend %q", backend.Kind())
		}
		manager.backends[backend.Kind()] = backend
	}
	return manager, nil
}

func (m *RuntimeManager) Acquire(ctx context.Context, lease Lease, spec Spec, now time.Time) (Handle, error) {
	if err := validateSpec(spec, m.allowedRoot); err != nil {
		return Handle{}, err
	}
	backend, ok := m.backends[spec.Backend]
	if !ok {
		return Handle{}, fmt.Errorf("environment backend %q is unavailable", spec.Backend)
	}
	acquired, err := m.leases.Acquire(lease, now)
	if err != nil {
		return Handle{}, err
	}
	opCtx, cancel := context.WithTimeout(ctx, m.operationTimeout)
	handle, err := backend.Prepare(opCtx, acquired, cloneSpec(spec))
	cancel()
	if err != nil {
		m.leases.remove(acquired.ID)
		return Handle{}, fmt.Errorf("prepare environment backend: %w", err)
	}
	if strings.TrimSpace(handle.ID) == "" {
		m.leases.remove(acquired.ID)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), m.operationTimeout)
		_ = backend.Release(cleanupCtx, handle)
		cleanupCancel()
		return Handle{}, fmt.Errorf("environment backend returned an empty handle id")
	}
	handle.Backend = backend.Kind()
	m.mu.Lock()
	m.active[acquired.ID] = runtimeEntry{lease: acquired, handle: handle, backend: backend}
	m.mu.Unlock()
	return handle, nil
}

func (m *RuntimeManager) Inspect(ctx context.Context, leaseID string, now time.Time) (Snapshot, error) {
	entry, ok := m.entry(leaseID)
	if !ok {
		return Snapshot{}, ErrLeaseNotFound
	}
	opCtx, cancel := context.WithTimeout(ctx, m.operationTimeout)
	usage, refs, err := entry.backend.Inspect(opCtx, entry.handle)
	cancel()
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect environment backend: %w", err)
	}
	return m.leases.Snapshot(leaseID, usage, refs, now)
}

func (m *RuntimeManager) Release(ctx context.Context, leaseID string, now time.Time) (Lease, error) {
	entry, ok := m.entry(leaseID)
	if !ok {
		lease, err := m.leases.Get(leaseID)
		if err != nil {
			return Lease{}, err
		}
		return lease, nil
	}
	opCtx, cancel := context.WithTimeout(ctx, m.operationTimeout)
	err := entry.backend.Release(opCtx, entry.handle)
	cancel()
	if err != nil {
		return Lease{}, fmt.Errorf("release environment backend: %w", err)
	}
	lease, err := m.leases.Release(leaseID, now)
	if err != nil {
		return Lease{}, err
	}
	m.mu.Lock()
	delete(m.active, leaseID)
	m.mu.Unlock()
	return lease, nil
}

func (m *RuntimeManager) GC(ctx context.Context, now time.Time) ([]Lease, error) {
	entries := m.gcCandidates(now)
	released := make([]Lease, 0, len(entries))
	var errs []error
	for _, entry := range entries {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		lease, err := m.Release(ctx, entry.lease.ID, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("gc lease %q: %w", entry.lease.ID, err))
			continue
		}
		m.leases.remove(lease.ID)
		released = append(released, lease)
	}
	sort.Slice(released, func(i, j int) bool { return released[i].ID < released[j].ID })
	return released, errors.Join(errs...)
}

func (m *RuntimeManager) entry(leaseID string) (runtimeEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.active[leaseID]
	return entry, ok
}

func (m *RuntimeManager) gcCandidates(now time.Time) []runtimeEntry {
	m.mu.RLock()
	entries := make([]runtimeEntry, 0, len(m.active))
	for _, entry := range m.active {
		if !now.Before(entry.lease.ExpiresAt) || !entry.lease.ReleasedAt.IsZero() {
			entries = append(entries, entry)
		}
	}
	m.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].lease.ID < entries[j].lease.ID })
	if len(entries) > m.gcBatchLimit {
		entries = entries[:m.gcBatchLimit]
	}
	return entries
}

func validateSpec(spec Spec, allowedRoot string) error {
	if spec.Backend != BackendLocalProcess && spec.Backend != BackendLocalContainer && spec.Backend != BackendKubernetes {
		return fmt.Errorf("unsupported environment backend %q", spec.Backend)
	}
	if !restrictedNamePattern.MatchString(strings.TrimSpace(spec.Name)) {
		return fmt.Errorf("environment name is invalid")
	}
	if len(spec.Args) > maxEnvironmentArgs || len(spec.Env) > maxEnvironmentValues || len(spec.ArtifactRefs) > maxEnvironmentValues {
		return fmt.Errorf("environment inputs exceed bounded limits")
	}
	if err := validateWorkspace(spec.Workspace, allowedRoot); err != nil {
		return err
	}
	if spec.Backend == BackendLocalProcess && strings.TrimSpace(spec.Command) == "" {
		return fmt.Errorf("local process command is required")
	}
	if spec.Backend != BackendLocalProcess && !validImage(spec.Image) {
		return fmt.Errorf("container image is invalid")
	}
	if spec.Backend == BackendKubernetes && !restrictedNamePattern.MatchString(strings.TrimSpace(spec.Namespace)) {
		return fmt.Errorf("kubernetes namespace is invalid")
	}
	for key, value := range spec.Env {
		if strings.TrimSpace(key) == "" || len(key) > 128 || len(value) > 4096 {
			return fmt.Errorf("environment variable input is invalid")
		}
	}
	for _, arg := range spec.Args {
		if len(arg) > 4096 {
			return fmt.Errorf("environment argument exceeds the size limit")
		}
	}
	return nil
}

func validateWorkspace(workspace, allowedRoot string) error {
	if strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("environment workspace is required")
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve environment workspace: %w", err)
	}
	canonical, err := canonicalWorkspacePath(filepath.Clean(abs))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(allowedRoot, canonical)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("environment workspace must remain under the allowed root")
	}
	current := allowedRoot
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return fmt.Errorf("inspect environment workspace path: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("environment workspace cannot contain symbolic links")
		}
	}
	return nil
}

func canonicalWorkspacePath(candidate string) (string, error) {
	current := candidate
	missing := make([]string, 0)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect environment workspace path: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve environment workspace path")
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", fmt.Errorf("resolve environment workspace path: %w", err)
	}
	for index := len(missing) - 1; index >= 0; index-- {
		resolved = filepath.Join(resolved, missing[index])
	}
	return filepath.Clean(resolved), nil
}

func validImage(image string) bool {
	image = strings.TrimSpace(image)
	return image != "" && len(image) <= 512 && !strings.ContainsAny(image, " \t\r\n")
}

func cloneSpec(input Spec) Spec {
	out := input
	out.Args = append([]string(nil), input.Args...)
	out.ArtifactRefs = append([]string(nil), input.ArtifactRefs...)
	out.Env = make(map[string]string, len(input.Env))
	for key, value := range input.Env {
		out.Env[key] = value
	}
	return out
}
