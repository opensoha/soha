package workflow

import (
	"context"
	"sync"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type runStateStore struct {
	mu        sync.Mutex
	snapshots map[string]domainworkflow.Run
	waiters   map[string]map[chan struct{}]struct{}
}

func (s *runStateStore) publish(run domainworkflow.Run) {
	snapshot := cloneRunForAsyncWorker(run)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshots == nil {
		s.snapshots = make(map[string]domainworkflow.Run)
	}
	s.snapshots[run.ID] = snapshot
	for waiter := range s.waiters[run.ID] {
		select {
		case waiter <- struct{}{}:
		default:
		}
	}
}

func (s *runStateStore) latest(runID string) (domainworkflow.Run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.snapshots[runID]
	if !ok {
		return domainworkflow.Run{}, false
	}
	return cloneRunForAsyncWorker(run), true
}

func (s *runStateStore) register(runID string) chan struct{} {
	waiter := make(chan struct{}, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiters == nil {
		s.waiters = make(map[string]map[chan struct{}]struct{})
	}
	if s.waiters[runID] == nil {
		s.waiters[runID] = make(map[chan struct{}]struct{})
	}
	s.waiters[runID][waiter] = struct{}{}
	return waiter
}

func (s *runStateStore) unregister(runID string, waiter chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	waiters := s.waiters[runID]
	if waiters == nil {
		return
	}
	delete(waiters, waiter)
	if len(waiters) == 0 {
		delete(s.waiters, runID)
	}
}

func (s *runStateStore) wait(ctx context.Context, runID string, statuses ...string) (domainworkflow.Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	allowed := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	waiter := s.register(runID)
	defer s.unregister(runID, waiter)
	for {
		if run, ok := s.latest(runID); ok {
			if len(allowed) == 0 {
				return run, nil
			}
			if _, matched := allowed[run.Status]; matched {
				return run, nil
			}
		}
		select {
		case <-waiter:
		case <-ctx.Done():
			return domainworkflow.Run{}, ctx.Err()
		}
	}
}
