package aigateway

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appvm "github.com/opensoha/soha/internal/application/virtualization"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainop "github.com/opensoha/soha/internal/domain/operation"
	domainvm "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type VirtualizationCapabilityService interface {
	ToolScopeProvider
	ListWorkerPools(context.Context, domainidentity.Principal, string) ([]sohaapi.VirtualizationWorkerPool, error)
	GetWorkerPool(context.Context, domainidentity.Principal, string) (sohaapi.VirtualizationWorkerPool, error)
	CreateWorker(context.Context, domainidentity.Principal, string, sohaapi.VirtualizationWorkerCreateInput) (domainvm.Task, error)
	FindWorkerCreation(context.Context, domainidentity.Principal, string, sohaapi.VirtualizationWorkerCreateInput) (domainvm.Task, error)
	AssessWorkerReadiness(context.Context, domainidentity.Principal, string) (sohaapi.CapabilityAssessment, error)
	CheckCapacity(context.Context, domainidentity.Principal, sohaapi.VirtualizationCapacityInput) (sohaapi.VirtualizationCapacityResult, error)
	PlanVMCreate(context.Context, domainidentity.Principal, appvm.CreateVMInput) (domainop.Plan, error)
	CreateVM(context.Context, domainidentity.Principal, appvm.CreateVMInput) (domainvm.Task, error)
	FindVMCreation(context.Context, domainidentity.Principal, appvm.CreateVMInput) (domainvm.Task, error)
	GetOperation(context.Context, domainidentity.Principal, string) (domainvm.Task, error)
	FindOperationMutation(context.Context, domainidentity.Principal, string, string, appvm.OperationMutationInput) (domainvm.Task, error)
	CancelOperationIdempotent(context.Context, domainidentity.Principal, string, appvm.OperationMutationInput) (domainvm.Task, error)
	RetryOperationIdempotent(context.Context, domainidentity.Principal, string, appvm.OperationMutationInput) (domainvm.Task, error)
}

type virtualizationCapabilityProvider struct {
	service VirtualizationCapabilityService
	tools   []domainai.ToolCapability
}

func NewVirtualizationCapabilityProvider(service VirtualizationCapabilityService) (CapabilityProvider, error) {
	capacity, err := capabilityOpenAPISchema("VirtualizationCapacityInput")
	if err != nil {
		return nil, err
	}
	create, properties, err := capabilityOpenAPIObject("VirtualMachineCreateInput")
	if err != nil {
		return nil, err
	}
	plan, err := capabilityOpenAPISchema("VirtualMachineCreateInput")
	if err != nil {
		return nil, err
	}

	properties["idempotencyKey"] = map[string]any{"type": "string", "minLength": 8, "maxLength": 128}
	create["required"] = []string{"connectionId", "name", "idempotencyKey"}
	operation := gatewayObjectSchema([]string{"operationId"}, map[string]any{"operationId": map[string]any{"type": "string", "minLength": 1}})
	mutation := gatewayObjectSchema([]string{"operationId", "idempotencyKey"}, map[string]any{"operationId": map[string]any{"type": "string", "minLength": 1}, "idempotencyKey": map[string]any{"type": "string", "minLength": 8, "maxLength": 128}, "reason": map[string]any{"type": "string", "maxLength": 1024}})
	tools := []domainai.ToolCapability{
		{Name: "virtualization.capacity.check", Title: "Check VM Capacity", Description: "Read fresh provider commitments and concurrent Soha reservations for a running VM with one root disk. Returns an expiring placement suggestion, not a reservation or health verdict. Creation must use requireCapacity for atomic admission.", Action: "check", RiskLevel: domainai.RiskLevelRead, PermissionKeys: []string{appaccess.PermVirtualizationClustersView, appaccess.PermVirtualizationVMsView, appaccess.PermVirtualizationStorageView}, InputSchema: capacity},
		{Name: "virtualization.vms.create.plan", Title: "Plan Virtual Machine Create", Description: "Validate and summarize VM creation without writes. Planning does not reserve or prove available capacity.", Action: "plan", RiskLevel: domainai.RiskLevelAnalyze, PermissionKeys: []string{appaccess.PermVirtualizationVMsView, appaccess.PermVirtualizationFlavorsView, appaccess.PermVirtualizationImagesView}, InputSchema: plan},
		{Name: "virtualization.vms.create.trigger", Title: "Create Virtual Machine", Description: "Enqueue a durable VM creation with a stable actor and request key. requireCapacity atomically reserves supported provider capacity. Recover the original task after a lost reply; verify workload readiness separately.", Action: "execute", RiskLevel: domainai.RiskLevelExecute, RequiresApproval: true, PermissionKeys: []string{appaccess.PermVirtualizationVMsCreate}, InputSchema: create, Effects: []string{"virtualization.vm.created"}},
		{Name: "virtualization.operations.get", Title: "Get VM Operation", Description: "Read a VM operation receipt without provider writes. Bootstrap content and raw provider output are omitted. Unknown provider effects do not prove failure or cancellation.", Action: "get", RiskLevel: domainai.RiskLevelRead, PermissionKeys: []string{appaccess.PermVirtualizationOperationsView}, InputSchema: operation},
		{Name: "virtualization.operations.cancel", Title: "Cancel VM Operation", Description: "Request cancellation of the original VM operation. Existing provider resources are retained; canceling remains unresolved until the provider effect is known.", Action: "cancel", RiskLevel: domainai.RiskLevelExecute, RequiresApproval: true, PermissionKeys: []string{appaccess.PermVirtualizationOperationsView, appaccess.ManagedActionPermission(appaccess.PermVirtualizationOperationsManage, "cancel")}, InputSchema: mutation},
		{Name: "virtualization.operations.retry", Title: "Retry VM Operation", Description: "Retry the same operation identity. Undispatched capacity-backed creation requires fresh atomic admission; dispatched creation keeps its reservation and provider identity.", Action: "retry", RiskLevel: domainai.RiskLevelExecute, RequiresApproval: true, PermissionKeys: []string{appaccess.PermVirtualizationOperationsView, appaccess.ManagedActionPermission(appaccess.PermVirtualizationOperationsManage, "retry")}, InputSchema: mutation},
	}
	for i := range tools {
		tool := &tools[i]
		tool.Version, tool.Domain, tool.MCPAdapterID, tool.MCPToolName = "1", "virtualization", "virtualization.v1", tool.Name
		tool.PermissionKeys = append([]string{appaccess.PermAIGatewayInvoke}, tool.PermissionKeys...)
		tool.RequiredScopes = []string{"virtualizationConnection", "namespace", "vm"}
		tool.Execution = &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true}
		if i >= 2 {
			tool.Execution.TaskKind, tool.Execution.StatusTool, tool.Execution.CancelTool = "virtualization.operation", "virtualization.operations.get", "virtualization.operations.cancel"
			tool.OutputSemantics = []domainai.CapabilityValueSemantic{{Path: "/id", Kind: "virtualization.operation"}, {Path: "/vmId", Kind: "virtualization.vm"}, {Path: "/connectionId", Kind: "virtualization.connection"}}
		}
		if i == 2 || i == 4 || i == 5 {
			tool.Execution.Mode, tool.Execution.IdempotencyKeyField = "async", "idempotencyKey"
			tool.Execution.RecoveryMode = "original_call"
		}
		if i <= 2 {
			tool.InputSemantics = []domainai.CapabilityValueSemantic{{Path: "/connectionId", Kind: "virtualization.connection"}, {Path: "/node", Kind: "virtualization.node"}, {Path: "/namespace", Kind: "k8s.namespace"}}
			if i != 0 {
				tool.InputSemantics = append(tool.InputSemantics, domainai.CapabilityValueSemantic{Path: "/cloudInit", Kind: "secret"})
			}
		} else {
			tool.InputSemantics = []domainai.CapabilityValueSemantic{{Path: "/operationId", Kind: "virtualization.operation"}}
		}
		if _, err := compileCapabilityInputSchema(tool.InputSchema); err != nil {
			return nil, err
		}
	}
	tools[0].OutputSemantics = []domainai.CapabilityValueSemantic{{Path: "/connectionId", Kind: "virtualization.connection"}, {Path: "/node", Kind: "virtualization.node"}, {Path: "/namespace", Kind: "k8s.namespace"}, {Path: "/storage", Kind: "virtualization.storage"}}
	tools[2].Execution.Checks = []domainai.CapabilityCheckReference{
		{Purpose: "availability", ToolName: "virtualization.capacity.check", CapabilityVersion: "1"},
		{Purpose: "precondition", ToolName: "virtualization.vms.create.plan", CapabilityVersion: "1"},
	}
	for i := 0; i <= 2; i++ {
		if i == 0 {
			tools[i].InputSemantics = append(tools[i].InputSemantics, domainai.CapabilityValueSemantic{Path: "/storage", Kind: "virtualization.storage"})
		} else {
			tools[i].InputSemantics = append(tools[i].InputSemantics, domainai.CapabilityValueSemantic{Path: "/providerParams/storage", Kind: "virtualization.storage"}, domainai.CapabilityValueSemantic{Path: "/providerParams/storageClass", Kind: "virtualization.storage"})
		}
		for _, semantics := range [][]domainai.CapabilityValueSemantic{tools[i].InputSemantics, tools[i].OutputSemantics} {
			for j := range semantics {
				if semantics[j].Kind == "virtualization.node" || semantics[j].Kind == "virtualization.storage" || semantics[j].Kind == "k8s.namespace" {
					semantics[j].ScopePaths = map[string]string{"virtualizationConnectionId": "/connectionId"}
				}
			}
		}
	}

	workers, err := workerCapabilities()
	if err != nil {
		return nil, err
	}
	return &virtualizationCapabilityProvider{service: service, tools: append(tools, workers...)}, nil
}

func workerCapabilities() ([]domainai.ToolCapability, error) {
	create, properties, err := capabilityOpenAPIObject("VirtualizationWorkerCreateInput")
	if err != nil {
		return nil, err
	}

	properties["workerPoolId"] = map[string]any{"type": "string", "format": "uuid"}
	create["required"] = []string{"workerPoolId", "poolRevision", "idempotencyKey"}
	pool := gatewayObjectSchema([]string{"workerPoolId"}, map[string]any{"workerPoolId": map[string]any{"type": "string", "format": "uuid"}})
	tools := []domainai.ToolCapability{
		{Name: "virtualization.worker_pools.list", Title: "List Worker Pools", Description: "Discover explicitly registered worker supply owners and current bounded pool revisions for a virtualization connection. Configuration availability does not prove current capacity.", Action: "list", RiskLevel: domainai.RiskLevelRead, InputSchema: gatewayObjectSchema([]string{"connectionId"}, map[string]any{"connectionId": gatewayStringSchema("Registered virtualization connection.")})},
		{Name: "virtualization.worker_pools.get", Title: "Get Worker Pool", Description: "Read the operator-registered pool owner, supported image, exact Kubernetes version and maximum nodes before requesting supply.", Action: "get", RiskLevel: domainai.RiskLevelRead, InputSchema: pool},
		{Name: "virtualization.workers.create", Title: "Create Kubernetes Worker", Description: "Reserve pool budget and provider capacity, clone one original VM and join it through short-lived protected kubeadm bootstrap. The existing VM task waits for owned Node readiness and required daemons. Partial VMs remain owned; retries reuse the original operation.", Action: "create", RiskLevel: domainai.RiskLevelExecute, RequiresApproval: true, InputSchema: create, Effects: []string{"virtualization.vm.created", "k8s.worker.joined"}},
		{Name: "virtualization.workers.assess", Title: "Assess Kubernetes Worker", Description: "Read fresh original VM and Node identity, schedulability, heartbeat and required network/storage daemon evidence. Missing or stale evidence is inconclusive. Delivery still performs its own scheduling preflight.", Action: "assess", RiskLevel: domainai.RiskLevelRead, ProducesAssessment: true, InputSchema: gatewayObjectSchema([]string{"operationId"}, map[string]any{"operationId": gatewayStringSchema("Original worker operation.")})},
	}
	for i := range tools {
		tool := &tools[i]
		tool.Version, tool.Domain, tool.MCPAdapterID, tool.MCPToolName = "1", "virtualization", "virtualization.v1", tool.Name
		tool.PermissionKeys = []string{appaccess.PermAIGatewayInvoke, appaccess.PermVirtualizationClustersView, appaccess.PermPlatformClustersView, appaccess.PlatformActionPermission("", "Node", "view")}
		tool.RequiredScopes = []string{"virtualizationConnection", "cluster", "workerPool"}
		tool.Execution = &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true}
		tool.InputSemantics = []domainai.CapabilityValueSemantic{{Path: "/workerPoolId", Kind: "virtualization.worker_pool"}}
		if i == 0 {
			tool.InputSemantics = []domainai.CapabilityValueSemantic{{Path: "/connectionId", Kind: "virtualization.connection"}}
		}
		if i == 2 {
			tool.PermissionKeys = append(tool.PermissionKeys, appaccess.PermVirtualizationVMsCreate, appaccess.PermVirtualizationImagesView, appaccess.PlatformActionPermission("", "Node", "create"))
			tool.Execution = &domainai.ToolExecutionContract{Mode: "async", Idempotent: true, IdempotencyKeyField: "idempotencyKey", TaskKind: "virtualization.operation", StatusTool: "virtualization.operations.get", CancelTool: "virtualization.operations.cancel"}
			tool.Execution.RecoveryMode = "original_call"
			tool.Execution.Checks = []domainai.CapabilityCheckReference{{Purpose: "verification", ToolName: "virtualization.workers.assess", CapabilityVersion: "1"}}
			tool.OutputSemantics = []domainai.CapabilityValueSemantic{{Path: "/id", Kind: "virtualization.operation"}, {Path: "/vmId", Kind: "virtualization.vm"}}
		}
		if i == 3 {
			tool.PermissionKeys = append(tool.PermissionKeys, appaccess.PermVirtualizationOperationsView)
			tool.InputSemantics = []domainai.CapabilityValueSemantic{{Path: "/operationId", Kind: "virtualization.operation"}}
		}
		if _, err := compileCapabilityInputSchema(tool.InputSchema); err != nil {
			return nil, err
		}
	}
	return tools, nil
}

func (p *virtualizationCapabilityProvider) Tools() []domainai.ToolCapability       { return p.tools }
func (*virtualizationCapabilityProvider) Resources() []domainai.ResourceCapability { return nil }
func (*virtualizationCapabilityProvider) Prompts() []domainai.PromptCapability     { return nil }
func (*virtualizationCapabilityProvider) Skills() []domainai.SkillCapability       { return nil }
func (p *virtualizationCapabilityProvider) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) ([]map[string]string, error) {
	return p.service.ToolInvocationScopes(ctx, principal, tool, input)
}

func withVirtualizationScopeCheck(ctx context.Context, tool string) context.Context {
	resolved, _ := ctx.Value(capabilityScopeContextKey{}).(capabilityScopeContext)
	if resolved.tool != tool {
		return ctx
	}
	return domainvm.WithScopeCheck(ctx, func(actual map[string]string) error {
		for _, scope := range resolved.scopes {
			if actual["workerPoolId"] != "" && actual["workerPoolId"] != scope["workerPoolId"] {
				continue
			}
			matches := true
			for _, key := range []string{"virtualizationConnectionId", "namespace", "clusterId", "connectionRevision", "operationId", "vmId", "workerPoolId", "workerPoolRevision"} {
				if expected, exists := scope[key]; exists && actual[key] != expected {
					matches = false
				}
			}
			if matches && actual["virtualizationConnectionId"] != "" && actual["virtualizationConnectionId"] == scope["virtualizationConnectionId"] {
				return nil
			}
		}
		return apperrors.ErrConflict
	})
}

func (p *virtualizationCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, map[string]any, error) {
	ctx = withVirtualizationScopeCheck(ctx, tool.Name)
	switch tool.Name {
	case "virtualization.worker_pools.list":
		result, err := p.service.ListWorkerPools(ctx, principal, firstMapString(input, "connectionId"))
		return result, nil, err
	case "virtualization.worker_pools.get":
		result, err := p.service.GetWorkerPool(ctx, principal, firstMapString(input, "workerPoolId"))
		return result, nil, err
	case "virtualization.workers.create":
		var request sohaapi.VirtualizationWorkerCreateInput
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		task, err := p.service.CreateWorker(ctx, principal, firstMapString(input, "workerPoolId"), request)
		return virtualizationTaskOutput(task), nil, err
	case "virtualization.workers.assess":
		result, err := p.service.AssessWorkerReadiness(ctx, principal, firstMapString(input, "operationId"))
		return result, nil, err
	case "virtualization.capacity.check":
		var request sohaapi.VirtualizationCapacityInput
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		result, err := p.service.CheckCapacity(ctx, principal, request)
		return result, nil, err
	case "virtualization.vms.create.plan", "virtualization.vms.create.trigger":
		var request appvm.CreateVMInput
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		request.IdempotencyKey = firstMapString(input, "idempotencyKey")
		if tool.Action == "plan" {
			result, err := p.service.PlanVMCreate(ctx, principal, request)
			return result, nil, err
		}
		task, err := p.service.CreateVM(ctx, principal, request)
		return virtualizationTaskOutput(task), nil, err
	case "virtualization.operations.get", "virtualization.operations.cancel", "virtualization.operations.retry":
		return p.invokeOperation(ctx, principal, tool, input)
	default:
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
}

func (p *virtualizationCapabilityProvider) invokeOperation(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, map[string]any, error) {
	id := firstMapString(input, "operationId")
	task, err := p.service.GetOperation(ctx, principal, id)
	if err != nil {
		return nil, nil, err
	}
	if task.TaskKind != "vm_create" && task.TaskKind != "vm_action" {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	mutation := appvm.OperationMutationInput{IdempotencyKey: firstMapString(input, "idempotencyKey"), Reason: firstMapString(input, "reason")}
	switch tool.Action {
	case "cancel":
		task, err = p.service.CancelOperationIdempotent(ctx, principal, id, mutation)
	case "retry":
		task, err = p.service.RetryOperationIdempotent(ctx, principal, id, mutation)
	}
	return virtualizationTaskOutput(task), nil, err
}

// Return only receipt facts. Raw task payload/result and failure text can contain
// cloud-init credentials or provider response content and never reach an agent.
func virtualizationTaskOutput(task domainvm.Task) map[string]any {
	state := domainvm.BuildOperationState(task, time.Now().UTC())
	outcome := "unknown"
	effect, _ := task.Result["providerEffect"].(string)
	confirmed, _ := task.Result["cancellationConfirmed"].(bool)
	confirmed = confirmed && task.TaskKind == "vm_create" // Legacy VM actions have no provider-stop acknowledgment.
	if task.Status == "completed" && (task.TaskKind != "vm_create" || effect == "created" && task.VMID != "") {
		outcome = "succeeded"
	}
	if task.Status == "canceled" && confirmed {
		outcome = "canceled"
	}
	if task.Status == "failed" && effect == "not_started" {
		outcome = "failed"
	}
	result := map[string]any{"id": task.ID, "status": task.Status, "connectionId": task.ConnectionID, "taskKind": task.TaskKind, "terminal": state.Terminal, "outcome": outcome, "attemptCount": task.AttemptCount, "createdAt": task.CreatedAt, "updatedAt": task.UpdatedAt}
	if poolID, ok := task.Payload["workerPoolId"].(string); ok && poolID != "" {
		result["workerPoolId"] = poolID
		for _, key := range []string{"workerReady", "workerNodeName", "workerNodeUid", "workerObservedAt", "workerValidUntil", "workerBootstrapRevoked"} {
			if value, exists := task.Result[key]; exists {
				result[key] = value
			}
		}
	}
	if task.VMID != "" {
		result["vmId"] = task.VMID
	}
	if effect == "created" || effect == "not_started" || effect == "unknown" {
		result["providerEffect"] = effect
	}
	if task.FinishedAt != nil {
		result["finishedAt"] = task.FinishedAt
	}
	result["cancellationConfirmed"] = confirmed
	return result
}

func (*virtualizationCapabilityProvider) TaskReference(tool domainai.ToolCapability, output, visible any) *domainai.CapabilityTaskRef {
	if tool.Execution == nil || tool.Execution.TaskKind != "virtualization.operation" {
		return nil
	}
	data, err := capabilityJSON(output)
	if err != nil {
		return nil
	}
	object, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	id, _ := object["id"].(string)
	status, _ := object["status"].(string)
	outcome, _ := object["outcome"].(string)
	terminal, _ := object["terminal"].(bool)
	if strings.TrimSpace(id) == "" || status == "" {
		return nil
	}
	visibleData, err := capabilityJSON(visible)
	if err != nil {
		return nil
	}
	visibleObject, ok := visibleData.(map[string]any)
	if !ok || visibleObject["outcome"] != outcome || visibleObject["terminal"] != terminal {
		return nil
	}
	ref := &domainai.CapabilityTaskRef{Kind: "virtualization.operation", ID: id, Status: status, Outcome: outcome, Terminal: terminal, StatusCall: domainai.CapabilityCall{ToolName: "virtualization.operations.get", CapabilityVersion: "1", Input: map[string]any{"operationId": id}}}
	if !terminal && status != "canceling" {
		ref.CancelCall = &domainai.CapabilityCall{ToolName: "virtualization.operations.cancel", CapabilityVersion: "1", Input: map[string]any{"operationId": id, "idempotencyKey": "cancel-operation:" + id}}
	}
	return visibleCapabilityTaskRef(ref, visible)
}

func (p *virtualizationCapabilityProvider) RecoverTool(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, bool, error) {
	ctx = withVirtualizationScopeCheck(ctx, tool.Name)
	var task domainvm.Task
	var err error
	switch tool.Name {
	case "virtualization.workers.create":
		var request sohaapi.VirtualizationWorkerCreateInput
		if err := mapInput(input, &request); err != nil {
			return nil, false, err
		}
		task, err = p.service.FindWorkerCreation(ctx, principal, firstMapString(input, "workerPoolId"), request)
	case "virtualization.vms.create.trigger":
		var request appvm.CreateVMInput
		if err := mapInput(input, &request); err != nil {
			return nil, false, err
		}
		request.IdempotencyKey = firstMapString(input, "idempotencyKey")
		task, err = p.service.FindVMCreation(ctx, principal, request)
	case "virtualization.operations.cancel", "virtualization.operations.retry":
		task, err = p.service.FindOperationMutation(ctx, principal, firstMapString(input, "operationId"), tool.Action, appvm.OperationMutationInput{IdempotencyKey: firstMapString(input, "idempotencyKey"), Reason: firstMapString(input, "reason")})
	default:
		return nil, false, nil
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	return virtualizationTaskOutput(task), err == nil, err
}
