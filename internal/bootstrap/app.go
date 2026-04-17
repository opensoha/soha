package bootstrap

import (
	"context"
	"fmt"
	"net/http"

	apiHandlers "github.com/kubecrux/kubecrux/internal/api/handlers"
	apiRoutes "github.com/kubecrux/kubecrux/internal/api/routes"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	appannouncement "github.com/kubecrux/kubecrux/internal/application/announcement"
	appregistry "github.com/kubecrux/kubecrux/internal/application/app"
	appaudit "github.com/kubecrux/kubecrux/internal/application/audit"
	appbuild "github.com/kubecrux/kubecrux/internal/application/build"
	appcatalog "github.com/kubecrux/kubecrux/internal/application/catalog"
	appcluster "github.com/kubecrux/kubecrux/internal/application/cluster"
	appcopilot "github.com/kubecrux/kubecrux/internal/application/copilot"
	appevent "github.com/kubecrux/kubecrux/internal/application/event"
	appidentity "github.com/kubecrux/kubecrux/internal/application/identity"
	appintegration "github.com/kubecrux/kubecrux/internal/application/integration"
	appmenu "github.com/kubecrux/kubecrux/internal/application/menu"
	appmonitoring "github.com/kubecrux/kubecrux/internal/application/monitoring"
	appoperation "github.com/kubecrux/kubecrux/internal/application/operation"
	appregistryconn "github.com/kubecrux/kubecrux/internal/application/registry"
	apprelease "github.com/kubecrux/kubecrux/internal/application/release"
	appresource "github.com/kubecrux/kubecrux/internal/application/resource"
	appscopegrant "github.com/kubecrux/kubecrux/internal/application/scopegrant"
	appsettings "github.com/kubecrux/kubecrux/internal/application/settings"
	appworkflow "github.com/kubecrux/kubecrux/internal/application/workflow"
	agentinfra "github.com/kubecrux/kubecrux/internal/infrastructure/agent"
	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
	dbinfra "github.com/kubecrux/kubecrux/internal/infrastructure/db"
	gitlabinfra "github.com/kubecrux/kubecrux/internal/infrastructure/gitlab"
	informerinfra "github.com/kubecrux/kubecrux/internal/infrastructure/informer"
	k8sinfra "github.com/kubecrux/kubecrux/internal/infrastructure/kubernetes"
	loggerinfra "github.com/kubecrux/kubecrux/internal/infrastructure/logger"
	mcpinfra "github.com/kubecrux/kubecrux/internal/infrastructure/mcp"
	"github.com/kubecrux/kubecrux/internal/platform/runtimeobs"
	"github.com/kubecrux/kubecrux/internal/policy"
	alertrepo "github.com/kubecrux/kubecrux/internal/repository/alert"
	announcementrepo "github.com/kubecrux/kubecrux/internal/repository/announcement"
	applicationrepo "github.com/kubecrux/kubecrux/internal/repository/application"
	auditrepo "github.com/kubecrux/kubecrux/internal/repository/auditlog"
	buildrepo "github.com/kubecrux/kubecrux/internal/repository/build"
	catalogrepo "github.com/kubecrux/kubecrux/internal/repository/catalog"
	clusterrepo "github.com/kubecrux/kubecrux/internal/repository/cluster"
	copilotrepo "github.com/kubecrux/kubecrux/internal/repository/copilot"
	eventrepo "github.com/kubecrux/kubecrux/internal/repository/eventstream"
	menurepo "github.com/kubecrux/kubecrux/internal/repository/menu"
	operationrepo "github.com/kubecrux/kubecrux/internal/repository/operationlog"
	policyrepo "github.com/kubecrux/kubecrux/internal/repository/policy"
	registryrepo "github.com/kubecrux/kubecrux/internal/repository/registry"
	releaserepo "github.com/kubecrux/kubecrux/internal/repository/release"
	scopegrantrepo "github.com/kubecrux/kubecrux/internal/repository/scopegrant"
	settingsrepo "github.com/kubecrux/kubecrux/internal/repository/settings"
	userrepo "github.com/kubecrux/kubecrux/internal/repository/user"
	workflowrepo "github.com/kubecrux/kubecrux/internal/repository/workflow"
	"go.uber.org/zap"
)

type App struct {
	Config          cfgpkg.Config
	Logger          *zap.Logger
	Database        *dbinfra.Store
	Informers       *informerinfra.Service
	WorkflowService *appworkflow.Service
	HTTP            *http.Server
	cancel          context.CancelFunc
}

func New(ctx context.Context) (*App, error) {
	cfg, err := cfgpkg.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	lifecycleCtx, cancel := context.WithCancel(ctx)

	logger, err := loggerinfra.New(cfg.Logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build logger: %w", err)
	}

	databaseStore, err := dbinfra.New(cfg.Database)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if cfg.Database.AutoMigrate {
		if err := databaseStore.MigrateFromFile(ctx, cfg.Database.ResolveMigrationPath()); err != nil {
			cancel()
			return nil, fmt.Errorf("run migration: %w", err)
		}
	}
	if cfg.Bootstrap.SeedDefaults {
		if err := seedDefaults(ctx, databaseStore); err != nil {
			cancel()
			return nil, fmt.Errorf("seed bootstrap data: %w", err)
		}
		if err := syncBootstrapRuntime(ctx, databaseStore, cfg); err != nil {
			cancel()
			return nil, fmt.Errorf("sync bootstrap runtime data: %w", err)
		}
	}
	if err := databaseStore.Ping(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	clusterManager := k8sinfra.NewManager(cfg.Kubernetes.Clusters)
	informers := informerinfra.New(clusterManager)
	if err := informers.Start(lifecycleCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("start informers: %w", err)
	}
	agentRegistry := agentinfra.NewRegistry(cfg.MCP.DefaultTimeout)
	mcpRegistry := mcpinfra.NewRegistry(cfg.MCP.DefaultTimeout)
	runtimeMetrics := runtimeobs.NewRegistry()

	auditRepository := auditrepo.New(databaseStore.DB())
	announcementRepository := announcementrepo.New(databaseStore.DB())
	eventRepository := eventrepo.New(databaseStore.DB())
	menuRepository := menurepo.New(databaseStore.DB())
	operationRepository := operationrepo.New(databaseStore.DB())
	alertRepository := alertrepo.New(databaseStore.DB())
	alertRepository.SetUpsertBatchSize(cfg.Runtime.AlertUpsertBatchSize)
	applicationRepository := applicationrepo.New(databaseStore.DB())
	buildRepository := buildrepo.New(databaseStore.DB())
	catalogRepository := catalogrepo.New(databaseStore.DB())
	workflowRepository := workflowrepo.New(databaseStore.DB())
	registryRepository := registryrepo.New(databaseStore.DB())
	releaseRepository := releaserepo.New(databaseStore.DB())
	copilotRepository := copilotrepo.New(databaseStore.DB())
	auditService := appaudit.New(auditRepository)
	announcementService := appannouncement.New(announcementRepository)
	menuService := appmenu.New(menuRepository)
	operationService := appoperation.New(operationRepository)
	identityRepository := userrepo.New(databaseStore.DB())
	settingsRepository := settingsrepo.New(databaseStore.DB())
	scopeGrantRepository := scopegrantrepo.New(databaseStore.DB())
	policyRepository := policyrepo.New(databaseStore.DB())
	clusterRepository := clusterrepo.New(databaseStore.DB())
	settingsService := appsettings.New(settingsRepository, cfg.Auth, cfg.Monitoring)

	identityService, err := appidentity.New(ctx, cfg.Auth, identityRepository, auditService, settingsService)
	if err != nil {
		return nil, fmt.Errorf("build identity service: %w", err)
	}
	policyEngine := policy.NewEngine()
	accessService := appaccess.New(policyEngine, policyRepository, scopeGrantRepository, catalogRepository)
	accessCatalogService := appaccess.NewCatalog(identityRepository, policyRepository, accessService, menuService)
	accessManagementService := appaccess.NewManagement(identityRepository, policyRepository)
	accessConsoleService := appaccess.NewConsole(accessCatalogService, accessManagementService)
	gitlabClient := gitlabinfra.New(cfg.GitLab)
	clusterService := appcluster.New(clusterManager, informers, agentRegistry, clusterRepository, accessService, auditService)
	clusterService.SetSyncLimit(cfg.Runtime.ClusterSyncParallelism)
	clusterService.SetInstrumentation(logger, runtimeMetrics)
	clusterService.Start(lifecycleCtx)
	resourceService := appresource.New(clusterManager, informers, agentRegistry, clusterRepository, accessService, auditService, settingsService)
	eventService := appevent.New(eventRepository)
	monitoringService := appmonitoring.New(alertRepository, eventRepository, cfg.Monitoring.Enabled, cfg.Monitoring.WebhookToken)
	applicationService := appregistry.New(applicationRepository, gitlabClient, accessService, auditService)
	buildService := appbuild.New(buildRepository, applicationRepository, accessService, eventRepository, auditService)
	catalogService := appcatalog.New(catalogRepository)
	scopeGrantService := appscopegrant.New(scopeGrantRepository)
	registryService := appregistryconn.New(registryRepository)
	releaseService := apprelease.New(releaseRepository, applicationRepository, clusterRepository, accessService, eventRepository, auditService, clusterManager, agentRegistry)
	workflowService := appworkflow.New(workflowRepository, applicationRepository, accessService, catalogRepository, buildService, releaseService, resourceService)
	workflowService.SetRuntimeOptions(cfg.Runtime.WorkflowWorkers, cfg.Runtime.WorkflowQueueSize, cfg.Runtime.WorkflowNodeParallelism)
	workflowService.SetInstrumentation(logger, runtimeMetrics)
	workflowService.Start(lifecycleCtx)
	copilotService := appcopilot.New(copilotRepository, clusterService, monitoringService, eventService, auditService, applicationRepository, buildRepository, releaseRepository, settingsService)
	copilotService.SetMCPRegistry(mcpRegistry)
	copilotService.SetInspectionParallelism(cfg.Runtime.CopilotInspectionParallelism)
	copilotService.SetInstrumentation(logger, runtimeMetrics)
	monitoringService.SetAutomation(copilotService)
	copilotService.Start(lifecycleCtx)
	integrationService := appintegration.New(mcpRegistry)

	systemHandler := apiHandlers.NewSystemHandler(databaseStore, runtimeMetrics)
	authHandler := apiHandlers.NewAuthHandler(identityService)
	announcementHandler := apiHandlers.NewAnnouncementHandler(announcementService)
	menuHandler := apiHandlers.NewMenuHandler(menuService)
	monitoringHandler := apiHandlers.NewMonitoringHandler(monitoringService)
	catalogHandler := apiHandlers.NewCatalogHandler(catalogService)
	applicationHandler := apiHandlers.NewApplicationHandler(applicationService)
	buildHandler := apiHandlers.NewBuildHandler(buildService)
	workflowHandler := apiHandlers.NewWorkflowHandler(workflowService)
	registryHandler := apiHandlers.NewRegistryHandler(registryService)
	releaseHandler := apiHandlers.NewReleaseHandler(releaseService)
	copilotHandler := apiHandlers.NewCopilotHandler(copilotService)
	accessHandler := apiHandlers.NewAccessHandler(accessConsoleService)
	scopeGrantHandler := apiHandlers.NewScopeGrantHandler(scopeGrantService)
	settingsHandler := apiHandlers.NewSettingsHandler(settingsService)
	platformHandler := apiHandlers.NewPlatformHandler(clusterService, resourceService, auditService, eventService, operationService, integrationService)
	httpServer := apiRoutes.New(cfg, logger, apiRoutes.Dependencies{
		System:        systemHandler,
		Platform:      platformHandler,
		Announcements: announcementHandler,
		Menu:          menuHandler,
		Monitoring:    monitoringHandler,
		Catalog:       catalogHandler,
		Applications:  applicationHandler,
		Builds:        buildHandler,
		Workflows:     workflowHandler,
		Registries:    registryHandler,
		Releases:      releaseHandler,
		Copilot:       copilotHandler,
		Access:        accessHandler,
		ScopeGrants:   scopeGrantHandler,
		Settings:      settingsHandler,
		Auth:          authHandler,
		Authn:         identityService,
	})

	return &App{
		Config:          cfg,
		Logger:          logger,
		Database:        databaseStore,
		Informers:       informers,
		WorkflowService: workflowService,
		HTTP:            httpServer,
		cancel:          cancel,
	}, nil
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
	if a.Informers != nil {
		a.Informers.Stop()
	}
	if a.Database != nil {
		_ = a.Database.Close()
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
	}
	return nil
}
