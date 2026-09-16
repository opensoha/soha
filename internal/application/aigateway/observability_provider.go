package aigateway

import (
	"context"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type observabilityCapabilityProvider struct {
	assess func(context.Context, domainidentity.Principal, sohaapi.ObservabilityMetricAssessmentInput) (sohaapi.CapabilityAssessment, error)
	tool   domainaigateway.ToolCapability
}

func NewObservabilityCapabilityProvider(assess func(context.Context, domainidentity.Principal, sohaapi.ObservabilityMetricAssessmentInput) (sohaapi.CapabilityAssessment, error)) (CapabilityProvider, error) {
	schema, err := capabilityOpenAPISchema("ObservabilityMetricAssessmentInput")
	if err != nil {
		return nil, err
	}
	if _, err := compileCapabilityInputSchema(schema); err != nil {
		return nil, err
	}
	return &observabilityCapabilityProvider{assess: assess, tool: domainaigateway.ToolCapability{Name: "observability.metrics.assess", Title: "Assess Metric Conditions", Domain: "observability", Action: "assess", Version: "1", RiskLevel: domainaigateway.RiskLevelRead,
		Description:  "Verify every step of a registered metric window with source-sample freshness, complete coverage and explicit threshold units. Bind notBefore to deployment completion for post-release evidence; no traffic or missing instrumentation remains inconclusive.",
		MCPAdapterID: "observability.v1", MCPToolName: "observability.metrics.assess", InputSchema: schema, PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermObserveMonitoringView}, RequiredScopes: []string{"dataSource"}, ProducesAssessment: true,
		Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}, InputSemantics: []domainaigateway.CapabilityValueSemantic{{Path: "/notBefore", Kind: "time.instant"}}}}, nil
}

func (p *observabilityCapabilityProvider) Tools() []domainaigateway.ToolCapability {
	return []domainaigateway.ToolCapability{p.tool}
}
func (*observabilityCapabilityProvider) Resources() []domainaigateway.ResourceCapability { return nil }
func (*observabilityCapabilityProvider) Prompts() []domainaigateway.PromptCapability     { return nil }
func (*observabilityCapabilityProvider) Skills() []domainaigateway.SkillCapability       { return nil }

func (*observabilityCapabilityProvider) ToolInvocationScopes(_ context.Context, _ domainidentity.Principal, _ domainaigateway.ToolCapability, input map[string]any) ([]map[string]string, error) {
	var request sohaapi.ObservabilityMetricAssessmentInput
	if err := mapInput(input, &request); err != nil {
		return nil, err
	}
	scope := map[string]string{"dataSourceId": request.DataSourceID}
	for key, value := range map[string]string{"clusterId": request.Scope.ClusterID, "namespace": request.Scope.Namespace, "workload": request.Scope.Workload, "service": request.Scope.Service} {
		if value != "" {
			scope[key] = value
		}
	}
	return []map[string]string{scope}, nil
}

func (p *observabilityCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if tool.Name != p.tool.Name || p.assess == nil {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	var request sohaapi.ObservabilityMetricAssessmentInput
	if err := mapInput(input, &request); err != nil {
		return nil, nil, err
	}
	result, err := p.assess(ctx, principal, request)
	return result, nil, err
}
