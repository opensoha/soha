package aigateway

import (
	"context"
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type PersonalEnvironmentInspector interface {
	InspectPersonalEnvironment(context.Context, domainidentity.Principal) (domaincopilot.PersonalEnvironment, error)
	ReadPersonalEnvironmentFile(context.Context, domainidentity.Principal, string) (domaincopilot.PersonalEnvironmentFile, error)
	ExecutePersonalCommand(context.Context, domainidentity.Principal, domaincopilot.PersonalCommandInput) (domaincopilot.PersonalCommand, error)
	FindPersonalCommand(context.Context, domainidentity.Principal, domaincopilot.PersonalCommandInput) (domaincopilot.PersonalCommand, error)
	GetPersonalCommand(context.Context, domainidentity.Principal, string) (domaincopilot.PersonalCommand, error)
	CancelPersonalCommand(context.Context, domainidentity.Principal, string) (domaincopilot.PersonalCommand, error)
}

type personalEnvironmentCapabilityProvider struct{ inspector PersonalEnvironmentInspector }

func NewPersonalEnvironmentCapabilityProvider(inspector PersonalEnvironmentInspector) CapabilityProvider {
	return &personalEnvironmentCapabilityProvider{inspector: inspector}
}

func (*personalEnvironmentCapabilityProvider) Tools() []domain.ToolCapability {
	tools := []domain.ToolCapability{{
		Name: "personal.environment.inspect", Version: "1", Title: "Inspect personal environment", Description: "Obtain a fresh receipt for the authenticated assistant's own environment. Unprovisioned, busy and unknown states never imply command readiness.",
		Domain: "personal_environment", Action: "inspect", RiskLevel: domain.RiskLevelRead,
		PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse}, RequiredScopes: []string{"aiClient", "skill"}, MCPAdapterID: "personal.environment.v1", MCPToolName: "personal.environment.inspect",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
	}, {
		Name: "personal.environment.files.read", Version: "1", Title: "Read personal data file", Description: "Read a bounded UTF-8 file from the authenticated assistant's persistent data. Supply a relative path. Requires an already ready environment; never provisions data or executes caller-selected commands.",
		Domain: "personal_environment", Action: "file_read", RiskLevel: domain.RiskLevelRead,
		PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse}, RequiredScopes: []string{"aiClient", "skill"}, MCPAdapterID: "personal.environment.v1", MCPToolName: "personal.environment.files.read",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "minLength": 1, "maxLength": 1024}}, "required": []string{"path"}, "additionalProperties": false},
	}}
	return append(tools, personalCommandCapabilities()...)
}

func (*personalEnvironmentCapabilityProvider) Resources() []domain.ResourceCapability { return nil }
func (*personalEnvironmentCapabilityProvider) Prompts() []domain.PromptCapability     { return nil }
func (*personalEnvironmentCapabilityProvider) Skills() []domain.SkillCapability       { return nil }

func (p *personalEnvironmentCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domain.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if p == nil || p.inspector == nil {
		return nil, nil, apperrors.ErrServiceUnavailable
	}
	if tool.Domain == "personal_command" {
		return p.invokePersonalCommand(ctx, principal, tool, input)
	}
	if tool.Name == "personal.environment.files.read" {
		path, valid := input["path"].(string)
		if !valid || path == "" || len(input) != 1 {
			return nil, nil, apperrors.ErrInvalidArgument
		}
		item, err := p.inspector.ReadPersonalEnvironmentFile(ctx, principal, path)
		if err != nil {
			return nil, nil, err
		}
		return item, map[string]any{"personalAssistantId": item.AssistantID, "environmentVersion": item.EnvironmentVersion}, nil
	}
	if tool.Name != "personal.environment.inspect" {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	if len(input) != 0 {
		return nil, nil, fmt.Errorf("%w: personal environment inspection accepts no owner, node or command", apperrors.ErrInvalidArgument)
	}
	item, err := p.inspector.InspectPersonalEnvironment(ctx, principal)
	if err != nil {
		return nil, nil, err
	}
	return item, map[string]any{"personalAssistantId": item.AssistantID, "environmentVersion": item.Version}, nil
}
