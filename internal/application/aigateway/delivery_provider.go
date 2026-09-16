package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeliveryCapabilityService interface {
	AssessDeliveryBatch(context.Context, domainidentity.Principal, sohaapi.DeliveryBatchAssessmentInput) (sohaapi.DeliveryBatchAssessment, error)
	ResolveDeliveryScopes(context.Context, domainidentity.Principal, domainworkflow.DeliveryWorkflowDefinition) ([]map[string]string, error)
	SaveDeliveryWorkflow(context.Context, domainidentity.Principal, string, domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflow, error)
	PrepareDeliveryWorkflow(context.Context, domainidentity.Principal, string, domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflowInput, error)
	FindDeliveryWorkflowCreation(context.Context, domainidentity.Principal, domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflow, error)
	GetDeliveryWorkflow(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryWorkflow, error)
	PrepareDeliveryBatch(context.Context, domainidentity.Principal, domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryWorkflowDefinition, error)
	CreateDeliveryBatch(context.Context, domainidentity.Principal, domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error)
	FindDeliveryBatch(context.Context, domainidentity.Principal, domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error)
	GetDeliveryBatch(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryBatch, error)
	CancelDeliveryBatch(context.Context, domainidentity.Principal, string, string) (domainworkflow.DeliveryBatch, error)
}

type deliveryCapabilityProvider struct {
	service DeliveryCapabilityService
	tools   []domainaigateway.ToolCapability
}

func NewDeliveryCapabilityProvider(service DeliveryCapabilityService) (CapabilityProvider, error) {
	workflowInput, workflowProperties, err := capabilityOpenAPIObject("DeliveryWorkflowInput")
	if err != nil {
		return nil, err
	}
	workflowInput["required"] = []string{"definition", "idempotencyKey"}

	delete(workflowProperties, "expectedVersion")
	batchInput, err := capabilityOpenAPISchema("DeliveryBatchInput")
	if err != nil {
		return nil, err
	}
	assessmentInput, err := capabilityOpenAPISchema("DeliveryBatchAssessmentInput")
	if err != nil {
		return nil, err
	}
	idSchema := func(field string) map[string]any {
		return gatewayObjectSchema([]string{field}, map[string]any{field: map[string]any{"type": "string", "minLength": 1}})
	}
	cancelInput := gatewayObjectSchema([]string{"batchId"}, map[string]any{"batchId": map[string]any{"type": "string", "minLength": 1}, "reason": map[string]any{"type": "string", "maxLength": 2000}})
	tools := []domainaigateway.ToolCapability{
		{Name: "delivery.workflows.create", Title: "Create Delivery Workflow", Action: "create", Description: "Save an authorized delivery definition with an immutable creation receipt; does not start deployment.", InputSchema: workflowInput, Effects: []string{"delivery.workflow.created"}},
		{Name: "delivery.workflows.get", Title: "Get Delivery Workflow", Action: "get", Description: "Read the current authorized workflow and version.", InputSchema: idSchema("workflowId")},
		{Name: "delivery.batches.create", Title: "Start Delivery Batch", Action: "create", Description: "Freeze and enqueue the existing delivery batch. Build, plan approvals, deploy and health checks remain domain-owned.", InputSchema: batchInput, Effects: []string{"delivery.batch.queued"}},
		{Name: "delivery.batches.get", Title: "Get Delivery Batch", Action: "get", Description: "Read a complete authorized delivery batch and its existing run. Completion alone is not a goal assessment.", InputSchema: idSchema("batchId")},
		{Name: "delivery.batches.cancel", Title: "Cancel Delivery Batch", Action: "cancel", Description: "Request cancellation of the existing batch; poll until domain execution confirms a terminal outcome. Applied changes remain.", InputSchema: cancelInput, Effects: []string{"delivery.batch.cancellation_requested"}},
		{Name: "delivery.batches.assess", Title: "Assess Deployed Target", Action: "assess", Description: "Verify a frozen batch target against fresh deployment evidence and optional explicit HTTP conditions. Access results identify the probe location; missing signals remain inconclusive.", InputSchema: assessmentInput},
	}
	for i := range tools {
		tool := &tools[i]
		tool.Domain, tool.Version, tool.MCPAdapterID, tool.MCPToolName = "delivery", "1", "delivery.v1", tool.Name
		tool.RequiredScopes = []string{"application", "service", "applicationEnvironment"}
		tool.PermissionKeys = []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryWorkflowsView}
		tool.RiskLevel = domainaigateway.RiskLevelRead
		tool.Execution = &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}
		if tool.Action == "create" || tool.Action == "cancel" {
			tool.RiskLevel, tool.RequiresApproval = domainaigateway.RiskLevelExecute, true
			tool.PermissionKeys = append(tool.PermissionKeys, appaccess.PermDeliveryWorkflowsTrigger)
		}
		if tool.Action == "create" {
			tool.Execution.IdempotencyKeyField = "idempotencyKey"
			tool.Execution.RecoveryMode = "original_call"
		}
		if i < 2 {
			tool.OutputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/id", Kind: "delivery.workflow"}, {Path: "/version", Kind: "delivery.workflow.version", ScopePaths: map[string]string{"workflowId": "/id"}}}
		} else if tool.Action != "assess" {
			tool.Execution.TaskKind, tool.Execution.StatusTool, tool.Execution.CancelTool = "delivery.batch", "delivery.batches.get", "delivery.batches.cancel"
			tool.OutputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/id", Kind: "delivery.batch"}}
			if tool.Action != "get" {
				tool.Execution.Mode = "async"
			}
		} else {
			tool.ProducesAssessment = true
			tool.OutputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/batchId", Kind: "delivery.batch"}, {Path: "/serviceId", Kind: "delivery.service", ScopePaths: map[string]string{"applicationId": "/applicationId"}}, {Path: "/deployedAt", Kind: "time.instant"}}
		}
	}
	for _, index := range []int{0, 2} {
		tools[index].InputSemantics = append(tools[index].InputSemantics,
			domainaigateway.CapabilityValueSemantic{Path: "/definition/targets/*/applicationId", Kind: "delivery.application"},
			domainaigateway.CapabilityValueSemantic{Path: "/definition/targets/*/serviceId", Kind: "delivery.service", ScopePaths: map[string]string{"applicationId": "/definition/targets/*/applicationId"}},
			domainaigateway.CapabilityValueSemantic{Path: "/definition/targets/*/applicationEnvironmentId", Kind: "delivery.application_environment", ScopePaths: map[string]string{"applicationId": "/definition/targets/*/applicationId"}},
			domainaigateway.CapabilityValueSemantic{Path: "/definition/targets/*/releaseTargetId", Kind: "delivery.release_target", ScopePaths: map[string]string{"applicationId": "/definition/targets/*/applicationId", "serviceId": "/definition/targets/*/serviceId", "applicationEnvironmentId": "/definition/targets/*/applicationEnvironmentId"}},
		)
	}
	tools[1].InputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/workflowId", Kind: "delivery.workflow"}}
	tools[2].Execution.Checks = []domainaigateway.CapabilityCheckReference{{Purpose: "verification", ToolName: "delivery.batches.assess", CapabilityVersion: "1"}}
	tools[2].InputSemantics = append(tools[2].InputSemantics, []domainaigateway.CapabilityValueSemantic{{Path: "/workflowId", Kind: "delivery.workflow"}, {Path: "/workflowVersion", Kind: "delivery.workflow.version", ScopePaths: map[string]string{"workflowId": "/workflowId"}}}...)
	for i := 3; i < len(tools); i++ {
		tools[i].InputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/batchId", Kind: "delivery.batch"}}
	}
	for _, tool := range tools {
		if _, err := compileCapabilityInputSchema(tool.InputSchema); err != nil {
			return nil, fmt.Errorf("%s: %w", tool.Name, err)
		}
	}
	return &deliveryCapabilityProvider{service: service, tools: tools}, nil
}
func (p *deliveryCapabilityProvider) Tools() []domainaigateway.ToolCapability       { return p.tools }
func (*deliveryCapabilityProvider) Resources() []domainaigateway.ResourceCapability { return nil }
func (*deliveryCapabilityProvider) Prompts() []domainaigateway.PromptCapability     { return nil }
func (*deliveryCapabilityProvider) Skills() []domainaigateway.SkillCapability       { return nil }

func (p *deliveryCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	ctx = withDeliveryScopeCheck(ctx, tool, input)
	var output any
	var err error
	switch tool.Name {
	case "delivery.workflows.create":
		var request domainworkflow.DeliveryWorkflowInput
		if err = mapInput(input, &request); err == nil {
			output, err = p.service.SaveDeliveryWorkflow(ctx, principal, "", request)
		}
	case "delivery.workflows.get":
		output, err = p.service.GetDeliveryWorkflow(ctx, principal, firstMapString(input, "workflowId"))
	case "delivery.batches.create":
		var request domainworkflow.DeliveryBatchInput
		if err = mapInput(input, &request); err == nil {
			output, err = p.service.CreateDeliveryBatch(ctx, principal, request)
		}
	case "delivery.batches.get":
		output, err = p.service.GetDeliveryBatch(ctx, principal, firstMapString(input, "batchId"))
	case "delivery.batches.cancel":
		output, err = p.service.CancelDeliveryBatch(ctx, principal, firstMapString(input, "batchId"), firstMapString(input, "reason"))
	case "delivery.batches.assess":
		var request sohaapi.DeliveryBatchAssessmentInput
		if err = mapInput(input, &request); err == nil {
			output, err = p.service.AssessDeliveryBatch(ctx, principal, request)
		}
	default:
		err = apperrors.ErrUnsupportedOperation
	}
	if batch, ok := output.(domainworkflow.DeliveryBatch); ok && batch.PartialView {
		return nil, nil, apperrors.ErrAccessDenied
	}
	if err == nil {
		err = verifyDeliveryOutputScopes(ctx, tool, input, output)
	}
	return output, nil, err
}

func (p *deliveryCapabilityProvider) RecoverTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, bool, error) {
	ctx = withDeliveryScopeCheck(ctx, tool, input)
	var output any
	var err error
	switch tool.Name {
	case "delivery.workflows.create":
		var request domainworkflow.DeliveryWorkflowInput
		if err = mapInput(input, &request); err == nil {
			output, err = p.service.FindDeliveryWorkflowCreation(ctx, principal, request)
		}
	case "delivery.batches.create":
		var request domainworkflow.DeliveryBatchInput
		if err = mapInput(input, &request); err == nil {
			output, err = p.service.FindDeliveryBatch(ctx, principal, request)
		}
	default:
		return nil, false, nil
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	if err == nil {
		err = verifyDeliveryOutputScopes(ctx, tool, input, output)
	}
	return output, err == nil, err
}

func (*deliveryCapabilityProvider) TaskReference(tool domainaigateway.ToolCapability, output, visible any) *domainaigateway.CapabilityTaskRef {
	batch, ok := output.(domainworkflow.DeliveryBatch)
	if !ok {
		raw, err := json.Marshal(output)
		if err != nil || json.Unmarshal(raw, &batch) != nil {
			return nil
		}
	}
	if batch.PartialView || batch.ID == "" || tool.Execution == nil || tool.Execution.TaskKind != "delivery.batch" {
		return nil
	}
	terminal, outcome := false, "unknown"
	switch batch.Status {
	case "completed":
		terminal, outcome = true, "succeeded"
	case "failed", "partially_completed":
		terminal, outcome = true, "failed"
	case "canceled":
		terminal, outcome = true, "canceled"
	}
	task := &domainaigateway.CapabilityTaskRef{Kind: "delivery.batch", ID: batch.ID, Status: batch.Status, Terminal: terminal, Outcome: outcome,
		StatusCall: domainaigateway.CapabilityCall{ToolName: "delivery.batches.get", CapabilityVersion: "1", Input: map[string]any{"batchId": batch.ID}},
		CancelCall: &domainaigateway.CapabilityCall{ToolName: "delivery.batches.cancel", CapabilityVersion: "1", Input: map[string]any{"batchId": batch.ID}},
	}
	return visibleCapabilityTaskRef(task, visible)
}
