package copilot

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	env "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func runtimePGFixture(t *testing.T, repo *Repository, ctx context.Context) (domain.PersonalEnvironmentRecord, guest.RuntimeOperation) {
	t.Helper()
	g := seedPersonalGuestWithDiskQuota(t, repo, ctx, 6<<30)
	item, err := repo.GetPersonalEnvironment(ctx, g.Binding.OwnerID, g.Binding.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	plan := guest.DataPlan{SchemaVersion: guest.DataPlanVersion, OperationID: uuid.NewString(), Binding: g.Binding, DiskQuotaBytes: item.Lease.Quota.DiskBytes}
	if err = plan.Validate(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = repo.ReservePersonalGuestData(ctx, item, plan.OperationID); err != nil {
		t.Fatal(err)
	}
	if err = repo.GuestDataCheckpoint(ctx, plan, "complete", guest.DataResult{DataRef: g.Binding.DataRef, FilesystemID: g.Binding.DataRef, DiskQuota: plan.DiskQuotaBytes}); err != nil {
		t.Fatal(err)
	}
	item, err = repo.GetPersonalEnvironment(ctx, item.CreatedBy, item.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	item, err = repo.ReservePersonalEnvironment(ctx, item, item.Lease, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	item.State = "ready"
	item, err = repo.CompletePersonalEnvironment(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	op := guest.RuntimeOperation{SchemaVersion: guest.RuntimeOperationVersion, OperationID: uuid.NewString(), Binding: g.Binding, Kind: "environment", Deadline: time.Now().UTC().Add(50 * time.Second), Request: env.Request{SchemaVersion: env.RequestVersion, Action: "execute", TaskID: uuid.NewString(), Lease: item.Lease, Argv: []string{"/usr/bin/true"}}}
	op.ArtifactSHA256 = g.Binding.ProviderIdentityHash[len("sha256:"):]
	return item, op
}
func runtimePGReceipt(op guest.RuntimeOperation) guest.RuntimeResult {
	now := time.Now().UTC()
	code := 7
	output := "command failed"
	return guest.RuntimeResult{SchemaVersion: guest.RuntimeResultVersion, OperationID: op.OperationID, BindingID: op.Binding.ID, Environment: env.Result{SchemaVersion: env.ResultVersion, LeaseID: op.Request.Lease.ID, RunID: op.Request.Lease.RunID, TaskID: op.Request.TaskID, State: "active", ObservedAt: now, ExitCode: &code, Output: &output, Snapshot: &env.Snapshot{SchemaVersion: env.SnapshotVersion, LeaseID: op.Request.Lease.ID, RunID: op.Request.Lease.RunID, State: "active", ObservedAt: now}}}
}
func TestPersonalRuntimeWithPostgres(t *testing.T) {
	repo, ctx := retainedPersonalGuestRepository(t)
	t.Run("cas_owner_and_generation", func(t *testing.T) { testRuntimePGCasOwnerAndGeneration(t, repo, ctx) })
	t.Run("receipt_and_late_callback", func(t *testing.T) { testRuntimePGReceiptAndLateCallback(t, repo, ctx) })
	t.Run("unknown_and_expired_recovery", func(t *testing.T) { testRuntimePGUnknownAndExpiredRecovery(t, repo, ctx) })
	t.Run("browser_generation_fence", func(t *testing.T) { testRuntimePGBrowserGenerationFence(t, repo, ctx) })
}

func testRuntimePGCasOwnerAndGeneration(t *testing.T, repo *Repository, ctx context.Context) {
	item, op := runtimePGFixture(t, repo, ctx)
	foreign := item
	foreign.CreatedBy = uuid.NewString()
	if _, err := repo.ReservePersonalRuntime(ctx, foreign, op); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("foreign owner reserved", err)
	}
	var wins atomic.Int32
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			if _, err := repo.ReservePersonalRuntime(ctx, item, op); err == nil {
				wins.Add(1)
			} else if !errors.Is(err, apperrors.ErrConflict) {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if wins.Load() != 1 {
		t.Fatal("CAS dispatched more than once")
	}
	other := op
	other.Binding.ComputeGeneration++
	if repo.AuthorizePersonalGuestRuntime(ctx, other) == nil {
		t.Fatal("stale compute generation allowed")
	}
	if err := repo.AuthorizePersonalGuestRuntime(ctx, op); err != nil {
		t.Fatal(err)
	}
}

func testRuntimePGReceiptAndLateCallback(t *testing.T, repo *Repository, ctx context.Context) {
	item, op := runtimePGFixture(t, repo, ctx)
	if _, err := repo.ReservePersonalRuntime(ctx, item, op); err != nil {
		t.Fatal(err)
	}
	result := runtimePGReceipt(op)
	if err := repo.CompletePersonalRuntime(ctx, op, result, true); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetPersonalEnvironment(ctx, item.CreatedBy, item.AssistantID)
	if err != nil || current.State != "ready" || current.ActiveTaskID != "" || current.OperationID != "" {
		t.Fatal("confirmed nonzero process exit made environment unusable", err)
	}
	next := op
	next.OperationID = uuid.NewString()
	reserved, err := repo.ReservePersonalRuntime(ctx, current, next)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CompletePersonalRuntime(ctx, op, result, true); err != nil {
		t.Fatal(err)
	}
	latest, err := repo.GetPersonalEnvironment(ctx, item.CreatedBy, item.AssistantID)
	if err != nil || latest.Version != reserved.Version || latest.OperationID != next.OperationID {
		t.Fatal("late callback replaced new operation", err)
	}
}

func testRuntimePGPrivilegedReservation(t *testing.T, repo *Repository, ctx context.Context) {
	protected, protectedOp := runtimePGFixture(t, repo, ctx)
	if _, err := repo.ReservePersonalRuntime(ctx, protected, protectedOp); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompletePersonalRuntime(ctx, protectedOp, guest.RuntimeResult{}, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.WithContext(ctx).Exec(`UPDATE ai_personal_environments SET operation_until=now()-interval '1 second' WHERE assistant_id=?`, protected.AssistantID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.db.WithContext(ctx).Exec(`INSERT INTO virtualization_tasks(id,provider,task_kind,status) VALUES(?,'pve','personal_guest_tools','running')`, protectedOp.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	protected, err := repo.GetPersonalEnvironment(ctx, protected.CreatedBy, protected.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RecoverTerminalPersonalRuntime(ctx, protected); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("privileged dispatch was cleared as a missing SSH task", err)
	}
}

func testRuntimePGUnknownAndExpiredRecovery(t *testing.T, repo *Repository, ctx context.Context) {
	item, op := runtimePGFixture(t, repo, ctx)
	if _, err := repo.ReservePersonalRuntime(ctx, item, op); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompletePersonalRuntime(ctx, op, guest.RuntimeResult{}, false); err != nil {
		t.Fatal(err)
	}
	unknown, err := repo.GetPersonalEnvironment(ctx, item.CreatedBy, item.AssistantID)
	if err != nil || unknown.State != "unknown" || unknown.OperationID != op.OperationID {
		t.Fatal("unknown dispatch lost original identity", err)
	}
	if _, err = repo.RecoverTerminalPersonalRuntime(ctx, unknown); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("recovered before dispatch deadline", err)
	}
	// Advance only the isolated fixture's bounded dispatch clock.
	if err = repo.db.WithContext(ctx).Exec(`UPDATE ai_personal_environments SET operation_until=now()-interval '1 second' WHERE assistant_id=?`, item.AssistantID).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := repo.RecoverTerminalPersonalRuntime(ctx, unknown)
	if err != nil || recovered.State != "unknown" || recovered.OperationID != "" || recovered.ActiveTaskID != "" {
		t.Fatal("terminal recovery claimed ready or retained stale dispatch", err)
	}
	if _, err = repo.RecoverTerminalPersonalRuntime(ctx, unknown); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("replayed recovery accepted", err)
	}
	testRuntimePGPrivilegedReservation(t, repo, ctx)

}

func testRuntimePGBrowserGenerationFence(t *testing.T, repo *Repository, ctx context.Context) {
	item, op := runtimePGFixture(t, repo, ctx)
	first, err := repo.ReservePersonalBrowser(ctx, item, guest.BrowserAction{Action: "acquire", Owner: "user"})
	if err != nil {
		t.Fatal(err)
	}
	op.Kind = "browser"
	op.Request.Action = "inspect"
	op.Request.Argv = nil
	op.Request.TaskID = ""
	op.Browser = &first
	if repo.CheckPersonalBrowserOperation(ctx, op) != nil {
		t.Fatal("current browser control refused")
	}
	next := first
	next.Action = "takeover"
	next, err = repo.ReservePersonalBrowser(ctx, item, next)
	if err != nil || next.Generation != first.Generation+1 {
		t.Fatal("handover did not fence earlier actions", err)
	}
	if repo.CheckPersonalBrowserOperation(ctx, op) == nil {
		t.Fatal("late browser action retained authority")
	}
	if _, err = repo.ReservePersonalBrowser(ctx, item, first); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("old control reacquired", err)
	}
}
