package copilot

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	contractenv "github.com/opensoha/soha/contracts/agent/environment"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalCleanupStore struct {
	PersonalEnvironmentStore
	item domain.PersonalEnvironmentRecord
}

func (s *personalCleanupStore) FindPersonalTaskEnvironment(_ context.Context, owner, task string) (domain.PersonalEnvironmentRecord, error) {
	if owner != s.item.CreatedBy || task != s.item.ActiveTaskID {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrNotFound
	}
	return s.item, nil
}

type noPersonalEnvironmentStore struct {
	PersonalEnvironmentStore
	err error
}

func (s noPersonalEnvironmentStore) FindPersonalTaskEnvironment(context.Context, string, string) (domain.PersonalEnvironmentRecord, error) {
	return domain.PersonalEnvironmentRecord{}, s.err
}

func TestNativeDomainTaskWithoutPersonalLeaseCanFinish(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			service, loop, _, _ := nativeLoopFixture(t)
			capture := &nativeQueueCapture{nativeLoopStore: loop, inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}}
			service.nativeTaskParents = &personalCleanupParentStore{nativeTaskStoreFixture: &nativeTaskStoreFixture{nativeQueueCapture: capture}}
			cause := apperrors.ErrNotFound
			if !missing {
				cause = apperrors.ErrAccessDenied
			}
			service.personalEnvironment = &PersonalEnvironmentDependencies{Store: noPersonalEnvironmentStore{err: cause}}
			// Any attempt to activate Personal Agent or create an assistant panics.
			service.sessions = nil
			cp := initialNativeCheckpoint(loop.lease.Run)
			cp.Usage.ToolCalls = 3
			if err := service.finishNativeRun(t.Context(), loop, loop.lease, cp, "completed", "answer_accepted", "verified result"); err != nil {
				t.Fatal(err)
			}
			if missing && (loop.lease.Run.Status != "completed" || len(loop.final) != 1) {
				t.Fatal("unrelated Personal Agent activation blocked completion")
			}
			if !missing && (len(loop.final) != 0 || loop.lease.Phase != "cancelling") {
				t.Fatal("ambiguous lease lookup claimed cleanup")
			}
		})
	}
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
