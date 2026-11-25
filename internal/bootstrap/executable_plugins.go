package bootstrap

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opensoha/soha/internal/application/systemintegration"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/infrastructure/githubplugin"
	"github.com/opensoha/soha/internal/infrastructure/pluginactivation"
	"github.com/opensoha/soha/internal/infrastructure/pluginrelease"
	"github.com/opensoha/soha/internal/infrastructure/storagedriverhost"
)

type storageDriverRuntimes struct {
	static  *storagedriverhost.Supervisor
	managed *pluginactivation.Manager
}

func (r *storageDriverRuntimes) Close(ctx context.Context) error {
	var err error
	if r.managed != nil {
		err = r.managed.Close(ctx)
	}
	if r.static != nil {
		err = errors.Join(err, r.static.Close(ctx))
	}
	return err
}

type executablePluginRuntime struct {
	*pluginactivation.Manager
	packageRoot string
}

func (r executablePluginRuntime) Switch(ctx context.Context, item domainplugin.InstalledPlugin, commit func() error) error {
	repository := item.SourceInfo.RepositoryURL
	if repository == "" {
		repository = item.SourceInfo.CanonicalSource
	}
	release := pluginactivation.Release{
		PluginID: item.ID, Version: item.Version,
		Repository: repository, Commit: item.SourceInfo.CommitSha,
		PublicKeyPath: filepath.Join(r.packageRoot, item.ID, item.Version, pluginrelease.PublisherFilename),
	}
	if item.Manifest.Components != nil && item.Manifest.Components.Backend != nil {
		release.DriverID = item.Manifest.Components.Backend.DriverID
	}
	return r.Manager.Switch(ctx, release, commit)
}

func startExecutablePlugins(ctx context.Context, cfg cfgpkg.PluginsConfig, core *coreServices, registry *systemintegration.StorageDriverRegistry) (*pluginactivation.Manager, error) {
	settings := cfg.Executable
	if settings.PackageRoot == "" && len(settings.StorageAccounts) == 0 && len(settings.TrustedPublisherKeys) == 0 {
		return nil, nil
	}
	if !filepath.IsAbs(settings.PackageRoot) || filepath.Clean(settings.PackageRoot) != settings.PackageRoot || settings.PackageRoot == "/" {
		return nil, errors.New("plugins.executable.package_root must be a dedicated canonical absolute directory")
	}
	if overlapsPluginRoot(settings.PackageRoot, settings.RuntimeRoot) ||
		overlapsPluginRoot(settings.PackageRoot, cfg.Storage.PackageRoot) ||
		overlapsPluginRoot(settings.PackageRoot, cfg.Storage.RuntimeRoot) ||
		overlapsPluginRoot(settings.RuntimeRoot, cfg.Storage.PackageRoot) ||
		overlapsPluginRoot(settings.RuntimeRoot, cfg.Storage.RuntimeRoot) {
		return nil, errors.New("managed executable plugins require separate package and runtime directories")
	}
	slots, err := executableStorageAccounts(settings.StorageAccounts, settings.TrustedPublisherKeys, cfg.Storage.Drivers)
	if err != nil {
		return nil, err
	}
	manager, err := pluginactivation.New(ctx, pluginactivation.Config{PackageRoot: settings.PackageRoot, RuntimeRoot: settings.RuntimeRoot, Slots: slots}, registry)
	if err != nil {
		return nil, err
	}
	store := pluginrelease.New(githubplugin.New(cfg.Repository.GitHubToken), settings.PackageRoot)
	core.pluginService.SetExecutablePlugins(store, executablePluginRuntime{Manager: manager, packageRoot: settings.PackageRoot}, core.systemIntegrationService, settings.TrustedPublisherKeys)
	return manager, nil
}

func executableStorageAccounts(accountsByPlugin map[string][]cfgpkg.PluginExecutableAccount, trustedKeys map[string]string,
	staticDrivers []cfgpkg.PluginStorageDriverConfig) (map[string][2]pluginactivation.Account, error) {
	for repository, key := range trustedKeys {
		pub, err := base64.StdEncoding.Strict().DecodeString(key)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid configured publisher key for %q", repository)
		}
	}
	slots := make(map[string][2]pluginactivation.Account, len(accountsByPlugin))
	for pluginID, accounts := range accountsByPlugin {
		if len(accounts) != 2 {
			return nil, fmt.Errorf("plugin %q requires exactly two dedicated execution accounts", pluginID)
		}
		var pair [2]pluginactivation.Account
		for i, account := range accounts {
			for _, static := range staticDrivers {
				if static.PluginID == pluginID || static.UID == account.UID || static.GID == account.GID {
					return nil, errors.New("managed and statically configured drivers must use separate plugin identities and accounts")
				}
			}
			pair[i] = pluginactivation.Account{UID: account.UID, GID: account.GID}
		}
		slots[pluginID] = pair
	}
	return slots, nil
}

func overlapsPluginRoot(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	separator := string(filepath.Separator)
	return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, separator)+separator) ||
		strings.HasPrefix(b, strings.TrimSuffix(a, separator)+separator)
}
