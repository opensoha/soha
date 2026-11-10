package memory

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func ReadAllowed(p Policy) bool  { return p.Enabled && (p.ReadEnabled == nil || *p.ReadEnabled) }
func WriteAllowed(p Policy) bool { return p.Enabled && (p.WriteEnabled == nil || *p.WriteEnabled) }

// Scope binds a private memory bank to one product surface. It is never client supplied.
func Scope(owner, mode string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("soha-memory-v1\x00"+owner+"\x00"+mode)))
}
func (s *Service) SetBackend(backend *BackendService) { s.backend = backend }
func (s *Service) Recall(ctx context.Context, owner, mode, query string) ([]Record, error) {
	if s.backend == nil {
		return nil, fmt.Errorf("memory backend unavailable")
	}
	return s.backend.Recall(ctx, "user", owner, Scope(owner, mode), query)
}

// RunSync is blocking: bootstrap owns its goroutine, cancellation and join.
func (s *BackendService) RunSync(ctx context.Context, report func(error)) {
	for ctx.Err() == nil {
		worked, err := s.SyncOne(ctx)
		if err != nil && ctx.Err() == nil && report != nil {
			report(err)
		}
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) ListRecordStates(ctx context.Context, ownerType, owner string, includeDeleted bool) ([]Record, error) {
	if reader, ok := s.store.(interface {
		ListRecordStates(context.Context, string, string, time.Time, bool) ([]Record, error)
	}); ok {
		items, err := reader.ListRecordStates(ctx, ownerType, owner, s.now().UTC(), includeDeleted)
		for i := range items {
			items[i] = s.recordState(ctx, items[i])
		}
		return items, err
	}
	return s.ListRecords(ctx, ownerType, owner)
}

func (s *Service) recordState(ctx context.Context, r Record) Record {
	if r.Quarantined && (r.SyncStatus == "synced" || r.SyncStatus == "pending") {
		r.SyncStatus = "disabled"
		return r
	}
	if r.Status == "active" && (r.SyncStatus == "synced" || r.SyncStatus == "pending" || r.SyncStatus == "") {
		p, err := s.store.GetPolicy(ctx, r.PolicyID, r.PolicyVer)
		if len(r.SourceRefs) == 0 || s.backend == nil || (err == nil && !ReadAllowed(p) && !WriteAllowed(p)) {
			r.SyncStatus = "disabled"
		}
	}
	return r
}

func PrivatePolicyID(owner, mode string) string {
	return fmt.Sprintf("personal-%x", sha256.Sum256([]byte(owner+"\x00"+mode)))
}

// LegacyChatConsent applies an explicit personal setting to records using a global Chat policy.
func LegacyChatConsent(ctx context.Context, policies interface {
	GetPolicy(context.Context, string, string) (Policy, error)
}, p Policy, owner string, write bool) (bool, error) {
	if p.OwnerID != "" || (p.Mode != "" && p.Mode != "chat") {
		return true, nil
	}
	consent, err := policies.GetPolicy(ctx, PrivatePolicyID(owner, "chat"), "v1")
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if consent.OwnerID != owner || consent.Mode != "chat" {
		return false, nil
	}
	if write {
		return WriteAllowed(consent), nil
	}
	return ReadAllowed(consent), nil
}

// SavePersonalPolicy preserves existing global policy management, but all new
// policies are private and have a server-derived identity.
func (s *Service) SavePersonalPolicy(ctx context.Context, owner string, p Policy) (Policy, error) {
	if owner == "" {
		return Policy{}, apperrors.ErrAccessDenied
	}
	version := p.Version
	if version == "" {
		version = "v1"
	}
	existing, err := s.store.GetPolicy(ctx, p.ID, version)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Policy{}, err
	}
	if err == nil && existing.OwnerID != "" && existing.OwnerID != owner {
		return Policy{}, apperrors.ErrAccessDenied
	}
	if err == nil && existing.OwnerID == "" && existing.Mode == "" && p.Mode == "" {
		if !p.ExplicitWriteOnly && p.Enabled {
			return Policy{}, fmt.Errorf("%w: global automatic memory is unsupported", apperrors.ErrInvalidArgument)
		}
		p.OwnerID = ""
		p.Version = version
	} else {
		if p.Mode == "" {
			p.Mode = "chat"
		}
		p.OwnerID = owner
		p.ID = PrivatePolicyID(owner, p.Mode)
		p.Version = "v1"
		p.OwnerTypes = []string{"user"}
		if p.Mode == "chat" && !p.ExplicitWriteOnly && p.Enabled {
			return Policy{}, fmt.Errorf("%w: Chat requires explicit memory writes", apperrors.ErrInvalidArgument)
		}
	}
	if err := s.PutPolicy(ctx, p); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (s *Service) ListPersonalPolicies(ctx context.Context, owner string) ([]Policy, error) {
	policies, err := s.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	out := []Policy{}
	for _, p := range policies {
		if p.OwnerID == "" || p.OwnerID == owner {
			out = append(out, p)
		}
	}
	return out, nil
}
