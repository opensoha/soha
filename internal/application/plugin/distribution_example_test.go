package plugin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

func TestStaticDistributionExampleLifecycleWithoutCloud(t *testing.T) {
	const pluginID = "example.distribution-smoke"
	const expectedDigest = "sha256:2152e97127a9fbbc7f6f3663a78196fdbd68d006d438dc0ee0b23816cd668ecb"
	exampleDirectory := filepath.Join("..", "..", "..", "contracts", "examples", "plugin-distribution")
	staticSite := httptest.NewServer(http.FileServer(http.Dir(exampleDirectory)))
	t.Cleanup(staticSite.Close)

	catalogURL := staticSite.URL + "/marketplace/index.json"
	provider, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: "opensoha-local", URL: catalogURL}, staticSite.Client())
	if err != nil {
		t.Fatal(err)
	}
	permissions := appaccess.NewPermissionResolver(stubRolePermissions{
		"tester": {appaccess.PermPluginView, appaccess.PermPluginInstall, appaccess.PermPluginLifecycle, appaccess.PermPlatformExtensionsView},
	})
	service := NewWithOptions(newMemoryPluginRepo(), permissions, nil, WithMarketplaceProvider(provider))
	operator := domainidentity.Principal{UserID: "local-smoke-test", Roles: []string{"tester"}}
	items, err := service.ListMarketplace(t.Context(), operator, domainplugin.MarketplaceFilter{})
	if err != nil || len(items) != 1 || items[0].ID != pluginID {
		t.Fatalf("static marketplace returned %v, %v", items, err)
	}
	installed, err := service.Install(t.Context(), operator, domainplugin.PluginInstallRequest{
		PluginID: pluginID, Version: "0.1.0", ExpectedChecksum: expectedDigest,
	})
	if err != nil || installed.Status != statusDisabled || installed.ChecksumStatus != "verified" {
		t.Fatalf("static installation returned status %q, checksum %q, error %v", installed.Status, installed.ChecksumStatus, err)
	}

	staticSite.Close()
	installedItems, err := service.ListInstalled(t.Context(), operator)
	if err != nil || len(installedItems) != 1 || installedItems[0].ID != pluginID {
		t.Fatalf("installed list with offline directory returned %v, %v", installedItems, err)
	}
	if _, err := service.Enable(t.Context(), operator, pluginID); err != nil {
		t.Fatal(err)
	}
	extensions, err := service.ListExtensions(t.Context(), operator, "ai")
	if err != nil || len(extensions) != 1 || extensions[0].PluginID != pluginID || extensions[0].Point != "ai.skillPacks" {
		t.Fatalf("extension registry after enabling returned %v, %v", extensions, err)
	}
	if _, err := service.Disable(t.Context(), operator, pluginID); err != nil {
		t.Fatal(err)
	}
	extensions, err = service.ListExtensions(t.Context(), operator, "ai")
	if err != nil || len(extensions) != 0 {
		t.Fatalf("extension registry after disabling returned %v, %v", extensions, err)
	}
}
