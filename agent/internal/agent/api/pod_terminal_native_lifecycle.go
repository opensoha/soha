package api

import (
	"context"
	"sync"
)

// Native PTYs are hijacked HTTP connections. They need an explicit owner:
// http.Server.Shutdown alone neither cancels nor waits for these sessions.
type nativePodTerminals struct {
	mu     sync.Mutex
	closed bool
	next   uint64
	active map[uint64]context.CancelFunc
	wg     sync.WaitGroup
}

func newNativePodTerminals() *nativePodTerminals {
	return &nativePodTerminals{active: map[uint64]context.CancelFunc{}}
}

func (registry *nativePodTerminals) acquire(ctx context.Context) (context.Context, func(), bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.closed || len(registry.active) >= 64 {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(ctx)
	registry.next++
	id := registry.next
	registry.active[id] = cancel
	registry.wg.Add(1)
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			registry.mu.Lock()
			delete(registry.active, id)
			registry.mu.Unlock()
			registry.wg.Done()
		})
	}
	return ctx, release, true
}

func (registry *nativePodTerminals) shutdown(ctx context.Context) error {
	registry.mu.Lock()
	registry.closed = true
	for _, cancel := range registry.active {
		cancel()
	}
	registry.mu.Unlock()
	done := make(chan struct{})
	go func() { registry.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
