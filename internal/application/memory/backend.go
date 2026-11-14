package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

var ErrBackendNotFound = errors.New("memory backend object not found")
var ErrSyncUnknown = errors.New("memory sync submission outcome unknown")

// Backend exposes only raw source-backed memories; generated text is never trusted as a user fact.
type Backend interface {
	Retain(context.Context, string, SyncJob) error
	Operation(context.Context, string, string) (string, error)
	DeleteDocument(context.Context, string, string, string) error
	Recall(context.Context, string, string) ([]Candidate, error)
}
type Candidate struct {
	DocumentID string
	Revision   int64
}
type SyncJob struct {
	ID                string
	Record            Record
	Revision          int64
	DocumentID        string
	Kind              string
	Attempted         bool
	LeaseToken        string
	RetainOperationID string
	Revalidate        bool
}

// SyncStore must atomically persist CRUD, revisions, tombstones and outbox entries.
// Claimed jobs have a bounded lease; delete jobs depend on terminal retain jobs.
type SyncStore interface {
	EnsureBank(context.Context, Record, string) error
	MarkSubmitting(context.Context, SyncJob) error
	ClaimSync(context.Context) (SyncJob, error)
	FinishSync(context.Context, SyncJob, string) error
	ResolveDocument(context.Context, string) (Record, error)
	InvalidateSync(context.Context, Record, bool) error
}
type SourceValidator interface {
	// ValidateSource checks current source existence, revision and caller access. Missing provenance is denied.
	ValidateSource(context.Context, Record) error
}
type BackendService struct {
	store     Store
	syncStore SyncStore
	backend   Backend
	sources   SourceValidator
	instance  string
}

func NewBackendService(store Store, syncStore SyncStore, backend Backend, sources SourceValidator, instance string) (*BackendService, error) {
	if nilDependency(store) || nilDependency(syncStore) || nilDependency(backend) || nilDependency(sources) || strings.TrimSpace(instance) == "" {
		return nil, fmt.Errorf("memory backend dependencies and instance namespace required")
	}
	return &BackendService{store: store, syncStore: syncStore, backend: backend, sources: sources, instance: instance}, nil
}
func (s *BackendService) bank(r Record) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q/%q/%q/%q", s.instance, r.OwnerType, r.OwnerID, r.ScopeHash)))
	return "soha-" + hex.EncodeToString(sum[:])
}

// SyncOne executes at most one leased outbox step; callers own scheduling and shutdown.
func (s *BackendService) SyncOne(ctx context.Context) (bool, error) {
	job, err := s.syncStore.ClaimSync(ctx)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	stepCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	state, err := s.syncStep(stepCtx, job)
	if errors.Is(err, ErrSyncUnknown) {
		state = "unknown"
	}
	if err != nil && state == "" {
		state = "retry"
	}
	if finishErr := s.syncStore.FinishSync(ctx, job, state); finishErr != nil {
		return true, errors.Join(err, finishErr)
	}
	return true, err
}
func (s *BackendService) syncStep(ctx context.Context, j SyncJob) (string, error) {
	if j.Revalidate {
		return s.revalidateSync(ctx, j)
	}
	bank := s.bank(j.Record)
	if err := s.syncStore.EnsureBank(ctx, j.Record, bank); err != nil {
		return "retry", err
	}
	if j.Kind == "delete" {
		if err := s.backend.DeleteDocument(ctx, bank, j.DocumentID, j.RetainOperationID); err != nil {
			return "retry", err
		}
		return "done", nil
	}
	if !j.Attempted {
		eligible, err := s.canSubmit(ctx, j)
		if err != nil {
			return "retry", err
		}
		if !eligible {
			return "done", nil
		}
	}
	status, err := s.backend.Operation(ctx, bank, j.ID)
	if errors.Is(err, ErrBackendNotFound) {
		// A lost ACK may outlive vendor operation retention. Never blindly enqueue it again.
		if j.Attempted {
			return "retry", ErrSyncUnknown
		}
		if err := s.syncStore.MarkSubmitting(ctx, j); err != nil {
			return "retry", err
		}
		if err := s.backend.Retain(ctx, bank, j); err != nil {
			return "retry", err
		}
		return "pending", nil
	}
	if err != nil {
		return "retry", err
	}
	if !j.Attempted {
		if err := s.syncStore.MarkSubmitting(ctx, j); err != nil {
			return "retry", err
		}
	}
	switch status {
	case "completed":
		return "done", nil
	case "failed", "cancelled":
		return "failed", fmt.Errorf("Hindsight retain ended %s", status)
	case "pending", "processing":
		return "pending", nil
	default:
		return "retry", fmt.Errorf("invalid memory operation status")
	}
}

func (s *BackendService) revalidateSync(ctx context.Context, j SyncJob) (string, error) {
	current, err := s.store.GetRecord(ctx, j.Record.ID)
	if err != nil || current.Revision != j.Revision || current.Status != "active" {
		return "done", err
	}
	_, err = s.authorizeSync(ctx, current, false)
	if err != nil {
		return "retry", err
	}
	return "done", nil
}

// Recall accepts server-resolved ownership/scope only. No bank or generated fact crosses this boundary.
func (s *BackendService) Recall(ctx context.Context, ownerType, ownerID, scopeHash, query string) ([]Record, error) {
	if ownerType == "" || ownerID == "" || !strings.HasPrefix(scopeHash, "sha256:") || strings.TrimSpace(query) == "" || len(query) > 8000 {
		return nil, fmt.Errorf("invalid memory recall scope or query")
	}
	scope := Record{OwnerType: ownerType, OwnerID: ownerID, ScopeHash: scopeHash}
	bank := s.bank(scope)
	if err := s.syncStore.EnsureBank(ctx, scope, bank); err != nil {
		return nil, err
	}
	candidates, err := s.backend.Recall(ctx, bank, query)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(candidates))
	seen := map[string]bool{}
	for _, c := range candidates {
		r, err := s.syncStore.ResolveDocument(ctx, c.DocumentID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !recallable(r, c, ownerType, ownerID, scopeHash, time.Now()) || seen[r.ID] {
			continue
		}
		allowed, err := s.authorizeSync(ctx, r, false)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		// Re-read after source authorization so a concurrent delete/correction cannot reuse this projection.
		latest, err := s.syncStore.ResolveDocument(ctx, c.DocumentID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !recallable(latest, c, ownerType, ownerID, scopeHash, time.Now()) {
			continue
		}
		seen[r.ID] = true
		out = append(out, cloneRecord(r))
	}
	return out, nil
}
func recallable(r Record, c Candidate, ownerType, ownerID, scope string, now time.Time) bool {
	if r.Quarantined {
		return false
	}
	return c.DocumentID != "" && c.Revision > 0 && c.Revision == r.Revision && r.OwnerType == ownerType && r.OwnerID == ownerID && r.ScopeHash == scope && r.Status == "active" && r.DeletedAt == nil && r.ExpiresAt != nil && r.ExpiresAt.After(now) && !r.ValidFrom.After(now) && len(r.SourceRefs) > 0
}

func nilDependency(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return r.IsNil()
	}
	return false
}

func (s *BackendService) canSubmit(ctx context.Context, j SyncJob) (bool, error) {
	current, err := s.store.GetRecord(ctx, j.Record.ID)
	if err != nil {
		return false, err
	}
	if current.Quarantined || current.Status != "active" || current.DeletedAt != nil || current.Revision != j.Revision || current.ExpiresAt == nil || !current.ExpiresAt.After(time.Now()) {
		return false, nil
	}
	if len(current.SourceRefs) == 0 {
		return false, nil
	}
	return s.authorizeSync(ctx, current, true)
}

func (s *BackendService) authorizeSync(ctx context.Context, r Record, submitting bool) (bool, error) {
	if r.Quarantined {
		return false, s.syncStore.InvalidateSync(ctx, r, false)
	}
	p, err := s.store.GetPolicy(ctx, r.PolicyID, r.PolicyVer)
	if errors.Is(err, ErrNotFound) {
		return false, s.syncStore.InvalidateSync(ctx, r, false)
	}
	if err != nil {
		return false, err
	}
	consented, err := LegacyChatConsent(ctx, s.store, p, r.OwnerID, false)
	if err != nil {
		return false, err
	}
	if !consented || !policyAllows(p, r) {
		return false, s.syncStore.InvalidateSync(ctx, r, false)
	}
	if submitting {
		writable, err := LegacyChatConsent(ctx, s.store, p, r.OwnerID, true)
		if err != nil || !writable || !WriteAllowed(p) {
			return false, err
		}
	}
	if err := s.sources.ValidateSource(ctx, r); err != nil {
		if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrUnauthorized) {
			return false, s.syncStore.InvalidateSync(ctx, r, true)
		}
		return false, err
	}
	return true, nil
}

func policyAllows(p Policy, r Record) bool {
	return ReadAllowed(p) && (p.OwnerID == "" || p.OwnerID == r.OwnerID) && p.Version == r.PolicyVer && slices.Contains(p.OwnerTypes, r.OwnerType) && r.Confidence >= p.MinimumConfidence && (!p.ExplicitWriteOnly || r.SourceType == "explicit_user")
}
