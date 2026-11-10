package aigateway

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type fakeKubernetesWorkloadSnapshotService struct {
	*fakeResourceService
	clusterID string
	request   domainresource.WorkloadSnapshotRequest
}

func (s *fakeKubernetesWorkloadSnapshotService) GenerateWorkloadSnapshot(_ context.Context, _ domainidentity.Principal, clusterID string, request domainresource.WorkloadSnapshotRequest) (domainresource.WorkloadSnapshot, error) {
	s.clusterID = clusterID
	s.request = request
	return domainresource.WorkloadSnapshot{Content: "kind: Job", SourceUID: "source-uid", SelectedContainer: "worker", Containers: []domainresource.WorkloadSnapshotContainer{{Name: "worker", Image: "example/worker:1"}}, Warnings: []string{}}, nil
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
	principal := testPrincipal("developer")
	input := map[string]any{"clusterId": "cluster-1", "source": "global_yaml", "content": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: demo"}
	preflight := domainaigateway.ToolInvocationRequest{ToolName: "k8s.resources.create.preflight", Input: input}
	if _, err := service.InvokeTool(t.Context(), principal, preflight); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("resource creation without its provider returned %v", err)
	}
	creation := &resourceCreationCapabilityFixture{refs: []domainresource.ResourceCreateRef{{APIVersion: "v1", Kind: "Namespace", Name: "demo"}}}
	provider, err := NewResourceCreationCapabilityProvider(creation)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AddCapabilityProviders(provider); err != nil {
		t.Fatal(err)
	}

	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{})
	if err != nil || !hasTool(manifest.Tools, "k8s.resources.create.preflight") || !hasTool(manifest.Tools, "k8s.resources.create.trigger") || !hasSkill(manifest.Skills, "k8s-resource-provisioner") {
		t.Fatalf("resource creation capabilities missing: manifest=%#v err=%v", manifest.Summary, err)
	}

	result, err := service.InvokeTool(t.Context(), principal, preflight)
	if err != nil || result.Result != "success" || creation.preflights != 1 || creation.creates != 0 || creation.request.RequestID != "" {
		t.Fatalf("preflight result=%#v request=%#v err=%v", result, creation.request, err)
	}

	input["idempotencyKey"] = "demo-create-1"
	result, err = service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{ToolName: "k8s.resources.create.trigger", Input: input})
	if err != nil || result.Task == nil || result.Task.ID != "original-operation" || result.RelatedIDs["operationId"] != "original-operation" || creation.creates != 1 || creation.request.RequestID != "demo-create-1" {
		t.Fatalf("trigger result=%#v request=%#v err=%v", result, creation.request, err)
	}
}

func TestKubernetesWorkloadSnapshotToolReusesRuntimeResourceService(t *testing.T) {
	repo := &memoryGatewayRepository{accessPolicies: []domainaigateway.AccessPolicy{{
		ID: "allow-k8s-snapshot", Enabled: true, SubjectType: "role", SubjectID: "developer", Effect: "allow",
		ToolPatterns: []string{"k8s.workloads.snapshot.generate"}, RiskLevels: []domainaigateway.RiskLevel{domainaigateway.RiskLevelAnalyze},
		ApprovalPolicy: map[string]any{"strategy": "allow"},
	}}}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermPlatformResourceCreationUse},
	}}), nil, repo)
	resources := &fakeKubernetesWorkloadSnapshotService{fakeResourceService: &fakeResourceService{}}
	service.SetResourceService(resources)
	principal := testPrincipal("developer")

	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{})
	if err != nil || !hasTool(manifest.Tools, "k8s.workloads.snapshot.generate") || !hasSkill(manifest.Skills, "k8s-resource-provisioner") {
		t.Fatalf("workload snapshot capability missing: manifest=%#v err=%v", manifest.Summary, err)
	}

	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{
		ToolName: "k8s.workloads.snapshot.generate",
		Input: map[string]any{
			"clusterId": "cluster-1", "namespace": "ops", "sourceKind": "Deployment", "sourceName": "api", "sourceContainer": "worker",
			"targetKind": "CronJob", "targetName": "report", "restartPolicy": "Never", "schedule": "0 * * * *", "description": "hourly report",
		},
	})
	if err != nil || result.Result != "success" || result.RelatedIDs["sourceUid"] != "source-uid" {
		t.Fatalf("snapshot result=%#v err=%v", result, err)
	}
	if resources.clusterID != "cluster-1" || resources.request.SourceName != "api" || resources.request.TargetKind != domainresource.WorkloadSnapshotTargetCronJob || resources.request.Description != "hourly report" {
		t.Fatalf("snapshot request=%#v cluster=%q", resources.request, resources.clusterID)
	}
}
