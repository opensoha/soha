package copilot

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type PersonalGuestStore interface {
	CreatePersonalGuest(context.Context, domain.PersonalGuestRecord) error
	GetPersonalGuest(context.Context, string) (domain.PersonalGuestRecord, error)
	CheckPersonalGuestBinding(context.Context, domain.PersonalGuestRecord) error
	ConsumePersonalGuestEnrollment(context.Context, domain.PersonalGuestRecord, string, time.Time) error
	RecordPersonalGuestHeartbeat(context.Context, domain.PersonalGuestRecord, guest.Heartbeat, time.Time) error
	WithinPersonalGuestTransaction(context.Context, func(context.Context) error) error
}

type PersonalGuestService struct {
	store          PersonalGuestStore
	authorizeOwner func(context.Context, guest.Binding) error
	audit          PersonalGuestAudit
	issuer         PersonalGuestCertificateIssuer
	artifact       []byte
	dataGrant      func(context.Context, guest.Binding, string) (guest.DataPlan, error)
	dataLease      func(context.Context, guest.Binding) (guest.DataPlan, error)
	toolsGrant     func(context.Context, guest.Binding, string) (guest.ToolsPlan, error)
	runtimeGrant   func(context.Context, guest.Binding, string) (guest.RuntimeOperation, error)
	browserNetwork personalBrowserFetcher
	now            func() time.Time
}

type PersonalGuestAudit interface {
	Record(context.Context, domainaudit.Entry) error
}

// authorizeOwner must reload current identity, permissions and provider generation.
func NewPersonalGuestService(store PersonalGuestStore, authorizeOwner func(context.Context, guest.Binding) error, audit PersonalGuestAudit) (*PersonalGuestService, error) {
	if isNilDependency(store) || authorizeOwner == nil || isNilDependency(audit) {
		return nil, fmt.Errorf("personal guest storage, current owner authorization and audit are required")
	}
	return &PersonalGuestService{store: store, authorizeOwner: authorizeOwner, audit: audit, now: time.Now}, nil
}

// Prepare consumes a Core-derived frozen VM binding, not guest or model input.
// The returned challenge is bootstrap-only and must not enter audits or events.
func (s *PersonalGuestService) Prepare(ctx context.Context, binding guest.Binding, certificateFingerprint string, accessVersion int64, ttl time.Duration) (domain.PersonalGuestRecord, string, error) {
	now := s.now().UTC()
	if binding.Validate() != nil || binding.IssuedAt.After(now) || !binding.ExpiresAt.After(now) || ttl < guest.EnrollmentMinTTL || ttl > guest.EnrollmentMaxTTL {
		return domain.PersonalGuestRecord{}, "", apperrors.ErrInvalidArgument
	}
	if err := s.authorizeOwner(ctx, binding); err != nil {
		return domain.PersonalGuestRecord{}, "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return domain.PersonalGuestRecord{}, "", fmt.Errorf("generate personal guest enrollment: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	hash, err := guest.EnrollmentTokenHash("Bearer " + token)
	if err != nil {
		return domain.PersonalGuestRecord{}, "", err
	}
	item := domain.PersonalGuestRecord{Binding: binding, CertificateFingerprint: certificateFingerprint, AccessVersion: accessVersion,
		EnrollmentID: uuid.NewString(), ChallengeHash: hash, EnrollmentIssuedAt: now.Truncate(time.Microsecond), EnrollmentExpiresAt: now.Truncate(time.Microsecond).Add(ttl), Status: "pending"}
	if err := item.Validate(); err != nil {
		return domain.PersonalGuestRecord{}, "", apperrors.ErrInvalidArgument
	}
	if err := s.store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.CreatePersonalGuest(txCtx, item); err != nil {
			return err
		}
		return s.recordRegistration(txCtx, item, "prepare")
	}); err != nil {
		return domain.PersonalGuestRecord{}, "", err
	}
	return item, token, nil
}

func (s *PersonalGuestService) authenticatedGuest(ctx context.Context, identity networkidentity.Identity) (domain.PersonalGuestRecord, error) {
	if identity.Scope != networkidentity.ScopePersonalGuest || identity.Kind != "guest" || identity.ID == "" || identity.CertificateFingerprint == "" {
		return domain.PersonalGuestRecord{}, apperrors.ErrUnauthorized
	}
	item, err := s.store.GetPersonalGuest(ctx, identity.ID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return domain.PersonalGuestRecord{}, apperrors.ErrUnauthorized
		}
		return domain.PersonalGuestRecord{}, err
	}
	now := s.now().UTC()
	if item.Validate() != nil || item.Binding.ID != identity.ID || item.CertificateFingerprint != identity.CertificateFingerprint || now.Before(item.Binding.IssuedAt) || !now.Before(item.Binding.ExpiresAt) || item.Status == "revoked" {
		return domain.PersonalGuestRecord{}, apperrors.ErrUnauthorized
	}
	if err := s.authorizeOwner(ctx, item.Binding); err != nil {
		return domain.PersonalGuestRecord{}, err
	}
	if err := s.store.CheckPersonalGuestBinding(ctx, item); err != nil {
		return domain.PersonalGuestRecord{}, err
	}
	return item, nil
}

func (s *PersonalGuestService) Enroll(ctx context.Context, identity networkidentity.Identity, authorization string, enrollment guest.Enrollment) (guest.Binding, error) {
	if err := enrollment.Validate(); err != nil {
		return guest.Binding{}, apperrors.ErrInvalidArgument
	}
	hash, err := guest.EnrollmentTokenHash(authorization)
	if err != nil {
		return guest.Binding{}, apperrors.ErrUnauthorized
	}
	item, err := s.authenticatedGuest(ctx, identity)
	if err != nil {
		return guest.Binding{}, err
	}
	if enrollment.EnrollmentID != item.EnrollmentID || item.Status != "pending" {
		return guest.Binding{}, apperrors.ErrUnauthorized
	}
	if err := s.store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.ConsumePersonalGuestEnrollment(txCtx, item, hash, s.now().UTC()); err != nil {
			return err
		}
		return s.recordRegistration(txCtx, item, "enroll")
	}); err != nil {
		return guest.Binding{}, err
	}
	return item.Binding, nil
}

func (s *PersonalGuestService) Binding(ctx context.Context, identity networkidentity.Identity) (guest.Binding, error) {
	item, err := s.authenticatedGuest(ctx, identity)
	if err != nil {
		return guest.Binding{}, err
	}
	if item.Status != "registered" {
		return guest.Binding{}, apperrors.ErrUnauthorized
	}
	return item.Binding, nil
}

func (s *PersonalGuestService) Heartbeat(ctx context.Context, identity networkidentity.Identity, heartbeat guest.Heartbeat) error {
	item, err := s.authenticatedGuest(ctx, identity)
	if err != nil {
		return err
	}
	if item.Status != "registered" {
		return apperrors.ErrUnauthorized
	}
	now := s.now().UTC()
	if err := heartbeat.ValidateFor(item.Binding, now, item.LastSequence); err != nil {
		return apperrors.ErrConflict
	}
	return s.store.RecordPersonalGuestHeartbeat(ctx, item, heartbeat, now)
}

func (s *PersonalGuestService) recordRegistration(ctx context.Context, item domain.PersonalGuestRecord, action string) error {
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{ActorID: item.Binding.OwnerID, ResourceKind: "PersonalGuest", ResourceName: item.Binding.ID,
		Action: "personal_guest." + action, Result: "success", Summary: "Personal guest " + action,
		RequestID: meta.RequestID, RequestPath: meta.Path, RequestMethod: meta.Method, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"bindingVersion": item.Binding.Version, "computeRef": item.Binding.ComputeRef, "computeGeneration": item.Binding.ComputeGeneration, "accessVersion": item.AccessVersion}})
}
