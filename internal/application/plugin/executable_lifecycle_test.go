package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func TestExecutableRepositoryTrustAndOfflineLifecycle(t *testing.T) {
	ctx := t.Context()
	upstream := &fixtureRepository{source: "https://github.com/example/storage-demo", commit: strings.Repeat("a", 40)}
	upstream.manifest = executableTestManifest(t, "1.0.0", false)
	history := &testExecutableRepository{memoryPluginRepo: newMemoryPluginRepo()}
	store := &testExecutableStore{fingerprint: strings.Repeat("a", 64)}
	runtime := &testExecutableRuntime{}
	references := &testDriverReferences{}
	permissions := appaccess.NewPermissionResolver(stubRolePermissions{
		"operator": {appaccess.PermPluginInstall, appaccess.PermPluginUpgrade, appaccess.PermPluginLifecycle, appaccess.PermPluginRemove},
		"manager":  {appaccess.PermPluginManage},
	})
	service := NewWithOptions(history, permissions, nil, WithRepositoryDistribution(upstream, bytes.Repeat([]byte{1}, 32)))
	service.SetExecutablePlugins(store, runtime, references, nil)
	operator := domainidentity.Principal{UserID: "operator", Roles: []string{"operator"}}
	manager := domainidentity.Principal{UserID: "manager", Roles: []string{"operator", "manager"}}
	installed := installTrustedExecutable(t, service, history, store, runtime, operator, manager, upstream.source)

	upstream.manifest, upstream.commit = executableTestManifest(t, "2.0.0", true), strings.Repeat("b", 40)
	request := domainplugin.PluginRepositoryPreviewRequest{RepositoryURL: upstream.source, Ref: "refs/tags/v2.0.0"}
	preview, err := service.PreviewRepository(ctx, operator, request)
	require.NoError(t, err)
	require.EqualValues(t, "confirmable", preview.InstallEligibility)
	upgrade := domainplugin.PluginRepositoryUpgradeRequest{PreviewToken: preview.PreviewToken}
	_, err = service.UpgradeRepository(ctx, operator, installed.ID, upgrade)
	require.ErrorIs(t, err, apperrors.ErrConflict)
	require.Equal(t, "1.0.0", runtime.active)
	upgrade.ConfirmPermissionIncrease = true
	runtime.fail = true
	_, err = service.UpgradeRepository(ctx, operator, installed.ID, upgrade)
	require.ErrorIs(t, err, apperrors.ErrServiceUnavailable)
	require.Equal(t, "1.0.0", runtime.active)
	require.Empty(t, history.previous.ID)

	runtime.fail = false
	type changeResult struct {
		item domainplugin.InstalledPlugin
		err  error
	}
	upgrades := make(chan changeResult, 2)
	for range 2 {
		go func() {
			item, err := service.UpgradeRepository(ctx, operator, installed.ID, upgrade)
			upgrades <- changeResult{item: item, err: err}
		}()
	}
	first, duplicate := <-upgrades, <-upgrades
	require.NoError(t, first.err)
	require.NoError(t, duplicate.err)
	upgraded := first.item
	require.Equal(t, "2.0.0", runtime.active)
	require.Equal(t, "1.0.0", upgraded.RollbackVersion)
	require.Equal(t, upgraded.UpdatedAt, duplicate.item.UpdatedAt)

	assertOfflineExecutableRollback(t, service, store, runtime, references, operator, installed.ID, upgraded)
}

func installTrustedExecutable(t *testing.T, service *Service, history *testExecutableRepository,
	store *testExecutableStore, runtime *testExecutableRuntime, operator, manager domainidentity.Principal, repositoryURL string,
) domainplugin.InstalledPlugin {
	t.Helper()
	ctx := t.Context()
	request := domainplugin.PluginRepositoryPreviewRequest{RepositoryURL: repositoryURL, Ref: "refs/heads/main"}

	preview, err := service.PreviewRepository(ctx, operator, request)
	require.NoError(t, err)
	require.EqualValues(t, "signed-release-required", preview.InstallEligibility)
	_, err = service.InstallRepository(ctx, operator, domainplugin.PluginRepositoryInstallRequest{PreviewToken: preview.PreviewToken})
	require.ErrorIs(t, err, apperrors.ErrConflict)
	require.Empty(t, history.items)

	store.published = true
	preview, err = service.PreviewRepository(ctx, operator, request)
	require.NoError(t, err)
	require.EqualValues(t, "publisher-trust-required", preview.InstallEligibility)
	require.Equal(t, store.fingerprint, preview.PublisherKeyFingerprint)
	request.TrustPublisherFingerprint = store.fingerprint
	_, err = service.PreviewRepository(ctx, operator, request)
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	preview, err = service.PreviewRepository(ctx, manager, request)
	require.NoError(t, err)
	require.EqualValues(t, "confirmable", preview.InstallEligibility)

	install := domainplugin.PluginRepositoryInstallRequest{PreviewToken: preview.PreviewToken, Enable: true}
	_, err = service.InstallRepository(ctx, manager, install)
	require.ErrorIs(t, err, apperrors.ErrInvalidArgument)
	require.Zero(t, store.staged)
	install.Enable = false
	installed, err := service.InstallRepository(ctx, manager, install)
	require.NoError(t, err)
	require.Equal(t, statusDisabled, installed.Status)
	require.Equal(t, store.fingerprint, installed.SourceInfo.PublisherKeyFingerprint)
	require.False(t, *installed.RuntimeReady)
	require.Zero(t, runtime.switches)
	installed, err = service.Enable(ctx, manager, installed.ID)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", runtime.active)
	require.True(t, *installed.RuntimeReady)

	return installed
}

func assertOfflineExecutableRollback(t *testing.T, service *Service, store *testExecutableStore,
	runtime *testExecutableRuntime, references *testDriverReferences, operator domainidentity.Principal, id string, upgraded domainplugin.InstalledPlugin,
) {
	t.Helper()
	ctx := t.Context()
	// Neither confirmation nor the release source can be used for offline rollback.
	service.repositories.source = nil
	rollback := domainplugin.PluginRepositoryRollbackRequest{ExpectedVersion: "2.0.0", ExpectedUpdatedAt: upgraded.UpdatedAt, Version: "1.0.0"}
	reverted, err := service.RollbackRepository(ctx, operator, id, rollback)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", runtime.active)
	require.Equal(t, "2.0.0", reverted.RollbackVersion)
	require.True(t, reverted.RollbackRequiresPermissionConfirmation)
	repeated, err := service.RollbackRepository(ctx, operator, id, rollback)
	require.NoError(t, err)
	require.Equal(t, reverted.UpdatedAt, repeated.UpdatedAt)
	_, err = service.RollbackRepository(ctx, operator, id,
		domainplugin.PluginRepositoryRollbackRequest{ExpectedVersion: "1.0.0", ExpectedUpdatedAt: reverted.UpdatedAt, Version: "2.0.0"})
	require.ErrorIs(t, err, apperrors.ErrConflict)

	// Restore from persisted selected release after rebuilding the execution host.
	replacement := &testExecutableRuntime{}
	service.SetExecutablePlugins(store, replacement, references, nil)
	require.NoError(t, service.RestoreExecutablePlugins(ctx))
	require.Equal(t, reverted.Version, replacement.active)
	references.count = 1
	_, err = service.Disable(ctx, operator, id)
	require.ErrorIs(t, err, apperrors.ErrConflict)
	require.ErrorIs(t, service.Remove(ctx, operator, id), apperrors.ErrConflict)
	require.Equal(t, reverted.Version, replacement.active)
	references.count = 0
	require.NoError(t, service.Remove(ctx, operator, id))
	require.Empty(t, replacement.active)
}

func TestExecutableRepositoryOfficialPublisherAndImmutableConfirmation(t *testing.T) {
	ctx := t.Context()
	rawKey := bytes.Repeat([]byte{3}, 32)
	encoded := base64.StdEncoding.EncodeToString(rawKey)
	fingerprint := publisherFromKey(encoded, trustCatalog).fingerprint
	const repoURL = "https://github.com/example/storage-demo"
	const catalog = `{"schemaVersion":"marketplace.opensoha.io/repositories/v1","sourceId":"directory","repositories":[{"id":"example.storage-demo","name":"Storage demo","type":"resource-extension","publisher":"Example","repositoryUrl":"` + repoURL + `","publisherKeys":[%q]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, catalog, encoded)
	}))
	t.Cleanup(server.Close)
	upstream := &fixtureRepository{source: repoURL, commit: strings.Repeat("a", 40), manifest: executableTestManifest(t, "1.0.0", false)}
	store := &testExecutableStore{fingerprint: fingerprint, published: true}
	request := domainplugin.PluginRepositoryPreviewRequest{RepositoryURL: repoURL, PluginID: "example.storage-demo", MarketplaceSourceID: "directory"}
	operator := domainidentity.Principal{UserID: "installer", Roles: []string{"operator"}}

	for _, official := range []bool{false, true} {
		provider, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: "directory", URL: server.URL, Official: official}, server.Client())
		require.NoError(t, err)
		history := &testExecutableRepository{memoryPluginRepo: newMemoryPluginRepo()}
		service := NewWithOptions(history, appaccess.NewPermissionResolver(stubRolePermissions{"operator": {appaccess.PermPluginInstall, appaccess.PermPluginUpgrade}}), nil,
			WithMarketplaceProvider(provider), WithRepositoryDistribution(upstream, bytes.Repeat([]byte{2}, 32)))
		service.SetExecutablePlugins(store, &testExecutableRuntime{}, &testDriverReferences{}, nil)
		preview, err := service.PreviewRepository(ctx, operator, request)
		require.NoError(t, err)
		if !official {
			require.EqualValues(t, "publisher-trust-required", preview.InstallEligibility)
			continue
		}
		require.EqualValues(t, "confirmable", preview.InstallEligibility)
		require.Equal(t, rawKey, store.inspectedKey)
		item := installOfficialExecutable(t, service, store, history, operator, preview.PreviewToken, preview.ExpiresAt)
		upstream.manifest, upstream.commit = executableTestManifest(t, "2.0.0", false), strings.Repeat("b", 40)
		preview, err = service.PreviewRepository(ctx, operator, request)
		require.NoError(t, err)
		staleUpgrade := domainplugin.PluginRepositoryUpgradeRequest{PreviewToken: preview.PreviewToken}
		oldFingerprint := store.fingerprint
		encoded = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
		rotatedProvider, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: "directory", URL: server.URL, Official: true}, server.Client())
		require.NoError(t, err)
		require.NoError(t, service.ReconfigureMarketplace(rotatedProvider))
		_, err = service.UpgradeRepository(ctx, operator, item.ID, staleUpgrade)
		require.ErrorIs(t, err, apperrors.ErrConflict)
		store.fingerprint = publisherFromKey(encoded, trustCatalog).fingerprint
		preview, err = service.PreviewRepository(ctx, operator, request)
		require.NoError(t, err)
		rotated, err := service.UpgradeRepository(ctx, operator, item.ID, domainplugin.PluginRepositoryUpgradeRequest{PreviewToken: preview.PreviewToken})
		require.NoError(t, err)
		require.Equal(t, store.fingerprint, rotated.SourceInfo.PublisherKeyFingerprint)
		reverted, err := service.RollbackRepository(ctx, operator, item.ID, domainplugin.PluginRepositoryRollbackRequest{
			ExpectedVersion: rotated.Version, ExpectedUpdatedAt: rotated.UpdatedAt, Version: item.Version,
		})
		require.NoError(t, err)
		require.Equal(t, oldFingerprint, reverted.SourceInfo.PublisherKeyFingerprint)

		upstream.source = "https://github.com/other/storage-demo"
		preview, err = service.PreviewRepository(ctx, operator, domainplugin.PluginRepositoryPreviewRequest{RepositoryURL: upstream.source, Ref: "refs/heads/main"})
		require.NoError(t, err)
		_, err = service.InstallRepository(ctx, operator, domainplugin.PluginRepositoryInstallRequest{PreviewToken: preview.PreviewToken})
		require.ErrorIs(t, err, apperrors.ErrConflict)
	}
}

func installOfficialExecutable(t *testing.T, service *Service, store *testExecutableStore,
	history *testExecutableRepository, operator domainidentity.Principal, token string, expiry time.Time,
) domainplugin.InstalledPlugin {
	t.Helper()
	ctx := t.Context()
	install := domainplugin.PluginRepositoryInstallRequest{PreviewToken: token}
	service.repositories.now = func() time.Time { return expiry }
	_, err := service.InstallRepository(ctx, operator, install)
	require.ErrorIs(t, err, apperrors.ErrConflict)
	service.repositories.now = time.Now
	store.attestationStamp = "changed"
	_, err = service.InstallRepository(ctx, operator, install)
	require.ErrorIs(t, err, apperrors.ErrConflict)
	require.Empty(t, history.items)
	store.attestationStamp = ""
	item, err := service.InstallRepository(ctx, operator, install)
	require.NoError(t, err)
	require.True(t, item.SourceInfo.Official)
	return item
}

func executableTestManifest(t *testing.T, version string, additionalPermissions bool) []byte {
	t.Helper()
	manifest := domainplugin.PluginManifest{
		ID: "example.storage-demo", Name: "Storage Demo", Version: version, Publisher: "Example",
		Type: "resource-extension", Runtime: &domainplugin.PluginRuntimeSpec{Mode: "executable-package-v1"},
		Components: &sohaapi.PluginPackageComponents{
			Backend: &sohaapi.PluginStorageDriverPackage{DriverID: "storage-demo", Protocol: "object-storage/v1",
				Platforms: map[string]sohaapi.PluginReleaseArtifact{"linux/amd64": {Name: "backend", Sha256: strings.Repeat("1", 64), ByteLength: 1}}},
		},
	}
	if additionalPermissions {
		manifest.Permissions = &domainplugin.PluginPermissionRequest{Required: []string{"plugin.view"}}
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	return raw
}

type testExecutableRepository struct {
	*memoryPluginRepo
	previous domainplugin.InstalledPlugin
}

func (r *testExecutableRepository) PreviousRelease(context.Context, string) (domainplugin.InstalledPlugin, error) {
	if r.previous.ID == "" {
		return r.previous, apperrors.ErrNotFound
	}
	return r.previous, nil
}
func (r *testExecutableRepository) RetainRelease(_ context.Context, item domainplugin.InstalledPlugin) error {
	r.previous = item
	return nil
}

type testExecutableStore struct {
	published        bool
	fingerprint      string
	attestationStamp string
	inspectedKey     []byte
	staged           int
}

func (s *testExecutableStore) Inspect(_ context.Context, _ domainplugin.PluginRepositoryPreviewSource, _ domainplugin.PluginManifest,
	_ []byte, fingerprint string, key []byte) (*sohaapi.PluginReleaseAttestation, string, bool, error) {
	s.inspectedKey = key
	if fingerprint != "" && fingerprint != s.fingerprint {
		return nil, "", false, publisherUntrusted()
	}
	if !s.published {
		return nil, "", false, nil
	}
	return &sohaapi.PluginReleaseAttestation{SchemaVersion: s.attestationStamp}, s.fingerprint, true, nil
}
func (s *testExecutableStore) Stage(ctx context.Context, source domainplugin.PluginRepositoryPreviewSource, manifest domainplugin.PluginManifest,
	raw []byte, fingerprint string, key []byte, digest string) (string, error) {
	details, _, _, _ := s.Inspect(ctx, source, manifest, raw, fingerprint, key)
	payload, _ := json.Marshal(details)
	if checksumBytes(payload) != "sha256:"+digest {
		return "", apperrors.ErrConflict
	}
	s.staged++
	return "/local/" + manifest.Version, nil
}
func (*testExecutableStore) RemovePlugin(context.Context, string) error { return nil }

func (*testExecutableStore) VerifyLocal(context.Context, domainplugin.PluginRepositoryPreviewSource, domainplugin.PluginManifest, string, string) error {
	return nil
}

type testExecutableRuntime struct {
	active   string
	switches int
	fail     bool
}

func (r *testExecutableRuntime) Switch(_ context.Context, item domainplugin.InstalledPlugin, commit func() error) error {
	if r.fail {
		return apperrors.ErrServiceUnavailable
	}
	if err := commit(); err != nil {
		return err
	}
	r.switches++
	r.active = item.Version
	return nil
}
func (*testExecutableRuntime) WaitRetired(context.Context, string) error { return nil }

func (r *testExecutableRuntime) Ready(_, version string) bool { return r.active == version }
func (r *testExecutableRuntime) Disable(_ context.Context, _ string, commit func() error) error {
	if err := commit(); err != nil {
		return err
	}
	r.active = ""
	return nil
}

type testDriverReferences struct{ count int }

func (r *testDriverReferences) StorageDriverReferenceCount(context.Context, string) (int, error) {
	return r.count, nil
}
func (r *testDriverReferences) WithStorageDriverReferences(_ context.Context, _ string, apply func([][]sohaapi.SystemIntegrationConfigurationField) error) error {
	if apply == nil {
		return errors.New("missing reference callback")
	}
	return apply(make([][]sohaapi.SystemIntegrationConfigurationField, r.count))
}
