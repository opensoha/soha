package aigateway

import (
	"context"
	"regexp"
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

var gatewaySensitiveValuePattern = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key|authorization|credential)(\s*[:=]\s*)([^\s,;]+)`)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Repository interface {
	ListPersonalAccessTokens(context.Context, string) ([]domainaigateway.PersonalAccessToken, error)
	ListAllPersonalAccessTokens(context.Context) ([]domainaigateway.PersonalAccessToken, error)
	CreatePersonalAccessToken(context.Context, domainaigateway.PersonalAccessToken) (domainaigateway.PersonalAccessToken, error)
	RevokePersonalAccessToken(context.Context, string, string) error
	ListServiceAccounts(context.Context) ([]domainaigateway.ServiceAccount, error)
	CreateServiceAccount(context.Context, domainaigateway.ServiceAccount) (domainaigateway.ServiceAccount, error)
	GetServiceAccount(context.Context, string) (domainaigateway.ServiceAccount, error)
	ListAllServiceAccountTokens(context.Context) ([]domainaigateway.ServiceAccountToken, error)
	CreateServiceAccountToken(context.Context, domainaigateway.ServiceAccountToken) (domainaigateway.ServiceAccountToken, error)
	RevokeServiceAccountToken(context.Context, string) error
	ListAIClients(context.Context) ([]domainaigateway.AIClient, error)
	GetAIClient(context.Context, string) (domainaigateway.AIClient, error)
	CreateAIClient(context.Context, domainaigateway.AIClient) (domainaigateway.AIClient, error)
	UpdateAIClient(context.Context, domainaigateway.AIClient) (domainaigateway.AIClient, error)
	ListToolGrants(context.Context, domainaigateway.ToolGrantFilter) ([]domainaigateway.ToolGrant, error)
	CreateToolGrant(context.Context, domainaigateway.ToolGrant) (domainaigateway.ToolGrant, error)
	DeleteToolGrant(context.Context, string) error
	ListActiveToolGrants(context.Context, string, string, string, time.Time) ([]domainaigateway.ToolGrant, error)
	ListAccessPolicies(context.Context, domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error)
	CreateAccessPolicy(context.Context, domainaigateway.AccessPolicy) (domainaigateway.AccessPolicy, error)
	UpdateAccessPolicy(context.Context, domainaigateway.AccessPolicy) (domainaigateway.AccessPolicy, error)
	DeleteAccessPolicy(context.Context, string) error
	ListActiveAccessPolicies(context.Context, string, string, string) ([]domainaigateway.AccessPolicy, error)
	ListSkillBindings(context.Context, domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error)
	CreateSkillBinding(context.Context, domainaigateway.SkillBinding) (domainaigateway.SkillBinding, error)
	UpdateSkillBinding(context.Context, domainaigateway.SkillBinding) (domainaigateway.SkillBinding, error)
	DeleteSkillBinding(context.Context, string) error
	ListActiveSkillBindings(context.Context, string, string, string) ([]domainaigateway.SkillBinding, error)
	ListAuditLogs(context.Context, domainaigateway.AuditLogFilter) ([]domainaigateway.AuditLog, error)
	CreateAuditLog(context.Context, domainaigateway.AuditLog) error
	IncrementRateLimitCounter(context.Context, domainaigateway.RateLimitCounter) (domainaigateway.RateLimitCounter, error)
	ApplyRateLimitState(context.Context, domainaigateway.RateLimitState) (domainaigateway.RateLimitState, error)
	CreateApprovalRequest(context.Context, domainaigateway.ApprovalRequest) (domainaigateway.ApprovalRequest, error)
	GetApprovalRequest(context.Context, string) (domainaigateway.ApprovalRequest, error)
	ListApprovalRequests(context.Context, domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error)
	UpdateApprovalRequest(context.Context, string, domainaigateway.ApprovalRequestUpdate) (domainaigateway.ApprovalRequest, error)
	ExpirePendingApprovalRequests(context.Context, time.Time) ([]domainaigateway.ApprovalRequest, error)
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

type DeliveryService interface {
	GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error)
	GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error)
	TriggerApplicationDeliveryAction(context.Context, domainidentity.Principal, string, domaindelivery.ApplicationDeliveryActionInput) (domaindelivery.ApplicationDeliveryActionResult, error)
	GetApprovalPolicy(context.Context, string) (domaindelivery.ApprovalPolicy, error)
	ListApprovalPolicies(context.Context, domainidentity.Principal) ([]domaindelivery.ApprovalPolicy, error)
	ListReleaseBundles(context.Context, domainidentity.Principal, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error)
	GetReleaseBundle(context.Context, domainidentity.Principal, string) (domaindelivery.ReleaseBundle, error)
	ListReleaseBundleArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
	ListExecutionTasks(context.Context, domainidentity.Principal, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error)
	GetExecutionTask(context.Context, domainidentity.Principal, string) (domaindelivery.ExecutionTask, error)
	ListExecutionLogs(context.Context, domainidentity.Principal, string, int) ([]domaindelivery.ExecutionLog, error)
}

type CatalogService interface {
	ListWorkflowTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.WorkflowTemplate, error)
}

type ResourceService interface {
	ListPods(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodView, error)
	GetPodDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.PodDetailView, error)
	GetPodLogs(context.Context, domainidentity.Principal, string, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error)
	ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error)
	GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
	ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error)
	ListIngresses(context.Context, domainidentity.Principal, string, string) ([]domainresource.IngressView, error)
	ListGatewayClasses(context.Context, domainidentity.Principal, string) ([]domainresource.GatewayClassView, error)
	ListGateways(context.Context, domainidentity.Principal, string, string) ([]domainresource.GatewayView, error)
	ListHTTPRoutes(context.Context, domainidentity.Principal, string, string) ([]domainresource.HTTPRouteView, error)
	ListBackendTLSPolicies(context.Context, domainidentity.Principal, string, string) ([]domainresource.BackendTLSPolicyView, error)
	ListGRPCRoutes(context.Context, domainidentity.Principal, string, string) ([]domainresource.GRPCRouteView, error)
	ListReferenceGrants(context.Context, domainidentity.Principal, string, string) ([]domainresource.ReferenceGrantView, error)
	ListPersistentVolumeClaims(context.Context, domainidentity.Principal, string, string) ([]domainresource.PersistentVolumeClaimView, error)
	ListPersistentVolumes(context.Context, domainidentity.Principal, string) ([]domainresource.PersistentVolumeView, error)
	ListStorageClasses(context.Context, domainidentity.Principal, string) ([]domainresource.StorageClassView, error)
	GetNodeDetail(context.Context, domainidentity.Principal, string, string) (domainresource.NodeDetailView, error)
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
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
	repo        Repository
	rateLimits  RateLimitBackend
	apps        ApplicationService
	delivery    DeliveryService
	catalog     CatalogService
	resources   ResourceService
	copilot     AnalysisArtifactRecorder
	oncall      OnCallResolver
	registry    *capabilityRegistry
}

func New(permissions *appaccess.PermissionResolver, audit AuditRecorder, repos ...Repository) *Service {
	var repo Repository
	if len(repos) > 0 {
		repo = repos[0]
	}
	return &Service{permissions: permissions, audit: audit, repo: repo, registry: newDefaultCapabilityRegistry()}
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
