package runtimeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type marketplaceRuntimePermissions struct{}

func (marketplaceRuntimePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{
		"admin":     {appaccess.PermSettingsRuntimeConfigView, appaccess.PermSettingsRuntimeConfigManage},
		"installer": {appaccess.PermPluginView, appaccess.PermPluginInstall},
	}, nil
}

func TestManagedMarketplaceRuntimeSettingsAuthorizePersistAndReapply(t *testing.T) {
	ctx := context.Background()
	var store memoryStore
	var audit captureAudit
	service, err := New(ctx, &store, NewRegistry(RegistryOptions{}), appaccess.NewPermissionResolver(marketplaceRuntimePermissions{}), &audit)
	if err != nil {
		t.Fatal(err)
	}
	admin := adminPrincipal()
	installer := domainidentity.Principal{UserID: "installer", Roles: []string{"installer"}}
	change := sohaapi.RuntimeConfigChangeRequest{Changes: []sohaapi.RuntimeConfigChange{{Key: KeyMarketplaceThirdPartyURLs, Value: []any{" https://CATALOG.example.com:443/index.json "}}}}
	if _, err := service.Get(ctx, installer); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("installer settings read error = %v", err)
	}
	if _, err := service.Validate(ctx, installer, change); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("installer validate error = %v", err)
	}
	if _, err := service.Apply(ctx, installer, change); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("installer apply error = %v", err)
	}
	if _, err := service.Rollback(ctx, installer, sohaapi.RuntimeConfigRollbackRequest{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("installer rollback error = %v", err)
	}
	validation, err := service.Validate(ctx, admin, change)
	if err != nil || !validation.Valid || !slices.Equal(validation.Changes[0].ProposedValue.([]string), []string{"https://catalog.example.com/index.json"}) {
		t.Fatalf("validate = %#v, error = %v", validation, err)
	}
	result, err := service.Apply(ctx, admin, change)
	if err != nil || result.Application.Status != sohaapi.RuntimeConfigApplicationStatusApplied || result.Revision.Version != 1 {
		t.Fatalf("apply = %#v, error = %v", result, err)
	}
	if len(audit.entries) != 1 || audit.entries[0].ActorID != admin.UserID || !slices.Equal(audit.entries[0].Metadata["keys"].([]string), []string{KeyMarketplaceThirdPartyURLs}) {
		t.Fatalf("audit = %#v", audit.entries)
	}
	canonical := "https://catalog.example.com/index.json"
	wire, err := json.Marshal(store.state.Overrides)
	if err != nil {
		t.Fatal(err)
	}
	var overrides map[string]any
	if err := json.Unmarshal(wire, &overrides); err != nil {
		t.Fatal(err)
	}
	if urls := overrides[KeyMarketplaceThirdPartyURLs].([]any); len(urls) != 1 || urls[0] != canonical {
		t.Fatalf("stored URLs = %#v", urls)
	}
	store.state.Overrides = overrides
	restarted := newTestService(t, &store, &audit)
	if sources, err := restarted.Current().ManagedMarketplaceSources(); err != nil || len(sources) != 1 || sources[0].URL != canonical {
		t.Fatalf("restart sources = %#v, error = %v", sources, err)
	}
	applier := &captureApplier{key: KeyMarketplaceThirdPartyURLs, called: make(chan struct{}, 1)}
	restarted.RegisterApplier(applier)
	if _, err := service.Apply(ctx, admin, sohaapi.RuntimeConfigChangeRequest{ExpectedVersion: 1, Changes: []sohaapi.RuntimeConfigChange{{Key: KeyMarketplaceThirdPartyURLs, Reset: true}}}); err != nil {
		t.Fatal(err)
	}
	restarted.reconcile(ctx)
	select {
	case <-applier.called:
	default:
		t.Fatal("persisted change did not reapply on peer")
	}
	if sources, err := restarted.Current().ManagedMarketplaceSources(); err != nil || len(sources) != 0 {
		t.Fatalf("reset sources = %#v, error = %v", sources, err)
	}
	if _, err := restarted.Rollback(ctx, admin, sohaapi.RuntimeConfigRollbackRequest{ExpectedVersion: 2, TargetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if sources, err := restarted.Current().ManagedMarketplaceSources(); err != nil || len(sources) != 1 || sources[0].URL != canonical {
		t.Fatalf("rollback sources = %#v, error = %v", sources, err)
	}
}

func TestManagedMarketplaceSettingsRejectConflictsBeforeCommit(t *testing.T) {
	ctx := context.Background()
	const deployed = "https://deployed.example.com/index.json"
	const catalog = "https://team.example.com/index.json"
	sources, err := appplugin.ManagedMarketplaceSources([]string{catalog})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(RegistryOptions{MarketplaceSources: []appplugin.MarketplaceSource{{ID: "deployed", URL: deployed}}})
	var store memoryStore
	service, err := New(ctx, &store, registry, appaccess.NewPermissionResolver(runtimePermissions{}), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, changes := range [][]sohaapi.RuntimeConfigChange{
		{{Key: KeyMarketplaceThirdPartyURLs, Value: []string{DefaultMarketplaceURL}}},
		{{Key: KeyMarketplaceThirdPartyURLs, Value: []string{"https://marketplace.opensoha.com/marketplace/index.json"}}},
		{{Key: KeyMarketplaceThirdPartyURLs, Value: []string{"https://opensoha.github.io/soha-marketplace/marketplace/index.json"}}},
		{{Key: KeyMarketplaceThirdPartyURLs, Value: []string{deployed}}},
		{{Key: KeyMarketplaceThirdPartyURLs, Value: []string{catalog, "https://TEAM.example.com:443/index.json"}}},
		{{Key: KeyMarketplaceThirdPartyURLs, Value: []string{catalog}}, {Key: KeyMarketplaceSourceID, Value: sources[0].ID}},
		{{Key: KeyMarketplaceThirdPartyURLs, Value: []string{catalog}}, {Key: KeyMarketplaceURL, Value: catalog}},
	} {
		request := sohaapi.RuntimeConfigChangeRequest{Changes: changes}
		validation, err := service.Validate(ctx, adminPrincipal(), request)
		if err != nil || validation.Valid || len(validation.Issues) == 0 || validation.Issues[0].Key != KeyMarketplaceThirdPartyURLs {
			t.Fatalf("validate %#v = %#v, error = %v", changes, validation, err)
		}
		if _, err := service.Apply(ctx, adminPrincipal(), request); !errors.Is(err, apperrors.ErrInvalidArgument) || store.state.Version != 0 {
			t.Fatalf("invalid apply %#v returned %v at revision %d", changes, err, store.state.Version)
		}
	}
}
