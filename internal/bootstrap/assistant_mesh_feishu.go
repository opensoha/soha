package bootstrap

import (
	"context"
	"errors"
	"strings"
	"time"

	appmesh "github.com/opensoha/soha/internal/application/assistantmesh"
	meshinfra "github.com/opensoha/soha/internal/infrastructure/assistantmesh"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	meshrepo "github.com/opensoha/soha/internal/repository/assistantmesh"
	"go.uber.org/zap"
)

func wireFeishuMesh(settings cfgpkg.AIGatewayConfig, infra *infrastructure, repos *repositories, core *coreServices,
	reception *appmesh.Reception, store *meshrepo.Repository,
) error {
	cfg := settings.FeishuMesh
	if !cfg.Enabled {
		return nil
	}
	if err := validateFeishuMeshConfig(settings); err != nil {
		return err
	}
	directory := meshinfra.NewFeishuDirectory(repos.directorySyncRepository, core.settingsService, cfg.ConnectionID, cfg.AppID)
	sender, err := meshinfra.NewFeishuSender(cfg.Endpoint, cfg.RuntimeToken, cfg.AppID)
	if err != nil {
		return err
	}
	channel := appmesh.NewFeishuChannel(reception, store, directory, sender, cfg.AppID, cfg.TenantKey, cfg.BotOpenID, cfg.ConnectionID)
	core.eventService.SetFeishuMeshReceiver(cfg.IngressToken, cfg.AppID, cfg.TenantKey, channel.Enqueue)
	go runFeishuMeshWorker(infra.lifecycleCtx, infra.logger, channel)
	return nil
}

func validateFeishuMeshConfig(settings cfgpkg.AIGatewayConfig) error {
	cfg := settings.FeishuMesh
	if !strings.HasPrefix(cfg.AppID, "cli_") || !strings.HasPrefix(cfg.BotOpenID, "ou_") ||
		strings.TrimSpace(cfg.TenantKey) == "" || strings.TrimSpace(cfg.ConnectionID) == "" ||
		strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.IngressToken) == "" ||
		strings.TrimSpace(cfg.RuntimeToken) == "" {
		return errors.New("the Heya Feishu channel requires app, tenant, bot, directory connection, runtime endpoint and both channel tokens")
	}
	ingress := strings.TrimSpace(cfg.IngressToken)
	outbound := strings.TrimSpace(cfg.RuntimeToken)
	if ingress == outbound || ingress == strings.TrimSpace(settings.ConnectorEventSink.Token) {
		return errors.New("the Heya Feishu ingress requires a distinct token")
	}
	return nil
}

func runFeishuMeshWorker(ctx context.Context, logger *zap.Logger, channel *appmesh.FeishuChannel) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Bound a single sweep even under continuous incoming traffic.
			for range 20 {
				processed, err := channel.ProcessOne(ctx)
				if err != nil && ctx.Err() == nil {
					logger.Warn("Heya Feishu job could not be processed")
				}
				if err != nil || !processed || ctx.Err() != nil {
					break
				}
			}
		}
	}
}
