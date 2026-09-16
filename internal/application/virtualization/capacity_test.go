package virtualization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type capacityReaderAdapter struct {
	fakeAdapter
	observed domain.CapacitySnapshot
	err      error
	reads    int
}

func (a *capacityReaderAdapter) ObserveCapacity(context.Context, domain.AdapterConnection, domain.AdapterCreateVMInput) (domain.CapacitySnapshot, error) {
	a.reads++
	return a.observed, a.err
}

type capacityReaderRepo struct {
	*memoryRepo
	demand domain.CapacityDemand
	err    error
}

func (r *capacityReaderRepo) SelectCapacity(_ context.Context, _ domain.CapacitySnapshot, demand domain.CapacityDemand) (domain.CapacityNode, domain.CapacityStorage, error) {
	r.demand = demand
	return domain.CapacityNode{Name: "node-a"}, domain.CapacityStorage{Name: "disk-a"}, r.err
}

func TestCapacityCheckReportsEvidenceAndNeverCreatesTask(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
	adapter := &capacityReaderAdapter{observed: domain.CapacitySnapshot{SourceID: "physical", ObservedAt: time.Now().UTC(), Complete: true}}
	service := newTestService(repo, &captureOperations{}, adapter)
	reader := &capacityReaderRepo{memoryRepo: repo}
	service.tasks = reader
	input := sohaapi.VirtualizationCapacityInput{ConnectionID: connection.ID, CPU: 2, MemoryMiB: 2048, DiskGiB: 20}
	result, err := service.CheckCapacity(context.Background(), testPrincipal(), input)
	if err != nil || result.Status != sohaapi.VirtualizationCapacityResultStatusAvailable || result.Node != "node-a" || result.Storage != "disk-a" || result.ValidUntil.Sub(*result.ObservedAt) != 30*time.Second || reader.demand.MemoryMiB != 2560 {
		t.Fatalf("capacity result: %+v demand=%+v err=%v", result, reader.demand, err)
	}
	reader.err = apperrors.ErrConflict
	result, err = service.CheckCapacity(context.Background(), testPrincipal(), input)
	if err != nil || result.Status != sohaapi.VirtualizationCapacityResultStatusUnavailable {
		t.Fatalf("exhausted: %+v %v", result, err)
	}
	reader.err = domain.ErrCapacityUnknown
	result, err = service.CheckCapacity(context.Background(), testPrincipal(), input)
	if err != nil || result.Status != sohaapi.VirtualizationCapacityResultStatusUnknown {
		t.Fatalf("unknown: %+v %v", result, err)
	}
	adapter.err = errors.New("sensitive-provider-response")
	result, err = service.CheckCapacity(context.Background(), testPrincipal(), input)
	if err != nil || result.Status != sohaapi.VirtualizationCapacityResultStatusUnknown || result.Reason == adapter.err.Error() {
		t.Fatalf("provider failure: %+v %v", result, err)
	}
	if len(repo.tasks) != 0 {
		t.Fatal("capacity check created tasks")
	}
	input.CPU = 0
	reads := adapter.reads
	if _, err := service.CheckCapacity(context.Background(), testPrincipal(), input); !errors.Is(err, apperrors.ErrInvalidArgument) || adapter.reads != reads {
		t.Fatalf("invalid demand contacted provider: %v", err)
	}
	input.CPU = 2
	denied := domain.WithScopeCheck(context.Background(), func(map[string]string) error { return apperrors.ErrConflict })
	if _, err := service.CheckCapacity(denied, testPrincipal(), input); !errors.Is(err, apperrors.ErrConflict) || adapter.reads != reads {
		t.Fatalf("changed scope contacted provider: %v", err)
	}
}

func TestVMCreationReceiptRecoveryAndScopeGuard(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderKubeVirt, Enabled: true, DefaultNamespace: "team-a"})
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})
	input := CreateVMInput{ConnectionID: connection.ID, Name: "worker", DiskGiB: 10, IdempotencyKey: "recover-worker-1"}
	original, err := service.CreateVM(context.Background(), testPrincipal(), input)
	if err != nil {
		t.Fatal(err)
	}
	if original.Payload["namespace"] != "team-a" {
		t.Fatal("namespace default not frozen")
	}
	recovered, err := service.FindVMCreation(context.Background(), testPrincipal(), input)
	if err != nil || recovered.ID != original.ID || len(repo.tasks) != 1 {
		t.Fatalf("receipt recovery: %+v %v", recovered, err)
	}
	input.CPU = 4
	if _, err := service.FindVMCreation(context.Background(), testPrincipal(), input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("different input adopted old receipt: %v", err)
	}
	denied := domain.WithScopeCheck(context.Background(), func(map[string]string) error { return apperrors.ErrConflict })
	input.IdempotencyKey = "new-worker-blocked"
	if _, err := service.CreateVM(denied, testPrincipal(), input); !errors.Is(err, apperrors.ErrConflict) || len(repo.tasks) != 1 {
		t.Fatalf("changed scope enqueued VM: %v", err)
	}
	if _, err := service.CancelOperation(denied, testPrincipal(), original.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("cancel changed scope: %v", err)
	}
	if _, err := service.GetOperation(denied, testPrincipal(), original.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("read changed scope: %v", err)
	}
}

func TestVMOperationMutationRecoveryAndCurrentRetryPermission(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})
	task, err := service.CreateVM(context.Background(), testPrincipal(), CreateVMInput{ConnectionID: connection.ID, Name: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	input := OperationMutationInput{IdempotencyKey: "cancel-worker-1", Reason: "stop"}
	if _, err := service.FindOperationMutation(context.Background(), testPrincipal(), task.ID, "cancel", input); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("invented mutation receipt: %v", err)
	}
	canceled, err := service.CancelOperationIdempotent(context.Background(), testPrincipal(), task.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := service.FindOperationMutation(context.Background(), testPrincipal(), task.ID, "cancel", input)
	if err != nil || recovered.ID != canceled.ID || !recovered.UpdatedAt.Equal(canceled.UpdatedAt) {
		t.Fatalf("recovery changed task: %+v %v", recovered, err)
	}
	input.Reason = "different"
	if _, err := service.FindOperationMutation(context.Background(), testPrincipal(), task.ID, "cancel", input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("different mutation recovered: %v", err)
	}
	service.permissions = appaccess.NewPermissionResolver(testRoleReader{matrix: map[string][]string{"admin": {appaccess.PermVirtualizationOperationsManage, appaccess.PermVirtualizationOperationsView}}})
	if _, err := service.RetryOperation(context.Background(), testPrincipal(), task.ID); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("retry bypassed revoked VM create permission: %v", err)
	}
	current, _ := repo.GetTask(context.Background(), task.ID)
	if current.Status != TaskStatusCanceled {
		t.Fatal("denied retry changed original task")
	}
}

type executionPrincipalFunc func(context.Context, string, string) (domainidentity.Principal, error)

func (f executionPrincipalFunc) CurrentExecutionPrincipal(ctx context.Context, actor, token string) (domainidentity.Principal, error) {
	return f(ctx, actor, token)
}

func TestVMWorkerRechecksExecutionIdentityBeforeDispatch(t *testing.T) {
	for _, duringPreparation := range []bool{false, true} {
		repo := newMemoryRepo()
		connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
		adapter := &recoveryAdapter{}
		service := newTestService(repo, &captureOperations{}, adapter)
		principal := testPrincipal()
		principal.AccessTokenID = "original-token"
		task, err := service.CreateVM(context.Background(), principal, CreateVMInput{ConnectionID: connection.ID, Name: "worker"})
		if err != nil {
			t.Fatal(err)
		}
		revoked := !duringPreparation
		service.executionPrincipals = executionPrincipalFunc(func(_ context.Context, actor, token string) (domainidentity.Principal, error) {
			if actor != principal.UserID || token != principal.AccessTokenID {
				t.Fatalf("lost execution identity: %s %s", actor, token)
			}
			if revoked {
				return domainidentity.Principal{}, apperrors.ErrUnauthorized
			}
			return principal, nil
		})
		adapter.onPrepare = func() { revoked = true }
		adapter.create = func(domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
			t.Fatal("revoked task reached provider write")
			return domain.AdapterVM{}, nil
		}
		claimed, err := repo.ClaimTask(context.Background(), "worker", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		service.executeTask(context.Background(), claimed)
		current, _ := repo.GetTask(context.Background(), task.ID)
		if current.Status != TaskStatusFailed || current.Result["providerEffect"] != "not_started" || boolValue(current.Payload, "providerDispatchStarted") {
			t.Fatalf("revoked task not fenced: %+v", current)
		}
	}
}
