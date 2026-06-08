package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryUpsertInstalledPersistsManifestSnapshot(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer sqlDB.Close()

	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	item := domainplugin.InstalledPlugin{
		ID:              "opensoha.k8s-sre-pack",
		Name:            "K8s SRE Pack",
		Version:         "0.1.0",
		Publisher:       "opensoha",
		Type:            "skill-pack",
		Status:          "enabled",
		Source:          "marketplace:opensoha/k8s-sre-pack",
		Manifest:        domainplugin.PluginManifest{ID: "opensoha.k8s-sre-pack", Name: "K8s SRE Pack", Version: "0.1.0", Publisher: "opensoha", Type: "skill-pack"},
		ChecksumStatus:  "verified",
		SignatureStatus: "catalog",
		RequestedPermissions: &domainplugin.PluginPermissionRequest{
			Required: []string{"ai.gateway.view", "ai.gateway.invoke"},
			Domain:   []string{"workspace.resource.view"},
		},
		ConfiguredSecretRefs: map[string]string{"kubeconfig": "secret://k8s/default"},
		InstalledBy:          "admin",
		InstalledAt:          now,
		UpdatedAt:            now,
		EnabledAt:            &now,
		Metadata:             map[string]any{"permissionModel": "requested-only"},
	}

	mock.ExpectExec(`(?s)INSERT INTO installed_plugins .*ON CONFLICT \(id\) DO UPDATE SET`).
		WithArgs(
			item.ID,
			item.Name,
			item.Version,
			item.Publisher,
			item.Type,
			item.Status,
			item.Source,
			`{"id":"opensoha.k8s-sre-pack","name":"K8s SRE Pack","version":"0.1.0","publisher":"opensoha","type":"skill-pack"}`,
			item.ChecksumStatus,
			item.SignatureStatus,
			`{"required":["ai.gateway.view","ai.gateway.invoke"],"domain":["workspace.resource.view"]}`,
			`{"kubeconfig":"secret://k8s/default"}`,
			item.InstalledBy,
			item.InstalledAt,
			item.UpdatedAt,
			item.EnabledAt,
			item.DisabledAt,
			`{"permissionModel":"requested-only"}`,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT id, name, version, publisher, type, status, source, manifest, checksum_status, signature_status,\s+requested_permissions, configured_secret_refs, installed_by, installed_at, updated_at,\s+enabled_at, disabled_at, metadata\s+FROM installed_plugins\s+WHERE id = .*LIMIT 1`).
		WithArgs(item.ID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"name",
			"version",
			"publisher",
			"type",
			"status",
			"source",
			"manifest",
			"checksum_status",
			"signature_status",
			"requested_permissions",
			"configured_secret_refs",
			"installed_by",
			"installed_at",
			"updated_at",
			"enabled_at",
			"disabled_at",
			"metadata",
		}).AddRow(
			item.ID,
			item.Name,
			item.Version,
			item.Publisher,
			item.Type,
			item.Status,
			item.Source,
			[]byte(`{"id":"opensoha.k8s-sre-pack","name":"K8s SRE Pack","version":"0.1.0","publisher":"opensoha","type":"skill-pack"}`),
			item.ChecksumStatus,
			item.SignatureStatus,
			[]byte(`{"required":["ai.gateway.view","ai.gateway.invoke"],"domain":["workspace.resource.view"]}`),
			[]byte(`{"kubeconfig":"secret://k8s/default"}`),
			item.InstalledBy,
			item.InstalledAt,
			item.UpdatedAt,
			item.EnabledAt,
			item.DisabledAt,
			[]byte(`{"permissionModel":"requested-only"}`),
		))

	repo := New(db)
	saved, err := repo.UpsertInstalled(context.Background(), item)
	if err != nil {
		t.Fatalf("UpsertInstalled() error = %v", err)
	}
	if saved.Manifest.ID != item.Manifest.ID {
		t.Fatalf("manifest id = %q, want %q", saved.Manifest.ID, item.Manifest.ID)
	}
	if saved.RequestedPermissions == nil || len(saved.RequestedPermissions.Required) != 2 {
		t.Fatalf("requested permissions were not restored: %#v", saved.RequestedPermissions)
	}
	if saved.ConfiguredSecretRefs["kubeconfig"] != "secret://k8s/default" {
		t.Fatalf("configured secret refs = %#v", saved.ConfiguredSecretRefs)
	}
	if saved.Metadata["permissionModel"] != "requested-only" {
		t.Fatalf("metadata = %#v", saved.Metadata)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
