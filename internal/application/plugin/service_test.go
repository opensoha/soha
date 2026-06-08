package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
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
