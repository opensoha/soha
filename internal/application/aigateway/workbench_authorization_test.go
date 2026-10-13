package aigateway

import (
	"errors"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestNativePlanValidationKeepsExecutionAndPublicViewPermissionsSeparate(t *testing.T) {
	repo, docker := &memoryGatewayRepository{}, &contractDockerService{}
	service := contractTestService(repo, docker)
	service.permissions = appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"operator": {appaccess.PermObserveAIChatUse, appaccess.PermAIGatewayInvoke, appaccess.PermDockerProjectsView, appaccess.PermDockerTemplatesView, appaccess.PermDockerProjectsDeploy, appaccess.PermDockerOperationsView, appaccess.PermDockerServicesView, appaccess.ManagedActionPermission(appaccess.PermDockerOperationsManage, "cancel")}}})
	input := domainaigateway.CapabilityTaskInput{IdempotencyKey: "native:task", Plan: domainaigateway.CapabilityPlan{Goal: "Deploy and verify the actual operation", TimeoutSeconds: 600, VerificationSteps: []string{"assess"}, Steps: []domainaigateway.CapabilityPlanStep{
		{ID: "plan", Call: sohaapi.CapabilityCall{ToolName: "docker.projects.deploy.plan", CapabilityVersion: "1", Input: map[string]any{"projectId": "project-1"}}},
		{ID: "deploy", DependsOn: []string{"plan"}, Call: sohaapi.CapabilityCall{ToolName: "docker.projects.deploy.trigger", CapabilityVersion: "1", Input: map[string]any{"projectId": "project-1", "idempotencyKey": "native:operation"}}},
		{ID: "assess", DependsOn: []string{"deploy"}, Call: sohaapi.CapabilityCall{ToolName: "docker.projects.runtime.assess", CapabilityVersion: "1", Input: map[string]any{"projectId": "project-1", "expectedServices": []string{"web"}}}, Bindings: []sohaapi.CapabilityInputBinding{{StepID: "deploy", OutputPath: "/id", InputPath: "/afterOperationId"}}},
	}}}
	principal := testPrincipal("operator")
	if _, err := service.ValidateCapabilityPlan(t.Context(), principal, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("public discovery gate changed: %v", err)
	}
	validation, err := service.ValidateCapabilityPlan(WithNativeWorkbenchInvocation(t.Context()), principal, input)
	if err != nil || !validation.Valid {
		t.Fatalf("Native execution incorrectly requires Gateway view: %+v %v", validation, err)
	}
	if docker.calls != 0 || len(repo.approvalRequests) != 0 {
		t.Fatal("plan validation dispatched or approved the write")
	}
}

func TestNativeCannotCreateApprovalDetachedFromItsParentTask(t *testing.T) {
	repo := &memoryGatewayRepository{}
	docker := &contractDockerService{}
	service := contractTestService(repo, docker)
	input := domainaigateway.ToolInvocationRequest{ToolName: "docker.projects.deploy.trigger", Input: map[string]any{"projectId": "project-1", "idempotencyKey": "intent-1"}}
	_, err := service.InvokeTool(WithNativeWorkbenchInvocation(t.Context()), testPrincipal("developer"), input)
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) || docker.calls != 0 || len(repo.approvalRequests) != 0 {
		t.Fatalf("Native created unowned approval: err=%v dispatches=%d approvals=%d", err, docker.calls, len(repo.approvalRequests))
	}
	if _, err := service.InvokeTool(t.Context(), testPrincipal("developer"), input); err != nil || len(repo.approvalRequests) != 1 || docker.calls != 0 {
		t.Fatalf("legacy domain approval changed: err=%v approvals=%d", err, len(repo.approvalRequests))
	}
}

func TestWorkbenchAuthorizationCeilingRejectsExpandedGrantBeforeDispatchAndApproval(t *testing.T) {
	for _, change := range []string{"unchanged", "grant_scope", "new_grant", "policy", "skill_binding"} {
		t.Run(change, func(t *testing.T) {
			repo := &memoryGatewayRepository{toolGrants: []domainaigateway.ToolGrant{{ID: "grant", SubjectType: "user", SubjectID: "user-1", ToolName: "docker.operations.get", Effect: "allow", ResourceScopes: map[string]any{"operationId": []string{"operation-1"}}}}}
			docker := &contractDockerService{}
			service := contractTestService(repo, docker)
			service.toolGrants, service.accessPolicies, service.skillBindings = repo, repo, repo
			principal := testPrincipal("developer")
			revision, err := service.WorkbenchAuthorizationRevision(t.Context(), principal)
			if err != nil {
				t.Fatal(err)
			}
			ctx := WithWorkbenchAuthorizationCeiling(t.Context(), revision)
			switch change {
			case "grant_scope":
				repo.toolGrants[0].ResourceScopes["operationId"] = []string{"operation-1", "operation-2"}
			case "new_grant":
				repo.toolGrants = append(repo.toolGrants, domainaigateway.ToolGrant{ID: "new", SubjectType: "user", SubjectID: "user-1", ToolName: "docker.*", Effect: "allow"})
			case "policy":
				repo.accessPolicies = []domainaigateway.AccessPolicy{{ID: "policy", Enabled: true, SubjectType: "user", SubjectID: "user-1", Effect: "allow", ToolPatterns: []string{"docker.*"}}}
			case "skill_binding":
				repo.skillBindings = []domainaigateway.SkillBinding{{ID: "binding", Enabled: true, SubjectType: "user", SubjectID: "user-1", SkillID: "docker-operator"}}
			}
			_, err = service.withWorkbenchAuthorization(ctx, principal, domainaigateway.ToolInvocationRequest{})
			if change == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("changed scope accepted: %v", err)
			}
			_, err = service.InvokeTool(ctx, principal, domainaigateway.ToolInvocationRequest{ToolName: "docker.operations.get", Input: map[string]any{"operationId": "operation-2"}})
			if !errors.Is(err, apperrors.ErrAccessDenied) || docker.calls != 0 {
				t.Fatalf("changed scope dispatched: %v calls=%d", err, docker.calls)
			}
			if _, err := service.activeToolGrants(ctx, principal, ""); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("approval grant read lost ceiling: %v", err)
			}
			if _, err := service.activeAccessPolicies(ctx, principal, ""); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("approval policy read lost ceiling: %v", err)
			}
		})
	}
}
