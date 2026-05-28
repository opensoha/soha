package bootstrap

import (
	"context"
	"fmt"
	"net/http"

	apiHandlers "github.com/soha/soha/internal/api/handlers"
	apiRoutes "github.com/soha/soha/internal/api/routes"
	appaccess "github.com/soha/soha/internal/application/access"
	appannouncement "github.com/soha/soha/internal/application/announcement"
	appregistry "github.com/soha/soha/internal/application/app"
	appaudit "github.com/soha/soha/internal/application/audit"
	appbuild "github.com/soha/soha/internal/application/build"
	appcatalog "github.com/soha/soha/internal/application/catalog"
	appcluster "github.com/soha/soha/internal/application/cluster"
	appcopilot "github.com/soha/soha/internal/application/copilot"
	appdelivery "github.com/soha/soha/internal/application/delivery"
	appdocker "github.com/soha/soha/internal/application/docker"
	appevent "github.com/soha/soha/internal/application/event"
	appexecution "github.com/soha/soha/internal/application/execution"
	appidentity "github.com/soha/soha/internal/application/identity"
	appintegration "github.com/soha/soha/internal/application/integration"
	appmenu "github.com/soha/soha/internal/application/menu"
	appmodule "github.com/soha/soha/internal/application/module"
	appmonitoring "github.com/soha/soha/internal/application/monitoring"
	appoperation "github.com/soha/soha/internal/application/operation"
	appregistryconn "github.com/soha/soha/internal/application/registry"
	apprelease "github.com/soha/soha/internal/application/release"
	appresource "github.com/soha/soha/internal/application/resource"
	appscopegrant "github.com/soha/soha/internal/application/scopegrant"
	appsettings "github.com/soha/soha/internal/application/settings"
	appvirtualization "github.com/soha/soha/internal/application/virtualization"
	appworkflow "github.com/soha/soha/internal/application/workflow"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainvirtualization "github.com/soha/soha/internal/domain/virtualization"
	agentinfra "github.com/soha/soha/internal/infrastructure/agent"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
	dbinfra "github.com/soha/soha/internal/infrastructure/db"
	gitlabinfra "github.com/soha/soha/internal/infrastructure/gitlab"
	informerinfra "github.com/soha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/soha/soha/internal/infrastructure/kubernetes"
	loggerinfra "github.com/soha/soha/internal/infrastructure/logger"
	mcpinfra "github.com/soha/soha/internal/infrastructure/mcp"
	virtualizationinfra "github.com/soha/soha/internal/infrastructure/virtualization"
	"github.com/soha/soha/internal/platform/runtimeobs"
	"github.com/soha/soha/internal/policy"
	alertrepo "github.com/soha/soha/internal/repository/alert"
	announcementrepo "github.com/soha/soha/internal/repository/announcement"
	applicationrepo "github.com/soha/soha/internal/repository/application"
	auditrepo "github.com/soha/soha/internal/repository/auditlog"
	buildrepo "github.com/soha/soha/internal/repository/build"
	catalogrepo "github.com/soha/soha/internal/repository/catalog"
	clusterrepo "github.com/soha/soha/internal/repository/cluster"
	copilotrepo "github.com/soha/soha/internal/repository/copilot"
	deliveryrepo "github.com/soha/soha/internal/repository/delivery"
	dockerrepo "github.com/soha/soha/internal/repository/docker"
	eventrepo "github.com/soha/soha/internal/repository/eventstream"
	menurepo "github.com/soha/soha/internal/repository/menu"
	operationrepo "github.com/soha/soha/internal/repository/operationlog"
	policyrepo "github.com/soha/soha/internal/repository/policy"
	portforwardrepo "github.com/soha/soha/internal/repository/portforward"
	registryrepo "github.com/soha/soha/internal/repository/registry"
	releaserepo "github.com/soha/soha/internal/repository/release"
	scopegrantrepo "github.com/soha/soha/internal/repository/scopegrant"
	settingsrepo "github.com/soha/soha/internal/repository/settings"
	userrepo "github.com/soha/soha/internal/repository/user"
	virtualizationrepo "github.com/soha/soha/internal/repository/virtualization"
	workflowrepo "github.com/soha/soha/internal/repository/workflow"
	"go.uber.org/zap"
)

type App struct {
	Config                cfgpkg.Config
	Logger                *zap.Logger
	Database              *dbinfra.Store
	Informers             *informerinfra.Service
	WorkflowService       *appworkflow.Service
	VirtualizationService *appvirtualization.Service
	HTTP                  *http.Server
	cancel                context.CancelFunc
}

type dockerHostProvisioner struct {
	virtualization interface {
		CreateVM(context.Context, domainidentity.Principal, appvirtualization.CreateVMInput) (domainvirtualization.Task, error)
	}
}

func (p dockerHostProvisioner) ProvisionDockerHost(ctx context.Context, principal domainidentity.Principal, input appdocker.HostProvisionInput) (appdocker.HostProvisionTask, error) {
	task, err := p.virtualization.CreateVM(ctx, principal, appvirtualization.CreateVMInput{
		ConnectionID:      input.ConnectionID,
		Name:              input.Name,
		CPU:               input.CPU,
		MemoryMiB:         input.MemoryMiB,
		DiskGiB:           input.DiskGiB,
		BootImageID:       input.BootImageID,
		ImageID:           input.ImageID,
		FlavorID:          input.FlavorID,
		Network:           input.Network,
		StartAfterCreate:  input.StartAfterCreate,
		TemplateID:        input.TemplateID,
		ProviderParams:    input.ProviderParams,
		ProviderExtraJSON: input.ProviderExtraJSON,
	})
	if err != nil {
		return appdocker.HostProvisionTask{}, err
	}
	return appdocker.HostProvisionTask{
		ID:           task.ID,
		Provider:     task.Provider,
		ConnectionID: task.ConnectionID,
		Status:       task.Status,
	}, nil
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
		if err := seedDefaults(ctx, databaseStore, cfg); err != nil {
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
	deliveryRepository := deliveryrepo.New(databaseStore.DB())
	registryRepository := registryrepo.New(databaseStore.DB())
	releaseRepository := releaserepo.New(databaseStore.DB())
	copilotRepository := copilotrepo.New(databaseStore.DB())
	identityRepository := userrepo.New(databaseStore.DB())
	settingsRepository := settingsrepo.New(databaseStore.DB())
	scopeGrantRepository := scopegrantrepo.New(databaseStore.DB())
	policyRepository := policyrepo.New(databaseStore.DB())
	clusterRepository := clusterrepo.New(databaseStore.DB())
	virtualizationRepository := virtualizationrepo.New(databaseStore.DB())
	dockerRepository := dockerrepo.New(databaseStore.DB())
	permissionResolver := appaccess.NewPermissionResolver(policyRepository)
	auditService := appaudit.New(auditRepository, permissionResolver)
	operationService := appoperation.New(operationRepository, permissionResolver)
	announcementService := appannouncement.New(announcementRepository, permissionResolver, auditService, operationService)
	menuService := appmenu.New(menuRepository, permissionResolver, auditService, operationService)
	moduleService := appmodule.New(cfg.Modules)
	settingsService := appsettings.New(settingsRepository, cfg.Auth, cfg.Monitoring, permissionResolver)

	identityService, err := appidentity.New(ctx, cfg.Auth, identityRepository, auditService, operationService, settingsService, permissionResolver)
	if err != nil {
		return nil, fmt.Errorf("build identity service: %w", err)
	}
	policyEngine := policy.NewEngine()
	accessService := appaccess.New(policyEngine, policyRepository, scopeGrantRepository, catalogRepository)
	accessCatalogService := appaccess.NewCatalog(identityRepository, policyRepository, accessService, menuService, permissionResolver)
	accessManagementService := appaccess.NewManagement(identityRepository, policyRepository, permissionResolver, auditService, operationService)
	accessConsoleService := appaccess.NewConsole(accessCatalogService, accessManagementService)
	gitlabClient := gitlabinfra.New(cfg.GitLab)
	clusterService := appcluster.New(clusterManager, informers, agentRegistry, clusterRepository, accessService, auditService, operationService)
	clusterService.SetSyncLimit(cfg.Runtime.ClusterSyncParallelism)
	clusterService.SetInstrumentation(logger, runtimeMetrics)
	clusterService.Start(lifecycleCtx)
	resourceService := appresource.New(clusterManager, informers, agentRegistry, clusterRepository, accessService, permissionResolver, auditService, operationService, settingsService)
	portForwardRepository := portforwardrepo.New(databaseStore.DB())
	resourceService.SetPortForwardRepository(portForwardRepository)
	if err := resourceService.RestorePortForwards(ctx); err != nil {
		logger.Warn("restore port forwards failed", zap.Error(err))
	}
	eventService := appevent.New(eventRepository)
	monitoringService := appmonitoring.New(alertRepository, eventRepository, copilotRepository, permissionResolver, cfg.Monitoring.Enabled, cfg.Monitoring.WebhookToken)
	applicationService := appregistry.New(applicationRepository, gitlabClient, accessService, auditService, operationService)
	applicationService.SetPermissionResolver(permissionResolver)
	executionService := appexecution.New(
		deliveryRepository,
		buildRepository,
		releaseRepository,
		clusterManager,
		cfg.Runtime.ExecutionJobClusterID,
		cfg.Runtime.ExecutionJobNamespace,
		cfg.Runtime.ExecutionJobImage,
		cfg.Runtime.ExecutionJobGitImage,
		cfg.Runtime.ExecutionJobTTLSeconds,
		cfg.Runtime.ExecutionRunnerToken,
		permissionResolver,
	)
	if cfg.Modules.Delivery.Enabled {
		executionService.Start(lifecycleCtx)
	}
	buildService := appbuild.New(buildRepository, applicationRepository, catalogRepository, executionService, accessService, eventRepository, auditService, operationService)
	catalogService := appcatalog.New(catalogRepository, accessService, applicationRepository, permissionResolver, auditService, operationService)
	scopeGrantService := appscopegrant.New(scopeGrantRepository, permissionResolver, auditService, operationService)
	registryService := appregistryconn.New(registryRepository, permissionResolver)
	releaseService := apprelease.New(releaseRepository, applicationRepository, catalogRepository, clusterRepository, executionService, accessService, permissionResolver, eventRepository, auditService, operationService, clusterManager, agentRegistry)
	workflowService := appworkflow.New(workflowRepository, applicationRepository, accessService, permissionResolver, catalogRepository, buildService, releaseService, resourceService)
	workflowService.SetRuntimeOptions(cfg.Runtime.WorkflowWorkers, cfg.Runtime.WorkflowQueueSize, cfg.Runtime.WorkflowNodeParallelism)
	workflowService.SetInstrumentation(logger, runtimeMetrics)
	workflowService.SetAlertMutator(monitoringService)
	if cfg.Modules.Delivery.Enabled {
		workflowService.Start(lifecycleCtx)
	}
	monitoringService.SetWorkflowExecutor(workflowService)
	if cfg.Modules.Monitoring.Enabled {
		monitoringService.Start(lifecycleCtx)
	}
	deliveryService := appdelivery.New(applicationService, catalogService, buildService, workflowService, releaseService, deliveryRepository, executionService, resourceService, permissionResolver)
	copilotService := appcopilot.New(copilotRepository, clusterService, monitoringService, eventService, auditService, applicationRepository, buildRepository, releaseRepository, settingsService, permissionResolver)
	copilotService.SetMCPRegistry(mcpRegistry)
	copilotService.SetInspectionParallelism(cfg.Runtime.CopilotInspectionParallelism)
	copilotService.SetInstrumentation(logger, runtimeMetrics)
	monitoringService.SetAutomation(copilotService)
	if cfg.Modules.AI.Enabled {
		copilotService.Start(lifecycleCtx)
	}
	integrationService := appintegration.New(mcpRegistry)
	virtualizationService := appvirtualization.New(
		virtualizationRepository,
		map[string]appvirtualization.Adapter{
			appvirtualization.ProviderKubeVirt: virtualizationinfra.NewKubeVirtAdapter(clusterManager),
			appvirtualization.ProviderPVE:      virtualizationinfra.NewPVEAdapter(nil),
		},
		permissionResolver,
		operationService,
		appvirtualization.Options{
			CredentialEncryptionKey: cfg.Security.CredentialEncryptionKey,
			StartupSyncEnabled:      true,
		},
	)
	virtualizationService.SetInstrumentation(runtimeMetrics)
	if cfg.Modules.Virtualization.Enabled {
		virtualizationService.Start(lifecycleCtx)
	}
	dockerService := appdocker.New(dockerRepository, permissionResolver, operationService, appdocker.WithHostProvisioner(dockerHostProvisioner{virtualization: virtualizationService}))
	copilotService.SetAgentRuntimeReaders(executionService, resourceService, dockerService, virtualizationService, monitoringService)

	systemHandler := apiHandlers.NewSystemHandler(databaseStore, runtimeMetrics)
	authHandler := apiHandlers.NewAuthHandler(identityService, accessConsoleService, settingsService)
	announcementHandler := apiHandlers.NewAnnouncementHandler(announcementService)
	menuHandler := apiHandlers.NewMenuHandler(menuService)
	moduleHandler := apiHandlers.NewModuleHandler(moduleService)
	monitoringHandler := apiHandlers.NewMonitoringHandler(monitoringService)
	catalogHandler := apiHandlers.NewCatalogHandler(catalogService)
	deliveryHandler := apiHandlers.NewDeliveryHandler(deliveryService, cfg.Runtime.ExecutionRunnerToken)
	applicationHandler := apiHandlers.NewApplicationHandler(applicationService)
	buildHandler := apiHandlers.NewBuildHandler(buildService)
	workflowHandler := apiHandlers.NewWorkflowHandler(workflowService)
	registryHandler := apiHandlers.NewRegistryHandler(registryService)
	releaseHandler := apiHandlers.NewReleaseHandler(releaseService)
	copilotHandler := apiHandlers.NewCopilotHandler(copilotService, cfg.Runtime.ExecutionRunnerToken)
	virtualizationHandler := apiHandlers.NewVirtualizationHandler(virtualizationService)
	dockerHandler := apiHandlers.NewDockerHandler(dockerService, cfg.Runtime.ExecutionRunnerToken)
	accessHandler := apiHandlers.NewAccessHandler(accessConsoleService)
	scopeGrantHandler := apiHandlers.NewScopeGrantHandler(scopeGrantService)
	settingsHandler := apiHandlers.NewSettingsHandler(settingsService, permissionResolver)
	platformHandler := apiHandlers.NewPlatformHandler(clusterService, resourceService, auditService, eventService, operationService, integrationService)
	httpServer := apiRoutes.New(cfg, logger, apiRoutes.Dependencies{
		System:         systemHandler,
		Platform:       platformHandler,
		Announcements:  announcementHandler,
		Menu:           menuHandler,
		Module:         moduleHandler,
		Monitoring:     monitoringHandler,
		Catalog:        catalogHandler,
		Delivery:       deliveryHandler,
		Applications:   applicationHandler,
		Builds:         buildHandler,
		Workflows:      workflowHandler,
		Registries:     registryHandler,
		Releases:       releaseHandler,
		Copilot:        copilotHandler,
		Virtualization: virtualizationHandler,
		Docker:         dockerHandler,
		Access:         accessHandler,
		ScopeGrants:    scopeGrantHandler,
		Settings:       settingsHandler,
		Auth:           authHandler,
		Authn:          identityService,
	})

	return &App{
		Config:                cfg,
		Logger:                logger,
		Database:              databaseStore,
		Informers:             informers,
		WorkflowService:       workflowService,
		VirtualizationService: virtualizationService,
		HTTP:                  httpServer,
		cancel:                cancel,
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
	if a.VirtualizationService != nil {
		a.VirtualizationService.Shutdown()
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
