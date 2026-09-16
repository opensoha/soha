package aigateway

import (
	"context"
	"errors"
	"reflect"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DockerProjectCapabilityService interface {
	CreateProject(context.Context, domainidentity.Principal, domaindocker.ProjectInput) (domaindocker.Project, error)
	FindProjectCreation(context.Context, domainidentity.Principal, domaindocker.ProjectInput) (domaindocker.Project, error)
	GetProject(context.Context, domainidentity.Principal, string) (domaindocker.Project, error)
}

type dockerProjectCapabilityProvider struct {
	service DockerProjectCapabilityService
	tools   []domainaigateway.ToolCapability
}

func NewDockerProjectCapabilityProvider(service DockerProjectCapabilityService) (CapabilityProvider, error) {
	schema, properties, err := capabilityOpenAPIObject("DockerProjectInput")
	if err != nil {
		return nil, err
	}
	schema["required"] = []string{"hostId", "name", "composeContent", "idempotencyKey"}

	for _, key := range []string{"id", "sourceRef", "status", "desiredState", "templateId", "ttlSeconds"} {
		delete(properties, key)
	}
	properties["sourceKind"] = map[string]any{"type": "string", "enum": []string{"compose", "single_container"}}
	if _, err := compileCapabilityInputSchema(schema); err != nil {
		return nil, err
	}
	tools := []domainaigateway.ToolCapability{
		{Name: "docker.projects.create", Title: "Create Docker Project", Description: "Save inline Docker/Compose configuration and service definitions atomically with a fixed creation receipt. Does not deploy. Use delivery drafts and a delivery batch to bind verified artifacts and deploy.", Action: "create", RiskLevel: domainaigateway.RiskLevelExecute, RequiresApproval: true, PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.ManagedActionPermission(appaccess.PermDockerProjectsManage, "create")}, InputSchema: schema, Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true, IdempotencyKeyField: "idempotencyKey"}, Effects: []string{"docker.project.configured"}},
		{Name: "docker.projects.get", Title: "Get Docker Project", Description: "Read the current authorized project identity and configuration state; Compose and environment content are omitted.", Action: "get", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDockerProjectsView}, InputSchema: gatewayObjectSchema([]string{"projectId"}, map[string]any{"projectId": map[string]any{"type": "string", "minLength": 1}}), Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}},
	}
	for i := range tools {
		tools[i].Version, tools[i].Domain, tools[i].MCPAdapterID, tools[i].MCPToolName = "1", "docker", "docker.v1", tools[i].Name
		tools[i].RequiredScopes = []string{"dockerHost", "dockerProject"}
		tools[i].OutputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/id", Kind: "docker.project"}, {Path: "/hostId", Kind: "docker.host"}}
	}
	tools[0].InputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/hostId", Kind: "docker.host"}, {Path: "/envContent", Kind: "secret"}}
	tools[0].Execution.RecoveryMode = "original_call"
	tools[1].InputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/projectId", Kind: "docker.project"}}
	return &dockerProjectCapabilityProvider{service: service, tools: tools}, nil
}

func (p *dockerProjectCapabilityProvider) Tools() []domainaigateway.ToolCapability       { return p.tools }
func (*dockerProjectCapabilityProvider) Resources() []domainaigateway.ResourceCapability { return nil }
func (*dockerProjectCapabilityProvider) Prompts() []domainaigateway.PromptCapability     { return nil }
func (*dockerProjectCapabilityProvider) Skills() []domainaigateway.SkillCapability       { return nil }

func (p *dockerProjectCapabilityProvider) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) ([]map[string]string, error) {
	if tool.Action == "create" {
		return []map[string]string{{"hostId": firstMapString(input, "hostId")}}, nil
	}
	item, err := p.service.GetProject(ctx, principal, firstMapString(input, "projectId"))
	return []map[string]string{{"hostId": item.HostID, "projectId": item.ID}}, err
}

func projectCapabilityResult(ctx context.Context, tool domainaigateway.ToolCapability, input map[string]any, item domaindocker.Project) (domaindocker.Project, error) {
	resolved, _ := ctx.Value(capabilityScopeContextKey{}).(capabilityScopeContext)
	if resolved.tool == tool.Name {
		scope := map[string]string{"hostId": item.HostID}
		if tool.Action == "get" {
			scope["projectId"] = item.ID
		}
		actual, err := mergeCapabilityScope(capabilityGatewayScope(tool, input), scope)
		if err != nil {
			return domaindocker.Project{}, err
		}
		if !reflect.DeepEqual(resolved.scopes, []map[string]string{actual}) {
			return domaindocker.Project{}, apperrors.ErrConflict
		}
	}
	return domaindocker.Project{ID: item.ID, HostID: item.HostID, Name: item.Name, Slug: item.Slug, SourceKind: item.SourceKind, Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}, nil
}

func (p *dockerProjectCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	var item domaindocker.Project
	var err error
	switch tool.Name {
	case "docker.projects.create":
		var request domaindocker.ProjectInput
		if err = mapInput(input, &request); err == nil {
			item, err = p.service.CreateProject(ctx, principal, request)
		}
	case "docker.projects.get":
		item, err = p.service.GetProject(ctx, principal, firstMapString(input, "projectId"))
	default:
		err = apperrors.ErrUnsupportedOperation
	}
	if err != nil {
		return nil, nil, err
	}
	item, err = projectCapabilityResult(ctx, tool, input, item)
	return item, nil, err
}

func (p *dockerProjectCapabilityProvider) RecoverTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, bool, error) {
	if tool.Name != "docker.projects.create" {
		return nil, false, nil
	}
	var request domaindocker.ProjectInput
	if err := mapInput(input, &request); err != nil {
		return nil, false, err
	}
	item, err := p.service.FindProjectCreation(ctx, principal, request)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	if err == nil {
		item, err = projectCapabilityResult(ctx, tool, input, item)
	}
	return item, err == nil, err
}
