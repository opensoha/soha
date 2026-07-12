package aigateway

import (
	"context"
	"net/http"
	"regexp"
	"sync"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

const manifestVersion = "v1alpha1"

const rotatedTokenDefaultTTL = 90 * 24 * time.Hour

const (
	defaultRelayTimeout           = 120 * time.Second
	defaultRelayStreamTimeout     = 300 * time.Second
	defaultRelayFirstByteTimeout  = 30 * time.Second
	defaultRelayStreamIdleTimeout = 60 * time.Second
	defaultRelayMaxRequestBytes   = 32 << 20
)

var gatewaySensitiveValuePattern = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key|authorization|credential)(\s*[:=]\s*)([^\s,;]+)`)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type RateLimitBackend interface {
	IncrementRateLimitCounter(context.Context, domainaigateway.RateLimitCounter) (domainaigateway.RateLimitCounter, error)
	ApplyRateLimitState(context.Context, domainaigateway.RateLimitState) (domainaigateway.RateLimitState, error)
}

type ApplicationService interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Create(context.Context, domainidentity.Principal, domainapp.UpsertInput) (domainapp.App, error)
	ListServices(context.Context, domainidentity.Principal, string) ([]domainapp.Service, error)
}

type DeliveryApplicationReader interface {
	GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error)
	GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error)
}

type DeliveryActionExecutor interface {
	TriggerApplicationDeliveryAction(context.Context, domainidentity.Principal, string, domaindelivery.ApplicationDeliveryActionInput) (domaindelivery.ApplicationDeliveryActionResult, error)
}

type ReleaseBundleReader interface {
	ListReleaseBundles(context.Context, domainidentity.Principal, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error)
	GetReleaseBundle(context.Context, domainidentity.Principal, string) (domaindelivery.ReleaseBundle, error)
	ListReleaseBundleArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
}

type ExecutionTaskReader interface {
	ListExecutionTasks(context.Context, domainidentity.Principal, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error)
	GetExecutionTask(context.Context, domainidentity.Principal, string) (domaindelivery.ExecutionTask, error)
	ListExecutionLogs(context.Context, domainidentity.Principal, string, int) ([]domaindelivery.ExecutionLog, error)
}

type DeliveryService interface {
	DeliveryApplicationReader
	DeliveryActionExecutor
	ReleaseBundleReader
	ExecutionTaskReader
}

type CatalogService interface {
	ListWorkflowTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.WorkflowTemplate, error)
}

type PodResourceReader interface {
	ListPods(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodView, error)
	GetPodDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.PodDetailView, error)
	GetPodLogs(context.Context, domainidentity.Principal, string, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error)
}

type DeploymentResourceReader interface {
	ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error)
	GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
}

type NetworkResourceReader interface {
	ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error)
	ListIngresses(context.Context, domainidentity.Principal, string, string) ([]domainresource.IngressView, error)
}

type GatewayResourceReader interface {
	ListGatewayClasses(context.Context, domainidentity.Principal, string) ([]domainresource.GatewayClassView, error)
	ListGateways(context.Context, domainidentity.Principal, string, string) ([]domainresource.GatewayView, error)
	ListHTTPRoutes(context.Context, domainidentity.Principal, string, string) ([]domainresource.HTTPRouteView, error)
}

type GatewayPolicyResourceReader interface {
	ListBackendTLSPolicies(context.Context, domainidentity.Principal, string, string) ([]domainresource.BackendTLSPolicyView, error)
	ListGRPCRoutes(context.Context, domainidentity.Principal, string, string) ([]domainresource.GRPCRouteView, error)
	ListReferenceGrants(context.Context, domainidentity.Principal, string, string) ([]domainresource.ReferenceGrantView, error)
}

type StorageResourceReader interface {
	ListPersistentVolumeClaims(context.Context, domainidentity.Principal, string, string) ([]domainresource.PersistentVolumeClaimView, error)
	ListPersistentVolumes(context.Context, domainidentity.Principal, string) ([]domainresource.PersistentVolumeView, error)
	ListStorageClasses(context.Context, domainidentity.Principal, string) ([]domainresource.StorageClassView, error)
}

type NodeResourceReader interface {
	GetNodeDetail(context.Context, domainidentity.Principal, string, string) (domainresource.NodeDetailView, error)
}

type ClusterEventReader interface {
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
}

type ResourceService interface {
	PodResourceReader
	DeploymentResourceReader
	NetworkResourceReader
	GatewayResourceReader
	GatewayPolicyResourceReader
	StorageResourceReader
	NodeResourceReader
	ClusterEventReader
}

type AnalysisArtifactRecorder interface {
	RecordGatewayAnalysisArtifact(context.Context, domainidentity.Principal, domaincopilot.GatewayAnalysisArtifactInput) (domaincopilot.AgentRun, error)
	QueueGatewayAnalysisAgentRun(context.Context, domainidentity.Principal, domaincopilot.GatewayAnalysisAgentRunInput) (domaincopilot.AgentRun, error)
}

type OnCallResolver interface {
	GetCurrentOnCall(context.Context, domainidentity.Principal, string) (map[string]any, error)
	ResolveOnCall(context.Context, domainidentity.Principal, domainalert.OnCallResolveInput) (map[string]any, error)
}

type Service struct {
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder

	personalTokens  PersonalAccessTokenRepository
	serviceAccounts ServiceAccountRepository
	clients         AIClientRepository
	toolGrants      ToolGrantRepository
	accessPolicies  AccessPolicyRepository
	skillBindings   SkillBindingRepository
	auditLogs       AuditLogRepository
	approvals       ApprovalRepository
	rateLimitRepo   RateLimitRepository
	llmRelayRepo    LLMRelayRepository

	rateLimits         RateLimitBackend
	relayConfig        LLMRelayConfig
	httpClient         *http.Client
	relaySelector      *relaySelector
	relayCredentials   *relayCredentialCodec
	relayTransport     *relayHTTPTransport
	relayCache         *relayResponseCache
	relayConcurrencyMu sync.Mutex
	relayConcurrency   map[string]int
	relayHealthOnce    sync.Once
	apps               ApplicationService
	delivery           DeliveryService
	catalog            CatalogService
	resources          ResourceService
	copilot            AnalysisArtifactRecorder
	oncall             OnCallResolver
	registry           *capabilityRegistry
}

func NewWithDeps(deps ServiceDeps) *Service {
	relayConfig := normalizedRelayConfig(deps.RelayConfig)
	httpClient := normalizedHTTPClient(deps.HTTPClient)
	credentials := newRelayCredentialCodec(relayConfig)
	service := &Service{
		permissions:      deps.Permissions,
		audit:            deps.Audit,
		personalTokens:   deps.PersonalTokens,
		serviceAccounts:  deps.ServiceAccounts,
		clients:          deps.Clients,
		toolGrants:       deps.ToolGrants,
		accessPolicies:   deps.AccessPolicies,
		skillBindings:    deps.SkillBindings,
		auditLogs:        deps.AuditLogs,
		approvals:        deps.Approvals,
		rateLimitRepo:    deps.RateLimits,
		llmRelayRepo:     deps.LLMRelay,
		rateLimits:       deps.RateLimitBackend,
		relayConfig:      relayConfig,
		httpClient:       httpClient,
		relaySelector:    newRelaySelector(deps.LLMRelay),
		relayCredentials: credentials,
		registry:         newDefaultCapabilityRegistry(),
	}
	service.relayTransport = newRelayHTTPTransport(httpClient, relayConfig, credentials)
	service.relayCache = newRelayResponseCache(deps.LLMRelay, credentials, relayConfig)
	return service
}

func (s *Service) SetCapabilityProviders(providers ...CapabilityProvider) {
	s.registry = newCapabilityRegistry(providers...)
}

func (s *Service) AddCapabilityProviders(providers ...CapabilityProvider) {
	if s.registry == nil {
		s.registry = newDefaultCapabilityRegistry()
	}
	s.registry.AddProviders(providers...)
}

func (s *Service) SetDeliveryServices(apps ApplicationService, delivery DeliveryService) {
	s.apps = apps
	s.delivery = delivery
}

func (s *Service) SetCatalogService(catalog CatalogService) {
	s.catalog = catalog
}

func (s *Service) SetResourceService(resources ResourceService) {
	s.resources = resources
}

func (s *Service) SetAnalysisArtifactRecorder(copilot AnalysisArtifactRecorder) {
	s.copilot = copilot
}

func (s *Service) SetOperationRecorder(operations OperationRecorder) {
	s.operations = operations
}

func (s *Service) SetRateLimitBackend(rateLimits RateLimitBackend) {
	s.rateLimits = rateLimits
}

func (s *Service) SetOnCallResolver(oncall OnCallResolver) {
	s.oncall = oncall
}

func (s *Service) personalTokenRepository() PersonalAccessTokenRepository {
	if s != nil && s.personalTokens != nil {
		return s.personalTokens
	}
	return nil
}

func (s *Service) serviceAccountRepository() ServiceAccountRepository {
	if s != nil && s.serviceAccounts != nil {
		return s.serviceAccounts
	}
	return nil
}

func (s *Service) clientRepository() AIClientRepository {
	if s != nil && s.clients != nil {
		return s.clients
	}
	return nil
}

func (s *Service) toolGrantRepository() ToolGrantRepository {
	if s != nil && s.toolGrants != nil {
		return s.toolGrants
	}
	return nil
}

func (s *Service) accessPolicyRepository() AccessPolicyRepository {
	if s != nil && s.accessPolicies != nil {
		return s.accessPolicies
	}
	return nil
}

func (s *Service) skillBindingRepository() SkillBindingRepository {
	if s != nil && s.skillBindings != nil {
		return s.skillBindings
	}
	return nil
}

func (s *Service) auditLogRepository() AuditLogRepository {
	if s != nil && s.auditLogs != nil {
		return s.auditLogs
	}
	return nil
}

func (s *Service) approvalRepository() ApprovalRepository {
	if s != nil && s.approvals != nil {
		return s.approvals
	}
	return nil
}

func (s *Service) rateLimitRepository() RateLimitRepository {
	if s != nil && s.rateLimitRepo != nil {
		return s.rateLimitRepo
	}
	return nil
}

func (s *Service) llmRelayRepository() LLMRelayRepository {
	if s != nil && s.llmRelayRepo != nil {
		return s.llmRelayRepo
	}
	return nil
}

func normalizedRelayConfig(cfg LLMRelayConfig) LLMRelayConfig {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = defaultRelayTimeout
	}
	if cfg.StreamTimeout <= 0 {
		cfg.StreamTimeout = defaultRelayStreamTimeout
	}
	if cfg.FirstByteTimeout <= 0 {
		cfg.FirstByteTimeout = defaultRelayFirstByteTimeout
	}
	if cfg.StreamIdleTimeout <= 0 {
		cfg.StreamIdleTimeout = defaultRelayStreamIdleTimeout
	}
	if cfg.HealthCheckInterval <= 0 {
		cfg.HealthCheckInterval = time.Minute
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = defaultRelayMaxRequestBytes
	}
	return cfg
}

func normalizedHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	// Relay requests carry per-operation deadlines. A client-wide timeout would
	// incorrectly cap streams before their longer StreamTimeout expires.
	return &http.Client{}
}
