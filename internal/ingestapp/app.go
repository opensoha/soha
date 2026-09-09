package ingestapp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	api "github.com/opensoha/soha/internal/api/networkingest"
	app "github.com/opensoha/soha/internal/application/networkingest"
	config "github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	loggerinfra "github.com/opensoha/soha/internal/infrastructure/logger"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	repository "github.com/opensoha/soha/internal/repository/networkingest"
	"go.uber.org/zap"
)

type App struct {
	Logger     *zap.Logger
	server     *http.Server
	store      *dbstore.Store
	repository *repository.Repository
	retention  time.Duration
	stop       context.CancelFunc
	closeOnce  sync.Once
}

func New(ctx context.Context) (*App, error) {
	cfg, err := config.LoadIngestConfig()
	if err != nil {
		return nil, fmt.Errorf("load ingest config: %w", err)
	}
	logger, err := loggerinfra.New(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("initialize ingest logger: %w", err)
	}
	store, err := dbstore.New(cfg.Database, logger)
	if err != nil {
		_ = logger.Sync()
		return nil, fmt.Errorf("initialize ingest database: %w", err)
	}
	fail := func(err error) (*App, error) {
		_ = store.Close()
		_ = logger.Sync()
		return nil, err
	}
	if cfg.Database.AutoMigrate {
		if err := store.MigrateFromFile(ctx, cfg.Database.ResolveMigrationPath()); err != nil {
			return fail(fmt.Errorf("migrate ingest database: %w", err))
		}
	}
	if err := store.Ping(ctx); err != nil {
		return fail(fmt.Errorf("ping ingest database: %w", err))
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		return fail(err)
	}
	repo := repository.New(store.DB())
	service, err := app.New(repo, schemas, app.Options{MaxEventsPerBatch: cfg.MaxEventsPerBatch, MaxClockSkew: cfg.MaxClockSkew, Retention: cfg.Retention})
	if err != nil {
		return fail(err)
	}
	router, err := api.NewRouter(service, store, api.Options{MaxBodyBytes: cfg.MaxBodyBytes, RequestsPerMinute: cfg.RequestsPerMinute})
	if err != nil {
		return fail(err)
	}
	tlsConfig, err := networkidentity.LoadServerTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.ClientCAFile)
	if err != nil {
		return fail(err)
	}
	cleanupCtx, stop := context.WithCancel(context.Background())
	application := &App{
		Logger: logger, store: store, repository: repo, retention: cfg.Retention, stop: stop,
		server: &http.Server{
			Addr: cfg.HTTP.Addr, Handler: router, TLSConfig: tlsConfig,
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.HTTP.ReadTimeout,
			WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout,
			MaxHeaderBytes: cfg.HTTP.MaxHeaderBytes,
		},
	}
	go application.cleanupLoop(cleanupCtx)
	return application, nil
}

func (a *App) Run() error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return err
	}
	a.Logger.Info("network ingest started", zap.String("event", "network_ingest.started"), zap.String("address", a.server.Addr))
	err = a.server.Serve(tls.NewListener(listener, a.server.TLSConfig))
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *App) Shutdown(ctx context.Context) error {
	var closeErr error
	a.closeOnce.Do(func() {
		a.stop()
		shutdownErr := a.server.Shutdown(ctx)
		storeErr := a.store.Close()
		_ = a.Logger.Sync()
		closeErr = errors.Join(shutdownErr, storeErr)
	})
	return closeErr
}

func (a *App) cleanupLoop(ctx context.Context) {
	a.cleanupExpired(ctx)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.cleanupExpired(ctx)
		}
	}
}

func (a *App) cleanupExpired(ctx context.Context) {
	removed, err := a.repository.DeleteBefore(ctx, time.Now().UTC().Add(-a.retention))
	if err != nil {
		a.Logger.Warn("network ingest retention cleanup failed", zap.String("event", "network_ingest.retention.failed"))
		return
	}
	if removed > 0 {
		a.Logger.Info("network ingest retention cleanup completed", zap.String("event", "network_ingest.retention.completed"), zap.Int64("removed", removed))
	}
}
