package memory

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

var ErrNotFound = fmt.Errorf("memory record not found: %w", apperrors.ErrNotFound)

type Record struct {
	Quarantined      bool              `json:"quarantined,omitempty"`
	QuarantineReason string            `json:"quarantineReason,omitempty"`
	SyncStatus       string            `json:"syncStatus,omitempty"`
	SourceHashes     map[string]string `json:"sourceHashes,omitempty"`
	Revision         int64             `json:"-"`
	PolicyID         string            `json:"policyId,omitempty"`
	ID               string            `json:"id"`
	OwnerType        string            `json:"ownerType"`
	OwnerID          string            `json:"ownerId"`
	ScopeHash        string            `json:"scopeHash"`
	Fact             string            `json:"fact"`
	SourceType       string            `json:"sourceType"`
	SourceRefs       []string          `json:"sourceRefs"`
	Confidence       float64           `json:"confidence"`
	ValidFrom        time.Time         `json:"validFrom"`
	ExpiresAt        *time.Time        `json:"expiresAt,omitempty"`
	PolicyVer        string            `json:"policyVersion"`
	Status           string            `json:"status"`
	CreatedAt        time.Time         `json:"createdAt"`
	DeletedAt        *time.Time        `json:"deletedAt,omitempty"`
}

type Policy struct {
	OwnerID           string        `json:"ownerId,omitempty"`
	Mode              string        `json:"mode,omitempty"`
	ReadEnabled       *bool         `json:"readEnabled,omitempty"`
	WriteEnabled      *bool         `json:"writeEnabled,omitempty"`
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
	store   Store
	now     func() time.Time
	backend *BackendService
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
		return fmt.Errorf("%w: invalid memory policy", apperrors.ErrInvalidArgument)
	}
	if policy.Mode != "" && policy.Mode != "chat" && policy.Mode != "heya" {
		return fmt.Errorf("%w: invalid memory mode", apperrors.ErrInvalidArgument)
	}
	if policy.MinimumConfidence < 0 || policy.MinimumConfidence > 1 {
		return fmt.Errorf("%w: invalid memory policy confidence", apperrors.ErrInvalidArgument)
	}
	policy.OwnerTypes = normalizeStrings(policy.OwnerTypes, 8)
	if len(policy.OwnerTypes) == 0 {
		return fmt.Errorf("%w: memory policy owner types are required", apperrors.ErrInvalidArgument)
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
	if err := validateMemoryWrite(record, policy); err != nil {
		return Record{}, err
	}
	consented, err := LegacyChatConsent(ctx, s.store, policy, record.OwnerID, true)
	if err != nil {
		return Record{}, err
	}
	if !consented {
		return Record{}, fmt.Errorf("%w: personal Chat memory writes are disabled", apperrors.ErrAccessDenied)
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
		return Record{}, fmt.Errorf("%w: memory expiry is outside policy", apperrors.ErrInvalidArgument)
	}
	record.PolicyID = policy.ID
	record.Status = "active"
	record.SyncStatus = ""
	record.DeletedAt = nil
	record.CreatedAt = now
	if err := s.store.PutRecord(ctx, cloneRecord(record)); err != nil {
		return Record{}, err
	}
	return s.GetRecord(ctx, record.ID)
}

func (s *Service) ListRecords(ctx context.Context, ownerType, ownerID string) ([]Record, error) {
	items, err := s.store.ListRecords(ctx, strings.TrimSpace(ownerType), strings.TrimSpace(ownerID), s.now().UTC())
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = s.recordState(ctx, cloneRecord(items[i]))
	}
	return items, nil
}

func (s *Service) GetRecord(ctx context.Context, id string) (Record, error) {
	record, err := s.store.GetRecord(ctx, strings.TrimSpace(id))
	if err != nil {
		return Record{}, err
	}
	return s.recordState(ctx, record), nil
}

func (s *Service) DeleteRecord(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: memory record id is required", apperrors.ErrInvalidArgument)
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
	if record.SourceHashes != nil {
		hashes := make(map[string]string, len(record.SourceHashes))
		for k, v := range record.SourceHashes {
			hashes[k] = v
		}
		record.SourceHashes = hashes
	}
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

func clonePolicy(p Policy) Policy {
	p.OwnerTypes = slices.Clone(p.OwnerTypes)
	if p.ReadEnabled != nil {
		value := *p.ReadEnabled
		p.ReadEnabled = &value
	}
	if p.WriteEnabled != nil {
		value := *p.WriteEnabled
		p.WriteEnabled = &value
	}
	return p
}

func validateMemoryWrite(record Record, policy Policy) error {
	if record.ID == "" || record.OwnerID == "" || record.Fact == "" || len(record.Fact) > 8_000 || !strings.HasPrefix(record.ScopeHash, "sha256:") {
		return fmt.Errorf("%w: invalid memory record", apperrors.ErrInvalidArgument)
	}
	if !WriteAllowed(policy) || (policy.OwnerID != "" && policy.OwnerID != record.OwnerID) || !slices.Contains(policy.OwnerTypes, record.OwnerType) || !policy.Enabled || record.PolicyVer != policy.Version {
		return fmt.Errorf("%w: memory policy does not allow owner or version", apperrors.ErrInvalidArgument)
	}
	if record.SourceType != "explicit_user" && record.SourceType != "curated_extractor" {
		return fmt.Errorf("%w: memory source must be explicit or curated", apperrors.ErrInvalidArgument)
	}
	if policy.ExplicitWriteOnly && record.SourceType != "explicit_user" {
		return fmt.Errorf("%w: memory policy requires explicit user writes", apperrors.ErrInvalidArgument)
	}
	if record.Confidence < policy.MinimumConfidence || record.Confidence > 1 {
		return fmt.Errorf("%w: memory confidence is outside policy", apperrors.ErrInvalidArgument)
	}
	return nil
}
