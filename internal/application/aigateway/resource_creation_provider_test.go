package aigateway

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type resourceCreationCapabilityFixture struct {
	creates int
	refs    []domainresource.ResourceCreateRef
	item    domainresource.ResourceCreateExecution
}

func (f *resourceCreationCapabilityFixture) PreflightCreate(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) (domainresource.ResourceCreatePreflight, error) {
	return domainresource.ResourceCreatePreflight{Ready: true}, nil
}
func (f *resourceCreationCapabilityFixture) ResolveCreateTargets(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) ([]domainresource.ResourceCreateRef, error) {
	return f.refs, nil
}
func (f *resourceCreationCapabilityFixture) ExecuteCreate(_ context.Context, _ domainidentity.Principal, cluster string, _ domainresource.ResourceCreateRequest) (domainresource.ResourceCreateExecution, error) {
	f.creates++
	f.item = domainresource.ResourceCreateExecution{OperationID: "original-operation", ClusterID: cluster, Status: "succeeded", ContentHash: strings.Repeat("a", 64), Documents: []domainresource.ResourceCreateExecutionDocument{{Index: 0, Resource: f.refs[0], Status: "succeeded"}}}
	f.item.Documents[0].Resource.UID = "original-uid"
	return f.item, nil
}
func (f *resourceCreationCapabilityFixture) FindCreate(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) (domainresource.ResourceCreateExecution, error) {
	if f.creates == 0 {
		return f.item, apperrors.ErrNotFound
	}
	return f.item, nil
}
func (f *resourceCreationCapabilityFixture) GetCreate(context.Context, domainidentity.Principal, string, string) (domainresource.ResourceCreateExecution, error) {
	return f.item, nil
}
func (f *resourceCreationCapabilityFixture) AssessCreate(context.Context, domainidentity.Principal, string, string) (sohaapi.CapabilityAssessment, error) {
	return sohaapi.CapabilityAssessment{Verdict: "inconclusive", Summary: "no observation", Evidence: []sohaapi.CapabilityEvidence{}}, nil
}

func newCreationGateway(repo *memoryGatewayRepository, provider CapabilityProvider) *Service {
	s := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"admin": {appaccess.PermAIGatewayManage, appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayView, appaccess.PermPlatformResourceCreationUse}}}), nil, repo)
	s.SetCapabilityProviders(provider, BuiltinCapabilityProvider{})
	return s
}

func TestResourceCreationCapabilityPlanApprovalAndRecovery(t *testing.T) {
	ctx, principal := context.Background(), testPrincipal("admin")
	f := &resourceCreationCapabilityFixture{refs: []domainresource.ResourceCreateRef{{APIVersion: "v1", Kind: "Service", Name: "api", Namespace: "team-a", Namespaced: true}}}
	provider, err := NewResourceCreationCapabilityProvider(f)
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryGatewayRepository{}
	s := newCreationGateway(repo, provider)
	count := 0
	for _, skill := range s.gatewayRegistry().Skills() {
		if skill.ID == "k8s-resource-provisioner" {
			count++
			if !slices.Contains(skill.CapabilityRefs, "k8s.resources.create.assess") {
				t.Fatal("legacy skill shadowed provider verification")
			}
		}
	}
	if count != 1 {
		t.Fatalf("duplicate creation skills: %d", count)
	}
	input := map[string]any{"clusterId": "cluster", "source": "global_yaml", "content": "apiVersion: v1\nkind: Service\nmetadata:\n  name: api\n  namespace: team-a", "idempotencyKey": "original-key"}
	plan := domainai.CapabilityTaskInput{IdempotencyKey: "goal-key", SkillID: "k8s-resource-provisioner", Plan: domainai.CapabilityPlan{Goal: "Create a Kubernetes Service", VerificationSteps: []string{"verify"}, Steps: []domainai.CapabilityPlanStep{
		{ID: "create", Call: sohaapi.CapabilityCall{ToolName: "k8s.resources.create.trigger", CapabilityVersion: "1", Input: input}},
		{ID: "verify", DependsOn: []string{"create"}, Call: sohaapi.CapabilityCall{ToolName: "k8s.resources.create.assess", CapabilityVersion: "1", Input: map[string]any{"clusterId": "cluster"}}, Bindings: []domainai.CapabilityInputBinding{{StepID: "create", OutputPath: "/operationId", InputPath: "/operationId"}}},
	}}}
	validation, err := s.ValidateCapabilityPlan(ctx, principal, plan)
	if err != nil || !validation.Valid {
		t.Fatalf("shared plan rejected: %+v %v", validation, err)
	}
	call := domainai.ToolInvocationRequest{ToolName: "k8s.resources.create.trigger", CapabilityVersion: "1", Input: input}
	held, err := s.InvokeTool(ctx, principal, call)
	if err != nil || !held.RequiresApproval || f.creates != 0 {
		t.Fatalf("approval escaped: %+v %v", held, err)
	}
	approved, err := s.ApproveApprovalRequest(ctx, principal, repo.approvalRequests[0].ID, domainai.ApprovalDecisionInput{})
	if err != nil || approved.Invocation == nil || approved.Invocation.Task == nil || !approved.Invocation.Task.Terminal || f.creates != 1 {
		t.Fatalf("create task: %+v %v", approved, err)
	}
	if approved.Invocation.Task.Outcome != "succeeded" || approved.Invocation.Task.CancelCall != nil {
		t.Fatalf("unsupported cancellation or wrong outcome: %+v", approved.Invocation.Task)
	}
	verifyCreationReceiptSchema(t, provider.Tools()[1], approved.Invocation.Output)
	verifyResourceCreationRecovery(t, repo, provider, f, call)
}

func verifyCreationReceiptSchema(t *testing.T, tool domainai.ToolCapability, output any) {
	t.Helper()
	outputSchema, err := compileCapabilityInputSchema(tool.OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	data, err := capabilityJSON(output)
	if err != nil || outputSchema.Validate(data) != nil {
		t.Fatalf("receipt contract: %+v %v", data, err)
	}
}

func verifyResourceCreationRecovery(t *testing.T, repo *memoryGatewayRepository, provider CapabilityProvider, f *resourceCreationCapabilityFixture, call domainai.ToolInvocationRequest) {
	t.Helper()
	ctx := context.Background()
	principal := testPrincipal("admin")
	recreated := newCreationGateway(repo, provider)
	recovered, err := recreated.recoverCapabilityCall(ctx, principal, call)
	if err != nil || recovered == nil || recovered.Task == nil || recovered.Task.ID != "original-operation" || f.creates != 1 {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	f.item.Status = "running"
	recovered, err = recreated.recoverCapabilityCall(ctx, principal, call)
	if err != nil || recovered.Task.Terminal || recovered.Task.Outcome != "unknown" || f.creates != 1 {
		t.Fatalf("unknown receipt repeated or claimed complete: %+v %v", recovered, err)
	}
	visible := map[string]any{"operationId": "original-operation", "status": "running"}
	taskReferenceProvider, ok := provider.(ToolTaskReferenceProvider)
	if !ok {
		t.Fatalf("provider type = %T, want ToolTaskReferenceProvider", provider)
	}
	if taskReferenceProvider.TaskReference(provider.Tools()[1], f.item, visible) != nil {
		t.Fatal("task restored a redacted cluster identity")
	}
}

func TestResourceCreationCapabilityChecksAllManifestNamespaces(t *testing.T) {
	f := &resourceCreationCapabilityFixture{refs: []domainresource.ResourceCreateRef{{APIVersion: "v1", Kind: "Service", Name: "one", Namespace: "team-a", Namespaced: true}, {APIVersion: "v1", Kind: "Service", Name: "two", Namespace: "team-b", Namespaced: true}}}
	p, err := NewResourceCreationCapabilityProvider(f)
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryGatewayRepository{accessPolicies: []domainai.AccessPolicy{{ID: "team-a-only", Enabled: true, SubjectType: "role", SubjectID: "admin", Effect: "allow", ToolPatterns: []string{"k8s.resources.create.*"}, RiskLevels: []domainai.RiskLevel{domainai.RiskLevelHigh}, ResourceScopes: map[string]any{"namespace": []string{"team-a"}}, ApprovalPolicy: map[string]any{"strategy": "allow"}}}}
	s := newCreationGateway(repo, p)
	_, err = s.InvokeTool(context.Background(), testPrincipal("admin"), domainai.ToolInvocationRequest{ToolName: "k8s.resources.create.trigger", CapabilityVersion: "1", Input: map[string]any{"clusterId": "cluster", "source": "global_yaml", "content": "two manifests", "idempotencyKey": "key"}})
	if !errors.Is(err, apperrors.ErrAccessDenied) || f.creates != 0 {
		t.Fatalf("second namespace escaped policy: %v creates=%d", err, f.creates)
	}
}
