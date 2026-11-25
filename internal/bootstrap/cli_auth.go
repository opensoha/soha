package bootstrap

import (
	"context"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	"go.uber.org/zap"
	"time"
)

func startCLIAuthCleanup(ctx context.Context, enabled bool, service *appidentity.Service, logger *zap.Logger) {
	interval := 5 * time.Second
	if !enabled {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run, cancel := context.WithTimeout(ctx, 4*time.Second)
				err := service.CleanupCLIAuth(run)
				cancel()
				if err != nil && ctx.Err() == nil {
					logger.Warn("CLI authentication cleanup failed", zap.String("event", "auth.cli.cleanup.failed"), zap.Error(err))
				}
			}
		}
	}()
}
