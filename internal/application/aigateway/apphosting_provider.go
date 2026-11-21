package aigateway

import (
	"context"
	"fmt"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type AppHostingCapabilityService interface {
	ListAudienceCandidates(ctx context.Context, principal domainidentity.Principal, query string, limit int) (sohaapi.AppHostingAudienceCandidates, error)
	ListApps(ctx context.Context, principal domainidentity.Principal, filter domainapphosting.AppListFilter) (sohaapi.HostedAppList, error)
	CreateApp(ctx context.Context, principal domainidentity.Principal, req sohaapi.CreateHostedAppRequest) (sohaapi.HostedApp, bool, error)
	GetApp(ctx context.Context, principal domainidentity.Principal, appID string) (sohaapi.HostedApp, error)
	UpdateVisibility(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.UpdateHostedAppVisibilityRequest) (sohaapi.HostedApp, error)
	UpdateMaintainers(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.UpdateHostedAppMaintainersRequest) (sohaapi.HostedApp, error)
	TakeOffline(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error)
	BringOnline(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error)
	Preflight(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.HostedAppPreflightRequest) (sohaapi.HostedAppPreflightResult, error)
	ListRevisions(ctx context.Context, principal domainidentity.Principal, appID string, limit, offset int) (sohaapi.HostedAppRevisionList, error)
	Rollback(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.HostedAppRollbackRequest) (sohaapi.HostedAppDeploymentReceipt, error)
	DeployFromWorkspace(ctx context.Context, principal domainidentity.Principal, req sohaapi.WorkspaceHostedAppDeploymentRequest) (sohaapi.WorkspaceHostedAppDeploymentResult, error)
}

type appHostingToolHandler func(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error)

type appHostingCapabilityProvider struct {
	service  AppHostingCapabilityService
	tools    []domainaigateway.ToolCapability
	invokers map[string]appHostingToolHandler
}

func NewAppHostingCapabilityProvider(service AppHostingCapabilityService) (CapabilityProvider, error) {
	schemas, err := buildAppHostingToolSchemas()
	if err != nil {
		return nil, err
	}
	tools := appHostingToolSpecs(schemas)
	for i := range tools {
		tools[i].Version = "1"
		tools[i].Domain = "apphosting"
		tools[i].MCPAdapterID = "apphosting.v1"
		tools[i].MCPToolName = tools[i].Name
		tools[i].RequiredScopes = []string{"aiClient", "skill"}
		if _, err := compileCapabilityInputSchema(tools[i].InputSchema); err != nil {
			return nil, fmt.Errorf("%s: %w", tools[i].Name, err)
		}
	}
	p := &appHostingCapabilityProvider{service: service, tools: tools}
	p.invokers = p.buildInvokerTable()
	return p, nil
}

func appHostingToolSpecs(schemas appHostingSchemas) []domainaigateway.ToolCapability {
	viewPerms := []string{appaccess.PermAIGatewayInvoke, appaccess.PermAppHostingAppsView}
	publishPerms := []string{appaccess.PermAIGatewayInvoke, appaccess.PermAppHostingAppsPublish}
	workspacePerms := []string{appaccess.PermAIGatewayInvoke, appaccess.PermAppHostingAppsPublish, appaccess.PermObserveAIChatUse}
	syncIdempotent := &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}
	syncCreate := &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: false}

	return []domainaigateway.ToolCapability{
		{Name: "apphosting.audience.search", Title: "Search App Hosting Audience Candidates", Description: "Search sanitized colleague users and cross-department teams for hosted application visibility sharing.", Action: "search", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: publishPerms, InputSchema: schemas.audienceSearch, Execution: syncIdempotent},
		{Name: "apphosting.apps.list", Title: "List Hosted Applications", Description: "List hosted applications visible in the caller's maintainer or administrator scope.", Action: "list", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: viewPerms, InputSchema: schemas.listApps, Execution: syncIdempotent},
		{Name: "apphosting.apps.create", Title: "Create Hosted Application", Description: "Create a hosted application and allocate its immutable subdomain label.", Action: "create", RiskLevel: domainaigateway.RiskLevelMutate, PermissionKeys: publishPerms, InputSchema: schemas.createApp, Effects: []string{"apphosting.app.created"}, Execution: syncCreate},
		{Name: "apphosting.apps.get", Title: "Get Hosted Application", Description: "Get hosted application metadata, visibility scope, and current active revision binding.", Action: "get", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: viewPerms, InputSchema: schemas.getApp, Execution: syncIdempotent},
		{Name: "apphosting.apps.visibility.set", Title: "Set Hosted Application Visibility", Description: "Replace a hosted application's audience visibility scope and bump routeGeneration for immediate revocation.", Action: "update", RiskLevel: domainaigateway.RiskLevelMutate, PermissionKeys: publishPerms, InputSchema: schemas.setVisibility, Effects: []string{"apphosting.app.visibility_updated"}, Execution: syncIdempotent},
		{Name: "apphosting.apps.maintainers", Title: "Update Hosted Application Maintainers", Description: "Replace collaborator maintainers authorized to upload and roll back revisions of a hosted application.", Action: "update", RiskLevel: domainaigateway.RiskLevelMutate, PermissionKeys: publishPerms, InputSchema: schemas.maintainers, Effects: []string{"apphosting.app.maintainers_updated"}, Execution: syncIdempotent},
		{Name: "apphosting.apps.offline", Title: "Take Hosted Application Offline", Description: "Take an online hosted application offline and immediately revoke visitor routing.", Action: "offline", RiskLevel: domainaigateway.RiskLevelMutate, PermissionKeys: publishPerms, InputSchema: schemas.lifecycleAction, Effects: []string{"apphosting.app.offline"}, Execution: syncIdempotent},
		{Name: "apphosting.apps.online", Title: "Bring Hosted Application Online", Description: "Bring an offline hosted application with a ready active revision back online.", Action: "online", RiskLevel: domainaigateway.RiskLevelMutate, PermissionKeys: publishPerms, InputSchema: schemas.lifecycleAction, Effects: []string{"apphosting.app.online"}, Execution: syncIdempotent},
		{Name: "apphosting.apps.preflight", Title: "Preflight Hosted Application Bundle", Description: "Validate bundle size limits, entrypoint presence, and flagged file paths before uploading a revision.", Action: "preflight", RiskLevel: domainaigateway.RiskLevelAnalyze, PermissionKeys: publishPerms, InputSchema: schemas.preflight, Execution: syncIdempotent},
		{Name: "apphosting.apps.revisions.list", Title: "List Hosted Application Revisions", Description: "List immutable revisions of an authorized hosted application.", Action: "list", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: viewPerms, InputSchema: schemas.listRevisions, Execution: syncIdempotent},
		{Name: "apphosting.apps.rollback", Title: "Rollback Hosted Application Revision", Description: "Atomically switch the active revision of a hosted application to an earlier ready revision without altering visibility.", Action: "rollback", RiskLevel: domainaigateway.RiskLevelMutate, PermissionKeys: publishPerms, InputSchema: schemas.rollback, Effects: []string{"apphosting.app.rolled_back"}, Execution: syncIdempotent},
		{Name: "apphosting.workspace.publish", Title: "Publish Hosted Application From Assistant Workspace", Description: "Package and publish a static hosted application directly from the caller's personal assistant workspace or conversation artifact.", Action: "publish", RiskLevel: domainaigateway.RiskLevelMutate, PermissionKeys: workspacePerms, InputSchema: schemas.workspaceDeploy, Effects: []string{"apphosting.app.deployed"}, Execution: syncIdempotent},
	}
}

type appHostingSchemas struct {
	audienceSearch  map[string]any
	listApps        map[string]any
	createApp       map[string]any
	getApp          map[string]any
	setVisibility   map[string]any
	maintainers     map[string]any
	lifecycleAction map[string]any
	preflight       map[string]any
	listRevisions   map[string]any
	rollback        map[string]any
	workspaceDeploy map[string]any
}

func buildAppHostingToolSchemas() (appHostingSchemas, error) {
	appIDProp := map[string]any{"type": "string", "minLength": 1, "maxLength": 128}
	createApp, err := capabilityOpenAPISchema("CreateHostedAppRequest")
	if err != nil {
		return appHostingSchemas{}, err
	}
	setVisibility, err := schemaWithAppID("UpdateHostedAppVisibilityRequest", appIDProp, []string{"appId", "visibility"})
	if err != nil {
		return appHostingSchemas{}, err
	}
	maintainers, err := schemaWithAppID("UpdateHostedAppMaintainersRequest", appIDProp, []string{"appId", "maintainerUserIds"})
	if err != nil {
		return appHostingSchemas{}, err
	}
	preflight, err := schemaWithAppID("HostedAppPreflightRequest", appIDProp, []string{"appId", "kind", "uncompressedBytes", "fileCount", "hasIndexHtml"})
	if err != nil {
		return appHostingSchemas{}, err
	}
	rollback, err := schemaWithAppID("HostedAppRollbackRequest", appIDProp, []string{"appId", "targetRevisionId"})
	if err != nil {
		return appHostingSchemas{}, err
	}
	workspaceDeploy, err := capabilityOpenAPISchema("WorkspaceHostedAppDeploymentRequest")
	if err != nil {
		return appHostingSchemas{}, err
	}

	return appHostingSchemas{
		audienceSearch: gatewayObjectSchema(nil, map[string]any{
			"query": map[string]any{"type": "string", "maxLength": 200},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		}),
		listApps: gatewayObjectSchema(nil, map[string]any{
			"scope":       map[string]any{"type": "string", "enum": []string{"mine", "all"}},
			"query":       map[string]any{"type": "string", "maxLength": 200},
			"status":      map[string]any{"type": "string", "enum": []string{"draft", "online", "offline", "suspended_by_admin"}},
			"ownerUserId": map[string]any{"type": "string", "maxLength": 128},
			"teamId":      map[string]any{"type": "string", "maxLength": 128},
			"limit":       map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
			"offset":      map[string]any{"type": "integer", "minimum": 0},
		}),
		createApp:     createApp,
		getApp:        gatewayObjectSchema([]string{"appId"}, map[string]any{"appId": appIDProp}),
		setVisibility: setVisibility,
		maintainers:   maintainers,
		lifecycleAction: gatewayObjectSchema([]string{"appId"}, map[string]any{
			"appId":  appIDProp,
			"reason": map[string]any{"type": "string", "maxLength": 512},
		}),
		preflight: preflight,
		listRevisions: gatewayObjectSchema([]string{"appId"}, map[string]any{
			"appId":  appIDProp,
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
			"offset": map[string]any{"type": "integer", "minimum": 0},
		}),
		rollback:        rollback,
		workspaceDeploy: workspaceDeploy,
	}, nil
}

func schemaWithAppID(componentName string, appIDProp map[string]any, required []string) (map[string]any, error) {
	schema, props, err := capabilityOpenAPIObject(componentName)
	if err != nil {
		return nil, err
	}
	props["appId"] = appIDProp
	schema["required"] = required
	return schema, nil
}

func (p *appHostingCapabilityProvider) Tools() []domainaigateway.ToolCapability       { return p.tools }
func (*appHostingCapabilityProvider) Resources() []domainaigateway.ResourceCapability { return nil }
func (*appHostingCapabilityProvider) Prompts() []domainaigateway.PromptCapability     { return nil }
func (*appHostingCapabilityProvider) Skills() []domainaigateway.SkillCapability       { return nil }

func (p *appHostingCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if p == nil || p.service == nil {
		return nil, nil, fmt.Errorf("%w: app hosting capability provider is unavailable", apperrors.ErrServiceUnavailable)
	}
	handler, ok := p.invokers[tool.Name]
	if !ok {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	return handler(ctx, principal, input)
}

func (p *appHostingCapabilityProvider) buildInvokerTable() map[string]appHostingToolHandler {
	return map[string]appHostingToolHandler{
		"apphosting.audience.search":     p.invokeAudienceSearch,
		"apphosting.apps.list":           p.invokeListApps,
		"apphosting.apps.create":         p.invokeCreateApp,
		"apphosting.apps.get":            p.invokeGetApp,
		"apphosting.apps.visibility.set": p.invokeSetVisibility,
		"apphosting.apps.maintainers":    p.invokeUpdateMaintainers,
		"apphosting.apps.offline":        p.invokeOffline,
		"apphosting.apps.online":         p.invokeOnline,
		"apphosting.apps.preflight":      p.invokePreflight,
		"apphosting.apps.revisions.list": p.invokeListRevisions,
		"apphosting.apps.rollback":       p.invokeRollback,
		"apphosting.workspace.publish":   p.invokeWorkspacePublish,
	}
}

func (p *appHostingCapabilityProvider) invokeGetApp(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	out, err := p.service.GetApp(ctx, principal, firstMapString(input, "appId"))
	return out, appIDMeta(out.ID), err
}

func (p *appHostingCapabilityProvider) invokeOffline(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	out, err := p.service.TakeOffline(ctx, principal, firstMapString(input, "appId"), firstMapString(input, "reason"))
	return out, appIDMeta(out.ID), err
}

func (p *appHostingCapabilityProvider) invokeOnline(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	out, err := p.service.BringOnline(ctx, principal, firstMapString(input, "appId"), firstMapString(input, "reason"))
	return out, appIDMeta(out.ID), err
}

func (p *appHostingCapabilityProvider) invokeAudienceSearch(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := mapInput(input, &params); err != nil {
		return nil, nil, err
	}
	out, err := p.service.ListAudienceCandidates(ctx, principal, params.Query, params.Limit)
	return out, nil, err
}

func (p *appHostingCapabilityProvider) invokeListApps(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var params struct {
		Scope       sohaapi.HostedAppListScope `json:"scope"`
		Query       string                     `json:"query"`
		Status      sohaapi.HostedAppStatus    `json:"status"`
		OwnerUserID string                     `json:"ownerUserId"`
		TeamID      string                     `json:"teamId"`
		Limit       int                        `json:"limit"`
		Offset      int                        `json:"offset"`
	}
	if err := mapInput(input, &params); err != nil {
		return nil, nil, err
	}
	out, err := p.service.ListApps(ctx, principal, domainapphosting.AppListFilter{
		Scope:       params.Scope,
		Query:       params.Query,
		Status:      params.Status,
		OwnerUserID: params.OwnerUserID,
		TeamID:      params.TeamID,
		Limit:       params.Limit,
		Offset:      params.Offset,
	})
	return out, nil, err
}

func (p *appHostingCapabilityProvider) invokeCreateApp(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var req sohaapi.CreateHostedAppRequest
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	out, _, err := p.service.CreateApp(ctx, principal, req)
	return out, appIDMeta(out.ID), err
}

func (p *appHostingCapabilityProvider) invokeSetVisibility(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var payload struct {
		AppID      string                           `json:"appId"`
		Visibility sohaapi.HostedAppVisibilityInput `json:"visibility"`
		Reason     string                           `json:"reason"`
	}
	if err := mapInput(input, &payload); err != nil {
		return nil, nil, err
	}
	out, err := p.service.UpdateVisibility(ctx, principal, payload.AppID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: payload.Visibility,
		Reason:     payload.Reason,
	})
	return out, appIDMeta(out.ID), err
}

func (p *appHostingCapabilityProvider) invokeUpdateMaintainers(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var payload struct {
		AppID             string   `json:"appId"`
		MaintainerUserIDs []string `json:"maintainerUserIds"`
	}
	if err := mapInput(input, &payload); err != nil {
		return nil, nil, err
	}
	out, err := p.service.UpdateMaintainers(ctx, principal, payload.AppID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: payload.MaintainerUserIDs,
	})
	return out, appIDMeta(out.ID), err
}

func (p *appHostingCapabilityProvider) invokePreflight(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	appID, body := stripAppIDField(input)
	var req sohaapi.HostedAppPreflightRequest
	if err := mapInput(body, &req); err != nil {
		return nil, nil, err
	}
	out, err := p.service.Preflight(ctx, principal, appID, req)
	return out, appIDMeta(appID), err
}

func (p *appHostingCapabilityProvider) invokeListRevisions(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var params struct {
		AppID  string `json:"appId"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := mapInput(input, &params); err != nil {
		return nil, nil, err
	}
	out, err := p.service.ListRevisions(ctx, principal, params.AppID, params.Limit, params.Offset)
	return out, appIDMeta(params.AppID), err
}

func (p *appHostingCapabilityProvider) invokeRollback(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	appID, body := stripAppIDField(input)
	var req sohaapi.HostedAppRollbackRequest
	if err := mapInput(body, &req); err != nil {
		return nil, nil, err
	}
	out, err := p.service.Rollback(ctx, principal, appID, req)
	return out, appIDMeta(appID), err
}

func (p *appHostingCapabilityProvider) invokeWorkspacePublish(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var req sohaapi.WorkspaceHostedAppDeploymentRequest
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	out, err := p.service.DeployFromWorkspace(ctx, principal, req)
	return out, appIDMeta(out.App.ID), err
}

func stripAppIDField(input map[string]any) (string, map[string]any) {
	appID := firstMapString(input, "appId")
	cloned := make(map[string]any, len(input))
	for k, v := range input {
		if k != "appId" {
			cloned[k] = v
		}
	}
	return appID, cloned
}

func appIDMeta(appID string) map[string]any {
	if appID == "" {
		return nil
	}
	return map[string]any{"appId": appID}
}
