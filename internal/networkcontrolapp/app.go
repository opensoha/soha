package networkcontrolapp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	api "github.com/opensoha/soha/internal/api/networkcontrol"
	app "github.com/opensoha/soha/internal/application/networkcontrol"
	config "github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	loggerinfra "github.com/opensoha/soha/internal/infrastructure/logger"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkingestquery"
	"github.com/opensoha/soha/internal/networkprotocol"
	networkruntimerepository "github.com/opensoha/soha/internal/repository/networkruntime"
	runtimeconfigrepository "github.com/opensoha/soha/internal/repository/runtimeconfig"
	"go.uber.org/zap"
)

type App struct {
	Logger          *zap.Logger
	server          *http.Server
	store           *dbstore.Store
	service         *app.Service
	refreshInterval time.Duration
	stop            context.CancelFunc
	closeOnce       sync.Once
}

func New(ctx context.Context) (*App, error) {
	cfg, err := config.LoadNetworkControlConfig()
	if err != nil {
		return nil, fmt.Errorf("load network control config: %w", err)
	}
	logger, err := loggerinfra.New(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("initialize network control logger: %w", err)
	}
	store, err := dbstore.New(cfg.Database, logger)
	if err != nil {
		_ = logger.Sync()
		return nil, fmt.Errorf("initialize network control database: %w", err)
	}
	fail := func(err error) (*App, error) {
		_ = store.Close()
		_ = logger.Sync()
		return nil, err
	}
	if cfg.Database.AutoMigrate {
		if err := store.MigrateFromFile(ctx, cfg.Database.ResolveMigrationPath()); err != nil {
			return fail(fmt.Errorf("migrate network control database: %w", err))
		}
	}
	if err := store.Ping(ctx); err != nil {
		return fail(fmt.Errorf("ping network control database: %w", err))
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		return fail(err)
	}
	probeReader, err := newVPNProbeReader(cfg.IngestQuery)
	if err != nil {
		return fail(err)
	}
	service, err := app.New(networkruntimerepository.New(store.DB()), schemas, app.Options{
		MaxClockSkew: cfg.MaxClockSkew, ConfigurationTTL: cfg.ConfigurationTTL, LeaseTTL: cfg.LeaseTTL,
		CredentialEncryptionKeys: cfg.CredentialEncryptionKeys, VPNProbes: probeReader,
		LoadRuntimeConfig: runtimeconfigrepository.New(store.DB()).LoadState,
	})
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
	initialRefreshFailed := service.RefreshSnapshot(ctx) != nil
	if initialRefreshFailed {
		logger.Warn("network policy snapshot unavailable", zap.String("event", "network_control.snapshot.unavailable"))
	}
	refreshCtx, stop := context.WithCancel(context.Background())
	application := &App{
		Logger: logger, store: store, service: service, refreshInterval: cfg.SnapshotRefreshInterval, stop: stop,
		server: &http.Server{
			Addr: cfg.HTTP.Addr, Handler: router, TLSConfig: tlsConfig,
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.HTTP.ReadTimeout,
			WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout,
			MaxHeaderBytes: cfg.HTTP.MaxHeaderBytes,
		},
	}
	go application.refreshLoop(refreshCtx, initialRefreshFailed)
	return application, nil
}

func (a *App) Run() error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return err
	}
	a.Logger.Info("network control started", zap.String("event", "network_control.started"), zap.String("address", a.server.Addr))
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

func (a *App) refreshLoop(ctx context.Context, failed bool) {
	ticker := time.NewTicker(a.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := a.service.RefreshSnapshot(ctx)
			if err != nil && !failed {
				a.Logger.Warn("network policy snapshot refresh failed", zap.String("event", "network_control.snapshot.failed"))
			}
			if err == nil && failed {
				a.Logger.Info("network policy snapshot recovered", zap.String("event", "network_control.snapshot.recovered"))
			}
			failed = err != nil
		}
	}
}

func newVPNProbeReader(cfg config.NetworkIngestQueryConfig) (app.VPNProbeReader, error) {
	if !cfg.Configured() {
		return nil, nil
	}
	tlsConfig, err := networkidentity.LoadClientTLS(cfg.CertFile, cfg.KeyFile, cfg.CAFile, cfg.ServerName)
	if err != nil {
		return nil, err
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not configurable")
	}
	clone := transport.Clone()
	clone.TLSClientConfig = tlsConfig
	return networkingestquery.New(cfg.URL, &http.Client{Transport: clone, Timeout: cfg.Timeout}, cfg.MaxResponseBytes)
}
