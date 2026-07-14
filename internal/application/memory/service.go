package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

var ErrNotFound = errors.New("memory record not found")

type Record struct {
	ID         string     `json:"id"`
	OwnerType  string     `json:"ownerType"`
	OwnerID    string     `json:"ownerId"`
	ScopeHash  string     `json:"scopeHash"`
	Fact       string     `json:"fact"`
	SourceType string     `json:"sourceType"`
	SourceRefs []string   `json:"sourceRefs"`
	Confidence float64    `json:"confidence"`
	ValidFrom  time.Time  `json:"validFrom"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	PolicyVer  string     `json:"policyVersion"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	DeletedAt  *time.Time `json:"deletedAt,omitempty"`
}

type Policy struct {
	ID                string        `json:"id"`
	Version           string        `json:"version"`
	OwnerTypes        []string      `json:"ownerTypes"`
	DefaultTTL        time.Duration `json:"defaultTtl"`
	MaximumTTL        time.Duration `json:"maximumTtl"`
	MinimumConfidence float64       `json:"minimumConfidence"`
	ExplicitWriteOnly bool          `json:"explicitWriteOnly"`
	Enabled           bool          `json:"enabled"`
}

type Store interface {
	PutRecord(context.Context, Record) error
	ListRecords(context.Context, string, string, time.Time) ([]Record, error)
	GetRecord(context.Context, string) (Record, error)
	DeleteRecord(context.Context, string, time.Time) error
	PutPolicy(context.Context, Policy) error
	GetPolicy(context.Context, string, string) (Policy, error)
	ListPolicies(context.Context) ([]Policy, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	return &Service{store: store, now: time.Now}, nil
}

func (s *Service) PutPolicy(ctx context.Context, policy Policy) error {
	policy.ID = strings.TrimSpace(policy.ID)
	policy.Version = strings.TrimSpace(policy.Version)
	if policy.ID == "" || policy.Version == "" || policy.MaximumTTL <= 0 || policy.DefaultTTL <= 0 || policy.DefaultTTL > policy.MaximumTTL {
		return fmt.Errorf("invalid memory policy")
	}
	if policy.MinimumConfidence < 0 || policy.MinimumConfidence > 1 {
		return fmt.Errorf("invalid memory policy confidence")
	}
	policy.OwnerTypes = normalizeStrings(policy.OwnerTypes, 8)
	if len(policy.OwnerTypes) == 0 {
		return fmt.Errorf("memory policy owner types are required")
	}
	return s.store.PutPolicy(ctx, policy)
}

func (s *Service) ListPolicies(ctx context.Context) ([]Policy, error) {
	return s.store.ListPolicies(ctx)
}

func (s *Service) GetPolicy(ctx context.Context, id, version string) (Policy, error) {
	return s.store.GetPolicy(ctx, strings.TrimSpace(id), strings.TrimSpace(version))
}

func (s *Service) PutRecord(ctx context.Context, record Record, policy Policy) (Record, error) {
	record.ID = strings.TrimSpace(record.ID)
	record.OwnerType = strings.TrimSpace(record.OwnerType)
	record.OwnerID = strings.TrimSpace(record.OwnerID)
	record.Fact = strings.TrimSpace(record.Fact)
	record.PolicyVer = strings.TrimSpace(record.PolicyVer)
	record.SourceType = strings.TrimSpace(record.SourceType)
	if record.ID == "" || record.OwnerID == "" || record.Fact == "" || len(record.Fact) > 8_000 || !strings.HasPrefix(record.ScopeHash, "sha256:") {
		return Record{}, fmt.Errorf("invalid memory record")
	}
	if !slices.Contains(policy.OwnerTypes, record.OwnerType) || !policy.Enabled || record.PolicyVer != policy.Version {
		return Record{}, fmt.Errorf("memory policy does not allow owner or version")
	}
	if record.SourceType != "explicit_user" && record.SourceType != "curated_extractor" {
		return Record{}, fmt.Errorf("memory source must be explicit or curated")
	}
	if policy.ExplicitWriteOnly && record.SourceType != "explicit_user" {
		return Record{}, fmt.Errorf("memory policy requires explicit user writes")
	}
	if record.Confidence < policy.MinimumConfidence || record.Confidence > 1 {
		return Record{}, fmt.Errorf("memory confidence is outside policy")
	}
	record.SourceRefs = normalizeStrings(record.SourceRefs, 32)
	now := s.now().UTC()
	if record.ValidFrom.IsZero() {
		record.ValidFrom = now
	}
	if record.ExpiresAt == nil {
		expiresAt := now.Add(policy.DefaultTTL)
		record.ExpiresAt = &expiresAt
	}
	if record.ExpiresAt.Before(now) || record.ExpiresAt.After(now.Add(policy.MaximumTTL)) {
		return Record{}, fmt.Errorf("memory expiry is outside policy")
	}
	record.Status = "active"
	record.CreatedAt = now
	if err := s.store.PutRecord(ctx, cloneRecord(record)); err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

func (s *Service) ListRecords(ctx context.Context, ownerType, ownerID string) ([]Record, error) {
	items, err := s.store.ListRecords(ctx, strings.TrimSpace(ownerType), strings.TrimSpace(ownerID), s.now().UTC())
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = cloneRecord(items[i])
	}
	return items, nil
}

func (s *Service) GetRecord(ctx context.Context, id string) (Record, error) {
	return s.store.GetRecord(ctx, strings.TrimSpace(id))
}

func (s *Service) DeleteRecord(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("memory record id is required")
	}
	return s.store.DeleteRecord(ctx, id, s.now().UTC())
}

func normalizeStrings(items []string, limit int) []string {
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func cloneRecord(record Record) Record {
	record.SourceRefs = slices.Clone(record.SourceRefs)
	if record.ExpiresAt != nil {
		expiresAt := *record.ExpiresAt
		record.ExpiresAt = &expiresAt
	}
	if record.DeletedAt != nil {
		deletedAt := *record.DeletedAt
		record.DeletedAt = &deletedAt
	}
	return record
}
