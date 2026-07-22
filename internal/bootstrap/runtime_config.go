package bootstrap

import (
	"context"
	"errors"
	"fmt"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
	appruntimeconfig "github.com/opensoha/soha/internal/application/runtimeconfig"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

type restartableModule interface {
	Start(context.Context)
	Stop(context.Context) error
	Running() bool
}

type moduleLifecycleApplier struct {
	lifecycleCtx context.Context
	services     map[string]restartableModule
}

func newModuleLifecycleApplier(lifecycleCtx context.Context, services map[string]restartableModule) moduleLifecycleApplier {
	return moduleLifecycleApplier{lifecycleCtx: lifecycleCtx, services: services}
}

func (a moduleLifecycleApplier) Handles(key string) bool {
	_, ok := a.services[key]
	return ok
}

func (a moduleLifecycleApplier) Apply(ctx context.Context, _, next appruntimeconfig.Snapshot, keys []string) ([]sohaapi.RuntimeConfigAppliedItem, error) {
	items := make([]sohaapi.RuntimeConfigAppliedItem, 0, len(keys))
	var combined error
	for _, key := range keys {
		service := a.services[key]
		enabled := next.Bool(key, false)
		var err error
		if enabled {
			service.Start(a.lifecycleCtx)
			if !service.Running() {
				err = fmt.Errorf("module service did not enter running state")
			}
		} else {
			err = service.Stop(ctx)
			if err == nil && service.Running() {
				err = fmt.Errorf("module service did not stop")
			}
		}
		item := sohaapi.RuntimeConfigAppliedItem{Key: key, ApplyMode: sohaapi.RuntimeConfigApplyModeLifecycle, Status: sohaapi.RuntimeConfigApplicationStatusApplied}
		if err != nil {
			item.Status = sohaapi.RuntimeConfigApplicationStatusFailed
			item.Message = "module lifecycle transition failed"
			combined = errors.Join(combined, fmt.Errorf("apply %s lifecycle: %w", key, err))
		}
		items = append(items, item)
	}
	return items, combined
}

type marketplaceConfigApplier struct {
	base    cfgpkg.Config
	plugins *appplugin.Service
}

func (a marketplaceConfigApplier) Handles(key string) bool {
	return key == appruntimeconfig.KeyMarketplaceURL || key == appruntimeconfig.KeyMarketplaceSourceID
}

func (a marketplaceConfigApplier) Apply(_ context.Context, _, next appruntimeconfig.Snapshot, keys []string) ([]sohaapi.RuntimeConfigAppliedItem, error) {
	cfg := a.base
	cfg.Plugins.Marketplace.URL = next.String(appruntimeconfig.KeyMarketplaceURL, cfgpkg.DefaultMarketplaceURL)
	cfg.Plugins.Marketplace.SourceID = next.String(appruntimeconfig.KeyMarketplaceSourceID, cfgpkg.DefaultMarketplaceSourceID)
	provider, err := newMarketplaceProvider(cfg)
	if err == nil {
		err = a.plugins.ReconfigureMarketplace(provider)
	}
	items := make([]sohaapi.RuntimeConfigAppliedItem, 0, len(keys))
	for _, key := range keys {
		item := sohaapi.RuntimeConfigAppliedItem{Key: key, ApplyMode: sohaapi.RuntimeConfigApplyModeReconfigure, Status: sohaapi.RuntimeConfigApplicationStatusApplied}
		if err != nil {
			item.Status = sohaapi.RuntimeConfigApplicationStatusFailed
			item.Message = "marketplace provider reconfiguration failed"
		}
		items = append(items, item)
	}
	if err != nil {
		return items, fmt.Errorf("reconfigure marketplace: %w", err)
	}
	return items, nil
}
