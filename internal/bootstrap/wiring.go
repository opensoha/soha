package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	accesshandler "github.com/opensoha/soha/internal/api/handlers/access"
	directorysynchandler "github.com/opensoha/soha/internal/api/handlers/directorysync"
	providerportalhandler "github.com/opensoha/soha/internal/api/handlers/providerportal"
	apiRoutes "github.com/opensoha/soha/internal/api/routes"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	appannouncement "github.com/opensoha/soha/internal/application/announcement"
	appregistry "github.com/opensoha/soha/internal/application/app"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appbuild "github.com/opensoha/soha/internal/application/build"
	appcatalog "github.com/opensoha/soha/internal/application/catalog"
	appcluster "github.com/opensoha/soha/internal/application/cluster"
	appcompute "github.com/opensoha/soha/internal/application/compute"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	appdelivery "github.com/opensoha/soha/internal/application/delivery"
	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	appevent "github.com/opensoha/soha/internal/application/event"
	appexecution "github.com/opensoha/soha/internal/application/execution"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	appidentityprovider "github.com/opensoha/soha/internal/application/identityprovider"
	appintegration "github.com/opensoha/soha/internal/application/integration"
	appmenu "github.com/opensoha/soha/internal/application/menu"
	appmodule "github.com/opensoha/soha/internal/application/module"
	appmonitoring "github.com/opensoha/soha/internal/application/monitoring"
	appoperation "github.com/opensoha/soha/internal/application/operation"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
	appproviderportal "github.com/opensoha/soha/internal/application/providerportal"
	appregistryconn "github.com/opensoha/soha/internal/application/registry"
	apprelease "github.com/opensoha/soha/internal/application/release"
	appresource "github.com/opensoha/soha/internal/application/resource"
	appscopegrant "github.com/opensoha/soha/internal/application/scopegrant"
	appsettings "github.com/opensoha/soha/internal/application/settings"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	appworkflow "github.com/opensoha/soha/internal/application/workflow"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	directorysyncdomain "github.com/opensoha/soha/internal/domain/directorysync"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	dbinfra "github.com/opensoha/soha/internal/infrastructure/db"
	feishudirectory "github.com/opensoha/soha/internal/infrastructure/directoryconnector/feishu"
	executionbackendinfra "github.com/opensoha/soha/internal/infrastructure/executionbackend"
	gitlabinfra "github.com/opensoha/soha/internal/infrastructure/gitlab"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	loggerinfra "github.com/opensoha/soha/internal/infrastructure/logger"
	mcpinfra "github.com/opensoha/soha/internal/infrastructure/mcp"
	mcplogsinfra "github.com/opensoha/soha/internal/infrastructure/mcp/logs"
	mcpmetricsinfra "github.com/opensoha/soha/internal/infrastructure/mcp/metrics"
	mcptracesinfra "github.com/opensoha/soha/internal/infrastructure/mcp/traces"
	ratelimitinfra "github.com/opensoha/soha/internal/infrastructure/ratelimit"
	releasebackendinfra "github.com/opensoha/soha/internal/infrastructure/releasebackend"
	resourcebackendinfra "github.com/opensoha/soha/internal/infrastructure/resourcebackend"
	virtualizationinfra "github.com/opensoha/soha/internal/infrastructure/virtualization"
	"github.com/opensoha/soha/internal/platform/keyring"
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
	directorysyncrepo "github.com/opensoha/soha/internal/repository/directorysync"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
	eventrepo "github.com/opensoha/soha/internal/repository/eventstream"
	identityproviderrepo "github.com/opensoha/soha/internal/repository/identityprovider"
	menurepo "github.com/opensoha/soha/internal/repository/menu"
	operationrepo "github.com/opensoha/soha/internal/repository/operationlog"
	pluginrepo "github.com/opensoha/soha/internal/repository/plugin"
	policyrepo "github.com/opensoha/soha/internal/repository/policy"
	portforwardrepo "github.com/opensoha/soha/internal/repository/portforward"
	providerportalrepo "github.com/opensoha/soha/internal/repository/providerportal"
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
	auditRepository            *auditrepo.Repository
	announcementRepository     *announcementrepo.Repository
	eventRepository            *eventrepo.Repository
	menuRepository             *menurepo.Repository
	operationRepository        *operationrepo.Repository
	alertRepository            *alertrepo.Repository
	applicationRepository      *applicationrepo.Repository
	buildRepository            *buildrepo.Repository
	catalogRepository          *catalogrepo.Repository
	workflowRepository         *workflowrepo.Repository
	deliveryRepository         *deliveryrepo.Repository
	registryRepository         *registryrepo.Repository
	releaseRepository          *releaserepo.Repository
	copilotRepository          *copilotrepo.Repository
	identityRepository         *userrepo.Repository
	settingsRepository         *settingsrepo.Repository
	scopeGrantRepository       *scopegrantrepo.Repository
	policyRepository           *policyrepo.Repository
	clusterRepository          *clusterrepo.Repository
	virtualizationRepository   *virtualizationrepo.Repository
	dockerRepository           *dockerrepo.Repository
	aiGatewayRepository        *aigatewayrepo.Repository
	pluginRepository           *pluginrepo.Repository
	identityProviderRepository *identityproviderrepo.Repository
	providerPortalRepository   *providerportalrepo.Repository
	portForwardRepository      *portforwardrepo.Repository
	directorySyncRepository    *directorysyncrepo.Repository
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
	identityProviderService *appidentityprovider.Service
	providerPortalService   *appproviderportal.Service
	directorySyncService    *appdirectorysync.Service
	directorySyncConnectors *directorysynchandler.Registry
}

type deliveryServices struct {
	workflowService       *appworkflow.Service
	computeService        *appcompute.Service
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

func newInfrastructure(ctx context.Context, cfg *cfgpkg.Config) (*infrastructure, error) {
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
	cleanupDatabase := func() {
		_ = databaseStore.Close()
		cancel()
	}
	if cfg.Database.AutoMigrate {
		if err := databaseStore.MigrateFromFile(ctx, cfg.Database.ResolveMigrationPath()); err != nil {
			cleanupDatabase()
			return nil, fmt.Errorf("run migration: %w", err)
		}
	}
	if cfg.Bootstrap.SeedDefaults {
		if err := seedDefaults(ctx, databaseStore, *cfg); err != nil {
			cleanupDatabase()
			return nil, fmt.Errorf("seed bootstrap data: %w", err)
		}
		if err := syncBootstrapRuntime(ctx, databaseStore, *cfg); err != nil {
			cleanupDatabase()
			return nil, fmt.Errorf("sync bootstrap runtime data: %w", err)
		}
	}
	if err := databaseStore.Ping(ctx); err != nil {
		cleanupDatabase()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	clusterManager := k8sinfra.NewManager(cfg.Kubernetes.Clusters)
	informers := informerinfra.New(clusterManager)
	if err := informers.Start(lifecycleCtx); err != nil {
		cleanupDatabase()
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
		auditRepository:            auditrepo.New(db),
		announcementRepository:     announcementrepo.New(db),
		eventRepository:            eventrepo.New(db),
		menuRepository:             menurepo.New(db),
		operationRepository:        operationrepo.New(db),
		alertRepository:            alertRepository,
		applicationRepository:      applicationrepo.New(db),
		buildRepository:            buildrepo.New(db),
		catalogRepository:          catalogrepo.New(db),
		workflowRepository:         workflowrepo.New(db),
		deliveryRepository:         deliveryrepo.New(db),
		registryRepository:         registryrepo.New(db),
		releaseRepository:          releaserepo.New(db),
		copilotRepository:          copilotrepo.New(db),
		identityRepository:         userrepo.New(db),
		settingsRepository:         settingsrepo.New(db),
		scopeGrantRepository:       scopegrantrepo.New(db),
		policyRepository:           policyrepo.New(db),
		clusterRepository:          clusterrepo.New(db),
		virtualizationRepository:   virtualizationrepo.New(db),
		dockerRepository:           dockerrepo.New(db),
		aiGatewayRepository:        aigatewayrepo.New(db),
		pluginRepository:           pluginrepo.New(db),
		identityProviderRepository: identityproviderrepo.New(db),
		providerPortalRepository:   providerportalrepo.New(db),
		portForwardRepository:      portforwardrepo.New(db),
		directorySyncRepository:    directorysyncrepo.New(db, cfg.Security.CredentialEncryptionKeys),
	}
}

func newCoreServices(ctx context.Context, cfg cfgpkg.Config, infra *infrastructure, repos *repositories) (*coreServices, error) {
	permissionResolver := appaccess.NewPermissionResolver(repos.policyRepository)
	auditService := appaudit.New(repos.auditRepository, permissionResolver)
	operationService := appoperation.New(repos.operationRepository, permissionResolver)
	announcementService := appannouncement.New(repos.announcementRepository, permissionResolver, auditService, operationService)
	menuService := appmenu.New(repos.menuRepository, permissionResolver, auditService, operationService)
	moduleService := appmodule.New(cfg.Modules)
	settingsService := appsettings.New(repos.settingsRepository, cfg.Monitoring, permissionResolver)
	directorySyncConnectors := directorysynchandler.NewRegistry(directorysynchandler.TokenResolver(
		feishudirectory.NewTenantTokenResolver(settingsService, nil, ""),
	), settingsService, repos.directorySyncRepository)

	identityService, err := appidentity.New(appidentity.Dependencies{
		Config:   cfg.Auth,
		Accounts: repos.identityRepository, Passwords: repos.identityRepository,
		Authorization: repos.identityRepository, RoleBindings: repos.identityRepository,
		TeamBindings: repos.identityRepository, Identities: repos.identityRepository,
		Sessions: repos.identityRepository, SessionAdmin: repos.identityRepository,
		EphemeralTokens: repos.identityRepository,
		Audit:           auditService, Operations: operationService, Settings: settingsService,
		Permissions: permissionResolver, Gateway: repos.aiGatewayRepository,
	})
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
	directorySyncService := appdirectorysync.New(repos.directorySyncRepository, directorysyncrepo.NewDatabaseProjector(infra.databaseStore.DB()))
	directoryScheduler := appdirectorysync.NewScheduler(repos.directorySyncRepository, directorySyncService, func(_ context.Context, connection directorysyncdomain.Connection) (appdirectorysync.Connector, error) {
		return directorySyncConnectors.Connector(connection.ProviderType)
	})
	directoryScheduler.SetInstrumentation(infra.runtimeMetrics)
	go directoryScheduler.Start(infra.lifecycleCtx)

	platformCore, err := newPlatformCoreServices(ctx, cfg, infra, repos, permissionResolver, auditService, operationService, accessService, settingsService)
	if err != nil {
		infra.cancel()
		return nil, err
	}

	deliveryCore, err := newDeliveryCoreServices(cfg, infra, repos, permissionResolver, auditService, operationService, accessService, gitlabClient, identityService)
	if err != nil {
		return nil, err
	}

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
		clusterService:          platformCore.cluster,
		resourceService:         platformCore.resources,
		eventService:            platformCore.events,
		monitoringService:       platformCore.monitoring,
		applicationService:      deliveryCore.applications,
		executionService:        deliveryCore.execution,
		buildService:            deliveryCore.builds,
		catalogService:          deliveryCore.catalog,
		scopeGrantService:       deliveryCore.scopeGrants,
		registryService:         deliveryCore.registries,
		releaseService:          deliveryCore.releases,
		integrationService:      deliveryCore.integration,
		pluginService:           deliveryCore.plugins,
		identityProviderService: deliveryCore.identityProvider,
		providerPortalService:   deliveryCore.providerPortal,
		directorySyncService:    directorySyncService,
		directorySyncConnectors: directorySyncConnectors,
	}, nil
}

type platformCoreServices struct {
	cluster    *appcluster.Service
	resources  *appresource.Service
	events     *appevent.Service
	monitoring *appmonitoring.Service
}

func newPlatformCoreServices(ctx context.Context, cfg cfgpkg.Config, infra *infrastructure, repos *repositories, permissions *appaccess.PermissionResolver, audit *appaudit.Service, operations *appoperation.Service, access *appaccess.Service, settings *appsettings.Service) (*platformCoreServices, error) {
	clusterService, err := appcluster.New(
		infra.clusterManager, infra.clusterManager, infra.informers,
		func(connection domaincluster.Connection) (appcluster.AgentSummaryClient, error) {
			return infra.agentRegistry.ClientFor(connection)
		},
		repos.clusterRepository, access, audit, operations,
	)
	if err != nil {
		return nil, fmt.Errorf("build cluster service: %w", err)
	}
	clusterService.SetSyncLimit(cfg.Runtime.ClusterSyncParallelism)
	clusterService.SetInstrumentation(infra.logger, infra.runtimeMetrics)
	clusterService.Start(infra.lifecycleCtx)

	resourceClusters := resourcebackendinfra.NewClusters(infra.clusterManager)
	resourceDirect := resourcebackendinfra.NewDirect(resourceClusters, resourcebackendinfra.NewCache(infra.informers))
	resourceService := appresource.New(appresource.Dependencies{
		Clusters: resourceClusters, Agents: resourcebackendinfra.NewAgentClients(infra.agentRegistry), Connections: repos.clusterRepository,
		Authorizer: access, Permissions: permissions, Audit: audit, Operations: operations, Settings: settings, PortForwards: repos.portForwardRepository,
		DirectCustom: resourceDirect, DirectConfiguration: resourceDirect, DirectEvents: resourceDirect, DirectGeneric: resourceDirect,
		DirectGateway: resourceDirect, DirectHelm: resourceDirect, DirectInventory: resourceDirect, DirectNetwork: resourceDirect,
		DirectPods: resourceDirect, DirectRBAC: resourceDirect, DirectStorage: resourceDirect, DirectTunnel: resourceDirect, DirectWorkloads: resourceDirect,
	})
	if err := resourceService.PortForwards().RestorePortForwards(ctx); err != nil {
		infra.logger.Warn("restore port forwards failed", zap.Error(err))
	}
	eventService := newEventService(repos.eventRepository, audit, cfg.AIGateway.ConnectorEventSink.Token)
	monitoringService, err := appmonitoring.New(appmonitoring.Dependencies{
		AlertReader: repos.alertRepository, AlertWriter: repos.alertRepository, Channels: repos.alertRepository, Silences: repos.alertRepository,
		DeliveryLogs: repos.alertRepository, Rules: repos.alertRepository, RuleRuns: repos.alertRepository, AlertEvents: repos.alertRepository,
		NotificationPolicies: repos.alertRepository, NotificationTemplates: repos.alertRepository, HealingPolicies: repos.alertRepository, HealingRuns: repos.alertRepository,
		OnCallSchedules: repos.alertRepository, OnCallRotations: repos.alertRepository, OnCallEscalations: repos.alertRepository, OnCallAssignments: repos.alertRepository,
		Integrations: repos.alertRepository, Events: repos.eventRepository, DataSources: repos.copilotRepository, Permissions: permissions,
		Enabled: cfg.Monitoring.Enabled, WebhookKeys: cfg.Monitoring.WebhookKeys,
	}, appmonitoring.WithTelemetryBackends(mcplogsinfra.DefaultRegistry(), mcpmetricsinfra.DefaultRegistry(), mcptracesinfra.DefaultRegistry()))
	if err != nil {
		return nil, fmt.Errorf("build monitoring service: %w", err)
	}
	audit.SetAlertSink(monitoringService)
	operations.SetAlertSink(monitoringService)
	return &platformCoreServices{cluster: clusterService, resources: resourceService, events: eventService, monitoring: monitoringService}, nil
}

type deliveryCoreServices struct {
	applications     *appregistry.Service
	execution        *appexecution.Service
	builds           *appbuild.Service
	catalog          *appcatalog.Service
	scopeGrants      *appscopegrant.Service
	registries       *appregistryconn.Service
	releases         *apprelease.Service
	integration      *appintegration.Service
	plugins          *appplugin.Service
	identityProvider *appidentityprovider.Service
	providerPortal   *appproviderportal.Service
}

func newDeliveryCoreServices(cfg cfgpkg.Config, infra *infrastructure, repos *repositories, permissions *appaccess.PermissionResolver, audit *appaudit.Service, operations *appoperation.Service, access *appaccess.Service, gitlab *gitlabinfra.Client, identity *appidentity.Service) (*deliveryCoreServices, error) {
	applications := appregistry.New(repos.applicationRepository, gitlab, access, audit, operations)
	applications.SetPermissionResolver(permissions)
	executionService := appexecution.New(
		repos.deliveryRepository, repos.buildRepository, repos.releaseRepository, executionbackendinfra.NewClusters(infra.clusterManager),
		cfg.Runtime.ExecutionJobClusterID, cfg.Runtime.ExecutionJobNamespace, cfg.Runtime.ExecutionJobImage, cfg.Runtime.ExecutionJobGitImage,
		cfg.Runtime.ExecutionJobTTLSeconds, cfg.Runtime.ExecutionRunnerToken, permissions,
	)
	if cfg.Modules.Delivery.Enabled {
		executionService.Start(infra.lifecycleCtx)
	}
	releases := apprelease.New(
		repos.releaseRepository, repos.applicationRepository, repos.catalogRepository, repos.clusterRepository, executionService, access, permissions,
		repos.eventRepository, audit, operations, releasebackendinfra.NewDirectRuntime(infra.clusterManager),
		func(connection domaincluster.Connection) (apprelease.AgentDeploymentClient, error) {
			return infra.agentRegistry.ClientFor(connection)
		},
	)
	marketplaceProvider, err := newMarketplaceProvider(cfg)
	if err != nil {
		return nil, err
	}
	plugins := appplugin.NewWithOptions(repos.pluginRepository, permissions, audit, appplugin.WithMarketplaceProvider(marketplaceProvider))
	if err := plugins.Reconcile(infra.lifecycleCtx); err != nil {
		return nil, err
	}
	identityProvider := appidentityprovider.NewWithEncryptionKeys(repos.identityProviderRepository, repos.identityRepository, permissions, audit, cfg.Security.CredentialEncryptionKeys)
	providerPortal := appproviderportal.New(repos.providerPortalRepository, permissions, audit)
	providerPortal.SetOIDCLaunchResolver(identityProvider)
	providerPortal.SetProfileReader(identity)
	return &deliveryCoreServices{
		applications: applications, execution: executionService,
		builds:      appbuild.New(repos.buildRepository, repos.applicationRepository, repos.catalogRepository, executionService, access, repos.eventRepository, audit, operations),
		catalog:     appcatalog.New(repos.catalogRepository, access, repos.applicationRepository, permissions, audit, operations),
		scopeGrants: appscopegrant.New(repos.scopeGrantRepository, permissions, audit, operations),
		registries:  appregistryconn.New(repos.registryRepository, permissions, appregistryconn.WithCredentialEncryptionKeys(cfg.Security.CredentialEncryptionKeys)),
		releases:    releases, integration: appintegration.New(infra.mcpRegistry), plugins: plugins,
		identityProvider: identityProvider, providerPortal: providerPortal,
	}, nil
}

func newMarketplaceProvider(cfg cfgpkg.Config) (appplugin.MarketplaceProvider, error) {
	providers := []appplugin.MarketplaceProvider{appplugin.NewDefaultMarketplaceProvider()}
	addRemote := func(id, rawURL string) error {
		if strings.TrimSpace(rawURL) == "" {
			return nil
		}
		provider, err := appplugin.NewRemoteMarketplaceProvider(appplugin.MarketplaceSource{
			ID:  firstNonEmpty(id, cfg.Plugins.Marketplace.SourceID, "opensoha-official"),
			URL: rawURL,
		}, nil)
		if err != nil {
			return err
		}
		providers = append(providers, provider)
		return nil
	}
	if err := addRemote(cfg.Plugins.Marketplace.SourceID, cfg.Plugins.Marketplace.URL); err != nil {
		return nil, err
	}
	for _, source := range cfg.Plugins.Marketplace.Sources {
		if err := addRemote(source.ID, source.URL); err != nil {
			return nil, err
		}
	}
	return appplugin.NewCompositeMarketplaceProvider(providers...), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func newDeliveryServices(lifecycleCtx context.Context, cfg cfgpkg.Config, infra *infrastructure, repos *repositories, core *coreServices) *deliveryServices {
	runtimeResources := core.resourceService.Runtime()
	workflowService := appworkflow.New(repos.workflowRepository, repos.applicationRepository, core.accessService, core.permissionResolver, repos.catalogRepository, core.buildService, core.releaseService, runtimeResources)
	workflowService.SetArtifactStore(repos.deliveryRepository)
	workflowService.SetRuntimeOptions(cfg.Runtime.WorkflowWorkers, cfg.Runtime.WorkflowQueueSize, cfg.Runtime.WorkflowNodeParallelism)
	workflowService.SetInstrumentation(infra.logger, infra.runtimeMetrics)
	workflowService.SetAlertMutator(core.monitoringService)
	core.executionService.SetWorkflowExecutionTaskSink(workflowService)
	if cfg.Modules.Delivery.Enabled {
		workflowService.Start(lifecycleCtx)
	}

	copilotService := appcopilot.MustNew(appcopilot.Dependencies{
		Sessions: repos.copilotRepository, Messages: repos.copilotRepository,
		DataSources: repos.copilotRepository, AnalysisProfiles: repos.copilotRepository,
		AutomationPolicies: repos.copilotRepository, RootCauseRuns: repos.copilotRepository,
		AgentRuns: repos.copilotRepository, InspectionTasks: repos.copilotRepository,
		InspectionRuns: repos.copilotRepository,
		Clusters:       core.clusterService, Alerts: core.monitoringService,
		Events: core.eventService, Audits: core.auditService,
		Applications: repos.applicationRepository, Builds: repos.buildRepository,
		Releases: repos.releaseRepository, Settings: core.settingsService,
		Permissions: core.permissionResolver,
	},
		appcopilot.WithTelemetryBackends(mcplogsinfra.DefaultRegistry(), mcpmetricsinfra.DefaultRegistry(), mcptracesinfra.DefaultRegistry()),
	)
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

	virtualizationService := appvirtualization.MustNew(
		appvirtualization.Dependencies{
			Connections:      repos.virtualizationRepository,
			ConnectionWriter: repos.virtualizationRepository,
			DockerLinks:      repos.virtualizationRepository,
			VMs:              repos.virtualizationRepository,
			Images:           repos.virtualizationRepository,
			Flavors:          repos.virtualizationRepository,
			Tasks:            repos.virtualizationRepository,
			TaskQueue:        repos.virtualizationRepository,
			TaskLogs:         repos.virtualizationRepository,
		},
		map[string]appvirtualization.Adapter{
			appvirtualization.ProviderKubeVirt: virtualizationinfra.NewKubeVirtAdapter(infra.clusterManager),
			appvirtualization.ProviderPVE:      virtualizationinfra.NewPVEAdapter(nil),
		},
		core.permissionResolver,
		core.operationService,
		appvirtualization.Options{
			CredentialEncryptionKey:  cfg.Security.CredentialEncryptionKey,
			CredentialEncryptionKeys: cfg.Security.CredentialEncryptionKeys,
			StartupSyncEnabled:       cfg.Runtime.VirtualizationStartupSync,
			WorkerInterval:           cfg.Runtime.VirtualizationWorkerInterval,
			SyncConcurrency:          cfg.Runtime.VirtualizationSyncConcurrency,
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
	computeService := appcompute.New(repos.virtualizationRepository, repos.dockerRepository, repos.pluginRepository, core.permissionResolver, appcompute.Options{VirtualizationEnabled: cfg.Modules.Virtualization.Enabled, RuntimeEnabled: cfg.Modules.Docker.Enabled})
	copilotService.SetAgentRuntimeReaders(core.executionService, runtimeResources, dockerService, virtualizationService, core.monitoringService)

	deliveryService := appdelivery.New(core.applicationService, core.catalogService, core.buildService, workflowService, core.releaseService, repos.deliveryRepository, core.executionService, runtimeResources, core.permissionResolver)
	deliveryService.SetRecorders(core.auditService, core.operationService)
	core.catalogService.SetTemplateUsageRuntimeReaders(appcatalog.TemplateUsageRuntimeReaders{
		Builds:    core.buildService,
		Workflows: workflowService,
		Releases:  core.releaseService,
		Delivery:  repos.deliveryRepository,
	})

	return &deliveryServices{
		workflowService:       workflowService,
		computeService:        computeService,
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
			FirstByteTimeout:            cfg.AIGateway.Relay.FirstByteTimeout,
			StreamIdleTimeout:           cfg.AIGateway.Relay.StreamIdleTimeout,
			HealthCheckEnabled:          cfg.AIGateway.Relay.HealthCheckEnabled,
			HealthCheckInterval:         cfg.AIGateway.Relay.HealthCheckInterval,
			MaxRequestBodyBytes:         int64(cfg.AIGateway.Relay.MaxRequestBodyMB) << 20,
			AllowInsecureUpstreamHTTP:   cfg.AIGateway.Relay.AllowInsecureUpstreamHTTP,
			AllowPrivateUpstreamHosts:   cfg.AIGateway.Relay.AllowPrivateUpstreamHosts,
			IncludeUsageForOpenAIStream: cfg.AIGateway.Relay.IncludeUsageForOpenAIStream,
			CredentialEncryptionKey:     cfg.Security.CredentialEncryptionKey,
			CredentialEncryptionKeys:    cfg.Security.CredentialEncryptionKeys,
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
	aiGatewayService.SetResourceService(core.resourceService.Runtime())
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

func newEventService(repo appevent.Repository, audit appevent.AuditRecorder, connectorToken string) *appevent.Service {
	service := appevent.New(repo)
	service.SetAuditRecorder(audit)
	service.SetConnectorEventSinkToken(connectorToken)
	return service
}

func newHandlers(cfg cfgpkg.Config, infra *infrastructure, repos *repositories, core *coreServices, delivery *deliveryServices, gateway *gatewayServices) (*handlerSet, error) {
	platformResources := newPlatformResourceServices(core.resourceService)
	platform, err := apiHandlers.NewPlatformHandlerWithResources(apiHandlers.PlatformDependencies{
		Clusters: core.clusterService, Resources: platformResources, Audit: core.auditService,
		Events: core.eventService, Operations: core.operationService, Integration: core.integrationService,
	})
	if err != nil {
		return nil, fmt.Errorf("build platform handler: %w", err)
	}
	return &handlerSet{deps: newRouteDependencies(cfg, infra, repos, core, delivery, gateway, platform)}, nil
}

func newPlatformResourceServices(service *appresource.Service) apiHandlers.ResourceServices {
	workloads := service.Workloads()
	configuration := service.Configuration()
	network := service.Network()
	storage := service.Storage()
	rbac := service.RBAC()
	helm := service.Helm()
	inventory := service.Inventory()
	return apiHandlers.ResourceServices{
		PodReader: workloads, PodEditor: workloads, PodDiagnostics: workloads, PodStreams: workloads,
		DeploymentReader: workloads, DeploymentEditor: workloads,
		StatefulSetReader: workloads, StatefulSetEditor: workloads,
		DaemonSetReader: workloads, DaemonSetEditor: workloads,
		Jobs: workloads, CronJobs: workloads, WorkloadInventory: workloads,
		Creator: configuration, ConfigMaps: configuration, Secrets: configuration,
		ConfigurationInventory: configuration,
		NetworkOverview:        network, NetworkInventory: network, GatewayRouting: network, GatewayPolicy: network,
		PersistentVolumeClaims: storage, PersistentVolumes: storage, StorageClasses: storage,
		NamespacedRBAC: rbac, ClusterRBAC: rbac,
		CRDReader: service.CustomResources(), CRDEditor: service.CustomResources(),
		Helm: helm, HelmReleaseReader: helm, HelmReleaseEditor: helm,
		Namespaces: inventory, NodeReader: inventory, NodeEditor: inventory,
		Generic: service.GenericResources(), Events: service.Events(),
		PortForwards: service.PortForwards(),
	}

}

func newRouteDependencies(cfg cfgpkg.Config, infra *infrastructure, repos *repositories, core *coreServices, delivery *deliveryServices, gateway *gatewayServices, platform *apiHandlers.PlatformHandler) apiRoutes.Dependencies {
	directorySyncHandler := directorysynchandler.New(repos.directorySyncRepository, core.directorySyncService, core.directorySyncConnectors)
	directorySyncHandler.SetRecorders(core.auditService, core.operationService)
	return apiRoutes.Dependencies{
		System:        apiHandlers.NewSystemHandler(infra.databaseStore, infra.runtimeMetrics),
		Platform:      platform,
		Announcements: apiHandlers.NewAnnouncementHandlerWithServices(core.announcementService, core.announcementService),
		Menu:          apiHandlers.NewMenuHandler(core.menuService),
		Module:        apiHandlers.NewModuleHandler(core.moduleService),
		Monitoring: apiHandlers.NewMonitoringHandler(apiHandlers.MonitoringDependencies{
			Alerts: core.monitoringService, Channels: core.monitoringService, Routes: core.monitoringService,
			Silences: core.monitoringService, DeliveryLogs: core.monitoringService, Webhooks: core.monitoringService,
			Integrations: core.monitoringService, Rules: core.monitoringService, Events: core.monitoringService,
			HealingRuns: core.monitoringService, NotificationPolicies: core.monitoringService,
			NotificationTemplates: core.monitoringService, HealingPolicies: core.monitoringService,
			OnCallSchedules: core.monitoringService, OnCallRotations: core.monitoringService,
			OnCallEscalations: core.monitoringService, OnCallAssignments: core.monitoringService,
			OnCallRuntime: core.monitoringService,
		}),
		Catalog: apiHandlers.NewCatalogHandlerWithServices(
			core.catalogService, core.catalogService, core.catalogService,
		),
		Delivery: newDeliveryHandler(delivery.deliveryService, cfg.Runtime.ExecutionRunnerKeys),
		Applications: apiHandlers.NewApplicationHandlerWithServices(
			core.applicationService, core.applicationService, core.applicationService,
		),
		Builds:     apiHandlers.NewBuildHandler(core.buildService),
		Workflows:  apiHandlers.NewWorkflowHandler(delivery.workflowService),
		Registries: apiHandlers.NewRegistryHandler(core.registryService),
		Releases:   apiHandlers.NewReleaseHandler(core.releaseService),
		Copilot: apiHandlers.NewCopilotHandlerWithServices(apiHandlers.CopilotServices{
			Sessions: delivery.copilotService, Messages: delivery.copilotService,
			Streams: delivery.copilotService, Workbench: delivery.copilotService,
			DataSources: delivery.copilotService, AnalysisProfiles: delivery.copilotService,
			Automation: delivery.copilotService, RootCause: delivery.copilotService,
			AgentRuns: delivery.copilotService, InspectionTasks: delivery.copilotService,
			InspectionRuns: delivery.copilotService,
		}, cfg.Runtime.ExecutionRunnerKeys),
		AIGateway: apiHandlers.NewAIGatewayHandlerWithServices(apiHandlers.AIGatewayServices{
			Capabilities: gateway.aiGatewayService, PersonalTokens: gateway.aiGatewayService,
			ServiceAccounts: gateway.aiGatewayService, Clients: gateway.aiGatewayService,
			ToolGrants: gateway.aiGatewayService, AccessPolicies: gateway.aiGatewayService,
			Governance: gateway.aiGatewayService, Audit: gateway.aiGatewayService,
			Approvals: gateway.aiGatewayService, Upstreams: gateway.aiGatewayService,
			ModelRoutes: gateway.aiGatewayService, RelayObservability: gateway.aiGatewayService,
			Relay: gateway.aiGatewayService,
		}),
		Plugins: apiHandlers.NewPluginHandlerWithServices(
			core.pluginService, core.pluginService, core.pluginService, core.pluginService,
		),
		Compute:        apiHandlers.NewComputeHandler(delivery.computeService),
		Virtualization: newVirtualizationHandler(delivery.virtualizationService),
		Docker:         newDockerHandler(delivery.dockerService, cfg.Runtime.ExecutionRunnerKeys),
		Access: accesshandler.New(accesshandler.Services{
			Users: core.accessConsoleService, Catalog: core.accessConsoleService,
			Roles: core.accessConsoleService, Teams: core.accessConsoleService, Policies: core.accessConsoleService,
		}),
		DirectorySync: directorySyncHandler,
		ScopeGrants:   accesshandler.NewScopeGrantHandler(core.scopeGrantService),
		Settings:      newSettingsHandler(core.settingsService, core.permissionResolver),
		Auth:          newAuthHandler(core.identityService, core.accessConsoleService, core.settingsService, cfg.Auth),
		ProviderPortal: providerportalhandler.New(providerportalhandler.Services{
			PortalReader:     core.providerPortalService,
			PortalInteractor: core.providerPortalService,
			Applications:     core.providerPortalService,
			Policies:         core.providerPortalService,
			Providers:        core.identityProviderService,
			Outposts:         core.identityProviderService,
			OIDCClients:      core.identityProviderService,
			OIDC:             core.identityProviderService,
			Proxy:            core.identityProviderService,
			OutpostRuntime:   core.identityProviderService,
		}),
		Authn: core.identityService,
	}
}

func newDeliveryHandler(service *appdelivery.Service, keys keyring.Ring) *apiHandlers.DeliveryHandler {
	return apiHandlers.NewDeliveryHandlerWithServices(apiHandlers.DeliveryServices{
		Applications: service, Releases: service, Executions: service, Runtime: service,
		Blueprints: service, Drafts: service, Actions: service, Runner: service,
	}, keys)
}

func newVirtualizationHandler(service *appvirtualization.Service) *apiHandlers.VirtualizationHandler {
	return apiHandlers.NewVirtualizationHandlerWithServices(apiHandlers.VirtualizationServices{
		Overview: service, Connections: service, Sync: service, VMs: service,
		Images: service, Flavors: service, Operations: service, Runtime: service,
	})
}

func newDockerHandler(service *appdocker.Service, keys keyring.Ring) *apiHandlers.DockerHandler {
	return apiHandlers.NewDockerHandlerWithServices(apiHandlers.DockerServices{
		Overview: service, Hosts: service, Projects: service, ProjectRuntime: service,
		ProjectStorage: service, Services: service, PortMappings: service, Templates: service,
		Operations: service, RunnerOperations: service,
	}, keys)
}

func newSettingsHandler(service *appsettings.Service, permissions *appaccess.PermissionResolver) *apiHandlers.SettingsHandler {
	return apiHandlers.NewSettingsHandlerWithServices(service, service, service, service, permissions)
}

func newAuthHandler(identity *appidentity.Service, access *appaccess.ConsoleService, settings *appsettings.Service, cfg cfgpkg.AuthConfig) *apiHandlers.AuthHandler {
	return apiHandlers.NewAuthHandlerWithServices(
		identity, identity, identity, identity, identity, access, settings, cfg,
	)
}

func newHTTPServer(cfg cfgpkg.Config, logger *zap.Logger, handlers *handlerSet) *http.Server {
	return apiRoutes.New(cfg, logger, handlers.deps)
}
