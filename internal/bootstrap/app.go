package bootstrap

import (
	"context"
	"fmt"
	"strings"

	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	apiRoutes "github.com/opensoha/soha/internal/api/routes"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	appannouncement "github.com/opensoha/soha/internal/application/announcement"
	appregistry "github.com/opensoha/soha/internal/application/app"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appbuild "github.com/opensoha/soha/internal/application/build"
	appcatalog "github.com/opensoha/soha/internal/application/catalog"
	appcluster "github.com/opensoha/soha/internal/application/cluster"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	appdelivery "github.com/opensoha/soha/internal/application/delivery"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	appevent "github.com/opensoha/soha/internal/application/event"
	appexecution "github.com/opensoha/soha/internal/application/execution"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	appintegration "github.com/opensoha/soha/internal/application/integration"
	appmenu "github.com/opensoha/soha/internal/application/menu"
	appmodule "github.com/opensoha/soha/internal/application/module"
	appmonitoring "github.com/opensoha/soha/internal/application/monitoring"
	appoperation "github.com/opensoha/soha/internal/application/operation"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
	appregistryconn "github.com/opensoha/soha/internal/application/registry"
	apprelease "github.com/opensoha/soha/internal/application/release"
	appresource "github.com/opensoha/soha/internal/application/resource"
	appscopegrant "github.com/opensoha/soha/internal/application/scopegrant"
	appsettings "github.com/opensoha/soha/internal/application/settings"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	appworkflow "github.com/opensoha/soha/internal/application/workflow"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	dbinfra "github.com/opensoha/soha/internal/infrastructure/db"
	gitlabinfra "github.com/opensoha/soha/internal/infrastructure/gitlab"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	loggerinfra "github.com/opensoha/soha/internal/infrastructure/logger"
	mcpinfra "github.com/opensoha/soha/internal/infrastructure/mcp"
	ratelimitinfra "github.com/opensoha/soha/internal/infrastructure/ratelimit"
	virtualizationinfra "github.com/opensoha/soha/internal/infrastructure/virtualization"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
	"github.com/opensoha/soha/internal/policy"
	aigatewayrepo "github.com/opensoha/soha/internal/repository/aigateway"
	alertrepo "github.com/opensoha/soha/internal/repository/alert"
	announcementrepo "github.com/opensoha/soha/internal/repository/announcement"
	applicationrepo "github.com/opensoha/soha/internal/repository/application"
	auditrepo "github.com/opensoha/soha/internal/repository/auditlog"
	buildrepo "github.com/opensoha/soha/internal/repository/build"
	catalogrepo "github.com/opensoha/soha/internal/repository/catalog"
	clusterrepo "github.com/opensoha/soha/internal/repository/cluster"
	copilotrepo "github.com/opensoha/soha/internal/repository/copilot"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
	eventrepo "github.com/opensoha/soha/internal/repository/eventstream"
	menurepo "github.com/opensoha/soha/internal/repository/menu"
	operationrepo "github.com/opensoha/soha/internal/repository/operationlog"
	pluginrepo "github.com/opensoha/soha/internal/repository/plugin"
	policyrepo "github.com/opensoha/soha/internal/repository/policy"
	portforwardrepo "github.com/opensoha/soha/internal/repository/portforward"
	registryrepo "github.com/opensoha/soha/internal/repository/registry"
	releaserepo "github.com/opensoha/soha/internal/repository/release"
	scopegrantrepo "github.com/opensoha/soha/internal/repository/scopegrant"
	settingsrepo "github.com/opensoha/soha/internal/repository/settings"
	userrepo "github.com/opensoha/soha/internal/repository/user"
	virtualizationrepo "github.com/opensoha/soha/internal/repository/virtualization"
	workflowrepo "github.com/opensoha/soha/internal/repository/workflow"
	"go.uber.org/zap"
)

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
	aiGatewayRepository := aigatewayrepo.New(databaseStore.DB())
	pluginRepository := pluginrepo.New(databaseStore.DB())
	permissionResolver := appaccess.NewPermissionResolver(policyRepository)
	auditService := appaudit.New(auditRepository, permissionResolver)
	operationService := appoperation.New(operationRepository, permissionResolver)
	announcementService := appannouncement.New(announcementRepository, permissionResolver, auditService, operationService)
	menuService := appmenu.New(menuRepository, permissionResolver, auditService, operationService)
	moduleService := appmodule.New(cfg.Modules)
	settingsService := appsettings.New(settingsRepository, cfg.Auth, cfg.Monitoring, permissionResolver)

	identityService, err := appidentity.New(ctx, cfg.Auth, identityRepository, auditService, operationService, settingsService, permissionResolver, aiGatewayRepository)
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
	eventService.SetAuditRecorder(auditService)
	eventService.SetConnectorEventSinkToken(cfg.AIGateway.ConnectorEventSink.Token)
	monitoringService := appmonitoring.New(alertRepository, eventRepository, copilotRepository, permissionResolver, cfg.Monitoring.Enabled, cfg.Monitoring.WebhookToken)
	auditService.SetAlertSink(monitoringService)
	operationService.SetAlertSink(monitoringService)
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
	registryService := appregistryconn.New(registryRepository, permissionResolver, appregistryconn.WithCredentialEncryptionKey(cfg.Security.CredentialEncryptionKey))
	releaseService := apprelease.New(releaseRepository, applicationRepository, catalogRepository, clusterRepository, executionService, accessService, permissionResolver, eventRepository, auditService, operationService, clusterManager, agentRegistry)
	workflowService := appworkflow.New(workflowRepository, applicationRepository, accessService, permissionResolver, catalogRepository, buildService, releaseService, resourceService)
	workflowService.SetArtifactStore(deliveryRepository)
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
	deliveryService.SetRecorders(auditService, operationService)
	catalogService.SetTemplateUsageRuntimeReaders(appcatalog.TemplateUsageRuntimeReaders{
		Builds:    buildService,
		Workflows: workflowService,
		Releases:  releaseService,
		Delivery:  deliveryRepository,
	})
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
	dockerService := appdocker.New(
		dockerRepository,
		permissionResolver,
		operationService,
		appdocker.WithHostProvisioner(dockerHostProvisioner{virtualization: virtualizationService}),
		appdocker.WithRuntimeBearerToken(cfg.Runtime.ExecutionRunnerToken),
	)
	copilotService.SetAgentRuntimeReaders(executionService, resourceService, dockerService, virtualizationService, monitoringService)
	aiGatewayService := appaigateway.New(permissionResolver, auditService, aiGatewayRepository)
	var rateLimitBackend interface{ Close() error }
	if strings.EqualFold(strings.TrimSpace(cfg.AIGateway.RateLimit.Backend), "redis") {
		redisRateLimitBackend, err := ratelimitinfra.NewRedisBackend(cfg.AIGateway.RateLimit)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("build AI Gateway Redis rate limit backend: %w", err)
		}
		rateLimitBackend = redisRateLimitBackend
		aiGatewayService.SetRateLimitBackend(redisRateLimitBackend)
	}
	aiGatewayService.SetDeliveryServices(applicationService, deliveryService)
	aiGatewayService.SetCatalogService(catalogService)
	aiGatewayService.SetResourceService(resourceService)
	aiGatewayService.SetAnalysisArtifactRecorder(copilotService)
	aiGatewayService.SetOperationRecorder(operationService)
	aiGatewayService.SetOnCallResolver(monitoringService)
	if err := registerAIGatewayConnectorRuntimes(ctx, aiGatewayService, cfg.AIGateway); err != nil {
		cancel()
		return nil, err
	}
	pluginService := appplugin.New(pluginRepository, permissionResolver, auditService)

	systemHandler := apiHandlers.NewSystemHandler(databaseStore, runtimeMetrics)
	authHandler := apiHandlers.NewAuthHandler(identityService, accessConsoleService, settingsService, cfg.Auth)
	aiGatewayHandler := apiHandlers.NewAIGatewayHandler(aiGatewayService)
	pluginHandler := apiHandlers.NewPluginHandler(pluginService)
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
		AIGateway:      aiGatewayHandler,
		Plugins:        pluginHandler,
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
		RateLimitBackend:      rateLimitBackend,
		HTTP:                  httpServer,
		cancel:                cancel,
	}, nil
}
