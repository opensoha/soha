package aigateway

import (
	"context"
	"fmt"
	"reflect"

	contractresource "github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type PodExecutionCapabilityService interface {
	ExecutePodArgv(context.Context, domainidentity.Principal, contractresource.PodArgvToolInput) (contractresource.PodExecutionReceipt, error)
	GetPodExecution(context.Context, domainidentity.Principal, contractresource.PodExecutionLookupInput) (contractresource.PodExecutionReceipt, error)
	ResolvePodExecutionScope(context.Context, domainidentity.Principal, contractresource.PodExecutionLookupInput) (map[string]string, error)
	FindOriginalPodExecution(context.Context, domainidentity.Principal, contractresource.PodArgvToolInput) (contractresource.PodExecutionReceipt, error)
}

type podExecutionCapabilityProvider struct {
	service PodExecutionCapabilityService
	tools   []domainai.ToolCapability
}

func NewPodExecutionCapabilityProvider(service PodExecutionCapabilityService) (CapabilityProvider, error) {
	if service == nil || (reflect.ValueOf(service).Kind() == reflect.Pointer && reflect.ValueOf(service).IsNil()) {
		return nil, fmt.Errorf("%w: Pod execution service is required", apperrors.ErrInvalidArgument)
	}
	execute, err := capabilityOpenAPISchema("KubernetesPodArgvToolInput")
	if err != nil {
		return nil, err
	}
	lookup, err := capabilityOpenAPISchema("KubernetesPodExecutionLookupInput")
	if err != nil {
		return nil, err
	}
	output, err := capabilityOpenAPISchema("KubernetesPodGatewayResult")
	if err != nil {
		return nil, err
	}
	assessment, err := capabilityOpenAPISchema("CapabilityAssessment")
	if err != nil {
		return nil, err
	}
	tools := []domainai.ToolCapability{
		{Name: "k8s.pods.exec", Title: "Execute Reviewed Pod Arguments", Description: "Run the approved literal argv once against the original Pod UID and key. Requires a credential-bound actor and current exec permission. Lost or unknown outcomes retain the original receipt and never trigger an automatic second execution. Text output is redacted; binary output is withheld. No interactive TTY.", Action: "execute", RiskLevel: domainai.RiskLevelHigh, RequiresApproval: true, InputSchema: execute,
			Execution: &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true, IdempotencyKeyField: "idempotencyKey", TaskKind: "k8s.pod_execution", StatusTool: "k8s.pods.exec.receipt"}, Effects: []string{"k8s.pod.process_executed"}},
		{Name: "k8s.pods.exec.receipt", Title: "Read Original Pod Execution Receipt", Description: "Read the original actor-owned execution by key or ID under current exec permission and Gateway policy. Never submits or retries a process. Text output is redacted and binary output withheld; tool success is separate from process status and exit code.", Action: "get", RiskLevel: domainai.RiskLevelRead, InputSchema: lookup,
			Execution: &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true, TaskKind: "k8s.pod_execution", StatusTool: "k8s.pods.exec.receipt"}},
		{Name: "k8s.pods.exec.assess", Title: "Assess Original Pod Process Completion", Description: "Read-only verification of the original actor-owned process receipt. Satisfied requires a known zero exit code and a recorded finish time; running, lost, expired-output or unknown outcomes remain inconclusive. This proves process completion only, not workload readiness or an arbitrary business goal.", Action: "assess", RiskLevel: domainai.RiskLevelRead, InputSchema: lookup, OutputSchema: assessment, ProducesAssessment: true, Execution: &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true}},
	}
	tools[0].Description += " Durable tasks require explicitly enabled encrypted storage; Native execution uses an owned governed task and receipt-bound process verification."
	tools[0].Execution.RecoveryMode = "original_call"
	tools[0].Execution.Checks = []domainai.CapabilityCheckReference{{Purpose: "precondition", ToolName: "k8s.resources.identity.read", CapabilityVersion: "1"}, {Purpose: "verification", ToolName: "k8s.pods.exec.assess", CapabilityVersion: "1"}}
	for i := range tools {
		tools[i].Version, tools[i].Domain, tools[i].MCPAdapterID, tools[i].MCPToolName = "1", "k8s", "k8s.v1", tools[i].Name
		tools[i].PermissionKeys = []string{appaccess.PermAIGatewayInvoke, appaccess.PermPlatformPodsExec}
		tools[i].RequiredScopes = []string{"cluster", "namespace", "pod"}
		tools[i].InputSemantics = []domainai.CapabilityValueSemantic{{Path: "/podName", Kind: "k8s.pod", ScopePaths: map[string]string{"clusterId": "/clusterId", "namespace": "/namespace", "podName": "/podName"}}}
		tools[i].OutputSemantics = []domainai.CapabilityValueSemantic{{Path: "/receipt/id", Kind: "k8s.pod_execution", ScopePaths: map[string]string{"clusterId": "/receipt/clusterId", "namespace": "/receipt/namespace", "podName": "/receipt/podName"}}}
		if tools[i].OutputSchema == nil {
			tools[i].OutputSchema = output
		}
		if _, err := compileCapabilityInputSchema(tools[i].InputSchema); err != nil {
			return nil, err
		}
	}
	tools[2].InputSemantics = []domainai.CapabilityValueSemantic{{Path: "/executionId", Kind: "k8s.pod_execution", ScopePaths: map[string]string{"clusterId": "/clusterId", "namespace": "/namespace", "podName": "/podName"}}}
	tools[2].OutputSemantics = nil
	return &podExecutionCapabilityProvider{service: service, tools: tools}, nil
}

func (p *podExecutionCapabilityProvider) Tools() []domainai.ToolCapability       { return p.tools }
func (*podExecutionCapabilityProvider) Resources() []domainai.ResourceCapability { return nil }
func (*podExecutionCapabilityProvider) Prompts() []domainai.PromptCapability     { return nil }
func (*podExecutionCapabilityProvider) Skills() []domainai.SkillCapability {
	return []domainai.SkillCapability{{ID: "k8s-pod-executor", Name: "K8s Pod Executor", Description: "Approval-bound, credential-bound Pod argv with original execution receipt recovery.", Category: "platform", CapabilityRefs: []string{"k8s.resources.identity.read", "k8s.pods.exec", "k8s.pods.exec.receipt", "k8s.pods.exec.assess"}, RequiredScopes: []string{"cluster", "namespace", "pod"}}}
}

func (p *podExecutionCapabilityProvider) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) ([]map[string]string, error) {
	if err := validateCapabilityInput(tool, input); err != nil {
		return nil, err
	}
	var lookup contractresource.PodExecutionLookupInput
	if err := mapInput(input, &lookup); err != nil {
		return nil, err
	}
	if tool.Name == protectedPodExecTool {
		var request contractresource.PodArgvToolInput
		if err := mapInput(input, &request); err != nil {
			return nil, err
		}
		if _, err := request.Normalized(); err != nil {
			return nil, apperrors.ErrInvalidArgument
		}
	} else if tool.Name != "k8s.pods.exec.receipt" && tool.Name != "k8s.pods.exec.assess" {
		return nil, apperrors.ErrUnsupportedOperation
	}
	scope, err := p.service.ResolvePodExecutionScope(ctx, principal, lookup)
	if err != nil {
		return nil, err
	}
	return []map[string]string{scope}, nil
}

func (p *podExecutionCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if err := validateCapabilityInput(tool, input); err != nil {
		return nil, nil, err
	}
	var receipt contractresource.PodExecutionReceipt
	var err error
	switch tool.Name {
	case protectedPodExecTool:
		var request contractresource.PodArgvToolInput
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		receipt, err = p.service.ExecutePodArgv(ctx, principal, request)
	case "k8s.pods.exec.receipt", "k8s.pods.exec.assess":
		var request contractresource.PodExecutionLookupInput
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		receipt, err = p.service.GetPodExecution(ctx, principal, request)
	default:
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	if err != nil {
		return nil, nil, err
	}
	if tool.Name == "k8s.pods.exec.assess" {
		return podExecutionAssessment(receipt), map[string]any{"executionId": receipt.ID}, nil
	}
	result := podGatewayOutput(receipt)
	return result, map[string]any{"executionId": receipt.ID, "clusterId": receipt.ClusterID, "namespace": receipt.Namespace, "podName": receipt.PodName, "outputState": result.OutputState}, nil
}
