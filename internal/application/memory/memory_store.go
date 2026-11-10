package memory

import (
	"context"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu       sync.RWMutex
	records  map[string]Record
	policies map[string]Policy
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string]Record{}, policies: map[string]Policy{}}
}

func (s *MemoryStore) PutRecord(_ context.Context, record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.records[record.ID]; ok && (existing.OwnerType != record.OwnerType || existing.OwnerID != record.OwnerID || existing.ScopeHash != record.ScopeHash || existing.Status == "deleted") {
		return apperrors.ErrAccessDenied
	}
	s.records[record.ID] = cloneRecord(record)
	return nil
}

func (s *MemoryStore) ListRecords(_ context.Context, ownerType, ownerID string, now time.Time) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Record, 0)
	for _, record := range s.records {
		if record.OwnerType != ownerType || record.OwnerID != ownerID || record.Status != "active" || record.ExpiresAt == nil || !record.ExpiresAt.After(now) {
			continue
		}
		items = append(items, cloneRecord(record))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *MemoryStore) GetRecord(_ context.Context, id string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return cloneRecord(record), nil
}

func (s *MemoryStore) DeleteRecord(_ context.Context, id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return ErrNotFound
	}
	record.Status = "deleted"
	record.DeletedAt = &now
	s.records[id] = record
	return nil
}

func (s *MemoryStore) PutPolicy(_ context.Context, policy Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy = clonePolicy(policy)
	s.policies[policy.ID+":"+policy.Version] = policy
	return nil
}

func (s *MemoryStore) GetPolicy(_ context.Context, id, version string) (Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	policy, ok := s.policies[id+":"+version]
	if !ok {
		return Policy{}, ErrNotFound
	}
	policy = clonePolicy(policy)
	return clonePolicy(policy), nil
}

func (s *MemoryStore) ListPolicies(context.Context) ([]Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		policy = clonePolicy(policy)
		items = append(items, policy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID+items[i].Version < items[j].ID+items[j].Version })
	return items, nil
}

func (s *MemoryStore) ListRecordStates(ctx context.Context, ownerType, owner string, now time.Time, includeDeleted bool) ([]Record, error) {
	if !includeDeleted {
		return s.ListRecords(ctx, ownerType, owner, now)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Record{}
	for _, r := range s.records {
		if r.OwnerType == ownerType && r.OwnerID == owner {
			out = append(out, cloneRecord(r))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
