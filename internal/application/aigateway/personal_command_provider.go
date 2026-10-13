package aigateway

import (
	"context"
	"errors"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func personalCommandCapabilities() []domain.ToolCapability {
	read := gatewayObjectSchema([]string{"commandId"}, map[string]any{"commandId": map[string]any{"type": "string", "format": "uuid"}})
	tools := []domain.ToolCapability{
		{Name: "personal.commands.preflight", Title: "Check personal command readiness", Description: "Inspect the authenticated owner's original environment. Ready requires a fresh Agent receipt; never wakes or provisions it.", Action: "preflight", RiskLevel: domain.RiskLevelRead, InputSchema: gatewayObjectSchema(nil, map[string]any{}), ProducesAssessment: true, Execution: &domain.ToolExecutionContract{Mode: "sync", Idempotent: true}},
		{Name: "personal.commands.execute", Title: "Run a personal command", Description: "Run bounded argv in the authenticated assistant's restricted Docker environment through an approved Native Workflow. No network, host paths or injected credentials. Persistent files use /data; the task's temporary working directory is disposable. Unknown results are never replayed.", Action: "execute", RiskLevel: domain.RiskLevelExecute, RequiresApproval: true,
			InputSchema: gatewayObjectSchema([]string{"argv", "idempotencyKey"}, map[string]any{"argv": map[string]any{"type": "array", "minItems": 1, "maxItems": 64, "items": map[string]any{"type": "string", "maxLength": 4096}}, "idempotencyKey": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}}),
			Execution:   &domain.ToolExecutionContract{Mode: "async", Idempotent: true, IdempotencyKeyField: "idempotencyKey", TaskKind: "personal.command", StatusTool: "personal.commands.get", CancelTool: "personal.commands.cancel", RecoveryMode: "original_call", Checks: []domain.CapabilityCheckReference{{Purpose: "precondition", ToolName: "personal.commands.preflight", CapabilityVersion: "1"}, {Purpose: "verification", ToolName: "personal.commands.assess", CapabilityVersion: "1"}}}, Effects: []string{"personal.command.executed"}},
		{Name: "personal.commands.get", Title: "Read original command receipt", Description: "Read the owner's durable command result under current permissions. No dispatch or repair.", Action: "get", RiskLevel: domain.RiskLevelRead, InputSchema: read, Execution: &domain.ToolExecutionContract{Mode: "sync", Idempotent: true, TaskKind: "personal.command", StatusTool: "personal.commands.get", CancelTool: "personal.commands.cancel"}},
		{Name: "personal.commands.cancel", Title: "Cancel original personal command", Description: "Fence the original command and stop its original container, retaining personal data. Unknown cancellation is unconfirmed; a newer task or lease is never stopped.", Action: "cancel", RiskLevel: domain.RiskLevelExecute, InputSchema: read, Execution: &domain.ToolExecutionContract{Mode: "sync", Idempotent: true, TaskKind: "personal.command", StatusTool: "personal.commands.get", CancelTool: "personal.commands.cancel"}},
		{Name: "personal.commands.assess", Title: "Assess original command completion", Description: "Verify a recent, persisted Agent success receipt and output digest. This proves command completion only; file contents and business goal postconditions need separate readbacks.", Action: "assess", RiskLevel: domain.RiskLevelRead, InputSchema: read, ProducesAssessment: true, Execution: &domain.ToolExecutionContract{Mode: "sync", Idempotent: true}},
	}
	for i := range tools {
		tools[i].Version, tools[i].Domain, tools[i].MCPAdapterID, tools[i].MCPToolName = "1", "personal_command", "personal.environment.v1", tools[i].Name
		tools[i].PermissionKeys = []string{appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse}
		tools[i].RequiredScopes = []string{"aiClient", "skill"}
		if i >= 2 {
			tools[i].InputSemantics = []domain.CapabilityValueSemantic{{Path: "/commandId", Kind: "personal.command"}}
		}
		if i >= 1 && i <= 3 {
			tools[i].OutputSemantics = []domain.CapabilityValueSemantic{{Path: "/id", Kind: "personal.command"}}
		}
	}
	return tools
}

func personalCommandArguments(ctx context.Context, principal domainidentity.Principal, input map[string]any) (domaincopilot.PersonalCommandInput, error) {
	parent, bound := ctx.Value(nativeCapabilityParentKey{}).(nativeCapabilityParentScope)
	execution, executing := capabilityExecutionFrom(ctx)
	if !bound || !executing || execution.Scope != domainworkflow.ScopeCapabilityTask || parent.parent.RunID == "" || parent.parent.OwnerID != principal.UserID {
		return domaincopilot.PersonalCommandInput{}, apperrors.ErrAccessDenied
	}
	var request struct {
		Argv []string `json:"argv"`
		Key  string   `json:"idempotencyKey"`
	}
	if len(input) != 2 || mapInput(input, &request) != nil || request.Key == "" || len(request.Argv) == 0 {
		return domaincopilot.PersonalCommandInput{}, apperrors.ErrInvalidArgument
	}
	return domaincopilot.PersonalCommandInput{TaskID: domaincopilot.PersonalTaskID(parent.parent.RunID), TaskDeadline: parent.parent.Deadline, Argv: request.Argv, IdempotencyKey: request.Key}, nil
}

func (p *personalEnvironmentCapabilityProvider) invokePersonalCommand(ctx context.Context, principal domainidentity.Principal, tool domain.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if tool.Action == "preflight" {
		if len(input) != 0 {
			return nil, nil, apperrors.ErrInvalidArgument
		}
		item, err := p.inspector.InspectPersonalEnvironment(ctx, principal)
		return personalCommandReadiness(item), nil, err
	}
	if tool.Action == "execute" {
		request, err := personalCommandArguments(ctx, principal, input)
		if err != nil {
			return nil, nil, err
		}
		item, err := p.inspector.ExecutePersonalCommand(ctx, principal, request)
		return item, map[string]any{"personalAssistantId": item.AssistantID, "commandId": item.ID}, err
	}
	id, valid := input["commandId"].(string)
	if !valid || id == "" || len(input) != 1 {
		return nil, nil, apperrors.ErrInvalidArgument
	}
	var item domaincopilot.PersonalCommand
	var err error
	switch tool.Action {
	case "get", "assess":
		item, err = p.inspector.GetPersonalCommand(ctx, principal, id)
	case "cancel":
		item, err = p.inspector.CancelPersonalCommand(ctx, principal, id)
	default:
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	if tool.Action == "assess" {
		return personalCommandAssessment(item), nil, err
	}
	return item, map[string]any{"personalAssistantId": item.AssistantID, "commandId": item.ID}, err
}

func personalCommandReadiness(item domaincopilot.PersonalEnvironment) domain.CapabilityAssessment {
	assessment := domain.CapabilityAssessment{Verdict: "inconclusive", Summary: "A fresh ready personal environment is required.", Evidence: []domain.CapabilityEvidence{}}
	if item.ObservedAt == nil {
		return assessment
	}
	fresh := item.ObservedAt.After(time.Now().Add(-time.Minute)) && !item.ObservedAt.After(time.Now().Add(time.Minute))
	ready := fresh && item.State == "ready" && item.ExpiresAt != nil && item.ExpiresAt.After(time.Now())
	if ready {
		assessment.Verdict, assessment.Summary = "satisfied", "The owner's original personal environment is ready."
	}
	assessment.Evidence = append(assessment.Evidence, domain.CapabilityEvidence{Kind: "runtime_inventory", Source: "personal.environment.agent", ObservedAt: *item.ObservedAt, DataThrough: item.ObservedAt, Incomplete: !ready, Summary: "Personal environment is " + item.State, Resource: &domain.CapabilityResourceRef{Kind: "personal.environment", ID: item.AssistantID}})
	return assessment
}

func personalCommandAssessment(item domaincopilot.PersonalCommand) domain.CapabilityAssessment {
	assessment := domain.CapabilityAssessment{Verdict: "inconclusive", Summary: "The original command has no recent confirmed success receipt.", Evidence: []domain.CapabilityEvidence{}}
	if item.ObservedAt == nil {
		return assessment
	}
	fresh := item.ObservedAt.After(time.Now().Add(-5*time.Minute)) && !item.ObservedAt.After(time.Now().Add(time.Minute))
	confirmed := fresh && item.State == "succeeded" && len(item.OutputDigest) == 71 && strings.HasPrefix(item.OutputDigest, "sha256:")
	if confirmed {
		assessment.Verdict, assessment.Summary = "satisfied", "The original command returned successfully; goal postconditions need their own readback."
	}
	assessment.Evidence = append(assessment.Evidence, domain.CapabilityEvidence{Kind: "command_receipt", Source: "personal.command.agent", ObservedAt: *item.ObservedAt, DataThrough: item.ObservedAt, Incomplete: !confirmed, Summary: "Original command is " + item.State, Resource: &domain.CapabilityResourceRef{Kind: "personal.command", ID: item.ID}})
	return assessment
}

func (p *personalEnvironmentCapabilityProvider) RecoverTool(ctx context.Context, principal domainidentity.Principal, tool domain.ToolCapability, input map[string]any) (any, bool, error) {
	if tool.Name != "personal.commands.execute" {
		return nil, false, nil
	}
	request, err := personalCommandArguments(ctx, principal, input)
	if err != nil {
		return nil, false, err
	}
	item, err := p.inspector.FindPersonalCommand(ctx, principal, request)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	return item, err == nil, err
}

func (*personalEnvironmentCapabilityProvider) TaskReference(tool domain.ToolCapability, output, visible any) *domain.CapabilityTaskRef {
	item, ok := output.(domaincopilot.PersonalCommand)
	if !ok || item.ID == "" || tool.Execution == nil || tool.Execution.TaskKind != "personal.command" {
		return nil
	}
	terminal, outcome := false, "unknown"
	switch item.State {
	case "succeeded":
		terminal, outcome = true, "succeeded"
	case "not_dispatched", "failed":
		terminal, outcome = true, "failed"
	case "canceled":
		terminal, outcome = true, "canceled"
	}
	ref := &domain.CapabilityTaskRef{Kind: "personal.command", ID: item.ID, Status: item.State, Terminal: terminal, Outcome: outcome, StatusCall: domain.CapabilityCall{ToolName: "personal.commands.get", CapabilityVersion: "1", Input: map[string]any{"commandId": item.ID}}, CancelCall: &domain.CapabilityCall{ToolName: "personal.commands.cancel", CapabilityVersion: "1", Input: map[string]any{"commandId": item.ID}}}
	return visibleCapabilityTaskRef(ref, visible)
}
