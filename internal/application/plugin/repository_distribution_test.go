package plugin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type fixtureRepository struct {
	source, commit    string
	manifest          []byte
	resolves, fetches int
}

func (f *fixtureRepository) Resolve(_ context.Context, _, ref string) (string, string, string, error) {
	f.resolves++
	return f.source, ref, f.commit, nil
}

func (f *fixtureRepository) FetchManifest(_ context.Context, source, commit string) ([]byte, error) {
	f.fetches++
	if source != f.source || commit != f.commit {
		return nil, apperrors.ErrNotFound
	}
	return f.manifest, nil
}

func TestRepositoryDistributionPreviewConfirmAndUpgrade(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/examples/plugin-distribution/plugin.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	upstream := &fixtureRepository{source: "https://github.com/example/demo", commit: strings.Repeat("a", 40), manifest: raw}
	repo := newMemoryPluginRepo()
	permissions := appaccess.NewPermissionResolver(stubRolePermissions{
		"installer": {appaccess.PermPluginInstall, appaccess.PermPluginUpgrade, appaccess.PermPluginLifecycle},
	})
	service := NewWithOptions(repo, permissions, nil, WithRepositoryDistribution(upstream, bytes.Repeat([]byte{1}, 32)))
	ctx := t.Context()
	actor := domainidentity.Principal{UserID: "installer-a", Roles: []string{"installer"}}
	other := domainidentity.Principal{UserID: "installer-b", Roles: []string{"installer"}}
	input := domainplugin.PluginRepositoryPreviewRequest{RepositoryURL: upstream.source}
	if _, err := service.PreviewRepository(ctx, domainidentity.Principal{UserID: "viewer"}, input); !errors.Is(err, apperrors.ErrAccessDenied) || upstream.resolves != 0 {
		t.Fatalf("unauthorized preview reached upstream: %v", err)
	}
	preview, err := service.PreviewRepository(ctx, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.SourceInfo.Official || preview.SourceInfo.CommitSha != upstream.commit ||
		preview.SourceInfo.RawManifestChecksum != checksumBytes(raw) || len(repo.items) != 0 {
		t.Fatal("preview source evidence or read-only behavior is incorrect")
	}
	install := domainplugin.PluginRepositoryInstallRequest{PreviewToken: preview.PreviewToken}
	for _, invalid := range []struct {
		name  string
		actor domainidentity.Principal
		token string
	}{
		{"cross-caller", other, preview.PreviewToken},
		{"tampered", actor, preview.PreviewToken + "a"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			readerCalls := upstream.fetches
			_, err := service.InstallRepository(ctx, invalid.actor, domainplugin.PluginRepositoryInstallRequest{PreviewToken: invalid.token})
			if !errors.Is(err, apperrors.ErrConflict) || upstream.fetches != readerCalls || len(repo.items) != 0 {
				t.Fatalf("invalid proof reached upstream or changed state: %v", err)
			}
		})
	}
	upstream.manifest = append(bytes.Clone(raw), '\n')
	if _, err := service.InstallRepository(ctx, actor, install); !errors.Is(err, apperrors.ErrConflict) || len(repo.items) != 0 {
		t.Fatalf("changed bytes accepted: %v", err)
	}
	upstream.manifest = raw
	service.repositories.now = func() time.Time { return preview.ExpiresAt }
	if _, err := service.InstallRepository(ctx, actor, install); !errors.Is(err, apperrors.ErrConflict) || len(repo.items) != 0 {
		t.Fatalf("expired proof accepted: %v", err)
	}
	service.repositories.now = time.Now
	item, err := service.InstallRepository(ctx, actor, install)
	if err != nil || item.Status != statusDisabled || item.SourceInfo == nil || item.SourceInfo.CanonicalSource != upstream.source || upstream.resolves != 1 {
		t.Fatalf("unexpected install status = %s, resolutions = %d, err = %v", item.Status, upstream.resolves, err)
	}
	if _, err := service.Enable(ctx, actor, item.ID); err != nil {
		t.Fatal(err)
	}
	again, err := service.InstallRepository(ctx, actor, install)
	if err != nil || again.Status != statusEnabled {
		t.Fatalf("retry changed state: %s, %v", again.Status, err)
	}
	if _, err := service.Install(ctx, actor, domainplugin.PluginInstallRequest{Manifest: &item.Manifest}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("cross-source install accepted: %v", err)
	}
	if _, err := service.Upgrade(ctx, actor, item.ID, domainplugin.PluginInstallRequest{Manifest: &item.Manifest}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy upgrade of repository plugin accepted: %v", err)
	}
	upstream.commit = strings.Repeat("b", 40)
	upstream.manifest = bytes.Replace(raw, []byte(`"version": "0.1.0"`), []byte(`"version": "0.2.0", "permissions": {"required": ["plugin.view"]}`), 1)
	next, err := service.PreviewRepository(ctx, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	change := domainplugin.PluginRepositoryUpgradeRequest{PreviewToken: next.PreviewToken}
	if _, err := service.UpgradeRepository(ctx, actor, item.ID, change); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unconfirmed added permissions accepted: %v", err)
	}
	change.ConfirmPermissionIncrease = true
	if _, err := service.Disable(ctx, actor, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpgradeRepository(ctx, actor, item.ID, change); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("upgrade used an outdated installation snapshot: %v", err)
	}
	if _, err := service.Enable(ctx, actor, item.ID); err != nil {
		t.Fatal(err)
	}
	next, err = service.PreviewRepository(ctx, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	change.PreviewToken = next.PreviewToken
	upgraded, err := service.UpgradeRepository(ctx, actor, item.ID, change)
	if err != nil || upgraded.Version != "0.2.0" || upgraded.Status != statusEnabled || upgraded.SourceInfo.CommitSha != upstream.commit {
		t.Fatalf("same-source upgrade got version %q, status %q, err %v", upgraded.Version, upgraded.Status, err)
	}
}

func TestRepositoryManifestUsesPublishedIDConstraints(t *testing.T) {
	raw := []byte(`{"id":"bad/path","name":"Invalid","version":"0.1.0","publisher":"example","type":"skill-pack"}`)
	if _, err := decodeRepositoryManifest(raw); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("manifest without a routable ID was accepted: %v", err)
	}
}

func TestLegacyMarketplaceUpgradeUsesConfiguredIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":"marketplace.opensoha.io/v1","sourceId":"author-id","plugins":[{"id":"legacy.plugin","version":"0.2.0","manifest":{"id":"legacy.plugin","name":"Legacy","version":"0.2.0","publisher":"example","type":"skill-pack"}}]}`))
	}))
	t.Cleanup(server.Close)
	provider, err := NewRemoteMarketplaceProvider(MarketplaceSource{ID: "configured-id", URL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	repo := newMemoryPluginRepo()
	repo.items["legacy.plugin"] = domainplugin.InstalledPlugin{
		ID: "legacy.plugin", Version: "0.1.0", Type: "skill-pack",
		Source: "marketplace:legacy.plugin", Metadata: map[string]any{"sourceId": "author-id", "marketplaceUrl": server.URL},
	}
	service := NewWithOptions(repo, appaccess.NewPermissionResolver(stubRolePermissions{
		"upgrader": {appaccess.PermPluginUpgrade},
	}), nil, WithMarketplaceProvider(provider))
	principal := domainidentity.Principal{UserID: "upgrader", Roles: []string{"upgrader"}}
	ambiguous := repo.items["legacy.plugin"]
	ambiguous.ID = "unknown.plugin"
	ambiguous.Metadata = nil
	repo.items["unknown.plugin"] = ambiguous
	if _, err := service.Upgrade(t.Context(), principal, "unknown.plugin", domainplugin.PluginInstallRequest{}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("upgrade chose a source for an ambiguous historical installation: %v", err)
	}
	if _, err := service.Upgrade(t.Context(), principal, "legacy.plugin", domainplugin.PluginInstallRequest{SourceID: "other-source"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unexpected source was accepted: %v", err)
	}
	item, err := service.Upgrade(t.Context(), principal, "legacy.plugin", domainplugin.PluginInstallRequest{SourceID: "author-id"})
	if err != nil || item.Version != "0.2.0" || item.SourceInfo == nil || item.SourceInfo.SourceID != "configured-id" {
		t.Fatalf("legacy same-directory upgrade failed: version=%s source=%v err=%v", item.Version, item.SourceInfo, err)
	}
}
