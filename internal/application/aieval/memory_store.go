package aieval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MemoryStore is intended for unit tests and local composition without a database.
type MemoryStore struct {
	mu       sync.RWMutex
	datasets map[string]Dataset
	runs     map[string]Run
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{datasets: map[string]Dataset{}, runs: map[string]Run{}}
}

func (s *MemoryStore) CreateDataset(_ context.Context, dataset Dataset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := datasetKey(dataset.ID, dataset.Version)
	if _, exists := s.datasets[key]; exists {
		return fmt.Errorf("%w: evaluation dataset %s already exists", ErrConflict, key)
	}
	s.datasets[key] = cloneDataset(dataset)
	return nil
}

func (s *MemoryStore) ListDatasets(_ context.Context) ([]Dataset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Dataset, 0, len(s.datasets))
	for _, dataset := range s.datasets {
		items = append(items, cloneDataset(dataset))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			if items[i].ID == items[j].ID {
				return items[i].Version < items[j].Version
			}
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (s *MemoryStore) GetDataset(_ context.Context, id, version string) (Dataset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dataset, exists := s.datasets[datasetKey(id, version)]
	if !exists {
		return Dataset{}, fmt.Errorf("%w: evaluation dataset %s@%s", ErrNotFound, id, version)
	}
	return cloneDataset(dataset), nil
}

func (s *MemoryStore) CreateRun(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[run.ID]; exists {
		return fmt.Errorf("%w: evaluation run %q already exists", ErrConflict, run.ID)
	}
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *MemoryStore) ListRuns(_ context.Context) ([]Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		items = append(items, cloneRun(run))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartedAt.Equal(items[j].StartedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	return items, nil
}

func (s *MemoryStore) GetRun(_ context.Context, id string) (Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, exists := s.runs[strings.TrimSpace(id)]
	if !exists {
		return Run{}, fmt.Errorf("%w: evaluation run %q", ErrNotFound, id)
	}
	return cloneRun(run), nil
}

func (s *MemoryStore) CompleteRun(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.runs[run.ID]
	if !exists {
		return fmt.Errorf("%w: evaluation run %q", ErrNotFound, run.ID)
	}
	if current.Status != "running" {
		return fmt.Errorf("%w: evaluation run %q is terminal", ErrConflict, run.ID)
	}
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func datasetKey(id, version string) string {
	return strings.TrimSpace(id) + "@" + strings.TrimSpace(version)
}

func cloneDatasets(items []Dataset) []Dataset {
	out := make([]Dataset, len(items))
	for index := range items {
		out[index] = cloneDataset(items[index])
	}
	return out
}

func cloneRuns(items []Run) []Run {
	out := make([]Run, len(items))
	for index := range items {
		out[index] = cloneRun(items[index])
	}
	return out
}
