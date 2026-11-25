package assistantmesh_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	mesh "github.com/opensoha/soha/internal/repository/assistantmesh"
)

func TestDelegationAndInquiryCleanupWithPostgres(t *testing.T) {
	store := receptionPostgres(t)
	repo := mesh.New(store.DB())
	ctx := t.Context()
	owner, caller := uuid.NewString(), uuid.NewString()
	mustUser(t, ctx, store, owner, "active")
	mustUser(t, ctx, store, caller, "active")
	now := time.Now().UTC()
	if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, time.Hour); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	device := uuid.NewString()
	if err := repo.PutManagedDevice(ctx, domain.ManagedDevice{OwnerID: owner, DeviceID: device, SessionID: "fixture-session", ProofHash: digest}); err != nil {
		t.Fatal(err)
	}

	create := func(start time.Time) domain.DelegationRecord {
		t.Helper()
		item, err := repo.CreateDelegation(ctx, domain.DelegationRecord{
			RequestID: uuid.NewString(), Delegation: domain.Delegation{
				ID: uuid.NewString(), CallerID: caller, OwnerID: owner, Capability: domain.ManagedOSVersion,
				Field: "productVersion", InputHash: digest, Status: domain.DelegationAwaitingApproval,
				CreatedAt: start, ExpiresAt: start.Add(time.Hour),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}

	purged := create(now.Add(-9 * 24 * time.Hour))
	if err := store.Exec(ctx, `UPDATE assistant_mesh_delegations
		SET status = 'completed', result = '26.1', result_hash = $2, closed_at = expires_at WHERE id = $1`, purged.ID, digest); err != nil {
		t.Fatal(err)
	}
	expired := create(now.Add(-2 * time.Hour))
	approved := create(now.Add(-time.Minute))
	if err := store.Exec(ctx, `UPDATE assistant_mesh_delegations
		SET status = 'approved', ticket_hash = $2, ticket_expires_at = $3 WHERE id = $1`, approved.ID, digest, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	inquiry := pendingInquiry(caller, owner, "cleanup-regression", "key", now.Add(-10*24*time.Hour), now.Add(-9*24*time.Hour))
	if _, err := repo.CreateInquiry(ctx, inquiry, generousLimits()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetDelegation(ctx, expired.ID); err != nil || got.Status != domain.DelegationExpired {
		t.Fatalf("expired delegation = %v, %v", got.Status, err)
	}
	if got, err := repo.GetDelegation(ctx, purged.ID); err != nil || got.Status != domain.DelegationCompleted || got.Result != "" || got.ResultHash != "" {
		t.Fatalf("completed delegation retains protected data: %+v, %v", got, err)
	}
	if got, err := repo.GetDelegation(ctx, approved.ID); err != nil || got.Status != domain.DelegationApproved {
		t.Fatalf("expired ticket wrongly closes delegation: %v, %v", got.Status, err)
	}
	if got, err := repo.GetInquiry(ctx, caller, inquiry.RequestID); err != nil || got.Status != domain.InquiryExpired || got.Question != "" {
		t.Fatalf("B2 inquiry did not expire and purge: %+v, %v", got, err)
	}
}
