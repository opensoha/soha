package plugin

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryUpsertInstalledPersistsManifestSnapshot(t *testing.T) {
	repo, mock := newMockRepository(t)
	item := installedPluginFixture(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))

	mock.ExpectExec(`(?s)INSERT INTO installed_plugins .*ON CONFLICT \(id\) DO UPDATE SET`).
		WithArgs(
			item.ID,
			item.Name,
			item.Version,
			item.Publisher,
			item.Type,
			item.Status,
			item.Source,
			jsonEqualArg(`{"id":"opensoha.k8s-sre-pack","name":"K8s SRE Pack","version":"0.1.0","publisher":"opensoha","type":"skill-pack"}`),
			item.ChecksumStatus,
			item.SignatureStatus,
			jsonEqualArg(`{"required":["ai.gateway.view","ai.gateway.invoke"],"domain":["workspace.resource.view"]}`),
			jsonEqualArg(`{"kubeconfig":"secret://k8s/default"}`),
			item.InstalledBy,
			item.InstalledAt,
			item.UpdatedAt,
			item.EnabledAt,
			item.DisabledAt,
			jsonEqualArg(`{"permissionModel":"requested-only"}`),
			jsonEqualArg(`{"kind":"github-repo","canonicalSource":"https://github.com/opensoha/k8s-sre-pack","pinnedRef":"v0.1.0","commitSha":"abc123","rawManifestChecksum":"sha256:abc123","official":false}`),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT id, .*metadata, source_info FROM installed_plugins WHERE id = \$1$`).
		WithArgs(item.ID).
		WillReturnRows(mockInstalledRows(t, item))

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
	if saved.SourceInfo == nil || *saved.SourceInfo != *item.SourceInfo {
		t.Fatalf("source info = %#v, want %#v", saved.SourceInfo, item.SourceInfo)
	}
}

func TestRepositoryUpdateInstalledLocksOriginalSnapshot(t *testing.T) {
	repo, mock := newMockRepository(t)
	current := installedPluginFixture(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	item := current
	upgradedSource := *current.SourceInfo
	upgradedSource.CommitSha = "def456"
	item.SourceInfo = &upgradedSource
	item.Version = "0.2.0"
	item.Manifest.Version = item.Version
	persisted := item
	persisted.UpdatedAt = item.UpdatedAt.Add(time.Microsecond)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, .*metadata, source_info FROM installed_plugins WHERE id = \$1 FOR UPDATE$`).
		WithArgs(current.ID).
		WillReturnRows(mockInstalledRows(t, current))
	mock.ExpectQuery(`(?s)UPDATE installed_plugins SET .*updated_at = GREATEST\(\$12::timestamp, updated_at \+ INTERVAL '1 microsecond'\).*source_info = \$16::jsonb WHERE id = \$17 RETURNING id, .*metadata, source_info`).
		WithArgs(item.Name, item.Version, item.Publisher, item.Type, item.Status, item.Source,
			jsonEqualArg(`{"id":"opensoha.k8s-sre-pack","name":"K8s SRE Pack","version":"0.2.0","publisher":"opensoha","type":"skill-pack"}`),
			item.ChecksumStatus, item.SignatureStatus,
			jsonEqualArg(`{"required":["ai.gateway.view","ai.gateway.invoke"],"domain":["workspace.resource.view"]}`),
			jsonEqualArg(`{"kubeconfig":"secret://k8s/default"}`),
			item.UpdatedAt, item.EnabledAt, item.DisabledAt,
			jsonEqualArg(`{"permissionModel":"requested-only"}`),
			jsonEqualArg(`{"kind":"github-repo","canonicalSource":"https://github.com/opensoha/k8s-sre-pack","pinnedRef":"v0.1.0","commitSha":"def456","rawManifestChecksum":"sha256:abc123","official":false}`),
			item.ID).
		WillReturnRows(mockInstalledRows(t, persisted))
	mock.ExpectCommit()

	calls := 0
	updated, err := repo.UpdateInstalled(t.Context(), current, current, func(ctx context.Context, next *domainplugin.InstalledPlugin) error {
		calls++
		if _, ok := dbtx.Bound(ctx, repo.db); !ok {
			t.Error("callback must receive the bound transaction")
		}
		next.Version = item.Version
		next.Manifest.Version = item.Manifest.Version
		next.SourceInfo = item.SourceInfo
		return nil
	})
	if err != nil || calls != 1 || !reflect.DeepEqual(updated, persisted) {
		t.Fatalf("UpdateInstalled() = %#v, %v with %d callbacks, want %#v", updated, err, calls, persisted)
	}
}

func TestRepositoryUpdateInstalledRejectsStaleWrites(t *testing.T) {
	callbackErr := errors.New("callback failed")
	tests := []struct {
		name           string
		missing        bool
		change         func(*domainplugin.InstalledPlugin, *domainplugin.InstalledPlugin)
		callbackChange func(*domainplugin.InstalledPlugin)
		wantErr        error
		wantCalls      int
	}{
		{name: "deleted", missing: true, wantErr: apperrors.ErrConflict},
		{name: "updated", change: func(current, _ *domainplugin.InstalledPlugin) {
			current.UpdatedAt = current.UpdatedAt.Add(time.Microsecond)
		}, wantErr: apperrors.ErrConflict},
		{name: "reinstalled", change: func(current, _ *domainplugin.InstalledPlugin) {
			current.InstalledAt = current.InstalledAt.Add(time.Microsecond)
		}, wantErr: apperrors.ErrConflict},
		{name: "different source snapshot", change: func(current, _ *domainplugin.InstalledPlugin) {
			current.SourceInfo = nil
		}, wantErr: apperrors.ErrConflict},
		{name: "attempted source switch", change: func(_, item *domainplugin.InstalledPlugin) {
			otherSource := *item.SourceInfo
			otherSource.CanonicalSource = "https://github.com/other/k8s-sre-pack"
			item.SourceInfo = &otherSource
		}, wantErr: apperrors.ErrConflict},
		{name: "callback attempted ID switch", callbackChange: func(item *domainplugin.InstalledPlugin) {
			item.ID = "other"
		}, wantErr: apperrors.ErrConflict, wantCalls: 1},
		{name: "callback removed source binding", callbackChange: func(item *domainplugin.InstalledPlugin) {
			item.SourceInfo = nil
		}, wantErr: apperrors.ErrConflict, wantCalls: 1},
		{name: "callback failure", wantErr: callbackErr, wantCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newMockRepository(t)
			expected := installedPluginFixture(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
			current, item := expected, expected
			if tc.change != nil {
				tc.change(&current, &item)
			}
			rows := sqlmock.NewRows([]string{"id"})
			if !tc.missing {
				rows = mockInstalledRows(t, current)
			}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .* FOR UPDATE$`).WithArgs(expected.ID).WillReturnRows(rows)
			mock.ExpectRollback()

			calls := 0
			_, err := repo.UpdateInstalled(t.Context(), expected, item, func(_ context.Context, next *domainplugin.InstalledPlugin) error {
				calls++
				if tc.callbackChange != nil {
					tc.callbackChange(next)
					return nil
				}
				return callbackErr
			})
			if !errors.Is(err, tc.wantErr) || calls != tc.wantCalls {
				t.Fatalf("UpdateInstalled() error = %v with %d callbacks, want %v with %d", err, calls, tc.wantErr, tc.wantCalls)
			}
		})
	}
}

func TestRepositoryGetInstalledPreservesNullSourceInfo(t *testing.T) {
	repo, mock := newMockRepository(t)
	item := installedPluginFixture(time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC))
	item.SourceInfo = nil
	mock.ExpectQuery(`SELECT .* FROM installed_plugins WHERE id = \$1$`).
		WithArgs(item.ID).
		WillReturnRows(mockInstalledRows(t, item))
	stored, err := repo.GetInstalled(t.Context(), item.ID)
	if err != nil || stored.SourceInfo != nil {
		t.Fatalf("GetInstalled() source info = %#v, %v, want nil", stored.SourceInfo, err)
	}
}

func newMockRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("sql expectations: %v", err)
		}
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return New(db), mock
}

func mockInstalledRows(t *testing.T, item domainplugin.InstalledPlugin) *sqlmock.Rows {
	t.Helper()
	encode := func(value any) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		return raw
	}
	var sourceInfo driver.Value
	if item.SourceInfo != nil {
		sourceInfo = encode(item.SourceInfo)
	}
	return sqlmock.NewRows(strings.Fields(`id name version publisher type status source manifest checksum_status
		signature_status requested_permissions configured_secret_refs installed_by installed_at updated_at
		enabled_at disabled_at metadata source_info`)).AddRow(
		item.ID, item.Name, item.Version, item.Publisher, item.Type, item.Status, item.Source,
		encode(item.Manifest), item.ChecksumStatus, item.SignatureStatus, encode(item.RequestedPermissions),
		encode(item.ConfiguredSecretRefs), item.InstalledBy, item.InstalledAt, item.UpdatedAt,
		item.EnabledAt, item.DisabledAt, encode(item.Metadata), sourceInfo,
	)
}

func installedPluginFixture(now time.Time) domainplugin.InstalledPlugin {
	return domainplugin.InstalledPlugin{
		ID:        "opensoha.k8s-sre-pack",
		Name:      "K8s SRE Pack",
		Version:   "0.1.0",
		Publisher: "opensoha",
		Type:      "skill-pack",
		Status:    "enabled",
		Source:    "https://github.com/opensoha/k8s-sre-pack",
		SourceInfo: &domainplugin.PluginSourceInfo{
			Kind:                domainplugin.SourceKindGitHubRepo,
			CanonicalSource:     "https://github.com/opensoha/k8s-sre-pack",
			PinnedRef:           "v0.1.0",
			CommitSha:           "abc123",
			RawManifestChecksum: "sha256:abc123",
		},
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
}

type jsonEqualArg string

func (expected jsonEqualArg) Match(actual driver.Value) bool {
	actualText, ok := actual.(string)
	if !ok {
		return false
	}
	var expectedValue any
	if err := json.Unmarshal([]byte(expected), &expectedValue); err != nil {
		return false
	}
	var actualValue any
	if err := json.Unmarshal([]byte(actualText), &actualValue); err != nil {
		return false
	}
	return reflect.DeepEqual(expectedValue, actualValue)
}
