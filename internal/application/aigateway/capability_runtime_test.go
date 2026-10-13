package aigateway

import (
	"context"
	"errors"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type capabilityApprovalRepositoryFixture struct{ *memoryGatewayRepository }

type deadlineDockerService struct {
	contractDockerService
	deadline time.Time
	calls    int
	expired  bool
}

type deadlineDeployService struct {
	*contractDockerService
	deadline time.Time
}

func (s *deadlineDeployService) DeployProject(ctx context.Context, principal domainidentity.Principal, id string, input domaindocker.ProjectDeployInput) (domaindocker.Operation, error) {
	s.deadline, _ = ctx.Deadline()
	return s.contractDockerService.DeployProject(ctx, principal, id, input)
}

func TestCapabilityApprovalReplayCarriesStepDeadline(t *testing.T) {
	service, repo, docker, guard, _ := capabilityApprovalFixture(t)
	observed := &deadlineDeployService{contractDockerService: docker}
	service.docker = observed
	deadline := time.Now().Add(time.Minute)
	intent, err := domainworkflow.CapabilityIntentFrom(guard.run)
	if err != nil {
		t.Fatal(err)
	}
	intent.Input.Plan.Steps = []domainaigateway.CapabilityPlanStep{{ID: "deploy", TimeoutSeconds: 120}}
	guard.run.NodeRuns[0].StartedAt = deadline.Add(-120 * time.Second).Format(time.RFC3339Nano)
	guard.run.Metadata["capabilityIntent"] = intent
	_, err = service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), repo.approvalRequests[0].ID, domainaigateway.ApprovalDecisionInput{})
	if err != nil || docker.calls != 1 || !observed.deadline.Equal(deadline) {
		t.Fatalf("approved call deadline changed: %v got=%s want=%s calls=%d", err, observed.deadline, deadline, docker.calls)
	}
}

func (s *deadlineDockerService) GetOperation(ctx context.Context, _ domainidentity.Principal, _ string) (domaindocker.Operation, error) {
	s.calls++
	s.deadline, _ = ctx.Deadline()
	<-ctx.Done()
	s.expired = errors.Is(ctx.Err(), context.DeadlineExceeded)
	return domaindocker.Operation{}, ctx.Err()
}

func TestCapabilityInvocationHonorsRunAndStepDeadlines(t *testing.T) {
	for _, scenario := range []string{"run", "step", "already-expired"} {
		t.Run(scenario, func(t *testing.T) {
			deadline := time.Now().Add(time.Second)
			intent := domainworkflow.CapabilityIntent{ActorID: "user-1", Digest: "deadline-plan", Deadline: deadline}
			call := domainaigateway.ToolInvocationRequest{ToolName: "docker.operations.get", CapabilityVersion: "1", Input: map[string]any{"operationId": "operation-1"}}
			node := domainworkflow.NodeRun{NodeID: "observe", Status: "running", DispatchAttempted: true, PreparedCall: &call}
			if scenario == "step" {
				intent.Deadline = deadline.Add(time.Hour)
				intent.Input.Plan.Steps = []domainaigateway.CapabilityPlanStep{{ID: node.NodeID, TimeoutSeconds: 1}}
				node.StartedAt = deadline.Add(-time.Second).Format(time.RFC3339Nano)
			}
			if scenario == "already-expired" {
				intent.Deadline = time.Now().Add(-time.Second)
			}
			run := domainworkflow.Run{ID: "bounded-run", Scope: domainworkflow.ScopeCapabilityTask, Metadata: map[string]any{"capabilityIntent": intent}}
			repo := &memoryGatewayRepository{}
			service := contractTestService(repo, nil)
			service.approvals = capabilityApprovalRepositoryFixture{repo}
			docker := &deadlineDockerService{}
			service.docker = docker
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := service.AdvanceCapabilityNode(ctx, testPrincipal("developer"), run, node)
			if scenario == "already-expired" {
				if docker.calls != 0 || !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expired call was not rejected before the domain: %v calls=%d", err, docker.calls)
				}
			} else if docker.calls != 1 || !docker.deadline.Equal(deadline) || !docker.expired || result.Status == "completed" {
				t.Fatalf("domain deadline changed or timeout completed: got=%s want=%s calls=%d expired=%v result=%s err=%v", docker.deadline, deadline, docker.calls, docker.expired, result.Status, err)
			}
		})
	}
}

func (r capabilityApprovalRepositoryFixture) GetApprovalRequest(ctx context.Context, id string) (domainaigateway.ApprovalRequest, error) {
	request, err := r.memoryGatewayRepository.GetApprovalRequest(ctx, id)
	if err == nil && request.ID == "" {
		err = apperrors.ErrNotFound
	}
	return request, err
}

type capabilityGuardFixture struct{ run domainworkflow.Run }

type nativeParentGuardFixture struct {
	parent                             domaincopilot.NativeTaskParent
	stopped                            bool
	controls                           []bool
	workCalls, cleanupCalls, workLimit int
}

func (g *nativeParentGuardFixture) ReserveNativeTaskToolCall(ctx context.Context, parent domaincopilot.NativeTaskParent, cancellation bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if parent.RunID != g.parent.RunID || parent.RequestHash != g.parent.RequestHash {
		return apperrors.ErrConflict
	}
	if cancellation {
		if g.cleanupCalls >= domaincopilot.NativeCleanupToolCallLimit {
			return apperrors.ErrConflict
		}
		g.cleanupCalls++
		return nil
	}
	limit := g.workLimit
	if limit == 0 {
		limit = domaincopilot.NativeWorkToolCallLimit
	}
	if g.workCalls >= limit {
		return apperrors.ErrConflict
	}
	g.workCalls++
	return nil
}

func (g *nativeParentGuardFixture) WithNativeTaskParent(ctx context.Context, parent domaincopilot.NativeTaskParent, cancellation bool, execute func(context.Context) error) error {
	if parent.RunID != g.parent.RunID || parent.RequestHash != g.parent.RequestHash || (g.stopped && !cancellation) {
		return apperrors.ErrConflict
	}
	g.controls = append(g.controls, cancellation)
	if !cancellation {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, parent.Deadline)
		defer cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return execute(ctx)
}

func bindNativeApprovalParent(t *testing.T, service *Service, guard *capabilityGuardFixture) *nativeParentGuardFixture {
	t.Helper()
	parent := domaincopilot.NativeTaskParent{RunID: "native-parent", OwnerID: "user-1", SessionID: "original-session", RequestHash: "original-request", Deadline: time.Now().Add(time.Minute)}
	intent, _ := domainworkflow.CapabilityIntentFrom(guard.run)
	intent.NativeParent = &parent
	guard.run.Metadata["capabilityIntent"] = intent
	native := &nativeParentGuardFixture{parent: parent}
	if err := service.SetNativeTaskGuard(native); err != nil {
		t.Fatal(err)
	}
	return native
}

func (g *capabilityGuardFixture) WithCapabilityApproval(_ context.Context, id string, cancellation bool, execute func(domainworkflow.Run) error) error {
	if id != g.run.ID || (g.run.StopReason != "" && !cancellation) {
		return apperrors.ErrConflict
	}
	return execute(g.run)
}

type capabilityIdentityFixture struct{ revoked bool }

func (f *capabilityIdentityFixture) CurrentPrincipal(context.Context, string) (domainidentity.Principal, error) {
	return testPrincipal("developer"), nil
}
func (f *capabilityIdentityFixture) CurrentExecutionPrincipal(_ context.Context, id, token string) (domainidentity.Principal, error) {
	if id != "user-1" || token != "frozen-token" || f.revoked {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	actor := testPrincipal("developer")
	actor.AccessTokenID = token
	return actor, nil
}

func (d *contractDockerService) CancelOperationIdempotent(_ context.Context, _ domainidentity.Principal, id string, input appdocker.OperationMutationInput) (domaindocker.Operation, error) {
	if id != d.operation.ID || input.IdempotencyKey == "" {
		return domaindocker.Operation{}, apperrors.ErrInvalidArgument
	}
	d.calls++
	d.operation.Status = "canceled"
	d.operation.Result = map[string]any{"cancellationAcknowledged": true}
	return d.operation, nil
}

func capabilityApprovalFixture(t *testing.T) (*Service, *memoryGatewayRepository, *contractDockerService, *capabilityGuardFixture, *capabilityIdentityFixture) {
	t.Helper()
	repo := &memoryGatewayRepository{}
	docker := &contractDockerService{operation: domaindocker.Operation{ID: "operation-1", Status: "queued"}}
	service := contractTestService(repo, docker)
	service.approvals = capabilityApprovalRepositoryFixture{repo}
	service.permissions = appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"admin":     {appaccess.PermAIGatewayManage},
		"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermDockerProjectsDeploy, appaccess.PermDockerOperationsView, appaccess.ManagedActionPermission(appaccess.PermDockerOperationsManage, "cancel")},
	}})
	identity := &capabilityIdentityFixture{}
	service.identity = identity
	call := domainaigateway.ToolInvocationRequest{ToolName: "docker.projects.deploy.trigger", CapabilityVersion: "1", RequestID: "stable-node-invocation", Input: map[string]any{"projectId": "project-1", "idempotencyKey": "stable-deploy"}}
	node := domainworkflow.NodeRun{NodeID: "deploy", Status: "running", DispatchAttempted: true, PreparedCall: &call}
	intent := domainworkflow.CapabilityIntent{ActorID: "user-1", ActorTokenID: "frozen-token", Digest: "plan-digest", Deadline: time.Now().Add(time.Hour), PlanVersion: 1}
	guard := &capabilityGuardFixture{run: domainworkflow.Run{ID: "goal-1", Scope: domainworkflow.ScopeCapabilityTask, Status: "running", Metadata: map[string]any{"capabilityIntent": intent}, NodeRuns: []domainworkflow.NodeRun{node}}}
	service.SetCapabilityApprovalGuard(guard)
	ctx := withCapabilityExecution(context.Background(), guard.run, node)
	held, err := service.InvokeTool(ctx, testPrincipal("developer"), call)
	if err != nil || !held.RequiresApproval {
		t.Fatalf("not held: %+v %v", held, err)
	}
	guard.run.NodeRuns[0].Status = "waiting_approval"
	guard.run.NodeRuns[0].Invocation = &held
	return service, repo, docker, guard, identity
}

func TestCapabilityApprovalRecoversOnceAndRejectsChangedTask(t *testing.T) {
	for _, scenario := range []string{"active", "canceled", "revoked", "changed-input", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, docker, guard, identity := capabilityApprovalFixture(t)
			node := guard.run.NodeRuns[0]
			// Lost worker reply must reuse the same pending approval.
			ctx := withCapabilityExecution(context.Background(), guard.run, node)
			if _, err := service.InvokeTool(ctx, testPrincipal("developer"), *node.PreparedCall); err != nil || len(repo.approvalRequests) != 1 {
				t.Fatalf("approval duplicated: %v %d", err, len(repo.approvalRequests))
			}
			switch scenario {
			case "canceled":
				guard.run.StopReason = "user"
			case "revoked":
				identity.revoked = true
			case "changed-input":
				node.PreparedCall.Input["projectId"] = "different-project"
			case "expired":
				intent, _ := domainworkflow.CapabilityIntentFrom(guard.run)
				intent.Deadline = time.Now().Add(-time.Second)
				guard.run.Metadata["capabilityIntent"] = intent
			}
			id := capabilityApprovalID(node.PreparedCall.RequestID)
			result, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), id, domainaigateway.ApprovalDecisionInput{})
			if scenario == "active" {
				if err != nil || docker.calls != 1 || result.Invocation == nil {
					t.Fatalf("active plan failed: %+v %v calls=%d", result, err, docker.calls)
				}
			} else if err == nil || docker.calls != 0 {
				t.Fatalf("stale task approval executed: %v calls=%d", err, docker.calls)
			}
		})
	}
}

func TestCapabilityCancellationApprovalRetainsChildReference(t *testing.T) {
	service, repo, docker, guard, _ := capabilityApprovalFixture(t)
	native := bindNativeApprovalParent(t, service, guard)
	native.workLimit = 1
	primary := capabilityApprovalID(guard.run.NodeRuns[0].PreparedCall.RequestID)
	approved, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), primary, domainaigateway.ApprovalDecisionInput{})
	if err != nil {
		t.Fatal(err)
	}
	guard.run.NodeRuns[0].Invocation = approved.Invocation
	guard.run.NodeRuns[0].Status = "waiting_execution"
	guard.run.StopReason = "user"
	native.stopped = true
	repo.accessPolicies = []domainaigateway.AccessPolicy{{ID: "cancel-policy", Enabled: true, SubjectType: "role", SubjectID: "developer", Effect: "allow", ToolPatterns: []string{"*"}, ApprovalPolicy: map[string]any{"strategy": "require_approval"}}}
	node, err := service.AdvanceCapabilityNode(context.Background(), testPrincipal("developer"), guard.run, guard.run.NodeRuns[0])
	if err != nil || node.ControlCall == nil || node.Invocation.Task == nil || node.Status != "waiting_approval" {
		t.Fatalf("child lost while cancel held: %+v %v", node, err)
	}
	guard.run.NodeRuns[0] = node
	cancelID := capabilityApprovalID(node.ControlCall.RequestID)
	if cancelID == primary || len(repo.approvalRequests) != 2 {
		t.Fatalf("cancel reused deployment approval: %s", cancelID)
	}
	if _, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), cancelID, domainaigateway.ApprovalDecisionInput{}); err != nil {
		t.Fatal(err)
	}
	if docker.operation.Status != "canceled" {
		t.Fatal("approved cancel did not reach its owning domain")
	}
	if len(native.controls) != 3 || native.controls[0] || !native.controls[1] || !native.controls[2] {
		t.Fatalf("cancellation bypassed or lost its Native parent fence: %v", native.controls)
	}
	if native.workCalls != 1 || native.cleanupCalls != 1 {
		t.Fatalf("held approvals consumed attempts or exhausted work blocked cleanup: work=%d cleanup=%d", native.workCalls, native.cleanupCalls)
	}
}

type budgetDockerService struct {
	*contractDockerService
	polls int
}

func (s *budgetDockerService) GetOperation(ctx context.Context, principal domainidentity.Principal, id string) (domaindocker.Operation, error) {
	s.polls++
	return s.contractDockerService.GetOperation(ctx, principal, id)
}

func TestNativeChildBudgetCountsApprovalReplayAndEveryStatusAttempt(t *testing.T) {
	service, repo, docker, guard, _ := capabilityApprovalFixture(t)
	native := bindNativeApprovalParent(t, service, guard)
	native.workLimit = 2
	observed := &budgetDockerService{contractDockerService: docker}
	service.docker = observed
	if native.workCalls != 0 {
		t.Fatal("pending approval consumed a provider attempt")
	}
	approved, err := service.ApproveApprovalRequest(t.Context(), testPrincipal("admin"), repo.approvalRequests[0].ID, domainaigateway.ApprovalDecisionInput{})
	if err != nil || approved.Invocation == nil || native.workCalls != 1 || docker.calls != 1 {
		t.Fatalf("approval replay missed the shared budget: %v calls=%d work=%d", err, docker.calls, native.workCalls)
	}
	node := guard.run.NodeRuns[0]
	node.Invocation, node.Status = approved.Invocation, "waiting_execution"
	node, err = service.AdvanceCapabilityNode(t.Context(), testPrincipal("developer"), guard.run, node)
	if err != nil || observed.polls != 1 || native.workCalls != 2 {
		t.Fatalf("first domain poll was not charged: %v polls=%d work=%d", err, observed.polls, native.workCalls)
	}
	node, err = service.AdvanceCapabilityNode(t.Context(), testPrincipal("developer"), guard.run, node)
	if err != nil || node.Status != "blocked" || observed.polls != 1 || native.workCalls != 2 {
		t.Fatalf("exhausted task dispatched another poll: %v status=%s polls=%d work=%d", err, node.Status, observed.polls, native.workCalls)
	}
}

func TestNativeParentBlocksStaleApprovalAndCancellationFlagForgery(t *testing.T) {
	for _, scenario := range []string{"active", "stopped", "expired", "missing_guard", "forged_cancel_flag"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, docker, guard, _ := capabilityApprovalFixture(t)
			native := bindNativeApprovalParent(t, service, guard)
			id := capabilityApprovalID(guard.run.NodeRuns[0].PreparedCall.RequestID)
			switch scenario {
			case "stopped":
				native.stopped = true
			case "expired":
				intent, _ := domainworkflow.CapabilityIntentFrom(guard.run)
				intent.NativeParent.Deadline = time.Now().Add(-time.Second)
				guard.run.Metadata["capabilityIntent"] = intent
			case "missing_guard":
				service.nativeTaskGuard = nil
			case "forged_cancel_flag":
				request := repo.approvalRequests[0]
				request.RelatedIDs["capabilityCancellation"] = true
				repo.approvalRequests[0] = request
				native.stopped = true
			}
			result, err := service.ApproveApprovalRequest(t.Context(), testPrincipal("admin"), id, domainaigateway.ApprovalDecisionInput{})
			if scenario == "active" {
				if err != nil || docker.calls != 1 || result.Invocation == nil {
					t.Fatalf("active parent did not execute: %v calls=%d", err, docker.calls)
				}
			} else if err == nil || docker.calls != 0 {
				t.Fatalf("stopped or forged parent executed: %v calls=%d", err, docker.calls)
			}
		})
	}
	service, _, _, _, _ := capabilityApprovalFixture(t)
	var absent *nativeParentGuardFixture
	for _, invalid := range []NativeTaskGuard{nil, absent} {
		if err := service.SetNativeTaskGuard(invalid); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("missing guard accepted: %v", err)
		}
	}
}

func TestCapabilityCancellationDoesNotTreatUnknownTerminalOutcomeAsStopped(t *testing.T) {
	service, _, docker, guard, _ := capabilityApprovalFixture(t)
	call := guard.run.NodeRuns[0].PreparedCall
	tool, _ := service.toolByName(call.ToolName)
	for _, acknowledged := range []bool{false, true} {
		docker.operation.Status = "canceled"
		docker.operation.Result = map[string]any{"cancellationAcknowledged": acknowledged}
		task := capabilityTaskRef(tool, docker.operation)
		result := domainaigateway.ToolInvocationResult{Result: "success", Task: task}
		node := service.canceledCapabilityNodeResult(guard.run.NodeRuns[0], result)
		want := "blocked"
		if acknowledged {
			want = "canceled"
		}
		if node.Status != want {
			t.Fatalf("acknowledged=%t state=%s want=%s", acknowledged, node.Status, want)
		}
	}
}

func TestCapabilityTaskViewRechecksCurrentDenyPolicy(t *testing.T) {
	service, repo, _, guard, _ := capabilityApprovalFixture(t)
	intent, _ := domainworkflow.CapabilityIntentFrom(guard.run)
	intent.Input.Plan = domainaigateway.CapabilityPlan{Goal: "inspect my task", Steps: []domainaigateway.CapabilityPlanStep{{ID: "deploy", Call: sohaapi.CapabilityCall{ToolName: "docker.projects.deploy.trigger", CapabilityVersion: "1", Input: map[string]any{"projectId": "project-1", "idempotencyKey": "stable-deploy"}}}}}
	guard.run.Metadata["capabilityIntent"] = intent
	if _, err := service.VisibleCapabilityTask(context.Background(), testPrincipal("developer"), guard.run); err != nil {
		t.Fatal(err)
	}
	repo.accessPolicies = []domainaigateway.AccessPolicy{{ID: "revoked-output", Enabled: true, SubjectType: "role", SubjectID: "developer", Effect: "deny", ToolPatterns: []string{"*"}}}
	if _, err := service.VisibleCapabilityTask(context.Background(), testPrincipal("developer"), guard.run); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("historical task bypassed deny policy: %v", err)
	}
}
