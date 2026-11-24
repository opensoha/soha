package plugin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoplugin "github.com/opensoha/soha/internal/repository/plugin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type installerPermissions struct{}

func (installerPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"tester": {appaccess.PermPluginInstall, appaccess.PermPluginLifecycle}}, nil
}

func TestConcurrentPluginInstallDoesNotReplaceExistingStateWithPostgres(t *testing.T) {
	repository, pluginID := newPluginIntegrationRepository(t)
	manifest := domainplugin.PluginManifest{ID: pluginID, Name: "Concurrent Installation", Version: "1.0.0", Publisher: "test", Type: "skill-pack"}
	operator := domainidentity.Principal{UserID: "installer", Roles: []string{"tester"}}
	localSource := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"schemaVersion":"marketplace.opensoha.io/v1","sourceId":"test-catalog","plugins":[{"id":"` + pluginID + `","version":"1.0.0","manifest":{"id":"` + pluginID + `","name":"Concurrent Installation","version":"1.0.0","publisher":"test","type":"skill-pack"}}]}`))
	}))
	t.Cleanup(localSource.Close)
	provider, err := appplugin.NewRemoteMarketplaceProvider(appplugin.MarketplaceSource{ID: "test-catalog", URL: localSource.URL}, localSource.Client())
	if err != nil {
		t.Fatal(err)
	}
	service := appplugin.NewWithOptions(repository, appaccess.NewPermissionResolver(installerPermissions{}), nil, appplugin.WithMarketplaceProvider(provider))
	request := domainplugin.PluginInstallRequest{Manifest: &manifest}

	start := make(chan struct{})
	results := make(chan error, 8)
	for range cap(results) {
		go func() {
			<-start
			_, err := service.Install(t.Context(), operator, request)
			results <- err
		}()
	}
	close(start)
	for range cap(results) {
		if err := <-results; err != nil {
			t.Errorf("concurrent duplicate install: %v", err)
		}
	}
	enabled, err := service.Enable(t.Context(), operator, pluginID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(t.Context(), operator, domainplugin.PluginInstallRequest{PluginID: pluginID}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("cross-source install error = %v, want conflict", err)
	}
	newManifest := manifest
	newManifest.Version = "2.0.0"
	if _, err := service.Install(t.Context(), operator, domainplugin.PluginInstallRequest{Manifest: &newManifest}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("different version install error = %v, want conflict", err)
	}
	repeated, err := service.Install(t.Context(), operator, request)
	if err != nil || repeated.Status != "enabled" || repeated.Source != enabled.Source || !repeated.UpdatedAt.Equal(enabled.UpdatedAt) {
		t.Fatalf("repeated install changed existing state: %#v, error %v", repeated, err)
	}
}

func TestPluginSourceAndStaleUpgradeWithPostgres(t *testing.T) {
	repository, pluginID := newPluginIntegrationRepository(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	source := domainplugin.PluginSourceInfo{
		Kind:                domainplugin.SourceKindGitHubRepo,
		CanonicalSource:     "https://github.com/open-source/test-plugin",
		PinnedRef:           "v1.0.0",
		CommitSha:           "0123456789abcdef0123456789abcdef01234567",
		RawManifestChecksum: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	initial := domainplugin.InstalledPlugin{
		ID: pluginID, Name: "Source Test Plugin", Version: "1.0.0", Publisher: "test", Type: "skill-pack",
		Status: "disabled", Source: source.CanonicalSource, SourceInfo: &source, InstalledBy: "installer",
		InstalledAt: now, UpdatedAt: now, ChecksumStatus: "verified", SignatureStatus: "unverified",
		Manifest: domainplugin.PluginManifest{ID: pluginID, Name: "Source Test Plugin", Version: "1.0.0", Publisher: "test", Type: "skill-pack"},
	}
	installed, created, err := repository.CreateInstalled(ctx, initial, nil)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, &source, installed.SourceInfo)

	expectStaleUpgrade := func(expected domainplugin.InstalledPlugin) {
		t.Helper()
		upgrade := expected
		upgrade.Version = "2.0.0"
		upgrade.Manifest.Version = upgrade.Version
		newSource := source
		newSource.PinnedRef = "v2.0.0"
		upgrade.SourceInfo = &newSource
		called := false
		_, err := repository.UpdateInstalled(ctx, expected, upgrade, func(context.Context, *domainplugin.InstalledPlugin) error {
			called = true
			return nil
		})
		require.ErrorIs(t, err, apperrors.ErrConflict)
		require.False(t, called, "stale upgrade ran its callback")
	}

	configuration := installed
	configuration.ConfiguredSecretRefs = map[string]string{"kubeconfig": "secret://test"}
	configured, err := repository.UpdateInstalled(ctx, installed, configuration, nil)
	require.NoError(t, err)
	require.True(t, configured.UpdatedAt.Equal(installed.UpdatedAt.Add(time.Microsecond)),
		"configure revision = %v, previous revision = %v", configured.UpdatedAt, installed.UpdatedAt)
	expectStaleUpgrade(installed)
	removed, err := repository.RemoveInstalled(ctx, pluginID, func(context.Context, domainplugin.InstalledPlugin) error { return nil })
	require.NoError(t, err)
	require.Equal(t, &source, removed.SourceInfo)
	require.Equal(t, initial.Version, removed.Version)
	require.Equal(t, "secret://test", removed.ConfiguredSecretRefs["kubeconfig"])
	expectStaleUpgrade(configured)

	replacement := initial
	replacement.InstalledAt = configured.UpdatedAt
	replacement.UpdatedAt = configured.UpdatedAt
	reinstalled, created, err := repository.CreateInstalled(ctx, replacement, nil)
	require.NoError(t, err)
	require.True(t, created)
	require.True(t, reinstalled.UpdatedAt.Equal(configured.UpdatedAt), "replacement revision = %v", reinstalled.UpdatedAt)
	expectStaleUpgrade(configured)
	stored, err := repository.GetInstalled(ctx, pluginID)
	require.NoError(t, err)
	require.True(t, stored.InstalledAt.Equal(replacement.InstalledAt))
	require.Equal(t, replacement.Version, stored.Version)
	require.Equal(t, &source, stored.SourceInfo)
}

func newPluginIntegrationRepository(t *testing.T) (*repoplugin.Repository, string) {
	t.Helper()
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("requires an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port,
		Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable",
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	if err := store.MigrateFromFile(t.Context(), filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	id := "test." + uuid.NewString()
	t.Cleanup(func() { _ = store.DB().Exec("DELETE FROM installed_plugins WHERE id = ?", id).Error })
	return repoplugin.New(store.DB()), id
}
