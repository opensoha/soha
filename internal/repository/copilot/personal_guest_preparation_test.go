package copilot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// PostgreSQL and normal activation/binding are real; provider and certificate issuance are stubs.
func TestPersonalGuestPreparationWithPostgres(t *testing.T) {
	f := newPersonalVMFixture(t)
	testPersonalVMPendingAndReplay(t, f)
	authorize := func(ctx context.Context, binding guest.Binding) error {
		access, err := f.service.GetPersonalAgentAccess(ctx, f.actor)
		if err != nil {
			return err
		}
		if access.Status != "approved" || binding.OwnerID != f.actor.UserID || binding.ComputeRef != f.bound.ComputeRef || binding.ProviderIdentityHash != f.proof.ProviderIdentityHash {
			return apperrors.ErrAccessDenied
		}
		return nil
	}
	service, err := appcopilot.NewPersonalGuestService(f.repo, authorize, f.audit)
	if err != nil {
		t.Fatal(err)
	}
	prepare, err := appcopilot.NewPersonalGuestPreparationService(f.repo, service, preparationPGIssuer{}, "https://guest.example:8444")
	if err != nil {
		t.Fatal(err)
	}
	input := sohaapi.PersonalGuestPreparationInput{EnvironmentVersion: f.bound.Version, CsrPEM: "public CSR tested by dedicated issuer tests"}
	first, ident := testGuestPreparedRegistration(t, f, service, prepare, input)
	testGuestPreparationOwnerCAS(t, f, prepare, input)
	latest := testGuestPreparationConcurrency(t, f, service, prepare, input, ident)
	testGuestPreparationRevocation(t, f, service, prepare, input, first, latest)
}

func testGuestPreparedRegistration(t *testing.T, f *personalVMFixture, service *appcopilot.PersonalGuestService, prepare *appcopilot.PersonalGuestPreparationService, input sohaapi.PersonalGuestPreparationInput) (sohaapi.PersonalGuestPreparation, networkidentity.Identity) {
	first, err := prepare.Prepare(f.ctx, f.actor, input)
	if err != nil || first.Challenge == "" {
		t.Fatalf("normal preparation failed: %v", err)
	}
	item, err := f.repo.GetPersonalGuest(f.ctx, first.BindingID.String())
	if err != nil || item.Binding.Version != 1 || item.Binding.DataRef != f.bound.DataRef || item.AccessVersion != 2 {
		t.Fatalf("preparation lost locked authority: %v", err)
	}
	ident := networkidentity.Identity{Scope: networkidentity.ScopePersonalGuest, Kind: "guest", ID: item.Binding.ID, CertificateFingerprint: item.CertificateFingerprint}
	if _, err := service.Enroll(f.ctx, ident, "Bearer "+first.Challenge, guest.Enrollment{SchemaVersion: guest.EnrollmentVersion, EnrollmentID: first.EnrollmentID.String()}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Binding(f.ctx, ident); err != nil {
		t.Fatal(err)
	}
	return first, ident
}
func testGuestPreparationOwnerCAS(t *testing.T, f *personalVMFixture, prepare *appcopilot.PersonalGuestPreparationService, input sohaapi.PersonalGuestPreparationInput) {
	stale := input
	stale.EnvironmentVersion++
	if result, err := prepare.Prepare(f.ctx, f.actor, stale); !errors.Is(err, apperrors.ErrConflict) || result.Challenge != "" {
		t.Fatal("stale CAS returned a grant")
	}
	if result, err := prepare.Prepare(f.ctx, identity.Principal{UserID: uuid.NewString(), Roles: []string{"chat"}}, input); !errors.Is(err, apperrors.ErrAccessDenied) || result.Challenge != "" {
		t.Fatal("foreign owner returned a grant")
	}

}
func testGuestPreparationConcurrency(t *testing.T, f *personalVMFixture, service *appcopilot.PersonalGuestService, prepare *appcopilot.PersonalGuestPreparationService, input sohaapi.PersonalGuestPreparationInput, ident networkidentity.Identity) sohaapi.PersonalGuestPreparation {

	var wg sync.WaitGroup
	results := make(chan sohaapi.PersonalGuestPreparation, 2)
	for range 2 {
		wg.Go(func() {
			result, err := prepare.Prepare(f.ctx, f.actor, input)
			if err != nil {
				t.Errorf("concurrent preparation: %v", err)
				return
			}
			results <- result
		})
	}
	wg.Wait()
	close(results)
	versions := map[int64]bool{}
	var latest sohaapi.PersonalGuestPreparation
	for result := range results {
		record, err := f.repo.GetPersonalGuest(f.ctx, result.BindingID.String())
		if err != nil {
			t.Fatal(err)
		}
		versions[record.Binding.Version] = true
		if record.Binding.Version == 3 {
			latest = result
		}
	}
	if !versions[2] || !versions[3] || len(versions) != 2 {
		t.Fatal("row locks did not serialize unique guest revisions")
	}
	if _, err := service.Binding(f.ctx, ident); err == nil {
		t.Fatal("superseded guest retained access")
	}
	f.audit.fail = true
	if result, err := prepare.Prepare(f.ctx, f.actor, input); err == nil || result.Challenge != "" {
		t.Fatal("failed audit returned a grant")
	}
	f.audit.fail = false
	var count, revision int64
	if err := f.repo.db.WithContext(f.ctx).Raw(`SELECT COUNT(*),MAX(binding_version) FROM ai_personal_guests WHERE assistant_id=?`, f.assistant.ID).Row().Scan(&count, &revision); err != nil || count != 3 || revision != 3 {
		t.Fatalf("failed audit committed a registration: count=%d revision=%d err=%v", count, revision, err)
	}
	return latest
}
func testGuestPreparationRevocation(t *testing.T, f *personalVMFixture, service *appcopilot.PersonalGuestService, prepare *appcopilot.PersonalGuestPreparationService, input sohaapi.PersonalGuestPreparationInput, first, latest sohaapi.PersonalGuestPreparation) {

	if _, err := f.repo.DecidePersonalAgentAccess(f.ctx, f.actor.UserID, "test-admin", "revoked", 2); err != nil {
		t.Fatal(err)
	}
	if result, err := prepare.Prepare(f.ctx, f.actor, input); err == nil || result.Challenge != "" {
		t.Fatal("revoked owner prepared a guest")
	}
	last, err := f.repo.GetPersonalGuest(f.ctx, latest.BindingID.String())
	if err != nil {
		t.Fatal(err)
	}
	lastIdentity := networkidentity.Identity{Scope: networkidentity.ScopePersonalGuest, Kind: "guest", ID: last.Binding.ID, CertificateFingerprint: last.CertificateFingerprint}
	if _, err := service.Enroll(f.ctx, lastIdentity, "Bearer "+latest.Challenge, guest.Enrollment{SchemaVersion: guest.EnrollmentVersion, EnrollmentID: latest.EnrollmentID.String()}); err == nil {
		t.Fatal("revoked owner enrolled a guest")
	}
	current, err := f.repo.GetPersonalEnvironment(f.ctx, f.actor.UserID, f.assistant.ID)
	if err != nil || current.State != "pending" || current.DataInitialized || current.DataRef != f.bound.DataRef || current.ComputeGeneration != 1 || current.Version != f.bound.Version {
		t.Fatal("registration changed environment readiness or durable identity")
	}
	var secrets int64
	if err := f.repo.db.WithContext(f.ctx).Raw(`SELECT COUNT(*) FROM audit_logs WHERE metadata::text LIKE ?`, "%"+first.Challenge+"%").Row().Scan(&secrets); err != nil || secrets != 0 {
		t.Fatal("challenge leaked to durable audit")
	}
}

type preparationPGIssuer struct{}

func (preparationPGIssuer) Issue(_ context.Context, binding guest.Binding, _ string) (string, string, string, error) {
	digest := sha256.Sum256([]byte(binding.ID))
	return "public certificate", "public CA", fmt.Sprintf("sha256:%x", digest), nil
}
