package aiproduction

import (
	"context"
	"testing"
	"time"
)

type memoryStore struct {
	rollouts    map[string]ProviderRollout
	conformance []ConformanceRun
	templates   []EnvironmentTemplate
	leases      map[string]EnvironmentLease
	operations  []Operation
}

func newMemoryStore() *memoryStore {
	return &memoryStore{rollouts: map[string]ProviderRollout{}, leases: map[string]EnvironmentLease{}}
}
func (s *memoryStore) ListRollouts(context.Context) ([]ProviderRollout, error) {
	out := make([]ProviderRollout, 0, len(s.rollouts))
	for _, v := range s.rollouts {
		out = append(out, v)
	}
	return out, nil
}
func (s *memoryStore) PutRollout(_ context.Context, v ProviderRollout) error {
	s.rollouts[v.ID] = v
	return nil
}
func (s *memoryStore) GetRollout(_ context.Context, id string) (ProviderRollout, error) {
	v, ok := s.rollouts[id]
	if !ok {
		return ProviderRollout{}, ErrNotFound
	}
	return v, nil
}
func (s *memoryStore) ListConformanceRuns(context.Context) ([]ConformanceRun, error) {
	return s.conformance, nil
}
func (s *memoryStore) PutConformanceRun(_ context.Context, v ConformanceRun) error {
	s.conformance = append(s.conformance, v)
	return nil
}
func (s *memoryStore) ListEnvironmentTemplates(context.Context) ([]EnvironmentTemplate, error) {
	return s.templates, nil
}
func (s *memoryStore) PutEnvironmentTemplate(_ context.Context, v EnvironmentTemplate) error {
	s.templates = append(s.templates, v)
	return nil
}
func (s *memoryStore) ListEnvironmentLeases(context.Context) ([]EnvironmentLease, error) {
	out := make([]EnvironmentLease, 0, len(s.leases))
	for _, v := range s.leases {
		out = append(out, v)
	}
	return out, nil
}
func (s *memoryStore) GetEnvironmentLease(_ context.Context, id string) (EnvironmentLease, error) {
	v, ok := s.leases[id]
	if !ok {
		return EnvironmentLease{}, ErrNotFound
	}
	return v, nil
}
func (s *memoryStore) PutEnvironmentLease(_ context.Context, v EnvironmentLease) error {
	s.leases[v.ID] = v
	return nil
}
func (s *memoryStore) ListOperations(context.Context) ([]Operation, error) { return s.operations, nil }
func (s *memoryStore) PutOperation(_ context.Context, v Operation) error {
	s.operations = append(s.operations, v)
	return nil
}
func (s *memoryStore) ListRunbookEvidence(context.Context) ([]RunbookEvidence, error) {
	return []RunbookEvidence{}, nil
}

func TestRolloutTransitionsUseActions(t *testing.T) {
	store := newMemoryStore()
	service, _ := New(store)
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	created, err := service.CreateRollout(t.Context(), ProviderRollout{ID: "rollout-1", DesiredRevision: 2, PreviousRevision: 1, CanaryPercent: 10, Target: FleetTarget{Environments: []string{"prod"}}})
	if err != nil || created.Status != "validating" {
		t.Fatalf("create=%#v err=%v", created, err)
	}
	canary, err := service.TransitionRollout(t.Context(), created.ID, "resume")
	if err != nil || canary.Status != "canary" {
		t.Fatalf("resume=%#v err=%v", canary, err)
	}
	rolledBack, err := service.TransitionRollout(t.Context(), created.ID, "rollback")
	if err != nil || rolledBack.Status != "rolled_back" {
		t.Fatalf("rollback=%#v err=%v", rolledBack, err)
	}
	if _, err := service.TransitionRollout(t.Context(), created.ID, "resume"); err == nil {
		t.Fatal("terminal rollout accepted resume")
	}
}

func TestEnvironmentGCExpiresOnlyElapsedActiveLeases(t *testing.T) {
	store := newMemoryStore()
	service, _ := New(store)
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	store.leases["old"] = EnvironmentLease{ID: "old", TemplateID: "t", Status: "active", ExpiresAt: &past, CreatedAt: now}
	store.leases["new"] = EnvironmentLease{ID: "new", TemplateID: "t", Status: "active", ExpiresAt: &future, CreatedAt: now}
	op, err := service.GCEnvironmentLeases(t.Context())
	if err != nil || op.Status != "completed" || store.leases["old"].Status != "expired" || store.leases["new"].Status != "active" {
		t.Fatalf("op=%#v leases=%#v err=%v", op, store.leases, err)
	}
}

func TestProductionOperationRequiresRunbookAndKnownKind(t *testing.T) {
	service, _ := New(newMemoryStore())
	if _, err := service.StartOperation(t.Context(), Operation{Kind: "restore", TargetRef: "knowledge:kb-1"}); err == nil {
		t.Fatal("operation without runbook accepted")
	}
	item, err := service.StartOperation(t.Context(), Operation{Kind: "index_rebuild", TargetRef: "knowledge:kb-1", RunbookID: "rag-index-rebuild"})
	if err != nil || item.Category != "recovery" || item.Status != "queued" {
		t.Fatalf("operation=%#v err=%v", item, err)
	}
}
