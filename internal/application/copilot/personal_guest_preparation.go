package copilot

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type PersonalGuestPreparationStore interface {
	WithinPersonalGuestTransaction(context.Context, func(context.Context) error) error
	LockPersonalGuestPreparation(context.Context, string) (domain.PersonalEnvironmentRecord, int64, int64, error)
}

type PersonalGuestCertificateIssuer interface {
	Issue(context.Context, guest.Binding, string) (certificatePEM, clientCAPEM, fingerprint string, err error)
}

type PersonalGuestPreparationService struct {
	store       PersonalGuestPreparationStore
	guest       *PersonalGuestService
	issuer      PersonalGuestCertificateIssuer
	endpoint    string
	provisioner PersonalGuestProvisioner
	artifactSHA string
}

func NewPersonalGuestPreparationService(store PersonalGuestPreparationStore, service *PersonalGuestService, issuer PersonalGuestCertificateIssuer, endpoint string) (*PersonalGuestPreparationService, error) {
	u, err := url.Parse(endpoint)
	if isNilDependency(store) || service == nil || isNilDependency(issuer) || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || len(endpoint) > 512 || strings.TrimSpace(endpoint) != endpoint {
		return nil, fmt.Errorf("personal guest preparation requires storage, authority, issuer and an explicit HTTPS endpoint")
	}
	return &PersonalGuestPreparationService{store: store, guest: service, issuer: issuer, endpoint: endpoint}, nil
}

func (s *PersonalGuestPreparationService) Prepare(ctx context.Context, principal identity.Principal, input sohaapi.PersonalGuestPreparationInput) (sohaapi.PersonalGuestPreparation, error) {
	return s.prepare(ctx, principal, input, "")
}

func (s *PersonalGuestPreparationService) prepare(ctx context.Context, principal identity.Principal, input sohaapi.PersonalGuestPreparationInput, originalOperation string) (sohaapi.PersonalGuestPreparation, error) {
	if principal.UserID == "" {
		return sohaapi.PersonalGuestPreparation{}, apperrors.ErrUnauthorized
	}
	if input.EnvironmentVersion < 1 || input.EnvironmentVersion > guest.MaxCounter || len(input.CsrPEM) < 1 || len(input.CsrPEM) > 4096 {
		return sohaapi.PersonalGuestPreparation{}, apperrors.ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var result sohaapi.PersonalGuestPreparation
	err := s.store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		environment, accessVersion, revision, err := s.store.LockPersonalGuestPreparation(txCtx, principal.UserID)
		if err != nil {
			return err
		}
		if environment.Version != input.EnvironmentVersion {
			return apperrors.ErrConflict
		}
		if originalOperation != "" {
			if environment.OperationID != originalOperation { return apperrors.ErrConflict }
			// The dedicated reconciliation path has authorized this exact retained operation.
			environment.OperationID = ""
		}
		binding, err := personalGuestBinding(environment, principal.UserID, revision, s.guest.now().UTC())
		if err != nil {
			return err
		}
		// Recheck the live provider before signing, while the frozen owner/access rows stay locked.
		if err := s.guest.authorizeOwner(txCtx, binding); err != nil {
			return err
		}
		certificate, ca, fingerprint, err := s.issuer.Issue(txCtx, binding, input.CsrPEM)
		if err != nil {
			return err
		}
		record, token, err := s.guest.Prepare(txCtx, binding, fingerprint, accessVersion, guest.EnrollmentMaxTTL)
		if err != nil {
			return err
		}
		result = sohaapi.PersonalGuestPreparation{BindingID: uuid.MustParse(binding.ID), EnrollmentID: uuid.MustParse(record.EnrollmentID),
			Challenge: token, CertificatePEM: certificate, ClientCAPEM: ca, Endpoint: s.endpoint, ExpiresAt: record.EnrollmentExpiresAt}
		return nil
	})
	if err != nil {
		return sohaapi.PersonalGuestPreparation{}, err
	}
	return result, nil
}

func personalGuestBinding(environment domain.PersonalEnvironmentRecord, owner string, revision int64, now time.Time) (guest.Binding, error) {
	proof := environment.VMIdentity
	if environment.CreatedBy != owner || environment.Backend != "vm" || environment.State != "pending" || proof == nil || proof.Validate() != nil || environment.OperationID != "" || environment.ActiveTaskID != "" {
		return guest.Binding{}, apperrors.ErrAccessDenied
	}
	binding := guest.Binding{SchemaVersion: guest.BindingVersion, ID: uuid.NewString(), Version: revision, AssistantID: environment.AssistantID,
		OwnerID: owner, Provider: environment.Provider, ComputeRef: environment.ComputeRef, ComputeGeneration: environment.ComputeGeneration,
		ProviderIdentityHash: proof.ProviderIdentityHash, DataRef: environment.DataRef, DataDiskSerial: proof.DataDiskSerial,
		DataDiskBytes: proof.DataDiskBytes, PolicyRevision: environment.PolicyRevision, IssuedAt: now.Truncate(time.Microsecond), ExpiresAt: now.Truncate(time.Microsecond).Add(time.Hour)}
	if binding.Validate() != nil {
		return guest.Binding{}, apperrors.ErrConflict
	}
	return binding, nil
}
