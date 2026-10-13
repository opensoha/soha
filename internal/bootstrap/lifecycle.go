package bootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	appworkflow "github.com/opensoha/soha/internal/application/workflow"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	dbinfra "github.com/opensoha/soha/internal/infrastructure/db"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
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
	PersonalGuest         *http.Server
	cancel                context.CancelFunc
}

func (a *App) Run() error {
	if a.PersonalGuest != nil {
		return a.runWithPersonalGuest()
	}
	err := a.HTTP.ListenAndServe()
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *App) runWithPersonalGuest() error {
	if a.PersonalGuest.TLSConfig == nil || a.PersonalGuest.TLSConfig.ClientAuth != tls.RequireAndVerifyClientCert || a.PersonalGuest.TLSConfig.MinVersion < tls.VersionTLS13 || len(a.PersonalGuest.TLSConfig.Certificates) == 0 {
		return fmt.Errorf("personal guest requires verified client TLS")
	}
	guestListener, err := net.Listen("tcp", a.PersonalGuest.Addr)
	if err != nil {
		return fmt.Errorf("listen personal guest: %w", err)
	}
	listener, err := net.Listen("tcp", a.HTTP.Addr)
	if err != nil {
		return errors.Join(fmt.Errorf("listen management HTTP: %w", err), guestListener.Close())
	}
	results := make(chan error, 2)
	go func() { results <- a.HTTP.Serve(listener) }()
	go func() { results <- a.PersonalGuest.ServeTLS(guestListener, "", "") }()
	first := <-results
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := errors.Join(a.PersonalGuest.Shutdown(ctx), a.HTTP.Shutdown(ctx))
	if shutdownErr != nil {
		shutdownErr = errors.Join(shutdownErr, a.PersonalGuest.Close(), a.HTTP.Close())
	}
	second := <-results
	return errors.Join(httpServerError(first), httpServerError(second), shutdownErr)
}

func httpServerError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *App) Shutdown(ctx context.Context) error {
	var shutdownErr error
	if a.PersonalGuest != nil {
		shutdownErr = errors.Join(shutdownErr, a.PersonalGuest.Shutdown(ctx))
	}
	if a.HTTP != nil {
		if err := a.HTTP.Shutdown(ctx); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}
	if a.cancel != nil {
		a.cancel()
	}
	if a.WorkflowService != nil {
		if err := a.WorkflowService.Shutdown(ctx); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}
	if a.VirtualizationService != nil {
		a.VirtualizationService.Shutdown()
	}
	if a.Informers != nil {
		a.Informers.Stop()
	}
	if a.Database != nil {
		shutdownErr = errors.Join(shutdownErr, a.Database.Close())
	}
	if a.RateLimitBackend != nil {
		_ = a.RateLimitBackend.Close()
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
	}
	return shutdownErr
}
