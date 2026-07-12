package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type stubRolePermissions map[string][]string

func (s stubRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s, nil
}

type memoryPluginRepo struct {
	items map[string]domainplugin.InstalledPlugin
}

func newMemoryPluginRepo() *memoryPluginRepo {
	return &memoryPluginRepo{items: map[string]domainplugin.InstalledPlugin{}}
}

func (r *memoryPluginRepo) ListInstalled(context.Context) ([]domainplugin.InstalledPlugin, error) {
	items := make([]domainplugin.InstalledPlugin, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryPluginRepo) GetInstalled(_ context.Context, pluginID string) (domainplugin.InstalledPlugin, error) {
	item, ok := r.items[pluginID]
	if !ok {
		return domainplugin.InstalledPlugin{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryPluginRepo) UpsertInstalled(_ context.Context, item domainplugin.InstalledPlugin) (domainplugin.InstalledPlugin, error) {
	if item.InstalledAt.IsZero() {
		item.InstalledAt = time.Now().UTC()
	}
	r.items[item.ID] = item
	return item, nil
}

func (r *memoryPluginRepo) DeleteInstalled(_ context.Context, pluginID string) error {
	delete(r.items, pluginID)
	return nil
}

func boolPtr(value bool) *bool {
	return &value
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

type capturePluginAuditRecorder struct {
	entries []domainaudit.Entry
}

func (r *capturePluginAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func TestInstallRequiresPluginInstallPermission(t *testing.T) {
	service := New(newMemoryPluginRepo(), appaccess.NewPermissionResolver(stubRolePermissions{
		"viewer": {appaccess.PermPluginView},
	}), nil)

	_, err := service.Install(context.Background(), domainidentity.Principal{Roles: []string{"viewer"}}, domainplugin.PluginInstallRequest{
		PluginID: "opensoha.k8s-sre-pack",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Install error = %v, want access denied", err)
	}
}

func TestInstallStoresManifestSnapshotWithoutGrantingPermissions(t *testing.T) {
	repo := newMemoryPluginRepo()
	service := New(repo, appaccess.NewPermissionResolver(stubRolePermissions{
		"installer": {appaccess.PermPluginView, appaccess.PermPluginInstall},
	}), nil)

	item, err := service.Install(context.Background(), domainidentity.Principal{UserID: "admin", Roles: []string{"installer"}}, domainplugin.PluginInstallRequest{
		PluginID: "opensoha.k8s-sre-pack",
		Enable:   true,
	})
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if item.Status != statusEnabled {
		t.Fatalf("status = %q, want enabled", item.Status)
	}
	if item.RequestedPermissions == nil || len(item.RequestedPermissions.Required) == 0 {
		t.Fatalf("requested permissions were not snapshotted: %#v", item.RequestedPermissions)
	}
	if item.Metadata["permissionModel"] != "requested-only" {
		t.Fatalf("permission model metadata = %#v", item.Metadata)
	}
	if item.ConfiguredSecretRefs == nil {
		t.Fatalf("configured secret refs should be initialized")
	}
}

func TestInstallConnectorMarketplaceManifestRecordsSnapshotAndAudit(t *testing.T) {
	repo := newMemoryPluginRepo()
	audit := &capturePluginAuditRecorder{}
	service := New(repo, appaccess.NewPermissionResolver(stubRolePermissions{
		"installer": {appaccess.PermPluginView, appaccess.PermPluginInstall},
	}), audit)

	item, err := service.Install(context.Background(), domainidentity.Principal{
		UserID:   "admin",
		UserName: "Admin",
		Roles:    []string{"installer"},
	}, domainplugin.PluginInstallRequest{
		PluginID: "opensoha.feishu",
		Enable:   true,
	})
	expectPlugin(t, err == nil, "Install returned error: %v", err)
	expectPlugin(t, item.ID == "opensoha.feishu", "plugin id = %q", item.ID)
	expectPlugin(t, item.Type == "connector", "plugin type = %q", item.Type)
	expectPlugin(t, item.Status == statusEnabled, "plugin status = %q", item.Status)
	expectPlugin(t, item.Source == "marketplace:opensoha/feishu", "source = %q", item.Source)
	expectPlugin(t, item.SignatureStatus == "catalog", "signature status = %q", item.SignatureStatus)
	expectPlugin(t, item.ChecksumStatus == "not_provided", "checksum status = %q", item.ChecksumStatus)
	expectPlugin(t, item.Manifest.Assets != nil, "connector assets are missing")
	expectPlugin(t, len(item.Manifest.Assets.Connectors) == 1, "connector assets = %#v", item.Manifest.Assets)
	expectPlugin(t, item.Manifest.Assets.Connectors[0] == "connectors/feishu/connector.manifest.json", "connector asset = %#v", item.Manifest.Assets)
	expectPlugin(t, item.Manifest.Capabilities != nil, "connector capabilities are missing")
	expectPlugin(t, len(item.Manifest.Capabilities.Tools) == 1, "connector tools = %#v", item.Manifest.Capabilities)
	expectPlugin(t, item.Manifest.Capabilities.Tools[0] == "feishu.message.send_text", "connector tools = %#v", item.Manifest.Capabilities)
	expectPlugin(t, item.RequestedPermissions != nil, "requested permissions were not snapshotted")
	expectPlugin(t, containsString(item.RequestedPermissions.Required, appaccess.PermAIGatewayView), "view permission missing: %#v", item.RequestedPermissions.Required)
	expectPlugin(t, containsString(item.RequestedPermissions.Required, appaccess.PermAIGatewayInvoke), "invoke permission missing: %#v", item.RequestedPermissions.Required)
	expectPlugin(t, item.Metadata["permissionModel"] == "requested-only", "permission model metadata = %#v", item.Metadata)
	expectPlugin(t, item.Metadata["manifestChecksum"] != "", "manifest checksum metadata missing: %#v", item.Metadata)
	expectPlugin(t, len(audit.entries) == 1, "audit entries = %d, want 1", len(audit.entries))
	entry := audit.entries[0]
	expectPlugin(t, entry.Action == "plugin.install", "audit action = %q", entry.Action)
	expectPlugin(t, entry.Result == "success", "audit result = %q", entry.Result)
	expectPlugin(t, entry.Metadata["pluginId"] == "opensoha.feishu", "audit plugin id missing: %#v", entry.Metadata)
	expectPlugin(t, entry.Metadata["pluginType"] == "connector", "audit plugin type missing: %#v", entry.Metadata)
	expectPlugin(t, entry.Metadata["checksumStatus"] == "not_provided", "audit checksum status missing: %#v", entry.Metadata)
	expectPlugin(t, entry.Metadata["signatureStatus"] == "catalog", "audit signature status missing: %#v", entry.Metadata)
	snapshot, ok := entry.Metadata["manifestSnapshot"].(domainplugin.PluginManifest)
	if !ok {
		t.Fatalf("audit manifest snapshot has unexpected type: %#v", entry.Metadata["manifestSnapshot"])
	}
	expectPlugin(t, snapshot.ID == "opensoha.feishu", "snapshot id = %q", snapshot.ID)
	expectPlugin(t, snapshot.Type == "connector", "snapshot type = %q", snapshot.Type)
	expectPlugin(t, snapshot.Assets != nil, "snapshot assets missing")
	expectPlugin(t, snapshot.Capabilities != nil, "snapshot capabilities missing")
	expectPlugin(t, snapshot.Assets.Connectors[0] == "connectors/feishu/connector.manifest.json", "snapshot connector drifted: %#v", snapshot)
	expectPlugin(t, snapshot.Capabilities.Tools[0] == "feishu.message.send_text", "snapshot tool drifted: %#v", snapshot)
}

func expectPlugin(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

func TestInstallRemoteMarketplaceManifestVerifiesChecksumAndRegistersExtensions(t *testing.T) {
	manifestRaw := []byte(`{"id":"opensoha.remote-skill","name":"Remote Skill","version":"0.2.0","publisher":"community","type":"skill-pack","runtime":{"mode":"manifest-only"},"extensionPoints":{"ai":{"skillPacks":[{"id":"remote-skill","label":"Remote Skill","permissionKeys":["ai.gateway.view"]}]}}}`)
	manifestChecksum := checksumBytes(manifestRaw)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"schemaVersion":"marketplace.opensoha.io/v1",
				"generatedAt":"2026-06-28T00:00:00Z",
				"sourceId":"community",
				"sourceUrl":%q,
				"plugins":[{
					"id":"opensoha.remote-skill",
					"name":"Remote Skill",
					"version":"0.2.0",
					"publisher":"community",
					"type":"skill-pack",
					"source":"marketplace:community/remote-skill",
					"riskLevel":"read",
					"latestVersion":"0.2.0",
					"manifest":{"id":"opensoha.remote-skill","name":"Remote Skill","version":"0.2.0","publisher":"community","type":"skill-pack"},
					"versions":[{"version":"0.2.0","manifestUrl":%q,"checksum":%q,"signature":"sig:test"}]
				}]
				}`, server.URL+"/index.json", server.URL+"/plugin.manifest.json", manifestChecksum)
		case "/plugin.manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(manifestRaw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	remoteProvider, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: "community", URL: server.URL + "/index.json"}, server.Client())
	if err != nil {
		t.Fatalf("NewRemoteMarketplaceProvider returned error: %v", err)
	}
	service := NewWithOptions(
		newMemoryPluginRepo(),
		appaccess.NewPermissionResolver(stubRolePermissions{
			"installer": {appaccess.PermPluginView, appaccess.PermPluginInstall, appaccess.PermPlatformExtensionsView},
		}),
		nil,
		WithMarketplaceProvider(NewCompositeMarketplaceProvider(NewDefaultMarketplaceProvider(), remoteProvider)),
	)

	item, err := service.Install(context.Background(), domainidentity.Principal{Roles: []string{"installer"}}, domainplugin.PluginInstallRequest{
		PluginID: "opensoha.remote-skill",
		SourceID: "community",
		Version:  "0.2.0",
		Enable:   true,
	})
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if item.Status != statusEnabled || item.ChecksumStatus != "verified" || item.SignatureStatus != "verified" {
		t.Fatalf("unexpected remote install state: status=%q checksum=%q signature=%q", item.Status, item.ChecksumStatus, item.SignatureStatus)
	}
	if item.Metadata["sourceId"] != "community" || item.Metadata["marketplaceUrl"] != server.URL+"/index.json" {
		t.Fatalf("source metadata not recorded: %#v", item.Metadata)
	}
	extensions, err := service.ListExtensions(context.Background(), domainidentity.Principal{Roles: []string{"installer"}}, "ai")
	if err != nil {
		t.Fatalf("ListExtensions returned error: %v", err)
	}
	if len(extensions) != 1 || extensions[0].Point != "ai.skillPacks" || extensions[0].PluginID != "opensoha.remote-skill" {
		t.Fatalf("remote extension not registered: %#v", extensions)
	}
}

func TestAdHocMarketplaceRejectsPrivateURLs(t *testing.T) {
	service := New(newMemoryPluginRepo(), appaccess.NewPermissionResolver(stubRolePermissions{
		"viewer": {appaccess.PermPluginView},
	}), nil)
	tests := []struct {
		name string
		url  string
	}{
		{name: "loopback ipv4", url: "http://127.0.0.1/marketplace/index.json"},
		{name: "loopback hostname", url: "http://localhost/marketplace/index.json"},
		{name: "metadata service", url: "http://169.254.169.254/latest/meta-data"},
		{name: "private network", url: "https://10.0.0.10/marketplace/index.json"},
		{name: "carrier grade nat", url: "http://100.64.0.10/marketplace/index.json"},
		{name: "documentation network", url: "https://192.0.2.10/marketplace/index.json"},
		{name: "ipv6 unique local", url: "https://[fc00::1]/marketplace/index.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.ListMarketplace(context.Background(), domainidentity.Principal{Roles: []string{"viewer"}}, domainplugin.MarketplaceFilter{
				MarketplaceURL: tt.url,
			})
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("ListMarketplace error = %v, want invalid argument", err)
			}
		})
	}
}

func TestRequiredSecretsKeepPluginPendingUntilConfigured(t *testing.T) {
	repo := newMemoryPluginRepo()
	service := New(repo, appaccess.NewPermissionResolver(stubRolePermissions{
		"installer": {appaccess.PermPluginInstall, appaccess.PermPluginView},
		"manager": {
			appaccess.PermPluginManage,
			appaccess.PermPluginConfigureSecrets,
			appaccess.PermPlatformExtensionsView,
		},
	}), nil)
	manifest := domainplugin.PluginManifest{
		ID:        "opensoha.secured-skill",
		Name:      "Secured Skill",
		Version:   "0.1.0",
		Publisher: "community",
		Type:      "skill-pack",
		Runtime:   &domainplugin.PluginRuntimeSpec{Mode: "manifest-only"},
		Secrets: &struct {
			Required []sohaapi.PluginSecretRequirement `json:"required,omitempty"`
		}{
			Required: []sohaapi.PluginSecretRequirement{
				{Name: "apiToken", Required: true},
			},
		},
		ExtensionPoints: &domainplugin.PluginExtensionPoints{
			AI: &sohaapi.PluginAIExtensions{
				SkillPacks: []sohaapi.PluginExtensionContribution{
					{ID: "secured-skill", Label: "Secured Skill"},
				},
			},
		},
	}

	item, err := service.Install(context.Background(), domainidentity.Principal{Roles: []string{"installer"}}, domainplugin.PluginInstallRequest{
		Manifest: &manifest,
		Enable:   true,
	})
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if item.Status != statusPendingConfig {
		t.Fatalf("status = %q, want pending_config", item.Status)
	}
	extensions, err := service.ListExtensions(context.Background(), domainidentity.Principal{Roles: []string{"manager"}}, "ai")
	if err != nil {
		t.Fatalf("ListExtensions returned error: %v", err)
	}
	if len(extensions) != 0 {
		t.Fatalf("pending plugin should not register extensions: %#v", extensions)
	}

	item, err = service.Configure(context.Background(), domainidentity.Principal{Roles: []string{"manager"}}, manifest.ID, domainplugin.PluginConfigRequest{
		Enabled:    boolPtr(true),
		SecretRefs: map[string]string{"apiToken": "secret://plugins/secured-skill/api-token"},
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	if item.Status != statusEnabled {
		t.Fatalf("status = %q, want enabled", item.Status)
	}
	extensions, err = service.ListExtensions(context.Background(), domainidentity.Principal{Roles: []string{"manager"}}, "ai")
	if err != nil {
		t.Fatalf("ListExtensions returned error: %v", err)
	}
	if len(extensions) != 1 || extensions[0].ID != "secured-skill" {
		t.Fatalf("configured plugin extension not registered: %#v", extensions)
	}
}

func TestConfigureSecretRefsRequiresDedicatedPermission(t *testing.T) {
	repo := newMemoryPluginRepo()
	now := time.Now().UTC()
	repo.items["opensoha.k8s-sre-pack"] = domainplugin.InstalledPlugin{
		ID:             "opensoha.k8s-sre-pack",
		Name:           "K8s SRE Pack",
		Version:        "0.1.0",
		Publisher:      "opensoha",
		Type:           "skill-pack",
		Status:         statusDisabled,
		Source:         "test",
		Manifest:       domainplugin.PluginManifest{ID: "opensoha.k8s-sre-pack", Name: "K8s SRE Pack", Version: "0.1.0", Publisher: "opensoha", Type: "skill-pack"},
		ChecksumStatus: "not_provided",
		InstalledBy:    "admin",
		InstalledAt:    now,
		UpdatedAt:      now,
	}
	service := New(repo, appaccess.NewPermissionResolver(stubRolePermissions{
		"manager": {appaccess.PermPluginManage},
	}), nil)

	_, err := service.Configure(context.Background(), domainidentity.Principal{Roles: []string{"manager"}}, "opensoha.k8s-sre-pack", domainplugin.PluginConfigRequest{
		SecretRefs: map[string]string{"kubeconfig": "secret://k8s/default"},
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Configure error = %v, want access denied", err)
	}
}

func TestConfigureExplicitEnabledFalseDisablesPlugin(t *testing.T) {
	repo := newMemoryPluginRepo()
	now := time.Now().UTC()
	repo.items["opensoha.k8s-sre-pack"] = domainplugin.InstalledPlugin{
		ID:             "opensoha.k8s-sre-pack",
		Name:           "K8s SRE Pack",
		Version:        "0.1.0",
		Publisher:      "opensoha",
		Type:           "skill-pack",
		Status:         statusEnabled,
		Source:         "test",
		Manifest:       domainplugin.PluginManifest{ID: "opensoha.k8s-sre-pack", Name: "K8s SRE Pack", Version: "0.1.0", Publisher: "opensoha", Type: "skill-pack"},
		ChecksumStatus: "not_provided",
		InstalledBy:    "admin",
		InstalledAt:    now,
		UpdatedAt:      now,
		EnabledAt:      &now,
	}
	service := New(repo, appaccess.NewPermissionResolver(stubRolePermissions{
		"manager": {appaccess.PermPluginManage},
	}), nil)

	item, err := service.Configure(context.Background(), domainidentity.Principal{Roles: []string{"manager"}}, "opensoha.k8s-sre-pack", domainplugin.PluginConfigRequest{
		Enabled: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	if item.Status != statusDisabled {
		t.Fatalf("status = %q, want disabled", item.Status)
	}
	if item.DisabledAt == nil {
		t.Fatalf("disabledAt should be set")
	}
	if item.EnabledAt != nil {
		t.Fatalf("enabledAt should be cleared")
	}
}
