package aigateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type professionalCapabilityProvider struct {
	invoke func(context.Context, domainidentity.Principal, string, sohaapi.WorkbenchProfessionalCapabilityInput) (domaincopilot.AnalysisArtifact, error)
	tools  []domainaigateway.ToolCapability
}

func NewProfessionalCapabilityProvider(invoke func(context.Context, domainidentity.Principal, string, sohaapi.WorkbenchProfessionalCapabilityInput) (domaincopilot.AnalysisArtifact, error)) (CapabilityProvider, error) {
	if invoke == nil {
		return nil, fmt.Errorf("professional capability owner is required")
	}
	schema, err := capabilityOpenAPISchema("WorkbenchProfessionalCapabilityInput")
	if err != nil {
		return nil, err
	}
	if _, err = compileCapabilityInputSchema(schema); err != nil {
		return nil, err
	}
	provider := &professionalCapabilityProvider{invoke: invoke}
	for _, kind := range []string{"root_cause", "performance", "trace", "inspection_review"} {
		permission := appaccess.PermObserveMonitoringView
		if kind == "root_cause" {
			permission = appaccess.PermObserveAIRootCauseRun
		}
		if kind == "inspection_review" {
			permission = appaccess.PermObserveAIView
		}
		name := "copilot." + kind + ".analyze"
		provider.tools = append(provider.tools, domainaigateway.ToolCapability{Name: name, Title: kind, Description: "Read authorized " + kind + " evidence for this task. Requires explicit target and query parameters; no evidence is reported as a verified result. Does not change session mode or schedule inspections.", Domain: "copilot", Action: "analyze", Version: "1", RiskLevel: domainaigateway.RiskLevelAnalyze, PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse, permission}, InputSchema: schema, Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: false}, MCPAdapterID: "professional.v1", MCPToolName: name})
	}
	return provider, nil
}
func (p *professionalCapabilityProvider) Tools() []domainaigateway.ToolCapability       { return p.tools }
func (*professionalCapabilityProvider) Resources() []domainaigateway.ResourceCapability { return nil }
func (*professionalCapabilityProvider) Prompts() []domainaigateway.PromptCapability     { return nil }
func (*professionalCapabilityProvider) Skills() []domainaigateway.SkillCapability       { return nil }
func (*professionalCapabilityProvider) ToolInvocationScopes(_ context.Context, _ domainidentity.Principal, _ domainaigateway.ToolCapability, input map[string]any) ([]map[string]string, error) {
	var request sohaapi.WorkbenchProfessionalCapabilityInput
	if err := mapInput(input, &request); err != nil {
		return nil, err
	}
	scope := map[string]string{"sessionId": request.SessionID}
	for key, value := range map[string]string{"clusterId": request.Scope.ClusterID, "namespace": request.Scope.Namespace, "workload": request.Scope.Workload, "service": request.Scope.Service, "pod": request.Scope.Pod, "node": request.Scope.Node, "alertId": request.Scope.AlertID, "dataSourceId": request.Intent.DataSourceID} {
		if value != "" {
			scope[key] = value
		}
	}
	return []map[string]string{scope}, nil
}
func (p *professionalCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if !strings.HasPrefix(tool.Name, "copilot.") || !strings.HasSuffix(tool.Name, ".analyze") {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	var request sohaapi.WorkbenchProfessionalCapabilityInput
	if err := mapInput(input, &request); err != nil {
		return nil, nil, err
	}
	kind := strings.TrimSuffix(strings.TrimPrefix(tool.Name, "copilot."), ".analyze")
	result, err := p.invoke(ctx, principal, kind, request)
	return result, map[string]any{"analysisRunId": result.RunID}, err
}
