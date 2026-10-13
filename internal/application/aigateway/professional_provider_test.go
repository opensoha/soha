package aigateway

import (
	"context"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestProfessionalCapabilitiesAcceptOptionalScopeThroughGateway(t *testing.T) {
	calls := []string{}
	provider, err := NewProfessionalCapabilityProvider(func(_ context.Context, _ domainidentity.Principal, kind string, input sohaapi.WorkbenchProfessionalCapabilityInput) (domaincopilot.AnalysisArtifact, error) {
		calls = append(calls, kind)
		if input.SessionID != "owned" {
			t.Fatal("session lost")
		}
		return domaincopilot.AnalysisArtifact{Kind: kind, RunID: "analysis", Summary: "Observed evidence"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse, appaccess.PermObserveAIRootCauseRun, appaccess.PermObserveMonitoringView, appaccess.PermObserveAIView}}}), nil, &memoryGatewayRepository{})
	service.SetCapabilityProviders(provider)
	for _, kind := range []string{"root_cause", "performance", "trace", "inspection_review"} {
		result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "copilot." + kind + ".analyze", CapabilityVersion: "1", Input: map[string]any{"sessionId": "owned", "question": "Explain evidence", "scope": map[string]any{}, "intent": map[string]any{}}})
		if err != nil || result.Result != "success" {
			t.Fatalf("%s optional scope failed: %v %+v", kind, err, result)
		}
	}
	if len(calls) != 4 {
		t.Fatalf("owner calls=%v", calls)
	}
}
