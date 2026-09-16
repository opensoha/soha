package aigateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestMetricCapabilityScopesBlockQueriesBeforeProviderAndBindDeploymentTime(t *testing.T) {
	called := 0
	provider, err := NewObservabilityCapabilityProvider(func(_ context.Context, _ domainidentity.Principal, _ sohaapi.ObservabilityMetricAssessmentInput) (sohaapi.CapabilityAssessment, error) {
		called++
		return sohaapi.CapabilityAssessment{Verdict: "inconclusive", Summary: "waiting for complete samples", Evidence: []sohaapi.CapabilityEvidence{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayView, appaccess.PermObserveMonitoringView}}}), nil, repo)
	service.SetCapabilityProviders(provider)
	input := map[string]any{"dataSourceId": "source", "metricKey": "cpu_usage", "scope": map[string]any{"clusterId": "cluster-a", "namespace": "app"}, "windowStart": "2026-09-15T10:00:00Z", "windowEnd": "2026-09-15T10:01:00Z", "unit": "cores", "threshold": map[string]any{"operator": "lte", "value": 2.0}}
	call := domainaigateway.ToolInvocationRequest{ToolName: "observability.metrics.assess", CapabilityVersion: "1", Input: input}
	repo.toolGrants = []domainaigateway.ToolGrant{{ID: "grant", SubjectType: "user", SubjectID: "user-1", ToolName: call.ToolName, Effect: "allow", ResourceScopes: map[string]any{"clusterIds": []string{"cluster-b"}}}}
	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call); !errors.Is(err, apperrors.ErrAccessDenied) || called != 0 {
		t.Fatalf("query escaped actual nested scope: %v calls=%d", err, called)
	}
	repo.toolGrants[0].ResourceScopes = map[string]any{"clusterIds": []string{"cluster-a"}, "dataSourceId": "source"}
	if result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call); err != nil || result.RequiresApproval || called != 1 {
		t.Fatalf("authorized query failed: %+v %v", result, err)
	}
	deliveryProvider, err := NewDeliveryCapabilityProvider(&deliveryProviderFixture{})
	if err != nil {
		t.Fatal(err)
	}
	service.AddCapabilityProviders(deliveryProvider)
	step := domainaigateway.CapabilityPlanStep{ID: "metrics", DependsOn: []string{"runtime"}, Call: sohaapi.CapabilityCall{ToolName: call.ToolName, CapabilityVersion: "1", Input: input}, Bindings: []domainaigateway.CapabilityInputBinding{{InputPath: "/notBefore", StepID: "runtime", OutputPath: "/deployedAt"}}}
	plan := domainaigateway.CapabilityPlan{Steps: []domainaigateway.CapabilityPlanStep{{ID: "runtime", Call: sohaapi.CapabilityCall{ToolName: "delivery.batches.assess", CapabilityVersion: "1", Input: map[string]any{"batchId": "batch", "targetId": "api"}}}, step}}
	deployedAt := time.Now().UTC().Format(time.RFC3339)
	resolved, err := service.ResolveCapabilityStep(step, plan, map[string]domainaigateway.ToolInvocationResult{"runtime": {Result: "success", Output: map[string]any{"deployedAt": deployedAt}}})
	if err != nil || resolved.Input["notBefore"] != deployedAt {
		t.Fatalf("post-release binding failed: %+v %v", resolved, err)
	}
}
