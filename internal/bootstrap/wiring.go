package bootstrap

import (
	"context"
	"fmt"
	"net/http"
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

type infrastructure struct {
	logger         *zap.Logger
	databaseStore  *dbinfra.Store
	clusterManager *k8sinfra.Manager
	informers      *informerinfra.Service
	agentRegistry  *agentinfra.Registry
	mcpRegistry    *mcpinfra.Registry
	runtimeMetrics *runtimeobs.Registry
	lifecycleCtx   context.Context
	cancel         context.CancelFunc
}

type repositories struct {
	auditRepository          *auditrepo.Repository
	announcementRepository   *announcementrepo.Repository
	eventRepository          *eventrepo.Repository
	menuRepository           *menurepo.Repository
	operationRepository      *operationrepo.Repository
	alertRepository          *alertrepo.Repository
	applicationRepository    *applicationrepo.Repository
	buildRepository          *buildrepo.Repository
	catalogRepository        *catalogrepo.Repository
	workflowRepository       *workflowrepo.Repository
	deliveryRepository       *deliveryrepo.Repository
	registryRepository       *registryrepo.Repository
	releaseRepository        *releaserepo.Repository
	copilotRepository        *copilotrepo.Repository
	identityRepository       *userrepo.Repository
	settingsRepository       *settingsrepo.Repository
	scopeGrantRepository     *scopegrantrepo.Repository
	policyRepository         *policyrepo.Repository
	clusterRepository        *clusterrepo.Repository
	virtualizationRepository *virtualizationrepo.Repository
	dockerRepository         *dockerrepo.Repository
	aiGatewayRepository      *aigatewayrepo.Repository
	pluginRepository         *pluginrepo.Repository
	portForwardRepository    *portforwardrepo.Repository
}

type coreServices struct {
	permissionResolver      *appaccess.PermissionResolver
	auditService            *appaudit.Service
	operationService        *appoperation.Service
	announcementService     *appannouncement.Service
	menuService             *appmenu.Service
	moduleService           *appmodule.Service
	settingsService         *appsettings.Service
	identityService         *appidentity.Service
	policyEngine            *policy.Engine
	accessService           *appaccess.Service
	accessCatalogService    *appaccess.CatalogService
	accessManagementService *appaccess.ManagementService
	accessConsoleService    *appaccess.ConsoleService
	gitlabClient            *gitlabinfra.Client
	clusterService          *appcluster.Service
	resourceService         *appresource.Service
	eventService            *appevent.Service
	monitoringService       *appmonitoring.Service
	applicationService      *appregistry.Service
	executionService        *appexecution.Service
	buildService            *appbuild.Service
	catalogService          *appcatalog.Service
	scopeGrantService       *appscopegrant.Service
	registryService         *appregistryconn.Service
	releaseService          *apprelease.Service
	integrationService      *appintegration.Service
	pluginService           *appplugin.Service
}

type deliveryServices struct {
	workflowService       *appworkflow.Service
	virtualizationService *appvirtualization.Service
	dockerService         *appdocker.Service
	copilotService        *appcopilot.Service
	deliveryService       *appdelivery.Service
}

type gatewayServices struct {
	aiGatewayService *appaigateway.Service
	rateLimitBackend interface{ Close() error }
}

type handlerSet struct {
	deps apiRoutes.Dependencies
}

func newInfrastructure(ctx context.Context, cfg cfgpkg.Config) (*infrastructure, error) {
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

	return &infrastructure{
		logger:         logger,
		databaseStore:  databaseStore,
		clusterManager: clusterManager,
		informers:      informers,
		agentRegistry:  agentinfra.NewRegistry(cfg.MCP.DefaultTimeout),
		mcpRegistry:    mcpinfra.NewRegistry(cfg.MCP.DefaultTimeout),
		runtimeMetrics: runtimeobs.NewRegistry(),
		lifecycleCtx:   lifecycleCtx,
		cancel:         cancel,
	}, nil
}

func newRepositories(cfg cfgpkg.Config, databaseStore *dbinfra.Store) *repositories {
	db := databaseStore.DB()
	alertRepository := alertrepo.New(db)
	alertRepository.SetUpsertBatchSize(cfg.Runtime.AlertUpsertBatchSize)
	return &repositories{
		auditRepository:          auditrepo.New(db),
		announcementRepository:   announcementrepo.New(db),
		eventRepository:          eventrepo.New(db),
		menuRepository:           menurepo.New(db),
		operationRepository:      operationrepo.New(db),
		alertRepository:          alertRepository,
		applicationRepository:    applicationrepo.New(db),
		buildRepository:          buildrepo.New(db),
		catalogRepository:        catalogrepo.New(db),
		workflowRepository:       workflowrepo.New(db),
		deliveryRepository:       deliveryrepo.New(db),
		registryRepository:       registryrepo.New(db),
		releaseRepository:        releaserepo.New(db),
		copilotRepository:        copilotrepo.New(db),
		identityRepository:       userrepo.New(db),
		settingsRepository:       settingsrepo.New(db),
		scopeGrantRepository:     scopegrantrepo.New(db),
		policyRepository:         policyrepo.New(db),
		clusterRepository:        clusterrepo.New(db),
		virtualizationRepository: virtualizationrepo.New(db),
		dockerRepository:         dockerrepo.New(db),
		aiGatewayRepository:      aigatewayrepo.New(db),
		pluginRepository:         pluginrepo.New(db),
		portForwardRepository:    portforwardrepo.New(db),
	}
}

func newCoreServices(ctx context.Context, cfg cfgpkg.Config, infra *infrastructure, repos *repositories) (*coreServices, error) {
	permissionResolver := appaccess.NewPermissionResolver(repos.policyRepository)
	auditService := appaudit.New(repos.auditRepository, permissionResolver)
	operationService := appoperation.New(repos.operationRepository, permissionResolver)
	announcementService := appannouncement.New(repos.announcementRepository, permissionResolver, auditService, operationService)
	menuService := appmenu.New(repos.menuRepository, permissionResolver, auditService, operationService)
	moduleService := appmodule.New(cfg.Modules)
	settingsService := appsettings.New(repos.settingsRepository, cfg.Auth, cfg.Monitoring, permissionResolver)

	identityService, err := appidentity.New(ctx, cfg.Auth, repos.identityRepository, auditService, operationService, settingsService, permissionResolver, repos.aiGatewayRepository)
	if err != nil {
		infra.cancel()
		return nil, fmt.Errorf("build identity service: %w", err)
	}
	policyEngine := policy.NewEngine()
	accessService := appaccess.New(policyEngine, repos.policyRepository, repos.scopeGrantRepository, repos.catalogRepository)
	accessCatalogService := appaccess.NewCatalog(repos.identityRepository, repos.policyRepository, accessService, menuService, permissionResolver)
	accessManagementService := appaccess.NewManagement(repos.identityRepository, repos.policyRepository, permissionResolver, auditService, operationService)
	accessConsoleService := appaccess.NewConsole(accessCatalogService, accessManagementService)
	gitlabClient := gitlabinfra.New(cfg.GitLab)

	clusterService := appcluster.New(infra.clusterManager, infra.informers, infra.agentRegistry, repos.clusterRepository, accessService, auditService, operationService)
	clusterService.SetSyncLimit(cfg.Runtime.ClusterSyncParallelism)
	clusterService.SetInstrumentation(infra.logger, infra.runtimeMetrics)
	clusterService.Start(infra.lifecycleCtx)

	resourceService := appresource.New(infra.clusterManager, infra.informers, infra.agentRegistry, repos.clusterRepository, accessService, permissionResolver, auditService, operationService, settingsService)
	resourceService.SetPortForwardRepository(repos.portForwardRepository)
	if err := resourceService.RestorePortForwards(ctx); err != nil {
		infra.logger.Warn("restore port forwards failed", zap.Error(err))
	}

	eventService := appevent.New(repos.eventRepository)
	eventService.SetAuditRecorder(auditService)
	eventService.SetConnectorEventSinkToken(cfg.AIGateway.ConnectorEventSink.Token)

	monitoringService := appmonitoring.New(repos.alertRepository, repos.eventRepository, repos.copilotRepository, permissionResolver, cfg.Monitoring.Enabled, cfg.Monitoring.WebhookToken)
	auditService.SetAlertSink(monitoringService)
	operationService.SetAlertSink(monitoringService)

	applicationService := appregistry.New(repos.applicationRepository, gitlabClient, accessService, auditService, operationService)
	applicationService.SetPermissionResolver(permissionResolver)

	executionService := appexecution.New(
		repos.deliveryRepository,
		repos.buildRepository,
		repos.releaseRepository,
		infra.clusterManager,
		cfg.Runtime.ExecutionJobClusterID,
		cfg.Runtime.ExecutionJobNamespace,
		cfg.Runtime.ExecutionJobImage,
		cfg.Runtime.ExecutionJobGitImage,
		cfg.Runtime.ExecutionJobTTLSeconds,
		cfg.Runtime.ExecutionRunnerToken,
		permissionResolver,
	)
	if cfg.Modules.Delivery.Enabled {
		executionService.Start(infra.lifecycleCtx)
	}

	buildService := appbuild.New(repos.buildRepository, repos.applicationRepository, repos.catalogRepository, executionService, accessService, repos.eventRepository, auditService, operationService)
	catalogService := appcatalog.New(repos.catalogRepository, accessService, repos.applicationRepository, permissionResolver, auditService, operationService)
	scopeGrantService := appscopegrant.New(repos.scopeGrantRepository, permissionResolver, auditService, operationService)
	registryService := appregistryconn.New(repos.registryRepository, permissionResolver, appregistryconn.WithCredentialEncryptionKey(cfg.Security.CredentialEncryptionKey))
	releaseService := apprelease.New(repos.releaseRepository, repos.applicationRepository, repos.catalogRepository, repos.clusterRepository, executionService, accessService, permissionResolver, repos.eventRepository, auditService, operationService, infra.clusterManager, infra.agentRegistry)
	integrationService := appintegration.New(infra.mcpRegistry)
	pluginService := appplugin.New(repos.pluginRepository, permissionResolver, auditService)

	return &coreServices{
		permissionResolver:      permissionResolver,
		auditService:            auditService,
		operationService:        operationService,
		announcementService:     announcementService,
		menuService:             menuService,
		moduleService:           moduleService,
		settingsService:         settingsService,
		identityService:         identityService,
		policyEngine:            policyEngine,
		accessService:           accessService,
		accessCatalogService:    accessCatalogService,
		accessManagementService: accessManagementService,
		accessConsoleService:    accessConsoleService,
		gitlabClient:            gitlabClient,
		clusterService:          clusterService,
		resourceService:         resourceService,
		eventService:            eventService,
		monitoringService:       monitoringService,
		applicationService:      applicationService,
		executionService:        executionService,
		buildService:            buildService,
		catalogService:          catalogService,
		scopeGrantService:       scopeGrantService,
		registryService:         registryService,
		releaseService:          releaseService,
		integrationService:      integrationService,
		pluginService:           pluginService,
	}, nil
}

func newDeliveryServices(lifecycleCtx context.Context, cfg cfgpkg.Config, infra *infrastructure, repos *repositories, core *coreServices) *deliveryServices {
	workflowService := appworkflow.New(repos.workflowRepository, repos.applicationRepository, core.accessService, core.permissionResolver, repos.catalogRepository, core.buildService, core.releaseService, core.resourceService)
	workflowService.SetArtifactStore(repos.deliveryRepository)
	workflowService.SetRuntimeOptions(cfg.Runtime.WorkflowWorkers, cfg.Runtime.WorkflowQueueSize, cfg.Runtime.WorkflowNodeParallelism)
	workflowService.SetInstrumentation(infra.logger, infra.runtimeMetrics)
	workflowService.SetAlertMutator(core.monitoringService)
	if cfg.Modules.Delivery.Enabled {
		workflowService.Start(lifecycleCtx)
	}

	copilotService := appcopilot.New(repos.copilotRepository, core.clusterService, core.monitoringService, core.eventService, core.auditService, repos.applicationRepository, repos.buildRepository, repos.releaseRepository, core.settingsService, core.permissionResolver)
	copilotService.SetMCPRegistry(infra.mcpRegistry)
	copilotService.SetInspectionParallelism(cfg.Runtime.CopilotInspectionParallelism)
	copilotService.SetInstrumentation(infra.logger, infra.runtimeMetrics)
	core.monitoringService.SetWorkflowExecutor(workflowService)
	core.monitoringService.SetAutomation(copilotService)
	if cfg.Modules.Monitoring.Enabled {
		core.monitoringService.Start(lifecycleCtx)
	}
	if cfg.Modules.AI.Enabled {
		copilotService.Start(lifecycleCtx)
	}

	virtualizationService := appvirtualization.New(
		repos.virtualizationRepository,
		map[string]appvirtualization.Adapter{
			appvirtualization.ProviderKubeVirt: virtualizationinfra.NewKubeVirtAdapter(infra.clusterManager),
			appvirtualization.ProviderPVE:      virtualizationinfra.NewPVEAdapter(nil),
		},
		core.permissionResolver,
		core.operationService,
		appvirtualization.Options{
			CredentialEncryptionKey: cfg.Security.CredentialEncryptionKey,
			StartupSyncEnabled:      cfg.Runtime.VirtualizationStartupSync,
			WorkerInterval:          cfg.Runtime.VirtualizationWorkerInterval,
			SyncConcurrency:         cfg.Runtime.VirtualizationSyncConcurrency,
		},
	)
	virtualizationService.SetInstrumentation(infra.runtimeMetrics)
	if cfg.Modules.Virtualization.Enabled {
		virtualizationService.Start(lifecycleCtx)
	}

	dockerService := appdocker.New(
		repos.dockerRepository,
		core.permissionResolver,
		core.operationService,
		appdocker.WithHostProvisioner(dockerHostProvisioner{virtualization: virtualizationService}),
		appdocker.WithRuntimeBearerToken(cfg.Runtime.ExecutionRunnerToken),
	)
	copilotService.SetAgentRuntimeReaders(core.executionService, core.resourceService, dockerService, virtualizationService, core.monitoringService)

	deliveryService := appdelivery.New(core.applicationService, core.catalogService, core.buildService, workflowService, core.releaseService, repos.deliveryRepository, core.executionService, core.resourceService, core.permissionResolver)
	deliveryService.SetRecorders(core.auditService, core.operationService)
	core.catalogService.SetTemplateUsageRuntimeReaders(appcatalog.TemplateUsageRuntimeReaders{
		Builds:    core.buildService,
		Workflows: workflowService,
		Releases:  core.releaseService,
		Delivery:  repos.deliveryRepository,
	})

	return &deliveryServices{
		workflowService:       workflowService,
		virtualizationService: virtualizationService,
		dockerService:         dockerService,
		copilotService:        copilotService,
		deliveryService:       deliveryService,
	}
}

func newGatewayServices(ctx context.Context, cfg cfgpkg.Config, repos *repositories, core *coreServices, delivery *deliveryServices) (*gatewayServices, error) {
	aiGatewayService := appaigateway.NewWithDeps(appaigateway.ServiceDeps{
		Permissions:     core.permissionResolver,
		Audit:           core.auditService,
		PersonalTokens:  repos.aiGatewayRepository,
		ServiceAccounts: repos.aiGatewayRepository,
		Clients:         repos.aiGatewayRepository,
		ToolGrants:      repos.aiGatewayRepository,
		AccessPolicies:  repos.aiGatewayRepository,
		SkillBindings:   repos.aiGatewayRepository,
		AuditLogs:       repos.aiGatewayRepository,
		RateLimits:      repos.aiGatewayRepository,
		Approvals:       repos.aiGatewayRepository,
		LLMRelay:        repos.aiGatewayRepository,
		RelayConfig: appaigateway.LLMRelayConfig{
			Enabled:                     cfg.AIGateway.Relay.Enabled,
			DefaultTimeout:              cfg.AIGateway.Relay.DefaultTimeout,
			StreamTimeout:               cfg.AIGateway.Relay.StreamTimeout,
			HealthCheckEnabled:          cfg.AIGateway.Relay.HealthCheckEnabled,
			HealthCheckInterval:         cfg.AIGateway.Relay.HealthCheckInterval,
			MaxRequestBodyBytes:         int64(cfg.AIGateway.Relay.MaxRequestBodyMB) << 20,
			AllowInsecureUpstreamHTTP:   cfg.AIGateway.Relay.AllowInsecureUpstreamHTTP,
			AllowPrivateUpstreamHosts:   cfg.AIGateway.Relay.AllowPrivateUpstreamHosts,
			IncludeUsageForOpenAIStream: cfg.AIGateway.Relay.IncludeUsageForOpenAIStream,
			CredentialEncryptionKey:     cfg.Security.CredentialEncryptionKey,
		},
	})
	var rateLimitBackend interface{ Close() error }
	if strings.EqualFold(strings.TrimSpace(cfg.AIGateway.RateLimit.Backend), "redis") {
		redisRateLimitBackend, err := ratelimitinfra.NewRedisBackend(cfg.AIGateway.RateLimit)
		if err != nil {
			return nil, fmt.Errorf("build AI Gateway Redis rate limit backend: %w", err)
		}
		rateLimitBackend = redisRateLimitBackend
		aiGatewayService.SetRateLimitBackend(redisRateLimitBackend)
	}
	aiGatewayService.SetDeliveryServices(core.applicationService, delivery.deliveryService)
	aiGatewayService.SetCatalogService(core.catalogService)
	aiGatewayService.SetResourceService(core.resourceService)
	aiGatewayService.SetAnalysisArtifactRecorder(delivery.copilotService)
	aiGatewayService.SetOperationRecorder(core.operationService)
	aiGatewayService.SetOnCallResolver(core.monitoringService)
	delivery.copilotService.SetWorkbenchModelInvoker(aiGatewayService)
	aiGatewayService.StartRelayHealthChecks(ctx)
	if err := registerAIGatewayConnectorRuntimes(ctx, aiGatewayService, cfg.AIGateway); err != nil {
		if rateLimitBackend != nil {
			_ = rateLimitBackend.Close()
		}
		return nil, err
	}
	return &gatewayServices{
		aiGatewayService: aiGatewayService,
		rateLimitBackend: rateLimitBackend,
	}, nil
}

func newHandlers(cfg cfgpkg.Config, infra *infrastructure, core *coreServices, delivery *deliveryServices, gateway *gatewayServices) *handlerSet {
	return &handlerSet{
		deps: apiRoutes.Dependencies{
			System:         apiHandlers.NewSystemHandler(infra.databaseStore, infra.runtimeMetrics),
			Platform:       apiHandlers.NewPlatformHandler(core.clusterService, core.resourceService, core.auditService, core.eventService, core.operationService, core.integrationService),
			Announcements:  apiHandlers.NewAnnouncementHandler(core.announcementService),
			Menu:           apiHandlers.NewMenuHandler(core.menuService),
			Module:         apiHandlers.NewModuleHandler(core.moduleService),
			Monitoring:     apiHandlers.NewMonitoringHandler(core.monitoringService),
			Catalog:        apiHandlers.NewCatalogHandler(core.catalogService),
			Delivery:       apiHandlers.NewDeliveryHandler(delivery.deliveryService, cfg.Runtime.ExecutionRunnerToken),
			Applications:   apiHandlers.NewApplicationHandler(core.applicationService),
			Builds:         apiHandlers.NewBuildHandler(core.buildService),
			Workflows:      apiHandlers.NewWorkflowHandler(delivery.workflowService),
			Registries:     apiHandlers.NewRegistryHandler(core.registryService),
			Releases:       apiHandlers.NewReleaseHandler(core.releaseService),
			Copilot:        apiHandlers.NewCopilotHandler(delivery.copilotService, cfg.Runtime.ExecutionRunnerToken),
			AIGateway:      apiHandlers.NewAIGatewayHandler(gateway.aiGatewayService),
			Plugins:        apiHandlers.NewPluginHandler(core.pluginService),
			Virtualization: apiHandlers.NewVirtualizationHandler(delivery.virtualizationService),
			Docker:         apiHandlers.NewDockerHandler(delivery.dockerService, cfg.Runtime.ExecutionRunnerToken),
			Access:         apiHandlers.NewAccessHandler(core.accessConsoleService),
			ScopeGrants:    apiHandlers.NewScopeGrantHandler(core.scopeGrantService),
			Settings:       apiHandlers.NewSettingsHandler(core.settingsService, core.permissionResolver),
			Auth:           apiHandlers.NewAuthHandler(core.identityService, core.accessConsoleService, core.settingsService, cfg.Auth),
			Authn:          core.identityService,
		},
	}
}

func newHTTPServer(cfg cfgpkg.Config, logger *zap.Logger, handlers *handlerSet) *http.Server {
	return apiRoutes.New(cfg, logger, handlers.deps)
}
