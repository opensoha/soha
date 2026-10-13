package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type guestPreparationSource struct {
	environment domain.PersonalEnvironmentRecord
}

func (s *guestPreparationSource) WithinPersonalGuestTransaction(ctx context.Context, apply func(context.Context) error) error {
	return apply(ctx)
}

func (s *guestPreparationSource) LockPersonalGuestPreparation(_ context.Context, owner string) (domain.PersonalEnvironmentRecord, int64, int64, error) {
	if owner != s.environment.CreatedBy {
		return domain.PersonalEnvironmentRecord{}, 0, 0, apperrors.ErrAccessDenied
	}
	return s.environment, 2, 3, nil
}

type guestPreparationIssuer struct {
	issued guest.Binding
	err    error
}

func (s *guestPreparationIssuer) Issue(_ context.Context, binding guest.Binding, _ string) (string, string, string, error) {
	s.issued = binding
	return "public-leaf", "public-ca", "sha256:" + strings.Repeat("b", 64), s.err
}

func guestPreparationFixture(t *testing.T) (*PersonalGuestPreparationService, *PersonalGuestService, *personalGuestMemory, *guestPreparationSource, *guestPreparationIssuer) {
	t.Helper()
	service, memory, _, _, _ := personalGuestFixture(t)
	b := memory.item.Binding
	memory.item, memory.audits = domain.PersonalGuestRecord{}, nil
	source := &guestPreparationSource{environment: domain.PersonalEnvironmentRecord{AssistantID: b.AssistantID, CreatedBy: b.OwnerID, Backend: "vm", Provider: b.Provider,
		ComputeRef: b.ComputeRef, ComputeGeneration: b.ComputeGeneration, DataRef: b.DataRef, PolicyRevision: b.PolicyRevision, Version: 1, State: "pending",
		VMIdentity: &domain.PersonalVMIdentity{ProviderIdentityHash: b.ProviderIdentityHash, DataIdentityHash: "sha256:" + strings.Repeat("d", 64), DataDiskSerial: b.DataDiskSerial, DataDiskBytes: b.DataDiskBytes}}}
	issuer := &guestPreparationIssuer{}
	preparation, err := NewPersonalGuestPreparationService(source, service, issuer, "https://core.example:8444")
	if err != nil {
		t.Fatal(err)
	}
	return preparation, service, memory, source, issuer
}

func TestPersonalGuestPreparationDerivesOwnerAndKeepsPending(t *testing.T) {
	preparation, _, memory, source, issuer := guestPreparationFixture(t)
	result, err := preparation.Prepare(t.Context(), identity.Principal{UserID: source.environment.CreatedBy}, sohaapi.PersonalGuestPreparationInput{EnvironmentVersion: 1, CsrPEM: "public-csr"})
	if err != nil {
		t.Fatal(err)
	}
	b := issuer.issued
	if b.ID == "" || b.Version != 3 || b.OwnerID != source.environment.CreatedBy || b.ComputeRef != source.environment.ComputeRef || b.DataRef != source.environment.DataRef || b.ComputeGeneration != 1 || result.BindingID.String() != b.ID || result.Endpoint != "https://core.example:8444" {
		t.Fatal("preparation did not derive the frozen current owner and VM identity")
	}
	if _, err := guest.EnrollmentTokenHash("Bearer " + result.Challenge); err != nil || memory.item.Status != "pending" || source.environment.DataInitialized || source.environment.State != "pending" {
		t.Fatal("preparation granted readiness or returned an invalid challenge")
	}
	audits, err := json.Marshal(memory.audits)
	if err != nil || len(memory.audits) != 1 || strings.Contains(string(audits), result.Challenge) || strings.Contains(string(audits), memory.item.ChallengeHash) {
		t.Fatal("preparation audit missing or contains bootstrap credentials")
	}
}

func TestPersonalGuestPreparationRejectsStaleForeignAndRevoked(t *testing.T) {
	for _, name := range []string{"stale", "foreign", "revoked", "active operation", "issuer failure", "audit failure"} {
		t.Run(name, func(t *testing.T) {
			preparation, service, memory, source, issuer := guestPreparationFixture(t)
			principal := identity.Principal{UserID: source.environment.CreatedBy}
			input := sohaapi.PersonalGuestPreparationInput{EnvironmentVersion: 1, CsrPEM: "public-csr"}
			switch name {
			case "stale":
				input.EnvironmentVersion = 2
			case "foreign":
				principal.UserID = "foreign"
			case "revoked":
				service.authorizeOwner = func(context.Context, guest.Binding) error { return apperrors.ErrAccessDenied }
			case "active operation":
				source.environment.OperationID = "11111111-1111-4111-8111-111111111111"
			case "issuer failure":
				issuer.err = errors.New("issuer unavailable")
			case "audit failure":
				memory.auditErr = errors.New("audit unavailable")
			}
			result, err := preparation.Prepare(t.Context(), principal, input)
			if err == nil || result.Challenge != "" || memory.item.Binding.ID != "" {
				t.Fatal("failed preparation retained a usable credential")
			}
			if name != "issuer failure" && name != "audit failure" && issuer.issued.ID != "" {
				t.Fatal("unauthorized preparation reached certificate issuance")
			}
		})
	}
}

func TestPersonalGuestPreparationRecoversInitializedDataWithoutReadinessGrant(t *testing.T) {
	preparation, _, _, source, _ := guestPreparationFixture(t)
	source.environment.DataInitialized, source.environment.DataCreationAttempted = true, true
	before := source.environment
	if _, err := preparation.Prepare(t.Context(), identity.Principal{UserID: before.CreatedBy}, sohaapi.PersonalGuestPreparationInput{EnvironmentVersion: 1, CsrPEM: "public-csr"}); err != nil {
		t.Fatal(err)
	}
	if source.environment.Version != before.Version || source.environment.DataRef != before.DataRef || source.environment.State != "pending" {
		t.Fatal("identity recovery changed data or readiness")
	}
}
