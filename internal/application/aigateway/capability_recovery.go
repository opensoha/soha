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

// Original-call recovery receives the stable server request identity as well
// as the frozen input. Input-only recovery cannot identify a YAML mutation.
type OriginalToolRecoveryProvider interface {
	RecoverOriginalTool(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, domainaigateway.ToolInvocationRequest) (any, bool, error)
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
	if call.ToolName == protectedPodExecTool {
		return s.recoverProtectedPodExecution(ctx, principal, call)
	}
	var output any
	var found bool
	switch owner := s.gatewayRegistry().toolOwner(tool.Name).(type) {
	case OriginalToolRecoveryProvider:
		output, found, err = owner.RecoverOriginalTool(ctx, principal, tool, call)
	case ToolRecoveryProvider:
		output, found, err = owner.RecoverTool(ctx, principal, tool, call.Input)
	case BuiltinCapabilityProvider:
		output, found, err = s.recoverDockerCapability(ctx, principal, call)
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

// A lost response is resolved by the existing original-key receipt read. This
// path never calls ExecutePodArgv or creates another approval or process.
func (s *Service) recoverProtectedPodExecution(ctx context.Context, principal domainidentity.Principal, call domainaigateway.ToolInvocationRequest) (*domainaigateway.ToolInvocationResult, error) {
	lookup := map[string]any{}
	for _, key := range []string{"clusterId", "namespace", "podName", "idempotencyKey"} {
		lookup[key] = call.Input[key]
	}
	observation := capabilityInvocationCall(domainaigateway.CapabilityCall{ToolName: "k8s.pods.exec.receipt", CapabilityVersion: "1", Input: lookup}, call)
	ctx, err := s.authorizeCapabilityCall(ctx, principal, observation)
	if err != nil {
		return nil, err
	}
	tool, _ := s.toolByName(call.ToolName)
	readTool, _ := s.toolByName(observation.ToolName)
	ctx = domainaigateway.WithExecutionAuthorization(ctx, domainaigateway.ExecutionAuthorization{ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, Call: observation, ResourceScope: resolvedCapabilityAuditScope(ctx, readTool, lookup, nil)})
	recovery, ok := s.gatewayRegistry().toolOwner(tool.Name).(ToolRecoveryProvider)
	if !ok {
		return nil, apperrors.ErrUnsupportedOperation
	}
	output, found, err := recovery.RecoverTool(ctx, principal, tool, call.Input)
	if err != nil || !found {
		return nil, err
	}
	visible, _, err := s.sanitizeToolOutputByAccessPolicy(ctx, principal, call.AIClientID, call.SkillID, tool, capabilityGatewayScope(tool, call.Input), output)
	if err != nil {
		return nil, err
	}
	return &domainaigateway.ToolInvocationResult{ToolName: tool.Name, CapabilityVersion: tool.Version, Result: "success", Output: visible, Task: s.gatewayRegistry().TaskReference(tool, output, visible)}, nil
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
