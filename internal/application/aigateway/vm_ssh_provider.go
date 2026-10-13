package aigateway

import (
	"context"
	"errors"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/aigateway"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type vmSSHCapabilityService interface {
	CreateVMSSHCommand(context.Context, identity.Principal, string, sohaapi.VirtualMachineSSHCommandInput) (sohaapi.VirtualMachineSSHCommand, error)
	FindVMSSHCommand(context.Context, identity.Principal, string, sohaapi.VirtualMachineSSHCommandInput) (sohaapi.VirtualMachineSSHCommand, error)
	GetVMSSHCommand(context.Context, identity.Principal, string) (sohaapi.VirtualMachineSSHCommand, error)
	CancelVMSSHCommand(context.Context, identity.Principal, string) (sohaapi.VirtualMachineSSHCommand, error)
	CheckVMSSHCommandReadiness(context.Context, identity.Principal, string, int) (domain.CapabilityAssessment, error)
}

func vmSSHCapabilities() ([]domain.ToolCapability, error) {
	request, properties, err := capabilityOpenAPIObject("VirtualMachineSSHCommandInput")
	if err != nil {
		return nil, err
	}
	properties["vmId"] = map[string]any{"type": "string", "minLength": 1}
	request["required"] = []string{"vmId", "bindingRevision", "command", "idempotencyKey"}
	read := gatewayObjectSchema([]string{"commandId"}, map[string]any{"commandId": gatewayStringSchema("Original SSH ExecutionTask identity.")})
	tools := []domain.ToolCapability{
		{Name: "virtualization.vm_ssh.preflight", Title: "Check VM SSH command readiness", Description: "Read current VM generation, trusted login binding, exact credential version and registered execution Agent. SSH readiness does not prove full personal-computer readiness.", Action: "preflight", RiskLevel: domain.RiskLevelRead, ProducesAssessment: true, InputSchema: gatewayObjectSchema([]string{"vmId", "bindingRevision"}, map[string]any{"vmId": gatewayStringSchema("Soha VM identity."), "bindingRevision": map[string]any{"type": "integer", "minimum": 1}})},
		{Name: "virtualization.vm_ssh.execute", Title: "Run approved VM SSH command", Description: "Run one exact approved command through the original VM binding and a short-lived vm_exec lease. Reuses ExecutionTask. Credentials stay in the registered Agent's memory; lost effects are unknown and never replayed.", Action: "execute", RiskLevel: domain.RiskLevelExecute, RequiresApproval: true, InputSchema: request, Effects: []string{"virtualization.vm.command.executed"}},
		{Name: "virtualization.vm_ssh.get", Title: "Read original VM SSH receipt", Description: "Read the current owner's bounded output and original execution result under current VM permissions.", Action: "get", RiskLevel: domain.RiskLevelRead, InputSchema: read},
		{Name: "virtualization.vm_ssh.cancel", Title: "Cancel original VM SSH command", Description: "Revoke the original SSH session and fence its existing ExecutionTask. Dispatched effects stay unknown until verified; cancellation does not roll back guest changes.", Action: "cancel", RiskLevel: domain.RiskLevelExecute, InputSchema: read},
		{Name: "virtualization.vm_ssh.assess", Title: "Assess original SSH command result", Description: "Require a recent persisted exit-code and output-digest receipt. Command success does not prove the user's business goal.", Action: "assess", RiskLevel: domain.RiskLevelRead, ProducesAssessment: true, InputSchema: read},
	}
	for i := range tools {
		tool := &tools[i]
		tool.Version, tool.Domain, tool.MCPAdapterID, tool.MCPToolName = "1", "virtualization", "virtualization.v1", tool.Name
		tool.PermissionKeys = []string{appaccess.PermAIGatewayInvoke, "virtualization.vms.connect", appaccess.PermSecretUse}
		tool.RequiredScopes = []string{"virtualizationConnection", "vm"}
		tool.Execution = &domain.ToolExecutionContract{Mode: "sync", Idempotent: true}
		tool.InputSemantics = []domain.CapabilityValueSemantic{{Path: "/vmId", Kind: "virtualization.vm"}}
		if i >= 2 {
			tool.InputSemantics = []domain.CapabilityValueSemantic{{Path: "/commandId", Kind: "execution.task"}}
		}
		if i >= 1 && i <= 3 {
			tool.Execution.TaskKind = "virtualization.ssh_command"
			tool.Execution.StatusTool = "virtualization.vm_ssh.get"
			tool.Execution.CancelTool = "virtualization.vm_ssh.cancel"
			tool.OutputSemantics = []domain.CapabilityValueSemantic{{Path: "/id", Kind: "execution.task"}, {Path: "/vmId", Kind: "virtualization.vm"}}
		}
		if i == 1 {
			tool.Execution.Mode = "async"
			tool.Execution.IdempotencyKeyField = "idempotencyKey"
			tool.Execution.RecoveryMode = "original_call"
			tool.Execution.Checks = []domain.CapabilityCheckReference{{Purpose: "precondition", ToolName: tools[0].Name, CapabilityVersion: "1"}, {Purpose: "verification", ToolName: tools[4].Name, CapabilityVersion: "1"}}
		}
		if _, err := compileCapabilityInputSchema(tool.InputSchema); err != nil {
			return nil, err
		}
	}
	return tools, nil
}

func (p *virtualizationCapabilityProvider) invokeVMSSH(ctx context.Context, principal identity.Principal, tool domain.ToolCapability, input map[string]any) (any, map[string]any, error) {
	service, ok := p.service.(vmSSHCapabilityService)
	if !ok {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	if tool.Action == "preflight" {
		var request struct {
			VMID     string `json:"vmId"`
			Revision int    `json:"bindingRevision"`
		}
		if mapInput(input, &request) != nil {
			return nil, nil, apperrors.ErrInvalidArgument
		}
		result, err := service.CheckVMSSHCommandReadiness(ctx, principal, request.VMID, request.Revision)
		return result, nil, err
	}
	if tool.Action == "execute" {
		var request sohaapi.VirtualMachineSSHCommandInput
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		result, err := service.CreateVMSSHCommand(ctx, principal, firstMapString(input, "vmId"), request)
		return result, nil, err
	}
	id := firstMapString(input, "commandId")
	var result sohaapi.VirtualMachineSSHCommand
	var err error
	if tool.Action == "cancel" {
		result, err = service.CancelVMSSHCommand(ctx, principal, id)
	} else {
		result, err = service.GetVMSSHCommand(ctx, principal, id)
	}
	if tool.Action != "assess" {
		return result, nil, err
	}
	assessment := domain.CapabilityAssessment{Verdict: "inconclusive", Summary: "Original SSH command has no recent confirmed success receipt.", Evidence: []domain.CapabilityEvidence{}}
	if result.FinishedAt != nil {
		confirmed := result.Outcome == "succeeded" && result.ExitCode != nil && *result.ExitCode == 0 && len(result.OutputDigest) == 71 && result.FinishedAt.After(time.Now().Add(-5*time.Minute)) && !result.FinishedAt.After(time.Now().Add(time.Minute))
		if confirmed {
			assessment.Verdict = "satisfied"
			assessment.Summary = "Original command exited successfully; business postconditions require a separate readback."
		}
		assessment.Evidence = append(assessment.Evidence, domain.CapabilityEvidence{Kind: "command_receipt", Source: "virtualization.ssh.agent", ObservedAt: *result.FinishedAt, DataThrough: result.FinishedAt, Incomplete: !confirmed, Summary: "Original SSH task is " + result.Status, Resource: &domain.CapabilityResourceRef{Kind: "execution.task", ID: result.ID}})
	}
	return assessment, nil, err
}

func vmSSHTaskReference(tool domain.ToolCapability, output, visible any) *domain.CapabilityTaskRef {
	item, ok := output.(sohaapi.VirtualMachineSSHCommand)
	if !ok || item.ID == "" || tool.Execution == nil || tool.Execution.TaskKind != "virtualization.ssh_command" {
		return nil
	}
	ref := &domain.CapabilityTaskRef{Kind: "virtualization.ssh_command", ID: item.ID, Status: item.Status, Outcome: string(item.Outcome), Terminal: item.Terminal, StatusCall: domain.CapabilityCall{ToolName: "virtualization.vm_ssh.get", CapabilityVersion: "1", Input: map[string]any{"commandId": item.ID}}}
	if !item.Terminal {
		ref.CancelCall = &domain.CapabilityCall{ToolName: "virtualization.vm_ssh.cancel", CapabilityVersion: "1", Input: map[string]any{"commandId": item.ID}}
	}
	return visibleCapabilityTaskRef(ref, visible)
}

func (p *virtualizationCapabilityProvider) recoverVMSSH(ctx context.Context, principal identity.Principal, tool domain.ToolCapability, input map[string]any) (any, bool, error) {
	service, ok := p.service.(vmSSHCapabilityService)
	if !ok || tool.Action != "execute" {
		return nil, false, nil
	}
	var request sohaapi.VirtualMachineSSHCommandInput
	if err := mapInput(input, &request); err != nil {
		return nil, false, err
	}
	item, err := service.FindVMSSHCommand(ctx, principal, firstMapString(input, "vmId"), request)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	return item, err == nil, err
}
