package aigateway

import (
	"context"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type projectCapabilityFixture struct {
	item    domaindocker.Project
	creates int
}

func (f *projectCapabilityFixture) CreateProject(_ context.Context, _ domainidentity.Principal, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	f.creates++
	f.item = domaindocker.Project{ID: "project", HostID: input.HostID, Name: input.Name, ComposeContent: input.ComposeContent, EnvContent: input.EnvContent}
	return f.item, nil
}
func (f *projectCapabilityFixture) FindProjectCreation(context.Context, domainidentity.Principal, domaindocker.ProjectInput) (domaindocker.Project, error) {
	if f.creates == 0 {
		return domaindocker.Project{}, apperrors.ErrNotFound
	}
	return f.item, nil
}
func (f *projectCapabilityFixture) GetProject(context.Context, domainidentity.Principal, string) (domaindocker.Project, error) {
	return f.item, nil
}

func TestDockerProjectCapabilityApprovalAndRecovery(t *testing.T) {
	fixture := &projectCapabilityFixture{}
	provider, err := NewDockerProjectCapabilityProvider(fixture)
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryGatewayRepository{}
	service := newProjectGatewayTestService(repo)
	service.SetCapabilityProviders(provider)
	principal := testPrincipal("admin")
	call := domainaigateway.ToolInvocationRequest{ToolName: "docker.projects.create", CapabilityVersion: "1", Input: map[string]any{"hostId": "host", "name": "api", "composeContent": "services:\n  api:\n    image: old\n", "idempotencyKey": "stable-project-creation"}}
	held, err := service.InvokeTool(context.Background(), principal, call)
	if err != nil || !held.RequiresApproval || fixture.creates != 0 {
		t.Fatalf("approval escaped: %+v %v", held, err)
	}
	approved, err := service.ApproveApprovalRequest(context.Background(), principal, repo.approvalRequests[0].ID, domainaigateway.ApprovalDecisionInput{})
	if err != nil || approved.Invocation == nil || fixture.creates != 1 {
		t.Fatalf("creation missing: %+v %v", approved, err)
	}
	recreated := newProjectGatewayTestService(repo)
	recreated.SetCapabilityProviders(provider)
	recovered, err := recreated.recoverCapabilityCall(context.Background(), principal, call)
	if err != nil || recovered == nil || fixture.creates != 1 {
		t.Fatalf("creation recovery: %+v %v", recovered, err)
	}
	projectProvider, ok := provider.(*dockerProjectCapabilityProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *dockerProjectCapabilityProvider", provider)
	}
	visible, _, err := projectProvider.InvokeTool(context.Background(), principal, provider.Tools()[1], map[string]any{"projectId": "project"})
	project, ok := visible.(domaindocker.Project)
	if err != nil || !ok || project.ComposeContent != "" {
		t.Fatalf("project config exposed: %+v %v", visible, err)
	}
}

func newProjectGatewayTestService(repo *memoryGatewayRepository) *Service {
	return newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"admin": {appaccess.PermAIGatewayManage, appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayView, appaccess.PermDockerProjectsView, appaccess.ManagedActionPermission(appaccess.PermDockerProjectsManage, "create")}}}), nil, repo)
}

func TestDockerProjectPlanBindsCreationDeploymentAndAssessment(t *testing.T) {
	provider, err := NewDockerProjectCapabilityProvider(&projectCapabilityFixture{})
	if err != nil {
		t.Fatal(err)
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"admin": {
		appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke,
		appaccess.ManagedActionPermission(appaccess.PermDockerProjectsManage, "create"),
		appaccess.PermDockerProjectsView, appaccess.PermDockerProjectsDeploy,
		appaccess.PermDockerTemplatesView, appaccess.PermDockerServicesView, appaccess.PermDockerOperationsView,
	}}}), nil)
	service.SetCapabilityProviders(provider, BuiltinCapabilityProvider{})
	projectBinding := sohaapi.CapabilityInputBinding{InputPath: "/projectId", StepID: "project", OutputPath: "/id"}
	input := domainaigateway.CapabilityTaskInput{IdempotencyKey: "compose-goal", Plan: domainaigateway.CapabilityPlan{
		Goal: "Create and deploy a Compose project and assess its runtime", VerificationSteps: []string{"assess"},
		Steps: []domainaigateway.CapabilityPlanStep{
			{ID: "project", Call: sohaapi.CapabilityCall{ToolName: "docker.projects.create", CapabilityVersion: "1", Input: map[string]any{"hostId": "host", "name": "api", "composeContent": "services:\n  api:\n    image: nginx\n", "idempotencyKey": "create-project"}}},
			{ID: "preflight", DependsOn: []string{"project"}, Bindings: []sohaapi.CapabilityInputBinding{projectBinding}, Call: sohaapi.CapabilityCall{ToolName: "docker.projects.deploy.plan", CapabilityVersion: "1", Input: map[string]any{}}},
			{ID: "deploy", DependsOn: []string{"project", "preflight"}, Bindings: []sohaapi.CapabilityInputBinding{projectBinding}, Call: sohaapi.CapabilityCall{ToolName: "docker.projects.deploy.trigger", CapabilityVersion: "1", Input: map[string]any{"idempotencyKey": "deploy-project"}}},
			{ID: "receipt", DependsOn: []string{"deploy"}, Bindings: []sohaapi.CapabilityInputBinding{{InputPath: "/operationId", StepID: "deploy", OutputPath: "/id"}}, Call: sohaapi.CapabilityCall{ToolName: "docker.operations.get", CapabilityVersion: "1", Input: map[string]any{}}},
			{ID: "assess", DependsOn: []string{"project", "deploy", "receipt"}, Bindings: []sohaapi.CapabilityInputBinding{projectBinding, {InputPath: "/afterOperationId", StepID: "deploy", OutputPath: "/id"}}, Call: sohaapi.CapabilityCall{ToolName: "docker.projects.runtime.assess", CapabilityVersion: "1", Input: map[string]any{"expectedServices": []string{"api"}}}},
		},
	}}
	validation, err := service.ValidateCapabilityPlan(context.Background(), testPrincipal("admin"), input)
	if err != nil || !validation.Valid {
		t.Fatalf("registered Docker capabilities cannot compose: %+v %v", validation, err)
	}
	completed := map[string]domainaigateway.ToolInvocationResult{"project": {Result: "success", Output: map[string]any{"id": "created-project"}}}
	resolved, err := service.ResolveCapabilityStep(input.Plan.Steps[2], input.Plan, completed)
	if err != nil || resolved.Input["projectId"] != "created-project" {
		t.Fatalf("created project identity did not reach deployment: %+v %v", resolved, err)
	}
}
