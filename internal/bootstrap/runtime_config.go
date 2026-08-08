package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appexecution "github.com/opensoha/soha/internal/application/execution"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
	appruntimeconfig "github.com/opensoha/soha/internal/application/runtimeconfig"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
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

type runtimeValueApplier struct {
	handlers map[string]func(context.Context, appruntimeconfig.Snapshot) error
}

func (a runtimeValueApplier) Handles(key string) bool {
	_, ok := a.handlers[key]
	return ok
}

func (a runtimeValueApplier) Apply(ctx context.Context, _, next appruntimeconfig.Snapshot, keys []string) ([]sohaapi.RuntimeConfigAppliedItem, error) {
	items := make([]sohaapi.RuntimeConfigAppliedItem, 0, len(keys))
	var combined error
	for _, key := range keys {
		err := a.handlers[key](ctx, next)
		item := sohaapi.RuntimeConfigAppliedItem{Key: key, Status: sohaapi.RuntimeConfigApplicationStatusApplied}
		if definition, ok := next.Definition(key); ok {
			item.ApplyMode = definition.ApplyMode
		}
		if err != nil {
			item.Status = sohaapi.RuntimeConfigApplicationStatusFailed
			item.Message = "runtime reconfiguration failed"
			combined = errors.Join(combined, fmt.Errorf("apply %s: %w", key, err))
		}
		items = append(items, item)
	}
	return items, combined
}

func runtimeEffectiveConfig(cfg cfgpkg.Config, snapshot appruntimeconfig.Snapshot) cfgpkg.Config {
	cfg.HTTP.AccessURL = strings.TrimRight(strings.TrimSpace(snapshot.String(appruntimeconfig.KeyAccessURL, cfg.HTTP.AccessURL)), "/")
	cfg.Runtime.WorkflowWorkers = snapshot.Int(appruntimeconfig.KeyWorkflowWorkers, cfg.Runtime.WorkflowWorkers)
	cfg.Runtime.WorkflowQueueSize = snapshot.Int(appruntimeconfig.KeyWorkflowQueueSize, cfg.Runtime.WorkflowQueueSize)
	cfg.Runtime.WorkflowNodeParallelism = snapshot.Int(appruntimeconfig.KeyWorkflowNodeParallelism, cfg.Runtime.WorkflowNodeParallelism)
	cfg.Runtime.ClusterSyncParallelism = snapshot.Int(appruntimeconfig.KeyClusterSyncParallelism, cfg.Runtime.ClusterSyncParallelism)
	cfg.Runtime.CopilotInspectionParallelism = snapshot.Int(appruntimeconfig.KeyCopilotInspectionParallelism, cfg.Runtime.CopilotInspectionParallelism)
	cfg.Runtime.AlertUpsertBatchSize = snapshot.Int(appruntimeconfig.KeyAlertUpsertBatchSize, cfg.Runtime.AlertUpsertBatchSize)
	cfg.Runtime.VirtualizationWorkerInterval = snapshot.Duration(appruntimeconfig.KeyVirtualizationWorkerInterval, cfg.Runtime.VirtualizationWorkerInterval)
	cfg.Runtime.VirtualizationSyncConcurrency = snapshot.Int(appruntimeconfig.KeyVirtualizationSyncConcurrency, cfg.Runtime.VirtualizationSyncConcurrency)
	cfg.Runtime.ExecutionJobClusterID = snapshot.String(appruntimeconfig.KeyExecutionJobClusterID, cfg.Runtime.ExecutionJobClusterID)
	cfg.Runtime.ExecutionJobNamespace = snapshot.String(appruntimeconfig.KeyExecutionJobNamespace, cfg.Runtime.ExecutionJobNamespace)
	cfg.Runtime.ExecutionJobImage = snapshot.String(appruntimeconfig.KeyExecutionJobImage, cfg.Runtime.ExecutionJobImage)
	cfg.Runtime.ExecutionJobGitImage = snapshot.String(appruntimeconfig.KeyExecutionJobGitImage, cfg.Runtime.ExecutionJobGitImage)
	cfg.Runtime.ExecutionJobTTLSeconds = snapshot.Int(appruntimeconfig.KeyExecutionJobTTLSeconds, cfg.Runtime.ExecutionJobTTLSeconds)
	cfg.MCP.DefaultTimeout = snapshot.Duration(appruntimeconfig.KeyMCPDefaultTimeout, cfg.MCP.DefaultTimeout)
	cfg.AIGateway.Relay.DefaultTimeout = snapshot.Duration(appruntimeconfig.KeyAIGatewayDefaultTimeout, cfg.AIGateway.Relay.DefaultTimeout)
	cfg.AIGateway.Relay.StreamTimeout = snapshot.Duration(appruntimeconfig.KeyAIGatewayStreamTimeout, cfg.AIGateway.Relay.StreamTimeout)
	cfg.AIGateway.Relay.FirstByteTimeout = snapshot.Duration(appruntimeconfig.KeyAIGatewayFirstByteTimeout, cfg.AIGateway.Relay.FirstByteTimeout)
	cfg.AIGateway.Relay.StreamIdleTimeout = snapshot.Duration(appruntimeconfig.KeyAIGatewayStreamIdleTimeout, cfg.AIGateway.Relay.StreamIdleTimeout)
	cfg.AIGateway.Relay.HealthCheckEnabled = snapshot.Bool(appruntimeconfig.KeyAIGatewayHealthCheckEnabled, cfg.AIGateway.Relay.HealthCheckEnabled)
	cfg.AIGateway.Relay.HealthCheckInterval = snapshot.Duration(appruntimeconfig.KeyAIGatewayHealthCheckInterval, cfg.AIGateway.Relay.HealthCheckInterval)
	cfg.AIGateway.Relay.MaxRequestBodyMB = snapshot.Int(appruntimeconfig.KeyAIGatewayMaxRequestBodyMB, cfg.AIGateway.Relay.MaxRequestBodyMB)
	cfg.AIGateway.Relay.IncludeUsageForOpenAIStream = snapshot.Bool(appruntimeconfig.KeyAIGatewayIncludeStreamUsage, cfg.AIGateway.Relay.IncludeUsageForOpenAIStream)
	return cfg
}

func applyExecutionJobRuntimeConfig(service *appexecution.Service, snapshot appruntimeconfig.Snapshot) {
	service.SetJobRuntimeOptions(appexecution.JobRuntimeOptions{
		ClusterID:  snapshot.String(appruntimeconfig.KeyExecutionJobClusterID, ""),
		Namespace:  snapshot.String(appruntimeconfig.KeyExecutionJobNamespace, "soha-system"),
		Image:      snapshot.String(appruntimeconfig.KeyExecutionJobImage, "alpine:3.20"),
		GitImage:   snapshot.String(appruntimeconfig.KeyExecutionJobGitImage, "alpine/git:2.47.0"),
		TTLSeconds: snapshot.Int(appruntimeconfig.KeyExecutionJobTTLSeconds, 3600),
	})
}

func virtualizationRuntimeHandler(lifecycleCtx context.Context, service *appvirtualization.Service) func(context.Context, appruntimeconfig.Snapshot) error {
	return func(ctx context.Context, snapshot appruntimeconfig.Snapshot) error {
		wasRunning := service.Running()
		if wasRunning {
			if err := service.Stop(ctx); err != nil {
				return err
			}
		}
		if err := service.SetRuntimeOptions(
			snapshot.Duration(appruntimeconfig.KeyVirtualizationWorkerInterval, 2*time.Second),
			snapshot.Int(appruntimeconfig.KeyVirtualizationSyncConcurrency, 1),
		); err != nil {
			return err
		}
		if wasRunning {
			service.Start(lifecycleCtx)
		}
		return nil
	}
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
