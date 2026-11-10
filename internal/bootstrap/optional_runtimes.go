package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/infrastructure/dshruntime"
	"go.uber.org/zap"
)

// optionalRuntimeServices owns startup-only secrets, clients and the memory worker.
// It is closed before the database; no runtime setting exposes service credentials.
type optionalRuntimeServices struct {
	dsh      *dshruntime.Client
	callback *apiHandlers.DSHRuntimeHandler
	memory   *MemoryBackendRuntime
}

func readRuntimeSecret(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open runtime secret file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("runtime secret must be a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 8194))
	if err != nil || len(raw) > 8193 {
		return "", fmt.Errorf("runtime secret file is invalid")
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 32 || len(token) > 8192 {
		return "", fmt.Errorf("runtime secret must contain 32 to 8192 printable characters")
	}
	for _, c := range token {
		if c <= 32 || c >= 127 {
			return "", fmt.Errorf("runtime secret must contain printable non-whitespace characters")
		}
	}
	return token, nil
}

func configureOptionalRuntimes(cfg cfgpkg.Config, repos *repositories, delivery *deliveryServices) (*optionalRuntimeServices, error) {
	result := &optionalRuntimeServices{}
	if cfg.DSHRuntime.Enabled {
		if durableStorageKeys(cfg).Active().ID() == "" {
			return nil, fmt.Errorf("configure dsh: security.protected_durable_storage and an active credential encryption key are required")
		}
		token, err := readRuntimeSecret(cfg.DSHRuntime.ServiceTokenFile)
		if err != nil {
			return nil, fmt.Errorf("configure dsh: %w", err)
		}
		client, err := dshruntime.New(dshruntime.Config{BaseURL: cfg.DSHRuntime.BaseURL, ServiceToken: token, Timeout: cfg.DSHRuntime.Timeout, AllowInsecureHTTP: cfg.DSHRuntime.AllowInsecureHTTP})
		if err != nil {
			return nil, fmt.Errorf("configure dsh: %w", err)
		}
		result.dsh = client
		adapter, err := dshruntime.NewAdapter(client)
		if err == nil {
			err = delivery.copilotService.ConfigureDSHRuntime(adapter, repos.copilotRepository)
		}
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("bind dsh runtime: %w", err)
		}
		result.callback, err = apiHandlers.NewDSHRuntimeHandler(delivery.copilotService, token)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("bind dsh callback: %w", err)
		}
	}
	if cfg.Hindsight.Enabled {
		token, err := readRuntimeSecret(cfg.Hindsight.TokenFile)
		if err != nil {
			result.Close(context.Background())
			return nil, fmt.Errorf("configure Hindsight: %w", err)
		}
		result.memory, err = ConfigureMemoryBackend(MemoryBackendConfig{Enabled: true, BaseURL: cfg.Hindsight.BaseURL, Token: token, InstanceNamespace: cfg.Hindsight.InstanceNamespace, Timeout: cfg.Hindsight.Timeout}, repos.memoryRepository, delivery.memoryService, delivery.copilotService)
		if err != nil {
			result.Close(context.Background())
			return nil, fmt.Errorf("configure Hindsight: %w", err)
		}
	}
	return result, nil
}

func (r *optionalRuntimeServices) Start(ctx context.Context, logger *zap.Logger) {
	if r == nil {
		return
	}
	r.memory.Start(ctx, func(err error) {
		logger.Named("memory-sync").Warn("memory backend reconciliation failed", zap.Error(err))
	})
}
func (r *optionalRuntimeServices) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	err := r.memory.Stop(ctx)
	if r.dsh != nil {
		r.dsh.Close()
	}
	return err
}
