package bootstrap

import (
	"context"
	"fmt"
	"time"

	appsecret "github.com/opensoha/soha/internal/application/secret"
	appsystemintegration "github.com/opensoha/soha/internal/application/systemintegration"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/infrastructure/softwareobjectstore"
	"github.com/opensoha/soha/internal/infrastructure/storagedriverhost"
)

func storageDriverRegistry(integrations *appsystemintegration.Service, secrets *appsecret.Service) *appsystemintegration.StorageDriverRegistry {
	registry := appsystemintegration.NewStorageDriverRegistry(softwareobjectstore.NewS3Driver())
	integrations.SetStorageDrivers(registry, secrets)
	return registry
}

func startStorageDrivers(ctx context.Context, cfg cfgpkg.PluginStorageConfig, registry *appsystemintegration.StorageDriverRegistry) (*storagedriverhost.Supervisor, error) {
	if len(cfg.Drivers) == 0 {
		return nil, nil
	}
	hostConfig := storagedriverhost.Config{PackageRoot: cfg.PackageRoot, RuntimeRoot: cfg.RuntimeRoot}
	for _, driver := range cfg.Drivers {
		hostConfig.Drivers = append(hostConfig.Drivers, storagedriverhost.DriverConfig{
			PluginID: driver.PluginID, Version: driver.Version, Repository: driver.Repository,
			Commit: driver.Commit, PublicKeyPath: driver.PublicKeyPath, UID: driver.UID, GID: driver.GID,
		})
	}
	supervisor, err := storagedriverhost.New(hostConfig)
	if err != nil {
		return nil, fmt.Errorf("configure storage driver supervisor: %w", err)
	}
	for _, descriptor := range supervisor.Descriptors() {
		driver, err := supervisor.Driver(descriptor.ID)
		if err != nil {
			return nil, err
		}
		if err := registry.RegisterPlugin(descriptor.ID, descriptor.Name, descriptor.ConfigSchema, driver); err != nil {
			return nil, err
		}
	}
	if err := supervisor.Start(ctx); err != nil {
		return nil, fmt.Errorf("start storage driver supervisor: %w", err)
	}
	return supervisor, nil
}

func closeStorageDrivers(supervisor interface{ Close(context.Context) error }) {
	if supervisor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = supervisor.Close(ctx)
	}
}
