package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceListIsolatesSources(t *testing.T) {
	var unrelatedCalls atomic.Int32
	fastRequested := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official":
			select {
			case <-fastRequested:
			case <-r.Context().Done():
				return
			}
		case "/timeout":
			unrelatedCalls.Add(1)
			<-r.Context().Done()
			return
		case "/broken":
			unrelatedCalls.Add(1)
			http.Error(w, "upstream-private-detail", http.StatusInternalServerError)
			return
		case "/malformed":
			_, _ = fmt.Fprint(w, "{")
			return
		case "/community":
			fastRequested <- struct{}{}
		}
		_, _ = fmt.Fprint(w, `{"schemaVersion":"marketplace.opensoha.io/v1","sourceId":"spoofed","plugins":[{"id":"shared","version":"1.0.0"}]}`)
	}))
	defer server.Close()
	remote := func(id string, client *http.Client) *RemoteMarketplaceProvider {
		t.Helper()
		result, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: id, URL: server.URL + "/" + id}, client)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	broken := remote("broken", server.Client())
	community := remote("community", server.Client())
	provider := NewCompositeMarketplaceProvider(
		remote("official", server.Client()),
		remote("timeout", &http.Client{Timeout: 50 * time.Millisecond}),
		broken,
		community,
		remote("malformed", server.Client()),
	)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	result, err := provider.listWithSources(ctx, domainplugin.MarketplaceFilter{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.Equal(t, "official", result.Items[0].SourceID)
	require.Equal(t, "community", result.Items[1].SourceID)
	wantSources := []sohaapi.MarketplaceSourceStatus{
		{SourceID: "official", Status: sohaapi.MarketplaceSourceStatusAvailable},
		{SourceID: "timeout", Status: sohaapi.MarketplaceSourceStatusUnavailable, Code: "source_unavailable"},
		{SourceID: "broken", Status: sohaapi.MarketplaceSourceStatusUnavailable, Code: "source_unavailable"},
		{SourceID: "community", Status: sohaapi.MarketplaceSourceStatusAvailable},
		{SourceID: "malformed", Status: sohaapi.MarketplaceSourceStatusUnavailable, Code: "source_unavailable"},
	}
	require.NotNil(t, result.Meta)
	require.Equal(t, wantSources, result.Meta.Sources)

	// An empty search response still proves that the selected catalog is available.
	result, err = provider.listWithSources(ctx, domainplugin.MarketplaceFilter{SourceID: "community", Query: "absent"})
	require.NoError(t, err)
	require.Empty(t, result.Items)
	require.NotNil(t, result.Meta)
	require.Equal(t, []sohaapi.MarketplaceSourceStatus{wantSources[3]}, result.Meta.Sources)
	require.Equal(t, int32(2), unrelatedCalls.Load(), "scoped listing fetched unrelated sources")
	if _, err = provider.Get(ctx, domainplugin.PluginVersionRef{PluginID: "shared", SourceID: "broken"}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("scoped detail lost its source error: %v", err)
	}
	if _, err = provider.FetchManifest(ctx, domainplugin.PluginVersionRef{PluginID: "shared", SourceID: "broken"}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("scoped install resolution lost its source error: %v", err)
	}
	if _, err = provider.List(ctx, domainplugin.MarketplaceFilter{SourceID: "broken"}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("scoped listing lost its source error: %v", err)
	}
	result, err = provider.listWithSources(ctx, domainplugin.MarketplaceFilter{SourceID: "unknown"})
	require.NoError(t, err)
	require.NotNil(t, result.Items)
	require.Empty(t, result.Items)
	require.Nil(t, result.Meta)
}

func TestMarketplaceListAllSourcesUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream-private-detail", http.StatusInternalServerError)
	}))
	defer server.Close()
	remote, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: "broken", URL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for name, provider := range map[string]MarketplaceProvider{
		"direct":    remote,
		"composite": NewCompositeMarketplaceProvider(remote, remote),
	} {
		t.Run(name, func(t *testing.T) {
			service := NewWithOptions(newMemoryPluginRepo(), appaccess.NewPermissionResolver(stubRolePermissions{
				"viewer": {appaccess.PermPluginView},
			}), nil, WithMarketplaceProvider(provider))
			result, err := service.ListMarketplaceWithSources(t.Context(), domainidentity.Principal{Roles: []string{"viewer"}}, domainplugin.MarketplaceFilter{})
			if !errors.Is(err, apperrors.ErrInvalidArgument) || !strings.Contains(err.Error(), "sources are unavailable") ||
				strings.Contains(err.Error(), "status 500") || result.Items != nil {
				t.Fatalf("all-unavailable marketplace list = %#v, %v", result, err)
			}
		})
	}
}

func TestMarketplaceListStartsHealthySourceAlongsideSlowSources(t *testing.T) {
	const healthyID = "healthy"
	client := &http.Client{Transport: marketplaceTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/"+healthyID {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		catalog := `{"schemaVersion":"marketplace.opensoha.io/v1","sourceId":"healthy","plugins":[{"id":"example"}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(catalog))}, nil
	})}
	var sources []MarketplaceProvider
	for _, id := range []string{"slow1", "slow2", "slow3", "slow4", "slow5", healthyID} {
		provider, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: id, URL: "https://catalog.example.com/" + id}, client)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, provider)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	result, err := NewCompositeMarketplaceProvider(sources...).listWithSources(ctx, domainplugin.MarketplaceFilter{})
	if err != nil || len(result.Items) != 1 || result.Items[0].SourceID != healthyID {
		t.Fatalf("healthy source was lost behind slow sources: %#v, %v", result, err)
	}
	if result.Meta == nil || len(result.Meta.Sources) != len(sources) {
		t.Fatalf("missing slow source diagnostics: %#v", result.Meta)
	}
}

func TestMarketplaceSuspendedReleaseRemainsVisibleAndCannotBeInstalled(t *testing.T) {
	var fallbackCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/official" {
			fallbackCalls.Add(1)
			t.Error("suspended release attempted another source or fetched its manifest")
		}
		_, _ = fmt.Fprint(w, `{
			"schemaVersion":"marketplace.opensoha.io/v1","sourceId":"official",
			"plugins":[{"id":"example.skill","version":"2.0.0","latestVersion":"2.0.0",
				"manifest":{"id":"example.skill","name":"Example","publisher":"example","type":"skill","version":"2.0.0"},
				"versions":[{"version":"1.0.0","manifestUrl":"","suspended":true},{"version":"3.0.0","manifestUrl":""}]}]
		}`)
	}))
	defer server.Close()
	official, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: "official", URL: server.URL + "/official"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: "other", URL: server.URL + "/other"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(newMemoryPluginRepo(), appaccess.NewPermissionResolver(stubRolePermissions{
		"operator": {appaccess.PermPluginView, appaccess.PermPluginInstall, appaccess.PermPluginUpgrade},
	}), nil, WithMarketplaceProvider(NewCompositeMarketplaceProvider(official, other)))
	principal := domainidentity.Principal{Roles: []string{"operator"}}
	old := domainplugin.PluginInstallRequest{PluginID: "example.skill", Version: "1.0.0"}
	ref := domainplugin.PluginVersionRef{PluginID: old.PluginID, Version: old.Version, SourceID: "official"}
	detail, err := service.GetMarketplace(t.Context(), principal, ref)
	if err != nil || len(detail.Versions) != 2 || !detail.Versions[0].Suspended {
		t.Fatalf("suspended release disappeared from metadata: %#v, %v", detail, err)
	}
	if _, err := service.Install(t.Context(), principal, old); !errors.Is(err, errMarketplaceReleaseSuspended) {
		t.Fatalf("install of suspended version = %v", err)
	}
	if _, err := service.Install(t.Context(), principal, domainplugin.PluginInstallRequest{
		PluginID: old.PluginID, SourceID: "official", Version: "3.0.0",
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("install of release with mismatched inline manifest = %v", err)
	}
	current, err := service.Install(t.Context(), principal, domainplugin.PluginInstallRequest{PluginID: old.PluginID, SourceID: "official"})
	if err != nil || current.Version != "2.0.0" {
		t.Fatalf("install of available version = %#v, %v", current, err)
	}
	if _, err := service.Upgrade(t.Context(), principal, current.ID, old); !errors.Is(err, errMarketplaceReleaseSuspended) {
		t.Fatalf("upgrade to suspended version = %v", err)
	}
	installed, err := service.GetInstalled(t.Context(), principal, current.ID)
	if err != nil || installed.Version != "2.0.0" || fallbackCalls.Load() != 0 {
		t.Fatalf("blocked upgrade affected installed snapshot or fetched another source: %#v, %v", installed, err)
	}
}
