package bootstrap

import (
	"context"
	"fmt"

	agentapi "github.com/kubecrux/kubecrux/internal/agent/api"
	cfgpkg "github.com/kubecrux/kubecrux/internal/agent/config"
	k8sagent "github.com/kubecrux/kubecrux/internal/agent/kubernetes"
	loggerpkg "github.com/kubecrux/kubecrux/internal/agent/logger"
	"go.uber.org/zap"
)

type App struct {
	Config cfgpkg.Config
	Logger *zap.Logger
	Server *agentapi.Server
}

func New(ctx context.Context) (*App, error) {
	_ = ctx
	cfg, err := cfgpkg.Load()
	if err != nil {
		return nil, fmt.Errorf("load agent config: %w", err)
	}

	logger, err := loggerpkg.New(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("build agent logger: %w", err)
	}

	client, err := k8sagent.New(cfg.Kubernetes)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client: %w", err)
	}

	server := agentapi.New(cfg, logger, client)
	return &App{Config: cfg, Logger: logger, Server: server}, nil
}

func (a *App) Run() error {
	return a.Server.Run()
}

func (a *App) Shutdown(ctx context.Context) error {
	if a.Server != nil {
		if err := a.Server.Shutdown(ctx); err != nil {
			return err
		}
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
	}
	return nil
}
