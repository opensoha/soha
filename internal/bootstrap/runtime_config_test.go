package bootstrap

import (
	"context"
	"errors"
	"sync"
	"testing"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
	appruntimeconfig "github.com/opensoha/soha/internal/application/runtimeconfig"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	domainruntimeconfig "github.com/opensoha/soha/internal/domain/runtimeconfig"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

type lifecycleTestStore struct{}

func (lifecycleTestStore) LoadState(context.Context) (domainruntimeconfig.State, error) {
	return domainruntimeconfig.State{Overrides: map[string]any{}}, nil
}
func (lifecycleTestStore) Commit(context.Context, domainruntimeconfig.Commit) (domainruntimeconfig.State, error) {
	return domainruntimeconfig.State{}, nil
}
func (lifecycleTestStore) UpdateApplication(context.Context, domainruntimeconfig.Application) error {
	return nil
}
func (lifecycleTestStore) ListRevisions(context.Context, int) ([]domainruntimeconfig.Revision, error) {
	return nil, nil
}
func (lifecycleTestStore) GetRevisionByVersion(context.Context, int64) (domainruntimeconfig.Revision, error) {
	return domainruntimeconfig.Revision{}, domainruntimeconfig.ErrNotFound
}
func (lifecycleTestStore) GetApplication(context.Context, string) (domainruntimeconfig.Application, error) {
	return domainruntimeconfig.Application{}, domainruntimeconfig.ErrNotFound
}

type fakeRestartableModule struct {
	mu      sync.Mutex
	running bool
	starts  int
	stops   int
}

func (s *fakeRestartableModule) Start(context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		s.starts++
		s.running = true
	}
}
func (s *fakeRestartableModule) Stop(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.stops++
	}
	s.running = false
	return nil
}
func (s *fakeRestartableModule) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func runtimeSnapshot(t *testing.T, enabled bool) appruntimeconfig.Snapshot {
	t.Helper()
	service, err := appruntimeconfig.New(context.Background(), lifecycleTestStore{}, appruntimeconfig.NewRegistry(appruntimeconfig.RegistryOptions{ModuleAI: enabled}), appaccess.NewPermissionResolver(nil), nil)
	if err != nil {
		t.Fatalf("build runtime config service: %v", err)
	}
	return service.Current()
}

func TestModuleLifecycleApplierSupportsStartStopStart(t *testing.T) {
	service := &fakeRestartableModule{}
	applier := newModuleLifecycleApplier(context.Background(), map[string]restartableModule{
		appruntimeconfig.KeyModuleAI: service,
	})
	disabled, enabled := runtimeSnapshot(t, false), runtimeSnapshot(t, true)

	for _, transition := range [][2]appruntimeconfig.Snapshot{{disabled, enabled}, {enabled, disabled}, {disabled, enabled}} {
		items, err := applier.Apply(context.Background(), transition[0], transition[1], []string{appruntimeconfig.KeyModuleAI})
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
		if len(items) != 1 || items[0].Status != sohaapi.RuntimeConfigApplicationStatusApplied {
			t.Fatalf("unexpected application items: %#v", items)
		}
	}
	if service.starts != 2 || service.stops != 1 || !service.Running() {
		t.Fatalf("lifecycle counts = starts %d stops %d running %v", service.starts, service.stops, service.Running())
	}
}

type marketplaceSnapshotStore struct {
	lifecycleTestStore
	overrides map[string]any
}

func (s marketplaceSnapshotStore) LoadState(context.Context) (domainruntimeconfig.State, error) {
	return domainruntimeconfig.State{Overrides: s.overrides}, nil
}

type marketplaceViewerPermissions struct{}

func (marketplaceViewerPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"marketplace-viewer": {appaccess.PermPluginView}}, nil
}

func TestManagedMarketplaceStartupAndReconfiguration(t *testing.T) {
	const primary = "http://primary.example.com/index.json"
	const declared = "https://deployed.example.com/index.json"
	const managed = "https://team.example.com/index.json"
	base := cfgpkg.PluginMarketplaceConfig{URL: primary, Sources: []cfgpkg.PluginMarketplaceSource{{ID: "deployed", URL: declared}}}
	registry := appruntimeconfig.NewRegistry(appruntimeconfig.RegistryOptions{
		MarketplaceURL: primary, MarketplaceSources: deploymentMarketplaceSources(base.Sources),
	})
	permissions := appaccess.NewPermissionResolver(marketplaceViewerPermissions{})
	readSnapshot := func(overrides map[string]any) appruntimeconfig.Snapshot {
		t.Helper()
		service, err := appruntimeconfig.New(context.Background(), marketplaceSnapshotStore{overrides: overrides}, registry, permissions, nil)
		if err != nil {
			t.Fatal(err)
		}
		return service.Current()
	}
	// A deployment change can reserve a URL previously saved in runtime settings.
	previous := readSnapshot(map[string]any{
		appruntimeconfig.KeyMarketplaceThirdPartyURLs: []any{declared, managed},
		appruntimeconfig.KeyMarketplaceURL:            "https://example@primary.example.com/index.json",
	})
	provider, err := startupMarketplaceProvider(base, previous, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	plugins := appplugin.NewWithOptions(nil, permissions, nil, appplugin.WithMarketplaceProvider(provider))
	viewer := domainidentity.Principal{UserID: "viewer", Roles: []string{"marketplace-viewer"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checkSource := func(url string, expected error) {
		t.Helper()
		_, err := plugins.GetMarketplace(ctx, viewer, domainplugin.PluginVersionRef{PluginID: "plugin", MarketplaceURL: url})
		if !errors.Is(err, expected) {
			t.Fatalf("source %s returned %v, want %v", url, err, expected)
		}
	}
	for _, url := range []string{primary, declared, managed} {
		checkSource(url, context.Canceled)
	}
	applier := marketplaceConfigApplier{base: base, plugins: plugins}
	if !applier.Handles(appruntimeconfig.KeyMarketplaceThirdPartyURLs) {
		t.Fatal("marketplace reconfiguration does not handle managed sources")
	}
	items, err := applier.Apply(context.Background(), previous, readSnapshot(nil), []string{appruntimeconfig.KeyMarketplaceThirdPartyURLs})
	if err != nil || len(items) != 1 || items[0].Status != sohaapi.RuntimeConfigApplicationStatusApplied {
		t.Fatalf("removal result = %#v, error = %v", items, err)
	}
	checkSource(managed, apperrors.ErrInvalidArgument)
	checkSource(primary, context.Canceled)
	checkSource(declared, context.Canceled)
	corrected := readSnapshot(map[string]any{appruntimeconfig.KeyMarketplaceThirdPartyURLs: []any{managed}})
	if _, err := applier.Apply(context.Background(), readSnapshot(nil), corrected, []string{appruntimeconfig.KeyMarketplaceThirdPartyURLs}); err != nil {
		t.Fatal(err)
	}
	checkSource(managed, context.Canceled)
}
