package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workerExecutionAdapter struct {
	recoveryAdapter
	prepareError error
	reads        int
	found        bool
}

func (a *workerExecutionAdapter) PrepareVMCreate(ctx context.Context, connection domain.AdapterConnection, input domain.AdapterCreateVMInput) (domain.AdapterCreateVMInput, error) {
	if a.prepareError != nil {
		return input, a.prepareError
	}
	return a.recoveryAdapter.PrepareVMCreate(ctx, connection, input)
}
func (a *workerExecutionAdapter) ObserveVMCreation(_ context.Context, _ domain.AdapterConnection, input domain.AdapterCreateVMInput) (domain.AdapterVM, bool, error) {
	a.reads++
	return domain.AdapterVM{ID: "702", Name: input.Name, Node: input.Node}, a.found, nil
}

func (*workerExecutionAdapter) InspectWorkerTemplate(context.Context, domain.AdapterConnection, string, string, int) (string, error) {
	return "template-fingerprint", nil
}
func (*workerExecutionAdapter) ObserveCapacity(context.Context, domain.AdapterConnection, domain.AdapterCreateVMInput) (domain.CapacitySnapshot, error) {
	return domain.CapacitySnapshot{}, nil
}

type workerExecutionTasks struct {
	*memoryRepo
	admissions int
	demand     domain.CapacityDemand
}

func (r *workerExecutionTasks) CreateTaskWithCapacity(ctx context.Context, task domain.Task, _ domain.CapacitySnapshot, demand domain.CapacityDemand) (domain.Task, error) {
	r.admissions++
	r.demand = demand
	task.Payload["capacityReserved"] = true
	return r.CreateTask(ctx, task)
}
func (r *workerExecutionTasks) RetryTaskWithCapacity(ctx context.Context, task domain.Task, _ domain.CapacitySnapshot, _ domain.CapacityDemand) (domain.Task, error) {
	return r.UpdateTask(ctx, task)
}

func (r *workerExecutionTasks) RecheckTaskWithCapacity(ctx context.Context, task domain.Task, _ domain.CapacitySnapshot, _ domain.CapacityDemand) (domain.Task, error) {
	task.Payload["providerDispatchStarted"] = true
	return r.UpdateTask(ctx, task)
}

func (r *workerPoolStore) GetWorkerPool(_ context.Context, id string) (domain.WorkerPool, error) {
	if r.saved.ID.String() != id {
		return domain.WorkerPool{}, apperrors.ErrNotFound
	}
	return r.saved, nil
}
func (r *workerPoolStore) WithWorkerPoolAdmission(ctx context.Context, id string, revision int, _ string, apply func(context.Context) error) error {
	if id != r.saved.ID.String() || revision != r.saved.Revision || !r.saved.Spec.Enabled {
		return apperrors.ErrConflict
	}
	return apply(ctx)
}

type workerExecutionFixture struct {
	service                                  *Service
	repo                                     *workerExecutionTasks
	pool                                     *workerPoolStore
	adapter                                  *workerExecutionAdapter
	bootstraps, creates, claims, revocations int
	ready                                    bool
	observeError, revokeError                error
	createError                              error
	afterBootstrap, afterCreate              func(string)
}

func newWorkerExecutionFixture(t *testing.T) *workerExecutionFixture {
	t.Helper()
	memory := newMemoryRepo()
	connection := memory.addConnection(domain.Connection{ID: "pve", Provider: ProviderPVE, Endpoint: "https://pve.example", Enabled: true, VerifyTLS: true})
	memory.images["image"] = domain.Image{ID: "image", Provider: ProviderPVE, ConnectionID: connection.ID, ExternalID: "101"}
	f := &workerExecutionFixture{repo: &workerExecutionTasks{memoryRepo: memory}, adapter: &workerExecutionAdapter{}, ready: true}
	f.service = newTestService(memory, &captureOperations{}, f.adapter)
	permissions, err := appaccess.RuntimePermissionKeys(context.Background(), testPermissions(), testPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	f.service.permissions = appaccess.NewPermissionResolver(testRoleReader{matrix: map[string][]string{"admin": append(permissions, appaccess.PermPlatformClustersView, appaccess.PlatformActionPermission("", "Node", "create"), appaccess.PlatformActionPermission("", "Node", "view"))}})
	f.service.tasks = f.repo
	f.service.authorizeWorkerCluster = func(context.Context, domainidentity.Principal, string, bool) error { return nil }
	spec := sohaapi.VirtualizationWorkerPoolSpec{Name: "pool", ConnectionID: "pve", ClusterID: "target", Owner: "soha-kubeadm", ImageID: "image", ProviderNode: "pve-a", Storage: "disk", Bridge: "vmbr0", SnippetStorage: "local", OsProfile: "ubuntu-24.04-amd64-containerd", KubernetesVersion: "v1.35.2", CPU: 4, MemoryMiB: 8192, DiskGiB: 80, MaxNodes: 2, Enabled: true, RequiredDaemonSets: []sohaapi.VirtualizationWorkerDaemonSet{{Namespace: "kube-system", Name: "network"}}}
	f.pool = &workerPoolStore{saved: domain.WorkerPool{VirtualizationWorkerPool: sohaapi.VirtualizationWorkerPool{ID: uuid.New(), Revision: 1, Spec: spec}, Identity: domain.WorkerPoolIdentity{ClusterUID: "cluster-uid", ConnectionIdentity: vmCreateConnectionIdentity(connection), TemplateID: "101", TemplateFingerprint: "template-fingerprint"}}}
	f.service.workerPools = f.pool
	f.service.workerRuntime = &WorkerRuntime{
		ValidatePoolSpec: k8sinfra.ValidateWorkerPoolKubernetesSpec,
		PrepareBootstrap: func(_ context.Context, _ domain.WorkerPool, operation, node string, deadline time.Time) (domain.WorkerBootstrap, error) {
			f.bootstraps++
			if node != domain.WorkerNodeName(operation) {
				t.Fatal("node name detached from original operation")
			}
			if f.afterBootstrap != nil {
				f.afterBootstrap(operation)
			}
			return domain.WorkerBootstrap{CloudInit: "#cloud-config\nsecret-worker-token", SecretUID: "secret-uid", ExpiresAt: deadline}, nil
		},
		Observe: func(_ context.Context, _ domain.WorkerPool, id string) (domain.WorkerObservation, error) {
			return domain.WorkerObservation{Ready: f.ready, NodeName: domain.WorkerNodeName(id), NodeUID: "node-uid", ObservedAt: time.Now(), ValidUntil: time.Now().Add(30 * time.Second)}, f.observeError
		},
		ClaimNode:       func(context.Context, domain.WorkerPool, string) error { f.claims++; return nil },
		RevokeBootstrap: func(context.Context, domain.WorkerPool, string, string) error { f.revocations++; return f.revokeError },
	}
	f.adapter.create = func(input domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
		f.creates++
		if !input.CapacityReserved || input.WorkerSystemUUID != input.OperationID || input.CloudInit != "#cloud-config\nsecret-worker-token" {
			t.Fatal("worker VM lost capacity, identity or sealed bootstrap")
		}
		if f.afterCreate != nil {
			f.afterCreate(input.OperationID)
		}
		return domain.AdapterVM{ID: "702", Name: input.Name, Node: input.Node}, f.createError
	}
	return f
}

func (f *workerExecutionFixture) enqueue(t *testing.T) domain.Task {
	t.Helper()
	input := sohaapi.VirtualizationWorkerCreateInput{PoolRevision: 1, IdempotencyKey: "worker-idempotency-1"}
	task, err := f.service.CreateWorker(context.Background(), testPrincipal(), f.pool.saved.ID.String(), input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.service.CreateWorker(context.Background(), testPrincipal(), f.pool.saved.ID.String(), input)
	if err != nil || task.ID != replay.ID || f.repo.admissions != 1 || f.bootstraps != 0 || f.creates != 0 || f.repo.demand.DiskGiB != 81 {
		t.Fatalf("enqueue/replay allocated effects: %v", err)
	}
	return task
}

func (f *workerExecutionFixture) run(t *testing.T, ctx context.Context) domain.Task {
	t.Helper()
	task, err := f.repo.ClaimTask(context.Background(), "worker", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.service.executeTask(ctx, task)
	current, err := f.repo.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(current)
	if strings.Contains(string(encoded), "secret-worker-token") {
		t.Fatal("worker bootstrap leaked into durable task")
	}
	return current
}

func TestWorkerCreationReadinessAndOriginalVMRecovery(t *testing.T) {
	f := newWorkerExecutionFixture(t)
	original := f.enqueue(t)
	f.observeError = errors.New("cluster observation unavailable")
	failed := f.run(t, context.Background())
	if failed.Status != TaskStatusFailed || failed.Result["providerEffect"] != "created" || failed.VMID == "" || f.creates != 1 || f.bootstraps != 1 || f.revocations != 1 {
		t.Fatalf("failed observation lost VM receipt: %+v", failed)
	}
	f.observeError = nil
	if _, err := f.service.RetryOperation(context.Background(), testPrincipal(), original.ID); err != nil {
		t.Fatal(err)
	}
	completed := f.run(t, context.Background())
	if completed.Status != TaskStatusSucceeded || completed.ID != original.ID || !boolValue(completed.Result, "workerReady") || f.creates != 1 || f.bootstraps != 1 || f.claims != 1 {
		t.Fatalf("worker recovery recreated resources or skipped readiness: %+v", completed)
	}
}

func TestInvalidWorkerPoolSpecBlocksCreationAndRetry(t *testing.T) {
	f := newWorkerExecutionFixture(t)
	ctx := context.Background()
	principal := testPrincipal()
	input := sohaapi.VirtualizationWorkerCreateInput{PoolRevision: 1, IdempotencyKey: "worker-idempotency-1"}
	f.pool.saved.Spec.Labels = map[string]string{"team": "invalid value"}
	if _, err := f.service.CreateWorker(ctx, principal, f.pool.saved.ID.String(), input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("creation error = %v, want invalid argument", err)
	}
	if len(f.repo.tasks) != 0 || f.repo.admissions != 0 {
		t.Fatal("invalid persisted pool allocated a task")
	}
	f.pool.saved.Spec.Labels = nil
	task := f.enqueue(t)
	// Simulate a historical record whose snapshot contains an invalid spec.
	invalid := f.pool.saved.VirtualizationWorkerPool
	invalid.Spec.Labels = map[string]string{"team": "invalid value"}
	task.Payload["workerPool"] = invalid
	if _, err := f.repo.UpdateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateWorker(ctx, principal, f.pool.saved.ID.String(), input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("idempotent replay error = %v, want invalid argument", err)
	}
	failed := f.run(t, ctx)
	if failed.Status != TaskStatusFailed || f.bootstraps != 0 || f.creates != 0 || f.revocations != 0 || f.adapter.reads != 0 {
		t.Fatalf("invalid replay did not fail before runtime effects: status=%s", failed.Status)
	}
	if _, err := f.service.RetryOperation(ctx, principal, task.ID); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("retry error = %v, want invalid argument", err)
	}
	current, err := f.repo.GetTask(ctx, task.ID)
	if err != nil || current.Status != failed.Status || !current.UpdatedAt.Equal(failed.UpdatedAt) || f.repo.admissions != 1 || len(f.repo.tasks) != 1 {
		t.Fatalf("rejected retry wrote task state: %+v, %v", current, err)
	}
}

func TestWorkerCancellationAndTimeoutRetainOriginalEffects(t *testing.T) {
	for _, stage := range []string{"bootstrap", "provider", "timeout", "revoke-failure"} {
		t.Run(stage, func(t *testing.T) {
			f := newWorkerExecutionFixture(t)
			f.enqueue(t)
			cancel := func(id string) {
				if _, err := f.service.CancelOperation(context.Background(), testPrincipal(), id); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			switch stage {
			case "bootstrap":
				f.afterBootstrap = cancel
			case "provider":
				f.afterCreate = cancel
			case "revoke-failure":
				f.afterCreate = cancel
				f.revokeError = errors.New("cluster unreachable")
			case "timeout":
				f.ready = false
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 30*time.Millisecond)
				defer stop()
			}
			current := f.run(t, ctx)
			verifyWorkerCancellationEffects(t, f, stage, current)
		})
	}
}

func verifyWorkerCancellationEffects(t *testing.T, f *workerExecutionFixture, stage string, current domain.Task) {
	t.Helper()
	if stage == "bootstrap" {
		if f.creates != 0 || f.revocations == 0 {
			t.Fatal("canceled bootstrap still dispatched VM")
		}
	} else if f.creates != 1 || current.VMID == "" || current.Result["providerEffect"] != "created" {
		t.Fatalf("original VM lost: %+v", current)
	}
	if stage == "provider" && (current.Status != TaskStatusCanceled || !boolValue(current.Result, "cancellationConfirmed") || f.claims != 0) {
		t.Fatalf("cancel not closed after revocation: %+v", current)
	}
	if stage == "revoke-failure" && (current.Status != TaskStatusCanceling || boolValue(current.Result, "cancellationConfirmed")) {
		t.Fatal("failed token revocation claimed cancellation complete")
	}
	if stage == "timeout" && (current.Status != TaskStatusFailed || boolValue(current.Result, "workerReady") || f.revocations != 1) {
		t.Fatalf("timeout lost partial worker: %+v", current)
	}
}

func TestWorkerRetryReplacesBootstrapOnlyBeforeVMDispatch(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		f := newWorkerExecutionFixture(t)
		original := f.enqueue(t)
		if dispatched {
			f.createError = errors.New("provider reply lost")
		} else {
			f.adapter.prepareError = errors.New("provider unavailable before dispatch")
		}
		failed := f.run(t, context.Background())
		if failed.Status != TaskStatusFailed || !boolValue(failed.Result, "workerBootstrapRevoked") {
			t.Fatalf("failed worker did not close bootstrap: %+v", failed)
		}
		f.createError, f.adapter.prepareError = nil, nil
		f.adapter.found = true
		if _, err := f.service.RetryOperation(context.Background(), testPrincipal(), original.ID); err != nil {
			t.Fatal(err)
		}
		completed := f.run(t, context.Background())
		if completed.Status != TaskStatusSucceeded || f.creates != 1 {
			t.Fatalf("retry changed the original VM: %+v creates=%d", completed, f.creates)
		}
		if dispatched && (f.bootstraps != 1 || f.adapter.reads != 1) {
			t.Fatal("unknown dispatched VM was not recovered read-only")
		}
		if !dispatched && f.bootstraps != 2 {
			t.Fatal("explicit pre-dispatch retry did not replace closed bootstrap")
		}
	}
}

func TestWorkerAssessmentIsFreshAndReadOnly(t *testing.T) {
	f := newWorkerExecutionFixture(t)
	original := f.enqueue(t)
	if assessed, err := f.service.AssessWorkerReadiness(context.Background(), testPrincipal(), original.ID); err != nil || assessed.Verdict != "inconclusive" {
		t.Fatalf("uncreated VM was ready: %+v %v", assessed, err)
	}
	f.run(t, context.Background())
	claims, revocations := f.claims, f.revocations
	assessment, err := f.service.AssessWorkerReadiness(context.Background(), testPrincipal(), original.ID)
	if err != nil || assessment.Verdict != "satisfied" || len(assessment.Evidence) != 1 {
		t.Fatalf("ready evidence missing: %+v %v", assessment, err)
	}
	f.service.workerRuntime.Observe = func(context.Context, domain.WorkerPool, string) (domain.WorkerObservation, error) {
		return domain.WorkerObservation{Ready: true, NodeUID: "node-uid", ObservedAt: time.Now().Add(-time.Minute), ValidUntil: time.Now().Add(-30 * time.Second)}, nil
	}
	assessment, err = f.service.AssessWorkerReadiness(context.Background(), testPrincipal(), original.ID)
	if err != nil || assessment.Verdict != "inconclusive" || f.claims != claims || f.revocations != revocations || f.creates != 1 {
		t.Fatalf("assessment reused stale evidence or made changes: %+v %v", assessment, err)
	}
}

func TestWorkerCreationRequiresBootstrapEncryptionBeforeAdmission(t *testing.T) {
	f := newWorkerExecutionFixture(t)
	f.service.credentialKey = ""
	_, err := f.service.CreateWorker(context.Background(), testPrincipal(), f.pool.saved.ID.String(), sohaapi.VirtualizationWorkerCreateInput{PoolRevision: 1, IdempotencyKey: "no-encryption"})
	if err == nil || f.repo.admissions != 0 || f.bootstraps != 0 || f.creates != 0 {
		t.Fatalf("unprotected worker admitted: %v", err)
	}
}

func TestHistoricalWorkerOperationsWithoutRuntime(t *testing.T) {
	f := newWorkerExecutionFixture(t)
	task := f.enqueue(t)
	// A bootstrap attempt may have created credentials before it saved their UID.
	task.Payload["workerBootstrapDeadline"] = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	ctx := domain.WithScopeCheck(context.Background(), func(scope map[string]string) error {
		if scope["operationId"] != task.ID || scope["clusterId"] != "target" || scope["workerPoolId"] != f.pool.saved.ID.String() || scope["connectionRevision"] != f.pool.saved.Identity.ConnectionIdentity {
			t.Fatalf("historical task lost its frozen scope: %v", scope)
		}
		return nil
	})
	if _, err := f.repo.UpdateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.CreateTaskLog(ctx, domain.TaskLog{TaskID: task.ID, Message: "stored bootstrap attempt"}); err != nil {
		t.Fatal(err)
	}
	f.service.workerRuntime = nil
	principal := testPrincipal()
	f.service.authorizeWorkerCluster = func(_ context.Context, caller domainidentity.Principal, clusterID string, mutate bool) error {
		if clusterID != "target" || mutate {
			t.Fatalf("historical access checked the wrong target or action: cluster=%q mutate=%v", clusterID, mutate)
		}
		if caller.UserID != principal.UserID {
			return apperrors.ErrAccessDenied
		}
		return nil
	}

	f.checkHistoricalWorkerReads(t, ctx, principal, task)
	f.checkHistoricalWorkerCancellation(t, ctx, principal, task)
}

func (f *workerExecutionFixture) checkHistoricalWorkerReads(t *testing.T, ctx context.Context, principal domainidentity.Principal, task domain.Task) {
	t.Helper()
	if items, err := f.service.ListOperations(ctx, principal, domain.TaskFilter{}); err != nil || len(items) != 1 || items[0].ID != task.ID {
		t.Fatalf("ListOperations() returned %d tasks, error=%v", len(items), err)
	}
	if page, err := f.service.ListOperationsPage(ctx, principal, domain.TaskFilter{Page: 1, PageSize: 10}); err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != task.ID {
		t.Fatalf("ListOperationsPage() returned %d items (total=%d), error=%v", len(page.Items), page.Total, err)
	}
	if got, err := f.service.GetOperation(ctx, principal, task.ID); err != nil || got.ID != task.ID {
		t.Fatalf("GetOperation() returned ID %q, error=%v", got.ID, err)
	}
	if logs, err := f.service.ListOperationLogs(ctx, principal, task.ID, 10); err != nil || len(logs) != 1 || logs[0].Message != "stored bootstrap attempt" {
		t.Fatalf("ListOperationLogs() did not return the stored log: %v", err)
	}
}

func (f *workerExecutionFixture) checkHistoricalWorkerCancellation(t *testing.T, ctx context.Context, principal domainidentity.Principal, task domain.Task) {
	t.Helper()
	noWorkerPermissions := principal
	noWorkerPermissions.PermissionKeys = []string{appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationOperationsManage}
	noClusterAccess := principal
	noClusterAccess.UserID = "other-user"
	for _, tt := range []struct {
		name      string
		principal domainidentity.Principal
	}{
		{"missing worker permissions", noWorkerPermissions},
		{"missing cluster access", noClusterAccess},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := f.service.ListOperationLogs(ctx, tt.principal, task.ID, 10); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("unauthorized log read error = %v", err)
			}
			if _, err := f.service.CancelOperation(ctx, tt.principal, task.ID); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("unauthorized cancellation error = %v", err)
			}
		})
	}
	if current, err := f.repo.GetTask(ctx, task.ID); err != nil || current.Status != TaskStatusQueued {
		t.Fatalf("unauthorized cancellation changed status to %q, error=%v", current.Status, err)
	}
	canceled, err := f.service.CancelOperation(ctx, principal, task.ID)
	if err != nil || canceled.Status != TaskStatusCanceling {
		t.Fatalf("CancelOperation() returned status %q, error=%v", canceled.Status, err)
	}
	f.service.observeCanceledCreation(ctx, canceled)
	current, err := f.repo.GetTask(ctx, task.ID)
	if err != nil || current.Status != TaskStatusCanceling || current.FinishedAt != nil || current.Result["canceledBy"] != principal.UserID || current.Result["cancellationConfirmed"] != false || boolValue(current.Result, "workerBootstrapRevoked") {
		t.Fatalf("cancellation intent or pending cleanup lost: status=%q, result=%+v, error=%v", current.Status, current.Result, err)
	}
}

func TestWorkerSupplyRechecksClusterAccess(t *testing.T) {
	for _, stage := range []string{"enqueue", "dispatch", "read", "assessment", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			f := newWorkerExecutionFixture(t)
			deny := func(_ context.Context, _ domainidentity.Principal, cluster string, _ bool) error {
				if cluster != "target" {
					t.Fatal("lost frozen target")
				}
				return apperrors.ErrAccessDenied
			}
			if stage == "enqueue" {
				f.service.authorizeWorkerCluster = deny
				_, err := f.service.CreateWorker(context.Background(), testPrincipal(), f.pool.saved.ID.String(), sohaapi.VirtualizationWorkerCreateInput{PoolRevision: 1, IdempotencyKey: "denied"})
				if !errors.Is(err, apperrors.ErrAccessDenied) || f.repo.admissions != 0 {
					t.Fatalf("denied enqueue: %v", err)
				}
				return
			}
			task, err := f.service.CreateWorker(context.Background(), testPrincipal(), f.pool.saved.ID.String(), sohaapi.VirtualizationWorkerCreateInput{PoolRevision: 1, IdempotencyKey: "cluster-revoked"})
			if err != nil {
				t.Fatal(err)
			}
			f.service.authorizeWorkerCluster = deny
			switch stage {
			case "dispatch":
				err = f.service.authorizeQueuedVMTask(context.Background(), task)
			case "read":
				_, err = f.service.GetOperation(context.Background(), testPrincipal(), task.ID)
			case "assessment":
				_, err = f.service.AssessWorkerReadiness(context.Background(), testPrincipal(), task.ID)
			case "cancel":
				_, err = f.service.CancelOperation(context.Background(), testPrincipal(), task.ID)
			}
			if !errors.Is(err, apperrors.ErrAccessDenied) || f.creates != 0 || f.bootstraps != 0 {
				t.Fatalf("revoked cluster executed: %v", err)
			}
		})
	}
}
