package aigateway

import (
	"context"
	"errors"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// A provider may recover its own durable idempotency record after a lost reply.
// Recovery must be read-only and use current identity and the exact frozen call.
type ToolRecoveryProvider interface {
	RecoverTool(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, bool, error)
}

type dockerCapabilityRecovery interface {
	FindQuickCreateOperation(context.Context, domainidentity.Principal, domaindocker.QuickCreateHostInput) (domaindocker.Operation, error)
	FindProjectDeployOperation(context.Context, domainidentity.Principal, string, domaindocker.ProjectDeployInput) (domaindocker.Operation, error)
	FindServiceActionOperation(context.Context, domainidentity.Principal, string, domaindocker.ServiceActionInput) (domaindocker.Operation, error)
}

func (s *Service) recoverCapabilityCall(ctx context.Context, principal domainidentity.Principal, call domainaigateway.ToolInvocationRequest) (*domainaigateway.ToolInvocationResult, error) {
	tool, ok := s.toolByName(call.ToolName)
	if !ok {
		return nil, apperrors.ErrAccessDenied
	}
	ctx, err := s.authorizeCapabilityCall(ctx, principal, call)
	if err != nil {
		return nil, err
	}
	var output any
	var found bool
	for _, provider := range s.gatewayRegistry().providers {
		if !providerHasTool(provider, tool.Name) {
			continue
		}
		if recovery, ok := provider.(ToolRecoveryProvider); ok {
			output, found, err = recovery.RecoverTool(ctx, principal, tool, call.Input)
		} else if _, builtin := provider.(BuiltinCapabilityProvider); builtin {
			output, found, err = s.recoverDockerCapability(ctx, principal, call)
		}
		break
	}
	if err != nil || !found {
		return nil, err
	}
	visible, _, err := s.sanitizeToolOutputByAccessPolicy(ctx, principal, call.AIClientID, call.SkillID, tool, capabilityGatewayScope(tool, call.Input), output)
	if err != nil {
		return nil, err
	}
	return &domainaigateway.ToolInvocationResult{ToolName: tool.Name, CapabilityVersion: tool.Version, Result: "success", Output: visible, Task: s.gatewayRegistry().TaskReference(tool, output, visible), Assessment: visibleCapabilityAssessment(tool, visible)}, nil
}

func (s *Service) recoverDockerCapability(ctx context.Context, principal domainidentity.Principal, call domainaigateway.ToolInvocationRequest) (any, bool, error) {
	ctx = withDockerScopeCheck(ctx, call.ToolName)
	service, ok := s.docker.(dockerCapabilityRecovery)
	if !ok {
		return nil, false, nil
	}
	key, _ := call.Input["idempotencyKey"].(string)
	var operation domaindocker.Operation
	var err error
	switch call.ToolName {
	case "docker.hosts.quick_create.trigger":
		var input domaindocker.QuickCreateHostInput
		if err := mapInput(call.Input, &input); err != nil {
			return nil, false, err
		}
		input.IdempotencyKey = key
		operation, err = service.FindQuickCreateOperation(ctx, principal, input)
	case "docker.projects.deploy.trigger":
		var input struct {
			ProjectID string `json:"projectId"`
			domaindocker.ProjectDeployInput
		}
		if err := mapInput(call.Input, &input); err != nil {
			return nil, false, err
		}
		input.IdempotencyKey = key
		operation, err = service.FindProjectDeployOperation(ctx, principal, input.ProjectID, input.ProjectDeployInput)
	case "docker.services.action.trigger":
		var input struct {
			ServiceID string `json:"serviceId"`
			domaindocker.ServiceActionInput
		}
		if err := mapInput(call.Input, &input); err != nil {
			return nil, false, err
		}
		input.IdempotencyKey = key
		operation, err = service.FindServiceActionOperation(ctx, principal, input.ServiceID, input.ServiceActionInput)
	default:
		return nil, false, nil
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	return operation, err == nil, err
}

func (s *Service) validCapabilityTaskResult(call domainaigateway.ToolInvocationRequest, result domainaigateway.ToolInvocationResult) bool {
	tool, ok := s.toolByName(call.ToolName)
	if !ok || tool.Execution == nil {
		return false
	}
	if result.Result != "success" {
		return true
	}
	if result.Task == nil {
		return tool.Execution.Mode != "async"
	}
	task := result.Task
	status, ok := s.toolByName(task.StatusCall.ToolName)
	if !ok || task.Kind != tool.Execution.TaskKind || task.StatusCall.ToolName != tool.Execution.StatusTool || task.StatusCall.CapabilityVersion == "" || validateCapabilityVersion(status, task.StatusCall.CapabilityVersion) != nil {
		return false
	}
	if status.RiskLevel != domainaigateway.RiskLevelRead && status.RiskLevel != domainaigateway.RiskLevelAnalyze {
		return false
	}
	if task.CancelCall != nil && task.CancelCall.ToolName != tool.Execution.CancelTool {
		return false
	}
	return true
}
