package bootstrap

import (
	"context"
	"fmt"
	"slices"

	agentapi "github.com/opensoha/soha-agent/internal/agent/api"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	loggerpkg "github.com/opensoha/soha-agent/internal/agent/logger"
	runnerpkg "github.com/opensoha/soha-agent/internal/agent/runner"
	sessionpkg "github.com/opensoha/soha-agent/internal/agent/session"
	"go.uber.org/zap"
)

type App struct {
	Config  cfgpkg.Config
	Logger  *zap.Logger
	Server  *agentapi.Server
	Runner  *runnerpkg.Runner
	Session *sessionpkg.Manager
	cancel  context.CancelFunc
}

func New(ctx context.Context) (*App, error) {
	lifecycleCtx, cancel := context.WithCancel(ctx)
	cfg, err := cfgpkg.Load()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("load agent config: %w", err)
	}

	logger, err := loggerpkg.New(cfg.Logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build agent logger: %w", err)
	}

	var client *k8sagent.Client
	if cfg.Kubernetes.Enabled {
		client, err = k8sagent.New(cfg.Kubernetes)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("build kubernetes client: %w", err)
		}
		client.StartResourceEvents(lifecycleCtx)
	} else {
		logger.Named("bootstrap").Info("agent kubernetes client disabled; platform proxy routes will be unavailable",
			zap.String("event", "agent.kubernetes.disabled"),
		)
	}

	controlPlane := cfg.ControlPlane
	if controlPlane.AgentRuntime.Environment == "" {
		controlPlane.AgentRuntime.Environment = cfg.Kubernetes.Environment
	}
	if len(controlPlane.AgentRuntime.Labels) == 0 && len(cfg.Kubernetes.Labels) > 0 {
		controlPlane.AgentRuntime.Labels = make(map[string]string, len(cfg.Kubernetes.Labels))
		for key, value := range cfg.Kubernetes.Labels {
			controlPlane.AgentRuntime.Labels[key] = value
		}
	}
	runner := runnerpkg.New(controlPlane, logger)
	runner.SetSSHExecutor(cfg.Kubernetes.ID, slices.Contains(cfg.Security.AllowedActions, "runtime.vm_ssh.connect") && slices.Contains(cfg.Security.AllowedActions, "runtime.vm_ssh.execute"))
	if client != nil {
		runner.SetManifestExecutor(client, cfg.Kubernetes.ID, cfg.Auth.BearerToken)
		runner.SetHelmExecutor(client, cfg.Kubernetes.ID, cfg.Auth.BearerToken, cfg.Security.AllowedActions)
		client.SetExecutionClaimCapabilities(runner.ExecutionClaimCapabilities())
	}
	runner.Start(lifecycleCtx)
	server := agentapi.New(cfg, logger, client, runner)
	sessionManager, err := sessionpkg.New(controlPlane, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build Agent session: %w", err)
	}
	sessionManager.Start(lifecycleCtx)
	return &App{Config: cfg, Logger: logger, Server: server, Runner: runner, Session: sessionManager, cancel: cancel}, nil
}

func (a *App) Run() error {
	return a.Server.Run()
}

func (a *App) Shutdown(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
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
