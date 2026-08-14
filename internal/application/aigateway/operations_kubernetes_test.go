package aigateway

import (
	"context"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type fakeKubernetesResourceCreationService struct {
	request domainresource.ResourceCreateRequest
}

func (s *fakeKubernetesResourceCreationService) PreflightCreate(_ context.Context, _ domainidentity.Principal, _ string, request domainresource.ResourceCreateRequest) (domainresource.ResourceCreatePreflight, error) {
	s.request = request
	return domainresource.ResourceCreatePreflight{Ready: true, ContentHash: "hash-1"}, nil
}

func (s *fakeKubernetesResourceCreationService) ExecuteCreate(_ context.Context, _ domainidentity.Principal, _ string, request domainresource.ResourceCreateRequest) (domainresource.ResourceCreateExecution, error) {
	s.request = request
	return domainresource.ResourceCreateExecution{OperationID: "operation-1", ContentHash: "hash-1", Status: "succeeded"}, nil
}

func TestKubernetesResourceCreationToolsReuseResourceCreationService(t *testing.T) {
	repo := &memoryGatewayRepository{accessPolicies: []domainaigateway.AccessPolicy{{
		ID: "allow-k8s-create", Enabled: true, SubjectType: "role", SubjectID: "developer", Effect: "allow",
		ToolPatterns: []string{"k8s.resources.create.*"}, RiskLevels: []domainaigateway.RiskLevel{domainaigateway.RiskLevelAnalyze, domainaigateway.RiskLevelHigh},
		ApprovalPolicy: map[string]any{"strategy": "allow"},
	}}}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformResourceCreationUse},
	}}), nil, repo)
	creation := &fakeKubernetesResourceCreationService{}
	service.SetResourceCreationService(creation)
	principal := testPrincipal("developer")

	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{})
	if err != nil || !hasTool(manifest.Tools, "k8s.resources.create.preflight") || !hasTool(manifest.Tools, "k8s.resources.create.trigger") || !hasSkill(manifest.Skills, "k8s-resource-provisioner") {
		t.Fatalf("resource creation capabilities missing: manifest=%#v err=%v", manifest.Summary, err)
	}

	input := map[string]any{"clusterId": "cluster-1", "source": "global_yaml", "content": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: demo"}
	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{ToolName: "k8s.resources.create.preflight", Input: input})
	if err != nil || result.Result != "success" || creation.request.RequestID != "" {
		t.Fatalf("preflight result=%#v request=%#v err=%v", result, creation.request, err)
	}

	input["idempotencyKey"] = "demo-create-1"
	result, err = service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{ToolName: "k8s.resources.create.trigger", Input: input})
	if err != nil || result.RelatedIDs["operationId"] != "operation-1" || creation.request.RequestID != "demo-create-1" {
		t.Fatalf("trigger result=%#v request=%#v err=%v", result, creation.request, err)
	}
}
