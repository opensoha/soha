package plugin

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

const updatesRepositoryURL = "https://github.com/example/demo"

type repositoryUpdatesFixture struct {
	listCalls  atomic.Int32
	resolves   atomic.Int32
	branchPage atomic.Int64
	manifest   []byte
}

func (f *repositoryUpdatesFixture) ListReleases(_ context.Context, repo string, _ int) (domainplugin.PluginRepositoryRefList, error) {
	f.listCalls.Add(1)
	if repo != updatesRepositoryURL {
		return domainplugin.PluginRepositoryRefList{}, apperrors.ErrServiceUnavailable
	}
	return domainplugin.PluginRepositoryRefList{Refs: []domainplugin.PluginRepositoryRef{
		{Name: "v0.2.0", ReleaseURL: updatesRepositoryURL + "/releases/tag/v0.2.0"},
		{Name: "v0.3.0"}, {Name: "v0.4.0"}, {Name: "v0.1.0"}, {Name: "v9.0.0", Prerelease: true}, {Name: "latest"},
	}}, nil
}

func (f *repositoryUpdatesFixture) ListBranches(_ context.Context, _ string, page int) (domainplugin.PluginRepositoryRefList, error) {
	f.branchPage.Store(int64(page))
	return domainplugin.PluginRepositoryRefList{}, nil
}

func (f *repositoryUpdatesFixture) Resolve(_ context.Context, repo, ref string) (string, string, string, error) {
	f.resolves.Add(1)
	if repo != updatesRepositoryURL {
		return "", "", "", apperrors.ErrNotFound
	}
	var digit string
	switch ref {
	case "refs/tags/v0.2.0":
		digit = "2"
	case "refs/tags/v0.3.0":
		digit = "3"
	case "refs/tags/v0.4.0":
		digit = "4"
	default:
		return "", "", "", apperrors.ErrNotFound
	}
	return repo, ref, strings.Repeat(digit, 40), nil
}

func (f *repositoryUpdatesFixture) FetchManifest(_ context.Context, _ string, commit string) ([]byte, error) {
	var version string
	switch commit[0] {
	case '2', '3':
		version = `"version": "0.2.0"`
	case '4':
		version = `"version": "0.4.0", "compatibility": {"soha": ">=999.0.0"}`
	default:
		return nil, apperrors.ErrNotFound
	}
	return bytes.Replace(f.manifest, []byte(`"version": "0.1.0"`), []byte(version), 1), nil
}

func TestRepositoryDiscoveryRequiresViewAndReturnsCompatibleReadOnlyUpdates(t *testing.T) {
	const pluginID = "example.distribution-smoke"
	store := newMemoryPluginRepo()
	installed := domainplugin.InstalledPlugin{ID: pluginID, Version: "0.1.0", Status: statusEnabled,
		SourceInfo: &domainplugin.PluginSourceInfo{Kind: domainplugin.SourceKindGitHubRepo, CanonicalSource: updatesRepositoryURL}}
	store.items[pluginID] = installed
	store.items["unhealthy"] = domainplugin.InstalledPlugin{ID: "unhealthy", Version: "0.1.0",
		SourceInfo: &domainplugin.PluginSourceInfo{Kind: domainplugin.SourceKindGitHubRepo, CanonicalSource: "https://github.com/example/unhealthy"}}
	store.items["static-old"] = domainplugin.InstalledPlugin{ID: "static-old", Version: "0.1.0",
		SourceInfo: &domainplugin.PluginSourceInfo{Kind: domainplugin.SourceKindMarketplace, CanonicalSource: "https://example.net/legacy.json"}}
	upstream := &repositoryUpdatesFixture{manifest: distributionTestManifest(t)}
	service := NewWithOptions(store, appaccess.NewPermissionResolver(stubRolePermissions{"viewer": {appaccess.PermPluginView}}), nil,
		WithRepositoryDistribution(upstream, bytes.Repeat([]byte{7}, 32)))
	ctx := t.Context()
	noAccess := domainidentity.Principal{UserID: "unprivileged"}
	viewer := domainidentity.Principal{UserID: "viewer", Roles: []string{"viewer"}}
	refRequest := domainplugin.PluginRepositoryRefsRequest{RepositoryURL: updatesRepositoryURL, Kind: "release"}
	if _, err := service.ListRepositoryRefs(ctx, noAccess, refRequest); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("refs without plugin.view: %v", err)
	}
	if _, err := service.ListUpdates(ctx, noAccess); !errors.Is(err, apperrors.ErrAccessDenied) || upstream.listCalls.Load() != 0 {
		t.Fatalf("updates without plugin.view reached upstream: %v", err)
	}
	invalid := 0
	refRequest.Page = &invalid
	if _, err := service.ListRepositoryRefs(ctx, viewer, refRequest); !errors.Is(err, apperrors.ErrInvalidArgument) || upstream.listCalls.Load() != 0 {
		t.Fatalf("invalid page reached upstream: %v", err)
	}
	page := 2
	refRequest.Kind, refRequest.Page = "branch", &page
	refs, err := service.ListRepositoryRefs(ctx, viewer, refRequest)
	require.NoError(t, err)
	require.Equal(t, int64(2), upstream.branchPage.Load())
	require.NotNil(t, refs.Refs)
	require.Empty(t, refs.Refs)
	updates, err := service.ListUpdates(ctx, viewer)
	require.NoError(t, err)
	require.Len(t, updates, 1, "discovery should ignore incompatible, mismatched, prerelease, unrelated and unavailable candidates")
	require.Equal(t, pluginID, updates[0].PluginID)
	require.Equal(t, "refs/tags/v0.2.0", updates[0].Ref)
	require.Equal(t, "0.2.0", updates[0].TargetVersion)
	require.Equal(t, installed.Version, updates[0].InstalledVersion)
	require.Equal(t, updatesRepositoryURL, updates[0].RepositoryURL)
	require.Equal(t, int32(2), upstream.listCalls.Load())
	require.Equal(t, int32(3), upstream.resolves.Load())
	got := store.items[pluginID]
	require.Equal(t, installed.Version, got.Version)
	require.Equal(t, installed.Status, got.Status)
	require.Same(t, installed.SourceInfo, got.SourceInfo)
	before := upstream.listCalls.Load()
	updates, err = service.ListUpdates(ctx, viewer, "uninstalled", pluginID)
	if err != nil || len(updates) != 1 || updates[0].PluginID != pluginID || upstream.listCalls.Load() != before+1 {
		t.Fatalf("scoped discovery queried an unrelated repository: %#v, %v", updates, err)
	}
	tooMany := make([]string, maxRepositoryUpdatePlugins+1)
	for index := range tooMany {
		tooMany[index] = pluginID
	}
	if _, err := service.ListUpdates(ctx, viewer, tooMany...); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("oversize update request: %v", err)
	}
	if _, err := service.ListUpdates(ctx, viewer, " "); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("blank plugin selector: %v", err)
	}
	delete(store.items, pluginID)
	if _, err := service.ListUpdates(ctx, viewer); !errors.Is(err, errRepositoryUpdatesUnavailable) {
		t.Fatalf("all eligible repositories unavailable should surface an outage: %v", err)
	}
	delete(store.items, "unhealthy")
	if updates, err := service.ListUpdates(ctx, viewer); err != nil || updates == nil || len(updates) != 0 {
		t.Fatalf("no eligible repositories should return an empty successful result: %#v, %v", updates, err)
	}
}
