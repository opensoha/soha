package plugin

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceRepositoryRegistrationBindsPreviewAndConfirmation(t *testing.T) {
	const pluginID = "example.distribution-smoke"
	const repoURL = "https://github.com/example/demo"
	const sourceID = "official"
	const registration = `{"id":"example.distribution-smoke","name":"Distribution Smoke Pack","publisher":"example","type":"skill-pack","repositoryUrl":"%s"}`
	var registeredURL atomic.Value
	registeredURL.Store(repoURL)
	var hiddenCatalogReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/legacy.json" {
			hiddenCatalogReads.Add(1)
			_, _ = fmt.Fprint(w, `{"schemaVersion":"marketplace.opensoha.io/v1","sourceId":"official","plugins":[{"id":"example.distribution-smoke","version":"0.1.0","repositoryUrl":"https://github.com/forged/river"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"schemaVersion":"marketplace.opensoha.io/repositories/v1","sourceId":"untrusted-claim","repositories":[`)
		_, _ = fmt.Fprintf(w, registration, registeredURL.Load())
		_, _ = fmt.Fprint(w, `]}`)
	}))
	defer server.Close()
	provider := func(path string, legacy bool) *RemoteMarketplaceProvider {
		t.Helper()
		result, err := NewRemoteMarketplaceProvider(MarketplaceSource{URL: server.URL + path, ID: sourceID, Official: true, Legacy: legacy}, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	raw := distributionTestManifest(t)
	upstream := &fixtureRepository{source: repoURL, commit: strings.Repeat("a", 40), manifest: raw}
	store := newMemoryPluginRepo()
	service := NewWithOptions(store, appaccess.NewPermissionResolver(stubRolePermissions{
		"operator": {appaccess.PermPluginView, appaccess.PermPluginInstall, appaccess.PermPluginUpgrade},
	}), nil, WithMarketplaceProvider(NewCompositeMarketplaceProvider(provider("/repositories.json", false), provider("/legacy.json", true))),
		WithRepositoryDistribution(upstream, bytes.Repeat([]byte{9}, 32)))
	ctx := t.Context()
	actor := domainidentity.Principal{UserID: "operator", Roles: []string{"operator"}}
	items, err := service.ListMarketplace(ctx, actor, domainplugin.MarketplaceFilter{SourceID: sourceID})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].SourceInfo)
	require.Equal(t, repoURL, items[0].RepositoryURL)
	require.Empty(t, items[0].Version)
	require.Equal(t, sourceID, items[0].SourceID)
	require.True(t, items[0].SourceInfo.Official)
	require.Zero(t, upstream.resolves, "marketplace home should only read registration metadata")
	require.Zero(t, hiddenCatalogReads.Load(), "marketplace home should read only visible registrations")
	if _, err := service.Install(ctx, actor, domainplugin.PluginInstallRequest{PluginID: pluginID, SourceID: sourceID, Version: "0.1.0"}); !errors.Is(err, errMarketplaceRepositoryPreviewRequired) {
		t.Fatalf("legacy install bypassed the dynamic preview or fell back to hidden legacy catalog: %v", err)
	}
	legacy, err := service.GetMarketplace(ctx, actor, domainplugin.PluginVersionRef{PluginID: pluginID, MarketplaceURL: server.URL + "/legacy.json"})
	if err != nil || legacy.RepositoryURL != "" || hiddenCatalogReads.Load() != 1 {
		t.Fatalf("explicit legacy lookup or claim stripping failed: %#v, %v", legacy, err)
	}
	input := domainplugin.PluginRepositoryPreviewRequest{PluginID: pluginID, MarketplaceSourceID: sourceID, RepositoryURL: "https://github.com/attacker/demo", Ref: "refs/tags/v0.1.0"}
	if _, err := service.PreviewRepository(ctx, actor, input); !errors.Is(err, errMarketplaceRepositoryMismatch) || upstream.resolves != 0 {
		t.Fatalf("unregistered repository was resolved: %v", err)
	}
	input.RepositoryURL = repoURL
	input.Ref = "refs/tags/v0.2.0"
	if _, err := service.PreviewRepository(ctx, actor, input); !errors.Is(err, apperrors.ErrInvalidArgument) || len(store.items) != 0 {
		t.Fatalf("preview accepted a release tag with a different manifest version: %v", err)
	}
	input.Ref = "refs/tags/v0.1.0"
	preview, err := service.PreviewRepository(ctx, actor, input)
	require.NoError(t, err)
	require.True(t, preview.SourceInfo.Official)
	require.Equal(t, repoURL, preview.SourceInfo.CanonicalSource)
	require.Empty(t, store.items, "dynamic preview should be read-only")
	confirm := domainplugin.PluginRepositoryInstallRequest{PreviewToken: preview.PreviewToken}
	registeredURL.Store("https://github.com/attacker/demo")
	reads := upstream.fetches
	if _, err := service.InstallRepository(ctx, actor, confirm); !errors.Is(err, apperrors.ErrConflict) || upstream.fetches != reads || len(store.items) != 0 {
		t.Fatalf("confirmation ignored a changed registration: %v", err)
	}
	registeredURL.Store(repoURL)
	installed, err := service.InstallRepository(ctx, actor, confirm)
	require.NoError(t, err)
	requireMarketplaceRegistrationProvenance(t, installed, server.URL+"/repositories.json", repoURL, upstream.commit)
	upstream.commit = strings.Repeat("b", 40)
	upstream.manifest = bytes.Replace(raw, []byte(`"version": "0.1.0"`), []byte(`"version": "0.2.0"`), 1)
	input.Ref = "refs/tags/v0.2.0"
	direct, err := service.PreviewRepository(ctx, actor, domainplugin.PluginRepositoryPreviewRequest{RepositoryURL: repoURL, Ref: input.Ref})
	require.NoError(t, err)
	require.False(t, direct.SourceInfo.Official, "direct preview received official identity")
	if _, err := service.UpgradeRepository(ctx, actor, pluginID, domainplugin.PluginRepositoryUpgradeRequest{PreviewToken: direct.PreviewToken}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("direct proof could upgrade a marketplace installation: %v", err)
	}
	preview, err = service.PreviewRepository(ctx, actor, input)
	require.NoError(t, err)
	updated, err := service.UpgradeRepository(ctx, actor, pluginID, domainplugin.PluginRepositoryUpgradeRequest{PreviewToken: preview.PreviewToken})
	require.NoError(t, err)
	require.Equal(t, "0.2.0", updated.Version)
	require.WithinDuration(t, installed.InstalledAt, updated.InstalledAt, 0)
	require.True(t, samePluginSource(updated.SourceInfo, installed.SourceInfo), "dynamic upgrade changed source identity")
}

func requireMarketplaceRegistrationProvenance(t *testing.T, installed domainplugin.InstalledPlugin, catalogURL, repoURL, commit string) {
	t.Helper()
	require.NotNil(t, installed.SourceInfo)
	require.Equal(t, domainplugin.SourceKindMarketplace, installed.SourceInfo.Kind)
	require.Equal(t, catalogURL, installed.SourceInfo.CanonicalSource)
	require.Equal(t, repoURL, installed.SourceInfo.RepositoryURL)
	require.Equal(t, commit, installed.SourceInfo.CommitSha)
	require.True(t, installed.SourceInfo.Official)
	require.Equal(t, "0.1.0", installed.Version)
}

func distributionTestManifest(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../contracts/examples/plugin-distribution/plugin.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
