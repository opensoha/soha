package bootstrap

import (
	"context"
	"fmt"
	"sync"
	"time"

	appmemory "github.com/opensoha/soha/internal/application/memory"
	"github.com/opensoha/soha/internal/infrastructure/hindsight"
	repomemory "github.com/opensoha/soha/internal/repository/memory"
)

type MemoryBackendConfig struct {
	Enabled           bool
	BaseURL           string
	Token             string
	InstanceNamespace string
	Timeout           time.Duration
}

type MemoryBackendRuntime struct {
	backend *appmemory.BackendService
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
}

// ConfigureMemoryBackend performs no I/O and starts no background workers.
func ConfigureMemoryBackend(cfg MemoryBackendConfig, repository *repomemory.Repository, service *appmemory.Service, sources appmemory.SourceValidator) (*MemoryBackendRuntime, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if service == nil {
		return nil, fmt.Errorf("memory service required")
	}
	client, err := hindsight.NewClient(hindsight.Config{BaseURL: cfg.BaseURL, Token: cfg.Token, Timeout: cfg.Timeout}, nil)
	if err != nil {
		return nil, err
	}
	backend, err := appmemory.NewBackendService(repository, repository, client, sources, cfg.InstanceNamespace)
	if err != nil {
		return nil, err
	}
	service.SetBackend(backend)
	return &MemoryBackendRuntime{backend: backend}, nil
}
func (r *MemoryBackendRuntime) Start(ctx context.Context, report func(error)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	workerCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() { defer close(r.done); r.backend.RunSync(workerCtx, report) }()
}
func (r *MemoryBackendRuntime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
