package copilot

import (
	"context"
	"errors"
	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	auditrepository "github.com/opensoha/soha/internal/repository/auditlog"
	"strings"
	"testing"
	"time"
)

type testRenewalIssuer struct{}

func (testRenewalIssuer) Issue(context.Context, guest.Binding, string) (string, string, string, error) {
	return "test-public-cert", "test-public-ca", "sha256:" + strings.Repeat("d", 64), nil
}

func testGuestRenewal(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) {
	heartbeat := registeredGuestHeartbeat(t, repo, ctx, item)
	if err := repo.RecordPersonalGuestHeartbeat(ctx, item, heartbeat, time.Now()); err != nil {
		t.Fatal(err)
	}
	audit := &guestFailingAudit{service: appaudit.New(auditrepository.New(repo.db), nil), fail: true}
	s, err := appcopilot.NewPersonalGuestService(repo, func(context.Context, guest.Binding) error { return nil }, audit)
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigureRenewal(testRenewalIssuer{})
	identity := networkidentity.Identity{Scope: networkidentity.ScopePersonalGuest, Kind: "guest", ID: item.Binding.ID, CertificateFingerprint: item.CertificateFingerprint}
	input := guest.Renewal{SchemaVersion: guest.RenewalVersion, RequestID: uuid.NewString(), BindingVersion: 1, CSRPEM: "signed-test-public-csr"}
	if _, err := s.Renew(ctx, identity, input); err == nil {
		t.Fatal("audit failure committed a renewal")
	}
	if _, err := repo.GetPersonalGuestRenewal(ctx, input.RequestID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("audit failure retained pending certificate", err)
	}
	audit.fail = false
	result, err := s.Renew(ctx, identity, input)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := s.Renew(ctx, identity, input)
	if err != nil || repeat != result {
		t.Fatal("lost renewal response is not recoverable", err)
	}
	next := identity
	next.CertificateFingerprint = "sha256:" + strings.Repeat("d", 64)
	commits := make(chan error, 2)
	for range 2 {
		go func() { _, err := s.CommitRenewal(ctx, next, input.RequestID); commits <- err }()
	}
	for range 2 {
		if err := <-commits; err != nil {
			t.Fatal("concurrent lost commit recovery failed", err)
		}
	}
	if _, err := s.CommitRenewal(ctx, next, input.RequestID); err != nil {
		t.Fatal("lost commit response is not recoverable", err)
	}
	current, err := repo.GetPersonalGuest(ctx, item.Binding.ID)
	if err != nil || current.LastSequence != 1 || current.Binding.Version != 2 || current.Validate() != nil {
		t.Fatal("renewal reset sequence or corrupted enrollment history", err)
	}
	if _, err := s.Binding(ctx, identity); err == nil {
		t.Fatal("old certificate survived promotion")
	}
	input.RequestID, input.BindingVersion = uuid.NewString(), 2
	if _, err := s.Renew(ctx, next, input); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DecidePersonalAgentAccess(ctx, item.Binding.OwnerID, "test-admin", "revoked", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitRenewal(ctx, next, input.RequestID); err == nil {
		t.Fatal("revoked owner promoted pending certificate")
	}
}
