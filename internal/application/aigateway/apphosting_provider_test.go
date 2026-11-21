package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	contractsopenapi "github.com/opensoha/soha/contracts/openapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Compile-time check that *appapphosting.Service directly satisfies AppHostingCapabilityService.
var _ AppHostingCapabilityService = (*appapphosting.Service)(nil)

func TestAppHostingProviderMatchesOpenAPICapabilitiesAndToolSchema(t *testing.T) {
	t.Parallel()

	provider, err := NewAppHostingCapabilityProvider(nil)
	if err != nil {
		t.Fatalf("NewAppHostingCapabilityProvider(nil): %v", err)
	}
	tools := provider.Tools()
	if len(tools) != 10 {
		t.Fatalf("len(tools) = %d, want 10", len(tools))
	}

	toolSchema, err := capabilityOpenAPISchema("ToolCapability")
	if err != nil {
		t.Fatal(err)
	}
	validator, err := compileCapabilityInputSchema(toolSchema)
	if err != nil {
		t.Fatal(err)
	}

	byName := make(map[string]domainaigateway.ToolCapability, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if err := validator.Validate(doc); err != nil {
			t.Fatalf("%s violates ToolCapability schema: %v", tool.Name, err)
		}
	}

	var spec struct {
		Paths map[string]openAPIPathItem `json:"paths"`
	}
	if err := json.Unmarshal(contractsopenapi.JSON(), &spec); err != nil {
		t.Fatal(err)
	}

	matched := 0
	for path, item := range spec.Paths {
		for method, op := range map[string]*openAPIOperation{
			"GET": item.Get, "POST": item.Post, "PUT": item.Put, "PATCH": item.Patch, "DELETE": item.Delete,
		} {
			if op == nil || op.Capability == nil || !strings.HasPrefix(op.Capability.Name, "apphosting.") {
				continue
			}
			matched++
			got, exists := byName[op.Capability.Name]
			if !exists {
				t.Fatalf("%s %s capability %q missing from provider", method, path, op.Capability.Name)
			}
			if string(got.RiskLevel) != op.Capability.RiskLevel || got.RequiresApproval != op.Capability.RequiresApproval {
				t.Fatalf("%s governance = (%s, %v), want (%s, %v)", got.Name, got.RiskLevel, got.RequiresApproval, op.Capability.RiskLevel, op.Capability.RequiresApproval)
			}
			wantPerms := slices.Clone(op.Capability.PermissionKeys)
			gotPerms := slices.Clone(got.PermissionKeys)
			slices.Sort(wantPerms)
			slices.Sort(gotPerms)
			if !slices.Equal(gotPerms, wantPerms) {
				t.Fatalf("%s permissionKeys = %v, want %v", got.Name, gotPerms, wantPerms)
			}
		}
	}
	if matched != 10 {
		t.Fatalf("matched %d OpenAPI apphosting.* capabilities, want 10", matched)
	}
}

type stubAppHostingCapabilityService struct {
	lastTool string
}

func (s *stubAppHostingCapabilityService) ListAudienceCandidates(_ context.Context, _ domainidentity.Principal, query string, limit int) (sohaapi.AppHostingAudienceCandidates, error) {
	s.lastTool = "audience:" + query
	return sohaapi.AppHostingAudienceCandidates{
		Users: []sohaapi.AppHostingAudienceUser{{ID: "usr_1", Username: "bob", DisplayName: "Bob"}},
		Teams: []sohaapi.AppHostingAudienceTeam{},
	}, nil
}

func (s *stubAppHostingCapabilityService) ListApps(_ context.Context, _ domainidentity.Principal, filter domainapphosting.AppListFilter) (sohaapi.HostedAppList, error) {
	s.lastTool = "list:" + string(filter.Scope)
	return sohaapi.HostedAppList{Items: []sohaapi.HostedApp{}, Total: 0}, nil
}

func (s *stubAppHostingCapabilityService) CreateApp(_ context.Context, _ domainidentity.Principal, req sohaapi.CreateHostedAppRequest) (sohaapi.HostedApp, bool, error) {
	s.lastTool = "create:" + req.Name
	return sohaapi.HostedApp{ID: "app_1", Name: req.Name}, false, nil
}

func (s *stubAppHostingCapabilityService) GetApp(_ context.Context, _ domainidentity.Principal, appID string) (sohaapi.HostedApp, error) {
	s.lastTool = "get:" + appID
	return sohaapi.HostedApp{ID: appID}, nil
}

func (s *stubAppHostingCapabilityService) UpdateVisibility(_ context.Context, _ domainidentity.Principal, appID string, req sohaapi.UpdateHostedAppVisibilityRequest) (sohaapi.HostedApp, error) {
	s.lastTool = "vis:" + appID + ":" + string(req.Visibility.Mode)
	return sohaapi.HostedApp{ID: appID}, nil
}

func (s *stubAppHostingCapabilityService) TakeOffline(_ context.Context, _ domainidentity.Principal, appID, _ string) (sohaapi.HostedApp, error) {
	s.lastTool = "offline:" + appID
	return sohaapi.HostedApp{ID: appID}, nil
}

func (s *stubAppHostingCapabilityService) BringOnline(_ context.Context, _ domainidentity.Principal, appID, _ string) (sohaapi.HostedApp, error) {
	s.lastTool = "online:" + appID
	return sohaapi.HostedApp{ID: appID}, nil
}

func (s *stubAppHostingCapabilityService) Preflight(_ context.Context, _ domainidentity.Principal, appID string, req sohaapi.HostedAppPreflightRequest) (sohaapi.HostedAppPreflightResult, error) {
	s.lastTool = "preflight:" + appID
	return sohaapi.HostedAppPreflightResult{Allowed: req.HasIndexHTML, AppID: appID, Blockers: []string{}, Warnings: []string{}}, nil
}

func (s *stubAppHostingCapabilityService) ListRevisions(_ context.Context, _ domainidentity.Principal, appID string, _, _ int) (sohaapi.HostedAppRevisionList, error) {
	s.lastTool = "revisions:" + appID
	return sohaapi.HostedAppRevisionList{Items: []sohaapi.HostedAppRevision{}, Total: 0}, nil
}

func (s *stubAppHostingCapabilityService) Rollback(_ context.Context, _ domainidentity.Principal, appID string, req sohaapi.HostedAppRollbackRequest) (sohaapi.HostedAppDeploymentReceipt, error) {
	s.lastTool = "rollback:" + appID + ":" + req.TargetRevisionID
	return sohaapi.HostedAppDeploymentReceipt{App: sohaapi.HostedApp{ID: appID}}, nil
}

func TestAppHostingProviderInvokeAllTools(t *testing.T) {
	t.Parallel()

	nilProvider, err := NewAppHostingCapabilityProvider(nil)
	if err != nil {
		t.Fatal(err)
	}
	invoker, ok := nilProvider.(ToolCapabilityInvoker)
	if !ok {
		t.Fatal("expected ToolCapabilityInvoker")
	}
	if _, _, err := invoker.InvokeTool(t.Context(), domainidentity.Principal{}, nilProvider.Tools()[0], map[string]any{}); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("nil service InvokeTool = %v, want ErrServiceUnavailable", err)
	}

	stub := &stubAppHostingCapabilityService{}
	provider, err := NewAppHostingCapabilityProvider(stub)
	if err != nil {
		t.Fatal(err)
	}
	liveInvoker, ok := provider.(ToolCapabilityInvoker)
	if !ok {
		t.Fatal("expected ToolCapabilityInvoker")
	}
	principal := domainidentity.Principal{
		UserID:         "usr_alice",
		PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView},
	}

	calls := []struct {
		name     string
		input    map[string]any
		wantLast string
	}{
		{"apphosting.audience.search", map[string]any{"query": "fin", "limit": 10}, "audience:fin"},
		{"apphosting.apps.list", map[string]any{"scope": "mine"}, "list:mine"},
		{"apphosting.apps.create", map[string]any{"name": "Board"}, "create:Board"},
		{"apphosting.apps.get", map[string]any{"appId": "app_1"}, "get:app_1"},
		{"apphosting.apps.visibility.set", map[string]any{"appId": "app_1", "visibility": map[string]any{"mode": "private"}}, "vis:app_1:private"},
		{"apphosting.apps.offline", map[string]any{"appId": "app_1", "reason": "maint"}, "offline:app_1"},
		{"apphosting.apps.online", map[string]any{"appId": "app_1"}, "online:app_1"},
		{"apphosting.apps.preflight", map[string]any{"appId": "app_1", "kind": "static", "uncompressedBytes": 128, "fileCount": 1, "hasIndexHtml": true}, "preflight:app_1"},
		{"apphosting.apps.revisions.list", map[string]any{"appId": "app_1", "limit": 5}, "revisions:app_1"},
		{"apphosting.apps.rollback", map[string]any{"appId": "app_1", "targetRevisionId": "rev_1"}, "rollback:app_1:rev_1"},
	}
	for _, tc := range calls {
		var tool domainaigateway.ToolCapability
		for _, candidate := range provider.Tools() {
			if candidate.Name == tc.name {
				tool = candidate
				break
			}
		}
		if err := validateCapabilityInput(tool, tc.input); err != nil {
			t.Fatalf("%s input schema rejected valid input: %v", tc.name, err)
		}
		if _, _, err := liveInvoker.InvokeTool(t.Context(), principal, tool, tc.input); err != nil {
			t.Fatalf("InvokeTool(%s): %v", tc.name, err)
		}
		if stub.lastTool != tc.wantLast {
			t.Fatalf("InvokeTool(%s) lastTool = %q, want %q", tc.name, stub.lastTool, tc.wantLast)
		}
	}
}
