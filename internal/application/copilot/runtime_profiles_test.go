package copilot

import (
	"context"
	"errors"
	"slices"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	settings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type runtimeProfileTestStore struct {
	RuntimeProfileStore
	scopes []string
}

func (*runtimeProfileTestStore) PublishRuntimeProfile(_ context.Context, _ string, revision int64, profile domain.RuntimeProfile) (domain.RuntimeProfile, error) {
	profile.Revision = revision + 1
	return profile, nil
}

func (s *runtimeProfileTestStore) ListCurrentRuntimeProfiles(_ context.Context, scopes []string) ([]domain.RuntimeProfile, error) {
	s.scopes = scopes
	return []domain.RuntimeProfile{}, nil
}

type profileModelValidator struct {
	runtimeModelCatalogFixture
	input   appaigateway.WorkbenchRelayRequest
	efforts []string
}

func (v *profileModelValidator) ResolveWorkbenchModelOption(_ context.Context, _ identity.Principal, input appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchModelOption, error) {
	v.input = input
	return appaigateway.WorkbenchModelOption{PublicModel: input.PublicModel, ReasoningEfforts: v.efforts}, nil
}

func TestPublishRuntimeProfileRejectsUnsupportedCombinations(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{
		"editor": {appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")},
	})
	service.runtimeProfiles = &runtimeProfileTestStore{}
	service.runtimeAssets = make(runtimeAssetTestStore)
	service.dshRuntime = &runtimeAssetTestCatalog{items: []domain.RuntimeAssetDescriptor{
		{Kind: "mcp", ID: "one", Version: "1", Transport: "stdio", ExecutionPosition: "personal-docker"},
		{Kind: "mcp", ID: "two", Version: "1", Transport: "stdio", ExecutionPosition: "personal-docker"},
	}}
	service.settings = inspectionAuthzSettingsResolver{settings: settings.AISettings{WorkbenchModel: settings.AIWorkbenchModelSettings{
		Enabled: true, DefaultPublicModel: "default-model", DefaultRouteID: "default-route", DefaultEndpoint: "responses",
	}}}
	models := &profileModelValidator{efforts: []string{"low"}}
	service.runtime.ModelCatalog = models
	principal := identity.Principal{UserID: "editor", Roles: []string{"editor"}}
	profile := domain.RuntimeProfile{ID: "example", Name: "Example", Scope: domain.RuntimeScopeHeya, Enabled: true, Assets: []domain.RuntimeAssetRef{
		{Kind: "mcp", ID: "one", Version: "1", Required: true},
		{Kind: "mcp", ID: "two", Version: "1", Required: true},
	}}
	if _, err := service.PublishRuntimeProfile(t.Context(), principal, 0, profile); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("two required personal connections were published: %v", err)
	}
	profile.Assets[0].Required = false
	profile.Scope = domain.RuntimeScopeChat
	if _, err := service.PublishRuntimeProfile(t.Context(), principal, 0, profile); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Chat profile requiring a personal connection was published: %v", err)
	}
	profile.Scope = domain.RuntimeScopeHeya
	profile.ModelPreferences.ReasoningEffort = "high"
	if _, err := service.PublishRuntimeProfile(t.Context(), principal, 0, profile); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unsupported model preference was published: %v", err)
	}
	if models.input.PublicModel != "default-model" || models.input.RouteID != "default-route" || models.input.Endpoint != "responses" || models.input.ReasoningEffort != "high" {
		t.Fatalf("default route was not validated: %+v", models.input)
	}
	profile.ModelPreferences.PublicModel = "selected-model"
	models.efforts = []string{"high"}
	if published, err := service.PublishRuntimeProfile(t.Context(), principal, 0, profile); err != nil || published.Revision != 1 || models.input.PublicModel != "selected-model" || models.input.RouteID != "" {
		t.Fatalf("supported override was not published: %+v, %+v, %v", published, models.input, err)
	}
}

func (s *runtimeProfileTestStore) GetCurrentRuntimeProfile(_ context.Context, id, scope string) (domain.RuntimeProfile, error) {
	if id != scope+"-default" {
		return domain.RuntimeProfile{}, apperrors.ErrNotFound
	}
	return domain.RuntimeProfile{ID: id, Scope: scope, Enabled: true, Revision: 1}, nil
}

func TestRuntimeProfilePermissionsAndScope(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	svc, sessions := newInspectionAuthzTestService(map[string][]string{
		"chat":            {appaccess.PermObserveAIChatUse},
		"settings-viewer": {appaccess.PermSettingsAIView},
	})
	store := &runtimeProfileTestStore{}
	svc.runtimeProfiles = store
	ctx := t.Context()
	chat := identity.Principal{UserID: "owner", Roles: []string{"chat"}}
	viewer := identity.Principal{UserID: "viewer", Roles: []string{"settings-viewer"}}
	if _, err := svc.ListRuntimeProfileRevisions(ctx, chat, "chat-default"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("chat actor read settings history: %v", err)
	}
	if _, err := svc.PublishRuntimeProfile(ctx, viewer, 0, domain.RuntimeProfile{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("settings viewer published profile: %v", err)
	}
	if _, err := svc.ListRuntimeProfiles(ctx, viewer); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("settings viewer read chat profiles: %v", err)
	}
	sessions.accessStates = map[string]domain.PersonalAgentAccess{}
	if _, err := svc.ListRuntimeProfiles(ctx, chat); err != nil || !slices.Equal(store.scopes, []string{domain.RuntimeScopeChat}) {
		t.Fatalf("unadmitted profile scope: %v %v", store.scopes, err)
	}
	sessions.session = domain.Session{ID: "session", CreatedBy: chat.UserID, Metadata: map[string]any{"conversationKind": "personal_main"}}
	if _, err := svc.SelectSessionRuntimeProfile(ctx, chat, sessions.session.ID, "heya-default"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("unadmitted Heya selection: %v", err)
	}
	sessions.accessStates[chat.UserID] = domain.PersonalAgentAccess{Status: "approved"}
	if _, err := svc.ListRuntimeProfiles(ctx, chat); err != nil || !slices.Equal(store.scopes, []string{domain.RuntimeScopeChat, domain.RuntimeScopeHeya}) {
		t.Fatalf("admitted profile scope: %v %v", store.scopes, err)
	}
	if _, err := svc.SelectSessionRuntimeProfile(ctx, chat, sessions.session.ID, "chat-default"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Chat profile selected for Heya: %v", err)
	}
	resolved, err := svc.resolveRuntimeProfile(ctx, chat, sessions.session, parseSessionMetadata(sessions.session.Metadata))
	if err != nil || resolved == nil || resolved.ID != "heya-default" {
		t.Fatalf("implicit Heya default: %+v %v", resolved, err)
	}
}
