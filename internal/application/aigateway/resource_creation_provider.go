package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ResourceCreationCapabilityService interface {
	KubernetesResourceCreationService
	ResolveCreateTargets(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) ([]domainresource.ResourceCreateRef, error)
	FindCreate(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) (domainresource.ResourceCreateExecution, error)
	GetCreate(context.Context, domainidentity.Principal, string, string) (domainresource.ResourceCreateExecution, error)
	AssessCreate(context.Context, domainidentity.Principal, string, string) (sohaapi.CapabilityAssessment, error)
}

type resourceCreationCapabilityProvider struct {
	service ResourceCreationCapabilityService
	tools   []domainai.ToolCapability
}

func NewResourceCreationCapabilityProvider(service ResourceCreationCapabilityService) (CapabilityProvider, error) {
	createSchema, properties, err := capabilityOpenAPIObject("KubernetesResourceCreateRequest")
	if err != nil {
		return nil, err
	}

	properties["clusterId"] = map[string]any{"type": "string", "minLength": 1}
	properties["idempotencyKey"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
	createSchema["required"] = []string{"clusterId", "source", "content", "idempotencyKey"}
	preflightSchema := maps.Clone(createSchema)
	preflightSchema["required"] = []string{"clusterId", "source", "content"}
	receiptSchema, err := capabilityOpenAPISchema("KubernetesResourceCreationReceipt")
	if err != nil {
		return nil, err
	}
	readSchema := gatewayObjectSchema([]string{"clusterId", "operationId"}, map[string]any{"clusterId": map[string]any{"type": "string", "minLength": 1}, "operationId": map[string]any{"type": "string", "minLength": 1}})
	tools := []domainai.ToolCapability{
		{Name: "k8s.resources.create.preflight", Title: "Preflight Kubernetes Resources", Description: "Resolve and authorize every manifest target and run Kubernetes server-side dry-run. Does not create resources.", Action: "preflight", RiskLevel: domainai.RiskLevelAnalyze, InputSchema: preflightSchema, Execution: &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true}},
		{Name: "k8s.resources.create.trigger", Title: "Create Kubernetes Resources", Description: "Create resources through the original actor-owned durable batch. Lost replies recover that receipt; partial or unknown effects are not retried or rolled back. Managed workloads use their existing delivery owner.", Action: "execute", RiskLevel: domainai.RiskLevelHigh, RequiresApproval: true, InputSchema: createSchema, OutputSchema: receiptSchema,
			Execution: &domainai.ToolExecutionContract{Mode: "async", Idempotent: true, IdempotencyKeyField: "idempotencyKey", TaskKind: "k8s.resource_creation", StatusTool: "k8s.resources.create.get", RecoveryMode: "original_call", Checks: []domainai.CapabilityCheckReference{{Purpose: "precondition", ToolName: "k8s.resources.create.preflight", CapabilityVersion: "1"}, {Purpose: "verification", ToolName: "k8s.resources.create.assess", CapabilityVersion: "1"}}}, Effects: []string{"k8s.resources.created"}},
		{Name: "k8s.resources.create.get", Title: "Read Kubernetes Creation Receipt", Description: "Read the original creation batch under current target permissions. This never repeats resource creation or repairs unknown results.", Action: "get", RiskLevel: domainai.RiskLevelRead, InputSchema: readSchema, OutputSchema: receiptSchema, Execution: &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true, TaskKind: "k8s.resource_creation", StatusTool: "k8s.resources.create.get"}},
		{Name: "k8s.resources.create.assess", Title: "Verify Created Kubernetes Objects", Description: "Verify that every original UID is still present. Replaced, deleting, missing or unobserved objects cannot satisfy the goal. Workload readiness and URL reachability require separate assessments.", Action: "assess", RiskLevel: domainai.RiskLevelRead, InputSchema: readSchema, ProducesAssessment: true, Execution: &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true}},
	}
	for i := range tools {
		tools[i].Version, tools[i].Domain, tools[i].MCPAdapterID, tools[i].MCPToolName = "1", "k8s", "k8s.v1", tools[i].Name
		tools[i].PermissionKeys = []string{appaccess.PermAIGatewayInvoke, appaccess.PermPlatformResourceCreationUse}
		tools[i].RequiredScopes = []string{"cluster", "namespace"}
		tools[i].InputSemantics = []domainai.CapabilityValueSemantic{{Path: "/clusterId", Kind: "k8s.cluster"}}
		if i >= 2 {
			tools[i].InputSemantics = append(tools[i].InputSemantics, domainai.CapabilityValueSemantic{Path: "/operationId", Kind: "k8s.resource_creation", ScopePaths: map[string]string{"clusterId": "/clusterId"}})
		}
		if i == 1 || i == 2 {
			tools[i].OutputSemantics = []domainai.CapabilityValueSemantic{{Path: "/clusterId", Kind: "k8s.cluster"}, {Path: "/operationId", Kind: "k8s.resource_creation", ScopePaths: map[string]string{"clusterId": "/clusterId"}}}
		}
		if _, err := compileCapabilityInputSchema(tools[i].InputSchema); err != nil {
			return nil, err
		}
	}
	return &resourceCreationCapabilityProvider{service: service, tools: tools}, nil
}

func (p *resourceCreationCapabilityProvider) Tools() []domainai.ToolCapability       { return p.tools }
func (*resourceCreationCapabilityProvider) Resources() []domainai.ResourceCapability { return nil }
func (*resourceCreationCapabilityProvider) Prompts() []domainai.PromptCapability     { return nil }
func (*resourceCreationCapabilityProvider) Skills() []domainai.SkillCapability {
	for _, skill := range (BuiltinCapabilityProvider{}).Skills() {
		if skill.ID == "k8s-resource-provisioner" {
			skill.CapabilityRefs = append(slices.Clone(skill.CapabilityRefs), "k8s.resources.create.get", "k8s.resources.create.assess")
			return []domainai.SkillCapability{skill}
		}
	}
	return nil
}
func (*resourceCreationCapabilityProvider) ResourceCapabilityRefs() []ResourceCapabilityRefs {
	return []ResourceCapabilityRefs{{Resource: "soha://k8s/runtime", Tools: []string{"k8s.resources.create.get", "k8s.resources.create.assess"}}}
}

func resourceCreateCallInput(input map[string]any) (string, domainresource.ResourceCreateRequest, error) {
	var request domainresource.ResourceCreateRequest
	err := mapInput(input, &request)
	request.RequestID = firstMapString(input, "idempotencyKey")
	return firstMapString(input, "clusterId"), request, err
}

func (p *resourceCreationCapabilityProvider) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) ([]map[string]string, error) {
	clusterID, request, err := resourceCreateCallInput(input)
	if err != nil {
		return nil, err
	}
	var refs []domainresource.ResourceCreateRef
	if tool.Action == "get" || tool.Action == "assess" {
		item, err := p.service.GetCreate(ctx, principal, clusterID, firstMapString(input, "operationId"))
		if err != nil {
			return nil, err
		}
		for _, document := range item.Documents {
			refs = append(refs, document.Resource)
		}
	} else {
		refs, err = p.service.ResolveCreateTargets(ctx, principal, clusterID, request)
		if err != nil {
			return nil, err
		}
	}
	scopes := make([]map[string]string, len(refs))
	for i, ref := range refs {
		scopes[i] = map[string]string{"clusterId": clusterID, "resourceKind": ref.Kind}
		if ref.Namespace != "" {
			scopes[i]["namespace"] = ref.Namespace
		}
	}
	return scopes, nil
}

func (p *resourceCreationCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, map[string]any, error) {
	clusterID, request, err := resourceCreateCallInput(input)
	if err != nil {
		return nil, nil, err
	}
	var output any
	switch tool.Action {
	case "preflight":
		output, err = p.service.PreflightCreate(ctx, principal, clusterID, request)
	case "execute":
		// A durable store is required even before the first request can execute.
		output, err = p.service.FindCreate(ctx, principal, clusterID, request)
		if errors.Is(err, apperrors.ErrNotFound) {
			output, err = p.service.ExecuteCreate(ctx, principal, clusterID, request)
		}
	case "get":
		output, err = p.service.GetCreate(ctx, principal, clusterID, firstMapString(input, "operationId"))
	case "assess":
		output, err = p.service.AssessCreate(ctx, principal, clusterID, firstMapString(input, "operationId"))
	default:
		err = apperrors.ErrUnsupportedOperation
	}
	related := map[string]any{"clusterId": clusterID}
	if receipt, ok := output.(domainresource.ResourceCreateExecution); ok {
		related["operationId"], related["contentHash"] = receipt.OperationID, receipt.ContentHash
	}
	return output, related, err
}

func (p *resourceCreationCapabilityProvider) RecoverTool(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, bool, error) {
	if tool.Action != "execute" {
		return nil, false, nil
	}
	clusterID, request, err := resourceCreateCallInput(input)
	if err != nil {
		return nil, false, err
	}
	item, err := p.service.FindCreate(ctx, principal, clusterID, request)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	return item, err == nil, err
}

func (*resourceCreationCapabilityProvider) TaskReference(tool domainai.ToolCapability, output, visible any) *domainai.CapabilityTaskRef {
	if tool.Execution == nil || tool.Execution.TaskKind != "k8s.resource_creation" {
		return nil
	}
	var item domainresource.ResourceCreateExecution
	raw, err := json.Marshal(output)
	if err != nil || json.Unmarshal(raw, &item) != nil || item.OperationID == "" || item.ClusterID == "" {
		return nil
	}
	data, err := capabilityJSON(visible)
	object, ok := data.(map[string]any)
	if err != nil || !ok || object["operationId"] != item.OperationID || object["clusterId"] != item.ClusterID || object["status"] != item.Status {
		return nil
	}
	outcome, terminal := "unknown", false
	switch strings.TrimSpace(item.Status) {
	case "succeeded":
		outcome, terminal = "succeeded", true
	case "failed", "partial":
		outcome, terminal = "failed", true
	}
	return &domainai.CapabilityTaskRef{Kind: "k8s.resource_creation", ID: item.OperationID, Status: item.Status, Outcome: outcome, Terminal: terminal,
		StatusCall: domainai.CapabilityCall{ToolName: "k8s.resources.create.get", CapabilityVersion: "1", Input: map[string]any{"clusterId": item.ClusterID, "operationId": item.OperationID}}}
}
