package aigateway

import (
	"context"
	"fmt"

	contractresource "github.com/opensoha/soha-contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ConditionalResourceCapabilityService interface {
	GetResourceIdentity(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.ResourceIdentity, error)
	PlanResourceYAMLConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ConditionalResourceYAMLInput) (domainoperation.Plan, error)
	ApplyResourceYAMLConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ConditionalResourceYAMLInput) (domainresource.ResourceYAMLView, error)
	DeleteResourceConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ResourceMutationPreconditions) (contractresource.ResourceDeletionReceipt, error)
}

type conditionalWorkloadCapabilityService interface {
	ExecuteWorkloadActionConditional(context.Context, domainidentity.Principal, string, string, string, string, string, contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error)
}

type conditionalOriginalMutationService interface {
	ExecuteOriginalConditionalMutation(context.Context, domainidentity.Principal, domainai.ToolInvocationRequest) (map[string]any, error)
	FindOriginalConditionalMutation(context.Context, domainidentity.Principal, domainai.ToolInvocationRequest) (map[string]any, error)
}

type conditionalMutationAssessmentService interface {
	AssessOriginalConditionalMutation(context.Context, domainidentity.Principal, domainai.ToolInvocationRequest) (domainai.CapabilityAssessment, error)
}

type conditionalResourceCapabilityProvider struct {
	service ConditionalResourceCapabilityService
	tools   []domainai.ToolCapability
}

func NewConditionalResourceCapabilityProvider(service ConditionalResourceCapabilityService) (CapabilityProvider, error) {
	guard, err := capabilityOpenAPISchema("KubernetesResourceMutationPreconditions")
	if err != nil {
		return nil, err
	}
	target := func() map[string]any {
		return map[string]any{
			"clusterId": map[string]any{"type": "string", "minLength": 1},
			"namespace": map[string]any{"type": "string"},
			"kind":      map[string]any{"type": "string", "minLength": 1},
			"name":      map[string]any{"type": "string", "minLength": 1},
		}
	}
	read := gatewayObjectSchema([]string{"clusterId", "namespace", "kind", "name"}, target())
	mutate := target()
	mutate["preconditions"] = guard
	deleteSchema := gatewayObjectSchema([]string{"clusterId", "namespace", "kind", "name", "preconditions"}, mutate)
	edit := target()
	edit["preconditions"], edit["content"] = guard, map[string]any{"type": "string", "minLength": 1, "maxLength": 4 << 20}
	editSchema := gatewayObjectSchema([]string{"clusterId", "namespace", "kind", "name", "preconditions", "content"}, edit)
	workload := target()
	workload["kind"] = map[string]any{"type": "string", "enum": []string{"Deployment", "StatefulSet", "DaemonSet"}}
	workload["action"] = map[string]any{"type": "string", "enum": []string{"restart", "scale", "rollback"}}
	workload["preconditions"] = guard
	workload["replicas"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 2147483647}
	workload["revision"] = map[string]any{"type": "string", "pattern": "^[1-9][0-9]{0,18}$"}
	workloadSchema := gatewayObjectSchema([]string{"clusterId", "namespace", "kind", "name", "action", "preconditions"}, workload)
	assessmentInput := target()
	assessmentInput["mutationRequestId"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 128}
	assessmentOutput, err := capabilityOpenAPISchema("CapabilityAssessment")
	if err != nil {
		return nil, err
	}
	tools := []domainai.ToolCapability{
		{Name: "k8s.resources.identity.read", Title: "Read Kubernetes Resource Identity", Description: "Read only the UID, resourceVersion and deletion metadata. Never returns Secret data.", Action: "read", RiskLevel: domainai.RiskLevelRead, InputSchema: read},
		{Name: "k8s.resources.conditional.preflight", Title: "Preflight Conditional Resource Update", Description: "Dry-run the exact reviewed YAML with mandatory UID/resourceVersion. Preserves external ownership and field conflicts.", Action: "preflight", RiskLevel: domainai.RiskLevelAnalyze, InputSchema: editSchema},
		{Name: "k8s.resources.conditional.update", Title: "Update Reviewed Kubernetes Resource", Description: "Apply approved YAML only to the original UID and resourceVersion. Never force field ownership or fall back to an older Agent.", Action: "update", RiskLevel: domainai.RiskLevelHigh, RequiresApproval: true, InputSchema: editSchema},
		{Name: "k8s.resources.conditional.delete", Title: "Request Reviewed Resource Deletion", Description: "Request deletion with Kubernetes UID and resourceVersion preconditions. Never removes finalizers; acceptance is not deletion completion.", Action: "delete", RiskLevel: domainai.RiskLevelHigh, RequiresApproval: true, InputSchema: deleteSchema},
		{Name: "k8s.workloads.conditional.preflight", Title: "Preflight Reviewed Workload Action", Description: "Kubernetes dry-run for restart, scale or Deployment rollback with the exact action permission and reviewed UID/version. Never writes.", Action: "preflight", RiskLevel: domainai.RiskLevelAnalyze, InputSchema: workloadSchema},
		{Name: "k8s.workloads.conditional.action", Title: "Perform Reviewed Workload Action", Description: "Perform the approved typed restart, scale or Deployment rollback against the original UID/version. Observe rollout readiness separately.", Action: "execute", RiskLevel: domainai.RiskLevelHigh, RequiresApproval: true, InputSchema: workloadSchema},
		{Name: "k8s.resources.conditional.assess", Title: "Verify Original Conditional Resource Effect", Description: "Read the original actor/credential-bound receipt and current metadata. For updates/actions, satisfied means the acknowledged UID/revision remains current. For acknowledged deletion, only a complete versioned metadata collection can prove the original UID absent; a replacement is left untouched. Unknown outcomes and generic 404 are never success. Rollout, related-resource cleanup and business health are not assessed.", Action: "assess", RiskLevel: domainai.RiskLevelRead, ProducesAssessment: true, InputSchema: gatewayObjectSchema([]string{"clusterId", "namespace", "kind", "name", "mutationRequestId"}, assessmentInput), OutputSchema: assessmentOutput},
	}
	tools = appendWorkloadScaleAssessment(tools, service, target(), guard, assessmentOutput)
	for i := range tools {
		tools[i].Version, tools[i].Domain, tools[i].MCPAdapterID, tools[i].MCPToolName = "1", "k8s", "k8s.v1", tools[i].Name
		tools[i].PermissionKeys = []string{appaccess.PermAIGatewayInvoke}
		tools[i].RequiredScopes = []string{"cluster", "namespace"}
		tools[i].Execution = &domainai.ToolExecutionContract{Mode: "sync", Idempotent: tools[i].RiskLevel != domainai.RiskLevelHigh}
		if _, supported := service.(conditionalOriginalMutationService); supported && conditionalMutationTool(tools[i].Name) {
			tools[i].Execution.Idempotent, tools[i].Execution.RecoveryMode = true, "original_call"
			tools[i].Description += " Approved writes claim the original actor/credential-bound request once. Unknown outcomes are never replayed; durable plans require protected storage where applicable."
			configureConditionalMutationChecks(&tools[i])
		}
		configureConditionalAssessmentSemantics(&tools[i])
		if _, err := compileCapabilityInputSchema(tools[i].InputSchema); err != nil {
			return nil, fmt.Errorf("compile conditional resource capability: %w", err)
		}
	}
	return &conditionalResourceCapabilityProvider{service: service, tools: tools}, nil
}

func (p *conditionalResourceCapabilityProvider) Tools() []domainai.ToolCapability       { return p.tools }
func (*conditionalResourceCapabilityProvider) Resources() []domainai.ResourceCapability { return nil }
func (*conditionalResourceCapabilityProvider) Prompts() []domainai.PromptCapability     { return nil }
func (*conditionalResourceCapabilityProvider) Skills() []domainai.SkillCapability       { return nil }

func (p *conditionalResourceCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, map[string]any, error) {
	clusterID, namespace, kind, name := firstMapString(input, "clusterId"), firstMapString(input, "namespace"), firstMapString(input, "kind"), firstMapString(input, "name")
	if tool.Name == "k8s.resources.conditional.assess" {
		return p.invokeConditionalAssessment(ctx, principal, tool, input)
	}
	if tool.Name == workloadScaleAssessmentTool {
		return p.invokeWorkloadScaleAssessment(ctx, principal, tool, input)
	}
	if service, ok := p.service.(conditionalOriginalMutationService); ok && conditionalMutationTool(tool.Name) {
		authorization, accepted := domainai.ExecutionAuthorizationFrom(ctx)
		if !accepted || authorization.Call.ToolName != tool.Name || !executionJSONEqual(authorization.Call.Input, input) {
			return nil, nil, apperrors.ErrAccessDenied
		}
		output, err := service.ExecuteOriginalConditionalMutation(ctx, principal, authorization.Call)
		return output, map[string]any{"clusterId": clusterID, "namespace": namespace, "resourceKind": kind, "resourceName": name}, err
	}
	var req contractresource.ConditionalResourceYAMLInput
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	var result any
	var err error
	switch tool.Name {
	case "k8s.resources.identity.read":
		result, err = p.service.GetResourceIdentity(ctx, principal, clusterID, namespace, kind, name)
	case "k8s.resources.conditional.preflight":
		result, err = p.service.PlanResourceYAMLConditional(ctx, principal, clusterID, namespace, kind, name, req)
	case "k8s.resources.conditional.update":
		var item domainresource.ResourceYAMLView
		item, err = p.service.ApplyResourceYAMLConditional(ctx, principal, clusterID, namespace, kind, name, req)
		// MCP receipts never echo the YAML (which may contain sensitive data).
		if err == nil {
			result = map[string]any{"applied": true, "kind": item.Kind, "name": item.Name, "namespace": item.Namespace, "preconditions": req.Preconditions}
		}
	case "k8s.resources.conditional.delete":
		result, err = p.service.DeleteResourceConditional(ctx, principal, clusterID, namespace, kind, name, req.Preconditions)
	case "k8s.workloads.conditional.preflight", "k8s.workloads.conditional.action":
		service, ok := p.service.(conditionalWorkloadCapabilityService)
		if !ok {
			return nil, nil, apperrors.ErrUnsupportedOperation
		}
		var actionInput contractresource.ConditionalWorkloadActionInput
		if err := mapInput(input, &actionInput); err != nil {
			return nil, nil, err
		}
		actionInput.DryRun = tool.Name == "k8s.workloads.conditional.preflight"
		result, err = service.ExecuteWorkloadActionConditional(ctx, principal, clusterID, namespace, kind, name, firstMapString(input, "action"), actionInput)
	default:
		err = apperrors.ErrUnsupportedOperation
	}
	return result, map[string]any{"clusterId": clusterID, "namespace": namespace, "resourceKind": kind, "resourceName": name}, err
}

func conditionalMutationTool(name string) bool {
	return name == "k8s.resources.conditional.update" || name == "k8s.resources.conditional.delete" || name == "k8s.workloads.conditional.action" || name == conditionalWorkloadScaleTool
}
