package copilot

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type PersonalGuestRenewalStore interface {
	ReadLockedPersonalGuest(context.Context, string) (domain.PersonalGuestRecord, error)
	LockPersonalGuestRenewal(context.Context, domain.PersonalGuestRecord) error
	GetPersonalGuestRenewal(context.Context, string) (domain.PersonalGuestRenewal, error)
	SavePersonalGuestRenewal(context.Context, domain.PersonalGuestRenewal) error
	CommitPersonalGuestRenewal(context.Context, domain.PersonalGuestRecord, domain.PersonalGuestRenewal, time.Time) error
}

func (s *PersonalGuestService) ConfigureRenewal(issuer PersonalGuestCertificateIssuer) {
	s.issuer = issuer
}

func (s *PersonalGuestService) Renew(ctx context.Context, identity networkidentity.Identity, input guest.Renewal) (guest.RenewalResult, error) {
	store, ok := s.store.(PersonalGuestRenewalStore)
	if !ok || s.issuer == nil {
		return guest.RenewalResult{}, apperrors.ErrServiceUnavailable
	}
	if input.Validate() != nil {
		return guest.RenewalResult{}, apperrors.ErrInvalidArgument
	}
	item, err := s.authenticatedGuest(ctx, identity)
	if err != nil {
		return guest.RenewalResult{}, err
	}
	if item.Status != "registered" || item.Binding.Version != input.BindingVersion {
		return guest.RenewalResult{}, apperrors.ErrConflict
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(input.CSRPEM)))
	var result guest.RenewalResult
	err = s.store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		if err := store.LockPersonalGuestRenewal(txCtx, item); err != nil {
			return err
		}
		if !s.now().Before(item.Binding.ExpiresAt) || identity.Certificate != nil && !s.now().Before(identity.Certificate.NotAfter) {
			return apperrors.ErrUnauthorized
		}
		old, err := store.GetPersonalGuestRenewal(txCtx, input.RequestID)
		if err == nil {
			if old.Result.Binding.ID != item.Binding.ID || old.OldFingerprint != identity.CertificateFingerprint || old.CSRHash != digest || old.Committed {
				return apperrors.ErrConflict
			}
			if !s.now().Before(old.CommitBefore) {
				return domain.ErrPersonalGuestRenewalExpired
			}
			result = old.Result
			return nil
		}
		if err != apperrors.ErrNotFound {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		binding := item.Binding
		binding.Version++
		binding.IssuedAt, binding.ExpiresAt = now, now.Add(time.Hour)
		certificate, ca, fingerprint, err := s.issuer.Issue(txCtx, binding, input.CSRPEM)
		if err != nil {
			return err
		}
		result = guest.RenewalResult{RequestID: input.RequestID, Binding: binding, CertificatePEM: certificate, ClientCAPEM: ca, CommitBefore: now.Add(5 * time.Minute)}
		pending := domain.PersonalGuestRenewal{Result: result, OldFingerprint: item.CertificateFingerprint, NewFingerprint: fingerprint, CSRHash: digest, CommitBefore: now.Add(5 * time.Minute)}
		if err := store.SavePersonalGuestRenewal(txCtx, pending); err != nil {
			return err
		}
		return s.recordRegistration(txCtx, item, "renew_prepare")
	})
	return result, err
}

func (s *PersonalGuestService) CommitRenewal(ctx context.Context, identity networkidentity.Identity, requestID string) (guest.Binding, error) {
	store, ok := s.store.(PersonalGuestRenewalStore)
	if !ok {
		return guest.Binding{}, apperrors.ErrServiceUnavailable
	}
	pending, item, err := s.authenticateRenewalCommit(ctx, store, identity, requestID)
	if err != nil {
		return guest.Binding{}, err
	}
	err = s.store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		current, err := store.ReadLockedPersonalGuest(txCtx, identity.ID)
		if err != nil {
			return err
		}
		pending, err = store.GetPersonalGuestRenewal(txCtx, requestID)
		if err != nil || pending.NewFingerprint != identity.CertificateFingerprint || pending.Result.Binding.ID != identity.ID {
			return apperrors.ErrUnauthorized
		}
		if err := s.authorizeOwner(txCtx, current.Binding); err != nil {
			return err
		}
		now := s.now().UTC()
		if identity.Certificate != nil && !now.Before(identity.Certificate.NotAfter) {
			return apperrors.ErrUnauthorized
		}
		if err := validateRenewalCommit(current, pending, now); err != nil {
			return err
		}
		if pending.Committed {
			return nil
		}
		if err := store.CommitPersonalGuestRenewal(txCtx, current, pending, now); err != nil {
			return err
		}
		return s.recordRegistration(txCtx, item, "renew_commit")
	})
	return pending.Result.Binding, err
}

func validateRenewalCommit(current domain.PersonalGuestRecord, pending domain.PersonalGuestRenewal, now time.Time) error {
	if current.Status != "registered" || !now.Before(pending.Result.Binding.ExpiresAt) || now.Before(pending.Result.Binding.IssuedAt) || !current.Binding.SameWorkspace(pending.Result.Binding) {
		return apperrors.ErrUnauthorized
	}
	if pending.Committed {
		if current.Binding != pending.Result.Binding || current.CertificateFingerprint != pending.NewFingerprint {
			return apperrors.ErrUnauthorized
		}
		return nil
	}
	if !now.Before(current.Binding.ExpiresAt) || current.CertificateFingerprint != pending.OldFingerprint || current.Binding.Version+1 != pending.Result.Binding.Version {
		return apperrors.ErrUnauthorized
	}
	if !now.Before(pending.CommitBefore) {
		return domain.ErrPersonalGuestRenewalExpired
	}
	return nil
}

func (s *PersonalGuestService) authenticateRenewalCommit(ctx context.Context, store PersonalGuestRenewalStore, identity networkidentity.Identity, requestID string) (domain.PersonalGuestRenewal, domain.PersonalGuestRecord, error) {
	if identity.Scope != networkidentity.ScopePersonalGuest || identity.Kind != "guest" {
		return domain.PersonalGuestRenewal{}, domain.PersonalGuestRecord{}, apperrors.ErrUnauthorized
	}
	pending, err := store.GetPersonalGuestRenewal(ctx, requestID)
	if err != nil || pending.Result.Binding.ID != identity.ID || pending.NewFingerprint != identity.CertificateFingerprint {
		return domain.PersonalGuestRenewal{}, domain.PersonalGuestRecord{}, apperrors.ErrUnauthorized
	}
	item, err := s.store.GetPersonalGuest(ctx, identity.ID)
	if err != nil {
		return domain.PersonalGuestRenewal{}, domain.PersonalGuestRecord{}, err
	}
	now := s.now().UTC()
	if item.Status != "registered" || !now.Before(pending.Result.Binding.ExpiresAt) || now.Before(pending.Result.Binding.IssuedAt) || !item.Binding.SameWorkspace(pending.Result.Binding) {
		return domain.PersonalGuestRenewal{}, domain.PersonalGuestRecord{}, apperrors.ErrUnauthorized
	}
	if err := s.authorizeOwner(ctx, item.Binding); err != nil {
		return domain.PersonalGuestRenewal{}, domain.PersonalGuestRecord{}, err
	}
	return pending, item, nil
}
