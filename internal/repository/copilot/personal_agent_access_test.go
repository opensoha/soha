package copilot

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func testHeyaAccessDecisions(t *testing.T, repo *Repository, ctx context.Context, owner string) {
	t.Helper()
	absent, err := repo.GetPersonalAgentAccess(ctx, owner)
	if err != nil || absent.Status != "not_requested" || absent.Version != 0 {
		t.Fatalf("implicit activation: %+v %v", absent, err)
	}
	pending, err := repo.RequestPersonalAgentAccess(ctx, owner)
	if err != nil || pending.Status != "pending" || pending.Version != 1 || pending.RequestedAt == nil {
		t.Fatalf("request: %+v %v", pending, err)
	}
	again, err := repo.RequestPersonalAgentAccess(ctx, owner)
	if err != nil || again.Version != pending.Version {
		t.Fatalf("request not idempotent: %+v %v", again, err)
	}
	var approved atomic.Int64
	runInspectionConcurrently(t, func() error {
		_, err := repo.DecidePersonalAgentAccess(ctx, owner, "admin", "approved", pending.Version)
		if errors.Is(err, apperrors.ErrConflict) {
			return nil
		}
		if err == nil {
			approved.Add(1)
		}
		return err
	})
	if approved.Load() != 1 {
		t.Fatal("approval CAS allowed multiple decisions", approved.Load())
	}
	current, err := repo.GetPersonalAgentAccess(ctx, owner)
	if err != nil || current.Status != "approved" || current.Version != 2 || current.DecidedBy != "admin" || current.DecidedAt == nil {
		t.Fatalf("decision: %+v %v", current, err)
	}
	testHeyaAccessRevocation(t, repo, ctx, owner, current.Version)
}

func testHeyaAccessRevocation(t *testing.T, repo *Repository, ctx context.Context, owner string, approvedVersion int64) {
	t.Helper()
	if _, err := repo.DecidePersonalAgentAccess(ctx, "foreign", "admin", "approved", 1); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("approval without request: %v", err)
	}
	unchanged, err := repo.RequestPersonalAgentAccess(ctx, owner)
	if err != nil || unchanged.Status != "approved" || unchanged.Version != 2 {
		t.Fatalf("request changed approval: %+v %v", unchanged, err)
	}
	revoked, err := repo.DecidePersonalAgentAccess(ctx, owner, "admin", "revoked", approvedVersion)
	if err != nil || revoked.Status != "revoked" {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	reapplied, err := repo.RequestPersonalAgentAccess(ctx, owner)
	if err != nil || reapplied.Status != "pending" || reapplied.Version != 4 || reapplied.DecidedAt != nil || reapplied.DecidedBy != "" {
		t.Fatalf("reapply: %+v %v", reapplied, err)
	}
	items, err := repo.ListPersonalAgentAccess(ctx)
	if err != nil || len(items) != 1 || items[0].UserID != owner || items[0].Status != "pending" {
		t.Fatalf("request listing: %+v %v", items, err)
	}
}
