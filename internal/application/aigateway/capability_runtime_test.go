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
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type capabilityApprovalRepositoryFixture struct{ *memoryGatewayRepository }

func (r capabilityApprovalRepositoryFixture) GetApprovalRequest(ctx context.Context, id string) (domainaigateway.ApprovalRequest, error) {
	request, err := r.memoryGatewayRepository.GetApprovalRequest(ctx, id)
	if err == nil && request.ID == "" {
		err = apperrors.ErrNotFound
	}
	return request, err
}

type capabilityGuardFixture struct{ run domainworkflow.Run }

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
	primary := capabilityApprovalID(guard.run.NodeRuns[0].PreparedCall.RequestID)
	approved, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), primary, domainaigateway.ApprovalDecisionInput{})
	if err != nil {
		t.Fatal(err)
	}
	guard.run.NodeRuns[0].Invocation = approved.Invocation
	guard.run.NodeRuns[0].Status = "waiting_execution"
	guard.run.StopReason = "user"
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
