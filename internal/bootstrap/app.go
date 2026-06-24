package bootstrap

import (
	"context"
	"fmt"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func New(ctx context.Context) (*App, error) {
	cfg, err := cfgpkg.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	infra, initErr := newInfrastructure(ctx, cfg)
	if initErr != nil {
		return nil, initErr
	}

	repos := newRepositories(cfg, infra.databaseStore)
	core, err := newCoreServices(ctx, cfg, infra, repos)
	if err != nil {
		infra.cancel()
		return nil, err
	}
	delivery := newDeliveryServices(infra.lifecycleCtx, cfg, infra, repos, core)
	gateway, err := newGatewayServices(ctx, cfg, repos, core, delivery)
	if err != nil {
		infra.cancel()
		return nil, err
	}
	handlers := newHandlers(cfg, infra, core, delivery, gateway)
	httpServer := newHTTPServer(cfg, infra.logger, handlers)

	return &App{
		Config:                cfg,
		Logger:                infra.logger,
		Database:              infra.databaseStore,
		Informers:             infra.informers,
		WorkflowService:       delivery.workflowService,
		VirtualizationService: delivery.virtualizationService,
		RateLimitBackend:      gateway.rateLimitBackend,
		HTTP:                  httpServer,
		cancel:                infra.cancel,
	}, nil
}
