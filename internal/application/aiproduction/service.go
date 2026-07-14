package aiproduction

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	appaieval "github.com/opensoha/soha/internal/application/aieval"
)

var (
	ErrNotFound = errors.New("AI production resource not found")
	ErrConflict = errors.New("AI production resource conflict")
)

type FleetTarget struct {
	Environments  []string          `json:"environments,omitempty"`
	Platforms     []string          `json:"platforms,omitempty"`
	Architectures []string          `json:"architectures,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type ProviderRollout struct {
	ID               string      `json:"id"`
	Name             string      `json:"name,omitempty"`
	DesiredRevision  uint64      `json:"desiredRevision"`
	PreviousRevision uint64      `json:"previousRevision"`
	Target           FleetTarget `json:"target"`
	CanaryPercent    int         `json:"canaryPercent"`
	Status           string      `json:"status"`
	CreatedAt        time.Time   `json:"createdAt"`
	UpdatedAt        time.Time   `json:"updatedAt"`
}

type ConformanceRun struct {
	ID             string           `json:"id"`
	ProviderRef    string           `json:"providerRef"`
	EnvironmentRef string           `json:"environmentRef"`
	SuiteVersion   string           `json:"suiteVersion"`
	Status         string           `json:"status"`
	Results        []map[string]any `json:"results,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

type EnvironmentTemplate struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Backend        string         `json:"backend"`
	IsolationMode  string         `json:"isolationMode"`
	ResourcePolicy map[string]any `json:"resourcePolicy,omitempty"`
	NetworkPolicy  map[string]any `json:"networkPolicy,omitempty"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type EnvironmentLease struct {
	ID         string     `json:"id"`
	TemplateID string     `json:"templateId"`
	OwnerRef   string     `json:"ownerRef"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type Operation struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Category     string    `json:"category"`
	TargetRef    string    `json:"targetRef"`
	RunbookID    string    `json:"runbookId"`
	Status       string    `json:"status"`
	EvidenceRefs []string  `json:"evidenceRefs,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type RunbookEvidence struct {
	ID           string    `json:"id"`
	RunbookID    string    `json:"runbookId"`
	OperationID  string    `json:"operationId"`
	Outcome      string    `json:"outcome"`
	Status       string    `json:"status"`
	EvidenceRefs []string  `json:"evidenceRefs,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Store interface {
	ListRollouts(context.Context) ([]ProviderRollout, error)
	PutRollout(context.Context, ProviderRollout) error
	GetRollout(context.Context, string) (ProviderRollout, error)
	ListConformanceRuns(context.Context) ([]ConformanceRun, error)
	PutConformanceRun(context.Context, ConformanceRun) error
	ListEnvironmentTemplates(context.Context) ([]EnvironmentTemplate, error)
	PutEnvironmentTemplate(context.Context, EnvironmentTemplate) error
	ListEnvironmentLeases(context.Context) ([]EnvironmentLease, error)
	GetEnvironmentLease(context.Context, string) (EnvironmentLease, error)
	PutEnvironmentLease(context.Context, EnvironmentLease) error
	ListOperations(context.Context) ([]Operation, error)
	PutOperation(context.Context, Operation) error
	ListRunbookEvidence(context.Context) ([]RunbookEvidence, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func New(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("AI production store is required")
	}
	return &Service{store: store, now: time.Now}, nil
}

func (s *Service) ListRollouts(ctx context.Context) ([]ProviderRollout, error) {
	return s.store.ListRollouts(ctx)
}
func (s *Service) CreateRollout(ctx context.Context, item ProviderRollout) (ProviderRollout, error) {
	item.ID, item.Name = strings.TrimSpace(item.ID), strings.TrimSpace(item.Name)
	if item.ID == "" || item.DesiredRevision == 0 || item.CanaryPercent < 1 || item.CanaryPercent > 100 {
		return ProviderRollout{}, fmt.Errorf("invalid provider rollout")
	}
	item.Target.Environments = normalize(item.Target.Environments, 64)
	item.Target.Platforms = normalize(item.Target.Platforms, 16)
	item.Target.Architectures = normalize(item.Target.Architectures, 16)
	if len(item.Target.Labels) > 64 {
		return ProviderRollout{}, fmt.Errorf("provider rollout target exceeds limits")
	}
	now := s.now().UTC()
	item.Status, item.CreatedAt, item.UpdatedAt = "validating", now, now
	if err := s.store.PutRollout(ctx, item); err != nil {
		return ProviderRollout{}, err
	}
	return item, nil
}
func (s *Service) TransitionRollout(ctx context.Context, id, action string) (ProviderRollout, error) {
	item, err := s.store.GetRollout(ctx, strings.TrimSpace(id))
	if err != nil {
		return ProviderRollout{}, err
	}
	next := map[string]map[string]string{"validating": {"resume": "canary", "rollback": "rolling_back"}, "canary": {"pause": "paused", "resume": "rolling_out", "rollback": "rolling_back"}, "rolling_out": {"pause": "paused", "rollback": "rolling_back"}, "paused": {"resume": "rolling_out", "rollback": "rolling_back"}, "failed": {"rollback": "rolling_back"}}[item.Status][action]
	if next == "" {
		return ProviderRollout{}, fmt.Errorf("%w: rollout action %s from %s", ErrConflict, action, item.Status)
	}
	if next == "rolling_back" {
		item.Status = "rolled_back"
	} else {
		item.Status = next
	}
	item.UpdatedAt = s.now().UTC()
	if err := s.store.PutRollout(ctx, item); err != nil {
		return ProviderRollout{}, err
	}
	return item, nil
}
func (s *Service) ListConformanceRuns(ctx context.Context) ([]ConformanceRun, error) {
	return s.store.ListConformanceRuns(ctx)
}
func (s *Service) CreateConformanceRun(ctx context.Context, item ConformanceRun) (ConformanceRun, error) {
	item.ID, item.ProviderRef, item.EnvironmentRef, item.SuiteVersion = strings.TrimSpace(item.ID), strings.TrimSpace(item.ProviderRef), strings.TrimSpace(item.EnvironmentRef), strings.TrimSpace(item.SuiteVersion)
	if item.ID == "" || item.ProviderRef == "" || item.EnvironmentRef == "" || item.SuiteVersion == "" {
		return ConformanceRun{}, fmt.Errorf("invalid conformance run")
	}
	now := s.now().UTC()
	item.Status, item.CreatedAt, item.UpdatedAt = "queued", now, now
	if err := s.store.PutConformanceRun(ctx, item); err != nil {
		return ConformanceRun{}, err
	}
	return item, nil
}
func (s *Service) ListEnvironmentTemplates(ctx context.Context) ([]EnvironmentTemplate, error) {
	return s.store.ListEnvironmentTemplates(ctx)
}
func (s *Service) PutEnvironmentTemplate(ctx context.Context, item EnvironmentTemplate) (EnvironmentTemplate, error) {
	item.ID, item.Name, item.Backend, item.IsolationMode = strings.TrimSpace(item.ID), strings.TrimSpace(item.Name), strings.TrimSpace(item.Backend), strings.TrimSpace(item.IsolationMode)
	if item.ID == "" || item.Name == "" || (item.Backend != "container" && item.Backend != "kubernetes") || (item.IsolationMode != "read-only" && item.IsolationMode != "disposable-write") {
		return EnvironmentTemplate{}, fmt.Errorf("invalid environment template")
	}
	now := s.now().UTC()
	item.Status, item.CreatedAt, item.UpdatedAt = "active", now, now
	if err := s.store.PutEnvironmentTemplate(ctx, item); err != nil {
		return EnvironmentTemplate{}, err
	}
	return item, nil
}
func (s *Service) ListEnvironmentLeases(ctx context.Context) ([]EnvironmentLease, error) {
	return s.store.ListEnvironmentLeases(ctx)
}
func (s *Service) ReleaseEnvironmentLease(ctx context.Context, id string) (EnvironmentLease, error) {
	item, err := s.store.GetEnvironmentLease(ctx, strings.TrimSpace(id))
	if err != nil {
		return EnvironmentLease{}, err
	}
	if item.Status == "released" || item.Status == "expired" {
		return item, nil
	}
	if item.Status != "active" {
		return EnvironmentLease{}, fmt.Errorf("%w: environment lease is not releasable", ErrConflict)
	}
	item.Status, item.UpdatedAt = "released", s.now().UTC()
	if err := s.store.PutEnvironmentLease(ctx, item); err != nil {
		return EnvironmentLease{}, err
	}
	return item, nil
}
func (s *Service) GCEnvironmentLeases(ctx context.Context) (Operation, error) {
	items, err := s.store.ListEnvironmentLeases(ctx)
	if err != nil {
		return Operation{}, err
	}
	now := s.now().UTC()
	count := 0
	for _, item := range items {
		if item.Status == "active" && item.ExpiresAt != nil && !item.ExpiresAt.After(now) {
			item.Status, item.UpdatedAt = "expired", now
			if err := s.store.PutEnvironmentLease(ctx, item); err != nil {
				return Operation{}, err
			}
			count++
		}
	}
	op := Operation{ID: fmt.Sprintf("environment-gc-%d", now.UnixNano()), Kind: "gc", Category: "recovery", TargetRef: "environment-leases", RunbookID: "ai-environment-gc", Status: "completed", EvidenceRefs: []string{fmt.Sprintf("expired-leases:%d", count)}, CreatedAt: now, UpdatedAt: now}
	if err := s.store.PutOperation(ctx, op); err != nil {
		return Operation{}, err
	}
	return op, nil
}
func (s *Service) ListOperations(ctx context.Context) ([]Operation, error) {
	return s.store.ListOperations(ctx)
}
func (s *Service) StartOperation(ctx context.Context, item Operation) (Operation, error) {
	item.ID, item.Kind, item.TargetRef, item.RunbookID = strings.TrimSpace(item.ID), strings.TrimSpace(item.Kind), strings.TrimSpace(item.TargetRef), strings.TrimSpace(item.RunbookID)
	if item.ID == "" {
		item.ID = fmt.Sprintf("ai-operation-%d", s.now().UnixNano())
	}
	categories := map[string]string{"backup": "backup", "restore": "recovery", "index_rebuild": "recovery", "drill": "slo"}
	item.Category = categories[item.Kind]
	if item.Category == "" || item.TargetRef == "" || item.RunbookID == "" {
		return Operation{}, fmt.Errorf("invalid AI production operation")
	}
	now := s.now().UTC()
	item.Status, item.CreatedAt, item.UpdatedAt = "queued", now, now
	item.EvidenceRefs = slices.Clone(item.EvidenceRefs)
	if err := s.store.PutOperation(ctx, item); err != nil {
		return Operation{}, err
	}
	return item, nil
}
func (s *Service) ListRunbookEvidence(ctx context.Context) ([]RunbookEvidence, error) {
	return s.store.ListRunbookEvidence(ctx)
}

func (s *Service) RecordGateDecision(ctx context.Context, decision appaieval.GateDecision) error {
	status := "completed"
	if decision.Decision == "pass" {
		status = "queued"
	}
	operation := Operation{
		ID: "release-gate-" + decision.ID, Kind: "release_gate", Category: "slo",
		TargetRef: "evaluation-run:" + decision.CandidateRunID, RunbookID: "ai-evaluation-release-gate",
		Status: status, EvidenceRefs: append([]string(nil), decision.EvidenceRefs...),
		CreatedAt: decision.EvaluatedAt, UpdatedAt: decision.EvaluatedAt,
	}
	if operation.CreatedAt.IsZero() {
		operation.CreatedAt = s.now().UTC()
		operation.UpdatedAt = operation.CreatedAt
	}
	return s.store.PutOperation(ctx, operation)
}

func normalize(items []string, limit int) []string {
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}
