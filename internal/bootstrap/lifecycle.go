package bootstrap

import (
	"context"
	"net/http"

	appvirtualization "github.com/soha/soha/internal/application/virtualization"
	appworkflow "github.com/soha/soha/internal/application/workflow"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
	dbinfra "github.com/soha/soha/internal/infrastructure/db"
	informerinfra "github.com/soha/soha/internal/infrastructure/informer"
	"go.uber.org/zap"
)

type App struct {
	Config                cfgpkg.Config
	Logger                *zap.Logger
	Database              *dbinfra.Store
	Informers             *informerinfra.Service
	WorkflowService       *appworkflow.Service
	VirtualizationService *appvirtualization.Service
	RateLimitBackend      interface{ Close() error }
	HTTP                  *http.Server
	cancel                context.CancelFunc
}

func (a *App) Run() error {
	err := a.HTTP.ListenAndServe()
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *App) Shutdown(ctx context.Context) error {
	if a.HTTP != nil {
		if err := a.HTTP.Shutdown(ctx); err != nil {
			return err
		}
	}
	if a.cancel != nil {
		a.cancel()
	}
	if a.WorkflowService != nil {
		if err := a.WorkflowService.Shutdown(ctx); err != nil {
			return err
		}
	}
	if a.VirtualizationService != nil {
		a.VirtualizationService.Shutdown()
	}
	if a.Informers != nil {
		a.Informers.Stop()
	}
	if a.Database != nil {
		_ = a.Database.Close()
	}
	if a.RateLimitBackend != nil {
		_ = a.RateLimitBackend.Close()
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
	}
	return nil
}
