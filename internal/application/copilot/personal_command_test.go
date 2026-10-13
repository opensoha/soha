package copilot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalCleanupStore struct {
	PersonalEnvironmentStore
	item domain.PersonalEnvironmentRecord
}

func (s *personalCleanupStore) GetPersonalEnvironment(_ context.Context, owner, assistant string) (domain.PersonalEnvironmentRecord, error) {
	if owner != s.item.CreatedBy || assistant != s.item.AssistantID {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrNotFound
	}
	return s.item, nil
}
func (s *personalCleanupStore) ReservePersonalEnvironment(_ context.Context, previous domain.PersonalEnvironmentRecord, lease contractenv.Lease, id string, create bool) (domain.PersonalEnvironmentRecord, error) {
	if previous.Version != s.item.Version || lease.ID != s.item.Lease.ID || create {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrConflict
	}
	s.item.State, s.item.OperationID = "pending", id
	s.item.Version++
	until := time.Now().Add(70 * time.Second)
	s.item.OperationUntil = &until
	return s.item, nil
}
func (s *personalCleanupStore) CompletePersonalEnvironment(_ context.Context, item domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	if item.Version != s.item.Version || item.OperationID != s.item.OperationID {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrConflict
	}
	item.Version++
	if item.State != "unknown" {
		item.OperationID, item.OperationUntil = "", nil
	}
	if item.State == "sleeping" || item.State == "missing" || item.State == "expired" {
		item.ActiveTaskID = ""
	}
	s.item = item
	return item, nil
}

type personalCleanupParentStore struct{ *nativeTaskStoreFixture }

func (s *personalCleanupParentStore) WithNativeTaskParent(ctx context.Context, _ domain.NativeTaskParent, cancellation bool, execute func(context.Context) error) error {
	if !cancellation || s.lease.Phase != "cancelling" {
		return errors.New("personal cleanup bypassed the persisted Native stop fence")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return execute(ctx)
}

func TestNativePersonalCleanupKeepsOriginalTaskLeaseAndIndependentBudget(t *testing.T) {
	for _, scenario := range []string{"complete", "failed", "canceled", "unknown_then_inspect", "other_task", "cleanup_budget_exhausted"} {
		t.Run(scenario, func(t *testing.T) {
			service, loop, _, _ := nativeLoopFixture(t)
			assistant, err := service.EnsurePersonalAssistant(t.Context(), domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}, "en-US")
			if err != nil {
				t.Fatal(err)
			}
			capture := &nativeQueueCapture{nativeLoopStore: loop, inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}}
			service.agentRuns = &personalCleanupParentStore{nativeTaskStoreFixture: &nativeTaskStoreFixture{nativeQueueCapture: capture}}
			cp := initialNativeCheckpoint(loop.lease.Run)
			cp.Usage.ToolCalls = 1 // Acquiring the owned task lease consumed a work call.
			loop.lease.ToolUsage.WorkCalls = 1
			// Cleanup still runs after the normal work deadline.
			cp.Budget.Deadline = time.Now().Add(-time.Second)
			now, leaseID := time.Now().UTC(), uuid.NewString()
			store := &personalCleanupStore{item: domain.PersonalEnvironmentRecord{AssistantID: assistant.ID, CreatedBy: "reader", Backend: "container", Provider: "docker", PolicyRevision: "legacy-docker-v1", DataRef: "personal-data:" + assistant.ID, ComputeRef: leaseID, ComputeGeneration: 1, Version: 1, State: "ready", DataInitialized: true, DataCreationAttempted: true, ActiveTaskID: domain.PersonalTaskID(loop.lease.Run.ID), Lease: contractenv.Lease{SchemaVersion: contractenv.LeaseVersion, ID: leaseID, RunID: leaseID, OwnerID: assistant.ID, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}}}
			calls := 0
			service.personalEnvironment = &PersonalEnvironmentDependencies{Store: store, Authorize: func(context.Context, domainidentity.Principal, string) error { return nil }, Invoke: personalCleanupInvoker(t, loop, scenario, leaseID, &calls)}
			status := "completed"
			if scenario == "failed" || scenario == "canceled" {
				status = scenario
			}
			if scenario == "other_task" {
				store.item.ActiveTaskID = uuid.NewString()
			}
			if scenario == "cleanup_budget_exhausted" {
				loop.lease.ToolUsage.CleanupCalls = domain.NativeCleanupToolCallLimit
			}
			if err := service.finishNativeRun(t.Context(), loop, loop.lease, cp, status, "fixture_stop", "original answer"); err != nil {
				t.Fatal(err)
			}
			if scenario == "cleanup_budget_exhausted" {
				if calls != 0 || len(loop.final) != 0 || loop.lease.Phase != "cancelling" {
					t.Fatal("exhausted cleanup budget dispatched or claimed completion")
				}
				return
			}
			if scenario == "unknown_then_inspect" {
				if calls != 1 || len(loop.final) != 0 || loop.lease.Phase != "cancelling" || store.item.State != "unknown" {
					t.Fatal("unknown cleanup became a final result")
				}
				expired := time.Now().Add(-time.Second)
				store.item.OperationUntil = &expired
				if err := service.processNativeRun(t.Context(), loop, loop.lease); err != nil {
					t.Fatal(err)
				}
			}
			assertPersonalCleanupFinal(t, loop, store, scenario, status, calls)
		})
	}
}

func TestNativeChatFinishesWithoutPersonalEnvironmentCleanup(t *testing.T) {
	for _, status := range []string{"completed", "failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			service, loop, _, _ := nativeLoopFixture(t)
			// Any environment access would panic: no tool has acquired a task lease.
			service.personalEnvironment = &PersonalEnvironmentDependencies{}
			if status == "failed" {
				service.agentPrincipals = nil
			}
			cp := initialNativeCheckpoint(loop.lease.Run)
			cp.Usage.Steps = 1
			if err := service.finishNativeRun(t.Context(), loop, loop.lease, cp, status, "fixture_stop", "chat result"); err != nil || loop.lease.Run.Status != status || len(loop.final) != 1 {
				t.Fatalf("chat did not reach its terminal result: status=%s messages=%d err=%v", loop.lease.Run.Status, len(loop.final), err)
			}
		})
	}
}

func personalCleanupInvoker(t *testing.T, loop *nativeLoopStore, scenario, leaseID string, calls *int) func(context.Context, domainidentity.Principal, string, contractenv.Request) (contractenv.Result, error) {
	t.Helper()
	return func(ctx context.Context, _ domainidentity.Principal, _ string, request contractenv.Request) (contractenv.Result, error) {
		*calls++
		if loop.lease.Phase != "cancelling" || loop.lease.ToolUsage.CleanupCalls != *calls || request.Lease.ID != leaseID || request.CreateData || len(request.Argv) != 0 || ctx.Err() != nil {
			t.Fatal("cleanup replayed work, changed lease or used exhausted parent deadline")
		}
		if scenario == "unknown_then_inspect" && *calls == 1 {
			return contractenv.Result{}, context.DeadlineExceeded
		}
		if (*calls == 1 && request.Action != "release") || (*calls == 2 && request.Action != "inspect") {
			t.Fatal("unknown cleanup was reissued instead of inspected")
		}
		return contractenv.Result{SchemaVersion: contractenv.ResultVersion, LeaseID: leaseID, RunID: leaseID, State: "released", ObservedAt: time.Now().UTC()}, nil
	}
}

func assertPersonalCleanupFinal(t *testing.T, loop *nativeLoopStore, store *personalCleanupStore, scenario, status string, calls int) {
	t.Helper()
	if len(loop.final) != 1 || loop.lease.Run.Status != status || loop.final[0].Content != "original answer" || (scenario == "other_task" && calls != 0) || (scenario != "other_task" && (store.item.ActiveTaskID != "" || !store.item.DataInitialized)) {
		t.Fatalf("cleanup lost final status/content, task isolation or data: status=%s calls=%d final=%+v", loop.lease.Run.Status, calls, loop.final)
	}
}
