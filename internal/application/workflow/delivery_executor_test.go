package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryExecutorRepository struct {
	*deliveryServiceRepository
	mu sync.Mutex
}

func (r *deliveryExecutorRepository) Get(_ context.Context, _ string) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneRunForAsyncWorker(r.run), nil
}

func (r *deliveryExecutorRepository) GetDeliveryBatch(_ context.Context, _ string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.batch, cloneRunForAsyncWorker(r.run), nil
}

func (r *deliveryExecutorRepository) ClaimManagedRun(_ context.Context, owner string, ttl time.Duration) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if deliveryRunTerminal(r.run.Status) || (r.run.LeaseUntil != nil && r.run.LeaseUntil.After(time.Now())) {
		return domainworkflow.Run{}, apperrors.ErrNotFound
	}
	until := time.Now().Add(ttl)
	r.run.Version++
	r.run.FencingToken++
	r.run.LeaseOwner, r.run.LeaseUntil = owner, &until
	return cloneRunForAsyncWorker(r.run), nil
}

func (r *deliveryExecutorRepository) SaveManagedRun(_ context.Context, run domainworkflow.Run, release bool) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !sameDeliveryLease(run, r.run) {
		return run, apperrors.ErrConflict
	}
	run.Version++
	if release {
		run.LeaseOwner, run.LeaseUntil = "", nil
	}
	r.run = cloneRunForAsyncWorker(run)
	return cloneRunForAsyncWorker(run), nil
}

func (r *deliveryExecutorRepository) StopManagedRun(_ context.Context, _ string, reason, summary string) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.run.StopReason == "" && !deliveryRunTerminal(r.run.Status) {
		r.run.StopReason, r.run.StopSummary, r.run.Status = reason, summary, "canceling"
		r.run.Version++
	}
	return cloneRunForAsyncWorker(r.run), nil
}

type deliveryExecutorRuntime struct {
	gatewayCalls []domainai.ExecutionAuthorization
	*deliveryServiceRuntime
	mu                sync.Mutex
	states            map[string]string
	calls             []domainworkflow.NodeRun
	stopAck           bool
	revoked           bool
	cancelCall        int
	expectedTokenID   string
	authorizerRevoked bool
}

func (r *deliveryExecutorRuntime) CurrentExecutionPrincipal(ctx context.Context, userID, tokenID string) (domainidentity.Principal, error) {
	if userID == "trigger-owner" {
		if tokenID != "owner-token" || r.authorizerRevoked {
			return domainidentity.Principal{}, apperrors.ErrAccessDenied
		}
		return r.deliveryServiceRuntime.CurrentExecutionPrincipal(ctx, userID, tokenID)
	}
	if tokenID != r.expectedTokenID {
		return domainidentity.Principal{}, fmt.Errorf("execution token identity lost")
	}
	if r.revoked {
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	return r.deliveryServiceRuntime.CurrentExecutionPrincipal(ctx, userID, tokenID)
}

func TestDeliveryExecutorStopsWhenTriggerOwnerLosesAuthorization(t *testing.T) {
	service, repo, runtime := newDeliveryExecutorCheck(t, domainworkflow.DeliveryWorkflowDefinition{Name: "trigger", Targets: deliveryCheckTargets(1)})
	repo.run.Metadata["triggerAuthorizerId"] = "trigger-owner"
	repo.run.Metadata["triggerAuthorizerTokenId"] = "owner-token"
	tickDeliveryCheck(t, service, repo)
	if len(runtime.calls) != 1 {
		t.Fatalf("delegated execution did not start: %+v", repo.run)
	}
	runtime.authorizerRevoked, runtime.stopAck = true, true
	tickDeliveryCheck(t, service, repo)
	if len(runtime.calls) != 1 || runtime.cancelCall == 0 {
		t.Fatal("revoked trigger owner left the service identity executing")
	}
}

func TestDeliveryExecutorRestoresTokenIdentityAfterRestart(t *testing.T) {
	service, repo, runtime := newDeliveryExecutorCheck(t, domainworkflow.DeliveryWorkflowDefinition{Name: "token", Targets: deliveryCheckTargets(1)})
	runtime.expectedTokenID = "scoped-token"
	repo.run.Metadata["executionTokenId"] = "scoped-token"
	tickDeliveryCheck(t, service, repo)
	if len(runtime.calls) != 1 {
		t.Fatalf("token-bound execution did not start: %+v", repo.run)
	}
	runtime.revoked, runtime.stopAck = true, true
	tickDeliveryCheck(t, service, repo)
	if len(runtime.calls) != 1 || runtime.cancelCall == 0 {
		t.Fatal("revoked token continued to the next execution stage")
	}
}

func (r *deliveryExecutorRuntime) ExecuteDeliveryStage(ctx context.Context, principal domainidentity.Principal, _ domainworkflow.Run, _ domainworkflow.DeliveryBatch, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if principal.UserID != "actor" {
		return node, fmt.Errorf("unexpected system identity: %s", principal.UserID)
	}
	r.calls = append(r.calls, node)
	if authorization, ok := domainai.ExecutionAuthorizationFrom(ctx); ok {
		r.gatewayCalls = append(r.gatewayCalls, authorization)
	}
	if node.ExecutionTaskID == "" {
		node.ExecutionTaskID = "task:" + node.NodeID
	}
	node.Status = r.states[node.NodeID]
	if node.Status == "" {
		node.Status = "completed"
	}
	if node.Status == "failed" {
		node.Summary = "build failed"
	}
	return node, nil
}

func (r *deliveryExecutorRuntime) CancelDeliveryStage(_ context.Context, _ domainworkflow.Run, _ domainworkflow.DeliveryBatch, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelCall++
	if r.stopAck {
		node.Status = "canceled"
	} else {
		node.Status = "waiting_execution"
	}
	return node, nil
}

func newDeliveryExecutorCheck(t *testing.T, definition domainworkflow.DeliveryWorkflowDefinition) (*Service, *deliveryExecutorRepository, *deliveryExecutorRuntime) {
	t.Helper()
	repo := &deliveryExecutorRepository{deliveryServiceRepository: &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}}
	runtime := &deliveryExecutorRuntime{deliveryServiceRuntime: &deliveryServiceRuntime{}, states: map[string]string{}}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger}}})
	service := New(repo, &stubWorkflowApps{}, nil, permissions, nil, nil, nil, nil)
	service.SetDeliveryRuntime(runtime, runtime)
	_, err := service.CreateDeliveryBatch(context.Background(), domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}, domainworkflow.DeliveryBatchInput{IdempotencyKey: "executor-check", Definition: &definition})
	if err != nil {
		t.Fatal(err)
	}
	return service, repo, runtime
}

func tickDeliveryCheck(t *testing.T, service *Service, repo *deliveryExecutorRepository) {
	t.Helper()
	run, err := repo.ClaimManagedRun(context.Background(), "restarted-worker", deliveryRunLease)
	if err != nil {
		t.Fatal(err)
	}
	newDAGRunExecutor(service).runDeliveryTick(context.Background(), run)
}

func deliveryCheckTargets(count int) []domainworkflow.DeliveryTargetInput {
	targets := make([]domainworkflow.DeliveryTargetInput, count)
	for i := range targets {
		id := fmt.Sprintf("service%d", i+1)
		// Distinct applications prevent the fixture's equal input fingerprints
		// from sharing builds; each target must run its own full chain.
		targets[i] = domainworkflow.DeliveryTargetInput{ID: id, ApplicationID: id, ServiceID: id, ApplicationEnvironmentID: "dev", Action: "build_deploy"}
	}
	return targets
}

func TestDeliveryExecutorSerialTenTargetsAndRecoveryKeepOneTask(t *testing.T) {
	service, repo, runtime := newDeliveryExecutorCheck(t, domainworkflow.DeliveryWorkflowDefinition{Name: "serial", Targets: deliveryCheckTargets(10)})
	runtime.states["service1:build"] = "waiting_execution"
	tickDeliveryCheck(t, service, repo)
	tickDeliveryCheck(t, service, repo)
	if len(runtime.calls) != 2 || runtime.calls[1].ExecutionTaskID != "task:service1:build" || repo.run.NodeRuns[0].FinishedAt != "" {
		t.Fatalf("waiting task was lost or falsely finished: calls=%+v nodes=%+v", runtime.calls, repo.run.NodeRuns)
	}
	// A late legacy callback must not rewrite or resume this scoped Run.
	version := repo.run.Version
	if err := service.RecordExecutionTaskResult(context.Background(), domaindelivery.ExecutionTask{ID: "old-attempt", Status: "completed", Payload: map[string]any{"workflowRunId": repo.run.ID, "workflowNodeId": "service1:build"}}); err != nil || repo.run.Version != version {
		t.Fatalf("legacy callback changed batch: %v", err)
	}
	runtime.states["service1:build"] = "completed"
	for i := 0; i < 80 && !deliveryRunTerminal(repo.run.Status); i++ {
		tickDeliveryCheck(t, service, repo)
	}
	if repo.run.Status != "completed" {
		t.Fatalf("serial delivery did not finish: %+v", repo.run)
	}
	last := ""
	for _, call := range runtime.calls {
		if last != "" && call.TargetID != last && call.Stage != "build" {
			t.Fatalf("target changed before its build: %+v", call)
		}
		last = call.TargetID
	}
	if len(runtime.calls) != 42 || runtime.calls[5].NodeID != "service1:health" || runtime.calls[6].NodeID != "service2:build" {
		t.Fatalf("serial stage ordering changed: %+v", runtime.calls)
	}
}

func TestDeliveryExecutorFailureBarrierAndHonestStop(t *testing.T) {
	service, repo, runtime := newDeliveryExecutorCheck(t, domainworkflow.DeliveryWorkflowDefinition{Name: "build-all", Mode: domainworkflow.DeliveryModeBuildAll, MaxConcurrency: 2, Targets: deliveryCheckTargets(3)})
	runtime.states["service1:build"], runtime.states["service2:build"] = "failed", "waiting_execution"
	tickDeliveryCheck(t, service, repo)
	if repo.run.Status != "canceling" || repo.run.StopReason != "failure" || len(runtime.calls) != 2 {
		t.Fatalf("failure did not stop new dispatch: run=%+v calls=%+v", repo.run, runtime.calls)
	}
	_, _ = repo.StopManagedRun(context.Background(), repo.run.ID, "user", "late cancel")
	tickDeliveryCheck(t, service, repo)
	if repo.run.Status != "canceling" || len(runtime.calls) != 2 {
		t.Fatal("unconfirmed cancellation became terminal or dispatched more work")
	}
	runtime.stopAck = true
	tickDeliveryCheck(t, service, repo)
	if repo.run.Status != "failed" || repo.run.StopReason != "failure" || repo.run.StopSummary != "build failed" {
		t.Fatalf("failure cause was lost: %+v", repo.run)
	}
}

func TestDeliveryExecutorContinuesIndependentTargetsAndRejectsRevokedIdentity(t *testing.T) {
	stop := false
	definition := domainworkflow.DeliveryWorkflowDefinition{Name: "continue", StopOnFailure: &stop, Targets: deliveryCheckTargets(3)}
	definition.Targets[1].DependsOn = []string{"service1"}
	service, repo, runtime := newDeliveryExecutorCheck(t, definition)
	runtime.states["service1:build"] = "failed"
	for i := 0; i < 40 && !deliveryRunTerminal(repo.run.Status); i++ {
		tickDeliveryCheck(t, service, repo)
	}
	if repo.run.Status != "partially_completed" {
		t.Fatalf("independent target did not continue: %+v", repo.run)
	}
	for _, call := range runtime.calls {
		if call.TargetID == "service2" || (call.TargetID == "service1" && call.Stage != "build") {
			t.Fatalf("failed dependency dispatched: %+v", call)
		}
	}
	service, repo, runtime = newDeliveryExecutorCheck(t, domainworkflow.DeliveryWorkflowDefinition{Name: "revoked", Targets: deliveryCheckTargets(1)})
	runtime.states["service1:build"] = "waiting_execution"
	tickDeliveryCheck(t, service, repo)
	runtime.revoked = true
	tickDeliveryCheck(t, service, repo)
	if len(runtime.calls) != 1 || repo.run.NodeRuns[0].Status != "canceling" {
		t.Fatalf("revoked identity continued execution: %+v", repo.run)
	}
	runtime.stopAck = true
	tickDeliveryCheck(t, service, repo)
	if repo.run.Status != "failed" || runtime.cancelCall < 2 {
		t.Fatalf("authorization stop was not reconciled: %+v", repo.run)
	}
}

func TestDAGSchedulerUsesExistingWorkerForDurablePollAndQueue(t *testing.T) {
	var scheduler dagScheduler
	var once sync.Once
	scheduler.configure(1, 2)
	scheduler.configurePoll(func(context.Context) (dagRunTask, bool) {
		claimed := false
		once.Do(func() { claimed = true })
		return dagRunTask{run: domainworkflow.Run{ID: "durable"}}, claimed
	}, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := make(chan string, 2)
	scheduler.start(ctx, func(_ context.Context, task dagRunTask) { seen <- task.run.ID })
	t.Cleanup(func() { _, _ = scheduler.shutdown(context.Background()) })
	if _, err := scheduler.enqueue(ctx, dagRunTask{run: domainworkflow.Run{ID: "legacy"}}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for range 2 {
		select {
		case id := <-seen:
			ids[id] = true
		case <-time.After(time.Second):
			t.Fatal("existing worker did not consume both durable and queued work")
		}
	}
	if !ids["legacy"] || !ids["durable"] {
		t.Fatalf("missing work: %+v", ids)
	}
}
