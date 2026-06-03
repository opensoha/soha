package aigateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domaincatalog "github.com/soha/soha/internal/domain/catalog"
	domaincopilot "github.com/soha/soha/internal/domain/copilot"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	domainresource "github.com/soha/soha/internal/domain/resource"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/operationentry"
	"github.com/soha/soha/internal/platform/requestctx"
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

func (s *Service) Capabilities(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ManifestRequest) (domainaigateway.Manifest, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayView); err != nil {
		_ = s.recordManifestAudit(ctx, principal, input, "deny", err.Error(), 0, len(s.gatewayTools()))
		return domainaigateway.Manifest{}, err
	}
	permissionKeys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}

	tools, deniedTools := filterTools(s.gatewayTools(), permissionKeys)
	resources, deniedResources := filterResources(s.gatewayResources(), permissionKeys)
	prompts, deniedPrompts := filterPrompts(s.gatewayPrompts(), permissionKeys)
	skills, deniedSkills := filterSkills(s.gatewaySkills(), permissionKeys)
	tools, grantDenied, err := s.filterToolsByGrants(ctx, principal, input.AIClientID, tools, permissionKeys)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}
	deniedTools += grantDenied
	tools, policyDeniedTools, err := s.filterToolsByAccessPolicies(ctx, principal, input.AIClientID, input.SkillID, tools)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}
	deniedTools += policyDeniedTools
	skills, policyDeniedSkills, err := s.filterSkillsByAccessPolicies(ctx, principal, input.AIClientID, skills)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}
	deniedSkills += policyDeniedSkills
	bindings, err := s.activeSkillBindings(ctx, principal, input.AIClientID)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}
	skills, bindingDeniedSkills := filterSkillsByBindingsWithSkills(skills, bindings, s.gatewaySkills())
	deniedSkills += bindingDeniedSkills
	tools, bindingDeniedTools := filterToolsBySkillBindingsWithSkills(tools, bindings, input.SkillID, s.gatewaySkills())
	deniedTools += bindingDeniedTools
	resources, bindingDeniedResources := filterResourcesBySkillBindingsWithCapabilities(resources, bindings, input.SkillID, s.gatewayResourceCapabilityRefs(), s.gatewaySkills())
	deniedResources += bindingDeniedResources
	prompts, bindingDeniedPrompts := filterPromptsBySkillBindingsWithCapabilities(prompts, bindings, input.SkillID, s.gatewayResources(), s.gatewayResourceCapabilityRefs(), s.gatewaySkills())
	deniedPrompts += bindingDeniedPrompts
	deniedCount := deniedTools + deniedResources + deniedPrompts + deniedSkills

	manifest := domainaigateway.Manifest{
		Name:           "soha AI Gateway",
		Version:        manifestVersion,
		GeneratedAt:    time.Now().UTC(),
		Principal:      principal,
		Caller:         callerContext(input),
		PermissionKeys: permissionKeys,
		Tools:          tools,
		Resources:      resources,
		Prompts:        prompts,
		Skills:         skills,
		Summary: domainaigateway.ManifestSummary{
			ToolCount:     len(tools),
			ResourceCount: len(resources),
			PromptCount:   len(prompts),
			SkillCount:    len(skills),
			DeniedCount:   deniedCount,
		},
	}
	_ = s.recordManifestAudit(ctx, principal, input, "success", "listed AI Gateway capabilities", len(tools), deniedCount)
	return manifest, nil
}

func (s *Service) gatewayRegistry() *capabilityRegistry {
	if s != nil && s.registry != nil {
		return s.registry
	}
	return newDefaultCapabilityRegistry()
}

func (s *Service) gatewayTools() []domainaigateway.ToolCapability {
	return s.gatewayRegistry().Tools()
}

func (s *Service) gatewayResources() []domainaigateway.ResourceCapability {
	return s.gatewayRegistry().Resources()
}

func (s *Service) gatewayPrompts() []domainaigateway.PromptCapability {
	return s.gatewayRegistry().Prompts()
}

func (s *Service) gatewaySkills() []domainaigateway.SkillCapability {
	return s.gatewayRegistry().Skills()
}

func (s *Service) gatewayResourceCapabilityRefs() []ResourceCapabilityRefs {
	return s.gatewayRegistry().ResourceCapabilityRefs()
}

func (s *Service) resourceToolRefs(name string) []string {
	return s.gatewayRegistry().ResourceToolRefs(name)
}

func (s *Service) resourcePromptRefs(name string) []string {
	return s.gatewayRegistry().ResourcePromptRefs(name)
}

func (s *Service) resourceSkillRefs(name string) []string {
	return s.gatewayRegistry().ResourceSkillRefs(name)
}

func (s *Service) toolByName(name string) (domainaigateway.ToolCapability, bool) {
	return s.gatewayRegistry().ToolByName(name)
}

func (s *Service) resourceByName(name string) (domainaigateway.ResourceCapability, bool) {
	return s.gatewayRegistry().ResourceByName(name)
}

func (s *Service) promptByName(name string) (domainaigateway.PromptCapability, bool) {
	return s.gatewayRegistry().PromptByName(name)
}

func (s *Service) skillByID(id string) (domainaigateway.SkillCapability, bool) {
	return s.gatewayRegistry().SkillByID(id)
}

func (s *Service) InvokeTool(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	toolName := strings.TrimSpace(input.ToolName)
	tool, ok := s.toolByName(toolName)
	if !ok {
		return domainaigateway.ToolInvocationResult{}, fmt.Errorf("%w: unknown AI Gateway tool %s", apperrors.ErrInvalidArgument, toolName)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}
	if err := s.authorizeTool(ctx, principal, tool); err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}
	invocationScope := standardGatewayScope(input.Input, nil)
	grantRequiresApproval, err := s.authorizeToolGrant(ctx, principal, input.AIClientID, tool, invocationScope)
	if err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}
	decision, policyInput, redactionSummary, err := s.authorizeAccessPolicy(ctx, principal, input.AIClientID, input.SkillID, &tool, invocationScope, input.Input)
	if err != nil {
		_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "deny", err.Error(), nil, redactionSummary)
		return domainaigateway.ToolInvocationResult{}, err
	}
	input.Input = policyInput
	if grantRequiresApproval {
		decision = mergeGatewayRiskDecision(decision, gatewayRiskDecision{
			Strategy: gatewayRiskRequireApproval,
			Reason:   "matching MCP tool grant requires approval",
		})
		tool.RequiresApproval = true
	}
	if err := s.authorizeSkillBinding(ctx, principal, input.AIClientID, input.SkillID, tool); err != nil {
		_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "deny", err.Error(), nil, redactionSummary)
		return domainaigateway.ToolInvocationResult{}, err
	}
	if decision.shouldHoldExecution() {
		return s.holdToolInvocation(ctx, principal, input, tool, decision, redactionSummary)
	}

	output, relatedIDs, err := s.invokeGatewayTool(ctx, principal, tool, input.Input)
	if err != nil {
		_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "failure", err.Error(), relatedIDs, redactionSummary)
		return domainaigateway.ToolInvocationResult{}, err
	}
	var outputRedactionSummary gatewayRedactionAuditSummary
	output, outputRedactionSummary, err = s.sanitizeToolOutputByAccessPolicy(ctx, principal, input.AIClientID, input.SkillID, tool, invocationScope, output)
	redactionSummary.merge(outputRedactionSummary)
	if err != nil {
		_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "deny", err.Error(), relatedIDs, redactionSummary)
		return domainaigateway.ToolInvocationResult{}, err
	}
	usageSummary := gatewayProviderUsageSummary(output, relatedIDs)
	_ = s.recordToolAuditWithMetadata(ctx, principal, input, tool, "success", "invoked AI Gateway tool", relatedIDs, redactionSummary, usageSummary)
	audit := map[string]any{
		"riskLevel":        tool.RiskLevel,
		"requiresApproval": tool.RequiresApproval,
	}
	addGatewayRedactionAuditMetadata(audit, redactionSummary)
	addGatewayUsageAuditMetadata(audit, usageSummary)
	return domainaigateway.ToolInvocationResult{
		ToolName:         tool.Name,
		RiskLevel:        tool.RiskLevel,
		RequiresApproval: tool.RequiresApproval,
		Result:           "success",
		Output:           output,
		RelatedIDs:       relatedIDs,
		Audit:            audit,
	}, nil
}

func (s *Service) ReadResource(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ResourceReadRequest) (domainaigateway.ResourceReadResult, error) {
	resourceName := normalizeGatewayResourceURI(firstNonEmpty(input.Name, input.URI))
	input.Name = resourceName
	input.URI = resourceName
	if input.Context == nil {
		input.Context = map[string]any{}
	}
	resource, ok := s.resourceByName(resourceName)
	if !ok {
		resource = domainaigateway.ResourceCapability{Name: resourceName}
		err := fmt.Errorf("%w: unknown AI Gateway resource %s", apperrors.ErrInvalidArgument, resourceName)
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	if err := s.authorizeResource(ctx, principal, resource); err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	if _, err := s.authorizeSkillContext(ctx, principal, input.AIClientID, input.SkillID); err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	bindings, err := s.activeSkillBindings(ctx, principal, input.AIClientID)
	if err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	toolRefs := s.resourceToolRefs(resource.Name)
	promptRefs := s.resourcePromptRefs(resource.Name)
	skillRefs := s.resourceSkillRefs(resource.Name)
	if err := authorizeResourceSkillBindingWithRefs(resource, bindings, input.SkillID, toolRefs, s.gatewaySkills()); err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}

	document := gatewayResourceDocumentWithCapabilities(resource, input, bindings, input.SkillID, toolRefs, promptRefs, skillRefs, s.gatewayTools(), s.gatewayPrompts(), s.gatewaySkills())
	text, err := marshalGatewayDocument(document)
	if err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "failure", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	resourceRefs := ResourceCapabilityRefs{Resource: resource.Name, Tools: toolRefs, Prompts: promptRefs, Skills: skillRefs}
	relatedToolRefs := filterToolRefsBySkillBindingsWithSkills(toolRefs, bindings, input.SkillID, s.gatewaySkills())
	relatedPromptRefs := filterPromptRefsBySkillBindingsWithResourceRefs(promptRefs, bindings, input.SkillID, resourceRefs, s.gatewaySkills())
	relatedSkillRefs := filterSkillRefsByBindingsWithSkills(skillRefs, bindings, input.SkillID, s.gatewaySkills())
	relatedIDs := map[string]any{
		"resourceUri":        resource.Name,
		"relatedToolCount":   len(relatedToolRefs),
		"relatedPromptCount": len(relatedPromptRefs),
		"relatedSkillCount":  len(relatedSkillRefs),
	}
	_ = s.recordResourceAudit(ctx, principal, input, resource, "success", "read AI Gateway resource manifest", relatedIDs)
	return domainaigateway.ResourceReadResult{
		Name:       resource.Name,
		URI:        resource.Name,
		MIMEType:   "application/json",
		Text:       text,
		Data:       document,
		RelatedIDs: relatedIDs,
		Audit: map[string]any{
			"riskLevel": domainaigateway.RiskLevelRead,
		},
	}, nil
}

func (s *Service) GetPrompt(ctx context.Context, principal domainidentity.Principal, input domainaigateway.PromptGetRequest) (domainaigateway.PromptGetResult, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Arguments == nil {
		input.Arguments = map[string]any{}
	}
	if input.Context == nil {
		input.Context = map[string]any{}
	}
	prompt, ok := s.promptByName(input.Name)
	if !ok {
		prompt = domainaigateway.PromptCapability{Name: input.Name}
		err := fmt.Errorf("%w: unknown AI Gateway prompt %s", apperrors.ErrInvalidArgument, input.Name)
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	if err := s.authorizePrompt(ctx, principal, prompt); err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	skill, err := s.authorizeSkillContext(ctx, principal, input.AIClientID, input.SkillID)
	if err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	bindings, err := s.activeSkillBindings(ctx, principal, input.AIClientID)
	if err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	if err := authorizePromptSkillBindingWithCapabilities(prompt, bindings, input.SkillID, s.gatewayResources(), s.gatewayResourceCapabilityRefs(), s.gatewaySkills()); err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	if skill != nil {
		*skill = narrowSkillCapabilityByBindingsWithSkills(*skill, bindings, input.SkillID, s.gatewaySkills())
	}

	messages := gatewayPromptMessages(prompt, input, skill)
	relatedIDs := map[string]any{
		"promptName":   prompt.Name,
		"messageCount": len(messages),
	}
	if skill != nil {
		relatedIDs["skillId"] = skill.ID
	}
	_ = s.recordPromptAudit(ctx, principal, input, prompt, "success", "read AI Gateway prompt template", relatedIDs)
	return domainaigateway.PromptGetResult{
		Name:        prompt.Name,
		Description: prompt.Description,
		Messages:    messages,
		RelatedIDs:  relatedIDs,
		Audit: map[string]any{
			"riskLevel": domainaigateway.RiskLevelAnalyze,
			"skillId":   strings.TrimSpace(input.SkillID),
		},
	}, nil
}

func (s *Service) holdToolInvocation(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest, tool domainaigateway.ToolCapability, decision gatewayRiskDecision, redactionSummary gatewayRedactionAuditSummary) (domainaigateway.ToolInvocationResult, error) {
	trackingID := uuid.NewString()
	result := decision.result()
	relatedIDs := map[string]any{
		"gatewayDecisionId": trackingID,
		"strategy":          decision.Strategy,
	}
	if decision.PolicyID != "" {
		relatedIDs["policyId"] = decision.PolicyID
	}
	switch decision.Strategy {
	case gatewayRiskRequireApproval:
		relatedIDs["approvalRequestId"] = trackingID
	case gatewayRiskRequireHumanConfirm:
		relatedIDs["confirmationRequestId"] = trackingID
	case gatewayRiskDryRunOnly:
		relatedIDs["dryRunId"] = trackingID
	}
	summary := gatewayHoldSummary(tool, decision)
	var expiresAt *time.Time
	if decision.requiresApproval() {
		if s.repo == nil {
			err := fmt.Errorf("%w: AI Gateway approval repository is not configured", apperrors.ErrInvalidArgument)
			_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "failure", err.Error(), relatedIDs, redactionSummary)
			return domainaigateway.ToolInvocationResult{}, err
		}
		now := time.Now().UTC()
		deliveryApprovalPolicy, hasDeliveryApprovalPolicy := s.gatewayDeliveryApprovalPolicy(ctx, decision)
		expiresAt = gatewayApprovalExpiresAt(decision, deliveryApprovalPolicy, hasDeliveryApprovalPolicy, now)
		actorType, actorID := gatewaySubject(principal)
		meta := requestctx.FromContext(ctx)
		approvalRouting := gatewayApprovalRoutingFromDecision(decision, deliveryApprovalPolicy, hasDeliveryApprovalPolicy)
		approvalRouting = s.resolveGatewayApprovalOnCall(ctx, input, approvalRouting)
		if len(approvalRouting) > 0 {
			relatedIDs["approvalRouting"] = approvalRouting
		}
		request := domainaigateway.ApprovalRequest{
			ID:                trackingID,
			Status:            "pending",
			Strategy:          string(decision.Strategy),
			PolicyID:          decision.PolicyID,
			ApprovalPolicyRef: decision.ApprovalPolicyRef,
			ActorType:         actorType,
			ActorID:           actorID,
			ActorName:         principal.UserName,
			ActorRoles:        normalizeStringSlice(principal.Roles),
			ActorTeams:        normalizeStringSlice(principal.Teams),
			AIClientID:        strings.TrimSpace(input.AIClientID),
			AIClientName:      strings.TrimSpace(input.AIClientName),
			SkillID:           strings.TrimSpace(input.SkillID),
			ToolName:          tool.Name,
			RiskLevel:         tool.RiskLevel,
			RequiresApproval:  true,
			ResourceScope:     gatewayAuditScope(input.Input, nil),
			ToolInput:         sanitizeGatewayMap(input.Input),
			RelatedIDs:        relatedIDs,
			Output:            map[string]any{},
			Summary:           summary,
			RequestID:         firstNonEmpty(input.RequestID, meta.RequestID),
			SourceIP:          meta.SourceIP,
			ExpiresAt:         expiresAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		created, err := s.repo.CreateApprovalRequest(ctx, request)
		if err != nil {
			_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "failure", err.Error(), relatedIDs, redactionSummary)
			return domainaigateway.ToolInvocationResult{}, err
		}
		relatedIDs = created.RelatedIDs
		if relatedIDs == nil {
			relatedIDs = map[string]any{}
		}
	}
	_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, result, summary, relatedIDs, redactionSummary)
	audit := map[string]any{
		"riskLevel":        tool.RiskLevel,
		"requiresApproval": decision.requiresApproval(),
		"strategy":         decision.Strategy,
		"policyId":         decision.PolicyID,
		"trackingId":       trackingID,
	}
	addGatewayRedactionAuditMetadata(audit, redactionSummary)
	return domainaigateway.ToolInvocationResult{
		ToolName:         tool.Name,
		RiskLevel:        tool.RiskLevel,
		RequiresApproval: decision.requiresApproval(),
		Result:           result,
		Output: map[string]any{
			"status":        result,
			"toolName":      tool.Name,
			"strategy":      decision.Strategy,
			"policyId":      decision.PolicyID,
			"trackingId":    trackingID,
			"message":       summary,
			"dryRun":        decision.Strategy == gatewayRiskDryRunOnly,
			"nextAction":    gatewayHoldNextAction(decision),
			"resourceScope": gatewayAuditScope(input.Input, nil),
			"expiresAt":     expiresAt,
		},
		RelatedIDs: relatedIDs,
		Audit:      audit,
	}, nil
}

func gatewayHoldSummary(tool domainaigateway.ToolCapability, decision gatewayRiskDecision) string {
	switch decision.Strategy {
	case gatewayRiskRequireApproval:
		return "AI Gateway policy requires approval before executing " + tool.Name
	case gatewayRiskRequireHumanConfirm:
		return "AI Gateway policy requires human confirmation before executing " + tool.Name
	case gatewayRiskDryRunOnly:
		return "AI Gateway policy allows dry-run only for " + tool.Name
	default:
		return "AI Gateway policy held " + tool.Name
	}
}

func gatewayHoldNextAction(decision gatewayRiskDecision) string {
	switch decision.Strategy {
	case gatewayRiskRequireApproval:
		return "approval_required"
	case gatewayRiskRequireHumanConfirm:
		return "human_confirmation_required"
	case gatewayRiskDryRunOnly:
		return "no_mutation_executed"
	default:
		return ""
	}
}

func (s *Service) invokeGatewayTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	switch {
	case strings.HasPrefix(tool.Name, "delivery."):
		return s.invokeDeliveryTool(ctx, principal, tool, input)
	case strings.HasPrefix(tool.Name, "k8s."):
		return s.invokeKubernetesTool(ctx, principal, tool, input)
	case tool.Name == "diagnosis.release_failure.analyze":
		return s.invokeReleaseFailureDiagnosis(ctx, principal, input)
	default:
		output, relatedIDs, handled, err := s.gatewayRegistry().InvokeTool(ctx, principal, tool, input)
		if handled {
			return output, relatedIDs, err
		}
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
}

func (s *Service) authorizeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability) error {
	for _, permissionKey := range tool.PermissionKeys {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) authorizeResource(ctx context.Context, principal domainidentity.Principal, resource domainaigateway.ResourceCapability) error {
	for _, permissionKey := range resource.PermissionKeys {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) authorizePrompt(ctx context.Context, principal domainidentity.Principal, prompt domainaigateway.PromptCapability) error {
	for _, permissionKey := range prompt.PermissionKeys {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) authorizeSkillContext(ctx context.Context, principal domainidentity.Principal, aiClientID, skillID string) (*domainaigateway.SkillCapability, error) {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return nil, nil
	}
	skill, ok := s.skillByID(skillID)
	if !ok {
		return nil, fmt.Errorf("%w: unknown AI Gateway skill %s", apperrors.ErrInvalidArgument, skillID)
	}
	for _, permissionKey := range skill.PermissionKeys {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey); err != nil {
			return nil, err
		}
	}
	bindings, err := s.activeSkillBindings(ctx, principal, aiClientID)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return &skill, nil
	}
	if _, ok := skillBindingRefsWithSkills(bindings, skillID, s.gatewaySkills())[skillID]; !ok {
		return nil, fmt.Errorf("%w: AI Gateway skill binding rejected %s: skill is not bound", apperrors.ErrAccessDenied, skillID)
	}
	return &skill, nil
}

func (s *Service) authorizeToolGrant(ctx context.Context, principal domainidentity.Principal, aiClientID string, tool domainaigateway.ToolCapability, invocationScope map[string]string) (bool, error) {
	grants, err := s.activeToolGrants(ctx, principal, aiClientID)
	if err != nil {
		return false, err
	}
	if len(grants) == 0 {
		return false, nil
	}
	permissionKeys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return false, err
	}
	allowed, requiresApproval, reason := toolAllowedByGrantsForInvocation(tool, grants, permissionKeys, invocationScope)
	if allowed {
		return requiresApproval, nil
	}
	return false, fmt.Errorf("%w: AI Gateway tool grant rejected %s: %s", apperrors.ErrAccessDenied, tool.Name, reason)
}

func (s *Service) filterToolsByGrants(ctx context.Context, principal domainidentity.Principal, aiClientID string, tools []domainaigateway.ToolCapability, permissionKeys []string) ([]domainaigateway.ToolCapability, int, error) {
	grants, err := s.activeToolGrants(ctx, principal, aiClientID)
	if err != nil {
		return nil, 0, err
	}
	if len(grants) == 0 {
		return tools, 0, nil
	}
	out := make([]domainaigateway.ToolCapability, 0, len(tools))
	denied := 0
	for _, tool := range tools {
		allowed, requiresApproval, _ := toolAllowedByGrants(tool, grants, permissionKeys)
		if allowed {
			if requiresApproval {
				tool.RequiresApproval = true
			}
			out = append(out, tool)
			continue
		}
		denied++
	}
	return out, denied, nil
}

func (s *Service) activeToolGrants(ctx context.Context, principal domainidentity.Principal, aiClientID string) ([]domainaigateway.ToolGrant, error) {
	if s.repo == nil {
		return nil, nil
	}
	aiClientID = strings.TrimSpace(aiClientID)
	now := time.Now().UTC()
	out := make([]domainaigateway.ToolGrant, 0)
	seen := map[string]struct{}{}
	appendGrants := func(subjectType, subjectID string) error {
		subjectType = normalizeSubjectType(subjectType)
		subjectID = strings.TrimSpace(subjectID)
		if subjectType == "" || subjectID == "" {
			return nil
		}
		items, err := s.repo.ListActiveToolGrants(ctx, subjectType, subjectID, aiClientID, now)
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			out = append(out, item)
		}
		return nil
	}
	subjectType, subjectID := gatewaySubject(principal)
	if err := appendGrants(subjectType, subjectID); err != nil {
		return nil, err
	}
	for _, role := range principal.Roles {
		if err := appendGrants("role", role); err != nil {
			return nil, err
		}
	}
	for _, team := range principal.Teams {
		if err := appendGrants("team", team); err != nil {
			return nil, err
		}
	}
	if aiClientID != "" {
		if err := appendGrants("ai_client", aiClientID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) authorizeAccessPolicy(ctx context.Context, principal domainidentity.Principal, aiClientID, skillID string, tool *domainaigateway.ToolCapability, invocationScope map[string]string, toolInput map[string]any) (gatewayRiskDecision, map[string]any, gatewayRedactionAuditSummary, error) {
	decision := gatewayRiskDecisionForTool(*tool)
	var redactionSummary gatewayRedactionAuditSummary
	policies, err := s.activeAccessPolicies(ctx, principal, aiClientID)
	if err != nil {
		return decision, toolInput, redactionSummary, err
	}
	if len(policies) == 0 {
		return decision, toolInput, redactionSummary, nil
	}
	allowed, policyDecision, reason := toolAllowedByAccessPoliciesForInvocationWithSkills(*tool, policies, skillID, invocationScope, s.gatewaySkills())
	if !allowed {
		return decision, toolInput, redactionSummary, fmt.Errorf("%w: AI Gateway access policy rejected %s: %s", apperrors.ErrAccessDenied, tool.Name, reason)
	}
	nextInput, redactionSummary, err := s.enforceAccessPolicyConditions(ctx, principal, aiClientID, skillID, *tool, invocationScope, policies, toolInput)
	if err != nil {
		return decision, toolInput, redactionSummary, err
	}
	if policyDecision.Strategy != "" {
		decision = policyDecision
	}
	if decision.requiresApproval() {
		tool.RequiresApproval = true
	}
	return decision, nextInput, redactionSummary, nil
}

func (s *Service) sanitizeToolOutputByAccessPolicy(ctx context.Context, principal domainidentity.Principal, aiClientID, skillID string, tool domainaigateway.ToolCapability, invocationScope map[string]string, output any) (any, gatewayRedactionAuditSummary, error) {
	var summary gatewayRedactionAuditSummary
	policies, err := s.activeAccessPolicies(ctx, principal, aiClientID)
	if err != nil {
		return output, summary, err
	}
	if len(policies) == 0 {
		return output, summary, nil
	}
	out := output
	for _, policy := range policies {
		if grantEffect(policy.Effect) != "allow" || len(policy.Conditions) == 0 {
			continue
		}
		if !accessPolicyToolSelectorsMatchWithSkills(policy, tool, skillID, s.gatewaySkills()) {
			continue
		}
		if !gatewayResourceScopeMatches(policy.ResourceScopes, invocationScope) {
			continue
		}
		nextOutput, policySummary, err := enforceGatewayOutputRedactionPolicyCondition(policy, tool, out)
		summary.merge(policySummary)
		if err != nil {
			return out, summary, err
		}
		out = nextOutput
	}
	return out, summary, nil
}

func (s *Service) enforceAccessPolicyConditions(ctx context.Context, principal domainidentity.Principal, aiClientID, skillID string, tool domainaigateway.ToolCapability, invocationScope map[string]string, policies []domainaigateway.AccessPolicy, toolInput map[string]any) (map[string]any, gatewayRedactionAuditSummary, error) {
	out := toolInput
	var redactionSummary gatewayRedactionAuditSummary
	rateLimits := make([]gatewayInvocationLimit, 0)
	for _, policy := range policies {
		if grantEffect(policy.Effect) != "allow" || len(policy.Conditions) == 0 {
			continue
		}
		if !accessPolicyToolSelectorsMatchWithSkills(policy, tool, skillID, s.gatewaySkills()) {
			continue
		}
		if !gatewayResourceScopeMatches(policy.ResourceScopes, invocationScope) {
			continue
		}
		for _, limit := range gatewayRateLimitRules(policy.Conditions, policy.ID) {
			rateLimits = append(rateLimits, limit)
		}
		for _, limit := range gatewayBudgetLimitRules(policy.Conditions, policy.ID) {
			if err := s.enforceGatewayInvocationLimit(ctx, principal, aiClientID, tool.Name, limit); err != nil {
				return out, redactionSummary, err
			}
		}
		for _, budget := range gatewayUsageBudgetRules(policy.Conditions, policy.ID) {
			if err := s.enforceGatewayUsageBudget(ctx, principal, aiClientID, tool.Name, budget); err != nil {
				return out, redactionSummary, err
			}
		}
		nextInput, policySummary, err := enforceGatewayRedactionPolicyCondition(policy, tool, out)
		redactionSummary.merge(policySummary)
		if err != nil {
			return out, redactionSummary, err
		}
		out = nextInput
	}
	for _, limit := range rateLimits {
		if err := s.enforceGatewayInvocationLimit(ctx, principal, aiClientID, tool.Name, limit); err != nil {
			return out, redactionSummary, err
		}
	}
	return out, redactionSummary, nil
}

type gatewayInvocationLimit struct {
	Kind        string
	PolicyID    string
	Limit       int
	Burst       int
	Window      time.Duration
	WindowLabel string
	Scope       string
	Mode        string
}

type gatewayUsageBudget struct {
	Kind        string
	PolicyID    string
	Limit       float64
	Window      time.Duration
	WindowLabel string
	Scope       string
}

func gatewayRateLimitRules(conditions map[string]any, policyID string) []gatewayInvocationLimit {
	values := gatewayConditionValues(conditions, "rateLimit", "rate_limit", "rateLimits")
	scope := gatewayLimitScope(values, "actor_client_tool")
	mode := gatewayRateLimitMode(values)
	burst := gatewayRateLimitBurst(values)
	out := make([]gatewayInvocationLimit, 0, 4)
	if limit, ok := gatewayFirstPositiveInt(values, "qps", "maxCallsPerSecond", "maxInvocationsPerSecond", "callsPerSecond", "invocationsPerSecond", "perSecond", "second"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "rate limit", PolicyID: policyID, Limit: limit, Burst: burst, Window: time.Second, WindowLabel: "1s", Scope: scope, Mode: mode})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerMinute", "maxInvocationsPerMinute", "callsPerMinute", "invocationsPerMinute", "perMinute", "minute", "rpm"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "rate limit", PolicyID: policyID, Limit: limit, Burst: burst, Window: time.Minute, WindowLabel: "1m", Scope: scope, Mode: mode})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerHour", "maxInvocationsPerHour", "callsPerHour", "invocationsPerHour", "perHour", "hour", "rph"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "rate limit", PolicyID: policyID, Limit: limit, Burst: burst, Window: time.Hour, WindowLabel: "1h", Scope: scope, Mode: mode})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCalls", "maxInvocations", "limit"); ok {
		window, label := gatewayConditionWindow(values, time.Minute, "1m")
		out = append(out, gatewayInvocationLimit{Kind: "rate limit", PolicyID: policyID, Limit: limit, Burst: burst, Window: window, WindowLabel: label, Scope: scope, Mode: mode})
	}
	return out
}

func gatewayRateLimitMode(values map[string]any) string {
	mode := strings.ToLower(strings.TrimSpace(gatewayFirstString(values, "mode", "algorithm", "strategy")))
	mode = strings.ReplaceAll(mode, "-", "_")
	switch mode {
	case "gcra", "token_bucket", "tokenbucket", "leaky_bucket", "leakybucket", "smooth", "strict":
		return "gcra"
	case "sliding_window", "slidingwindow", "rolling_window", "rollingwindow", "audit_window", "auditwindow":
		return "sliding_window"
	default:
		return "counter"
	}
}

func gatewayRateLimitBurst(values map[string]any) int {
	if burst, ok := gatewayFirstPositiveInt(values, "burst", "burstSize", "capacity", "bucketSize", "maxBurst"); ok {
		return burst
	}
	return 1
}

func gatewayBudgetLimitRules(conditions map[string]any, policyID string) []gatewayInvocationLimit {
	values := gatewayConditionValues(conditions, "budget", "budgets", "budgetPolicy")
	scope := gatewayLimitScope(values, "actor_client")
	out := make([]gatewayInvocationLimit, 0, 4)
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerHour", "maxInvocationsPerHour", "hourlyCalls", "hourlyInvocations", "hourlyInvocationBudget"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "budget", PolicyID: policyID, Limit: limit, Window: time.Hour, WindowLabel: "1h", Scope: scope})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerDay", "maxInvocationsPerDay", "maxDailyCalls", "maxDailyInvocations", "dailyCalls", "dailyInvocations", "dailyInvocationBudget", "dailyBudget", "daily"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "budget", PolicyID: policyID, Limit: limit, Window: 24 * time.Hour, WindowLabel: "24h", Scope: scope})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerMonth", "maxInvocationsPerMonth", "maxMonthlyCalls", "maxMonthlyInvocations", "monthlyCalls", "monthlyInvocations", "monthlyInvocationBudget", "monthlyBudget", "monthly"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "budget", PolicyID: policyID, Limit: limit, Window: 30 * 24 * time.Hour, WindowLabel: "30d", Scope: scope})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxBudgetCalls", "maxBudgetInvocations"); ok {
		window, label := gatewayConditionWindow(values, 24*time.Hour, "24h")
		out = append(out, gatewayInvocationLimit{Kind: "budget", PolicyID: policyID, Limit: limit, Window: window, WindowLabel: label, Scope: scope})
	}
	return out
}

func gatewayUsageBudgetRules(conditions map[string]any, policyID string) []gatewayUsageBudget {
	values := gatewayConditionValues(conditions, "budget", "budgets", "budgetPolicy")
	scope := gatewayLimitScope(values, "actor_client")
	window, label := gatewayConditionWindow(values, 24*time.Hour, "24h")
	out := make([]gatewayUsageBudget, 0, 4)
	appendRule := func(kind string, limit float64, window time.Duration, label string) {
		if limit > 0 && window > 0 {
			out = append(out, gatewayUsageBudget{Kind: kind, PolicyID: policyID, Limit: limit, Window: window, WindowLabel: label, Scope: scope})
		}
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxTokensPerHour", "hourlyTokens", "hourlyTokenBudget"); ok {
		appendRule("token budget", limit, time.Hour, "1h")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxTokensPerDay", "dailyTokens", "dailyTokenBudget"); ok {
		appendRule("token budget", limit, 24*time.Hour, "24h")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxTokensPerMonth", "monthlyTokens", "monthlyTokenBudget"); ok {
		appendRule("token budget", limit, 30*24*time.Hour, "30d")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxTokens", "maxTotalTokens", "tokenBudget", "maxBudgetTokens"); ok {
		appendRule("token budget", limit, window, label)
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxCostPerHour", "hourlyCost", "hourlyCostBudget"); ok {
		appendRule("cost budget", limit, time.Hour, "1h")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxCostPerDay", "dailyCost", "dailyCostBudget"); ok {
		appendRule("cost budget", limit, 24*time.Hour, "24h")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxCostPerMonth", "monthlyCost", "monthlyCostBudget"); ok {
		appendRule("cost budget", limit, 30*24*time.Hour, "30d")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxCost", "maxSpend", "costBudget", "maxBudgetCost"); ok {
		appendRule("cost budget", limit, window, label)
	}
	return out
}

func (s *Service) enforceGatewayInvocationLimit(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, limit gatewayInvocationLimit) error {
	if limit.Limit <= 0 || limit.Window <= 0 || s == nil || s.repo == nil {
		return nil
	}
	if strings.EqualFold(limit.Kind, "rate limit") && limit.Mode == "gcra" {
		state, err := s.applyGatewayRateLimitState(ctx, principal, aiClientID, toolName, limit)
		if err == nil {
			if state.Allowed {
				return nil
			}
			return fmt.Errorf("%w: AI Gateway %s policy %s exceeded for %s (retry after %s)", apperrors.ErrAccessDenied, limit.Kind, strings.TrimSpace(limit.PolicyID), toolName, formatGatewayRateLimitRetryAfter(state.RetryAfter))
		}
		if !gatewayRateLimitStateFallbackAllowed(err) {
			return err
		}
	}
	if strings.EqualFold(limit.Kind, "rate limit") && limit.Mode == "counter" {
		counter, err := s.incrementGatewayRateLimitCounter(ctx, principal, aiClientID, toolName, limit)
		if err == nil {
			if counter.Count <= limit.Limit {
				return nil
			}
			return fmt.Errorf("%w: AI Gateway %s policy %s exceeded for %s (%d/%d accepted calls in %s)", apperrors.ErrAccessDenied, limit.Kind, strings.TrimSpace(limit.PolicyID), toolName, counter.Count-1, limit.Limit, limit.WindowLabel)
		}
		if !gatewayRateLimitCounterFallbackAllowed(err) {
			return err
		}
	}
	count, err := s.gatewayAcceptedInvocationCount(ctx, principal, aiClientID, toolName, limit)
	if err != nil {
		return err
	}
	if count < limit.Limit {
		return nil
	}
	return fmt.Errorf("%w: AI Gateway %s policy %s exceeded for %s (%d/%d accepted calls in %s)", apperrors.ErrAccessDenied, limit.Kind, strings.TrimSpace(limit.PolicyID), toolName, count, limit.Limit, limit.WindowLabel)
}

func (s *Service) incrementGatewayRateLimitCounter(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, limit gatewayInvocationLimit) (domainaigateway.RateLimitCounter, error) {
	actorType, actorID := gatewaySubject(principal)
	windowStart, windowEnd := gatewayRateLimitWindow(time.Now().UTC(), limit.Window)
	key := gatewayRateLimitCounterKey(actorType, actorID, aiClientID, toolName, limit, windowStart)
	counter := domainaigateway.RateLimitCounter{
		Key:         key,
		PolicyID:    strings.TrimSpace(limit.PolicyID),
		Scope:       normalizeGatewayLimitScope(limit.Scope),
		ActorType:   actorType,
		ActorID:     actorID,
		AIClientID:  strings.TrimSpace(aiClientID),
		ToolName:    strings.TrimSpace(toolName),
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Limit:       limit.Limit,
		Metadata: map[string]any{
			"kind":        limit.Kind,
			"windowLabel": limit.WindowLabel,
		},
	}
	if s == nil {
		return counter, nil
	}
	if s.rateLimits != nil {
		next, err := s.rateLimits.IncrementRateLimitCounter(ctx, counter)
		if err == nil {
			return next, nil
		}
		if !gatewayExternalRateLimitFallbackAllowed(err) {
			return domainaigateway.RateLimitCounter{}, err
		}
	}
	if s.repo == nil {
		return counter, nil
	}
	return s.repo.IncrementRateLimitCounter(ctx, counter)
}

func (s *Service) applyGatewayRateLimitState(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, limit gatewayInvocationLimit) (domainaigateway.RateLimitState, error) {
	burst := limit.Burst
	if burst <= 0 {
		burst = 1
	}
	actorType, actorID := gatewaySubject(principal)
	key := gatewayRateLimitStateKey(actorType, actorID, aiClientID, toolName, limit)
	state := domainaigateway.RateLimitState{
		Key:             key,
		PolicyID:        strings.TrimSpace(limit.PolicyID),
		Scope:           normalizeGatewayLimitScope(limit.Scope),
		ActorType:       actorType,
		ActorID:         actorID,
		AIClientID:      strings.TrimSpace(aiClientID),
		ToolName:        strings.TrimSpace(toolName),
		Limit:           limit.Limit,
		Burst:           burst,
		IntervalSeconds: limit.Window.Seconds() / float64(limit.Limit),
		Metadata: map[string]any{
			"kind":        limit.Kind,
			"mode":        "gcra",
			"windowLabel": limit.WindowLabel,
		},
	}
	if s == nil {
		return state, nil
	}
	if s.rateLimits != nil {
		next, err := s.rateLimits.ApplyRateLimitState(ctx, state)
		if err == nil {
			return next, nil
		}
		if !gatewayExternalRateLimitFallbackAllowed(err) {
			return domainaigateway.RateLimitState{}, err
		}
	}
	if s.repo == nil {
		return state, nil
	}
	return s.repo.ApplyRateLimitState(ctx, state)
}

func gatewayExternalRateLimitFallbackAllowed(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.Canceled)
}

func gatewayRateLimitCounterFallbackAllowed(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "ai_gateway_rate_limit_counters") && strings.Contains(text, "does not exist")
}

func gatewayRateLimitStateFallbackAllowed(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "ai_gateway_rate_limit_states") && strings.Contains(text, "does not exist")
}

func formatGatewayRateLimitRetryAfter(value time.Duration) string {
	if value <= 0 {
		return "now"
	}
	if value < time.Second {
		return value.String()
	}
	return value.Round(time.Second).String()
}

func (s *Service) enforceGatewayUsageBudget(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, budget gatewayUsageBudget) error {
	if budget.Limit <= 0 || budget.Window <= 0 || s == nil || s.repo == nil {
		return nil
	}
	used, err := s.gatewayUsageBudgetConsumed(ctx, principal, aiClientID, toolName, budget)
	if err != nil {
		return err
	}
	if used < budget.Limit {
		return nil
	}
	return fmt.Errorf("%w: AI Gateway %s policy %s exceeded for %s (%s/%s in %s)", apperrors.ErrAccessDenied, budget.Kind, strings.TrimSpace(budget.PolicyID), toolName, formatGatewayBudgetValue(used), formatGatewayBudgetValue(budget.Limit), budget.WindowLabel)
}

func (s *Service) gatewayAcceptedInvocationCount(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, limit gatewayInvocationLimit) (int, error) {
	now := time.Now().UTC()
	from := now.Add(-limit.Window)
	filter := gatewayLimitAuditFilter(principal, aiClientID, toolName, limit.Scope)
	filter.Action = "ai_gateway.tool.invoke"
	filter.From = &from
	filter.To = &now
	filter.Limit = limit.Limit + 1
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 500
	}
	items, err := s.repo.ListAuditLogs(ctx, filter)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if gatewayAuditResultCountsTowardLimits(item.Result) {
			count++
		}
	}
	return count, nil
}

func (s *Service) gatewayUsageBudgetConsumed(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, budget gatewayUsageBudget) (float64, error) {
	now := time.Now().UTC()
	from := now.Add(-budget.Window)
	filter := gatewayLimitAuditFilter(principal, aiClientID, toolName, budget.Scope)
	filter.Action = "ai_gateway.tool.invoke"
	filter.From = &from
	filter.To = &now
	filter.Limit = 500
	items, err := s.repo.ListAuditLogs(ctx, filter)
	if err != nil {
		return 0, err
	}
	used := 0.0
	for _, item := range items {
		if !gatewayAuditResultCountsTowardLimits(item.Result) {
			continue
		}
		switch budget.Kind {
		case "token budget":
			used += gatewayAuditTokenUsage(item)
		case "cost budget":
			used += gatewayAuditCostUsage(item)
		}
	}
	return used, nil
}

func gatewayLimitAuditFilter(principal domainidentity.Principal, aiClientID, toolName, scope string) domainaigateway.AuditLogFilter {
	actorType, actorID := gatewaySubject(principal)
	scope = normalizeGatewayLimitScope(scope)
	filter := domainaigateway.AuditLogFilter{}
	switch scope {
	case "global":
	case "client":
		filter.AIClientID = strings.TrimSpace(aiClientID)
	case "client_tool":
		filter.AIClientID = strings.TrimSpace(aiClientID)
		filter.ToolName = strings.TrimSpace(toolName)
	case "actor":
		filter.ActorType = actorType
		filter.ActorID = actorID
	case "actor_tool":
		filter.ActorType = actorType
		filter.ActorID = actorID
		filter.ToolName = strings.TrimSpace(toolName)
	case "actor_client":
		filter.ActorType = actorType
		filter.ActorID = actorID
		filter.AIClientID = strings.TrimSpace(aiClientID)
	default:
		filter.ActorType = actorType
		filter.ActorID = actorID
		filter.AIClientID = strings.TrimSpace(aiClientID)
		filter.ToolName = strings.TrimSpace(toolName)
	}
	return filter
}

func gatewayRateLimitWindow(now time.Time, window time.Duration) (time.Time, time.Time) {
	if window <= 0 {
		window = time.Minute
	}
	unixNano := now.UnixNano()
	windowNano := int64(window)
	if windowNano <= 0 {
		windowNano = int64(time.Minute)
	}
	startUnix := unixNano - unixNano%windowNano
	start := time.Unix(0, startUnix).UTC()
	return start, start.Add(window)
}

func gatewayRateLimitCounterKey(actorType, actorID, aiClientID, toolName string, limit gatewayInvocationLimit, windowStart time.Time) string {
	scope := normalizeGatewayLimitScope(limit.Scope)
	if scope == "" {
		scope = "actor_client_tool"
	}
	parts := append(gatewayLimitKeyParts(limit, scope),
		"window", windowStart.UTC().Format(time.RFC3339Nano),
	)
	switch scope {
	case "global":
	case "client":
		parts = append(parts, "client", strings.TrimSpace(aiClientID))
	case "client_tool":
		parts = append(parts, "client", strings.TrimSpace(aiClientID), "tool", strings.TrimSpace(toolName))
	case "actor":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID))
	case "actor_tool":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "tool", strings.TrimSpace(toolName))
	case "actor_client":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "client", strings.TrimSpace(aiClientID))
	default:
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "client", strings.TrimSpace(aiClientID), "tool", strings.TrimSpace(toolName))
	}
	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func gatewayRateLimitStateKey(actorType, actorID, aiClientID, toolName string, limit gatewayInvocationLimit) string {
	scope := normalizeGatewayLimitScope(limit.Scope)
	if scope == "" {
		scope = "actor_client_tool"
	}
	parts := gatewayLimitKeyParts(limit, scope)
	switch scope {
	case "global":
	case "client":
		parts = append(parts, "client", strings.TrimSpace(aiClientID))
	case "client_tool":
		parts = append(parts, "client", strings.TrimSpace(aiClientID), "tool", strings.TrimSpace(toolName))
	case "actor":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID))
	case "actor_tool":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "tool", strings.TrimSpace(toolName))
	case "actor_client":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "client", strings.TrimSpace(aiClientID))
	default:
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "client", strings.TrimSpace(aiClientID), "tool", strings.TrimSpace(toolName))
	}
	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func gatewayLimitKeyParts(limit gatewayInvocationLimit, scope string) []string {
	return []string{
		"policy", strings.TrimSpace(limit.PolicyID),
		"kind", strings.ToLower(strings.TrimSpace(limit.Kind)),
		"mode", strings.ToLower(strings.TrimSpace(limit.Mode)),
		"scope", scope,
		"windowDuration", strconv.FormatInt(int64(limit.Window), 10),
		"windowLabel", strings.TrimSpace(limit.WindowLabel),
		"limit", strconv.Itoa(limit.Limit),
		"burst", strconv.Itoa(limit.Burst),
	}
}

func gatewayAuditResultCountsTowardLimits(result string) bool {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "success", "failure", "failed", "dry_run":
		return true
	default:
		return false
	}
}

func gatewayAuditTokenUsage(item domainaigateway.AuditLog) float64 {
	values := gatewayUsageWithDerivedTotals(gatewayAuditUsageValues(item.Metadata))
	if total, ok := gatewayFirstPositiveFloat(values, "totalTokens", "total_tokens", "tokens", "tokenCount", "billableTokens", "billable_tokens", "billedTokens", "billed_tokens", "usageTokens", "usage_tokens"); ok {
		return total
	}
	return gatewayPositiveFloatSum(values, "inputTokens", "input_tokens", "promptTokens", "prompt_tokens", "outputTokens", "output_tokens", "completionTokens", "completion_tokens")
}

func gatewayAuditCostUsage(item domainaigateway.AuditLog) float64 {
	values := gatewayUsageWithDerivedTotals(gatewayAuditUsageValues(item.Metadata))
	if total, ok := gatewayFirstPositiveFloat(values, "totalCost", "total_cost", "cost", "costUsd", "costUSD", "usd", "estimatedCost", "estimatedCostUsd", "responseCost", "response_cost", "totalCostMicros", "total_cost_micros", "costMicros", "cost_micros"); ok {
		return total
	}
	return gatewayPositiveFloatSum(values, "inputCost", "input_cost", "promptCost", "prompt_cost", "outputCost", "output_cost", "completionCost", "completion_cost")
}

func gatewayAuditUsageValues(metadata map[string]any) map[string]any {
	values := make(map[string]any, len(metadata)+8)
	for key, value := range metadata {
		values[key] = value
	}
	for _, key := range []string{"usage", "tokenUsage", "aiUsage", "providerUsage", "llmUsage", "metering", "billing", "costUsage"} {
		raw, ok := gatewayConditionRaw(metadata, key)
		if !ok {
			continue
		}
		for nestedKey, nestedValue := range mapValue(raw) {
			values[nestedKey] = nestedValue
		}
	}
	return values
}

func gatewayProviderUsageSummary(output any, relatedIDs map[string]any) map[string]any {
	summary := map[string]any{}
	for _, source := range []any{gatewayUsageSerializableValue(output), relatedIDs} {
		for _, usage := range gatewayProviderUsageCandidates(source) {
			gatewayMergeUsageSummary(summary, usage)
		}
	}
	if len(summary) == 0 {
		return nil
	}
	return gatewayNormalizeUsageSummary(summary)
}

func gatewayUsageSerializableValue(value any) any {
	switch value.(type) {
	case nil, map[string]any, []any, []map[string]any:
		return value
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return value
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return value
		}
		return out
	}
}

func gatewayProviderUsageCandidates(value any) []map[string]any {
	out := make([]map[string]any, 0)
	gatewayCollectProviderUsageCandidates(value, "$", 0, &out)
	return out
}

func gatewayCollectProviderUsageCandidates(value any, key string, depth int, out *[]map[string]any) {
	if out == nil || depth > 5 || value == nil {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if context := gatewayUsageDetailContext(key); context != "" {
			if usage := gatewayProviderUsageDetailNumbers(typed, context); len(usage) > 0 {
				*out = append(*out, usage)
				for childKey, child := range typed {
					switch child.(type) {
					case map[string]any, []any, []map[string]any:
						gatewayCollectProviderUsageCandidates(child, childKey, depth+1, out)
					}
				}
				return
			}
		}
		if gatewayUsageContainerKey(key) {
			if usage := gatewayPreferredBilledUsageNumbers(typed); len(usage) > 0 {
				*out = append(*out, usage)
				for childKey, child := range typed {
					switch normalizeGatewayConditionKey(childKey) {
					case "billedunits", "billedunit", "tokens":
						continue
					}
					switch child.(type) {
					case map[string]any, []any, []map[string]any:
						gatewayCollectProviderUsageCandidates(child, childKey, depth+1, out)
					}
				}
				return
			}
			if usage := gatewayUsageNumbersOnly(typed); len(usage) > 0 {
				*out = append(*out, usage)
				for childKey, child := range typed {
					switch child.(type) {
					case map[string]any, []any, []map[string]any:
						gatewayCollectProviderUsageCandidates(child, childKey, depth+1, out)
					}
				}
				return
			}
		}
		if usage := gatewayProviderNativeUsageNumbers(typed); len(usage) > 0 {
			*out = append(*out, usage)
			return
		}
		for childKey, child := range typed {
			gatewayCollectProviderUsageCandidates(child, childKey, depth+1, out)
		}
	case []any:
		for _, item := range typed {
			gatewayCollectProviderUsageCandidates(item, key, depth+1, out)
		}
	case []map[string]any:
		for _, item := range typed {
			gatewayCollectProviderUsageCandidates(item, key, depth+1, out)
		}
	}
}

func gatewayPreferredBilledUsageNumbers(values map[string]any) map[string]any {
	rawBilled, ok := gatewayConditionRaw(values, "billed_units")
	if !ok {
		return nil
	}
	rawTokens, ok := gatewayConditionRaw(values, "tokens")
	if !ok || len(mapValue(rawTokens)) == 0 {
		return nil
	}
	billed := mapValue(rawBilled)
	if len(billed) == 0 {
		return nil
	}
	out := gatewayUsageNumbersOnly(values)
	if out == nil {
		out = map[string]any{}
	}
	gatewayMergeUsageSummary(out, gatewayUsageNumbersOnly(billed))
	if len(out) == 0 {
		return nil
	}
	return gatewayNormalizeUsageSummary(out)
}

func gatewayUsageContainerKey(key string) bool {
	switch normalizeGatewayConditionKey(key) {
	case "usage", "tokenusage", "aiusage", "providerusage", "llmusage", "metering", "billing", "costusage", "usagemetadata", "tokenmetadata", "tokencount", "tokencounts", "tokens", "billedunits", "billedunit":
		return true
	default:
		return false
	}
}

func gatewayUsageDetailContext(key string) string {
	switch normalizeGatewayConditionKey(key) {
	case "prompttokendetails", "prompttokensdetails", "inputtokendetails", "inputtokensdetails", "requesttokendetails", "requesttokensdetails":
		return "input"
	case "completiontokendetails", "completiontokensdetails", "outputtokendetails", "outputtokensdetails", "responsetokendetails", "responsetokensdetails", "candidatestokendetails", "candidatestokensdetails":
		return "output"
	default:
		return ""
	}
}

func gatewayProviderUsageDetailNumbers(values map[string]any, context string) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		number, ok := gatewayPositiveFloat(value)
		if !ok {
			continue
		}
		normalized := normalizeGatewayConditionKey(key)
		switch context {
		case "input":
			switch normalized {
			case "cachedtokens", "cachetokens", "cachecreationtokens", "cachereadtokens", "cachewritetokens", "audiotokens", "texttokens", "imagetokens":
				existing, _ := gatewayPositiveFloat(out["inputTokens"])
				out["inputTokens"] = existing + gatewayNormalizeNativeUsageNumber(key, number)
			}
		case "output":
			switch normalized {
			case "reasoningtokens", "acceptedpredictiontokens", "rejectedpredictiontokens", "audiotokens", "texttokens", "imagetokens":
				existing, _ := gatewayPositiveFloat(out["outputTokens"])
				out["outputTokens"] = existing + gatewayNormalizeNativeUsageNumber(key, number)
			}
		}
	}
	return gatewayNormalizeUsageSummary(out)
}

func gatewayUsageNumbersOnly(values map[string]any) map[string]any {
	out := map[string]any{}
	hasGenericInput := gatewayNativeUsageHasAny(values, "inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count")
	hasGenericOutput := gatewayNativeUsageHasAny(values, "outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count")
	seen := map[string]struct{}{}
	for _, key := range gatewayUsageSummaryKeys() {
		normalized := normalizeGatewayConditionKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		value, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := gatewayPositiveFloat(value)
		if !ok {
			continue
		}
		gatewayAddNativeUsageNumber(out, key, number, hasGenericInput, hasGenericOutput)
	}
	if supplemental := gatewaySupplementalInputTokenUsage(values); supplemental > 0 {
		existing, _ := gatewayPositiveFloat(out["inputTokens"])
		out["inputTokens"] = existing + supplemental
	}
	if supplemental := gatewaySupplementalOutputTokenUsage(values); supplemental > 0 {
		existing, _ := gatewayPositiveFloat(out["outputTokens"])
		out["outputTokens"] = existing + supplemental
	}
	return out
}

func gatewayMergeUsageSummary(dst map[string]any, src map[string]any) {
	if dst == nil || len(src) == 0 {
		return
	}
	for key, value := range gatewayUsageWithDerivedTotals(src) {
		number, ok := gatewayPositiveFloat(value)
		if !ok {
			continue
		}
		canonical := gatewayCanonicalUsageKey(key)
		if existing, ok := gatewayPositiveFloat(dst[canonical]); ok {
			dst[canonical] = existing + number
		} else {
			dst[canonical] = number
		}
	}
}

func gatewayProviderNativeUsageNumbers(values map[string]any) map[string]any {
	out := map[string]any{}
	hasGenericInput := gatewayNativeUsageHasAny(values, "inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count")
	hasGenericOutput := gatewayNativeUsageHasAny(values, "outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count")
	tokenKeys := []string{
		"promptTokenCount", "prompt_token_count", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "inputTokenCount", "input_token_count", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage",
		"cachedContentTokenCount", "cached_content_token_count", "cachedContentTokens", "cached_content_tokens",
		"toolUsePromptTokenCount", "tool_use_prompt_token_count", "toolUsePromptTokens", "tool_use_prompt_tokens",
		"promptTokensDetailsCachedTokens", "prompt_tokens_details_cached_tokens",
		"cachedTokens", "cached_tokens", "promptCacheHitTokens", "prompt_cache_hit_tokens", "promptCacheMissTokens", "prompt_cache_miss_tokens",
		"cacheReadTokens", "cache_read_tokens",
		"readUnits", "read_units", "inputUnits", "input_units", "requestUnits", "request_units",
		"textInputTokens", "text_input_tokens", "imageInputTokens", "image_input_tokens", "imageTokens", "image_tokens", "videoTokens", "video_tokens", "audioInputTokens", "audio_input_tokens", "audioTokens", "audio_tokens",
		"candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage",
		"completionTokensDetailsReasoningTokens", "completion_tokens_details_reasoning_tokens",
		"reasoningTokens", "reasoning_tokens",
		"thoughtsTokenCount", "thoughts_token_count", "thoughtsTokens", "thoughts_tokens",
		"acceptedPredictionTokens", "accepted_prediction_tokens", "rejectedPredictionTokens", "rejected_prediction_tokens",
		"outputTokenDetailsReasoningTokens", "output_token_details_reasoning_tokens",
		"completionReasoningTokens", "completion_reasoning_tokens", "outputReasoningTokens", "output_reasoning_tokens",
		"writeUnits", "write_units", "outputUnits", "output_units", "responseUnits", "response_units",
		"totalTokenCount", "total_token_count", "billableTokens", "billable_tokens", "billedTokens", "billed_tokens", "usageTokens", "usage_tokens",
		"totalUnits", "total_units", "usageUnits", "usage_units", "searchUnits", "search_units", "classificationUnits", "classification_units", "classifications",
		"embeddingTokens", "embedding_tokens", "rerankTokens", "rerank_tokens",
		"queryUnits", "query_units", "searchRequests", "search_requests", "searchCredits", "search_credits", "serpapiSearches", "serpapi_searches", "braveSearchUnits", "brave_search_units",
		"browserMinutes", "browser_minutes", "browserSessions", "browser_sessions", "sessionMinutes", "session_minutes", "browserbaseMinutes", "browserbase_minutes", "pageLoads", "page_loads",
		"documentPages", "document_pages", "parsePages", "parse_pages", "llamaParsePages", "llama_parse_pages",
		"promptEvalCount", "prompt_eval_count", "evalCount", "eval_count",
		"inputTextTokens", "outputTextTokens",
		"inputImageTokens", "outputImageTokens",
		"inputAudioTokens", "outputAudioTokens",
		"textOutputTokens", "text_output_tokens", "imageOutputTokens", "image_output_tokens", "audioOutputTokens", "audio_output_tokens",
	}
	seen := map[string]struct{}{}
	for _, key := range tokenKeys {
		normalized := normalizeGatewayConditionKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := gatewayPositiveFloat(raw)
		if !ok {
			continue
		}
		gatewayAddNativeUsageNumber(out, key, number, hasGenericInput, hasGenericOutput)
	}
	if supplemental := gatewaySupplementalInputTokenUsage(values); supplemental > 0 {
		existing, _ := gatewayPositiveFloat(out["inputTokens"])
		out["inputTokens"] = existing + supplemental
	}
	if supplemental := gatewaySupplementalOutputTokenUsage(values); supplemental > 0 {
		existing, _ := gatewayPositiveFloat(out["outputTokens"])
		out["outputTokens"] = existing + supplemental
	}
	costKeys := []string{"responseCost", "response_cost", "totalCostUsd", "total_cost_usd", "totalCostUSD", "total_cost_USD", "estimatedCost", "estimated_cost", "estimatedCostUsd", "estimated_cost_usd", "billedAmount", "billed_amount", "chargeAmount", "charge_amount", "creditsUsed", "credits_used", "costMicros", "cost_micros", "totalCostMicros", "total_cost_micros", "estimatedCostMicros", "estimated_cost_micros", "costCents", "cost_cents", "totalCostCents", "total_cost_cents", "estimatedCostCents", "estimated_cost_cents", "inputCost", "input_cost", "promptCost", "prompt_cost", "inputCostUsd", "input_cost_usd", "promptCostUsd", "prompt_cost_usd", "inputCostMicros", "input_cost_micros", "promptCostMicros", "prompt_cost_micros", "inputCostCents", "input_cost_cents", "promptCostCents", "prompt_cost_cents", "outputCost", "output_cost", "completionCost", "completion_cost", "outputCostUsd", "output_cost_usd", "completionCostUsd", "completion_cost_usd", "outputCostMicros", "output_cost_micros", "completionCostMicros", "completion_cost_micros", "outputCostCents", "output_cost_cents", "completionCostCents", "completion_cost_cents"}
	clear(seen)
	for _, key := range costKeys {
		normalized := normalizeGatewayConditionKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := gatewayPositiveFloat(raw)
		if !ok {
			continue
		}
		gatewayAddNativeUsageNumber(out, key, number, false, false)
	}
	if len(out) == 0 {
		return nil
	}
	return gatewayNormalizeUsageSummary(out)
}

func gatewayNativeUsageHasAny(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := gatewayConditionRaw(values, key); ok {
			return true
		}
	}
	return false
}

func gatewayAddNativeUsageNumber(out map[string]any, key string, number float64, hasGenericInput, hasGenericOutput bool) {
	canonical := gatewayCanonicalUsageKey(key)
	if !gatewayCanonicalUsageSummaryKey(canonical) {
		return
	}
	number = gatewayNormalizeNativeUsageNumber(key, number)
	normalized := normalizeGatewayConditionKey(key)
	if gatewaySupplementalInputTokenKey(normalized) || gatewaySupplementalOutputTokenKey(normalized) {
		return
	}
	additiveInput := !hasGenericInput && (normalized == "inputtexttokens" || normalized == "inputimagetokens" || normalized == "inputaudiotokens" || normalized == "textinputtokens" || normalized == "imageinputtokens" || normalized == "audioinputtokens" || normalized == "imagetokens" || normalized == "videotokens" || normalized == "audiotokens")
	additiveOutput := !hasGenericOutput && (normalized == "outputtexttokens" || normalized == "outputimagetokens" || normalized == "outputaudiotokens" || normalized == "textoutputtokens" || normalized == "imageoutputtokens" || normalized == "audiooutputtokens")
	if additiveInput || additiveOutput {
		existing, _ := gatewayPositiveFloat(out[canonical])
		out[canonical] = existing + number
		return
	}
	if existing, ok := gatewayPositiveFloat(out[canonical]); !ok || number > existing {
		out[canonical] = number
	}
}

func gatewayNormalizeNativeUsageNumber(key string, number float64) float64 {
	switch normalizeGatewayConditionKey(key) {
	case "costmicros", "totalcostmicros", "estimatedcostmicros", "inputcostmicros", "promptcostmicros", "outputcostmicros", "completioncostmicros":
		return number / 1_000_000
	case "costcents", "totalcostcents", "estimatedcostcents", "inputcostcents", "promptcostcents", "outputcostcents", "completioncostcents":
		return number / 100
	default:
		return number
	}
}

func gatewayUsageWithDerivedTotals(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		canonical := gatewayCanonicalUsageKey(key)
		if number, ok := gatewayPositiveFloat(value); ok {
			number = gatewayNormalizeNativeUsageNumber(key, number)
			if existing, ok := gatewayPositiveFloat(out[canonical]); !ok || number > existing {
				out[canonical] = number
			}
			continue
		}
		if _, exists := out[canonical]; !exists {
			out[canonical] = value
		}
	}
	if _, ok := gatewayPositiveFloat(out["totalTokens"]); !ok {
		if total := gatewayPositiveFloatSum(out, "inputTokens", "outputTokens"); total > 0 {
			out["totalTokens"] = total
		}
	}
	if _, ok := gatewayPositiveFloat(out["totalCost"]); !ok {
		if total := gatewayPositiveFloatSum(out, "inputCost", "outputCost"); total > 0 {
			out["totalCost"] = total
		}
	}
	return out
}

func gatewaySupplementalInputTokenUsage(values map[string]any) float64 {
	total := 0.0
	for key, value := range values {
		if gatewaySupplementalInputTokenKey(normalizeGatewayConditionKey(key)) {
			if number, ok := gatewayPositiveFloat(value); ok {
				total += number
			}
		}
	}
	return total
}

func gatewaySupplementalOutputTokenUsage(values map[string]any) float64 {
	total := 0.0
	for key, value := range values {
		if gatewaySupplementalOutputTokenKey(normalizeGatewayConditionKey(key)) {
			if number, ok := gatewayPositiveFloat(value); ok {
				total += number
			}
		}
	}
	return total
}

func gatewaySupplementalInputTokenKey(normalized string) bool {
	switch normalized {
	case "cachedtokens", "cachetokens", "cachecreationinputtokens", "cachereadinputtokens", "cachewriteinputtokens", "cachecreationtokens", "cachereadtokens", "cachewritetokens", "cachedcontenttokencount", "cachedcontenttokens", "tooluseprompttokencount", "tooluseprompttokens", "promptcachereadtokens", "promptcachewritetokens", "promptcachehittokens", "promptcachemisstokens", "inputcachereadtokens", "inputcachewritetokens", "inputcachedtokens":
		return true
	default:
		return false
	}
}

func gatewaySupplementalOutputTokenKey(normalized string) bool {
	switch normalized {
	case "thoughtstokencount", "thoughtstokens", "acceptedpredictiontokens", "rejectedpredictiontokens":
		return true
	default:
		return false
	}
}

func gatewayNormalizeUsageSummary(values map[string]any) map[string]any {
	values = gatewayUsageWithDerivedTotals(values)
	out := map[string]any{}
	for _, key := range gatewayCanonicalUsageSummaryKeys() {
		if value, ok := gatewayPositiveFloat(values[key]); ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gatewayUsageSummaryKeys() []string {
	return []string{
		"totalTokens", "total_tokens", "tokens", "tokenCount", "totalTokenCount", "total_token_count", "tokenUsage", "token_usage", "billableTokens", "billable_tokens", "billedTokens", "billed_tokens", "usageTokens", "usage_tokens", "totalUnits", "total_units", "usageUnits", "usage_units", "searchUnits", "search_units", "classificationUnits", "classification_units", "classifications", "embeddingTokens", "embedding_tokens", "rerankTokens", "rerank_tokens", "queryUnits", "query_units", "queries", "searchRequests", "search_requests", "searchCredits", "search_credits", "serpapiSearches", "serpapi_searches", "braveSearchUnits", "brave_search_units", "browserMinutes", "browser_minutes", "browserSessions", "browser_sessions", "sessionMinutes", "session_minutes", "browserbaseMinutes", "browserbase_minutes", "pageLoads", "page_loads", "documentPages", "document_pages", "parsePages", "parse_pages", "llamaParsePages", "llama_parse_pages", "documents", "chunks", "characters", "chars", "requestCount", "request_count", "requests", "providerRequests", "provider_requests",
		"inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count", "cachedContentTokenCount", "cached_content_token_count", "cachedContentTokens", "cached_content_tokens", "toolUsePromptTokenCount", "tool_use_prompt_token_count", "toolUsePromptTokens", "tool_use_prompt_tokens", "inputTextTokens", "input_text_tokens", "textInputTokens", "text_input_tokens", "inputImageTokens", "input_image_tokens", "imageInputTokens", "image_input_tokens", "imageTokens", "image_tokens", "videoTokens", "video_tokens", "inputAudioTokens", "input_audio_tokens", "audioInputTokens", "audio_input_tokens", "audioTokens", "audio_tokens", "readUnits", "read_units", "inputUnits", "input_units", "requestUnits", "request_units", "promptCacheReadTokens", "prompt_cache_read_tokens", "promptCacheWriteTokens", "prompt_cache_write_tokens", "promptCacheHitTokens", "prompt_cache_hit_tokens", "promptCacheMissTokens", "prompt_cache_miss_tokens", "inputCacheReadTokens", "input_cache_read_tokens", "inputCacheWriteTokens", "input_cache_write_tokens", "inputCachedTokens", "input_cached_tokens",
		"outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count", "outputTextTokens", "output_text_tokens", "textOutputTokens", "text_output_tokens", "outputImageTokens", "output_image_tokens", "imageOutputTokens", "image_output_tokens", "outputAudioTokens", "output_audio_tokens", "audioOutputTokens", "audio_output_tokens", "thoughtsTokenCount", "thoughts_token_count", "thoughtsTokens", "thoughts_tokens", "reasoningTokens", "reasoning_tokens", "completionReasoningTokens", "completion_reasoning_tokens", "outputReasoningTokens", "output_reasoning_tokens", "acceptedPredictionTokens", "accepted_prediction_tokens", "rejectedPredictionTokens", "rejected_prediction_tokens", "writeUnits", "write_units", "outputUnits", "output_units", "responseUnits", "response_units",
		"totalCost", "total_cost", "cost", "costUsd", "costUSD", "usd", "estimatedCost", "estimated_cost", "estimatedCostUsd", "estimated_cost_usd", "responseCost", "response_cost", "totalCostUsd", "total_cost_usd", "totalCostUSD", "total_cost_USD", "billedAmount", "billed_amount", "chargeAmount", "charge_amount", "creditsUsed", "credits_used", "costMicros", "cost_micros", "totalCostMicros", "total_cost_micros", "estimatedCostMicros", "estimated_cost_micros", "costCents", "cost_cents", "totalCostCents", "total_cost_cents", "estimatedCostCents", "estimated_cost_cents",
		"inputCost", "input_cost", "promptCost", "prompt_cost", "inputCostUsd", "input_cost_usd", "promptCostUsd", "prompt_cost_usd", "inputCostMicros", "input_cost_micros", "promptCostMicros", "prompt_cost_micros", "inputCostCents", "input_cost_cents", "promptCostCents", "prompt_cost_cents",
		"outputCost", "output_cost", "completionCost", "completion_cost", "outputCostUsd", "output_cost_usd", "completionCostUsd", "completion_cost_usd", "outputCostMicros", "output_cost_micros", "completionCostMicros", "completion_cost_micros", "outputCostCents", "output_cost_cents", "completionCostCents", "completion_cost_cents",
	}
}

func gatewayCanonicalUsageSummaryKeys() []string {
	return []string{"totalTokens", "inputTokens", "outputTokens", "totalCost", "inputCost", "outputCost"}
}

func gatewayCanonicalUsageSummaryKey(key string) bool {
	switch key {
	case "totalTokens", "inputTokens", "outputTokens", "totalCost", "inputCost", "outputCost":
		return true
	default:
		return false
	}
}

func gatewayCanonicalUsageKey(key string) string {
	switch normalizeGatewayConditionKey(key) {
	case "totaltokens", "tokens", "tokencount", "totaltokencount", "tokenusage", "billabletokens", "billedtokens", "usagetokens", "totalunits", "usageunits", "searchunits", "classificationunits", "classifications", "embeddingtokens", "reranktokens", "queryunits", "queries", "searchrequests", "searchcredits", "serpapisearches", "bravesearchunits", "browserminutes", "browsersessions", "sessionminutes", "browserbaseminutes", "pageloads", "documentpages", "parsepages", "llamaparsepages", "documents", "chunks", "characters", "chars", "requestcount", "requests", "providerrequests":
		return "totalTokens"
	case "inputtokens", "inputtokenscount", "inputtokenusage", "prompttokens", "prompttokenscount", "prompttokenusage", "prompttokencount", "inputtokencount", "promptevalcount", "cachedcontenttokencount", "cachedcontenttokens", "tooluseprompttokencount", "tooluseprompttokens", "inputtexttokens", "textinputtokens", "inputimagetokens", "imageinputtokens", "imagetokens", "videotokens", "inputaudiotokens", "audioinputtokens", "audiotokens", "prompttokensdetailscachedtokens", "cachedtokens", "cachereadtokens", "promptcachehittokens", "promptcachemisstokens", "readunits", "inputunits", "requestunits":
		return "inputTokens"
	case "outputtokens", "outputtokenscount", "outputtokenusage", "completiontokens", "completiontokenscount", "completiontokenusage", "candidatestokencount", "outputtokencount", "evalcount", "outputtexttokens", "textoutputtokens", "outputimagetokens", "imageoutputtokens", "outputaudiotokens", "audiooutputtokens", "thoughtstokencount", "thoughtstokens", "completiontokensdetailsreasoningtokens", "completionreasoningtokens", "outputtokendetailsreasoningtokens", "outputreasoningtokens", "reasoningtokens", "acceptedpredictiontokens", "rejectedpredictiontokens", "writeunits", "outputunits", "responseunits":
		return "outputTokens"
	case "totalcost", "cost", "costusd", "usd", "estimatedcost", "estimatedcostusd", "responsecost", "totalcostusd", "billedamount", "chargeamount", "creditsused", "costmicros", "totalcostmicros", "estimatedcostmicros", "costcents", "totalcostcents", "estimatedcostcents":
		return "totalCost"
	case "inputcost", "promptcost", "inputcostusd", "promptcostusd", "inputcostmicros", "promptcostmicros", "inputcostcents", "promptcostcents":
		return "inputCost"
	case "outputcost", "completioncost", "outputcostusd", "completioncostusd", "outputcostmicros", "completioncostmicros", "outputcostcents", "completioncostcents":
		return "outputCost"
	default:
		return strings.TrimSpace(key)
	}
}

func formatGatewayBudgetValue(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', 4, 64)
}

func enforceGatewayRedactionPolicyCondition(policy domainaigateway.AccessPolicy, tool domainaigateway.ToolCapability, toolInput map[string]any) (map[string]any, gatewayRedactionAuditSummary, error) {
	values := gatewayConditionValues(policy.Conditions, "redactionPolicy", "redaction", "sensitiveDataRedaction")
	rules := gatewayRedactionRules(values, tool, "input")
	if len(rules) == 0 {
		return toolInput, gatewayRedactionAuditSummary{}, nil
	}
	out := toolInput
	var summary gatewayRedactionAuditSummary
	for _, rule := range rules {
		ruleSummary := gatewayRedactionAuditSummaryForValue(out, rule, "input")
		ruleSummary.PolicyIDs = append(ruleSummary.PolicyIDs, strings.TrimSpace(policy.ID))
		summary.merge(ruleSummary)
		if rule.DenySensitive {
			if ruleSummary.TotalMatches > 0 {
				return out, summary, fmt.Errorf("%w: AI Gateway redaction policy %s rejected sensitive input for %s", apperrors.ErrAccessDenied, strings.TrimSpace(policy.ID), tool.Name)
			}
			continue
		}
		if rule.SanitizeSensitive {
			out = applyGatewayRedactionRule(out, rule)
		}
	}
	return out, summary, nil
}

func enforceGatewayOutputRedactionPolicyCondition(policy domainaigateway.AccessPolicy, tool domainaigateway.ToolCapability, output any) (any, gatewayRedactionAuditSummary, error) {
	rules := gatewayOutputRedactionRules(policy.Conditions, tool)
	if len(rules) == 0 {
		return output, gatewayRedactionAuditSummary{}, nil
	}
	out := gatewayRedactionSerializableValue(output)
	var summary gatewayRedactionAuditSummary
	for _, rule := range rules {
		ruleSummary := gatewayRedactionAuditSummaryForValue(out, rule, "output")
		ruleSummary.PolicyIDs = append(ruleSummary.PolicyIDs, strings.TrimSpace(policy.ID))
		summary.merge(ruleSummary)
		if rule.DenySensitive {
			if ruleSummary.TotalMatches > 0 {
				return out, summary, fmt.Errorf("%w: AI Gateway output redaction policy %s rejected sensitive output for %s", apperrors.ErrAccessDenied, strings.TrimSpace(policy.ID), tool.Name)
			}
			continue
		}
		if rule.SanitizeSensitive {
			out = applyGatewayRedactionValue(out, rule, "")
		}
	}
	return out, summary, nil
}

func gatewayOutputRedactionRules(conditions map[string]any, tool domainaigateway.ToolCapability) []gatewayRedactionRule {
	out := make([]gatewayRedactionRule, 0)
	values := gatewayConditionValues(conditions, "redactionPolicy", "redaction", "sensitiveDataRedaction")
	out = append(out, gatewayRedactionRules(values, tool, "output")...)
	for _, alias := range []string{"outputRedactionPolicy", "outputRedaction", "responseRedactionPolicy", "responseRedaction"} {
		raw, ok := gatewayConditionRaw(conditions, alias)
		if !ok {
			continue
		}
		values := mapValue(raw)
		if len(values) == 0 {
			continue
		}
		if _, ok := gatewayConditionRaw(values, "target"); !ok {
			values["target"] = "output"
		}
		out = append(out, gatewayRedactionRules(values, tool, "output")...)
	}
	return out
}

type gatewayRedactionRule struct {
	Mode              string
	Target            string
	Fields            []string
	AllowFields       []string
	ValuePatterns     []string
	SecretTypes       []string
	Replacement       string
	PreserveFormat    bool
	DenySensitive     bool
	SanitizeSensitive bool
}

type gatewayRedactionAuditSummary struct {
	TotalMatches            int
	FieldMatches            int
	SensitiveKeyMatches     int
	SensitiveTextMatches    int
	ValuePatternMatches     int
	SecretClassifierMatches int
	StructuredSecretMatches int
	Targets                 []string
	FieldPaths              []string
	MatchTypes              []string
	Classifiers             []string
	PolicyIDs               []string
}

func (summary gatewayRedactionAuditSummary) empty() bool {
	return summary.TotalMatches == 0
}

func (summary *gatewayRedactionAuditSummary) merge(other gatewayRedactionAuditSummary) {
	if summary == nil || other.empty() {
		return
	}
	summary.TotalMatches += other.TotalMatches
	summary.FieldMatches += other.FieldMatches
	summary.SensitiveKeyMatches += other.SensitiveKeyMatches
	summary.SensitiveTextMatches += other.SensitiveTextMatches
	summary.ValuePatternMatches += other.ValuePatternMatches
	summary.SecretClassifierMatches += other.SecretClassifierMatches
	summary.StructuredSecretMatches += other.StructuredSecretMatches
	summary.Targets = gatewayAppendUniqueStrings(summary.Targets, other.Targets...)
	summary.FieldPaths = gatewayAppendUniqueStrings(summary.FieldPaths, other.FieldPaths...)
	summary.MatchTypes = gatewayAppendUniqueStrings(summary.MatchTypes, other.MatchTypes...)
	summary.Classifiers = gatewayAppendUniqueStrings(summary.Classifiers, other.Classifiers...)
	summary.PolicyIDs = gatewayAppendUniqueStrings(summary.PolicyIDs, other.PolicyIDs...)
}

func (summary *gatewayRedactionAuditSummary) add(target, fieldPath, matchType, classifier string) {
	if summary == nil {
		return
	}
	target = normalizeGatewayRedactionTarget(target)
	if target == "" {
		target = "input"
	}
	matchType = strings.TrimSpace(matchType)
	if matchType == "" {
		return
	}
	summary.TotalMatches++
	switch matchType {
	case "field":
		summary.FieldMatches++
	case "sensitive_key":
		summary.SensitiveKeyMatches++
	case "sensitive_text":
		summary.SensitiveTextMatches++
	case "value_pattern":
		summary.ValuePatternMatches++
	case "secret_classifier":
		summary.SecretClassifierMatches++
	case "structured_secret":
		summary.StructuredSecretMatches++
	}
	summary.Targets = gatewayAppendUniqueStrings(summary.Targets, target)
	summary.MatchTypes = gatewayAppendUniqueStrings(summary.MatchTypes, matchType)
	if fieldPath = gatewayAuditFieldPath(fieldPath); fieldPath != "" {
		summary.FieldPaths = gatewayAppendUniqueStrings(summary.FieldPaths, fieldPath)
	}
	if classifier = strings.TrimSpace(classifier); classifier != "" {
		summary.Classifiers = gatewayAppendUniqueStrings(summary.Classifiers, classifier)
	}
}

func (summary gatewayRedactionAuditSummary) toMap() map[string]any {
	if summary.empty() {
		return nil
	}
	return map[string]any{
		"totalMatches":            summary.TotalMatches,
		"fieldMatches":            summary.FieldMatches,
		"sensitiveKeyMatches":     summary.SensitiveKeyMatches,
		"sensitiveTextMatches":    summary.SensitiveTextMatches,
		"valuePatternMatches":     summary.ValuePatternMatches,
		"secretClassifierMatches": summary.SecretClassifierMatches,
		"structuredSecretMatches": summary.StructuredSecretMatches,
		"targets":                 gatewayLimitedSortedStrings(summary.Targets, 12),
		"fieldPaths":              gatewayLimitedSortedStrings(summary.FieldPaths, 24),
		"matchTypes":              gatewayLimitedSortedStrings(summary.MatchTypes, 12),
		"classifiers":             gatewayLimitedSortedStrings(summary.Classifiers, 24),
		"policyIds":               gatewayLimitedSortedStrings(summary.PolicyIDs, 24),
	}
}

func gatewayRedactionRules(values map[string]any, tool domainaigateway.ToolCapability, target string) []gatewayRedactionRule {
	target = normalizeGatewayRedactionTarget(target)
	base := gatewayBuildRedactionRule(values, gatewayRedactionRule{Target: "input"})
	out := make([]gatewayRedactionRule, 0, 1)
	if gatewayRedactionRuleConfigured(base) && gatewayRedactionRuleTargetMatches(base, target) {
		out = append(out, base)
	}
	for _, rawRule := range gatewayRedactionRuleMaps(values) {
		if !gatewayRedactionRuleAppliesToTool(rawRule, tool) {
			continue
		}
		rule := gatewayBuildRedactionRule(rawRule, base)
		if gatewayRedactionRuleConfigured(rule) && gatewayRedactionRuleTargetMatches(rule, target) {
			out = append(out, rule)
		}
	}
	return out
}

func gatewayBuildRedactionRule(values map[string]any, fallback gatewayRedactionRule) gatewayRedactionRule {
	rule := fallback
	if rule.Replacement == "" {
		rule.Replacement = "[REDACTED]"
	}
	if target := normalizeGatewayRedactionTarget(gatewayFirstString(values, "target", "appliesTo", "direction")); target != "" {
		rule.Target = target
	}
	if mode := normalizeGatewayRedactionMode(gatewayFirstString(values, "mode", "strategy", "redactionMode", "action")); mode != "" {
		rule.Mode = mode
	}
	if fields := gatewayConditionStringList(values, "fields", "field", "paths", "path", "redactFields", "maskFields", "sensitiveFields"); len(fields) > 0 {
		rule.Fields = fields
	}
	if fields := gatewayConditionStringList(values, "allowFields", "allowedFields", "allowlist", "fieldAllowList", "fieldAllowlist"); len(fields) > 0 {
		rule.AllowFields = fields
	}
	if patterns := gatewayConditionStringList(values, "valuePatterns", "valuePattern", "valueRegex", "valueRegexes", "regex", "regexes", "matchValues", "matchPatterns"); len(patterns) > 0 {
		rule.ValuePatterns = patterns
	}
	if secretTypes := gatewayConditionStringList(values, "secretTypes", "secretType", "classifiers", "classifier", "detect", "detectSecretTypes", "secretClassifiers"); len(secretTypes) > 0 {
		rule.SecretTypes = secretTypes
	}
	if gatewayFirstBool(values, "detectSecrets", "classifySecrets", "structuredSecrets") {
		rule.SecretTypes = append(rule.SecretTypes, "default")
	}
	if replacement := gatewayFirstString(values, "replacement", "replacementText", "redactionValue", "maskValue"); replacement != "" {
		rule.Replacement = replacement
	}
	if gatewayFirstBool(values, "preserveFormat", "formatPreserving", "preserveShape") {
		rule.PreserveFormat = true
	}
	if gatewayFirstBool(values, "denySensitiveInput", "blockSensitiveInput", "rejectSensitiveInput") {
		rule.DenySensitive = true
		rule.SanitizeSensitive = false
	}
	if gatewayFirstBool(values, "sanitizeInput", "maskSensitiveInput", "redactInput") {
		rule.SanitizeSensitive = true
	}
	switch rule.Mode {
	case "strict", "deny_sensitive", "block_sensitive", "reject_sensitive", "deny", "block":
		rule.DenySensitive = true
		rule.SanitizeSensitive = false
	case "sanitize", "sanitise", "mask", "redact", "redacted", "sanitize_input", "mask_input", "redact_input":
		rule.SanitizeSensitive = true
	}
	if rule.Mode == "" && (len(rule.Fields) > 0 || len(rule.AllowFields) > 0 || len(rule.ValuePatterns) > 0 || len(rule.SecretTypes) > 0) {
		rule.SanitizeSensitive = true
	}
	return rule
}

func normalizeGatewayRedactionTarget(target string) string {
	target = strings.ToLower(strings.TrimSpace(target))
	target = strings.ReplaceAll(target, "-", "_")
	target = strings.ReplaceAll(target, " ", "_")
	switch target {
	case "", "default":
		return ""
	case "input", "request", "tool_input", "before_invoke", "pre_invoke":
		return "input"
	case "output", "response", "tool_output", "after_invoke", "post_invoke", "result":
		return "output"
	case "both", "input_output", "request_response", "all":
		return "both"
	default:
		return target
	}
}

func gatewayRedactionRuleTargetMatches(rule gatewayRedactionRule, target string) bool {
	target = normalizeGatewayRedactionTarget(target)
	ruleTarget := normalizeGatewayRedactionTarget(rule.Target)
	if ruleTarget == "" {
		ruleTarget = "input"
	}
	if target == "" {
		target = "input"
	}
	return ruleTarget == "both" || ruleTarget == target
}

func gatewayRedactionRuleConfigured(rule gatewayRedactionRule) bool {
	return rule.DenySensitive || rule.SanitizeSensitive
}

func normalizeGatewayRedactionMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	mode = strings.ReplaceAll(mode, "-", "_")
	mode = strings.ReplaceAll(mode, " ", "_")
	return mode
}

func gatewayRedactionRuleMaps(values map[string]any) []map[string]any {
	rawValues := make([]any, 0, 4)
	for _, key := range []string{"rules", "fieldRules", "toolRules", "redactionRules"} {
		if raw, ok := gatewayConditionRaw(values, key); ok {
			rawValues = append(rawValues, raw)
		}
	}
	out := make([]map[string]any, 0)
	for _, raw := range rawValues {
		switch typed := raw.(type) {
		case []map[string]any:
			out = append(out, typed...)
		case []any:
			for _, item := range typed {
				if mapped := mapValue(item); len(mapped) > 0 {
					out = append(out, mapped)
				}
			}
		case map[string]any:
			out = append(out, typed)
		}
	}
	return out
}

func gatewayRedactionRuleAppliesToTool(rule map[string]any, tool domainaigateway.ToolCapability) bool {
	patterns := gatewayConditionStringList(rule, "tool", "tools", "toolName", "toolNames", "toolPattern", "toolPatterns")
	if len(patterns) == 0 {
		return true
	}
	return matchesToolPatternList(patterns, tool.Name)
}

func gatewayRedactionRuleMatchesSensitive(value any, rule gatewayRedactionRule) bool {
	return gatewayRedactionValueContainsSensitiveData(value, rule, "")
}

func gatewayRedactionAuditSummaryForValue(value any, rule gatewayRedactionRule, target string) gatewayRedactionAuditSummary {
	var summary gatewayRedactionAuditSummary
	gatewayCollectRedactionAudit(value, rule, target, "", &summary)
	return summary
}

func gatewayCollectRedactionAudit(value any, rule gatewayRedactionRule, target, path string, summary *gatewayRedactionAuditSummary) {
	if summary == nil || gatewayRedactionPathAllowed(path, rule.AllowFields) {
		return
	}
	for _, classifier := range gatewayStructuredSecretClassifiers(value, rule) {
		summary.add(target, path, "structured_secret", classifier)
	}
	switch typed := value.(type) {
	case nil:
		return
	case map[string]any:
		for key, item := range typed {
			nextPath := gatewayJoinFieldPath(path, key)
			if gatewayRedactionPathAllowed(nextPath, rule.AllowFields) {
				continue
			}
			if gatewayRedactionFieldMatches(nextPath, key, rule.Fields) {
				summary.add(target, nextPath, "field", "")
			}
			if gatewaySensitiveKey(key) {
				summary.add(target, nextPath, "sensitive_key", "")
			}
			gatewayCollectRedactionAudit(item, rule, target, nextPath, summary)
		}
	case []any:
		for index, item := range typed {
			gatewayCollectRedactionAudit(item, rule, target, gatewayJoinFieldPath(path, strconv.Itoa(index)), summary)
		}
	case []map[string]any:
		for index, item := range typed {
			gatewayCollectRedactionAudit(item, rule, target, gatewayJoinFieldPath(path, strconv.Itoa(index)), summary)
		}
	case []string:
		for index, item := range typed {
			gatewayCollectRedactionStringAudit(item, rule, target, gatewayJoinFieldPath(path, strconv.Itoa(index)), summary)
		}
	case string:
		gatewayCollectRedactionStringAudit(typed, rule, target, path, summary)
	}
}

func gatewayCollectRedactionStringAudit(value string, rule gatewayRedactionRule, target, path string, summary *gatewayRedactionAuditSummary) {
	if summary == nil || strings.TrimSpace(value) == "" {
		return
	}
	if gatewaySensitiveValuePattern.MatchString(value) {
		summary.add(target, path, "sensitive_text", "")
	}
	for _, pattern := range gatewayCompiledRedactionPatterns(rule.ValuePatterns) {
		if pattern.MatchString(value) {
			summary.add(target, path, "value_pattern", gatewayRegexSummary(pattern))
		}
	}
	for _, classifier := range gatewaySecretClassifierMatches(value, rule.SecretTypes) {
		summary.add(target, path, "secret_classifier", classifier)
	}
}

func gatewayRedactionValueContainsSensitiveData(value any, rule gatewayRedactionRule, path string) bool {
	if gatewayRedactionPathAllowed(path, rule.AllowFields) {
		return false
	}
	if len(gatewayStructuredSecretClassifiers(value, rule)) > 0 {
		return true
	}
	switch typed := value.(type) {
	case nil:
		return false
	case map[string]any:
		for key, item := range typed {
			nextPath := gatewayJoinFieldPath(path, key)
			if gatewayRedactionPathAllowed(nextPath, rule.AllowFields) {
				continue
			}
			if gatewayRedactionFieldMatches(nextPath, key, rule.Fields) || gatewaySensitiveKey(key) {
				return true
			}
			if gatewayRedactionValueContainsSensitiveData(item, rule, nextPath) {
				return true
			}
		}
		return false
	case []any:
		for index, item := range typed {
			if gatewayRedactionValueContainsSensitiveData(item, rule, gatewayJoinFieldPath(path, strconv.Itoa(index))) {
				return true
			}
		}
		return false
	case []map[string]any:
		for index, item := range typed {
			if gatewayRedactionValueContainsSensitiveData(item, rule, gatewayJoinFieldPath(path, strconv.Itoa(index))) {
				return true
			}
		}
		return false
	case string:
		return gatewayRedactionStringMatches(typed, rule)
	default:
		return false
	}
}

func applyGatewayRedactionRule(values map[string]any, rule gatewayRedactionRule) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	out, ok := applyGatewayRedactionValue(values, rule, "").(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return out
}

func gatewayRedactionSerializableValue(value any) any {
	switch value.(type) {
	case nil, map[string]any, []any, []map[string]any, []string, string:
		return value
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return value
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return value
		}
		return out
	}
}

func applyGatewayRedactionValue(value any, rule gatewayRedactionRule, path string) any {
	if gatewayRedactionPathAllowed(path, rule.AllowFields) {
		return cloneGatewayValue(value)
	}
	if len(gatewayStructuredSecretClassifiers(value, rule)) > 0 {
		return gatewayRedactionReplacementForValue(value, rule)
	}
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			nextPath := gatewayJoinFieldPath(path, key)
			if gatewayRedactionPathAllowed(nextPath, rule.AllowFields) {
				out[key] = cloneGatewayValue(item)
				continue
			}
			if gatewayRedactionFieldMatches(nextPath, key, rule.Fields) || gatewaySensitiveKey(key) {
				out[key] = gatewayRedactionReplacementForValue(item, rule)
				continue
			}
			out[key] = applyGatewayRedactionValue(item, rule, nextPath)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = applyGatewayRedactionValue(item, rule, gatewayJoinFieldPath(path, strconv.Itoa(index)))
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for index, item := range typed {
			value := applyGatewayRedactionValue(item, rule, gatewayJoinFieldPath(path, strconv.Itoa(index)))
			if mapped, ok := value.(map[string]any); ok {
				out[index] = mapped
			} else {
				out[index] = map[string]any{}
			}
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for index, item := range typed {
			out[index] = gatewayRedactSensitiveTextWithRule(item, rule)
		}
		return out
	case string:
		return gatewayRedactSensitiveTextWithRule(typed, rule)
	default:
		return typed
	}
}

func gatewayRedactionReplacementForValue(value any, rule gatewayRedactionRule) any {
	switch typed := value.(type) {
	case map[string]any, []any, []map[string]any:
		return applyGatewayRedactionValue(value, gatewayRedactionRule{
			Fields:            []string{"*"},
			Replacement:       gatewayRedactionReplacement(rule),
			PreserveFormat:    rule.PreserveFormat,
			SanitizeSensitive: true,
		}, "")
	case []string:
		out := make([]string, len(typed))
		for index, item := range typed {
			out[index] = gatewayMaskString(item, rule)
		}
		return out
	case string:
		return gatewayMaskString(typed, rule)
	default:
		return gatewayRedactionReplacement(rule)
	}
}

func gatewayRedactionStringMatches(value string, rule gatewayRedactionRule) bool {
	if gatewaySensitiveValuePattern.MatchString(value) {
		return true
	}
	for _, pattern := range gatewayCompiledRedactionPatterns(rule.ValuePatterns) {
		if pattern.MatchString(value) {
			return true
		}
	}
	for _, pattern := range gatewaySecretClassifierPatterns(rule.SecretTypes) {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func gatewayStructuredSecretClassifiers(value any, rule gatewayRedactionRule) []string {
	if len(rule.SecretTypes) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
	switch typed := value.(type) {
	case map[string]any:
		if gatewaySecretTypeEnabled(rule.SecretTypes, "kubernetes_secret", "k8s_secret", "kubernetes", "k8s") && gatewayKubernetesSecretMap(typed) {
			out = gatewayAppendUniqueStrings(out, "kubernetes_secret")
		}
		if gatewaySecretTypeEnabled(rule.SecretTypes, "kubeconfig", "kubernetes_config", "k8sconfig", "k8s_config") && gatewayKubeconfigMap(typed) {
			out = gatewayAppendUniqueStrings(out, "kubeconfig")
		}
		if gatewaySecretTypeEnabled(rule.SecretTypes, "docker_config", "dockerconfig", "docker_auth", "docker") && gatewayDockerConfigMap(typed) {
			out = gatewayAppendUniqueStrings(out, "docker_config")
		}
		if gatewaySecretTypeEnabled(rule.SecretTypes, "gcp_service_account", "google_service_account", "service_account_json", "google") && gatewayGCPServiceAccountMap(typed) {
			out = gatewayAppendUniqueStrings(out, "gcp_service_account")
		}
		if gatewaySecretTypeEnabled(rule.SecretTypes, "aws", "aws_credentials", "awscredential") && gatewayAWSCredentialsMap(typed) {
			out = gatewayAppendUniqueStrings(out, "aws")
		}
	case []any:
		for _, item := range typed {
			out = gatewayAppendUniqueStrings(out, gatewayStructuredSecretClassifiers(item, rule)...)
		}
	case []map[string]any:
		for _, item := range typed {
			out = gatewayAppendUniqueStrings(out, gatewayStructuredSecretClassifiers(item, rule)...)
		}
	}
	return out
}

func gatewayKubernetesSecretMap(values map[string]any) bool {
	if gatewayStringSliceContainsAny(gatewayStringList(values["kind"]), "secret", "kubernetes_secret", "kubernetes") {
		return true
	}
	if gatewayStringSliceContainsAny(gatewayStringList(values["resourceKind"]), "Secret", "secret") {
		return true
	}
	if _, ok := values["data"]; ok && gatewayStringSliceContainsAny(gatewayStringList(values["apiVersion"]), "v1") && gatewayStringSliceContainsAny(gatewayStringList(values["kind"]), "Secret") {
		return true
	}
	if _, ok := values["stringData"]; ok && gatewayStringSliceContainsAny(gatewayStringList(values["kind"]), "Secret") {
		return true
	}
	return false
}

func gatewayKubeconfigMap(values map[string]any) bool {
	_, hasClusters := gatewayConditionRaw(values, "clusters")
	_, hasContexts := gatewayConditionRaw(values, "contexts")
	_, hasUsers := gatewayConditionRaw(values, "users")
	_, hasCurrentContext := gatewayConditionRaw(values, "current-context")
	if !hasCurrentContext {
		_, hasCurrentContext = gatewayConditionRaw(values, "currentContext")
	}
	return hasClusters && hasContexts && hasUsers && hasCurrentContext
}

func gatewayDockerConfigMap(values map[string]any) bool {
	if raw, ok := gatewayConditionRaw(values, "auths"); ok && len(mapValue(raw)) > 0 {
		return true
	}
	if raw, ok := gatewayConditionRaw(values, "credHelpers"); ok && len(mapValue(raw)) > 0 {
		return true
	}
	if text := gatewayFirstString(values, "credsStore", "credStore"); text != "" {
		return true
	}
	return false
}

func gatewayGCPServiceAccountMap(values map[string]any) bool {
	if !gatewayStringSliceContainsAny(gatewayStringList(values["type"]), "service_account") {
		return false
	}
	_, hasPrivateKey := gatewayConditionRaw(values, "private_key")
	_, hasClientEmail := gatewayConditionRaw(values, "client_email")
	return hasPrivateKey && hasClientEmail
}

func gatewayAWSCredentialsMap(values map[string]any) bool {
	_, hasAccessKey := gatewayConditionRaw(values, "aws_access_key_id")
	if !hasAccessKey {
		_, hasAccessKey = gatewayConditionRaw(values, "accessKeyId")
	}
	_, hasSecretKey := gatewayConditionRaw(values, "aws_secret_access_key")
	if !hasSecretKey {
		_, hasSecretKey = gatewayConditionRaw(values, "secretAccessKey")
	}
	return hasAccessKey && hasSecretKey
}

func gatewaySecretTypeEnabled(secretTypes []string, aliases ...string) bool {
	aliasSet := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		aliasSet[normalizeGatewayConditionKey(alias)] = struct{}{}
	}
	for _, secretType := range secretTypes {
		switch normalizeGatewayConditionKey(secretType) {
		case "default", "all", "builtin", "builtins", "secret", "secrets":
			return true
		default:
			if _, ok := aliasSet[normalizeGatewayConditionKey(secretType)]; ok {
				return true
			}
		}
	}
	return false
}

func gatewayCompiledRedactionPatterns(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		out = append(out, compiled)
	}
	return out
}

func gatewaySecretClassifierPatterns(secretTypes []string) []*regexp.Regexp {
	patterns := gatewaySecretClassifierPatternSpecs(secretTypes)
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		out = append(out, pattern.Pattern)
	}
	return out
}

type gatewaySecretClassifierPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

func gatewaySecretClassifierPatternSpecs(secretTypes []string) []gatewaySecretClassifierPattern {
	out := make([]gatewaySecretClassifierPattern, 0, len(secretTypes)+4)
	seen := map[string]struct{}{}
	add := func(key, pattern string) {
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, gatewaySecretClassifierPattern{Name: key, Pattern: regexp.MustCompile(pattern)})
	}
	for _, secretType := range secretTypes {
		switch normalizeGatewayConditionKey(secretType) {
		case "default", "all", "builtin", "builtins", "secret", "secrets", "token", "tokens":
			add("token", `(?i)(?:bearer\s+)?(?:ghp|github_pat|glpat|sk|xox[baprs])[-_A-Za-z0-9]{12,}`)
			add("jwt", `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)
			add("aws", `AKIA[0-9A-Z]{16}`)
			add("private_key", `-----BEGIN [A-Z ]*PRIVATE KEY-----`)
			add("anthropic", `(?i)sk-ant-[A-Za-z0-9_-]{20,}`)
			add("google_api_key", `AIza[0-9A-Za-z_-]{30,}`)
			add("huggingface", `(?i)hf_[A-Za-z0-9]{30,}`)
			add("cohere", `(?i)\bcohere[_-]?(?:api[_-]?)?key[_-]?[A-Za-z0-9]{20,}\b`)
			add("mistral", `(?i)\bmistral[_-]?[A-Za-z0-9]{20,}\b`)
			add("deepseek", `(?i)\bsk-(?:deepseek|ds)-[A-Za-z0-9_-]{20,}\b`)
			add("groq", `(?i)\bgsk_[A-Za-z0-9]{20,}\b`)
			add("together", `(?i)\btgp_v1_[A-Za-z0-9_-]{20,}\b`)
			add("replicate", `(?i)\br8_[A-Za-z0-9]{20,}\b`)
			add("langsmith", `(?i)\bls[v]2?_[A-Za-z0-9_-]{20,}\b`)
			add("pinecone", `(?i)\bpcsk_[A-Za-z0-9_-]{20,}\b`)
			add("xai", `(?i)\bxai-[A-Za-z0-9_-]{20,}\b`)
			add("perplexity", `(?i)\bpplx-[A-Za-z0-9_-]{20,}\b`)
			add("tavily", `(?i)\btvly-[A-Za-z0-9_-]{20,}\b`)
			add("langfuse", `(?i)\b(?:pk|sk)-lf-[A-Za-z0-9_-]{20,}\b`)
			add("qdrant", `(?i)\bqdrant[_-][A-Za-z0-9_-]{20,}\b`)
			add("wandb", `(?i)\bwandb_[A-Za-z0-9]{20,}\b`)
			add("linear", `(?i)\blin_api_[A-Za-z0-9]{20,}\b`)
			add("openrouter", `(?i)\bsk-or-v1-[A-Za-z0-9_-]{20,}\b`)
			add("fireworks", `(?i)\bfw_[A-Za-z0-9_-]{20,}\b`)
			add("voyage", `(?i)\bpa-[A-Za-z0-9_-]{20,}\b`)
			add("brave_search", `(?i)\bBSA[A-Za-z0-9_-]{20,}\b`)
			add("serpapi", `(?i)\bserpapi[_-]?[A-Za-z0-9]{20,}\b`)
			add("browserbase", `(?i)\bbb_[A-Za-z0-9_-]{20,}\b`)
			add("exa", `(?i)\bexa_[A-Za-z0-9_-]{20,}\b`)
			add("jina", `(?i)\bjina_[A-Za-z0-9_-]{20,}\b`)
			add("unstructured", `(?i)\bunstructured[_-]?[A-Za-z0-9_-]{20,}\b`)
			add("llama_cloud", `(?i)\bllx-[A-Za-z0-9_-]{20,}\b`)
			add("helicone", `(?i)\bsk-helicone-[A-Za-z0-9_-]{20,}\b`)
			add("dashscope", `(?i)\b(?:dashscope|dash_scope|aliyun[_-]?bailian|bailian|tongyi|qwen)[\s:=_-]+sk-[A-Za-z0-9]{24,}\b`)
			add("moonshot", `(?i)\b(?:moonshot|kimi)[\s:=_-]+sk-[A-Za-z0-9]{32,}\b`)
			add("zhipu", `(?i)\b(?:zhipu|zhipuai|glm)[\s:=_-]+[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{24,}\b`)
			add("siliconflow", `(?i)\bsilicon[_-]?flow[\s:=_-]+sk-[A-Za-z0-9]{32,}\b`)
			add("hunyuan", `(?i)\bAKID[A-Za-z0-9]{16,}\b`)
			add("qianfan", `(?i)\bbce-v3/[A-Za-z0-9._~+/=-]{24,}\b`)
			add("volcengine", `(?i)\b(?:aklt|volc)[A-Za-z0-9_-]{20,}\b`)
			add("grafana", `(?i)\bgl(?:sa|c)_[A-Za-z0-9_=-]{20,}\b`)
			add("sentry", `(?i)\bsntrys_[A-Za-z0-9_=-]{20,}\b`)
			add("newrelic", `(?i)\bNRAK[-_A-Za-z0-9]{20,}\b`)
			add("azure_openai", `(?i)\b(?:azure[_-]?(?:openai|ai)?[_-]?(?:api[_-]?)?key|AZURE_OPENAI_API_KEY|OCP_APIM_SUBSCRIPTION_KEY)[\s:=_-]+[A-Za-z0-9]{32,}\b`)
			add("azure_devops", `(?i)\b[A-Za-z0-9]{76}AZDO[A-Za-z0-9]{4}\b`)
			add("datadog", `(?i)\b(?:datadog|dd)[_-]?(?:api|app)?[_-]?key[_-]?[A-Fa-f0-9]{32,40}\b`)
			add("pagerduty", `(?i)\bpd(?:us|at)\+[A-Za-z0-9._~-]{20,}\b`)
			add("posthog", `(?i)\bph[cp]_[A-Za-z0-9_-]{20,}\b`)
			add("splunk", `(?i)\bSplunk\s+[A-Za-z0-9+/=_-]{20,}\b`)
			add("elastic", `(?i)\bApiKey\s+[A-Za-z0-9+/=_-]{20,}\b`)
			add("terraform", `(?i)\batlasv1\.[A-Za-z0-9_-]{20,}\b`)
			add("npm", `(?i)npm_[A-Za-z0-9]{36,}`)
			add("stripe", `(?i)(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}`)
			add("docker_config", `(?is)"auths"\s*:\s*\{.*"auth"\s*:`)
			add("kubeconfig", `(?is)\bclusters\s*:.*\busers\s*:.*\bcurrent-context\s*:`)
		case "github", "githubtoken":
			add("github", `(?i)(?:ghp|github_pat)_[A-Za-z0-9_]{20,}`)
		case "gitlab", "gitlabtoken":
			add("gitlab", `(?i)glpat-[A-Za-z0-9_-]{20,}`)
		case "openai", "openaikey":
			add("openai", `(?i)sk-[A-Za-z0-9_-]{20,}`)
		case "anthropic", "anthropickey":
			add("anthropic", `(?i)sk-ant-[A-Za-z0-9_-]{20,}`)
		case "slack", "slacktoken":
			add("slack", `(?i)xox[baprs]-[A-Za-z0-9-]{20,}`)
		case "google", "googleapikey", "gcpapikey":
			add("google_api_key", `AIza[0-9A-Za-z_-]{30,}`)
		case "huggingface", "huggingfacetoken":
			add("huggingface", `(?i)hf_[A-Za-z0-9]{30,}`)
		case "cohere", "coherekey", "coheretoken":
			add("cohere", `(?i)\bcohere[_-]?(?:api[_-]?)?key[_-]?[A-Za-z0-9]{20,}\b`)
		case "mistral", "mistralkey", "mistraltoken":
			add("mistral", `(?i)\bmistral[_-]?[A-Za-z0-9]{20,}\b`)
		case "deepseek", "deepseekkey", "deepseektoken":
			add("deepseek", `(?i)\bsk-(?:deepseek|ds)-[A-Za-z0-9_-]{20,}\b`)
		case "groq", "groqkey", "groqtoken":
			add("groq", `(?i)\bgsk_[A-Za-z0-9]{20,}\b`)
		case "together", "togetherkey", "togethertoken", "togetherai":
			add("together", `(?i)\btgp_v1_[A-Za-z0-9_-]{20,}\b`)
		case "replicate", "replicatekey", "replicatetoken":
			add("replicate", `(?i)\br8_[A-Za-z0-9]{20,}\b`)
		case "langsmith", "langchain", "langsmithkey", "langsmithtoken":
			add("langsmith", `(?i)\bls[v]2?_[A-Za-z0-9_-]{20,}\b`)
		case "pinecone", "pineconekey", "pineconetoken":
			add("pinecone", `(?i)\bpcsk_[A-Za-z0-9_-]{20,}\b`)
		case "xai", "xaikey", "xaitoken", "grok", "grokkey", "groktoken":
			add("xai", `(?i)\bxai-[A-Za-z0-9_-]{20,}\b`)
		case "perplexity", "perplexitykey", "perplexitytoken", "pplx":
			add("perplexity", `(?i)\bpplx-[A-Za-z0-9_-]{20,}\b`)
		case "tavily", "tavilykey", "tavilytoken":
			add("tavily", `(?i)\btvly-[A-Za-z0-9_-]{20,}\b`)
		case "langfuse", "langfusekey", "langfusetoken":
			add("langfuse", `(?i)\b(?:pk|sk)-lf-[A-Za-z0-9_-]{20,}\b`)
		case "qdrant", "qdrantkey", "qdranttoken":
			add("qdrant", `(?i)\bqdrant[_-][A-Za-z0-9_-]{20,}\b`)
		case "wandb", "weightsandbiases", "wandbkey", "wandbtoken":
			add("wandb", `(?i)\bwandb_[A-Za-z0-9]{20,}\b`)
		case "linear", "linearkey", "lineartoken":
			add("linear", `(?i)\blin_api_[A-Za-z0-9]{20,}\b`)
		case "openrouter", "openrouterkey", "openroutertoken":
			add("openrouter", `(?i)\bsk-or-v1-[A-Za-z0-9_-]{20,}\b`)
		case "fireworks", "fireworksai", "fireworkskey", "fireworkstoken":
			add("fireworks", `(?i)\bfw_[A-Za-z0-9_-]{20,}\b`)
		case "voyage", "voyageai", "voyagekey", "voyagetoken":
			add("voyage", `(?i)\bpa-[A-Za-z0-9_-]{20,}\b`)
		case "bravesearch", "brave", "bravesearchkey", "bravesearchtoken":
			add("brave_search", `(?i)\bBSA[A-Za-z0-9_-]{20,}\b`)
		case "serpapi", "serp", "serpapikey", "serpapitoken":
			add("serpapi", `(?i)\bserpapi[_-]?[A-Za-z0-9]{20,}\b`)
		case "browserbase", "browserbasekey", "browserbasetoken":
			add("browserbase", `(?i)\bbb_[A-Za-z0-9_-]{20,}\b`)
		case "exa", "exasearch", "exakey", "exatoken":
			add("exa", `(?i)\bexa_[A-Za-z0-9_-]{20,}\b`)
		case "jina", "jinaai", "jinakey", "jinatoken":
			add("jina", `(?i)\bjina_[A-Za-z0-9_-]{20,}\b`)
		case "unstructured", "unstructuredio", "unstructuredkey", "unstructuredtoken":
			add("unstructured", `(?i)\bunstructured[_-]?[A-Za-z0-9_-]{20,}\b`)
		case "llamacloud", "llamaindex", "llamaparse", "llamacloudkey", "llamacloudtoken":
			add("llama_cloud", `(?i)\bllx-[A-Za-z0-9_-]{20,}\b`)
		case "helicone", "heliconekey", "heliconetoken":
			add("helicone", `(?i)\bsk-helicone-[A-Za-z0-9_-]{20,}\b`)
		case "dashscope", "dashscopekey", "dashscopetoken", "aliyunbailian", "bailian", "tongyi", "qwen":
			add("dashscope", `(?i)\bsk-[A-Za-z0-9]{24,}\b`)
		case "moonshot", "moonshotkey", "moonshottoken", "kimi":
			add("moonshot", `(?i)\bsk-[A-Za-z0-9]{32,}\b`)
		case "zhipu", "zhipuai", "zhipukey", "zhiputoken", "glm":
			add("zhipu", `(?i)\b[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{24,}\b`)
		case "siliconflow", "siliconflowkey", "siliconflowtoken":
			add("siliconflow", `(?i)\bsk-[A-Za-z0-9]{32,}\b`)
		case "hunyuan", "tencenthunyuan", "hunyuansecretid", "tencentcloud":
			add("hunyuan", `(?i)\bAKID[A-Za-z0-9]{16,}\b`)
		case "qianfan", "baiduqianfan", "wenxin", "ernie", "baiducloud":
			add("qianfan", `(?i)\bbce-v3/[A-Za-z0-9._~+/=-]{24,}\b`)
		case "volcengine", "volcano", "doubao", "ark", "volcengineark", "volctoken":
			add("volcengine", `(?i)\b(?:aklt|volc)[A-Za-z0-9_-]{20,}\b`)
		case "grafana", "grafanakey", "grafanatoken", "grafanaserviceaccount":
			add("grafana", `(?i)\bgl(?:sa|c)_[A-Za-z0-9_=-]{20,}\b`)
		case "sentry", "sentrykey", "sentrytoken", "sentryauthtoken":
			add("sentry", `(?i)\bsntrys_[A-Za-z0-9_=-]{20,}\b`)
		case "newrelic", "newrelickey", "newrelictoken", "newrelicuserkey":
			add("newrelic", `(?i)\bNRAK[-_A-Za-z0-9]{20,}\b`)
		case "azure", "azureopenai", "azureai", "azurekey", "azuretoken", "azureopenaikey", "azureopenaitoken":
			add("azure_openai", `(?i)\b(?:azure[_-]?(?:openai|ai)?[_-]?(?:api[_-]?)?key|AZURE_OPENAI_API_KEY|OCP_APIM_SUBSCRIPTION_KEY)[\s:=_-]+[A-Za-z0-9]{32,}\b`)
		case "azuredevops", "azuredevopspat", "azdo", "azdopat":
			add("azure_devops", `(?i)\b[A-Za-z0-9]{76}AZDO[A-Za-z0-9]{4}\b`)
		case "datadog", "datadogkey", "datadogtoken", "datadogapikey", "datadogappkey":
			add("datadog", `(?i)\b(?:datadog|dd)[_-]?(?:api|app)?[_-]?key[_-]?[A-Fa-f0-9]{32,40}\b`)
		case "pagerduty", "pagerdutykey", "pagerdutytoken", "pdtoken":
			add("pagerduty", `(?i)\bpd(?:us|at)\+[A-Za-z0-9._~-]{20,}\b`)
		case "posthog", "posthogkey", "posthogtoken":
			add("posthog", `(?i)\bph[cp]_[A-Za-z0-9_-]{20,}\b`)
		case "splunk", "splunktoken", "splunkhectoken":
			add("splunk", `(?i)\bSplunk\s+[A-Za-z0-9+/=_-]{20,}\b`)
		case "elastic", "elasticsearch", "elastickey", "elastictoken", "elasticsearchapikey":
			add("elastic", `(?i)\bApiKey\s+[A-Za-z0-9+/=_-]{20,}\b`)
		case "terraform", "terraformcloud", "terraformtoken", "tfc", "tfctoken":
			add("terraform", `(?i)\batlasv1\.[A-Za-z0-9_-]{20,}\b`)
		case "npm", "npmtoken":
			add("npm", `(?i)npm_[A-Za-z0-9]{36,}`)
		case "stripe", "stripetoken":
			add("stripe", `(?i)(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}`)
		case "jwt":
			add("jwt", `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)
		case "aws", "awsaccesskey":
			add("aws", `AKIA[0-9A-Z]{16}`)
		case "privatekey", "pem":
			add("private_key", `-----BEGIN [A-Z ]*PRIVATE KEY-----`)
		case "kubernetes", "kubernetessecret", "k8ssecret":
			add("k8s_secret_yaml", `(?im)^\s*kind:\s*Secret\s*$`)
		case "kubeconfig", "kubernetesconfig", "k8sconfig":
			add("kubeconfig", `(?is)\bclusters\s*:.*\busers\s*:.*\bcurrent-context\s*:`)
		case "docker", "dockerconfig", "dockerauth":
			add("docker_config", `(?is)"auths"\s*:\s*\{.*"auth"\s*:`)
		}
	}
	return out
}

func gatewaySecretClassifierMatches(value string, secretTypes []string) []string {
	out := make([]string, 0)
	for _, classifier := range gatewaySecretClassifierPatternSpecs(secretTypes) {
		if classifier.Pattern.MatchString(value) {
			out = gatewayAppendUniqueStrings(out, classifier.Name)
		}
	}
	return out
}

func gatewayRegexSummary(pattern *regexp.Regexp) string {
	if pattern == nil {
		return ""
	}
	text := pattern.String()
	if len(text) <= 80 {
		return text
	}
	return text[:80]
}

func gatewayReplaceRegexMatches(value string, pattern *regexp.Regexp, rule gatewayRedactionRule) string {
	return pattern.ReplaceAllStringFunc(value, func(match string) string {
		return gatewayMaskString(match, rule)
	})
}

func gatewayStringSliceContainsAny(values []string, needles ...string) bool {
	for _, value := range values {
		for _, needle := range needles {
			if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(needle)) {
				return true
			}
		}
	}
	return false
}

func gatewayAppendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	out := make([]string, 0, len(values)+len(additions))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, text)
	}
	for _, value := range additions {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, text)
	}
	return out
}

func gatewayLimitedSortedStrings(values []string, limit int) []string {
	values = gatewayAppendUniqueStrings(nil, values...)
	sort.Strings(values)
	if limit > 0 && len(values) > limit {
		return values[:limit]
	}
	return values
}

func gatewayAuditFieldPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "$"
	}
	parts := strings.Split(path, ".")
	for index, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			parts[index] = "*"
		}
	}
	return strings.Join(parts, ".")
}

func gatewayRedactSensitiveTextWithRule(value string, rule gatewayRedactionRule) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	out := gatewaySensitiveValuePattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := gatewaySensitiveValuePattern.FindStringSubmatch(match)
		if len(parts) < 4 {
			return gatewayRedactionReplacement(rule)
		}
		replacement := gatewayRedactionReplacement(rule)
		if rule.PreserveFormat {
			replacement = gatewayMaskString(parts[3], rule)
		}
		return parts[1] + parts[2] + replacement
	})
	for _, pattern := range gatewayCompiledRedactionPatterns(rule.ValuePatterns) {
		out = gatewayReplaceRegexMatches(out, pattern, rule)
	}
	for _, pattern := range gatewaySecretClassifierPatterns(rule.SecretTypes) {
		out = gatewayReplaceRegexMatches(out, pattern, rule)
	}
	return out
}

func gatewayMaskString(value string, rule gatewayRedactionRule) string {
	replacement := gatewayRedactionReplacement(rule)
	if !rule.PreserveFormat {
		return replacement
	}
	if value == "" {
		return replacement
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-4:])
}

func gatewayRedactionReplacement(rule gatewayRedactionRule) string {
	if rule.Replacement != "" {
		return rule.Replacement
	}
	return "[REDACTED]"
}

func gatewayJoinFieldPath(parent, child string) string {
	child = strings.TrimSpace(child)
	if child == "" {
		return parent
	}
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func gatewayRedactionPathAllowed(path string, patterns []string) bool {
	if strings.TrimSpace(path) == "" || len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		if gatewayFieldPathMatches(pattern, path, "") {
			return true
		}
	}
	return false
}

func gatewayRedactionFieldMatches(path, key string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		if gatewayFieldPathMatches(pattern, path, key) {
			return true
		}
	}
	return false
}

func gatewayFieldPathMatches(pattern, path, key string) bool {
	pattern = normalizeGatewayFieldPath(pattern)
	path = normalizeGatewayFieldPath(path)
	key = normalizeGatewayFieldPath(key)
	if pattern == "" || path == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if pattern == path || pattern == key {
		return true
	}
	if gatewayFieldPathSegmentPatternMatches(pattern, path) || (key != "" && gatewayFieldPathSegmentPatternMatches(pattern, key)) {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*.")
		return path == suffix || strings.HasSuffix(path, "."+suffix)
	}
	return false
}

func gatewayFieldPathSegmentPatternMatches(pattern, path string) bool {
	if pattern == "" || path == "" {
		return false
	}
	patternParts := strings.Split(pattern, ".")
	pathParts := strings.Split(path, ".")
	if len(patternParts) != len(pathParts) {
		return false
	}
	for index, patternPart := range patternParts {
		if patternPart == "*" {
			continue
		}
		if patternPart != pathParts[index] {
			return false
		}
	}
	return true
}

func normalizeGatewayFieldPath(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "[*]", ".*")
	value = strings.ReplaceAll(value, "[]", "")
	value = strings.Trim(value, ".")
	for strings.Contains(value, "..") {
		value = strings.ReplaceAll(value, "..", ".")
	}
	return value
}

func gatewayConditionValues(conditions map[string]any, aliases ...string) map[string]any {
	out := make(map[string]any, len(conditions)+4)
	for key, value := range conditions {
		out[key] = value
	}
	for _, alias := range aliases {
		raw, ok := gatewayConditionRaw(conditions, alias)
		if !ok {
			continue
		}
		for key, value := range mapValue(raw) {
			out[key] = value
		}
	}
	return out
}

func gatewayFirstPositiveInt(values map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		value, ok := gatewayPositiveInt(raw)
		if ok {
			return value, true
		}
	}
	return 0, false
}

func gatewayFirstPositiveFloat(values map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		value, ok := gatewayPositiveFloat(raw)
		if ok {
			return value, true
		}
	}
	return 0, false
}

func gatewayPositiveFloatSum(values map[string]any, keys ...string) float64 {
	total := 0.0
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		value, ok := gatewayPositiveFloat(raw)
		if ok {
			total += value
		}
	}
	return total
}

func gatewayFirstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok || raw == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(raw))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func gatewayFirstBool(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		if boolFromAny(raw) {
			return true
		}
	}
	return false
}

func gatewayConditionStringList(values map[string]any, keys ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		for _, value := range gatewayStringList(raw) {
			normalized := strings.TrimSpace(value)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	return out
}

func gatewayStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parts := strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '\n' || r == ';'
		})
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if text := strings.TrimSpace(part); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func gatewayConditionRaw(values map[string]any, key string) (any, bool) {
	if values == nil {
		return nil, false
	}
	if value, ok := values[key]; ok {
		return value, true
	}
	normalized := normalizeGatewayConditionKey(key)
	for candidate, value := range values {
		if normalizeGatewayConditionKey(candidate) == normalized {
			return value, true
		}
	}
	return nil, false
}

func normalizeGatewayConditionKey(key string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", ".", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(key)))
}

func gatewayPositiveInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, typed > 0
	case int32:
		value := int(typed)
		return value, value > 0
	case int64:
		value := int(typed)
		return value, value > 0
	case float32:
		value := int(typed)
		return value, value > 0
	case float64:
		value := int(typed)
		return value, value > 0
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			asFloat, floatErr := strconv.ParseFloat(typed.String(), 64)
			if floatErr != nil {
				return 0, false
			}
			value := int(asFloat)
			return value, value > 0
		}
		value := int(parsed)
		return value, value > 0
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		value := int(parsed)
		return value, value > 0
	default:
		return 0, false
	}
}

func gatewayNonNegativeInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, typed >= 0
	case int32:
		value := int(typed)
		return value, value >= 0
	case int64:
		value := int(typed)
		return value, value >= 0
	case float32:
		value := int(typed)
		return value, value >= 0
	case float64:
		value := int(typed)
		return value, value >= 0
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			asFloat, floatErr := strconv.ParseFloat(typed.String(), 64)
			if floatErr != nil {
				return 0, false
			}
			value := int(asFloat)
			return value, value >= 0
		}
		value := int(parsed)
		return value, value >= 0
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		value := int(parsed)
		return value, value >= 0
	default:
		return 0, false
	}
}

func gatewayPositiveFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		value := float64(typed)
		return value, value > 0
	case int32:
		value := float64(typed)
		return value, value > 0
	case int64:
		value := float64(typed)
		return value, value > 0
	case float32:
		value := float64(typed)
		return value, value > 0
	case float64:
		return typed, typed > 0
	case json.Number:
		parsed, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil {
			return 0, false
		}
		return parsed, parsed > 0
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		return parsed, parsed > 0
	default:
		return 0, false
	}
}

func gatewayConditionWindow(values map[string]any, fallback time.Duration, fallbackLabel string) (time.Duration, string) {
	if seconds, ok := gatewayFirstPositiveInt(values, "windowSeconds", "windowSecond", "seconds"); ok {
		duration := time.Duration(seconds) * time.Second
		return duration, formatGatewayWindowLabel(duration)
	}
	if minutes, ok := gatewayFirstPositiveInt(values, "windowMinutes", "windowMinute", "minutes"); ok {
		duration := time.Duration(minutes) * time.Minute
		return duration, formatGatewayWindowLabel(duration)
	}
	if hours, ok := gatewayFirstPositiveInt(values, "windowHours", "windowHour", "hours"); ok {
		duration := time.Duration(hours) * time.Hour
		return duration, formatGatewayWindowLabel(duration)
	}
	text := gatewayFirstString(values, "window", "windowDuration", "duration")
	if text != "" {
		if duration, err := time.ParseDuration(text); err == nil && duration > 0 {
			return duration, formatGatewayWindowLabel(duration)
		}
	}
	return fallback, fallbackLabel
}

func formatGatewayWindowLabel(duration time.Duration) string {
	if duration%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	}
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	}
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int(duration/time.Second))
	}
	return duration.String()
}

func gatewayLimitScope(values map[string]any, fallback string) string {
	scope := normalizeGatewayLimitScope(gatewayFirstString(values, "scope", "limitScope", "counterScope"))
	if scope == "" {
		return normalizeGatewayLimitScope(fallback)
	}
	return scope
}

func normalizeGatewayLimitScope(scope string) string {
	normalized := strings.ToLower(strings.TrimSpace(scope))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "", "default":
		return ""
	case "global", "shared":
		return "global"
	case "client", "ai_client", "per_client":
		return "client"
	case "client_tool", "ai_client_tool", "per_client_tool":
		return "client_tool"
	case "actor", "subject", "user", "service_account", "per_actor", "per_user", "per_subject":
		return "actor"
	case "actor_tool", "subject_tool", "user_tool", "per_actor_tool", "per_user_tool", "per_subject_tool":
		return "actor_tool"
	case "actor_client", "subject_client", "user_client", "per_actor_client", "per_user_client", "per_subject_client":
		return "actor_client"
	default:
		return "actor_client_tool"
	}
}

func gatewayValueContainsSensitiveData(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case map[string]any:
		for key, item := range typed {
			if gatewaySensitiveKey(key) {
				return true
			}
			if gatewayValueContainsSensitiveData(item) {
				return true
			}
		}
		return false
	case []any:
		for _, item := range typed {
			if gatewayValueContainsSensitiveData(item) {
				return true
			}
		}
		return false
	case []map[string]any:
		for _, item := range typed {
			if gatewayValueContainsSensitiveData(item) {
				return true
			}
		}
		return false
	case string:
		return gatewaySensitiveValuePattern.MatchString(typed)
	default:
		return false
	}
}

func (s *Service) filterToolsByAccessPolicies(ctx context.Context, principal domainidentity.Principal, aiClientID, skillID string, tools []domainaigateway.ToolCapability) ([]domainaigateway.ToolCapability, int, error) {
	policies, err := s.activeAccessPolicies(ctx, principal, aiClientID)
	if err != nil {
		return nil, 0, err
	}
	if len(policies) == 0 {
		return tools, 0, nil
	}
	out := make([]domainaigateway.ToolCapability, 0, len(tools))
	denied := 0
	for _, tool := range tools {
		allowed, decision, _ := toolAllowedByAccessPoliciesWithSkills(tool, policies, skillID, s.gatewaySkills())
		if allowed {
			if decision.Strategy != "" {
				tool.RequiresApproval = decision.requiresApproval()
			}
			out = append(out, tool)
			continue
		}
		denied++
	}
	return out, denied, nil
}

func (s *Service) filterSkillsByAccessPolicies(ctx context.Context, principal domainidentity.Principal, aiClientID string, skills []domainaigateway.SkillCapability) ([]domainaigateway.SkillCapability, int, error) {
	policies, err := s.activeAccessPolicies(ctx, principal, aiClientID)
	if err != nil {
		return nil, 0, err
	}
	if len(policies) == 0 {
		return skills, 0, nil
	}
	out := make([]domainaigateway.SkillCapability, 0, len(skills))
	denied := 0
	for _, skill := range skills {
		allowed, _ := skillAllowedByAccessPolicies(skill, policies)
		if allowed {
			out = append(out, skill)
			continue
		}
		denied++
	}
	return out, denied, nil
}

func (s *Service) activeAccessPolicies(ctx context.Context, principal domainidentity.Principal, aiClientID string) ([]domainaigateway.AccessPolicy, error) {
	if s.repo == nil {
		return nil, nil
	}
	aiClientID = strings.TrimSpace(aiClientID)
	out := make([]domainaigateway.AccessPolicy, 0)
	seen := map[string]struct{}{}
	appendPolicies := func(subjectType, subjectID string) error {
		subjectType = normalizeSubjectType(subjectType)
		subjectID = strings.TrimSpace(subjectID)
		if subjectType == "" || subjectID == "" {
			return nil
		}
		items, err := s.repo.ListActiveAccessPolicies(ctx, subjectType, subjectID, aiClientID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			out = append(out, item)
		}
		return nil
	}
	subjectType, subjectID := gatewaySubject(principal)
	if err := appendPolicies(subjectType, subjectID); err != nil {
		return nil, err
	}
	for _, role := range principal.Roles {
		if err := appendPolicies("role", role); err != nil {
			return nil, err
		}
	}
	for _, team := range principal.Teams {
		if err := appendPolicies("team", team); err != nil {
			return nil, err
		}
	}
	if aiClientID != "" {
		if err := appendPolicies("ai_client", aiClientID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) authorizeSkillBinding(ctx context.Context, principal domainidentity.Principal, aiClientID, skillID string, tool domainaigateway.ToolCapability) error {
	bindings, err := s.activeSkillBindings(ctx, principal, aiClientID)
	if err != nil {
		return err
	}
	if len(bindings) == 0 {
		return nil
	}
	allowed, reason := toolAllowedBySkillBindingsWithSkills(tool, bindings, skillID, s.gatewaySkills())
	if allowed {
		return nil
	}
	return fmt.Errorf("%w: AI Gateway skill binding rejected %s: %s", apperrors.ErrAccessDenied, tool.Name, reason)
}

func (s *Service) activeSkillBindings(ctx context.Context, principal domainidentity.Principal, aiClientID string) ([]domainaigateway.SkillBinding, error) {
	if s.repo == nil {
		return nil, nil
	}
	aiClientID = strings.TrimSpace(aiClientID)
	out := make([]domainaigateway.SkillBinding, 0)
	seen := map[string]struct{}{}
	appendBindings := func(subjectType, subjectID string) error {
		subjectType = normalizeSubjectType(subjectType)
		subjectID = strings.TrimSpace(subjectID)
		if subjectType == "" || subjectID == "" {
			return nil
		}
		items, err := s.repo.ListActiveSkillBindings(ctx, subjectType, subjectID, aiClientID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			out = append(out, item)
		}
		return nil
	}
	subjectType, subjectID := gatewaySubject(principal)
	if err := appendBindings(subjectType, subjectID); err != nil {
		return nil, err
	}
	for _, role := range principal.Roles {
		if err := appendBindings("role", role); err != nil {
			return nil, err
		}
	}
	for _, team := range principal.Teams {
		if err := appendBindings("team", team); err != nil {
			return nil, err
		}
	}
	if aiClientID != "" {
		if err := appendBindings("ai_client", aiClientID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) invokeDeliveryTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if !strings.HasPrefix(tool.Name, "delivery.") {
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
	if s.apps == nil || s.delivery == nil {
		return nil, nil, fmt.Errorf("%w: delivery gateway services are not configured", apperrors.ErrInvalidArgument)
	}
	switch tool.Name {
	case "delivery.applications.list":
		var req struct {
			Search string `json:"search"`
			Limit  int    `json:"limit"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		items, err := s.apps.List(ctx, principal, domainapp.Filter{Search: req.Search, Limit: req.Limit})
		return items, map[string]any{"count": len(items)}, err
	case "delivery.applications.detail":
		applicationID := stringInput(input, "applicationId")
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		item, err := s.delivery.GetApplicationDetail(ctx, principal, applicationID)
		return item, map[string]any{"applicationId": applicationID, "bindingCount": len(item.Bindings)}, err
	case "delivery.applications.create":
		var req domainapp.UpsertInput
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		item, err := s.apps.Create(ctx, principal, req)
		return item, map[string]any{"applicationId": item.ID}, err
	case "delivery.application_environments.list":
		applicationID := stringInput(input, "applicationId")
		bindingID := firstNonEmpty(stringInput(input, "applicationEnvironmentId"), stringInput(input, "bindingId"))
		if bindingID != "" {
			item, err := s.delivery.GetApplicationEnvironmentDetail(ctx, principal, bindingID)
			return item, map[string]any{"applicationEnvironmentId": bindingID}, err
		}
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId or applicationEnvironmentId is required", apperrors.ErrInvalidArgument)
		}
		detail, err := s.delivery.GetApplicationDetail(ctx, principal, applicationID)
		if err != nil {
			return nil, nil, err
		}
		return detail.Bindings, map[string]any{"applicationId": applicationID, "count": len(detail.Bindings)}, nil
	case "delivery.application_services.list":
		applicationID := stringInput(input, "applicationId")
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		items, err := s.apps.ListServices(ctx, principal, applicationID)
		items = redactedApplicationServices(items)
		return items, map[string]any{"applicationId": applicationID, "count": len(items)}, err
	case "delivery.build_sources.list":
		var req struct {
			ApplicationID string `json:"applicationId"`
			WithBindings  bool   `json:"withBindings"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(req.ApplicationID) == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		detail, err := s.delivery.GetApplicationDetail(ctx, principal, req.ApplicationID)
		if err != nil {
			return nil, nil, err
		}
		output := map[string]any{
			"applicationId": req.ApplicationID,
			"buildSources":  redactedBuildSources(detail.Application.BuildSources),
		}
		related := map[string]any{"applicationId": req.ApplicationID, "count": len(detail.Application.BuildSources)}
		if req.WithBindings {
			output["bindingUsage"] = buildSourceBindingUsage(detail)
			related["bindingCount"] = len(detail.Bindings)
		}
		return output, related, nil
	case "delivery.release_targets.list":
		applicationID := stringInput(input, "applicationId")
		bindingID := firstNonEmpty(stringInput(input, "applicationEnvironmentId"), stringInput(input, "bindingId"))
		if bindingID != "" {
			detail, err := s.delivery.GetApplicationEnvironmentDetail(ctx, principal, bindingID)
			if err != nil {
				return nil, nil, err
			}
			return detail.Binding.Targets, map[string]any{
				"applicationId":            detail.Application.ID,
				"applicationEnvironmentId": bindingID,
				"count":                    len(detail.Binding.Targets),
			}, nil
		}
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId or applicationEnvironmentId is required", apperrors.ErrInvalidArgument)
		}
		detail, err := s.delivery.GetApplicationDetail(ctx, principal, applicationID)
		if err != nil {
			return nil, nil, err
		}
		targets := releaseTargetsFromApplicationDetail(detail)
		return targets, map[string]any{"applicationId": applicationID, "count": len(targets)}, nil
	case "delivery.actions.trigger":
		var req struct {
			ApplicationID string `json:"applicationId"`
			domaindelivery.ApplicationDeliveryActionInput
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(req.ApplicationID) == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		item, err := s.delivery.TriggerApplicationDeliveryAction(ctx, principal, req.ApplicationID, req.ApplicationDeliveryActionInput)
		return item, map[string]any{
			"applicationId":            item.ApplicationID,
			"applicationEnvironmentId": item.ApplicationEnvironmentID,
			"releaseBundleId":          item.RelatedIDs.ReleaseBundleID,
			"executionTaskId":          item.RelatedIDs.ExecutionTaskID,
			"workflowRunId":            item.RelatedIDs.WorkflowRunID,
		}, err
	case "delivery.release_bundles.list":
		var req struct {
			ApplicationID            string `json:"applicationId"`
			ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
			Limit                    int    `json:"limit"`
			BundleID                 string `json:"bundleId"`
			Artifacts                bool   `json:"artifacts"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if req.Artifacts && strings.TrimSpace(req.BundleID) != "" {
			items, err := s.delivery.ListReleaseBundleArtifacts(ctx, principal, req.BundleID)
			return items, map[string]any{"releaseBundleId": req.BundleID, "count": len(items)}, err
		}
		items, err := s.delivery.ListReleaseBundles(ctx, principal, domaindelivery.ReleaseBundleFilter{
			ApplicationID:            req.ApplicationID,
			ApplicationEnvironmentID: req.ApplicationEnvironmentID,
			Limit:                    req.Limit,
		})
		return items, map[string]any{"count": len(items)}, err
	case "delivery.execution_tasks.list":
		var req struct {
			ApplicationID            string `json:"applicationId"`
			ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
			ReleaseBundleID          string `json:"releaseBundleId"`
			Status                   string `json:"status"`
			ProviderKind             string `json:"providerKind"`
			Limit                    int    `json:"limit"`
			TaskID                   string `json:"taskId"`
			Logs                     bool   `json:"logs"`
			LogLimit                 int    `json:"logLimit"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if req.Logs && strings.TrimSpace(req.TaskID) != "" {
			items, err := s.delivery.ListExecutionLogs(ctx, principal, req.TaskID, req.LogLimit)
			items = redactExecutionLogs(items)
			return items, map[string]any{"executionTaskId": req.TaskID, "count": len(items)}, err
		}
		items, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
			ApplicationID:            req.ApplicationID,
			ApplicationEnvironmentID: req.ApplicationEnvironmentID,
			ReleaseBundleID:          req.ReleaseBundleID,
			Status:                   req.Status,
			ProviderKind:             req.ProviderKind,
			Limit:                    req.Limit,
		})
		return items, map[string]any{"count": len(items)}, err
	case "delivery.execution_logs.list":
		var req struct {
			TaskID string `json:"taskId"`
			Limit  int    `json:"limit"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(req.TaskID) == "" {
			return nil, nil, fmt.Errorf("%w: taskId is required", apperrors.ErrInvalidArgument)
		}
		items, err := s.delivery.ListExecutionLogs(ctx, principal, req.TaskID, req.Limit)
		items = redactExecutionLogs(items)
		return items, map[string]any{"executionTaskId": req.TaskID, "count": len(items)}, err
	case "delivery.approval_policies.list":
		items, err := s.delivery.ListApprovalPolicies(ctx, principal)
		return items, map[string]any{"count": len(items)}, err
	case "delivery.workflow_templates.list":
		if s.catalog == nil {
			return nil, nil, fmt.Errorf("%w: catalog gateway services are not configured", apperrors.ErrInvalidArgument)
		}
		items, err := s.catalog.ListWorkflowTemplates(ctx, principal)
		return items, map[string]any{"count": len(items)}, err
	case "delivery.release_context.diff":
		return s.buildReleaseContextDiff(ctx, principal, input)
	case "delivery.rollback.context":
		return s.buildRollbackContext(ctx, principal, input)
	default:
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
}

func (s *Service) invokeKubernetesTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if s.resources == nil {
		return nil, nil, fmt.Errorf("%w: Kubernetes resource gateway service is not configured", apperrors.ErrInvalidArgument)
	}
	var req struct {
		ClusterID      string `json:"clusterId"`
		Namespace      string `json:"namespace"`
		PodName        string `json:"podName"`
		DeploymentName string `json:"deploymentName"`
		ServiceName    string `json:"serviceName"`
		NodeName       string `json:"nodeName"`
		Container      string `json:"container"`
		TailLines      int64  `json:"tailLines"`
		SinceSeconds   int64  `json:"sinceSeconds"`
		Previous       bool   `json:"previous"`
		Limit          int    `json:"limit"`
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	req.ClusterID = strings.TrimSpace(req.ClusterID)
	req.Namespace = strings.TrimSpace(req.Namespace)
	if req.ClusterID == "" {
		return nil, nil, fmt.Errorf("%w: clusterId is required", apperrors.ErrInvalidArgument)
	}
	related := map[string]any{"clusterId": req.ClusterID, "namespace": req.Namespace}
	switch tool.Name {
	case "k8s.pods.list":
		items, err := s.resources.ListPods(ctx, principal, req.ClusterID, req.Namespace)
		related["count"] = len(items)
		return items, related, err
	case "k8s.pods.logs":
		req.PodName = strings.TrimSpace(req.PodName)
		if req.PodName == "" {
			return nil, related, fmt.Errorf("%w: podName is required", apperrors.ErrInvalidArgument)
		}
		item, err := s.resources.GetPodLogs(ctx, principal, req.ClusterID, req.Namespace, req.PodName, req.Container, req.TailLines, req.SinceSeconds, req.Previous)
		item = redactPodLogs(item)
		related["podName"] = req.PodName
		related["container"] = strings.TrimSpace(req.Container)
		return item, related, err
	case "k8s.pods.describe":
		req.PodName = strings.TrimSpace(req.PodName)
		if req.Namespace == "" || req.PodName == "" {
			return nil, related, fmt.Errorf("%w: namespace and podName are required", apperrors.ErrInvalidArgument)
		}
		item, err := s.resources.GetPodDetail(ctx, principal, req.ClusterID, req.Namespace, req.PodName)
		related["podName"] = req.PodName
		return podDescribeContext(item), related, err
	case "k8s.deployments.list":
		items, err := s.resources.ListDeployments(ctx, principal, req.ClusterID, req.Namespace)
		related["count"] = len(items)
		return items, related, err
	case "k8s.deployments.rollout_status":
		req.DeploymentName = strings.TrimSpace(req.DeploymentName)
		if req.Namespace == "" || req.DeploymentName == "" {
			return nil, related, fmt.Errorf("%w: namespace and deploymentName are required", apperrors.ErrInvalidArgument)
		}
		item, err := s.resources.GetDeploymentRolloutStatus(ctx, principal, req.ClusterID, req.Namespace, req.DeploymentName)
		related["deploymentName"] = req.DeploymentName
		return item, related, err
	case "k8s.deployments.events":
		req.DeploymentName = strings.TrimSpace(req.DeploymentName)
		if req.Namespace == "" || req.DeploymentName == "" {
			return nil, related, fmt.Errorf("%w: namespace and deploymentName are required", apperrors.ErrInvalidArgument)
		}
		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}
		items, err := s.resources.ListClusterEvents(ctx, principal, req.ClusterID, req.Namespace, limit)
		filtered := filterEventsForDiagnosis(items, "", req.DeploymentName)
		related["deploymentName"] = req.DeploymentName
		related["count"] = len(filtered)
		related["limit"] = limit
		return filtered, related, err
	case "k8s.services.list":
		items, err := s.resources.ListServices(ctx, principal, req.ClusterID, req.Namespace)
		related["count"] = len(items)
		return items, related, err
	case "k8s.services.backends":
		req.ServiceName = strings.TrimSpace(req.ServiceName)
		if req.Namespace == "" || req.ServiceName == "" {
			return nil, related, fmt.Errorf("%w: namespace and serviceName are required", apperrors.ErrInvalidArgument)
		}
		item, err := s.serviceBackendContext(ctx, principal, req.ClusterID, req.Namespace, req.ServiceName)
		related["serviceName"] = req.ServiceName
		related["backendPodCount"] = item["backendPodCount"]
		return item, related, err
	case "k8s.routes.context":
		item, err := s.routeContext(ctx, principal, req.ClusterID, req.Namespace, req.ServiceName)
		related["serviceName"] = strings.TrimSpace(req.ServiceName)
		related["ingressCount"] = item["ingressCount"]
		related["httpRouteCount"] = item["httpRouteCount"]
		return item, related, err
	case "k8s.storage.context":
		item, err := s.storageContext(ctx, principal, req.ClusterID, req.Namespace)
		related["persistentVolumeClaimCount"] = item["persistentVolumeClaimCount"]
		related["persistentVolumeCount"] = item["persistentVolumeCount"]
		related["storageClassCount"] = item["storageClassCount"]
		return item, related, err
	case "k8s.nodes.detail":
		req.NodeName = strings.TrimSpace(req.NodeName)
		if req.NodeName == "" {
			return nil, related, fmt.Errorf("%w: nodeName is required", apperrors.ErrInvalidArgument)
		}
		item, err := s.resources.GetNodeDetail(ctx, principal, req.ClusterID, req.NodeName)
		related["nodeName"] = req.NodeName
		related["scheduledPodCount"] = len(item.Pods)
		return item, related, err
	case "k8s.events.list":
		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}
		items, err := s.resources.ListClusterEvents(ctx, principal, req.ClusterID, req.Namespace, limit)
		related["count"] = len(items)
		related["limit"] = limit
		return items, related, err
	default:
		return nil, related, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
}

func (s *Service) buildReleaseContextDiff(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var req struct {
		ApplicationID            string `json:"applicationId"`
		ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
		SourceBundleID           string `json:"sourceBundleId"`
		TargetBundleID           string `json:"targetBundleId"`
		ReleaseBundleID          string `json:"releaseBundleId"`
		Limit                    int    `json:"limit"`
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	req.ApplicationID = strings.TrimSpace(req.ApplicationID)
	req.ApplicationEnvironmentID = strings.TrimSpace(req.ApplicationEnvironmentID)
	req.SourceBundleID = strings.TrimSpace(req.SourceBundleID)
	req.TargetBundleID = firstNonEmpty(req.TargetBundleID, req.ReleaseBundleID)
	if req.ApplicationID == "" {
		return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	detail, err := s.delivery.GetApplicationDetail(ctx, principal, req.ApplicationID)
	if err != nil {
		return nil, nil, err
	}
	bundles, err := s.delivery.ListReleaseBundles(ctx, principal, domaindelivery.ReleaseBundleFilter{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		Limit:                    limit,
	})
	if err != nil {
		return nil, nil, err
	}
	tasks, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		ReleaseBundleID:          firstNonEmpty(req.TargetBundleID, req.SourceBundleID),
		Limit:                    limit,
	})
	if err != nil {
		return nil, nil, err
	}

	var sourceBundle *domaindelivery.ReleaseBundle
	var targetBundle *domaindelivery.ReleaseBundle
	if req.SourceBundleID != "" {
		sourceBundle, err = s.releaseBundleForContext(ctx, principal, req.SourceBundleID, bundles)
		if err != nil {
			return nil, nil, err
		}
	}
	if req.TargetBundleID != "" {
		targetBundle, err = s.releaseBundleForContext(ctx, principal, req.TargetBundleID, bundles)
		if err != nil {
			return nil, nil, err
		}
	}
	if targetBundle == nil && len(bundles) > 0 {
		copyItem := bundles[0]
		targetBundle = &copyItem
	}

	bindingSummaries := filterBindingSummaries(detail.Bindings, req.ApplicationEnvironmentID)
	output := map[string]any{
		"summary": "collected delivery release diff and promotion context",
		"scope": map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"sourceBundleId":           req.SourceBundleID,
			"targetBundleId":           req.TargetBundleID,
		},
		"application":    redactedApplication(detail.Application),
		"bindings":       redactedBindingSummaries(bindingSummaries),
		"releaseBundles": redactedReleaseBundles(bundles),
		"sourceBundle":   redactedReleaseBundlePtr(sourceBundle),
		"targetBundle":   redactedReleaseBundlePtr(targetBundle),
		"executionTasks": redactedExecutionTasks(tasks),
		"comparison":     compareReleaseBundles(sourceBundle, targetBundle),
		"nextChecks": []string{
			"Verify target binding release policy, approval policy, and enabled release targets before triggering a promotion.",
			"Inspect execution task logs for the candidate bundle if recent tasks are not successful.",
		},
	}
	related := map[string]any{
		"applicationId":            req.ApplicationID,
		"applicationEnvironmentId": req.ApplicationEnvironmentID,
		"releaseBundleCount":       len(bundles),
		"executionTaskCount":       len(tasks),
	}
	if sourceBundle != nil {
		related["sourceBundleId"] = sourceBundle.ID
	}
	if targetBundle != nil {
		related["targetBundleId"] = targetBundle.ID
	}
	return output, related, nil
}

func (s *Service) buildRollbackContext(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var req struct {
		ApplicationID            string `json:"applicationId"`
		ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
		ReleaseBundleID          string `json:"releaseBundleId"`
		ExecutionTaskID          string `json:"executionTaskId"`
		Limit                    int    `json:"limit"`
		LogLimit                 int    `json:"logLimit"`
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	req.ApplicationID = strings.TrimSpace(req.ApplicationID)
	req.ApplicationEnvironmentID = strings.TrimSpace(req.ApplicationEnvironmentID)
	req.ReleaseBundleID = strings.TrimSpace(req.ReleaseBundleID)
	req.ExecutionTaskID = strings.TrimSpace(req.ExecutionTaskID)
	if req.ApplicationID == "" {
		return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	logLimit := req.LogLimit
	if logLimit <= 0 {
		logLimit = 100
	}

	detail, err := s.delivery.GetApplicationDetail(ctx, principal, req.ApplicationID)
	if err != nil {
		return nil, nil, err
	}
	bundles, err := s.delivery.ListReleaseBundles(ctx, principal, domaindelivery.ReleaseBundleFilter{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		Limit:                    limit,
	})
	if err != nil {
		return nil, nil, err
	}
	tasks, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		ReleaseBundleID:          req.ReleaseBundleID,
		Limit:                    limit,
	})
	if err != nil {
		return nil, nil, err
	}

	var currentTask *domaindelivery.ExecutionTask
	if req.ExecutionTaskID != "" {
		currentTask, err = s.executionTaskForContext(ctx, principal, req.ExecutionTaskID, tasks)
		if err != nil {
			return nil, nil, err
		}
	}
	if currentTask == nil && len(tasks) > 0 {
		copyItem := tasks[0]
		currentTask = &copyItem
	}
	logs := []domaindelivery.ExecutionLog{}
	if currentTask != nil {
		logs, err = s.delivery.ListExecutionLogs(ctx, principal, currentTask.ID, logLimit)
		if err != nil {
			return nil, nil, err
		}
		logs = redactExecutionLogs(logs)
	}

	bindingSummaries := filterBindingSummaries(detail.Bindings, req.ApplicationEnvironmentID)
	output := map[string]any{
		"summary": "collected read-only rollback suggestion context",
		"scope": map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
		},
		"application":    redactedApplication(detail.Application),
		"bindings":       redactedBindingSummaries(bindingSummaries),
		"releaseBundles": redactedReleaseBundles(bundles),
		"executionTasks": redactedExecutionTasks(tasks),
		"currentTask":    redactedExecutionTaskPtr(currentTask),
		"executionLogs":  logs,
		"suggestions":    rollbackSuggestions(bindingSummaries, bundles, currentTask),
		"nextChecks": []string{
			"Confirm the selected previous bundle or image tag is valid for the target environment.",
			"Triggering rollback is intentionally outside this read-only tool and must use delivery.actions.trigger with policy approval.",
		},
	}
	related := map[string]any{
		"applicationId":            req.ApplicationID,
		"applicationEnvironmentId": req.ApplicationEnvironmentID,
		"releaseBundleCount":       len(bundles),
		"executionTaskCount":       len(tasks),
		"executionLogCount":        len(logs),
	}
	if currentTask != nil {
		related["executionTaskId"] = currentTask.ID
	}
	return output, related, nil
}

func (s *Service) invokeReleaseFailureDiagnosis(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var req struct {
		ApplicationID            string `json:"applicationId"`
		ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
		ReleaseBundleID          string `json:"releaseBundleId"`
		ExecutionTaskID          string `json:"executionTaskId"`
		ClusterID                string `json:"clusterId"`
		Namespace                string `json:"namespace"`
		WorkloadKind             string `json:"workloadKind"`
		WorkloadName             string `json:"workloadName"`
		PodName                  string `json:"podName"`
		Container                string `json:"container"`
		LogLimit                 int    `json:"logLimit"`
		EventLimit               int    `json:"eventLimit"`
		AgentProviderID          string `json:"agentProviderId"`
		ProviderID               string `json:"providerId"`
		DeepAnalysis             bool   `json:"deepAnalysis"`
		ExternalAnalysis         bool   `json:"externalAnalysis"`
		TimeoutSeconds           int    `json:"timeoutSeconds"`
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	agentProviderID := firstNonEmpty(strings.TrimSpace(req.AgentProviderID), strings.TrimSpace(req.ProviderID))
	diagnosisReq := releaseFailureDiagnosisRequest{
		ApplicationID:            strings.TrimSpace(req.ApplicationID),
		ApplicationEnvironmentID: strings.TrimSpace(req.ApplicationEnvironmentID),
		ReleaseBundleID:          strings.TrimSpace(req.ReleaseBundleID),
		ExecutionTaskID:          strings.TrimSpace(req.ExecutionTaskID),
		ClusterID:                strings.TrimSpace(req.ClusterID),
		Namespace:                strings.TrimSpace(req.Namespace),
		WorkloadKind:             strings.TrimSpace(req.WorkloadKind),
		WorkloadName:             strings.TrimSpace(req.WorkloadName),
		PodName:                  strings.TrimSpace(req.PodName),
		Container:                strings.TrimSpace(req.Container),
		AgentProviderID:          agentProviderID,
		DeepAnalysis:             req.DeepAnalysis || req.ExternalAnalysis || agentProviderID != "",
		TimeoutSeconds:           req.TimeoutSeconds,
	}
	req.ApplicationID = strings.TrimSpace(req.ApplicationID)
	req.ApplicationEnvironmentID = strings.TrimSpace(req.ApplicationEnvironmentID)
	req.ReleaseBundleID = strings.TrimSpace(req.ReleaseBundleID)
	req.ExecutionTaskID = strings.TrimSpace(req.ExecutionTaskID)
	req.ClusterID = strings.TrimSpace(req.ClusterID)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.WorkloadKind = strings.TrimSpace(req.WorkloadKind)
	req.WorkloadName = strings.TrimSpace(req.WorkloadName)
	req.PodName = strings.TrimSpace(req.PodName)
	req.Container = strings.TrimSpace(req.Container)
	related := map[string]any{
		"applicationId":            req.ApplicationID,
		"applicationEnvironmentId": req.ApplicationEnvironmentID,
		"releaseBundleId":          req.ReleaseBundleID,
		"executionTaskId":          req.ExecutionTaskID,
		"clusterId":                req.ClusterID,
		"namespace":                req.Namespace,
	}
	contextView := map[string]any{
		"summary": "collected release failure diagnosis context",
		"scope": map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
			"clusterId":                req.ClusterID,
			"namespace":                req.Namespace,
			"workloadKind":             req.WorkloadKind,
			"workloadName":             req.WorkloadName,
			"podName":                  req.PodName,
		},
		"delivery": map[string]any{},
		"runtime":  map[string]any{},
		"findings": []string{
			"Evidence is collected through soha application services; this Gateway tool does not execute cluster mutations.",
		},
		"nextChecks": []string{},
	}
	deliveryEvidence := contextView["delivery"].(map[string]any)
	runtimeEvidence := contextView["runtime"].(map[string]any)
	nextChecks := []string{}

	if s.delivery != nil {
		if taskID := req.ExecutionTaskID; taskID != "" {
			limit := req.LogLimit
			if limit <= 0 {
				limit = 100
			}
			logs, err := s.delivery.ListExecutionLogs(ctx, principal, taskID, limit)
			if err != nil {
				deliveryEvidence["executionLogsError"] = err.Error()
				nextChecks = append(nextChecks, "Re-check execution task logs after the delivery control plane is reachable.")
			} else {
				logs = redactExecutionLogs(logs)
				deliveryEvidence["executionLogs"] = logs
				deliveryEvidence["executionLogCount"] = len(logs)
				related["executionLogCount"] = len(logs)
			}
		}
		if bundleID := req.ReleaseBundleID; bundleID != "" {
			artifacts, err := s.delivery.ListReleaseBundleArtifacts(ctx, principal, bundleID)
			if err != nil {
				deliveryEvidence["releaseBundleArtifactsError"] = err.Error()
			} else {
				deliveryEvidence["releaseBundleArtifacts"] = artifacts
				deliveryEvidence["releaseBundleArtifactCount"] = len(artifacts)
				related["releaseBundleArtifactCount"] = len(artifacts)
			}
		}
		if req.ApplicationID != "" || req.ApplicationEnvironmentID != "" || req.ReleaseBundleID != "" {
			tasks, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
				ApplicationID:            req.ApplicationID,
				ApplicationEnvironmentID: req.ApplicationEnvironmentID,
				ReleaseBundleID:          req.ReleaseBundleID,
				Limit:                    10,
			})
			if err != nil {
				deliveryEvidence["executionTasksError"] = err.Error()
			} else {
				deliveryEvidence["executionTasks"] = tasks
				deliveryEvidence["executionTaskCount"] = len(tasks)
			}
		}
	} else {
		deliveryEvidence["error"] = "delivery gateway services are not configured"
		nextChecks = append(nextChecks, "Configure delivery services before collecting release execution evidence.")
	}

	if s.resources != nil && req.ClusterID != "" {
		clusterID := req.ClusterID
		namespace := req.Namespace
		eventLimit := req.EventLimit
		if eventLimit <= 0 {
			eventLimit = 100
		}
		if pods, err := s.resources.ListPods(ctx, principal, clusterID, namespace); err != nil {
			runtimeEvidence["podsError"] = err.Error()
		} else {
			runtimeEvidence["pods"] = filterPodsForDiagnosis(pods, req.PodName, req.WorkloadName)
		}
		if deployments, err := s.resources.ListDeployments(ctx, principal, clusterID, namespace); err != nil {
			runtimeEvidence["deploymentsError"] = err.Error()
		} else {
			runtimeEvidence["deployments"] = filterDeploymentsForDiagnosis(deployments, req.WorkloadName)
		}
		if services, err := s.resources.ListServices(ctx, principal, clusterID, namespace); err != nil {
			runtimeEvidence["servicesError"] = err.Error()
		} else {
			runtimeEvidence["services"] = services
		}
		if events, err := s.resources.ListClusterEvents(ctx, principal, clusterID, namespace, eventLimit); err != nil {
			runtimeEvidence["eventsError"] = err.Error()
		} else {
			runtimeEvidence["events"] = filterEventsForDiagnosis(events, req.PodName, req.WorkloadName)
		}
		if podName := req.PodName; podName != "" {
			logs, err := s.resources.GetPodLogs(ctx, principal, clusterID, namespace, podName, req.Container, 200, 0, false)
			if err != nil {
				runtimeEvidence["podLogsError"] = err.Error()
			} else {
				runtimeEvidence["podLogs"] = redactPodLogs(logs)
			}
		}
	} else if req.ClusterID == "" {
		nextChecks = append(nextChecks, "Provide clusterId and namespace to collect runtime Kubernetes evidence.")
	} else {
		runtimeEvidence["error"] = "Kubernetes resource gateway service is not configured"
	}

	contextView["nextChecks"] = nextChecks
	if s.copilot != nil {
		artifactInput := buildReleaseFailureArtifactInput(diagnosisReq, input, contextView)
		if diagnosisReq.DeepAnalysis {
			run, err := s.copilot.QueueGatewayAnalysisAgentRun(ctx, principal, domaincopilot.GatewayAnalysisAgentRunInput{
				GatewayAnalysisArtifactInput: artifactInput,
				AgentProviderID:              agentProviderID,
				TimeoutSeconds:               req.TimeoutSeconds,
			})
			if err != nil {
				contextView["analysisArtifactError"] = err.Error()
				nextChecks = append(nextChecks, "Retry external Agent Runtime queueing after the provider is available.")
				contextView["nextChecks"] = nextChecks
				related["analysisArtifactError"] = err.Error()
			} else {
				contextView["analysisArtifact"] = map[string]any{
					"agentRunId":     run.ID,
					"capabilityId":   run.CapabilityID,
					"providerId":     run.ProviderID,
					"providerKind":   run.ProviderKind,
					"status":         run.Status,
					"queued":         true,
					"artifactStored": false,
					"runtime":        "agent_runtime_claim_callback",
				}
				related["agentRunId"] = run.ID
				related["agentProviderId"] = run.ProviderID
				related["agentRunStatus"] = run.Status
			}
		} else {
			run, err := s.copilot.RecordGatewayAnalysisArtifact(ctx, principal, artifactInput)
			if err != nil {
				contextView["analysisArtifactError"] = err.Error()
				nextChecks = append(nextChecks, "Retry analysis artifact persistence after the AI Workbench runtime is available.")
				contextView["nextChecks"] = nextChecks
				related["analysisArtifactError"] = err.Error()
			} else {
				contextView["analysisArtifact"] = map[string]any{
					"agentRunId":     run.ID,
					"capabilityId":   run.CapabilityID,
					"status":         run.Status,
					"artifactCount":  len(run.AnalysisArtifacts),
					"artifactKind":   firstAnalysisArtifactKind(run.AnalysisArtifacts),
					"artifactRunId":  firstAnalysisArtifactRunID(run.AnalysisArtifacts),
					"artifactTitle":  firstAnalysisArtifactTitle(run.AnalysisArtifacts),
					"artifactStored": len(run.AnalysisArtifacts) > 0,
				}
				related["agentRunId"] = run.ID
				related["analysisArtifactCount"] = len(run.AnalysisArtifacts)
			}
		}
	} else {
		contextView["analysisArtifact"] = map[string]any{
			"artifactStored": false,
			"reason":         "AI Workbench artifact recorder is not configured",
		}
	}
	return contextView, related, nil
}

type releaseFailureDiagnosisRequest struct {
	ApplicationID            string
	ApplicationEnvironmentID string
	ReleaseBundleID          string
	ExecutionTaskID          string
	ClusterID                string
	Namespace                string
	WorkloadKind             string
	WorkloadName             string
	PodName                  string
	Container                string
	AgentProviderID          string
	DeepAnalysis             bool
	TimeoutSeconds           int
}

func buildReleaseFailureArtifactInput(req releaseFailureDiagnosisRequest, input map[string]any, contextView map[string]any) domaincopilot.GatewayAnalysisArtifactInput {
	delivery := mapValue(contextView["delivery"])
	runtime := mapValue(contextView["runtime"])
	nextChecks := stringSliceValue(contextView["nextChecks"])
	evidence := releaseFailureEvidence(req, delivery, runtime)
	recommendations := normalizeStringSlice(append([]string{
		"Review the persisted Gateway evidence before attempting any rollback, restart, or redeploy action.",
		"Use delivery.release_context.diff or delivery.rollback.context for read-only release comparison before triggering a mutation.",
	}, nextChecks...))
	hypotheses := releaseFailureHypotheses(evidence, recommendations)
	scope := domaincopilot.SessionScope{
		ClusterID: req.ClusterID,
		Namespace: req.Namespace,
		Workload:  firstNonEmpty(req.WorkloadName, req.PodName),
	}
	output := map[string]any{
		"summary":             contextView["summary"],
		"scope":               contextView["scope"],
		"evidenceSummary":     artifactEvidenceSnapshot(delivery, runtime),
		"nextChecks":          nextChecks,
		"recommendationCount": len(recommendations),
	}
	return domaincopilot.GatewayAnalysisArtifactInput{
		CapabilityID:    "delivery_failure",
		Title:           releaseFailureArtifactTitle(req),
		Summary:         releaseFailureArtifactSummary(req, evidence),
		SkillIDs:        []string{"delivery-tester", "k8s-sre"},
		Scope:           scope,
		Input:           sanitizeGatewayMap(input),
		Output:          sanitizeGatewayMap(output),
		Evidence:        evidence,
		Hypotheses:      hypotheses,
		Recommendations: recommendations,
		ToolExecutions: []domaincopilot.ToolExecution{{
			ID:        "gateway:" + uuid.NewString(),
			AdapterID: "platform-native.v1",
			ToolName:  "diagnosis.release_failure.analyze",
			Status:    "completed",
			Summary:   "Collected release failure evidence through AI Gateway application services.",
			Input: map[string]any{
				"applicationId":            req.ApplicationID,
				"applicationEnvironmentId": req.ApplicationEnvironmentID,
				"releaseBundleId":          req.ReleaseBundleID,
				"executionTaskId":          req.ExecutionTaskID,
				"clusterId":                req.ClusterID,
				"namespace":                req.Namespace,
				"workloadKind":             req.WorkloadKind,
				"workloadName":             req.WorkloadName,
				"podName":                  req.PodName,
			},
			Output:    artifactEvidenceSnapshot(delivery, runtime),
			StartedAt: time.Now().UTC(),
		}},
		Graph: buildReleaseFailureGraph(scope, req, evidence),
		DataSourceSnapshot: map[string]any{
			"source":                   "ai-gateway",
			"toolName":                 "diagnosis.release_failure.analyze",
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
			"clusterId":                req.ClusterID,
			"namespace":                req.Namespace,
			"workloadKind":             req.WorkloadKind,
			"workloadName":             req.WorkloadName,
			"podName":                  req.PodName,
			"agentProviderId":          req.AgentProviderID,
			"deepAnalysis":             req.DeepAnalysis,
			"redactionBoundary":        "gateway",
			"rawLogsPersisted":         false,
			"deliveryEvidence":         artifactDeliverySnapshot(delivery),
			"runtimeEvidence":          artifactRuntimeSnapshot(runtime),
		},
	}
}

func releaseFailureArtifactTitle(req releaseFailureDiagnosisRequest) string {
	target := firstNonEmpty(req.WorkloadName, req.PodName, req.ExecutionTaskID, req.ReleaseBundleID, req.ApplicationID, "release failure")
	return "Delivery failure diagnosis: " + target
}

func releaseFailureArtifactSummary(req releaseFailureDiagnosisRequest, evidence []domaincopilot.RootCauseEvidence) string {
	parts := []string{"Gateway collected a read-only delivery failure diagnosis artifact"}
	if target := firstNonEmpty(req.ApplicationID, req.ApplicationEnvironmentID, req.ReleaseBundleID, req.ExecutionTaskID); target != "" {
		parts = append(parts, "for "+target)
	}
	if runtime := firstNonEmpty(req.ClusterID, req.Namespace, req.WorkloadName, req.PodName); runtime != "" {
		parts = append(parts, "with runtime scope "+runtime)
	}
	parts = append(parts, fmt.Sprintf("and %d evidence summaries.", len(evidence)))
	return strings.Join(parts, " ")
}

func releaseFailureEvidence(req releaseFailureDiagnosisRequest, delivery, runtime map[string]any) []domaincopilot.RootCauseEvidence {
	items := make([]domaincopilot.RootCauseEvidence, 0)
	if count := intFromAny(delivery["executionLogCount"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "delivery:execution-logs:" + firstNonEmpty(req.ExecutionTaskID, "current"),
			Kind:      "delivery.execution_logs",
			Title:     "Delivery execution logs",
			Summary:   fmt.Sprintf("%d redacted execution log entries were collected through delivery service.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"executionTaskId": req.ExecutionTaskID,
				"logCount":        count,
				"rawLogsStored":   false,
			},
		})
	}
	if count := intFromAny(delivery["executionTaskCount"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "delivery:execution-tasks:" + firstNonEmpty(req.ReleaseBundleID, req.ApplicationID, "current"),
			Kind:      "delivery.execution_tasks",
			Title:     "Related execution tasks",
			Summary:   fmt.Sprintf("%d execution task summaries matched the release failure scope.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"applicationId":            req.ApplicationID,
				"applicationEnvironmentId": req.ApplicationEnvironmentID,
				"releaseBundleId":          req.ReleaseBundleID,
				"executionTaskCount":       count,
			},
		})
	}
	if count := intFromAny(delivery["releaseBundleArtifactCount"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "delivery:release-bundle-artifacts:" + firstNonEmpty(req.ReleaseBundleID, "current"),
			Kind:      "delivery.release_bundle_artifacts",
			Title:     "Release bundle artifacts",
			Summary:   fmt.Sprintf("%d release bundle artifact summaries were collected.", count),
			Severity:  "info",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"releaseBundleId": req.ReleaseBundleID,
				"artifactCount":   count,
			},
		})
	}
	if count := sliceLen(runtime["pods"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "runtime:pods:" + firstNonEmpty(req.PodName, req.WorkloadName, "selected"),
			Kind:      "k8s.pods",
			Title:     "Runtime pods",
			Summary:   fmt.Sprintf("%d pod summaries matched the diagnosis scope.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"podName":      req.PodName,
				"workloadName": req.WorkloadName,
				"podCount":     count,
			},
		})
	}
	if count := sliceLen(runtime["deployments"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "runtime:deployments:" + firstNonEmpty(req.WorkloadName, "selected"),
			Kind:      "k8s.deployments",
			Title:     "Runtime deployments",
			Summary:   fmt.Sprintf("%d deployment summaries matched the diagnosis scope.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"workloadName":    req.WorkloadName,
				"deploymentCount": count,
			},
		})
	}
	if count := sliceLen(runtime["services"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:         "runtime:services:" + firstNonEmpty(req.Namespace, req.ClusterID, "selected"),
			Kind:       "k8s.services",
			Title:      "Runtime services",
			Summary:    fmt.Sprintf("%d service summaries were collected for backend correlation.", count),
			Severity:   "info",
			ClusterID:  req.ClusterID,
			Namespace:  req.Namespace,
			Attributes: map[string]any{"serviceCount": count},
		})
	}
	if count := sliceLen(runtime["events"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "runtime:events:" + firstNonEmpty(req.PodName, req.WorkloadName, "selected"),
			Kind:      "k8s.events",
			Title:     "Runtime events",
			Summary:   fmt.Sprintf("%d Kubernetes event summaries matched the diagnosis scope.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"eventCount":   count,
				"podName":      req.PodName,
				"workloadName": req.WorkloadName,
			},
		})
	}
	if podLogs, ok := runtime["podLogs"].(domainresource.PodLogsView); ok && strings.TrimSpace(podLogs.Content) != "" {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "runtime:pod-logs:" + firstNonEmpty(req.PodName, podLogs.PodName, "selected"),
			Kind:      "k8s.pod_logs",
			Title:     "Runtime pod logs",
			Summary:   fmt.Sprintf("Redacted pod log sample was collected for %s/%s.", podLogs.Namespace, podLogs.PodName),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"podName":       podLogs.PodName,
				"container":     podLogs.Container,
				"contentBytes":  podLogs.ContentBytes,
				"truncated":     podLogs.Truncated,
				"rawLogsStored": false,
			},
		})
	}
	for key, value := range delivery {
		if !strings.HasSuffix(key, "Error") || strings.TrimSpace(fmt.Sprint(value)) == "" {
			continue
		}
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:         "delivery:error:" + strings.TrimSuffix(key, "Error"),
			Kind:       "delivery.error",
			Title:      key,
			Summary:    redactSensitiveText(fmt.Sprint(value)),
			Severity:   "warning",
			ClusterID:  req.ClusterID,
			Namespace:  req.Namespace,
			Attributes: map[string]any{"source": "delivery", "field": key},
		})
	}
	for key, value := range runtime {
		if !strings.HasSuffix(key, "Error") || strings.TrimSpace(fmt.Sprint(value)) == "" {
			continue
		}
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:         "runtime:error:" + strings.TrimSuffix(key, "Error"),
			Kind:       "k8s.error",
			Title:      key,
			Summary:    redactSensitiveText(fmt.Sprint(value)),
			Severity:   "warning",
			ClusterID:  req.ClusterID,
			Namespace:  req.Namespace,
			Attributes: map[string]any{"source": "runtime", "field": key},
		})
	}
	if len(items) == 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "gateway:diagnosis-context",
			Kind:      "gateway.context",
			Title:     "Gateway diagnosis context",
			Summary:   "Gateway completed the release failure diagnosis request, but no concrete delivery or runtime evidence summaries were available.",
			Severity:  "info",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"applicationId":   req.ApplicationID,
				"releaseBundleId": req.ReleaseBundleID,
				"executionTaskId": req.ExecutionTaskID,
			},
		})
	}
	return items
}

func releaseFailureHypotheses(evidence []domaincopilot.RootCauseEvidence, recommendations []string) []domaincopilot.RootCauseHypothesis {
	evidenceIDs := make([]string, 0, len(evidence))
	hasDeliveryFailure := false
	hasRuntimeSignal := false
	for _, item := range evidence {
		evidenceIDs = append(evidenceIDs, item.ID)
		if strings.HasPrefix(item.Kind, "delivery.") {
			hasDeliveryFailure = true
		}
		if strings.HasPrefix(item.Kind, "k8s.") {
			hasRuntimeSignal = true
		}
	}
	if hasDeliveryFailure && hasRuntimeSignal {
		return []domaincopilot.RootCauseHypothesis{{
			ID:              "hypothesis:release-runtime-correlation",
			Title:           "Delivery failure correlates with runtime evidence",
			Summary:         "Both delivery control-plane evidence and Kubernetes runtime evidence were collected for the same release scope.",
			Confidence:      70,
			EvidenceIDs:     evidenceIDs,
			Recommendations: recommendations,
		}}
	}
	if hasDeliveryFailure {
		return []domaincopilot.RootCauseHypothesis{{
			ID:              "hypothesis:delivery-control-plane",
			Title:           "Delivery control-plane failure is the primary signal",
			Summary:         "Delivery task, bundle, artifact, or log summaries are available, but runtime evidence is missing or inconclusive.",
			Confidence:      60,
			EvidenceIDs:     evidenceIDs,
			Recommendations: recommendations,
		}}
	}
	if hasRuntimeSignal {
		return []domaincopilot.RootCauseHypothesis{{
			ID:              "hypothesis:runtime-state",
			Title:           "Runtime state is the primary signal",
			Summary:         "Kubernetes runtime summaries are available, but delivery control-plane evidence is missing or inconclusive.",
			Confidence:      55,
			EvidenceIDs:     evidenceIDs,
			Recommendations: recommendations,
		}}
	}
	return []domaincopilot.RootCauseHypothesis{{
		ID:              "hypothesis:insufficient-evidence",
		Title:           "Insufficient evidence",
		Summary:         "The Gateway request completed without enough evidence to identify a likely release failure source.",
		Confidence:      30,
		EvidenceIDs:     evidenceIDs,
		Recommendations: recommendations,
	}}
}

func buildReleaseFailureGraph(scope domaincopilot.SessionScope, req releaseFailureDiagnosisRequest, evidence []domaincopilot.RootCauseEvidence) *domaincopilot.AnalysisGraph {
	rootID := "scope:" + firstNonEmpty(scope.Workload, scope.Namespace, scope.ClusterID, req.ApplicationID, "release-failure")
	nodes := []domaincopilot.AnalysisGraphNode{{
		ID:         rootID,
		Kind:       "scope",
		Title:      firstNonEmpty(scope.Workload, scope.Namespace, scope.ClusterID, req.ApplicationID, "release failure"),
		Subtitle:   strings.Join(compactStrings(req.ApplicationID, req.ApplicationEnvironmentID, req.ReleaseBundleID, req.ExecutionTaskID, req.ClusterID, req.Namespace), " / "),
		SourceRefs: []string{"ai-gateway"},
		Attributes: map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
			"clusterId":                req.ClusterID,
			"namespace":                req.Namespace,
			"workloadName":             req.WorkloadName,
			"podName":                  req.PodName,
		},
	}}
	edges := make([]domaincopilot.AnalysisGraphEdge, 0, len(evidence))
	for _, item := range evidence {
		nodeID := "evidence:" + item.ID
		nodes = append(nodes, domaincopilot.AnalysisGraphNode{
			ID:          nodeID,
			Kind:        item.Kind,
			Title:       item.Title,
			Subtitle:    item.Summary,
			Severity:    item.Severity,
			EvidenceIDs: []string{item.ID},
			SourceRefs:  []string{"ai-gateway"},
			Attributes:  item.Attributes,
		})
		edges = append(edges, domaincopilot.AnalysisGraphEdge{
			ID:          rootID + "->" + nodeID,
			Source:      rootID,
			Target:      nodeID,
			Relation:    "uses",
			Severity:    item.Severity,
			EvidenceIDs: []string{item.ID},
		})
	}
	return &domaincopilot.AnalysisGraph{Layout: "LR", FocusNodeID: rootID, Nodes: nodes, Edges: edges}
}

func artifactEvidenceSnapshot(delivery, runtime map[string]any) map[string]any {
	return map[string]any{
		"delivery": artifactDeliverySnapshot(delivery),
		"runtime":  artifactRuntimeSnapshot(runtime),
	}
}

func artifactDeliverySnapshot(delivery map[string]any) map[string]any {
	return map[string]any{
		"executionLogCount":           intFromAny(delivery["executionLogCount"]),
		"executionTaskCount":          intFromAny(delivery["executionTaskCount"]),
		"releaseBundleArtifactCount":  intFromAny(delivery["releaseBundleArtifactCount"]),
		"executionLogsError":          redactSensitiveText(strings.TrimSpace(fmt.Sprint(delivery["executionLogsError"]))),
		"executionTasksError":         redactSensitiveText(strings.TrimSpace(fmt.Sprint(delivery["executionTasksError"]))),
		"releaseBundleArtifactsError": redactSensitiveText(strings.TrimSpace(fmt.Sprint(delivery["releaseBundleArtifactsError"]))),
	}
}

func artifactRuntimeSnapshot(runtime map[string]any) map[string]any {
	podLogBytes := 0
	if podLogs, ok := runtime["podLogs"].(domainresource.PodLogsView); ok {
		podLogBytes = int(podLogs.ContentBytes)
	}
	return map[string]any{
		"podCount":         sliceLen(runtime["pods"]),
		"deploymentCount":  sliceLen(runtime["deployments"]),
		"serviceCount":     sliceLen(runtime["services"]),
		"eventCount":       sliceLen(runtime["events"]),
		"podLogBytes":      podLogBytes,
		"podsError":        redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["podsError"]))),
		"deploymentsError": redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["deploymentsError"]))),
		"servicesError":    redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["servicesError"]))),
		"eventsError":      redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["eventsError"]))),
		"podLogsError":     redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["podLogsError"]))),
	}
}

func firstAnalysisArtifactKind(items []domaincopilot.AnalysisArtifact) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].Kind
}

func firstAnalysisArtifactRunID(items []domaincopilot.AnalysisArtifact) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].RunID
}

func firstAnalysisArtifactTitle(items []domaincopilot.AnalysisArtifact) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].Title
}

func (s *Service) hasRuntimePermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) (bool, error) {
	if s.permissions == nil {
		return false, nil
	}
	return s.permissions.HasPermission(ctx, principal, permissionKey)
}

func (s *Service) ListPersonalAccessTokens(ctx context.Context, principal domainidentity.Principal, req domainaigateway.PersonalAccessTokenListRequest) ([]domainaigateway.PersonalAccessToken, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	userID := strings.TrimSpace(req.UserID)
	if scope == "all" || userID != "" {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
			return nil, err
		}
		items, err := s.repo.ListAllPersonalAccessTokens(ctx)
		if err != nil {
			return nil, err
		}
		if userID == "" {
			return items, nil
		}
		filtered := make([]domainaigateway.PersonalAccessToken, 0)
		for _, item := range items {
			if item.UserID == userID {
				filtered = append(filtered, item)
			}
		}
		return filtered, nil
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayView); err != nil {
		return nil, err
	}
	return s.repo.ListPersonalAccessTokens(ctx, principal.UserID)
}

func (s *Service) CreatePersonalAccessToken(ctx context.Context, principal domainidentity.Principal, input domainaigateway.PersonalAccessTokenInput) (domainaigateway.CreatedPersonalAccessToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	if s.repo == nil {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: token name is required", apperrors.ErrInvalidArgument)
	}
	permissionKeys, err := s.normalizeRequestedPermissionKeys(ctx, principal, input.PermissionKeys)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	value, prefix, err := generateOpaqueToken(domainaigateway.PersonalAccessTokenPrefix)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	now := time.Now().UTC()
	item := domainaigateway.PersonalAccessToken{
		ID:             uuid.NewString(),
		UserID:         principal.UserID,
		Name:           name,
		TokenHash:      domainaigateway.HashToken(value),
		TokenPrefix:    prefix,
		Scopes:         normalizeStringSlice(input.Scopes),
		PermissionKeys: permissionKeys,
		Metadata:       emptyMap(input.Metadata),
		ExpiresAt:      input.ExpiresAt,
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := s.repo.CreatePersonalAccessToken(ctx, item)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.personal_token.create", "success", "created personal access token", map[string]any{
		"tokenId":        created.ID,
		"tokenPrefix":    created.TokenPrefix,
		"permissionKeys": created.PermissionKeys,
		"expiresAt":      created.ExpiresAt,
	})
	return domainaigateway.CreatedPersonalAccessToken{Token: created, Value: value}, nil
}

func (s *Service) RevokePersonalAccessToken(ctx context.Context, principal domainidentity.Principal, tokenID string) error {
	if s.repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return fmt.Errorf("%w: token ID is required", apperrors.ErrInvalidArgument)
	}
	hasManage, err := s.hasRuntimePermission(ctx, principal, appaccess.PermAIGatewayManage)
	if err != nil {
		return err
	}
	ownerID := principal.UserID
	if hasManage {
		items, listErr := s.repo.ListAllPersonalAccessTokens(ctx)
		if listErr != nil {
			return listErr
		}
		ownerID = ""
		for _, item := range items {
			if item.ID == tokenID {
				ownerID = item.UserID
				break
			}
		}
		if ownerID == "" {
			return apperrors.ErrNotFound
		}
	} else if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return err
	}
	if err := s.repo.RevokePersonalAccessToken(ctx, ownerID, tokenID); err != nil {
		return err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.personal_token.revoke", "success", "revoked personal access token", map[string]any{
		"tokenId":      tokenID,
		"tokenOwnerId": ownerID,
	})
	return nil
}

func (s *Service) RotatePersonalAccessToken(ctx context.Context, principal domainidentity.Principal, tokenID string, input domainaigateway.TokenRotationInput) (domainaigateway.CreatedPersonalAccessToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	if s.repo == nil {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: token ID is required", apperrors.ErrInvalidArgument)
	}
	items, err := s.repo.ListPersonalAccessTokens(ctx, principal.UserID)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	var previous domainaigateway.PersonalAccessToken
	for _, item := range items {
		if item.ID == tokenID {
			previous = item
			break
		}
	}
	if previous.ID == "" {
		return domainaigateway.CreatedPersonalAccessToken{}, apperrors.ErrNotFound
	}
	if previous.RevokedAt != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: token is revoked", apperrors.ErrInvalidArgument)
	}
	permissionKeys := normalizeStringSlice(previous.PermissionKeys)
	if len(permissionKeys) > 0 {
		permissionKeys, err = s.normalizeRequestedPermissionKeys(ctx, principal, permissionKeys)
		if err != nil {
			return domainaigateway.CreatedPersonalAccessToken{}, err
		}
	}
	expiresAt, err := rotatedTokenExpiresAt(input.ExpiresAt, previous.ExpiresAt, time.Now().UTC())
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	value, prefix, err := generateOpaqueToken(domainaigateway.PersonalAccessTokenPrefix)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	now := time.Now().UTC()
	replacement := domainaigateway.PersonalAccessToken{
		ID:             uuid.NewString(),
		UserID:         principal.UserID,
		Name:           previous.Name,
		TokenHash:      domainaigateway.HashToken(value),
		TokenPrefix:    prefix,
		Scopes:         normalizeStringSlice(previous.Scopes),
		PermissionKeys: permissionKeys,
		Metadata:       copyMap(previous.Metadata),
		ExpiresAt:      expiresAt,
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := s.repo.CreatePersonalAccessToken(ctx, replacement)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	if err := s.repo.RevokePersonalAccessToken(ctx, principal.UserID, previous.ID); err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.personal_token.rotate", "success", "rotated personal access token", map[string]any{
		"previousTokenId": previous.ID,
		"tokenId":         created.ID,
		"tokenPrefix":     created.TokenPrefix,
		"permissionKeys":  created.PermissionKeys,
		"expiresAt":       created.ExpiresAt,
	})
	return domainaigateway.CreatedPersonalAccessToken{Token: created, Value: value}, nil
}

func (s *Service) ListServiceAccounts(ctx context.Context, principal domainidentity.Principal) ([]domainaigateway.ServiceAccount, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	return s.repo.ListServiceAccounts(ctx)
}

func (s *Service) CreateServiceAccount(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ServiceAccountInput) (domainaigateway.ServiceAccount, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	if s.repo == nil {
		return domainaigateway.ServiceAccount{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.ServiceAccount{}, fmt.Errorf("%w: service account name is required", apperrors.ErrInvalidArgument)
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	now := time.Now().UTC()
	item := domainaigateway.ServiceAccount{
		ID:            id,
		Name:          name,
		Description:   strings.TrimSpace(input.Description),
		Status:        status,
		OwnerUserID:   strings.TrimSpace(input.OwnerUserID),
		RoleIDs:       normalizeStringSlice(input.RoleIDs),
		TeamIDs:       normalizeStringSlice(input.TeamIDs),
		ScopeGrantIDs: normalizeStringSlice(input.ScopeGrantIDs),
		Metadata:      emptyMap(input.Metadata),
		CreatedBy:     principal.UserID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	created, err := s.repo.CreateServiceAccount(ctx, item)
	if err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_account.create", "success", "created service account", map[string]any{"serviceAccountId": created.ID, "roleIds": created.RoleIDs})
	return created, nil
}

func (s *Service) ListServiceAccountTokens(ctx context.Context, principal domainidentity.Principal) ([]domainaigateway.ServiceAccountToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	return s.repo.ListAllServiceAccountTokens(ctx)
}

func (s *Service) CreateServiceAccountToken(ctx context.Context, principal domainidentity.Principal, serviceAccountID string, input domainaigateway.ServiceAccountTokenInput) (domainaigateway.CreatedServiceAccountToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if s.repo == nil {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	account, err := s.repo.GetServiceAccount(ctx, strings.TrimSpace(serviceAccountID))
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if strings.TrimSpace(account.Status) != "active" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: service account is not active", apperrors.ErrInvalidArgument)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: token name is required", apperrors.ErrInvalidArgument)
	}
	servicePrincipal := domainidentity.Principal{UserID: "service_account:" + account.ID, UserName: account.Name, Roles: account.RoleIDs, Teams: account.TeamIDs}
	permissionKeys, err := s.normalizeRequestedPermissionKeys(ctx, servicePrincipal, input.PermissionKeys)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	value, prefix, err := generateOpaqueToken(domainaigateway.ServiceAccountTokenPrefix)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	now := time.Now().UTC()
	item := domainaigateway.ServiceAccountToken{
		ID:               uuid.NewString(),
		ServiceAccountID: account.ID,
		Name:             name,
		TokenHash:        domainaigateway.HashToken(value),
		TokenPrefix:      prefix,
		Scopes:           normalizeStringSlice(input.Scopes),
		PermissionKeys:   permissionKeys,
		Metadata:         emptyMap(input.Metadata),
		ExpiresAt:        input.ExpiresAt,
		CreatedBy:        principal.UserID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	created, err := s.repo.CreateServiceAccountToken(ctx, item)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_token.create", "success", "created service account token", map[string]any{
		"serviceAccountId": account.ID,
		"tokenId":          created.ID,
		"tokenPrefix":      created.TokenPrefix,
		"permissionKeys":   created.PermissionKeys,
	})
	return domainaigateway.CreatedServiceAccountToken{Token: created, Value: value}, nil
}

func (s *Service) RevokeServiceAccountToken(ctx context.Context, principal domainidentity.Principal, tokenID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return err
	}
	if s.repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.RevokeServiceAccountToken(ctx, strings.TrimSpace(tokenID)); err != nil {
		return err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_token.revoke", "success", "revoked service account token", map[string]any{"tokenId": strings.TrimSpace(tokenID)})
	return nil
}

func (s *Service) RotateServiceAccountToken(ctx context.Context, principal domainidentity.Principal, tokenID string, input domainaigateway.TokenRotationInput) (domainaigateway.CreatedServiceAccountToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if s.repo == nil {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: token ID is required", apperrors.ErrInvalidArgument)
	}
	items, err := s.repo.ListAllServiceAccountTokens(ctx)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	var previous domainaigateway.ServiceAccountToken
	for _, item := range items {
		if item.ID == tokenID {
			previous = item
			break
		}
	}
	if previous.ID == "" {
		return domainaigateway.CreatedServiceAccountToken{}, apperrors.ErrNotFound
	}
	if previous.RevokedAt != nil {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: token is revoked", apperrors.ErrInvalidArgument)
	}
	account, err := s.repo.GetServiceAccount(ctx, previous.ServiceAccountID)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if strings.TrimSpace(account.Status) != "active" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: service account is not active", apperrors.ErrInvalidArgument)
	}
	servicePrincipal := domainidentity.Principal{UserID: "service_account:" + account.ID, UserName: account.Name, Roles: account.RoleIDs, Teams: account.TeamIDs}
	permissionKeys := normalizeStringSlice(previous.PermissionKeys)
	if len(permissionKeys) > 0 {
		permissionKeys, err = s.normalizeRequestedPermissionKeys(ctx, servicePrincipal, permissionKeys)
		if err != nil {
			return domainaigateway.CreatedServiceAccountToken{}, err
		}
	}
	expiresAt, err := rotatedTokenExpiresAt(input.ExpiresAt, previous.ExpiresAt, time.Now().UTC())
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	value, prefix, err := generateOpaqueToken(domainaigateway.ServiceAccountTokenPrefix)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	now := time.Now().UTC()
	replacement := domainaigateway.ServiceAccountToken{
		ID:               uuid.NewString(),
		ServiceAccountID: account.ID,
		Name:             previous.Name,
		TokenHash:        domainaigateway.HashToken(value),
		TokenPrefix:      prefix,
		Scopes:           normalizeStringSlice(previous.Scopes),
		PermissionKeys:   permissionKeys,
		Metadata:         copyMap(previous.Metadata),
		ExpiresAt:        expiresAt,
		CreatedBy:        principal.UserID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	created, err := s.repo.CreateServiceAccountToken(ctx, replacement)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if err := s.repo.RevokeServiceAccountToken(ctx, previous.ID); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_token.rotate", "success", "rotated service account token", map[string]any{
		"serviceAccountId": account.ID,
		"previousTokenId":  previous.ID,
		"tokenId":          created.ID,
		"tokenPrefix":      created.TokenPrefix,
		"permissionKeys":   created.PermissionKeys,
		"expiresAt":        created.ExpiresAt,
	})
	return domainaigateway.CreatedServiceAccountToken{Token: created, Value: value}, nil
}

func (s *Service) ListAIClients(ctx context.Context, principal domainidentity.Principal) ([]domainaigateway.AIClient, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	return s.repo.ListAIClients(ctx)
}

func (s *Service) CreateAIClient(ctx context.Context, principal domainidentity.Principal, input domainaigateway.AIClientInput) (domainaigateway.AIClient, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.AIClient{}, err
	}
	if s.repo == nil {
		return domainaigateway.AIClient{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := buildAIClient(principal, input, "")
	if err != nil {
		return domainaigateway.AIClient{}, err
	}
	created, err := s.repo.CreateAIClient(ctx, item)
	if err != nil {
		return domainaigateway.AIClient{}, err
	}
	approval, approvalErr := s.createAIClientRegistrationApprovalRequest(ctx, principal, created)
	if approvalErr != nil {
		return domainaigateway.AIClient{}, approvalErr
	}
	if approval.ID != "" {
		created.Metadata = mergeAnyMaps(created.Metadata, map[string]any{"registrationApprovalRequestId": approval.ID})
		if updated, err := s.repo.UpdateAIClient(ctx, created); err == nil {
			created = updated
		}
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayAIClient", created.ID, "ai_gateway.ai_client.create", "success", "created AI Gateway client", map[string]any{
		"aiClientId":        created.ID,
		"kind":              created.Kind,
		"status":            created.Status,
		"approvalRequestId": approval.ID,
	})
	return created, nil
}

func (s *Service) UpdateAIClient(ctx context.Context, principal domainidentity.Principal, clientID string, input domainaigateway.AIClientInput) (domainaigateway.AIClient, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.AIClient{}, err
	}
	if s.repo == nil {
		return domainaigateway.AIClient{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := buildAIClient(principal, input, strings.TrimSpace(clientID))
	if err != nil {
		return domainaigateway.AIClient{}, err
	}
	updated, err := s.repo.UpdateAIClient(ctx, item)
	if err != nil {
		return domainaigateway.AIClient{}, err
	}
	approval, approvalErr := s.createAIClientRegistrationApprovalRequest(ctx, principal, updated)
	if approvalErr != nil {
		return domainaigateway.AIClient{}, approvalErr
	}
	if approval.ID != "" {
		updated.Metadata = mergeAnyMaps(updated.Metadata, map[string]any{"registrationApprovalRequestId": approval.ID})
		if next, err := s.repo.UpdateAIClient(ctx, updated); err == nil {
			updated = next
		}
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayAIClient", updated.ID, "ai_gateway.ai_client.update", "success", "updated AI Gateway client", map[string]any{
		"aiClientId":        updated.ID,
		"kind":              updated.Kind,
		"status":            updated.Status,
		"approvalRequestId": approval.ID,
	})
	return updated, nil
}

func (s *Service) createAIClientRegistrationApprovalRequest(ctx context.Context, principal domainidentity.Principal, client domainaigateway.AIClient) (domainaigateway.ApprovalRequest, error) {
	if s == nil || s.repo == nil || !aiClientRegistrationApprovalPending(client) {
		return domainaigateway.ApprovalRequest{}, nil
	}
	existingID := strings.TrimSpace(firstMapString(client.Metadata, "registrationApprovalRequestId", "approvalRequestId"))
	if existingID != "" {
		return domainaigateway.ApprovalRequest{ID: existingID}, nil
	}
	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour)
	actorType, actorID := gatewaySubject(principal)
	request := domainaigateway.ApprovalRequest{
		ID:               uuid.NewString(),
		Status:           "pending",
		Strategy:         string(gatewayRiskRequireApproval),
		ActorType:        actorType,
		ActorID:          actorID,
		ActorName:        principal.UserName,
		ActorRoles:       append([]string(nil), principal.Roles...),
		ActorTeams:       append([]string(nil), principal.Teams...),
		AIClientID:       client.ID,
		AIClientName:     client.Name,
		ToolName:         "ai_gateway.ai_client.registration",
		RiskLevel:        domainaigateway.RiskLevelHigh,
		RequiresApproval: true,
		ResourceScope:    map[string]any{"aiClientId": client.ID},
		ToolInput: map[string]any{
			"aiClientId":         client.ID,
			"name":               client.Name,
			"kind":               client.Kind,
			"status":             client.Status,
			"redirectUriCount":   len(client.RedirectURIs),
			"allowedOriginCount": len(client.AllowedOrigins),
		},
		RelatedIDs: map[string]any{
			"aiClientId": client.ID,
		},
		Output:    map[string]any{},
		Summary:   "AI client registration is waiting for approval",
		ExpiresAt: &expiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}
	created, err := s.repo.CreateApprovalRequest(ctx, request)
	if err != nil {
		return domainaigateway.ApprovalRequest{}, err
	}
	_ = s.recordApprovalDecisionAudit(ctx, principal, created, "ai_gateway.ai_client.registration.request", "pending", "AI client registration approval requested")
	return created, nil
}

func (s *Service) ListToolGrants(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.ToolGrantFilter) ([]domainaigateway.ToolGrant, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	filter.SubjectType = normalizeSubjectType(filter.SubjectType)
	filter.SubjectID = strings.TrimSpace(filter.SubjectID)
	filter.AIClientID = strings.TrimSpace(filter.AIClientID)
	filter.ToolName = strings.TrimSpace(filter.ToolName)
	return s.repo.ListToolGrants(ctx, filter)
}

func (s *Service) CreateToolGrant(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolGrantInput) (domainaigateway.ToolGrant, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.ToolGrant{}, err
	}
	if s.repo == nil {
		return domainaigateway.ToolGrant{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := s.buildToolGrant(principal, input)
	if err != nil {
		return domainaigateway.ToolGrant{}, err
	}
	created, err := s.repo.CreateToolGrant(ctx, item)
	if err != nil {
		return domainaigateway.ToolGrant{}, err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayToolGrant", created.ID, "ai_gateway.tool_grant.create", "success", "created MCP tool grant", map[string]any{
		"grantId":     created.ID,
		"subjectType": created.SubjectType,
		"subjectId":   created.SubjectID,
		"aiClientId":  created.AIClientID,
		"toolName":    created.ToolName,
		"effect":      created.Effect,
		"riskLevel":   created.RiskLevel,
	})
	return created, nil
}

func (s *Service) DeleteToolGrant(ctx context.Context, principal domainidentity.Principal, grantID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return err
	}
	if s.repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return fmt.Errorf("%w: grant id is required", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.DeleteToolGrant(ctx, grantID); err != nil {
		return err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayToolGrant", grantID, "ai_gateway.tool_grant.delete", "success", "deleted MCP tool grant", map[string]any{"grantId": grantID})
	return nil
}

func (s *Service) ListAccessPolicies(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	filter.SubjectType = normalizeSubjectType(filter.SubjectType)
	filter.SubjectID = strings.TrimSpace(filter.SubjectID)
	filter.AIClientID = strings.TrimSpace(filter.AIClientID)
	filter.Effect = strings.ToLower(strings.TrimSpace(filter.Effect))
	if filter.Effect != "" && !validStatus(filter.Effect, "allow", "deny") {
		return nil, fmt.Errorf("%w: effect must be allow or deny", apperrors.ErrInvalidArgument)
	}
	return s.repo.ListAccessPolicies(ctx, filter)
}

func (s *Service) CreateAccessPolicy(ctx context.Context, principal domainidentity.Principal, input domainaigateway.AccessPolicyInput) (domainaigateway.AccessPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	if s.repo == nil {
		return domainaigateway.AccessPolicy{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := buildAccessPolicy(principal, input, "")
	if err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	created, err := s.repo.CreateAccessPolicy(ctx, item)
	if err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayAccessPolicy", created.ID, "ai_gateway.access_policy.create", "success", "created AI Gateway access policy", map[string]any{
		"policyId":     created.ID,
		"subjectType":  created.SubjectType,
		"subjectId":    created.SubjectID,
		"aiClientId":   created.AIClientID,
		"effect":       created.Effect,
		"toolPatterns": created.ToolPatterns,
		"skillIds":     created.SkillIDs,
		"riskLevels":   created.RiskLevels,
	})
	return created, nil
}

func (s *Service) UpdateAccessPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainaigateway.AccessPolicyInput) (domainaigateway.AccessPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	if s.repo == nil {
		return domainaigateway.AccessPolicy{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := buildAccessPolicy(principal, input, strings.TrimSpace(policyID))
	if err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	updated, err := s.repo.UpdateAccessPolicy(ctx, item)
	if err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayAccessPolicy", updated.ID, "ai_gateway.access_policy.update", "success", "updated AI Gateway access policy", map[string]any{
		"policyId":     updated.ID,
		"subjectType":  updated.SubjectType,
		"subjectId":    updated.SubjectID,
		"aiClientId":   updated.AIClientID,
		"effect":       updated.Effect,
		"toolPatterns": updated.ToolPatterns,
		"skillIds":     updated.SkillIDs,
		"riskLevels":   updated.RiskLevels,
	})
	return updated, nil
}

func (s *Service) DeleteAccessPolicy(ctx context.Context, principal domainidentity.Principal, policyID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return err
	}
	if s.repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return fmt.Errorf("%w: policy id is required", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.DeleteAccessPolicy(ctx, policyID); err != nil {
		return err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayAccessPolicy", policyID, "ai_gateway.access_policy.delete", "success", "deleted AI Gateway access policy", map[string]any{"policyId": policyID})
	return nil
}

func (s *Service) ListSkillBindings(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	filter.SubjectType = normalizeSubjectType(filter.SubjectType)
	filter.SubjectID = strings.TrimSpace(filter.SubjectID)
	filter.AIClientID = strings.TrimSpace(filter.AIClientID)
	filter.SkillID = strings.TrimSpace(filter.SkillID)
	return s.repo.ListSkillBindings(ctx, filter)
}

func (s *Service) CreateSkillBinding(ctx context.Context, principal domainidentity.Principal, input domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	if s.repo == nil {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := s.buildSkillBinding(principal, input, "")
	if err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	created, err := s.repo.CreateSkillBinding(ctx, item)
	if err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewaySkillBinding", created.ID, "ai_gateway.skill_binding.create", "success", "created AI Gateway skill binding", map[string]any{
		"bindingId":      created.ID,
		"subjectType":    created.SubjectType,
		"subjectId":      created.SubjectID,
		"aiClientId":     created.AIClientID,
		"skillId":        created.SkillID,
		"capabilityRefs": created.CapabilityRefs,
	})
	return created, nil
}

func (s *Service) UpdateSkillBinding(ctx context.Context, principal domainidentity.Principal, bindingID string, input domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	if s.repo == nil {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := s.buildSkillBinding(principal, input, strings.TrimSpace(bindingID))
	if err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	updated, err := s.repo.UpdateSkillBinding(ctx, item)
	if err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewaySkillBinding", updated.ID, "ai_gateway.skill_binding.update", "success", "updated AI Gateway skill binding", map[string]any{
		"bindingId":      updated.ID,
		"subjectType":    updated.SubjectType,
		"subjectId":      updated.SubjectID,
		"aiClientId":     updated.AIClientID,
		"skillId":        updated.SkillID,
		"capabilityRefs": updated.CapabilityRefs,
	})
	return updated, nil
}

func (s *Service) DeleteSkillBinding(ctx context.Context, principal domainidentity.Principal, bindingID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return err
	}
	if s.repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return fmt.Errorf("%w: binding id is required", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.DeleteSkillBinding(ctx, bindingID); err != nil {
		return err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewaySkillBinding", bindingID, "ai_gateway.skill_binding.delete", "success", "deleted AI Gateway skill binding", map[string]any{"bindingId": bindingID})
	return nil
}

func (s *Service) ListAuditLogs(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.AuditLogFilter) ([]domainaigateway.AuditLog, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	filter.ActorType = normalizeSubjectType(filter.ActorType)
	filter.ActorID = strings.TrimSpace(filter.ActorID)
	filter.AIClientID = strings.TrimSpace(filter.AIClientID)
	filter.SkillID = strings.TrimSpace(filter.SkillID)
	filter.ToolName = strings.TrimSpace(filter.ToolName)
	filter.ApprovalRequestID = strings.TrimSpace(filter.ApprovalRequestID)
	filter.Result = strings.ToLower(strings.TrimSpace(filter.Result))
	filter.Action = strings.TrimSpace(filter.Action)
	filter.RiskLevel = domainaigateway.RiskLevel(strings.TrimSpace(string(filter.RiskLevel)))
	if filter.ActorType != "" && !validStatus(filter.ActorType, "user", "service_account") {
		return nil, fmt.Errorf("%w: actorType must be user or service_account", apperrors.ErrInvalidArgument)
	}
	if filter.RiskLevel != "" && !validRiskLevel(filter.RiskLevel) {
		return nil, fmt.Errorf("%w: invalid riskLevel", apperrors.ErrInvalidArgument)
	}
	if filter.Result != "" && !validStatus(filter.Result, "success", "failure", "deny", "pending", "pending_approval", "pending_human_confirm", "dry_run", "approved", "executed", "rejected", "canceled", "timeout") {
		return nil, fmt.Errorf("%w: invalid audit result", apperrors.ErrInvalidArgument)
	}
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	return s.repo.ListAuditLogs(ctx, filter)
}

func (s *Service) GovernanceStatus(ctx context.Context, principal domainidentity.Principal, input domainaigateway.GovernanceStatusRequest) (domainaigateway.GovernanceStatus, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	if s.repo == nil {
		return domainaigateway.GovernanceStatus{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.expirePendingApprovalRequests(ctx); err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	windowHours := input.WindowHours
	if windowHours <= 0 {
		windowHours = 24
	}
	if windowHours > 168 {
		windowHours = 168
	}
	now := time.Now().UTC()
	from := now.Add(-time.Duration(windowHours) * time.Hour)
	personalTokens, err := s.repo.ListAllPersonalAccessTokens(ctx)
	if err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	serviceTokens, err := s.repo.ListAllServiceAccountTokens(ctx)
	if err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	clients, err := s.repo.ListAIClients(ctx)
	if err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	accessPolicies, err := s.repo.ListAccessPolicies(ctx, domainaigateway.AccessPolicyFilter{IncludeDisabled: true})
	if err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	toolGrants, err := s.repo.ListToolGrants(ctx, domainaigateway.ToolGrantFilter{IncludeExpired: true})
	if err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	skillBindings, err := s.repo.ListSkillBindings(ctx, domainaigateway.SkillBindingFilter{IncludeDisabled: true})
	if err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	audits, err := s.repo.ListAuditLogs(ctx, domainaigateway.AuditLogFilter{From: &from, To: &now, Limit: 500})
	if err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	pendingApprovals, err := s.repo.ListApprovalRequests(ctx, domainaigateway.ApprovalRequestFilter{Status: "pending", Limit: 500})
	if err != nil {
		return domainaigateway.GovernanceStatus{}, err
	}
	return s.buildGovernanceStatus(now, windowHours, personalTokens, serviceTokens, clients, accessPolicies, toolGrants, skillBindings, audits, pendingApprovals), nil
}

func (s *Service) ListApprovalRequests(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.expirePendingApprovalRequests(ctx); err != nil {
		return nil, err
	}
	filter.ID = strings.TrimSpace(filter.ID)
	filter.Status = normalizeApprovalRequestStatus(filter.Status)
	filter.ActorType = normalizeSubjectType(filter.ActorType)
	filter.ActorID = strings.TrimSpace(filter.ActorID)
	filter.AIClientID = strings.TrimSpace(filter.AIClientID)
	filter.SkillID = strings.TrimSpace(filter.SkillID)
	filter.ToolName = strings.TrimSpace(filter.ToolName)
	filter.Strategy = strings.TrimSpace(filter.Strategy)
	filter.RiskLevel = domainaigateway.RiskLevel(strings.TrimSpace(string(filter.RiskLevel)))
	if filter.Status != "" && !validApprovalRequestStatus(filter.Status) {
		return nil, fmt.Errorf("%w: invalid approval request status", apperrors.ErrInvalidArgument)
	}
	if filter.ActorType != "" && !validStatus(filter.ActorType, "user", "service_account") {
		return nil, fmt.Errorf("%w: actorType must be user or service_account", apperrors.ErrInvalidArgument)
	}
	if filter.RiskLevel != "" && !validRiskLevel(filter.RiskLevel) {
		return nil, fmt.Errorf("%w: invalid riskLevel", apperrors.ErrInvalidArgument)
	}
	if filter.Strategy != "" {
		strategy, ok := parseGatewayRiskStrategy(filter.Strategy)
		if !ok || !strategy.requiresApprovalRequest() {
			return nil, fmt.Errorf("%w: invalid approval strategy", apperrors.ErrInvalidArgument)
		}
		filter.Strategy = string(strategy)
	}
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	items, err := s.repo.ListApprovalRequests(ctx, filter)
	if err != nil {
		return nil, err
	}
	return enrichApprovalRequests(items), nil
}

func (s *Service) GetApprovalTimeline(ctx context.Context, principal domainidentity.Principal, requestID string) (domainaigateway.ApprovalTimeline, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.ApprovalTimeline{}, err
	}
	if s.repo == nil {
		return domainaigateway.ApprovalTimeline{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.expirePendingApprovalRequests(ctx); err != nil {
		return domainaigateway.ApprovalTimeline{}, err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return domainaigateway.ApprovalTimeline{}, fmt.Errorf("%w: approval request id is required", apperrors.ErrInvalidArgument)
	}
	request, err := s.repo.GetApprovalRequest(ctx, requestID)
	if err != nil {
		return domainaigateway.ApprovalTimeline{}, err
	}
	request = enrichApprovalRequest(request)
	audits, err := s.repo.ListAuditLogs(ctx, domainaigateway.AuditLogFilter{
		ApprovalRequestID: request.ID,
		Limit:             500,
	})
	if err != nil {
		return domainaigateway.ApprovalTimeline{}, err
	}
	return domainaigateway.ApprovalTimeline{
		Request: request,
		Trace:   request.ApprovalTrace,
		Events:  gatewayApprovalTimelineEvents(request, audits),
	}, nil
}

func (s *Service) ApproveApprovalRequest(ctx context.Context, principal domainidentity.Principal, requestID string, input domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	return s.resolveApprovalRequest(ctx, principal, requestID, "approve", input)
}

func (s *Service) RejectApprovalRequest(ctx context.Context, principal domainidentity.Principal, requestID string, input domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	return s.resolveApprovalRequest(ctx, principal, requestID, "reject", input)
}

func (s *Service) CancelApprovalRequest(ctx context.Context, principal domainidentity.Principal, requestID string, input domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	return s.resolveApprovalRequest(ctx, principal, requestID, "cancel", input)
}

func (s *Service) resolveApprovalRequest(ctx context.Context, principal domainidentity.Principal, requestID, action string, input domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	if s.repo == nil {
		return domainaigateway.ApprovalDecisionResult{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.expirePendingApprovalRequests(ctx); err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return domainaigateway.ApprovalDecisionResult{}, fmt.Errorf("%w: approval request id is required", apperrors.ErrInvalidArgument)
	}
	request, err := s.repo.GetApprovalRequest(ctx, requestID)
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	if request.Status != "pending" {
		return domainaigateway.ApprovalDecisionResult{}, fmt.Errorf("%w: approval request is not pending", apperrors.ErrInvalidArgument)
	}
	if request.ExpiresAt != nil && !request.ExpiresAt.After(time.Now().UTC()) {
		if err := s.expireApprovalRequest(ctx, request); err != nil {
			return domainaigateway.ApprovalDecisionResult{}, err
		}
		return domainaigateway.ApprovalDecisionResult{}, fmt.Errorf("%w: approval request has timed out", apperrors.ErrInvalidArgument)
	}
	if err := authorizeGatewayApprovalDecision(principal, request, action, time.Now().UTC()); err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	switch action {
	case "approve":
		return s.approveApprovalRequest(ctx, principal, request, input)
	case "reject", "cancel":
		if isAIClientRegistrationApprovalRequest(request) {
			return s.rejectOrCancelAIClientRegistrationApproval(ctx, principal, request, action, input)
		}
		status := "rejected"
		operationAction := "ai_gateway.approval.reject"
		summary := "AI Gateway approval request rejected"
		if action == "cancel" {
			status = "canceled"
			operationAction = "ai_gateway.approval.cancel"
			summary = "AI Gateway approval request canceled"
		}
		updated, err := s.transitionApprovalRequest(ctx, principal, request, status, summary, input.Comment, request.RelatedIDs, request.Output)
		if err != nil {
			return domainaigateway.ApprovalDecisionResult{}, err
		}
		_ = s.recordApprovalDecisionAudit(ctx, principal, updated, operationAction, status, summary)
		return domainaigateway.ApprovalDecisionResult{Request: updated}, nil
	default:
		return domainaigateway.ApprovalDecisionResult{}, fmt.Errorf("%w: unsupported approval action", apperrors.ErrInvalidArgument)
	}
}

func (s *Service) approveApprovalRequest(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, input domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	if isAIClientRegistrationApprovalRequest(request) {
		return s.approveAIClientRegistrationApproval(ctx, principal, request, input)
	}
	voted, ready, err := s.recordGatewayApprovalVote(ctx, principal, request, input)
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	if !ready {
		return voted, nil
	}
	request.RelatedIDs = voted.Request.RelatedIDs
	approved, err := s.transitionApprovalRequest(ctx, principal, request, "approved", "AI Gateway approval request approved", input.Comment, request.RelatedIDs, request.Output)
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	_ = s.recordApprovalDecisionAudit(ctx, principal, approved, "ai_gateway.approval.approve", "approved", "AI Gateway approval request approved")
	tool, ok := s.toolByName(request.ToolName)
	if !ok {
		failed, failErr := s.finalizeApprovedApprovalRequest(ctx, principal, approved, "failed", "AI Gateway approved tool is no longer available", request.RelatedIDs, map[string]any{"error": "unknown tool"})
		if failErr != nil {
			return domainaigateway.ApprovalDecisionResult{}, failErr
		}
		return domainaigateway.ApprovalDecisionResult{Request: failed}, fmt.Errorf("%w: unknown AI Gateway tool %s", apperrors.ErrInvalidArgument, request.ToolName)
	}
	replayPrincipal := approvalRequestPrincipal(request)
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, replayPrincipal, appaccess.PermAIGatewayInvoke); err != nil {
		return s.failApprovedApprovalRequest(ctx, principal, approved, tool, err)
	}
	if err := s.authorizeTool(ctx, replayPrincipal, tool); err != nil {
		return s.failApprovedApprovalRequest(ctx, principal, approved, tool, err)
	}
	invocationScope := standardGatewayScope(request.ToolInput, nil)
	if _, err := s.authorizeToolGrant(ctx, replayPrincipal, request.AIClientID, tool, invocationScope); err != nil {
		return s.failApprovedApprovalRequest(ctx, principal, approved, tool, err)
	}
	decision, policyInput, redactionSummary, err := s.authorizeAccessPolicy(ctx, replayPrincipal, request.AIClientID, request.SkillID, &tool, invocationScope, request.ToolInput)
	if err != nil {
		return s.failApprovedApprovalRequest(ctx, principal, approved, tool, err)
	}
	request.ToolInput = policyInput
	if decision.Strategy == gatewayRiskDeny || decision.Strategy == gatewayRiskDryRunOnly {
		return s.failApprovedApprovalRequest(ctx, principal, approved, tool, fmt.Errorf("%w: current AI Gateway policy blocks approved request", apperrors.ErrAccessDenied))
	}
	if err := s.authorizeSkillBinding(ctx, replayPrincipal, request.AIClientID, request.SkillID, tool); err != nil {
		return s.failApprovedApprovalRequest(ctx, principal, approved, tool, err)
	}
	request.ToolInput = gatewayApprovalReplayInput(request)
	output, relatedIDs, err := s.invokeGatewayTool(ctx, replayPrincipal, tool, request.ToolInput)
	if err != nil {
		return s.failApprovedApprovalRequest(ctx, principal, approved, tool, err)
	}
	var outputRedactionSummary gatewayRedactionAuditSummary
	output, outputRedactionSummary, err = s.sanitizeToolOutputByAccessPolicy(ctx, replayPrincipal, request.AIClientID, request.SkillID, tool, invocationScope, output)
	redactionSummary.merge(outputRedactionSummary)
	if err != nil {
		return s.failApprovedApprovalRequest(ctx, principal, approved, tool, err)
	}
	relatedIDs = mergeAnyMaps(request.RelatedIDs, relatedIDs)
	relatedIDs["approvalRequestId"] = request.ID
	usageSummary := gatewayProviderUsageSummary(output, relatedIDs)
	audit := map[string]any{
		"riskLevel":         tool.RiskLevel,
		"approvedRequestId": request.ID,
		"approvedBy":        principal.UserID,
	}
	addGatewayRedactionAuditMetadata(audit, redactionSummary)
	addGatewayUsageAuditMetadata(audit, usageSummary)
	invocation := domainaigateway.ToolInvocationResult{
		ToolName:         tool.Name,
		RiskLevel:        tool.RiskLevel,
		RequiresApproval: false,
		Result:           "success",
		Output:           output,
		RelatedIDs:       relatedIDs,
		Audit:            audit,
	}
	updated, err := s.finalizeApprovedApprovalRequest(ctx, principal, approved, "executed", "AI Gateway approval request executed through owning service", relatedIDs, output)
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	_ = s.recordToolAuditWithMetadata(ctx, replayPrincipal, approvalRequestInvocationInput(updated), tool, "success", "invoked approved AI Gateway tool", relatedIDs, redactionSummary, usageSummary)
	_ = s.recordApprovalDecisionAudit(ctx, principal, updated, "ai_gateway.approval.execute", "executed", "AI Gateway approval request executed through owning service")
	return domainaigateway.ApprovalDecisionResult{Request: updated, Invocation: &invocation}, nil
}

func (s *Service) approveAIClientRegistrationApproval(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, input domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	client, err := s.repo.GetAIClient(ctx, request.AIClientID)
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	client.Status = "active"
	client.Metadata = mergeAnyMaps(client.Metadata, map[string]any{
		"registrationApproval":          "approved",
		"registrationApprovalRequestId": request.ID,
		"registrationApprovedBy":        principal.UserID,
		"registrationApprovedAt":        time.Now().UTC().Format(time.RFC3339),
	})
	if _, err := s.repo.UpdateAIClient(ctx, client); err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	approved, err := s.transitionApprovalRequest(ctx, principal, request, "approved", "AI client registration approved", input.Comment, request.RelatedIDs, map[string]any{"aiClientId": client.ID, "status": "active"})
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	updated, err := s.finalizeApprovedApprovalRequest(ctx, principal, approved, "executed", "AI client registration activated", request.RelatedIDs, map[string]any{"aiClientId": client.ID, "status": "active"})
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayAIClient", client.ID, "ai_gateway.ai_client.registration.approve", "success", "approved AI client registration", map[string]any{
		"aiClientId":        client.ID,
		"approvalRequestId": request.ID,
	})
	_ = s.recordApprovalDecisionAudit(ctx, principal, updated, "ai_gateway.approval.execute", "executed", "AI client registration activated")
	return domainaigateway.ApprovalDecisionResult{Request: updated}, nil
}

func (s *Service) rejectOrCancelAIClientRegistrationApproval(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, action string, input domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	status := "rejected"
	operationAction := "ai_gateway.approval.reject"
	summary := "AI client registration rejected"
	clientStatus := "rejected"
	if action == "cancel" {
		status = "canceled"
		operationAction = "ai_gateway.approval.cancel"
		summary = "AI client registration canceled"
		clientStatus = "disabled"
	}
	if client, err := s.repo.GetAIClient(ctx, request.AIClientID); err == nil && client.ID != "" {
		client.Status = clientStatus
		client.Metadata = mergeAnyMaps(client.Metadata, map[string]any{
			"registrationApproval":          status,
			"registrationApprovalRequestId": request.ID,
			"registrationDecidedBy":         principal.UserID,
			"registrationDecidedAt":         time.Now().UTC().Format(time.RFC3339),
		})
		_, _ = s.repo.UpdateAIClient(ctx, client)
		_ = s.recordConfigAudit(ctx, principal, "AIGatewayAIClient", client.ID, "ai_gateway.ai_client.registration."+status, status, summary, map[string]any{
			"aiClientId":        client.ID,
			"approvalRequestId": request.ID,
		})
	}
	updated, err := s.transitionApprovalRequest(ctx, principal, request, status, summary, input.Comment, request.RelatedIDs, request.Output)
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	_ = s.recordApprovalDecisionAudit(ctx, principal, updated, operationAction, status, summary)
	return domainaigateway.ApprovalDecisionResult{Request: updated}, nil
}

func (s *Service) failApprovedApprovalRequest(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, tool domainaigateway.ToolCapability, cause error) (domainaigateway.ApprovalDecisionResult, error) {
	relatedIDs := mergeAnyMaps(request.RelatedIDs, nil)
	relatedIDs["approvalRequestId"] = request.ID
	output := map[string]any{"error": sanitizeGatewayValue(cause.Error())}
	updated, err := s.finalizeApprovedApprovalRequest(ctx, principal, request, "failed", cause.Error(), relatedIDs, output)
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, err
	}
	_ = s.recordToolAudit(ctx, approvalRequestPrincipal(request), approvalRequestInvocationInput(updated), tool, "failure", cause.Error(), relatedIDs)
	_ = s.recordApprovalDecisionAudit(ctx, principal, updated, "ai_gateway.approval.execute", "failed", cause.Error())
	return domainaigateway.ApprovalDecisionResult{Request: updated}, cause
}

func (s *Service) transitionApprovalRequest(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, status, summary, comment string, relatedIDs map[string]any, output any) (domainaigateway.ApprovalRequest, error) {
	now := time.Now().UTC()
	return s.repo.UpdateApprovalRequest(ctx, request.ID, domainaigateway.ApprovalRequestUpdate{
		ExpectedStatus:  "pending",
		Status:          status,
		Summary:         summary,
		RelatedIDs:      relatedIDs,
		Output:          output,
		DecidedBy:       principal.UserID,
		DecidedByName:   principal.UserName,
		DecidedAt:       &now,
		DecisionComment: strings.TrimSpace(comment),
		UpdatedAt:       now,
	})
}

func (s *Service) finalizeApprovedApprovalRequest(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, status, summary string, relatedIDs map[string]any, output any) (domainaigateway.ApprovalRequest, error) {
	now := time.Now().UTC()
	return s.repo.UpdateApprovalRequest(ctx, request.ID, domainaigateway.ApprovalRequestUpdate{
		ExpectedStatus:  "approved",
		Status:          status,
		Summary:         summary,
		RelatedIDs:      relatedIDs,
		Output:          sanitizeGatewayValue(output),
		DecidedBy:       firstNonEmpty(request.DecidedBy, principal.UserID),
		DecidedByName:   firstNonEmpty(request.DecidedByName, principal.UserName),
		DecidedAt:       request.DecidedAt,
		DecisionComment: request.DecisionComment,
		UpdatedAt:       now,
	})
}

func (s *Service) expirePendingApprovalRequests(ctx context.Context) error {
	if s.repo == nil {
		return nil
	}
	expired, err := s.repo.ExpirePendingApprovalRequests(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, request := range expired {
		_ = s.recordApprovalTimeoutAudit(ctx, request)
	}
	return nil
}

func (s *Service) expireApprovalRequest(ctx context.Context, request domainaigateway.ApprovalRequest) error {
	if s.repo == nil || request.Status != "pending" {
		return nil
	}
	updated, err := s.repo.UpdateApprovalRequest(ctx, request.ID, domainaigateway.ApprovalRequestUpdate{
		ExpectedStatus: "pending",
		Status:         "timeout",
		Summary:        "AI Gateway approval request timed out",
		RelatedIDs:     request.RelatedIDs,
		Output:         request.Output,
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidArgument) {
			return nil
		}
		return err
	}
	_ = s.recordApprovalTimeoutAudit(ctx, updated)
	return nil
}

func buildAIClient(principal domainidentity.Principal, input domainaigateway.AIClientInput, forcedID string) (domainaigateway.AIClient, error) {
	id := strings.TrimSpace(forcedID)
	if id == "" {
		id = strings.TrimSpace(input.ID)
	}
	if id == "" {
		id = uuid.NewString()
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.AIClient{}, fmt.Errorf("%w: AI client name is required", apperrors.ErrInvalidArgument)
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "mcp_client"
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	status = normalizeAIClientStatus(status)
	if !validAIClientStatus(status) {
		return domainaigateway.AIClient{}, fmt.Errorf("%w: AI client status must be active, disabled, pending, pending_approval, or rejected", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	return domainaigateway.AIClient{
		ID:             id,
		Name:           name,
		Kind:           kind,
		Status:         status,
		RedirectURIs:   normalizeStringSlice(input.RedirectURIs),
		AllowedOrigins: normalizeStringSlice(input.AllowedOrigins),
		Metadata:       emptyMap(input.Metadata),
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func normalizeAIClientStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "approval_required":
		return "pending_approval"
	default:
		return value
	}
}

func validAIClientStatus(value string) bool {
	return validStatus(value, "active", "disabled", "pending", "pending_approval", "rejected")
}

func aiClientRegistrationApprovalPending(client domainaigateway.AIClient) bool {
	status := normalizeAIClientStatus(client.Status)
	return status == "pending" || status == "pending_approval" || boolFromAny(client.Metadata["registrationApprovalRequired"]) || boolFromAny(client.Metadata["requiresApproval"])
}

func isAIClientRegistrationApprovalRequest(request domainaigateway.ApprovalRequest) bool {
	return request.ToolName == "ai_gateway.ai_client.registration"
}

func buildToolGrant(principal domainidentity.Principal, input domainaigateway.ToolGrantInput) (domainaigateway.ToolGrant, error) {
	return buildToolGrantWithToolLookup(principal, input, toolByName)
}

func (s *Service) buildToolGrant(principal domainidentity.Principal, input domainaigateway.ToolGrantInput) (domainaigateway.ToolGrant, error) {
	return buildToolGrantWithToolLookup(principal, input, s.toolByName)
}

func buildToolGrantWithToolLookup(principal domainidentity.Principal, input domainaigateway.ToolGrantInput, lookup func(string) (domainaigateway.ToolCapability, bool)) (domainaigateway.ToolGrant, error) {
	subjectType := normalizeSubjectType(input.SubjectType)
	if !validStatus(subjectType, "user", "service_account", "role", "team", "ai_client") {
		return domainaigateway.ToolGrant{}, fmt.Errorf("%w: subjectType must be user, service_account, role, team, or ai_client", apperrors.ErrInvalidArgument)
	}
	subjectID := strings.TrimSpace(input.SubjectID)
	if subjectID == "" {
		return domainaigateway.ToolGrant{}, fmt.Errorf("%w: subjectId is required", apperrors.ErrInvalidArgument)
	}
	toolName := strings.TrimSpace(input.ToolName)
	if toolName == "" {
		return domainaigateway.ToolGrant{}, fmt.Errorf("%w: toolName is required", apperrors.ErrInvalidArgument)
	}
	effect := grantEffect(input.Effect)
	if !validStatus(effect, "allow", "deny") {
		return domainaigateway.ToolGrant{}, fmt.Errorf("%w: effect must be allow or deny", apperrors.ErrInvalidArgument)
	}
	riskLevel := input.RiskLevel
	if lookup != nil {
		exactTool, ok := lookup(toolName)
		if ok {
			if riskLevel == "" {
				riskLevel = exactTool.RiskLevel
			}
			if exactTool.RequiresApproval {
				input.RequiresApproval = true
			}
		}
	}
	if riskLevel == "" {
		riskLevel = domainaigateway.RiskLevelRead
	}
	if !validRiskLevel(riskLevel) {
		return domainaigateway.ToolGrant{}, fmt.Errorf("%w: invalid riskLevel", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	return domainaigateway.ToolGrant{
		ID:               uuid.NewString(),
		SubjectType:      subjectType,
		SubjectID:        subjectID,
		AIClientID:       strings.TrimSpace(input.AIClientID),
		ToolName:         toolName,
		Effect:           effect,
		RiskLevel:        riskLevel,
		PermissionKeys:   normalizeStringSlice(input.PermissionKeys),
		ResourceScopes:   emptyMap(input.ResourceScopes),
		RequiresApproval: input.RequiresApproval,
		ExpiresAt:        input.ExpiresAt,
		CreatedBy:        principal.UserID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func buildAccessPolicy(principal domainidentity.Principal, input domainaigateway.AccessPolicyInput, forcedID string) (domainaigateway.AccessPolicy, error) {
	id := strings.TrimSpace(forcedID)
	if id == "" {
		id = uuid.NewString()
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.AccessPolicy{}, fmt.Errorf("%w: policy name is required", apperrors.ErrInvalidArgument)
	}
	subjectType := normalizeSubjectType(input.SubjectType)
	if !validStatus(subjectType, "user", "service_account", "role", "team", "ai_client") {
		return domainaigateway.AccessPolicy{}, fmt.Errorf("%w: subjectType must be user, service_account, role, team, or ai_client", apperrors.ErrInvalidArgument)
	}
	subjectID := strings.TrimSpace(input.SubjectID)
	if subjectID == "" {
		return domainaigateway.AccessPolicy{}, fmt.Errorf("%w: subjectId is required", apperrors.ErrInvalidArgument)
	}
	effect := grantEffect(input.Effect)
	if !validStatus(effect, "allow", "deny") {
		return domainaigateway.AccessPolicy{}, fmt.Errorf("%w: effect must be allow or deny", apperrors.ErrInvalidArgument)
	}
	riskLevels, err := normalizeRiskLevels(input.RiskLevels)
	if err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	approvalPolicy, err := normalizeApprovalPolicy(input.ApprovalPolicy)
	if err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	return domainaigateway.AccessPolicy{
		ID:             id,
		Name:           name,
		Description:    strings.TrimSpace(input.Description),
		Enabled:        enabled,
		SubjectType:    subjectType,
		SubjectID:      subjectID,
		AIClientID:     strings.TrimSpace(input.AIClientID),
		Effect:         effect,
		ToolPatterns:   normalizeStringSlice(input.ToolPatterns),
		SkillIDs:       normalizeStringSlice(input.SkillIDs),
		ResourceScopes: emptyMap(input.ResourceScopes),
		RiskLevels:     riskLevels,
		ApprovalPolicy: approvalPolicy,
		Conditions:     emptyMap(input.Conditions),
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func buildSkillBinding(principal domainidentity.Principal, input domainaigateway.SkillBindingInput, forcedID string) (domainaigateway.SkillBinding, error) {
	return buildSkillBindingWithSkillLookup(principal, input, forcedID, skillByID)
}

func (s *Service) buildSkillBinding(principal domainidentity.Principal, input domainaigateway.SkillBindingInput, forcedID string) (domainaigateway.SkillBinding, error) {
	return buildSkillBindingWithSkillLookup(principal, input, forcedID, s.skillByID)
}

func buildSkillBindingWithSkillLookup(principal domainidentity.Principal, input domainaigateway.SkillBindingInput, forcedID string, lookup func(string) (domainaigateway.SkillCapability, bool)) (domainaigateway.SkillBinding, error) {
	id := strings.TrimSpace(forcedID)
	if id == "" {
		id = uuid.NewString()
	}
	subjectType := normalizeSubjectType(input.SubjectType)
	if !validStatus(subjectType, "user", "service_account", "role", "team", "ai_client") {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: subjectType must be user, service_account, role, team, or ai_client", apperrors.ErrInvalidArgument)
	}
	subjectID := strings.TrimSpace(input.SubjectID)
	if subjectID == "" {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: subjectId is required", apperrors.ErrInvalidArgument)
	}
	skillID := strings.TrimSpace(input.SkillID)
	if skillID == "" {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: skillId is required", apperrors.ErrInvalidArgument)
	}
	if lookup == nil {
		lookup = skillByID
	}
	if _, ok := lookup(skillID); !ok {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: unknown AI Gateway skill %s", apperrors.ErrInvalidArgument, skillID)
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	return domainaigateway.SkillBinding{
		ID:             id,
		SubjectType:    subjectType,
		SubjectID:      subjectID,
		AIClientID:     strings.TrimSpace(input.AIClientID),
		SkillID:        skillID,
		CapabilityRefs: normalizeStringSlice(input.CapabilityRefs),
		Enabled:        enabled,
		Metadata:       emptyMap(input.Metadata),
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func normalizeSubjectType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "org", "organization", "organizations", "group", "groups", "team_id", "org_unit":
		return "team"
	default:
		return normalized
	}
}

func validStatus(value string, allowed ...string) bool {
	value = strings.TrimSpace(value)
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func validRiskLevel(value domainaigateway.RiskLevel) bool {
	switch value {
	case domainaigateway.RiskLevelRead, domainaigateway.RiskLevelAnalyze, domainaigateway.RiskLevelMutate, domainaigateway.RiskLevelExecute, domainaigateway.RiskLevelHigh:
		return true
	default:
		return false
	}
}

func normalizeApprovalRequestStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validApprovalRequestStatus(value string) bool {
	return validStatus(value, "pending", "approved", "rejected", "canceled", "timeout", "executed", "failed")
}

func normalizeRiskLevels(values []domainaigateway.RiskLevel) ([]domainaigateway.RiskLevel, error) {
	out := make([]domainaigateway.RiskLevel, 0, len(values))
	for _, value := range values {
		value = domainaigateway.RiskLevel(strings.TrimSpace(string(value)))
		if value == "" {
			continue
		}
		if !validRiskLevel(value) {
			return nil, fmt.Errorf("%w: invalid riskLevel %s", apperrors.ErrInvalidArgument, value)
		}
		if slices.Contains(out, value) {
			continue
		}
		out = append(out, value)
	}
	slices.Sort(out)
	return out, nil
}

func normalizeApprovalPolicy(value map[string]any) (map[string]any, error) {
	out := emptyMap(value)
	for _, key := range []string{"strategy", "mode", "action", "approval", "state"} {
		raw, exists := out[key]
		if !exists || strings.TrimSpace(fmt.Sprint(raw)) == "" {
			continue
		}
		strategy, ok := parseGatewayRiskStrategy(fmt.Sprint(raw))
		if !ok {
			return nil, fmt.Errorf("%w: invalid approval strategy %s", apperrors.ErrInvalidArgument, raw)
		}
		if strategy != "" {
			out["strategy"] = string(strategy)
		}
		return out, nil
	}
	if boolFromAny(out["dryRunOnly"]) {
		out["strategy"] = string(gatewayRiskDryRunOnly)
		return out, nil
	}
	if boolFromAny(out["requiresHumanConfirm"]) || boolFromAny(out["humanConfirmRequired"]) {
		out["strategy"] = string(gatewayRiskRequireHumanConfirm)
		return out, nil
	}
	if boolFromAny(out["requiresApproval"]) {
		out["strategy"] = string(gatewayRiskRequireApproval)
	}
	return out, nil
}

func (s *Service) normalizeRequestedPermissionKeys(ctx context.Context, principal domainidentity.Principal, requested []string) ([]string, error) {
	granted, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return nil, err
	}
	if len(requested) == 0 {
		return granted, nil
	}
	keys := normalizeStringSlice(requested)
	for _, key := range keys {
		if !slices.Contains(granted, key) {
			return nil, fmt.Errorf("%w: requested permission %s is not granted to subject", apperrors.ErrAccessDenied, key)
		}
	}
	return keys, nil
}

func filterTools(items []domainaigateway.ToolCapability, permissionKeys []string) ([]domainaigateway.ToolCapability, int) {
	out := make([]domainaigateway.ToolCapability, 0, len(items))
	denied := 0
	for _, item := range items {
		if hasAllPermissions(permissionKeys, item.PermissionKeys) {
			out = append(out, item)
			continue
		}
		denied++
	}
	return out, denied
}

func toolByName(name string) (domainaigateway.ToolCapability, bool) {
	for _, item := range defaultTools() {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.ToolCapability{}, false
}

func resourceByName(name string) (domainaigateway.ResourceCapability, bool) {
	name = normalizeGatewayResourceURI(name)
	for _, item := range defaultResources() {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.ResourceCapability{}, false
}

func promptByName(name string) (domainaigateway.PromptCapability, bool) {
	name = strings.TrimSpace(name)
	for _, item := range defaultPrompts() {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.PromptCapability{}, false
}

func skillByID(id string) (domainaigateway.SkillCapability, bool) {
	for _, item := range defaultSkills() {
		if item.ID == id {
			return item, true
		}
	}
	return domainaigateway.SkillCapability{}, false
}

func filterResources(items []domainaigateway.ResourceCapability, permissionKeys []string) ([]domainaigateway.ResourceCapability, int) {
	out := make([]domainaigateway.ResourceCapability, 0, len(items))
	denied := 0
	for _, item := range items {
		if hasAllPermissions(permissionKeys, item.PermissionKeys) {
			out = append(out, item)
			continue
		}
		denied++
	}
	return out, denied
}

func filterPrompts(items []domainaigateway.PromptCapability, permissionKeys []string) ([]domainaigateway.PromptCapability, int) {
	out := make([]domainaigateway.PromptCapability, 0, len(items))
	denied := 0
	for _, item := range items {
		if hasAllPermissions(permissionKeys, item.PermissionKeys) {
			out = append(out, item)
			continue
		}
		denied++
	}
	return out, denied
}

func filterSkills(items []domainaigateway.SkillCapability, permissionKeys []string) ([]domainaigateway.SkillCapability, int) {
	out := make([]domainaigateway.SkillCapability, 0, len(items))
	denied := 0
	for _, item := range items {
		if hasAllPermissions(permissionKeys, item.PermissionKeys) {
			out = append(out, item)
			continue
		}
		denied++
	}
	return out, denied
}

func hasAllPermissions(granted []string, required []string) bool {
	for _, permissionKey := range required {
		if !slices.Contains(granted, strings.TrimSpace(permissionKey)) {
			return false
		}
	}
	return true
}

func toolAllowedByGrants(tool domainaigateway.ToolCapability, grants []domainaigateway.ToolGrant, permissionKeys []string) (bool, bool, string) {
	hasAllowGrant := false
	matchedAllowGrant := false
	matchedAllowNeedsPermission := false
	requiresApproval := false
	for _, grant := range grants {
		if grantEffect(grant.Effect) != "allow" {
			continue
		}
		hasAllowGrant = true
		if !grantMatchesTool(grant.ToolName, tool.Name) {
			continue
		}
		if hasAllPermissions(permissionKeys, grant.PermissionKeys) {
			matchedAllowGrant = true
			if grant.RequiresApproval {
				requiresApproval = true
			}
			continue
		}
		matchedAllowNeedsPermission = true
	}
	for _, grant := range grants {
		if grantEffect(grant.Effect) != "deny" || !grantMatchesTool(grant.ToolName, tool.Name) {
			continue
		}
		return false, requiresApproval, "matched deny grant " + grant.ID
	}
	if hasAllowGrant && !matchedAllowGrant {
		if matchedAllowNeedsPermission {
			return false, requiresApproval, "matching allow grant requires permissions not granted to the subject"
		}
		return false, requiresApproval, "no matching allow grant"
	}
	return true, requiresApproval, ""
}

func toolAllowedByGrantsForInvocation(tool domainaigateway.ToolCapability, grants []domainaigateway.ToolGrant, permissionKeys []string, invocationScope map[string]string) (bool, bool, string) {
	hasAllowGrant := false
	matchedAllowGrant := false
	matchedAllowNeedsPermission := false
	matchedAllowScopeMismatch := false
	requiresApproval := false
	for _, grant := range grants {
		if grantEffect(grant.Effect) != "allow" {
			continue
		}
		hasAllowGrant = true
		if !grantMatchesTool(grant.ToolName, tool.Name) {
			continue
		}
		if !gatewayResourceScopeMatches(grant.ResourceScopes, invocationScope) {
			matchedAllowScopeMismatch = true
			continue
		}
		if hasAllPermissions(permissionKeys, grant.PermissionKeys) {
			matchedAllowGrant = true
			if grant.RequiresApproval {
				requiresApproval = true
			}
			continue
		}
		matchedAllowNeedsPermission = true
	}
	for _, grant := range grants {
		if grantEffect(grant.Effect) != "deny" || !grantMatchesTool(grant.ToolName, tool.Name) {
			continue
		}
		if !gatewayResourceScopeMatches(grant.ResourceScopes, invocationScope) {
			continue
		}
		return false, requiresApproval, "matched deny grant " + grant.ID
	}
	if hasAllowGrant && !matchedAllowGrant {
		if matchedAllowNeedsPermission {
			return false, requiresApproval, "matching allow grant requires permissions not granted to the subject"
		}
		if matchedAllowScopeMismatch {
			return false, requiresApproval, "matching allow grant does not allow the requested resource scope"
		}
		return false, requiresApproval, "no matching allow grant"
	}
	return true, requiresApproval, ""
}

func toolAllowedByAccessPolicies(tool domainaigateway.ToolCapability, policies []domainaigateway.AccessPolicy, skillID string) (bool, gatewayRiskDecision, string) {
	return toolAllowedByAccessPoliciesWithSkills(tool, policies, skillID, defaultSkills())
}

func toolAllowedByAccessPoliciesWithSkills(tool domainaigateway.ToolCapability, policies []domainaigateway.AccessPolicy, skillID string, knownSkills []domainaigateway.SkillCapability) (bool, gatewayRiskDecision, string) {
	hasAllowPolicy := false
	matchedAllowPolicy := false
	decision := gatewayRiskDecision{}
	for _, policy := range policies {
		if grantEffect(policy.Effect) == "allow" {
			hasAllowPolicy = true
		}
		if grantEffect(policy.Effect) != "deny" || !accessPolicyMatchesToolWithSkills(policy, tool, skillID, knownSkills) {
			continue
		}
		return false, decision, "matched deny policy " + policy.ID
	}
	for _, policy := range policies {
		if grantEffect(policy.Effect) != "allow" || !accessPolicyMatchesToolWithSkills(policy, tool, skillID, knownSkills) {
			continue
		}
		matchedAllowPolicy = true
		policyDecision := accessPolicyRiskDecision(policy)
		if policyDecision.Strategy == gatewayRiskDeny {
			return false, decision, "matched deny strategy policy " + policy.ID
		}
		decision = mergeGatewayRiskDecision(decision, policyDecision)
	}
	if hasAllowPolicy && !matchedAllowPolicy {
		return false, decision, "no matching allow policy"
	}
	return true, decision, ""
}

func toolAllowedByAccessPoliciesForInvocation(tool domainaigateway.ToolCapability, policies []domainaigateway.AccessPolicy, skillID string, invocationScope map[string]string) (bool, gatewayRiskDecision, string) {
	return toolAllowedByAccessPoliciesForInvocationWithSkills(tool, policies, skillID, invocationScope, defaultSkills())
}

func toolAllowedByAccessPoliciesForInvocationWithSkills(tool domainaigateway.ToolCapability, policies []domainaigateway.AccessPolicy, skillID string, invocationScope map[string]string, knownSkills []domainaigateway.SkillCapability) (bool, gatewayRiskDecision, string) {
	hasAllowPolicy := false
	matchedAllowPolicy := false
	matchedAllowScopeMismatch := false
	decision := gatewayRiskDecision{}
	for _, policy := range policies {
		if grantEffect(policy.Effect) == "allow" {
			hasAllowPolicy = true
		}
		if grantEffect(policy.Effect) != "deny" || !accessPolicyToolSelectorsMatchWithSkills(policy, tool, skillID, knownSkills) {
			continue
		}
		if !gatewayResourceScopeMatches(policy.ResourceScopes, invocationScope) {
			continue
		}
		return false, decision, "matched deny policy " + policy.ID
	}
	for _, policy := range policies {
		if grantEffect(policy.Effect) != "allow" || !accessPolicyToolSelectorsMatchWithSkills(policy, tool, skillID, knownSkills) {
			continue
		}
		if !gatewayResourceScopeMatches(policy.ResourceScopes, invocationScope) {
			matchedAllowScopeMismatch = true
			continue
		}
		matchedAllowPolicy = true
		policyDecision := accessPolicyRiskDecision(policy)
		if policyDecision.Strategy == gatewayRiskDeny {
			return false, decision, "matched deny strategy policy " + policy.ID
		}
		decision = mergeGatewayRiskDecision(decision, policyDecision)
	}
	if hasAllowPolicy && !matchedAllowPolicy {
		if matchedAllowScopeMismatch {
			return false, decision, "matching allow policy does not allow the requested resource scope"
		}
		return false, decision, "no matching allow policy"
	}
	return true, decision, ""
}

func skillAllowedByAccessPolicies(skill domainaigateway.SkillCapability, policies []domainaigateway.AccessPolicy) (bool, string) {
	hasSkillAllowPolicy := false
	matchedAllowPolicy := false
	for _, policy := range policies {
		if len(policy.SkillIDs) == 0 {
			continue
		}
		if grantEffect(policy.Effect) == "allow" {
			hasSkillAllowPolicy = true
		}
		if grantEffect(policy.Effect) != "deny" || !matchesStringPatternList(policy.SkillIDs, skill.ID) {
			continue
		}
		return false, "matched deny policy " + policy.ID
	}
	for _, policy := range policies {
		if len(policy.SkillIDs) == 0 || grantEffect(policy.Effect) != "allow" {
			continue
		}
		if matchesStringPatternList(policy.SkillIDs, skill.ID) {
			matchedAllowPolicy = true
		}
	}
	if hasSkillAllowPolicy && !matchedAllowPolicy {
		return false, "no matching allow policy"
	}
	return true, ""
}

func accessPolicyMatchesTool(policy domainaigateway.AccessPolicy, tool domainaigateway.ToolCapability, skillID string) bool {
	return accessPolicyMatchesToolWithSkills(policy, tool, skillID, defaultSkills())
}

func accessPolicyMatchesToolWithSkills(policy domainaigateway.AccessPolicy, tool domainaigateway.ToolCapability, skillID string, knownSkills []domainaigateway.SkillCapability) bool {
	return accessPolicyToolSelectorsMatchWithSkills(policy, tool, skillID, knownSkills)
}

func accessPolicyToolSelectorsMatch(policy domainaigateway.AccessPolicy, tool domainaigateway.ToolCapability, skillID string) bool {
	return accessPolicyToolSelectorsMatchWithSkills(policy, tool, skillID, defaultSkills())
}

func accessPolicyToolSelectorsMatchWithSkills(policy domainaigateway.AccessPolicy, tool domainaigateway.ToolCapability, skillID string, knownSkills []domainaigateway.SkillCapability) bool {
	if len(policy.ToolPatterns) > 0 && !matchesToolPatternList(policy.ToolPatterns, tool.Name) {
		return false
	}
	if len(policy.RiskLevels) > 0 && !slices.Contains(policy.RiskLevels, tool.RiskLevel) {
		return false
	}
	if len(policy.SkillIDs) > 0 {
		skillID = strings.TrimSpace(skillID)
		if skillID != "" {
			return matchesStringPatternList(policy.SkillIDs, skillID)
		}
		return toolInAnySkillWithSkills(policy.SkillIDs, tool.Name, knownSkills)
	}
	return len(policy.ToolPatterns) > 0 || len(policy.RiskLevels) > 0 || len(policy.SkillIDs) > 0 || policyHasNoSelectors(policy)
}

func policyHasNoSelectors(policy domainaigateway.AccessPolicy) bool {
	return len(policy.ToolPatterns) == 0 && len(policy.RiskLevels) == 0 && len(policy.SkillIDs) == 0
}

type gatewayRiskStrategy string

const (
	gatewayRiskAllow               gatewayRiskStrategy = "allow"
	gatewayRiskDeny                gatewayRiskStrategy = "deny"
	gatewayRiskRequireApproval     gatewayRiskStrategy = "require_approval"
	gatewayRiskRequireHumanConfirm gatewayRiskStrategy = "require_human_confirm"
	gatewayRiskDryRunOnly          gatewayRiskStrategy = "dry_run_only"
)

func (s gatewayRiskStrategy) requiresApprovalRequest() bool {
	return s == gatewayRiskRequireApproval || s == gatewayRiskRequireHumanConfirm
}

type gatewayRiskDecision struct {
	Strategy          gatewayRiskStrategy
	PolicyID          string
	ApprovalPolicyRef string
	Reason            string
	ApprovalPolicy    map[string]any
}

func gatewayRiskDecisionForTool(tool domainaigateway.ToolCapability) gatewayRiskDecision {
	if tool.RequiresApproval {
		return gatewayRiskDecision{
			Strategy: gatewayRiskRequireApproval,
			Reason:   "tool catalog requires approval",
		}
	}
	return gatewayRiskDecision{Strategy: gatewayRiskAllow}
}

func mergeGatewayRiskDecision(current, next gatewayRiskDecision) gatewayRiskDecision {
	if next.Strategy == "" {
		return current
	}
	if current.Strategy == "" || gatewayRiskStrictness(next.Strategy) >= gatewayRiskStrictness(current.Strategy) {
		return next
	}
	return current
}

func gatewayRiskStrictness(strategy gatewayRiskStrategy) int {
	switch strategy {
	case gatewayRiskDeny:
		return 5
	case gatewayRiskDryRunOnly:
		return 4
	case gatewayRiskRequireApproval:
		return 3
	case gatewayRiskRequireHumanConfirm:
		return 2
	case gatewayRiskAllow:
		return 1
	default:
		return 0
	}
}

func (d gatewayRiskDecision) requiresApproval() bool {
	return d.Strategy == gatewayRiskRequireApproval || d.Strategy == gatewayRiskRequireHumanConfirm
}

func (d gatewayRiskDecision) shouldHoldExecution() bool {
	switch d.Strategy {
	case gatewayRiskRequireApproval, gatewayRiskRequireHumanConfirm, gatewayRiskDryRunOnly:
		return true
	default:
		return false
	}
}

func (d gatewayRiskDecision) result() string {
	switch d.Strategy {
	case gatewayRiskRequireApproval:
		return "pending_approval"
	case gatewayRiskRequireHumanConfirm:
		return "pending_human_confirm"
	case gatewayRiskDryRunOnly:
		return "dry_run"
	default:
		return "success"
	}
}

func (s *Service) gatewayDeliveryApprovalPolicy(ctx context.Context, decision gatewayRiskDecision) (domaindelivery.ApprovalPolicy, bool) {
	if s == nil || s.delivery == nil || strings.TrimSpace(decision.ApprovalPolicyRef) == "" {
		return domaindelivery.ApprovalPolicy{}, false
	}
	policy, err := s.delivery.GetApprovalPolicy(ctx, strings.TrimSpace(decision.ApprovalPolicyRef))
	if err != nil || !policy.Enabled {
		return domaindelivery.ApprovalPolicy{}, false
	}
	return policy, true
}

func gatewayApprovalExpiresAt(decision gatewayRiskDecision, deliveryPolicy domaindelivery.ApprovalPolicy, hasDeliveryPolicy bool, now time.Time) *time.Time {
	if !decision.requiresApproval() {
		return nil
	}
	duration := 24 * time.Hour
	if hasDeliveryPolicy && deliveryPolicy.SLAMinutes > 0 {
		duration = time.Duration(deliveryPolicy.SLAMinutes) * time.Minute
	}
	expiresAt := now.Add(duration)
	return &expiresAt
}

func accessPolicyRiskDecision(policy domainaigateway.AccessPolicy) gatewayRiskDecision {
	strategy, ok := approvalPolicyRiskStrategy(policy.ApprovalPolicy)
	if !ok {
		return gatewayRiskDecision{}
	}
	return gatewayRiskDecision{
		Strategy:          strategy,
		PolicyID:          policy.ID,
		ApprovalPolicyRef: approvalPolicyRef(policy.ApprovalPolicy),
		Reason:            "matched access policy " + policy.ID,
		ApprovalPolicy:    sanitizeGatewayMap(policy.ApprovalPolicy),
	}
}

func accessPolicyRequiresApproval(policy domainaigateway.AccessPolicy) bool {
	return accessPolicyRiskDecision(policy).requiresApproval()
}

func gatewayApprovalRoutingFromDecision(decision gatewayRiskDecision, deliveryPolicy domaindelivery.ApprovalPolicy, hasDeliveryPolicy bool) map[string]any {
	routing := map[string]any{}
	if hasDeliveryPolicy {
		routing = mergeAnyMaps(routing, gatewayApprovalRoutingFromDeliveryPolicy(deliveryPolicy))
	}
	inline := gatewayApprovalRoutingFromPolicy(decision.ApprovalPolicy)
	if len(inline) > 0 {
		routing = gatewayMergeApprovalRouting(routing, inline)
	}
	if len(routing) == 0 {
		return nil
	}
	return routing
}

func gatewayApprovalRoutingFromDeliveryPolicy(policy domaindelivery.ApprovalPolicy) map[string]any {
	if strings.TrimSpace(policy.ID) == "" {
		return nil
	}
	routing := map[string]any{
		"deliveryApprovalPolicyId":   strings.TrimSpace(policy.ID),
		"deliveryApprovalPolicyKey":  strings.TrimSpace(policy.Key),
		"deliveryApprovalPolicyName": strings.TrimSpace(policy.Name),
	}
	if mode := gatewayDeliveryApprovalMode(policy.Mode); mode != "" {
		routing["approvalMode"] = mode
	}
	if policy.RequiredApprovals > 0 {
		routing["requiredApprovals"] = policy.RequiredApprovals
	}
	if roles := normalizeStringSlice(policy.ApproverRoles); len(roles) > 0 {
		routing["candidateRoles"] = roles
	}
	if len(policy.ChangeWindow) > 0 {
		if window := gatewayApprovalChangeWindow(policy.ChangeWindow); len(window) > 0 {
			routing["changeWindow"] = window
		} else {
			routing["changeWindow"] = sanitizeGatewayMap(policy.ChangeWindow)
		}
	}
	if metadataRouting := gatewayApprovalRoutingFromPolicy(policy.Metadata); len(metadataRouting) > 0 {
		routing = gatewayMergeApprovalRouting(routing, metadataRouting)
	}
	return routing
}

func gatewayDeliveryApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "multi", "all", "joint", "joint_approval", "quorum":
		return "all"
	case "any", "single", "one", "or":
		return "any"
	default:
		return ""
	}
}

func gatewayApprovalRoutingFromPolicy(policy map[string]any) map[string]any {
	if len(policy) == 0 {
		return nil
	}
	values := gatewayApprovalPolicyValues(policy)
	out := map[string]any{}
	if users := normalizeStringSlice(gatewayConditionStringList(values, "approverUsers", "approverUserIds", "candidateUsers", "candidateUserIds", "approvalUsers", "approvalUserIds", "userIds", "users")); len(users) > 0 {
		out["candidateUserIds"] = users
	}
	roles := normalizeStringSlice(gatewayConditionStringList(values, "approverRoles", "candidateRoles", "approvalRoles", "roles", "roleIds"))
	if roleQuotas := gatewayApprovalQuotaMap(values, "requiredRoleApprovals", "minRoleApprovals", "roleApprovalQuorum", "roleApprovals", "roleQuotas", "requiredRoles"); len(roleQuotas) > 0 {
		out["requiredRoleApprovals"] = roleQuotas
		roles = gatewayAppendUniqueStrings(roles, gatewayApprovalQuotaNames(roleQuotas)...)
	}
	if len(roles) > 0 {
		out["candidateRoles"] = normalizeStringSlice(roles)
	}
	teams := normalizeStringSlice(gatewayConditionStringList(values, "approverTeams", "candidateTeams", "approvalTeams", "teams", "teamIds", "groups", "groupIds"))
	if teamQuotas := gatewayApprovalQuotaMap(values, "requiredTeamApprovals", "minTeamApprovals", "teamApprovalQuorum", "teamApprovals", "teamQuotas", "requiredTeams"); len(teamQuotas) > 0 {
		out["requiredTeamApprovals"] = teamQuotas
		teams = gatewayAppendUniqueStrings(teams, gatewayApprovalQuotaNames(teamQuotas)...)
	}
	if len(teams) > 0 {
		out["candidateTeams"] = teams
	}
	if onCallRef := gatewayFirstString(values, "onCallRef", "oncallRef", "onCall", "oncall", "dutyRef", "routeRef", "scheduleRef"); onCallRef != "" {
		out["onCallRef"] = onCallRef
	}
	if mode := gatewayApprovalMode(values); mode != "" {
		out["approvalMode"] = mode
	}
	if required, ok := gatewayFirstPositiveInt(values, "requiredApprovals", "minApprovals", "approvalQuorum", "quorum", "minApproverCount"); ok {
		out["requiredApprovals"] = required
	}
	if stages := gatewayApprovalStagesFromValues(values); len(stages) > 0 {
		out["approvalStages"] = stages
		out["currentStageIndex"] = 0
		if stageName := strings.TrimSpace(fmt.Sprint(stages[0]["name"])); stageName != "" && stageName != "<nil>" {
			out["currentStageName"] = stageName
		}
	}
	if changeWindow := gatewayApprovalChangeWindow(values); len(changeWindow) > 0 {
		out["changeWindow"] = changeWindow
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gatewayMergeApprovalRouting(base, override map[string]any) map[string]any {
	out := mergeAnyMaps(base, nil)
	for key, value := range override {
		switch key {
		case "candidateUserIds", "candidateRoles", "candidateTeams", "onCallCandidateUserIds":
			out[key] = normalizeStringSlice(gatewayAppendUniqueStrings(gatewayStringList(out[key]), gatewayStringList(value)...))
		case "requiredApprovals":
			current, _ := gatewayPositiveInt(out[key])
			next, ok := gatewayPositiveInt(value)
			if ok && next > current {
				out[key] = next
			} else if _, exists := out[key]; !exists && ok {
				out[key] = next
			}
		case "requiredRoleApprovals", "requiredTeamApprovals":
			out[key] = gatewayMergeApprovalQuotaMaps(out[key], value)
		case "approvalStages":
			if len(gatewayApprovalStages(out)) == 0 {
				out[key] = value
			}
		default:
			out[key] = value
		}
	}
	return out
}

func gatewayMergeApprovalQuotaMaps(base, override any) map[string]any {
	out := gatewayApprovalQuotaAnyMap(base)
	for key, value := range gatewayApprovalQuotaAnyMap(override) {
		current, _ := gatewayPositiveInt(out[key])
		next, ok := gatewayPositiveInt(value)
		if ok && next > current {
			out[key] = next
		} else if _, exists := out[key]; !exists && ok {
			out[key] = next
		}
	}
	return out
}

func gatewayApprovalQuotaAnyMap(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return mergeAnyMaps(typed, nil)
	case map[string]int:
		return gatewayIntMapToAnyMap(typed)
	default:
		return map[string]any{}
	}
}

func gatewayApprovalPolicyValues(policy map[string]any) map[string]any {
	values := make(map[string]any, len(policy)+8)
	for key, value := range policy {
		values[key] = value
	}
	for _, alias := range []string{"routing", "approvalRouting", "approvers", "candidates", "candidateApprovers"} {
		for key, value := range mapValue(policy[alias]) {
			values[key] = value
		}
	}
	return values
}

func gatewayApprovalChangeWindow(values map[string]any) map[string]any {
	window := mapValue(values["changeWindow"])
	if len(window) == 0 {
		window = mapValue(values["approvalWindow"])
	}
	if len(window) == 0 {
		window = mapValue(values["window"])
	}
	out := map[string]any{}
	merged := make(map[string]any, len(values)+len(window))
	for key, value := range values {
		merged[key] = value
	}
	for key, value := range window {
		merged[key] = value
	}
	if startsAt := gatewayFirstString(merged, "startsAt", "startAt", "start", "from", "notBefore", "beginAt"); startsAt != "" {
		out["startsAt"] = startsAt
	}
	if endsAt := gatewayFirstString(merged, "endsAt", "endAt", "end", "to", "until", "notAfter"); endsAt != "" {
		out["endsAt"] = endsAt
	}
	if timezone := gatewayFirstString(merged, "timezone", "timeZone", "tz"); timezone != "" {
		out["timezone"] = timezone
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) resolveGatewayApprovalOnCall(ctx context.Context, input domainaigateway.ToolInvocationRequest, routing map[string]any) map[string]any {
	if len(routing) == 0 || s == nil || s.oncall == nil {
		return routing
	}
	out := mergeAnyMaps(routing, nil)
	out = s.resolveGatewayApprovalOnCallRouting(ctx, input, out)
	stages := gatewayApprovalStages(out)
	if len(stages) > 0 {
		for index := range stages {
			stages[index] = s.resolveGatewayApprovalOnCallRouting(ctx, input, stages[index])
		}
		out["approvalStages"] = stages
	}
	return out
}

func (s *Service) resolveGatewayApprovalOnCallRouting(ctx context.Context, input domainaigateway.ToolInvocationRequest, routing map[string]any) map[string]any {
	onCallRef := strings.TrimSpace(fmt.Sprint(routing["onCallRef"]))
	if onCallRef == "" || onCallRef == "<nil>" {
		return routing
	}
	out := mergeAnyMaps(routing, nil)
	resolution, source, err := s.gatewayApprovalOnCallResolution(ctx, input, onCallRef)
	if err != nil {
		out["onCallResolution"] = map[string]any{
			"status": "unresolved",
			"ref":    onCallRef,
			"error":  err.Error(),
		}
		return out
	}
	candidates := gatewayOnCallCandidateUserIDs(resolution)
	metadata := gatewayOnCallResolutionMetadata(resolution, onCallRef, source, candidates)
	if len(candidates) == 0 {
		metadata["status"] = "no_current_participant"
		out["onCallResolution"] = metadata
		return out
	}
	out["candidateUserIds"] = normalizeStringSlice(gatewayAppendUniqueStrings(gatewayStringList(out["candidateUserIds"]), candidates...))
	out["onCallCandidateUserIds"] = candidates
	out["onCallResolution"] = metadata
	return out
}

func (s *Service) gatewayApprovalOnCallResolution(ctx context.Context, input domainaigateway.ToolInvocationRequest, onCallRef string) (map[string]any, string, error) {
	principal := gatewayOnCallResolverPrincipal()
	resolution, err := s.oncall.GetCurrentOnCall(ctx, principal, onCallRef)
	if err == nil && len(gatewayOnCallCandidateUserIDs(resolution)) > 0 {
		return resolution, "current_oncall", nil
	}
	routeInput := gatewayOnCallResolveInput(input, onCallRef)
	routeResolution, routeErr := s.oncall.ResolveOnCall(ctx, principal, routeInput)
	if routeErr == nil && len(routeResolution) > 0 {
		return routeResolution, "route_resolution", nil
	}
	if err != nil {
		if routeErr != nil {
			return nil, "", err
		}
		return routeResolution, "route_resolution", nil
	}
	if len(resolution) > 0 {
		return resolution, "current_oncall", nil
	}
	return nil, "", routeErr
}

func gatewayOnCallResolverPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "ai-gateway",
		UserName: "AI Gateway",
		Roles:    []string{"admin"},
	}
}

func gatewayOnCallResolveInput(input domainaigateway.ToolInvocationRequest, onCallRef string) domainalert.OnCallResolveInput {
	values := mergeAnyMaps(input.Input, map[string]any{})
	labels := map[string]string{
		"aiGatewayTool": strings.TrimSpace(input.ToolName),
		"onCallRef":     strings.TrimSpace(onCallRef),
	}
	for _, key := range []string{"applicationId", "applicationEnvironmentId", "environmentId", "releaseBundleId", "executionTaskId", "clusterId", "namespace", "service", "workload", "severity", "businessLineId"} {
		if value := firstMapString(values, key); value != "" {
			labels[key] = value
		}
	}
	for key, value := range mapValue(values["labels"]) {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			labels[key] = text
		}
	}
	return domainalert.OnCallResolveInput{
		IntegrationID:   firstNonEmpty(firstMapString(values, "integrationId", "integrationID", "integration"), "ai-gateway"),
		IntegrationType: firstNonEmpty(firstMapString(values, "integrationType", "sourceType", "source"), "ai_gateway"),
		BusinessLineID:  firstMapString(values, "businessLineId", "businessLineID", "businessLine"),
		AlertCategory:   firstNonEmpty(firstMapString(values, "alertCategory", "category", "riskCategory"), "ai_gateway_approval"),
		AlertName:       firstNonEmpty(firstMapString(values, "alertName", "title", "name"), input.ToolName),
		Severity:        firstNonEmpty(firstMapString(values, "severity", "riskLevel"), "warning"),
		Service:         firstMapString(values, "service", "serviceName", "applicationKey", "applicationId", "workload", "deploymentName"),
		Role:            firstNonEmpty(firstMapString(values, "oncallRole", "onCallRole", "ownerRole", "role"), "ops"),
		ClusterID:       firstMapString(values, "clusterId", "clusterID", "cluster"),
		Namespace:       firstMapString(values, "namespace"),
		Labels:          labels,
	}
}

func gatewayOnCallCandidateUserIDs(resolution map[string]any) []string {
	if len(resolution) == 0 {
		return nil
	}
	candidates := gatewayStringList(resolution["currentParticipant"])
	if len(candidates) == 0 {
		candidates = gatewayStringList(resolution["currentParticipants"])
	}
	if len(candidates) == 0 {
		candidates = gatewayStringList(resolution["participants"])
		if len(candidates) > 1 {
			candidates = candidates[:1]
		}
	}
	return normalizeStringSlice(candidates)
}

func gatewayOnCallResolutionMetadata(resolution map[string]any, onCallRef, source string, candidates []string) map[string]any {
	metadata := map[string]any{
		"status":           "resolved",
		"ref":              onCallRef,
		"source":           source,
		"candidateUserIds": candidates,
	}
	for _, key := range []string{"resolutionStatus", "scheduleId", "schedule", "rotationId", "rotation", "routeId", "route", "assignmentRuleId", "assignmentRule", "targetType", "targetRef", "groupKey", "windowStart", "windowEnd", "escalationPolicyId"} {
		if value := gatewaySerializableScalar(resolution[key]); value != nil {
			metadata[key] = value
		}
	}
	return metadata
}

func gatewaySerializableScalar(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		return text
	case bool, int, int32, int64, float32, float64:
		return value
	default:
		return nil
	}
}

func authorizeGatewayApprovalDecision(principal domainidentity.Principal, request domainaigateway.ApprovalRequest, action string, now time.Time) error {
	routing := mapValue(request.RelatedIDs["approvalRouting"])
	if len(routing) == 0 {
		return nil
	}
	activeRouting := gatewayApprovalActiveRouting(routing)
	if !gatewayApprovalPrincipalMatchesRouting(principal, activeRouting) {
		return fmt.Errorf("%w: approval request is restricted to configured candidate approvers", apperrors.ErrAccessDenied)
	}
	if action == "approve" {
		if err := gatewayApprovalChangeWindowAllows(activeRouting, now); err != nil {
			return err
		}
	}
	return nil
}

func enrichApprovalRequests(items []domainaigateway.ApprovalRequest) []domainaigateway.ApprovalRequest {
	out := make([]domainaigateway.ApprovalRequest, len(items))
	for index, item := range items {
		out[index] = enrichApprovalRequest(item)
	}
	return out
}

func enrichApprovalRequest(item domainaigateway.ApprovalRequest) domainaigateway.ApprovalRequest {
	trace := gatewayApprovalTrace(item)
	if trace != nil {
		item.ApprovalTrace = trace
	}
	return item
}

func gatewayApprovalTrace(request domainaigateway.ApprovalRequest) *domainaigateway.ApprovalTrace {
	routing := mapValue(request.RelatedIDs["approvalRouting"])
	if len(routing) == 0 {
		return nil
	}
	trace := &domainaigateway.ApprovalTrace{
		ApprovalMode:           strings.TrimSpace(fmt.Sprint(routing["approvalMode"])),
		CurrentStageName:       gatewayFirstString(routing, "currentStageName"),
		StageCount:             intFromAny(routing["stageCount"]),
		ApprovedCount:          intFromAny(routing["approvedCount"]),
		RequiredApprovals:      intFromAny(routing["requiredApprovals"]),
		PendingRequirements:    normalizeStringSlice(gatewayStringList(routing["pendingRequirements"])),
		SatisfiedRequirements:  normalizeStringSlice(gatewayStringList(routing["satisfiedRequirements"])),
		RoleApprovedCounts:     gatewayApprovalIntCounts(routing["roleApprovedCounts"]),
		TeamApprovedCounts:     gatewayApprovalIntCounts(routing["teamApprovedCounts"]),
		CandidateUserIDs:       normalizeStringSlice(gatewayStringList(routing["candidateUserIds"])),
		CandidateRoles:         normalizeStringSlice(gatewayStringList(routing["candidateRoles"])),
		CandidateTeams:         normalizeStringSlice(gatewayStringList(routing["candidateTeams"])),
		OnCallCandidateUserIDs: normalizeStringSlice(gatewayStringList(routing["onCallCandidateUserIds"])),
		WorkflowRunID:          gatewayApprovalRelatedID(request.RelatedIDs, "workflowRunId"),
		ExecutionTaskID:        gatewayApprovalRelatedID(request.RelatedIDs, "executionTaskId"),
		ReleaseBundleID:        gatewayApprovalRelatedID(request.RelatedIDs, "releaseBundleId"),
		Decisions:              gatewayApprovalDecisionTrace(routing),
		StageHistory:           gatewayApprovalStageTrace(routing),
	}
	if trace.ApprovalMode == "<nil>" {
		trace.ApprovalMode = ""
	}
	if stageIndex, ok := gatewayNonNegativeInt(routing["currentStageIndex"]); ok {
		trace.CurrentStageIndex = intPointer(stageIndex)
	} else if stageIndex, ok := gatewayApprovalCurrentStageIndex(routing); ok {
		trace.CurrentStageIndex = intPointer(stageIndex)
	}
	if trace.CurrentStageIndex != nil {
		if trace.StageCount == 0 {
			trace.StageCount = len(gatewayApprovalStages(routing))
		}
	}
	if trace.RequiredApprovals == 0 {
		if required, ok := gatewayApprovalRequiredCount(gatewayApprovalActiveRouting(routing)); ok {
			trace.RequiredApprovals = required
		}
	}
	if trace.ApprovedCount == 0 && len(trace.Decisions) > 0 {
		trace.ApprovedCount = len(gatewayApprovalDecisionUserIDs(gatewayApprovalActiveRouting(routing)))
	}
	return trace
}

func gatewayApprovalDecisionTrace(routing map[string]any) []domainaigateway.ApprovalDecisionTrace {
	decisions := gatewayApprovalDecisions(routing)
	out := make([]domainaigateway.ApprovalDecisionTrace, 0, len(decisions))
	for _, decision := range decisions {
		item := domainaigateway.ApprovalDecisionTrace{
			UserID:    gatewayFirstString(decision, "userId"),
			UserName:  gatewayFirstString(decision, "userName"),
			Roles:     normalizeStringSlice(gatewayStringList(decision["roles"])),
			Teams:     normalizeStringSlice(gatewayStringList(decision["teams"])),
			Result:    gatewayFirstString(decision, "result"),
			Comment:   redactSensitiveText(gatewayFirstString(decision, "comment")),
			StageName: gatewayFirstString(decision, "stageName"),
			DecidedAt: gatewayTimePointer(decision["decidedAt"]),
		}
		if stageIndex, ok := gatewayNonNegativeInt(decision["stageIndex"]); ok {
			item.StageIndex = intPointer(stageIndex)
		}
		out = append(out, item)
	}
	return out
}

func gatewayApprovalStageTrace(routing map[string]any) []domainaigateway.ApprovalStageTrace {
	history := gatewayApprovalStageHistory(routing)
	out := make([]domainaigateway.ApprovalStageTrace, 0, len(history))
	for _, stage := range history {
		item := domainaigateway.ApprovalStageTrace{
			StageName:   gatewayFirstString(stage, "stageName", "name"),
			Result:      gatewayFirstString(stage, "result"),
			CompletedAt: gatewayTimePointer(stage["completedAt"]),
		}
		if stageIndex, ok := gatewayNonNegativeInt(stage["stageIndex"]); ok {
			item.StageIndex = intPointer(stageIndex)
		}
		out = append(out, item)
	}
	return out
}

func gatewayApprovalTimelineEvents(request domainaigateway.ApprovalRequest, audits []domainaigateway.AuditLog) []domainaigateway.ApprovalTimelineEvent {
	events := make([]domainaigateway.ApprovalTimelineEvent, 0, 1+len(audits)+len(gatewayApprovalDecisions(mapValue(request.RelatedIDs["approvalRouting"])))+len(gatewayApprovalStageHistory(mapValue(request.RelatedIDs["approvalRouting"]))))
	if !request.CreatedAt.IsZero() {
		events = append(events, domainaigateway.ApprovalTimelineEvent{
			ID:        request.ID + ":request",
			Kind:      "request",
			Action:    "ai_gateway.approval.request",
			Result:    "pending",
			Summary:   request.Summary,
			ActorType: request.ActorType,
			ActorID:   request.ActorID,
			ActorName: request.ActorName,
			Metadata: map[string]any{
				"toolName":          request.ToolName,
				"riskLevel":         request.RiskLevel,
				"strategy":          request.Strategy,
				"policyId":          request.PolicyID,
				"approvalPolicyRef": request.ApprovalPolicyRef,
			},
			CreatedAt: request.CreatedAt,
		})
	}
	if request.ApprovalTrace != nil {
		for index, decision := range request.ApprovalTrace.Decisions {
			at := request.UpdatedAt
			if decision.DecidedAt != nil {
				at = *decision.DecidedAt
			}
			events = append(events, domainaigateway.ApprovalTimelineEvent{
				ID:         fmt.Sprintf("%s:decision:%d", request.ID, index),
				Kind:       "decision",
				Action:     "ai_gateway.approval.decision",
				Result:     firstNonEmpty(decision.Result, "approved"),
				Summary:    decision.Comment,
				ActorType:  "user",
				ActorID:    decision.UserID,
				ActorName:  decision.UserName,
				StageIndex: decision.StageIndex,
				StageName:  decision.StageName,
				Metadata: map[string]any{
					"roles": decision.Roles,
					"teams": decision.Teams,
				},
				CreatedAt: at,
			})
		}
		for index, stage := range request.ApprovalTrace.StageHistory {
			at := request.UpdatedAt
			if stage.CompletedAt != nil {
				at = *stage.CompletedAt
			}
			events = append(events, domainaigateway.ApprovalTimelineEvent{
				ID:         fmt.Sprintf("%s:stage:%d", request.ID, index),
				Kind:       "stage",
				Action:     "ai_gateway.approval.stage",
				Result:     firstNonEmpty(stage.Result, "approved"),
				Summary:    "AI Gateway approval stage completed",
				StageIndex: stage.StageIndex,
				StageName:  stage.StageName,
				CreatedAt:  at,
			})
		}
	}
	for _, audit := range audits {
		events = append(events, domainaigateway.ApprovalTimelineEvent{
			ID:        audit.ID,
			Kind:      "audit",
			Action:    audit.Action,
			Result:    audit.Result,
			Summary:   audit.Summary,
			ActorType: audit.ActorType,
			ActorID:   audit.ActorID,
			ActorName: audit.ActorName,
			Metadata:  sanitizeGatewayMap(audit.Metadata),
			CreatedAt: audit.CreatedAt,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	return events
}

func gatewayApprovalRelatedID(relatedIDs map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := firstMapString(relatedIDs, key); value != "" {
			return value
		}
	}
	nested := mapValue(relatedIDs["relatedIds"])
	for _, key := range keys {
		if value := firstMapString(nested, key); value != "" {
			return value
		}
	}
	return ""
}

func gatewayApprovalIntCounts(value any) map[string]int {
	values := mapValue(value)
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]int, len(values))
	for key, raw := range values {
		if count := intFromAny(raw); count > 0 {
			out[key] = count
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gatewayTimePointer(value any) *time.Time {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return nil
		}
		parsed := typed.UTC()
		return &parsed
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil
		}
		parsed = parsed.UTC()
		return &parsed
	default:
		return nil
	}
}

func intPointer(value int) *int {
	return &value
}

func (s *Service) recordGatewayApprovalVote(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, input domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, bool, error) {
	relatedIDs, routing := gatewayApprovalRoutingWithVote(request.RelatedIDs, principal, input.Comment, time.Now().UTC())
	status := gatewayApprovalQuorumStatusFromRouting(routing)
	if status.Ready {
		if gatewayApprovalHasNextStage(routing, status) {
			relatedIDs, status = gatewayApprovalAdvanceStage(relatedIDs, status, time.Now().UTC())
			summary := gatewayApprovalStagePendingSummary(status)
			updated, err := s.repo.UpdateApprovalRequest(ctx, request.ID, domainaigateway.ApprovalRequestUpdate{
				ExpectedStatus: "pending",
				Status:         "pending",
				Summary:        summary,
				RelatedIDs:     relatedIDs,
				Output:         request.Output,
				UpdatedAt:      time.Now().UTC(),
			})
			if err != nil {
				return domainaigateway.ApprovalDecisionResult{}, false, err
			}
			_ = s.recordApprovalDecisionAudit(ctx, principal, updated, "ai_gateway.approval.stage", "pending", summary)
			return domainaigateway.ApprovalDecisionResult{Request: updated}, false, nil
		}
		ready := request
		ready.RelatedIDs = relatedIDs
		return domainaigateway.ApprovalDecisionResult{Request: ready}, true, nil
	}
	summary := gatewayApprovalPendingSummary(status)
	updated, err := s.repo.UpdateApprovalRequest(ctx, request.ID, domainaigateway.ApprovalRequestUpdate{
		ExpectedStatus: "pending",
		Status:         "pending",
		Summary:        summary,
		RelatedIDs:     relatedIDs,
		Output:         request.Output,
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		return domainaigateway.ApprovalDecisionResult{}, false, err
	}
	_ = s.recordApprovalDecisionAudit(ctx, principal, updated, "ai_gateway.approval.vote", "pending", summary)
	return domainaigateway.ApprovalDecisionResult{Request: updated}, false, nil
}

func gatewayApprovalRoutingWithVote(relatedIDs map[string]any, principal domainidentity.Principal, comment string, now time.Time) (map[string]any, map[string]any) {
	out := mergeAnyMaps(relatedIDs, nil)
	routing := mergeAnyMaps(mapValue(out["approvalRouting"]), nil)
	decisions := gatewayApprovalDecisions(routing)
	decision := map[string]any{
		"userId":    strings.TrimSpace(principal.UserID),
		"userName":  strings.TrimSpace(principal.UserName),
		"roles":     normalizeStringSlice(principal.Roles),
		"teams":     normalizeStringSlice(principal.Teams),
		"comment":   strings.TrimSpace(comment),
		"decidedAt": now.UTC().Format(time.RFC3339),
		"result":    "approved",
	}
	stageIndex, hasStage := gatewayApprovalCurrentStageIndex(routing)
	if hasStage {
		decision["stageIndex"] = stageIndex
		if stageName := gatewayApprovalCurrentStageName(routing, stageIndex); stageName != "" {
			decision["stageName"] = stageName
		}
	}
	replaced := false
	for index := range decisions {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(decisions[index]["userId"])), principal.UserID) && gatewayApprovalDecisionMatchesStage(decisions[index], stageIndex, hasStage) {
			decisions[index] = decision
			replaced = true
			break
		}
	}
	if !replaced {
		decisions = append(decisions, decision)
	}
	routing["decisions"] = decisions
	status := gatewayApprovalQuorumStatusFromRouting(routing)
	gatewayApplyApprovalStatusToRouting(routing, status)
	out["approvalRouting"] = routing
	return out, routing
}

func gatewayApprovalDecisions(routing map[string]any) []map[string]any {
	raw, ok := routing["decisions"]
	if !ok {
		raw = routing["approvals"]
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]map[string]any); ok {
			return append([]map[string]any(nil), typed...)
		}
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped := mapValue(item); len(mapped) > 0 {
			out = append(out, mergeAnyMaps(mapped, nil))
		}
	}
	return out
}

func gatewayApprovalDecisionUserIDs(routing map[string]any) []string {
	out := make([]string, 0)
	for _, decision := range gatewayApprovalDecisions(routing) {
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprint(decision["result"])), "approved") {
			continue
		}
		if userID := strings.TrimSpace(fmt.Sprint(decision["userId"])); userID != "" && userID != "<nil>" {
			out = gatewayAppendUniqueStrings(out, userID)
		}
	}
	return out
}

func gatewayApprovalDecisionMatchesStage(decision map[string]any, stageIndex int, hasStage bool) bool {
	if !hasStage {
		return true
	}
	value, ok := gatewayNonNegativeInt(decision["stageIndex"])
	if !ok {
		return false
	}
	return value == stageIndex
}

type gatewayApprovalQuorumStatus struct {
	Ready                 bool
	Mode                  string
	RequiredApprovals     int
	HasApprovalQuota      bool
	ApprovedCount         int
	RoleQuotas            map[string]int
	TeamQuotas            map[string]int
	RoleApprovedCounts    map[string]int
	TeamApprovedCounts    map[string]int
	PendingRequirements   []string
	SatisfiedRequirements []string
	StageIndex            int
	StageName             string
	StageCount            int
}

func gatewayApprovalQuorumStatusFromRouting(routing map[string]any) gatewayApprovalQuorumStatus {
	activeRouting := gatewayApprovalActiveRouting(routing)
	stageIndex, hasStage := gatewayApprovalCurrentStageIndex(routing)
	requiredApprovals, hasApprovalQuota := gatewayApprovalRequiredCount(activeRouting)
	status := gatewayApprovalQuorumStatus{
		Mode:               gatewayApprovalMode(activeRouting),
		RequiredApprovals:  requiredApprovals,
		HasApprovalQuota:   hasApprovalQuota,
		ApprovedCount:      len(gatewayApprovalDecisionUserIDs(activeRouting)),
		RoleQuotas:         gatewayApprovalQuotaMap(activeRouting, "requiredRoleApprovals", "minRoleApprovals", "roleApprovalQuorum", "roleApprovals", "roleQuotas", "requiredRoles"),
		TeamQuotas:         gatewayApprovalQuotaMap(activeRouting, "requiredTeamApprovals", "minTeamApprovals", "teamApprovalQuorum", "teamApprovals", "teamQuotas", "requiredTeams"),
		RoleApprovedCounts: map[string]int{},
		TeamApprovedCounts: map[string]int{},
		StageIndex:         stageIndex,
		StageName:          gatewayApprovalCurrentStageName(routing, stageIndex),
		StageCount:         len(gatewayApprovalStages(routing)),
	}
	if status.Mode == "" {
		status.Mode = "all"
	}
	totalQuotaApplies := status.HasApprovalQuota || (status.Mode != "any" || (len(status.RoleQuotas) == 0 && len(status.TeamQuotas) == 0))
	if totalQuotaApplies && status.ApprovedCount >= status.RequiredApprovals {
		status.SatisfiedRequirements = append(status.SatisfiedRequirements, fmt.Sprintf("approvals:%d/%d", status.ApprovedCount, status.RequiredApprovals))
	} else if totalQuotaApplies {
		status.PendingRequirements = append(status.PendingRequirements, fmt.Sprintf("approvals:%d/%d", status.ApprovedCount, status.RequiredApprovals))
	}
	for _, decision := range gatewayApprovalDecisions(activeRouting) {
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprint(decision["result"])), "approved") {
			continue
		}
		for _, role := range normalizeStringSlice(gatewayStringList(decision["roles"])) {
			if _, ok := status.RoleQuotas[role]; ok {
				status.RoleApprovedCounts[role]++
			}
		}
		for _, team := range normalizeStringSlice(gatewayStringList(decision["teams"])) {
			if _, ok := status.TeamQuotas[team]; ok {
				status.TeamApprovedCounts[team]++
			}
		}
	}
	for _, role := range sortedStringKeys(status.RoleQuotas) {
		required := status.RoleQuotas[role]
		if status.RoleApprovedCounts[role] >= required {
			status.SatisfiedRequirements = append(status.SatisfiedRequirements, fmt.Sprintf("role:%s:%d/%d", role, status.RoleApprovedCounts[role], required))
		} else {
			status.PendingRequirements = append(status.PendingRequirements, fmt.Sprintf("role:%s:%d/%d", role, status.RoleApprovedCounts[role], required))
		}
	}
	for _, team := range sortedStringKeys(status.TeamQuotas) {
		required := status.TeamQuotas[team]
		if status.TeamApprovedCounts[team] >= required {
			status.SatisfiedRequirements = append(status.SatisfiedRequirements, fmt.Sprintf("team:%s:%d/%d", team, status.TeamApprovedCounts[team], required))
		} else {
			status.PendingRequirements = append(status.PendingRequirements, fmt.Sprintf("team:%s:%d/%d", team, status.TeamApprovedCounts[team], required))
		}
	}
	switch status.Mode {
	case "any":
		status.Ready = len(status.SatisfiedRequirements) > 0
	default:
		status.Ready = len(status.PendingRequirements) == 0
	}
	if hasStage {
		status.SatisfiedRequirements = gatewayPrefixApprovalRequirements(status.SatisfiedRequirements, fmt.Sprintf("stage:%d", status.StageIndex))
		status.PendingRequirements = gatewayPrefixApprovalRequirements(status.PendingRequirements, fmt.Sprintf("stage:%d", status.StageIndex))
	}
	return status
}

func gatewayApprovalPendingSummary(status gatewayApprovalQuorumStatus) string {
	if len(status.PendingRequirements) == 0 {
		return fmt.Sprintf("AI Gateway approval request approved by %d/%d candidates", status.ApprovedCount, status.RequiredApprovals)
	}
	return fmt.Sprintf("AI Gateway approval request pending %s quorum: %s", status.Mode, strings.Join(status.PendingRequirements, ", "))
}

func gatewayApprovalMode(values map[string]any) string {
	text := strings.ToLower(gatewayFirstString(values, "approvalMode", "approvalType", "quorumMode", "decisionMode", "mode"))
	switch text {
	case "any", "or", "anyof", "any_of", "oneof", "one_of", "atleastone", "at_least_one":
		return "any"
	case "all", "and", "allof", "all_of", "unanimous", "joint", "jointapproval", "joint_approval":
		return "all"
	default:
		return ""
	}
}

func gatewayApplyApprovalStatusToRouting(routing map[string]any, status gatewayApprovalQuorumStatus) {
	routing["approvedCount"] = status.ApprovedCount
	routing["requiredApprovals"] = status.RequiredApprovals
	if len(status.RoleApprovedCounts) > 0 {
		routing["roleApprovedCounts"] = gatewayIntMapToAnyMap(status.RoleApprovedCounts)
	} else {
		delete(routing, "roleApprovedCounts")
	}
	if len(status.TeamApprovedCounts) > 0 {
		routing["teamApprovedCounts"] = gatewayIntMapToAnyMap(status.TeamApprovedCounts)
	} else {
		delete(routing, "teamApprovedCounts")
	}
	if len(status.PendingRequirements) > 0 {
		routing["pendingRequirements"] = status.PendingRequirements
	} else {
		delete(routing, "pendingRequirements")
	}
	if len(status.SatisfiedRequirements) > 0 {
		routing["satisfiedRequirements"] = status.SatisfiedRequirements
	} else {
		delete(routing, "satisfiedRequirements")
	}
	routing["approvalMode"] = status.Mode
	if status.StageCount > 0 {
		routing["currentStageIndex"] = status.StageIndex
		if status.StageName != "" {
			routing["currentStageName"] = status.StageName
		}
		routing["stageCount"] = status.StageCount
	}
}

func gatewayPrefixApprovalRequirements(values []string, prefix string) []string {
	if prefix == "" || len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, prefix+":"+value)
	}
	return out
}

func gatewayApprovalActiveRouting(routing map[string]any) map[string]any {
	active := mergeAnyMaps(routing, nil)
	stageIndex, ok := gatewayApprovalCurrentStageIndex(routing)
	if !ok {
		return active
	}
	stage, ok := gatewayApprovalStageAt(routing, stageIndex)
	if !ok {
		return active
	}
	for _, key := range []string{"candidateUserIds", "candidateRoles", "candidateTeams", "approvalMode", "requiredApprovals", "requiredRoleApprovals", "requiredTeamApprovals"} {
		delete(active, key)
	}
	for key, value := range stage {
		active[key] = value
	}
	decisions := make([]map[string]any, 0)
	for _, decision := range gatewayApprovalDecisions(routing) {
		if gatewayApprovalDecisionMatchesStage(decision, stageIndex, true) {
			decisions = append(decisions, decision)
		}
	}
	active["decisions"] = decisions
	return active
}

func gatewayApprovalStagesFromValues(values map[string]any) []map[string]any {
	var raw any
	for _, key := range []string{"approvalStages", "stages", "approvalSteps", "steps"} {
		value, ok := gatewayConditionRaw(values, key)
		if ok {
			raw = value
			break
		}
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for index, item := range items {
		mapped := gatewayApprovalStageFromValue(item, index)
		if len(mapped) > 0 {
			out = append(out, mapped)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gatewayApprovalStageFromValue(value any, index int) map[string]any {
	values := mapValue(value)
	if len(values) == 0 {
		return nil
	}
	stage := map[string]any{
		"index": index,
	}
	if name := gatewayFirstString(values, "name", "id", "stage", "label"); name != "" {
		stage["name"] = name
	}
	if users := normalizeStringSlice(gatewayConditionStringList(values, "approverUsers", "approverUserIds", "candidateUsers", "candidateUserIds", "approvalUsers", "approvalUserIds", "userIds", "users")); len(users) > 0 {
		stage["candidateUserIds"] = users
	}
	roles := normalizeStringSlice(gatewayConditionStringList(values, "approverRoles", "candidateRoles", "approvalRoles", "roles", "roleIds"))
	if roleQuotas := gatewayApprovalQuotaMap(values, "requiredRoleApprovals", "minRoleApprovals", "roleApprovalQuorum", "roleApprovals", "roleQuotas", "requiredRoles"); len(roleQuotas) > 0 {
		stage["requiredRoleApprovals"] = roleQuotas
		roles = gatewayAppendUniqueStrings(roles, gatewayApprovalQuotaNames(roleQuotas)...)
	}
	if len(roles) > 0 {
		stage["candidateRoles"] = normalizeStringSlice(roles)
	}
	teams := normalizeStringSlice(gatewayConditionStringList(values, "approverTeams", "candidateTeams", "approvalTeams", "teams", "teamIds", "groups", "groupIds"))
	if teamQuotas := gatewayApprovalQuotaMap(values, "requiredTeamApprovals", "minTeamApprovals", "teamApprovalQuorum", "teamApprovals", "teamQuotas", "requiredTeams"); len(teamQuotas) > 0 {
		stage["requiredTeamApprovals"] = teamQuotas
		teams = gatewayAppendUniqueStrings(teams, gatewayApprovalQuotaNames(teamQuotas)...)
	}
	if len(teams) > 0 {
		stage["candidateTeams"] = teams
	}
	if mode := gatewayApprovalMode(values); mode != "" {
		stage["approvalMode"] = mode
	}
	if required, ok := gatewayFirstPositiveInt(values, "requiredApprovals", "minApprovals", "approvalQuorum", "quorum", "minApproverCount"); ok {
		stage["requiredApprovals"] = required
	}
	if changeWindow := gatewayApprovalChangeWindow(values); len(changeWindow) > 0 {
		stage["changeWindow"] = changeWindow
	}
	if len(stage) == 1 {
		return nil
	}
	return stage
}

func gatewayApprovalStages(routing map[string]any) []map[string]any {
	raw, ok := routing["approvalStages"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]map[string]any); ok {
			return append([]map[string]any(nil), typed...)
		}
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped := mapValue(item); len(mapped) > 0 {
			out = append(out, mergeAnyMaps(mapped, nil))
		}
	}
	return out
}

func gatewayApprovalCurrentStageIndex(routing map[string]any) (int, bool) {
	if len(gatewayApprovalStages(routing)) == 0 {
		return 0, false
	}
	if value, ok := gatewayNonNegativeInt(routing["currentStageIndex"]); ok {
		return value, true
	}
	return 0, true
}

func gatewayApprovalCurrentStageName(routing map[string]any, stageIndex int) string {
	stage, ok := gatewayApprovalStageAt(routing, stageIndex)
	if !ok {
		return ""
	}
	return gatewayFirstString(stage, "name", "id", "stage", "label")
}

func gatewayApprovalStageAt(routing map[string]any, stageIndex int) (map[string]any, bool) {
	stages := gatewayApprovalStages(routing)
	if stageIndex < 0 || stageIndex >= len(stages) {
		return nil, false
	}
	return mergeAnyMaps(stages[stageIndex], nil), true
}

func gatewayApprovalHasNextStage(routing map[string]any, status gatewayApprovalQuorumStatus) bool {
	return status.StageCount > 0 && status.StageIndex+1 < status.StageCount
}

func gatewayApprovalAdvanceStage(relatedIDs map[string]any, status gatewayApprovalQuorumStatus, now time.Time) (map[string]any, gatewayApprovalQuorumStatus) {
	out := mergeAnyMaps(relatedIDs, nil)
	routing := mergeAnyMaps(mapValue(out["approvalRouting"]), nil)
	nextStageIndex := status.StageIndex + 1
	routing["currentStageIndex"] = nextStageIndex
	if stageName := gatewayApprovalCurrentStageName(routing, nextStageIndex); stageName != "" {
		routing["currentStageName"] = stageName
	} else {
		delete(routing, "currentStageName")
	}
	history := gatewayApprovalStageHistory(routing)
	history = append(history, map[string]any{
		"stageIndex":  status.StageIndex,
		"stageName":   status.StageName,
		"completedAt": now.UTC().Format(time.RFC3339),
		"result":      "approved",
	})
	routing["stageHistory"] = history
	nextStatus := gatewayApprovalQuorumStatusFromRouting(routing)
	gatewayApplyApprovalStatusToRouting(routing, nextStatus)
	out["approvalRouting"] = routing
	return out, nextStatus
}

func gatewayApprovalStageHistory(routing map[string]any) []map[string]any {
	raw, ok := routing["stageHistory"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]map[string]any); ok {
			return append([]map[string]any(nil), typed...)
		}
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped := mapValue(item); len(mapped) > 0 {
			out = append(out, mergeAnyMaps(mapped, nil))
		}
	}
	return out
}

func gatewayApprovalStagePendingSummary(status gatewayApprovalQuorumStatus) string {
	stage := fmt.Sprintf("%d", status.StageIndex)
	if status.StageName != "" {
		stage = status.StageName
	}
	return fmt.Sprintf("AI Gateway approval request advanced to stage %s: %s", stage, gatewayApprovalPendingSummary(status))
}

func gatewayApprovalQuotaMap(values map[string]any, keys ...string) map[string]int {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		if quotas := gatewayApprovalQuotaMapFromValue(raw); len(quotas) > 0 {
			return quotas
		}
	}
	return nil
}

func gatewayApprovalQuotaMapFromValue(value any) map[string]int {
	out := map[string]int{}
	switch typed := value.(type) {
	case map[string]any:
		for key, raw := range typed {
			name := strings.TrimSpace(key)
			required, ok := gatewayPositiveInt(raw)
			if name != "" && ok {
				out[name] = required
			}
		}
	case map[string]int:
		for key, required := range typed {
			if name := strings.TrimSpace(key); name != "" && required > 0 {
				out[name] = required
			}
		}
	case []any:
		for _, item := range typed {
			if mapped := mapValue(item); len(mapped) > 0 {
				name := gatewayFirstString(mapped, "name", "id", "role", "roleId", "team", "teamId", "group", "groupId")
				required, ok := gatewayFirstPositiveInt(mapped, "requiredApprovals", "minApprovals", "required", "count", "quorum")
				if name != "" && ok {
					out[name] = required
				}
				continue
			}
			name := strings.TrimSpace(fmt.Sprint(item))
			if name != "" && name != "<nil>" {
				out[name] = 1
			}
		}
	case []string:
		for _, item := range typed {
			if name := strings.TrimSpace(item); name != "" {
				out[name] = 1
			}
		}
	case string:
		for _, item := range gatewayStringList(typed) {
			if name := strings.TrimSpace(item); name != "" {
				out[name] = 1
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gatewayApprovalQuotaNames(values map[string]int) []string {
	names := make([]string, 0, len(values))
	for _, key := range sortedStringKeys(values) {
		if strings.TrimSpace(key) != "" {
			names = append(names, key)
		}
	}
	return names
}

func gatewayIntMapToAnyMap(values map[string]int) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func sortedStringKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func gatewayApprovalRequiredCount(routing map[string]any) (int, bool) {
	required, ok := gatewayFirstPositiveInt(routing, "requiredApprovals", "minApprovals", "approvalQuorum", "quorum", "minApproverCount")
	if !ok || required <= 1 {
		return 1, ok
	}
	candidateCount := len(gatewayAppendUniqueStrings(nil, gatewayStringList(routing["candidateUserIds"])...))
	if candidateCount > 0 && required > candidateCount {
		return candidateCount, true
	}
	return required, true
}

func gatewayApprovalPrincipalMatchesRouting(principal domainidentity.Principal, routing map[string]any) bool {
	users := normalizeStringSlice(gatewayStringList(routing["candidateUserIds"]))
	roles := normalizeStringSlice(gatewayStringList(routing["candidateRoles"]))
	teams := normalizeStringSlice(gatewayStringList(routing["candidateTeams"]))
	if len(users) == 0 && len(roles) == 0 && len(teams) == 0 {
		return true
	}
	if slices.Contains(users, strings.TrimSpace(principal.UserID)) {
		return true
	}
	if len(intersectStringSlices(roles, principal.Roles)) > 0 {
		return true
	}
	if len(intersectStringSlices(teams, principal.Teams)) > 0 {
		return true
	}
	return false
}

func gatewayApprovalChangeWindowAllows(routing map[string]any, now time.Time) error {
	window := mapValue(routing["changeWindow"])
	if len(window) == 0 {
		return nil
	}
	if startsAt, ok := parseGatewayApprovalWindowTime(window["startsAt"]); ok && now.Before(startsAt) {
		return fmt.Errorf("%w: approval request is outside the configured change window", apperrors.ErrAccessDenied)
	}
	if endsAt, ok := parseGatewayApprovalWindowTime(window["endsAt"]); ok && now.After(endsAt) {
		return fmt.Errorf("%w: approval request is outside the configured change window", apperrors.ErrAccessDenied)
	}
	return nil
}

func parseGatewayApprovalWindowTime(value any) (time.Time, bool) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func approvalPolicyRiskStrategy(policy map[string]any) (gatewayRiskStrategy, bool) {
	if len(policy) == 0 {
		return "", false
	}
	for _, key := range []string{"strategy", "mode", "action", "approval", "state"} {
		raw, exists := policy[key]
		if !exists {
			continue
		}
		if strategy, ok := parseGatewayRiskStrategy(fmt.Sprint(raw)); ok {
			return strategy, true
		}
	}
	if boolFromAny(policy["dryRunOnly"]) {
		return gatewayRiskDryRunOnly, true
	}
	if boolFromAny(policy["requiresHumanConfirm"]) || boolFromAny(policy["humanConfirmRequired"]) {
		return gatewayRiskRequireHumanConfirm, true
	}
	if boolFromAny(policy["requiresApproval"]) {
		return gatewayRiskRequireApproval, true
	}
	return "", false
}

func approvalPolicyRef(policy map[string]any) string {
	for _, key := range []string{"approvalPolicyRef", "approvalPolicyId", "deliveryApprovalPolicyId", "policyRef", "policyKey"} {
		if value := strings.TrimSpace(fmt.Sprint(policy[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func parseGatewayRiskStrategy(value string) (gatewayRiskStrategy, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "", "none", "not_required":
		return "", true
	case "allow", "direct", "execute":
		return gatewayRiskAllow, true
	case "deny", "block", "blocked", "forbid", "forbidden":
		return gatewayRiskDeny, true
	case "require_approval", "required", "approval_required", "approval", "pending_approval":
		return gatewayRiskRequireApproval, true
	case "require_human_confirm", "human_confirm", "human_confirmation", "confirm", "confirmation_required", "pending_human_confirm":
		return gatewayRiskRequireHumanConfirm, true
	case "dry_run_only", "dry_run", "dryrun", "preview_only":
		return gatewayRiskDryRunOnly, true
	default:
		return "", false
	}
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.EqualFold(strings.TrimSpace(typed), "yes")
	default:
		return false
	}
}

func filterSkillsByBindings(skills []domainaigateway.SkillCapability, bindings []domainaigateway.SkillBinding) ([]domainaigateway.SkillCapability, int) {
	return filterSkillsByBindingsWithSkills(skills, bindings, defaultSkills())
}

func filterSkillsByBindingsWithSkills(skills []domainaigateway.SkillCapability, bindings []domainaigateway.SkillBinding, knownSkills []domainaigateway.SkillCapability) ([]domainaigateway.SkillCapability, int) {
	if len(bindings) == 0 {
		return skills, 0
	}
	bindingRefs := skillBindingRefsWithSkills(bindings, "", knownSkills)
	out := make([]domainaigateway.SkillCapability, 0, len(skills))
	denied := 0
	for _, skill := range skills {
		refs, ok := bindingRefs[skill.ID]
		if !ok {
			denied++
			continue
		}
		if len(refs) > 0 {
			skill.CapabilityRefs = intersectStringSlices(skill.CapabilityRefs, refs)
		}
		out = append(out, skill)
	}
	return out, denied
}

func filterToolsBySkillBindings(tools []domainaigateway.ToolCapability, bindings []domainaigateway.SkillBinding, skillID string) ([]domainaigateway.ToolCapability, int) {
	return filterToolsBySkillBindingsWithSkills(tools, bindings, skillID, defaultSkills())
}

func filterToolsBySkillBindingsWithSkills(tools []domainaigateway.ToolCapability, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) ([]domainaigateway.ToolCapability, int) {
	if len(bindings) == 0 {
		return tools, 0
	}
	allowedRefs := allowedCapabilityRefsForBindingsWithSkills(bindings, skillID, knownSkills)
	out := make([]domainaigateway.ToolCapability, 0, len(tools))
	denied := 0
	for _, tool := range tools {
		if matchesToolPatternList(allowedRefs, tool.Name) {
			out = append(out, tool)
			continue
		}
		denied++
	}
	return out, denied
}

func filterResourcesBySkillBindings(resources []domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string) ([]domainaigateway.ResourceCapability, int) {
	return filterResourcesBySkillBindingsWithSkills(resources, bindings, skillID, defaultSkills())
}

func filterResourcesBySkillBindingsWithSkills(resources []domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) ([]domainaigateway.ResourceCapability, int) {
	return filterResourcesBySkillBindingsWithCapabilities(resources, bindings, skillID, defaultResourceCapabilityRefs(), knownSkills)
}

func filterResourcesBySkillBindingsWithCapabilities(resources []domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string, resourceRefs []ResourceCapabilityRefs, knownSkills []domainaigateway.SkillCapability) ([]domainaigateway.ResourceCapability, int) {
	if len(bindings) == 0 {
		return resources, 0
	}
	out := make([]domainaigateway.ResourceCapability, 0, len(resources))
	denied := 0
	for _, resource := range resources {
		refs := resourceCapabilityRefsFrom(resourceRefs, resource.Name)
		if resourceAllowedBySkillBindingsWithRefs(resource, bindings, skillID, refs.Tools, knownSkills) {
			out = append(out, resource)
			continue
		}
		denied++
	}
	return out, denied
}

func filterPromptsBySkillBindings(prompts []domainaigateway.PromptCapability, bindings []domainaigateway.SkillBinding, skillID string) ([]domainaigateway.PromptCapability, int) {
	return filterPromptsBySkillBindingsWithCapabilities(prompts, bindings, skillID, defaultResources(), defaultResourceCapabilityRefs(), defaultSkills())
}

func filterPromptsBySkillBindingsWithCapabilities(prompts []domainaigateway.PromptCapability, bindings []domainaigateway.SkillBinding, skillID string, knownResources []domainaigateway.ResourceCapability, resourceRefs []ResourceCapabilityRefs, knownSkills []domainaigateway.SkillCapability) ([]domainaigateway.PromptCapability, int) {
	if len(bindings) == 0 {
		return prompts, 0
	}
	out := make([]domainaigateway.PromptCapability, 0, len(prompts))
	denied := 0
	for _, prompt := range prompts {
		if promptAllowedBySkillBindingsWithCapabilities(prompt, bindings, skillID, knownResources, resourceRefs, knownSkills) {
			out = append(out, prompt)
			continue
		}
		denied++
	}
	return out, denied
}

func toolAllowedBySkillBindings(tool domainaigateway.ToolCapability, bindings []domainaigateway.SkillBinding, skillID string) (bool, string) {
	return toolAllowedBySkillBindingsWithSkills(tool, bindings, skillID, defaultSkills())
}

func toolAllowedBySkillBindingsWithSkills(tool domainaigateway.ToolCapability, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) (bool, string) {
	allowedRefs := allowedCapabilityRefsForBindingsWithSkills(bindings, skillID, knownSkills)
	if len(allowedRefs) == 0 {
		if strings.TrimSpace(skillID) != "" {
			return false, "skill is not bound"
		}
		return false, "no capabilities are bound"
	}
	if matchesToolPatternList(allowedRefs, tool.Name) {
		return true, ""
	}
	if strings.TrimSpace(skillID) != "" {
		return false, "tool is outside bound skill capabilities"
	}
	return false, "tool is outside bound capability refs"
}

func authorizeResourceSkillBinding(resource domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string) error {
	return authorizeResourceSkillBindingWithSkills(resource, bindings, skillID, defaultSkills())
}

func authorizeResourceSkillBindingWithSkills(resource domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) error {
	return authorizeResourceSkillBindingWithRefs(resource, bindings, skillID, resourceToolRefs(resource.Name), knownSkills)
}

func authorizeResourceSkillBindingWithRefs(resource domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string, toolRefs []string, knownSkills []domainaigateway.SkillCapability) error {
	if len(bindings) == 0 {
		return nil
	}
	if resourceAllowedBySkillBindingsWithRefs(resource, bindings, skillID, toolRefs, knownSkills) {
		return nil
	}
	return fmt.Errorf("%w: AI Gateway skill binding rejected %s: resource is outside bound capability refs", apperrors.ErrAccessDenied, resource.Name)
}

func authorizePromptSkillBinding(prompt domainaigateway.PromptCapability, bindings []domainaigateway.SkillBinding, skillID string) error {
	return authorizePromptSkillBindingWithCapabilities(prompt, bindings, skillID, defaultResources(), defaultResourceCapabilityRefs(), defaultSkills())
}

func authorizePromptSkillBindingWithCapabilities(prompt domainaigateway.PromptCapability, bindings []domainaigateway.SkillBinding, skillID string, knownResources []domainaigateway.ResourceCapability, resourceRefs []ResourceCapabilityRefs, knownSkills []domainaigateway.SkillCapability) error {
	if len(bindings) == 0 {
		return nil
	}
	if promptAllowedBySkillBindingsWithCapabilities(prompt, bindings, skillID, knownResources, resourceRefs, knownSkills) {
		return nil
	}
	return fmt.Errorf("%w: AI Gateway skill binding rejected %s: prompt is outside bound capability refs", apperrors.ErrAccessDenied, prompt.Name)
}

func resourceAllowedBySkillBindings(resource domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string) bool {
	return resourceAllowedBySkillBindingsWithSkills(resource, bindings, skillID, defaultSkills())
}

func resourceAllowedBySkillBindingsWithSkills(resource domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) bool {
	return resourceAllowedBySkillBindingsWithRefs(resource, bindings, skillID, resourceToolRefs(resource.Name), knownSkills)
}

func resourceAllowedBySkillBindingsWithRefs(_ domainaigateway.ResourceCapability, bindings []domainaigateway.SkillBinding, skillID string, toolRefs []string, knownSkills []domainaigateway.SkillCapability) bool {
	return capabilityRefsAllowedBySkillBindingsWithSkills(toolRefs, bindings, skillID, knownSkills)
}

func promptAllowedBySkillBindings(prompt domainaigateway.PromptCapability, bindings []domainaigateway.SkillBinding, skillID string) bool {
	return promptAllowedBySkillBindingsWithCapabilities(prompt, bindings, skillID, defaultResources(), defaultResourceCapabilityRefs(), defaultSkills())
}

func promptAllowedBySkillBindingsWithCapabilities(prompt domainaigateway.PromptCapability, bindings []domainaigateway.SkillBinding, skillID string, knownResources []domainaigateway.ResourceCapability, resourceRefs []ResourceCapabilityRefs, knownSkills []domainaigateway.SkillCapability) bool {
	for _, resource := range knownResources {
		refs := resourceCapabilityRefsFrom(resourceRefs, resource.Name)
		if !slices.Contains(refs.Prompts, prompt.Name) {
			continue
		}
		if resourceAllowedBySkillBindingsWithRefs(resource, bindings, skillID, refs.Tools, knownSkills) {
			return true
		}
	}
	return false
}

func capabilityRefsAllowedBySkillBindings(capabilityRefs []string, bindings []domainaigateway.SkillBinding, skillID string) bool {
	return capabilityRefsAllowedBySkillBindingsWithSkills(capabilityRefs, bindings, skillID, defaultSkills())
}

func capabilityRefsAllowedBySkillBindingsWithSkills(capabilityRefs []string, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) bool {
	allowedRefs := allowedCapabilityRefsForBindingsWithSkills(bindings, skillID, knownSkills)
	if len(allowedRefs) == 0 || len(capabilityRefs) == 0 {
		return false
	}
	for _, ref := range capabilityRefs {
		if matchesToolPatternList(allowedRefs, ref) {
			return true
		}
	}
	return false
}

func filterToolRefsBySkillBindings(refs []string, bindings []domainaigateway.SkillBinding, skillID string) []string {
	return filterToolRefsBySkillBindingsWithSkills(refs, bindings, skillID, defaultSkills())
}

func filterToolRefsBySkillBindingsWithSkills(refs []string, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) []string {
	if len(bindings) == 0 {
		return refs
	}
	allowedRefs := allowedCapabilityRefsForBindingsWithSkills(bindings, skillID, knownSkills)
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if matchesToolPatternList(allowedRefs, ref) {
			out = append(out, ref)
		}
	}
	return out
}

func filterPromptRefsBySkillBindings(refs []string, bindings []domainaigateway.SkillBinding, skillID string) []string {
	return filterPromptRefsBySkillBindingsWithCapabilities(refs, bindings, skillID, defaultResources(), defaultResourceCapabilityRefs(), defaultSkills())
}

func filterPromptRefsBySkillBindingsWithCapabilities(refs []string, bindings []domainaigateway.SkillBinding, skillID string, knownResources []domainaigateway.ResourceCapability, resourceRefs []ResourceCapabilityRefs, knownSkills []domainaigateway.SkillCapability) []string {
	if len(bindings) == 0 {
		return refs
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if promptAllowedBySkillBindingsWithCapabilities(domainaigateway.PromptCapability{Name: ref}, bindings, skillID, knownResources, resourceRefs, knownSkills) {
			out = append(out, ref)
		}
	}
	return out
}

func filterPromptRefsBySkillBindingsWithResourceRefs(refs []string, bindings []domainaigateway.SkillBinding, skillID string, resourceRefs ResourceCapabilityRefs, knownSkills []domainaigateway.SkillCapability) []string {
	if len(bindings) == 0 {
		return refs
	}
	if !capabilityRefsAllowedBySkillBindingsWithSkills(resourceRefs.Tools, bindings, skillID, knownSkills) {
		return nil
	}
	allowedPrompts := normalizeStringSlice(resourceRefs.Prompts)
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if slices.Contains(allowedPrompts, strings.TrimSpace(ref)) {
			out = append(out, ref)
		}
	}
	return out
}

func filterSkillRefsByBindings(refs []string, bindings []domainaigateway.SkillBinding, skillID string) []string {
	return filterSkillRefsByBindingsWithSkills(refs, bindings, skillID, defaultSkills())
}

func filterSkillRefsByBindingsWithSkills(refs []string, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) []string {
	if len(bindings) == 0 {
		return refs
	}
	allowed := skillBindingRefsWithSkills(bindings, skillID, knownSkills)
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if _, ok := allowed[ref]; ok {
			out = append(out, ref)
		}
	}
	return out
}

func allowedCapabilityRefsForBindings(bindings []domainaigateway.SkillBinding, skillID string) []string {
	return allowedCapabilityRefsForBindingsWithSkills(bindings, skillID, defaultSkills())
}

func allowedCapabilityRefsForBindingsWithSkills(bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) []string {
	refsBySkill := skillBindingRefsWithSkills(bindings, skillID, knownSkills)
	out := make([]string, 0)
	for _, refs := range refsBySkill {
		out = append(out, refs...)
	}
	return normalizeStringSlice(out)
}

func skillBindingRefs(bindings []domainaigateway.SkillBinding, skillID string) map[string][]string {
	return skillBindingRefsWithSkills(bindings, skillID, defaultSkills())
}

func skillBindingRefsWithSkills(bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) map[string][]string {
	skillID = strings.TrimSpace(skillID)
	out := map[string][]string{}
	for _, binding := range bindings {
		if !binding.Enabled {
			continue
		}
		bindingSkillID := strings.TrimSpace(binding.SkillID)
		if bindingSkillID == "" || (skillID != "" && bindingSkillID != skillID) {
			continue
		}
		refs := normalizeStringSlice(binding.CapabilityRefs)
		if len(refs) == 0 {
			if skill, ok := skillByIDFrom(bindingSkillID, knownSkills); ok {
				refs = normalizeStringSlice(skill.CapabilityRefs)
			}
		}
		out[bindingSkillID] = normalizeStringSlice(append(out[bindingSkillID], refs...))
	}
	return out
}

func toolInAnySkill(skillIDs []string, toolName string) bool {
	return toolInAnySkillWithSkills(skillIDs, toolName, defaultSkills())
}

func toolInAnySkillWithSkills(skillIDs []string, toolName string, knownSkills []domainaigateway.SkillCapability) bool {
	for _, skillID := range skillIDs {
		skillID = strings.TrimSpace(skillID)
		if skillID == "" {
			continue
		}
		if skillID == "*" {
			return true
		}
		skill, ok := skillByIDFrom(skillID, knownSkills)
		if !ok {
			continue
		}
		if matchesToolPatternList(skill.CapabilityRefs, toolName) {
			return true
		}
	}
	return false
}

func skillByIDFrom(id string, skills []domainaigateway.SkillCapability) (domainaigateway.SkillCapability, bool) {
	id = strings.TrimSpace(id)
	for _, item := range skills {
		if item.ID == id {
			return item, true
		}
	}
	return domainaigateway.SkillCapability{}, false
}

func grantEffect(effect string) string {
	effect = strings.ToLower(strings.TrimSpace(effect))
	if effect == "" {
		return "allow"
	}
	return effect
}

func matchesToolPatternList(patterns []string, toolName string) bool {
	for _, pattern := range patterns {
		if grantMatchesTool(pattern, toolName) {
			return true
		}
	}
	return false
}

func matchesStringPatternList(patterns []string, value string) bool {
	value = strings.TrimSpace(value)
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "*" || pattern == value {
			return true
		}
	}
	return false
}

func grantMatchesTool(pattern, toolName string) bool {
	pattern = strings.TrimSpace(pattern)
	toolName = strings.TrimSpace(toolName)
	if pattern == "" {
		return false
	}
	if pattern == "*" || pattern == toolName {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		return strings.HasPrefix(toolName, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

func gatewaySubject(principal domainidentity.Principal) (string, string) {
	userID := strings.TrimSpace(principal.UserID)
	if strings.HasPrefix(userID, "service_account:") {
		return "service_account", strings.TrimPrefix(userID, "service_account:")
	}
	return "user", userID
}

func callerContext(input domainaigateway.ManifestRequest) domainaigateway.CallerContext {
	identityMode := strings.TrimSpace(input.TokenKind)
	if identityMode == "" {
		identityMode = "user_session"
	}
	return domainaigateway.CallerContext{
		IdentityMode: identityMode,
		AIClientID:   strings.TrimSpace(input.AIClientID),
		AIClientName: strings.TrimSpace(input.AIClientName),
		SkillID:      strings.TrimSpace(input.SkillID),
		TokenID:      strings.TrimSpace(input.TokenID),
		SessionID:    strings.TrimSpace(input.SessionID),
		SubjectType:  strings.TrimSpace(input.SubjectType),
		SubjectID:    strings.TrimSpace(input.SubjectID),
		Source:       strings.TrimSpace(input.Source),
	}
}

func approvalRequestPrincipal(request domainaigateway.ApprovalRequest) domainidentity.Principal {
	userID := strings.TrimSpace(request.ActorID)
	if request.ActorType == "service_account" && userID != "" && !strings.HasPrefix(userID, "service_account:") {
		userID = "service_account:" + userID
	}
	return domainidentity.Principal{
		UserID:   userID,
		UserName: request.ActorName,
		Roles:    append([]string(nil), request.ActorRoles...),
		Teams:    append([]string(nil), request.ActorTeams...),
	}
}

func approvalRequestInvocationInput(request domainaigateway.ApprovalRequest) domainaigateway.ToolInvocationRequest {
	return domainaigateway.ToolInvocationRequest{
		ToolName:     request.ToolName,
		Input:        request.ToolInput,
		AIClientID:   request.AIClientID,
		AIClientName: request.AIClientName,
		SkillID:      request.SkillID,
		RequestID:    request.RequestID,
	}
}

func (s *Service) recordManifestAudit(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ManifestRequest, result, summary string, allowedCount, deniedCount int) error {
	if s == nil || s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  "AIGatewayManifest",
		Action:        "ai_gateway.capabilities",
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"aiClientId":    strings.TrimSpace(input.AIClientID),
			"aiClientName":  strings.TrimSpace(input.AIClientName),
			"skillId":       strings.TrimSpace(input.SkillID),
			"source":        firstNonEmpty(strings.TrimSpace(input.Source), meta.Source),
			"allowedCount":  allowedCount,
			"deniedCount":   deniedCount,
			"manifest":      manifestVersion,
			"requestClient": meta.UserAgent,
		},
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func generateOpaqueToken(prefix string) (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate token entropy: %w", err)
	}
	value := prefix + base64.RawURLEncoding.EncodeToString(raw)
	tokenPrefix := value
	if len(tokenPrefix) > 20 {
		tokenPrefix = tokenPrefix[:20]
	}
	return value, tokenPrefix, nil
}

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(out, value) {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func intersectStringSlices(left, right []string) []string {
	normalizedRight := normalizeStringSlice(right)
	out := make([]string, 0)
	for _, value := range normalizeStringSlice(left) {
		if slices.Contains(normalizedRight, value) {
			out = append(out, value)
		}
	}
	return out
}

func emptyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	return values
}

func copyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func rotatedTokenExpiresAt(requested, previous *time.Time, now time.Time) (*time.Time, error) {
	if requested != nil {
		if !requested.After(now) {
			return nil, fmt.Errorf("%w: replacement token expiration must be in the future", apperrors.ErrInvalidArgument)
		}
		next := requested.UTC()
		return &next, nil
	}
	if previous == nil {
		return nil, nil
	}
	if previous.After(now) {
		next := previous.UTC()
		return &next, nil
	}
	next := now.Add(rotatedTokenDefaultTTL).UTC()
	return &next, nil
}

func buildSourceBindingUsage(detail domaindelivery.ApplicationDetail) []map[string]any {
	items := make([]map[string]any, 0, len(detail.Bindings))
	for _, binding := range detail.Bindings {
		items = append(items, map[string]any{
			"applicationEnvironmentId": binding.ApplicationEnvironmentID,
			"environmentId":            binding.EnvironmentID,
			"environmentName":          binding.EnvironmentName,
			"environmentKey":           binding.EnvironmentKey,
			"buildSourceId":            binding.BuildSourceID,
			"buildPolicy":              redactedBuildPolicy(binding.BuildPolicy),
			"latestBundleId":           optionalReleaseBundleID(binding.LatestBundle),
			"latestExecutionTaskId":    optionalExecutionTaskID(binding.LatestExecutionTask),
		})
	}
	return items
}

func redactedApplication(app domainapp.App) domainapp.App {
	app.Metadata = redactMap(app.Metadata)
	app.BuildSources = redactedBuildSources(app.BuildSources)
	return app
}

func redactedBuildSources(items []domainapp.BuildSource) []domainapp.BuildSource {
	out := make([]domainapp.BuildSource, len(items))
	copy(out, items)
	for index := range out {
		out[index].Config = redactMap(out[index].Config)
	}
	return out
}

func redactedApplicationServices(items []domainapp.Service) []domainapp.Service {
	out := make([]domainapp.Service, len(items))
	copy(out, items)
	for index := range out {
		out[index].Metadata = redactMap(out[index].Metadata)
		out[index].Containers = redactedServiceContainers(out[index].Containers)
	}
	return out
}

func redactedServiceContainers(items []domainapp.ServiceContainer) []domainapp.ServiceContainer {
	out := make([]domainapp.ServiceContainer, len(items))
	copy(out, items)
	for index := range out {
		out[index].EnvSchema = redactMap(out[index].EnvSchema)
		out[index].ResourceProfile = redactMap(out[index].ResourceProfile)
		out[index].HealthCheck = redactMap(out[index].HealthCheck)
		out[index].Metadata = redactMap(out[index].Metadata)
	}
	return out
}

func redactedBuildPolicy(policy domaincatalog.BuildPolicy) map[string]any {
	return map[string]any{
		"sourceId":         policy.SourceID,
		"refType":          policy.RefType,
		"refValue":         policy.RefValue,
		"imageTagMode":     policy.ImageTagMode,
		"imageTagTemplate": policy.ImageTagTemplate,
		"variables":        sanitizeGatewayValue(policy.Variables),
		"buildArgs":        sanitizeGatewayValue(policy.BuildArgs),
	}
}

func redactedBindingSummaries(items []domaindelivery.ApplicationBindingSummary) []domaindelivery.ApplicationBindingSummary {
	out := make([]domaindelivery.ApplicationBindingSummary, len(items))
	copy(out, items)
	for index := range out {
		if out[index].BuildSource != nil {
			copySource := *out[index].BuildSource
			copySource.Config = redactMap(copySource.Config)
			out[index].BuildSource = &copySource
		}
		if out[index].LatestBundle != nil {
			out[index].LatestBundle = redactedReleaseBundlePtr(out[index].LatestBundle)
		}
		if out[index].LatestExecutionTask != nil {
			out[index].LatestExecutionTask = redactedExecutionTaskPtr(out[index].LatestExecutionTask)
		}
		out[index].LatestBuild = nil
		out[index].LatestWorkflow = nil
		out[index].LatestRelease = nil
	}
	return out
}

func redactedReleaseBundles(items []domaindelivery.ReleaseBundle) []domaindelivery.ReleaseBundle {
	out := make([]domaindelivery.ReleaseBundle, len(items))
	copy(out, items)
	for index := range out {
		out[index].Metadata = redactMap(out[index].Metadata)
		out[index].Artifacts = redactedExecutionArtifacts(out[index].Artifacts)
	}
	return out
}

func redactedReleaseBundlePtr(item *domaindelivery.ReleaseBundle) *domaindelivery.ReleaseBundle {
	if item == nil {
		return nil
	}
	out := *item
	out.Metadata = redactMap(out.Metadata)
	out.Artifacts = redactedExecutionArtifacts(out.Artifacts)
	return &out
}

func redactedExecutionTasks(items []domaindelivery.ExecutionTask) []domaindelivery.ExecutionTask {
	out := make([]domaindelivery.ExecutionTask, len(items))
	copy(out, items)
	for index := range out {
		out[index] = redactedExecutionTask(out[index])
	}
	return out
}

func redactedExecutionTaskPtr(item *domaindelivery.ExecutionTask) *domaindelivery.ExecutionTask {
	if item == nil {
		return nil
	}
	out := redactedExecutionTask(*item)
	return &out
}

func redactedExecutionTask(item domaindelivery.ExecutionTask) domaindelivery.ExecutionTask {
	item.CallbackToken = ""
	item.Payload = redactMap(item.Payload)
	item.Result = redactMap(item.Result)
	item.Artifacts = redactedExecutionArtifacts(item.Artifacts)
	return item
}

func redactedExecutionArtifacts(items []domaindelivery.ExecutionArtifact) []domaindelivery.ExecutionArtifact {
	out := make([]domaindelivery.ExecutionArtifact, len(items))
	copy(out, items)
	for index := range out {
		out[index].Metadata = redactMap(out[index].Metadata)
	}
	return out
}

func releaseTargetsFromApplicationDetail(detail domaindelivery.ApplicationDetail) []map[string]any {
	items := make([]map[string]any, 0)
	for _, binding := range detail.Bindings {
		for _, target := range binding.Targets {
			items = append(items, map[string]any{
				"applicationId":            detail.Application.ID,
				"applicationEnvironmentId": binding.ApplicationEnvironmentID,
				"environmentId":            binding.EnvironmentID,
				"environmentName":          binding.EnvironmentName,
				"environmentKey":           binding.EnvironmentKey,
				"requiresApproval":         binding.RequiresApproval,
				"actionKind":               binding.ActionKind,
				"target":                   target,
			})
		}
	}
	return items
}

func filterBindingSummaries(items []domaindelivery.ApplicationBindingSummary, bindingID string) []domaindelivery.ApplicationBindingSummary {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return items
	}
	out := make([]domaindelivery.ApplicationBindingSummary, 0, 1)
	for _, item := range items {
		if item.ApplicationEnvironmentID == bindingID {
			out = append(out, item)
		}
	}
	return out
}

func podDescribeContext(item domainresource.PodDetailView) map[string]any {
	return map[string]any{
		"name":               item.Name,
		"namespace":          item.Namespace,
		"phase":              item.Phase,
		"podIp":              item.PodIP,
		"hostIp":             item.HostIP,
		"nodeName":           item.NodeName,
		"serviceAccountName": item.ServiceAccountName,
		"qosClass":           item.QOSClass,
		"startTime":          item.StartTime,
		"requests":           item.Requests,
		"limits":             item.Limits,
		"labels":             item.Labels,
		"containers":         item.Containers,
		"conditions":         item.Conditions,
		"volumes":            item.Volumes,
		"relatedResources":   item.RelatedResources,
		"allowedActions":     item.AllowedActions,
		"summary": map[string]any{
			"containerCount":       len(item.Containers),
			"conditionCount":       len(item.Conditions),
			"volumeCount":          len(item.Volumes),
			"relatedResourceCount": len(item.RelatedResources),
			"restarts":             totalContainerRestarts(item.Containers),
		},
	}
}

func (s *Service) serviceBackendContext(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, serviceName string) (map[string]any, error) {
	services, err := s.resources.ListServices(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	var selected *domainresource.ServiceView
	for _, item := range services {
		if item.Namespace == namespace && item.Name == serviceName {
			copyItem := item
			selected = &copyItem
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("%w: service %s/%s was not found", apperrors.ErrNotFound, namespace, serviceName)
	}
	pods, err := s.resources.ListPods(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	backendPods := filterPodsByLabels(pods, selected.Selector)
	ingresses, err := s.resources.ListIngresses(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	relatedIngresses := filterIngressesByBackendService(ingresses, serviceName)
	return map[string]any{
		"service":           selected,
		"backendPods":       backendPods,
		"relatedIngresses":  relatedIngresses,
		"backendPodCount":   len(backendPods),
		"relatedRouteCount": len(relatedIngresses),
		"summary": map[string]any{
			"selector":          selected.Selector,
			"hasSelector":       len(selected.Selector) > 0,
			"readyBackendPods":  countReadyPods(backendPods),
			"totalBackendPods":  len(backendPods),
			"relatedIngresses":  len(relatedIngresses),
			"unmatchedSelector": len(selected.Selector) > 0 && len(backendPods) == 0,
		},
	}, nil
}

func (s *Service) routeContext(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, serviceName string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	ingresses, err := s.resources.ListIngresses(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	gatewayClasses, gatewayClassErr := s.resources.ListGatewayClasses(ctx, principal, clusterID)
	gateways, gatewayErr := s.resources.ListGateways(ctx, principal, clusterID, namespace)
	httpRoutes, httpRouteErr := s.resources.ListHTTPRoutes(ctx, principal, clusterID, namespace)
	backendTLSPolicies, backendTLSErr := s.resources.ListBackendTLSPolicies(ctx, principal, clusterID, namespace)
	grpcRoutes, grpcRouteErr := s.resources.ListGRPCRoutes(ctx, principal, clusterID, namespace)
	referenceGrants, referenceGrantErr := s.resources.ListReferenceGrants(ctx, principal, clusterID, namespace)
	if serviceName != "" {
		ingresses = filterIngressesByBackendService(ingresses, serviceName)
		httpRoutes = filterHTTPRoutesByBackendService(httpRoutes, serviceName)
		grpcRoutes = filterGRPCRoutesByBackendService(grpcRoutes, serviceName)
	}
	output := map[string]any{
		"namespace":             namespace,
		"serviceName":           serviceName,
		"ingresses":             ingresses,
		"gatewayClasses":        gatewayClasses,
		"gateways":              gateways,
		"httpRoutes":            httpRoutes,
		"backendTLSPolicies":    backendTLSPolicies,
		"grpcRoutes":            grpcRoutes,
		"referenceGrants":       referenceGrants,
		"ingressCount":          len(ingresses),
		"gatewayClassCount":     len(gatewayClasses),
		"gatewayCount":          len(gateways),
		"httpRouteCount":        len(httpRoutes),
		"backendTLSPolicyCount": len(backendTLSPolicies),
		"grpcRouteCount":        len(grpcRoutes),
		"referenceGrantCount":   len(referenceGrants),
	}
	errors := gatewayRouteErrors(gatewayClassErr, gatewayErr, httpRouteErr, backendTLSErr, grpcRouteErr, referenceGrantErr)
	if len(errors) > 0 {
		output["capabilityWarnings"] = errors
	}
	return output, nil
}

func (s *Service) storageContext(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) (map[string]any, error) {
	pvcs, err := s.resources.ListPersistentVolumeClaims(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	pvs, err := s.resources.ListPersistentVolumes(ctx, principal, clusterID)
	if err != nil {
		return nil, err
	}
	storageClasses, err := s.resources.ListStorageClasses(ctx, principal, clusterID)
	if err != nil {
		return nil, err
	}
	if namespace != "" {
		pvs = filterPersistentVolumesByClaims(pvs, pvcs)
	}
	return map[string]any{
		"namespace":                     namespace,
		"persistentVolumeClaims":        pvcs,
		"persistentVolumes":             pvs,
		"storageClasses":                storageClasses,
		"persistentVolumeClaimCount":    len(pvcs),
		"persistentVolumeCount":         len(pvs),
		"storageClassCount":             len(storageClasses),
		"unboundPersistentVolumeClaims": unboundPVCNames(pvcs),
	}, nil
}

func (s *Service) releaseBundleForContext(ctx context.Context, principal domainidentity.Principal, bundleID string, fallback []domaindelivery.ReleaseBundle) (*domaindelivery.ReleaseBundle, error) {
	bundleID = strings.TrimSpace(bundleID)
	if bundleID == "" {
		return nil, nil
	}
	for _, item := range fallback {
		if item.ID == bundleID {
			copyItem := item
			return &copyItem, nil
		}
	}
	item, err := s.delivery.GetReleaseBundle(ctx, principal, bundleID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) executionTaskForContext(ctx context.Context, principal domainidentity.Principal, taskID string, fallback []domaindelivery.ExecutionTask) (*domaindelivery.ExecutionTask, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, nil
	}
	for _, item := range fallback {
		if item.ID == taskID {
			copyItem := item
			return &copyItem, nil
		}
	}
	item, err := s.delivery.GetExecutionTask(ctx, principal, taskID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func compareReleaseBundles(source, target *domaindelivery.ReleaseBundle) map[string]any {
	out := map[string]any{
		"hasSource": source != nil,
		"hasTarget": target != nil,
		"changes":   []string{},
	}
	if source == nil || target == nil {
		out["summary"] = "source and target bundle comparison requires both bundle ids or recent bundle history"
		return out
	}
	changes := make([]string, 0)
	if source.Version != target.Version {
		changes = append(changes, "version")
	}
	if source.ArtifactRef != target.ArtifactRef {
		changes = append(changes, "artifactRef")
	}
	if source.ArtifactDigest != target.ArtifactDigest {
		changes = append(changes, "artifactDigest")
	}
	if source.SourceType != target.SourceType {
		changes = append(changes, "sourceType")
	}
	out["sourceBundleId"] = source.ID
	out["targetBundleId"] = target.ID
	out["changes"] = changes
	if len(changes) == 0 {
		out["summary"] = "source and target bundles have the same version, artifact reference, digest, and source type"
	} else {
		out["summary"] = "source and target bundles differ in " + strings.Join(changes, ", ")
	}
	return out
}

func rollbackSuggestions(bindings []domaindelivery.ApplicationBindingSummary, bundles []domaindelivery.ReleaseBundle, currentTask *domaindelivery.ExecutionTask) []map[string]any {
	items := make([]map[string]any, 0)
	if currentTask != nil && strings.TrimSpace(currentTask.Status) != "" && !executionTaskSucceeded(currentTask.Status) {
		items = append(items, map[string]any{
			"type":            "investigate_failed_execution",
			"executionTaskId": currentTask.ID,
			"status":          currentTask.Status,
			"reason":          "current execution task is not successful",
		})
	}
	for _, binding := range bindings {
		if binding.LatestBundle != nil {
			items = append(items, map[string]any{
				"type":                         "consider_latest_stable_bundle",
				"applicationEnvironmentId":     binding.ApplicationEnvironmentID,
				"candidateReleaseBundleId":     binding.LatestBundle.ID,
				"candidateReleaseBundleStatus": binding.LatestBundle.Status,
			})
			break
		}
	}
	for _, bundle := range bundles {
		if strings.EqualFold(strings.TrimSpace(bundle.Status), "ready") || strings.EqualFold(strings.TrimSpace(bundle.Status), "succeeded") || strings.EqualFold(strings.TrimSpace(bundle.Status), "completed") {
			items = append(items, map[string]any{
				"type":                     "candidate_previous_bundle",
				"candidateReleaseBundleId": bundle.ID,
				"version":                  bundle.Version,
				"artifactRef":              bundle.ArtifactRef,
			})
			break
		}
	}
	if len(items) == 0 {
		items = append(items, map[string]any{
			"type":   "manual_review_required",
			"reason": "no successful prior release bundle is visible in the collected context",
		})
	}
	return items
}

func executionTaskSucceeded(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "success", "completed":
		return true
	default:
		return false
	}
}

func totalContainerRestarts(items []domainresource.WorkloadContainerView) int32 {
	var total int32
	for _, item := range items {
		total += item.RestartCount
	}
	return total
}

func filterPodsByLabels(items []domainresource.PodView, selector map[string]string) []domainresource.PodView {
	if len(selector) == 0 {
		return []domainresource.PodView{}
	}
	out := make([]domainresource.PodView, 0, len(items))
	for _, item := range items {
		if labelsMatchSelector(item.Labels, selector) {
			out = append(out, item)
		}
	}
	return out
}

func labelsMatchSelector(labels, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, expected := range selector {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if labels[key] != expected {
			return false
		}
	}
	return true
}

func countReadyPods(items []domainresource.PodView) int {
	count := 0
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Phase), "Running") {
			count++
		}
	}
	return count
}

func filterIngressesByBackendService(items []domainresource.IngressView, serviceName string) []domainresource.IngressView {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return items
	}
	out := make([]domainresource.IngressView, 0, len(items))
	for _, item := range items {
		if slices.Contains(item.BackendServices, serviceName) {
			out = append(out, item)
		}
	}
	return out
}

func filterHTTPRoutesByBackendService(items []domainresource.HTTPRouteView, serviceName string) []domainresource.HTTPRouteView {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return items
	}
	out := make([]domainresource.HTTPRouteView, 0, len(items))
	for _, item := range items {
		if slices.Contains(item.BackendServices, serviceName) {
			out = append(out, item)
		}
	}
	return out
}

func filterGRPCRoutesByBackendService(items []domainresource.GRPCRouteView, serviceName string) []domainresource.GRPCRouteView {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return items
	}
	out := make([]domainresource.GRPCRouteView, 0, len(items))
	for _, item := range items {
		if slices.Contains(item.BackendServices, serviceName) {
			out = append(out, item)
		}
	}
	return out
}

func gatewayRouteErrors(errs ...error) []string {
	out := make([]string, 0)
	for _, err := range errs {
		if err != nil {
			out = append(out, err.Error())
		}
	}
	return out
}

func filterPersistentVolumesByClaims(items []domainresource.PersistentVolumeView, claims []domainresource.PersistentVolumeClaimView) []domainresource.PersistentVolumeView {
	if len(claims) == 0 {
		return []domainresource.PersistentVolumeView{}
	}
	volumeNames := map[string]struct{}{}
	for _, claim := range claims {
		if strings.TrimSpace(claim.VolumeName) != "" {
			volumeNames[claim.VolumeName] = struct{}{}
		}
	}
	out := make([]domainresource.PersistentVolumeView, 0, len(items))
	for _, item := range items {
		if _, ok := volumeNames[item.Name]; ok {
			out = append(out, item)
		}
	}
	return out
}

func unboundPVCNames(items []domainresource.PersistentVolumeClaimView) []string {
	out := make([]string, 0)
	for _, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.Status), "Bound") {
			out = append(out, item.Namespace+"/"+item.Name)
		}
	}
	return out
}

func optionalReleaseBundleID(item *domaindelivery.ReleaseBundle) string {
	if item == nil {
		return ""
	}
	return item.ID
}

func optionalExecutionTaskID(item *domaindelivery.ExecutionTask) string {
	if item == nil {
		return ""
	}
	return item.ID
}

func (s *Service) recordTokenAudit(ctx context.Context, principal domainidentity.Principal, action, result, summary string, metadata map[string]any) error {
	if s == nil || s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  "AIGatewayCredential",
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
	})
}

func (s *Service) recordConfigAudit(ctx context.Context, principal domainidentity.Principal, resourceKind, resourceName, action, result, summary string, metadata map[string]any) error {
	if s == nil || s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  resourceKind,
		ResourceName:  resourceName,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
	})
}

func (s *Service) recordApprovalDecisionAudit(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, action, result, summary string) error {
	if s == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	metadata := map[string]any{
		"approvalRequestId": request.ID,
		"toolName":          request.ToolName,
		"riskLevel":         request.RiskLevel,
		"strategy":          request.Strategy,
		"policyId":          request.PolicyID,
		"approvalPolicyRef": request.ApprovalPolicyRef,
		"aiClientId":        request.AIClientID,
		"skillId":           request.SkillID,
		"relatedIds":        request.RelatedIDs,
	}
	if s.operations != nil {
		targetScope := map[string]any{
			"module":            "ai-gateway",
			"resourceKind":      "AIGatewayApprovalRequest",
			"resourceName":      request.ID,
			"approvalRequestId": request.ID,
			"toolName":          request.ToolName,
			"riskLevel":         request.RiskLevel,
		}
		for key, value := range request.ResourceScope {
			targetScope[key] = value
		}
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, action, targetScope, result, summary, metadata))
	}
	_ = s.recordGatewayApprovalAuditLog(ctx, principal, request, action, result, summary, meta)
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  "AIGatewayApprovalRequest",
		ResourceName:  request.ID,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
	})
}

func (s *Service) recordGatewayApprovalAuditLog(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, action, result, summary string, meta requestctx.Metadata) error {
	if s == nil || s.repo == nil {
		return nil
	}
	actorType, actorID := gatewaySubject(principal)
	return s.repo.CreateAuditLog(ctx, domainaigateway.AuditLog{
		ID:            uuid.NewString(),
		ActorType:     actorType,
		ActorID:       actorID,
		ActorName:     principal.UserName,
		AIClientID:    request.AIClientID,
		AIClientName:  request.AIClientName,
		SkillID:       request.SkillID,
		ToolName:      request.ToolName,
		RiskLevel:     request.RiskLevel,
		ResourceScope: request.ResourceScope,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestID:     firstNonEmpty(request.RequestID, meta.RequestID),
		SourceIP:      firstNonEmpty(request.SourceIP, meta.SourceIP),
		Metadata: map[string]any{
			"approvalRequestId": request.ID,
			"strategy":          request.Strategy,
			"policyId":          request.PolicyID,
			"approvalPolicyRef": request.ApprovalPolicyRef,
			"relatedIds":        request.RelatedIDs,
		},
		CreatedAt: time.Now().UTC(),
	})
}

func (s *Service) recordApprovalTimeoutAudit(ctx context.Context, request domainaigateway.ApprovalRequest) error {
	principal := approvalRequestPrincipal(request)
	return s.recordApprovalDecisionAudit(ctx, principal, request, "ai_gateway.approval.timeout", "timeout", "AI Gateway approval request timed out")
}

func (s *Service) recordResourceAudit(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ResourceReadRequest, resource domainaigateway.ResourceCapability, result, summary string, relatedIDs map[string]any) error {
	if s == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	_ = s.recordGatewayResourceAuditLog(ctx, principal, input, resource, result, summary, relatedIDs, meta)
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  "AIGatewayResource",
		ResourceName:  resource.Name,
		Action:        "ai_gateway.resource.read",
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     firstNonEmpty(input.RequestID, meta.RequestID),
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"aiClientId":    strings.TrimSpace(input.AIClientID),
			"aiClientName":  strings.TrimSpace(input.AIClientName),
			"skillId":       strings.TrimSpace(input.SkillID),
			"resourceUri":   resource.Name,
			"contextKeys":   sortedGatewayMapKeys(input.Context),
			"relatedIds":    relatedIDs,
			"requestClient": meta.UserAgent,
		},
	})
}

func (s *Service) recordGatewayResourceAuditLog(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ResourceReadRequest, resource domainaigateway.ResourceCapability, result, summary string, relatedIDs map[string]any, meta requestctx.Metadata) error {
	if s == nil || s.repo == nil {
		return nil
	}
	actorType, actorID := gatewaySubject(principal)
	return s.repo.CreateAuditLog(ctx, domainaigateway.AuditLog{
		ID:            uuid.NewString(),
		ActorType:     actorType,
		ActorID:       actorID,
		ActorName:     principal.UserName,
		AIClientID:    strings.TrimSpace(input.AIClientID),
		AIClientName:  strings.TrimSpace(input.AIClientName),
		SkillID:       strings.TrimSpace(input.SkillID),
		ToolName:      resource.Name,
		RiskLevel:     domainaigateway.RiskLevelRead,
		ResourceScope: gatewayAuditScope(input.Context, relatedIDs),
		Action:        "ai_gateway.resource.read",
		Result:        result,
		Summary:       summary,
		RequestID:     firstNonEmpty(input.RequestID, meta.RequestID),
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"resourceUri": resource.Name,
			"contextKeys": sortedGatewayMapKeys(input.Context),
			"relatedIds":  relatedIDs,
		},
		CreatedAt: time.Now().UTC(),
	})
}

func (s *Service) recordPromptAudit(ctx context.Context, principal domainidentity.Principal, input domainaigateway.PromptGetRequest, prompt domainaigateway.PromptCapability, result, summary string, relatedIDs map[string]any) error {
	if s == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	_ = s.recordGatewayPromptAuditLog(ctx, principal, input, prompt, result, summary, relatedIDs, meta)
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  "AIGatewayPrompt",
		ResourceName:  prompt.Name,
		Action:        "ai_gateway.prompt.get",
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     firstNonEmpty(input.RequestID, meta.RequestID),
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"aiClientId":    strings.TrimSpace(input.AIClientID),
			"aiClientName":  strings.TrimSpace(input.AIClientName),
			"skillId":       strings.TrimSpace(input.SkillID),
			"promptName":    prompt.Name,
			"argumentKeys":  sortedGatewayMapKeys(input.Arguments),
			"contextKeys":   sortedGatewayMapKeys(input.Context),
			"relatedIds":    relatedIDs,
			"requestClient": meta.UserAgent,
		},
	})
}

func (s *Service) recordGatewayPromptAuditLog(ctx context.Context, principal domainidentity.Principal, input domainaigateway.PromptGetRequest, prompt domainaigateway.PromptCapability, result, summary string, relatedIDs map[string]any, meta requestctx.Metadata) error {
	if s == nil || s.repo == nil {
		return nil
	}
	actorType, actorID := gatewaySubject(principal)
	scope := mergeAnyMaps(input.Context, input.Arguments)
	return s.repo.CreateAuditLog(ctx, domainaigateway.AuditLog{
		ID:            uuid.NewString(),
		ActorType:     actorType,
		ActorID:       actorID,
		ActorName:     principal.UserName,
		AIClientID:    strings.TrimSpace(input.AIClientID),
		AIClientName:  strings.TrimSpace(input.AIClientName),
		SkillID:       strings.TrimSpace(input.SkillID),
		ToolName:      prompt.Name,
		RiskLevel:     domainaigateway.RiskLevelAnalyze,
		ResourceScope: gatewayAuditScope(scope, relatedIDs),
		Action:        "ai_gateway.prompt.get",
		Result:        result,
		Summary:       summary,
		RequestID:     firstNonEmpty(input.RequestID, meta.RequestID),
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"promptName":   prompt.Name,
			"argumentKeys": sortedGatewayMapKeys(input.Arguments),
			"contextKeys":  sortedGatewayMapKeys(input.Context),
			"relatedIds":   relatedIDs,
		},
		CreatedAt: time.Now().UTC(),
	})
}

func (s *Service) recordToolAudit(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest, tool domainaigateway.ToolCapability, result, summary string, relatedIDs map[string]any) error {
	return s.recordToolAuditWithRedaction(ctx, principal, input, tool, result, summary, relatedIDs, gatewayRedactionAuditSummary{})
}

func (s *Service) recordToolAuditWithRedaction(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest, tool domainaigateway.ToolCapability, result, summary string, relatedIDs map[string]any, redactionSummary gatewayRedactionAuditSummary) error {
	return s.recordToolAuditWithMetadata(ctx, principal, input, tool, result, summary, relatedIDs, redactionSummary, map[string]any{})
}

func (s *Service) recordToolAuditWithMetadata(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest, tool domainaigateway.ToolCapability, result, summary string, relatedIDs map[string]any, redactionSummary gatewayRedactionAuditSummary, usageSummary map[string]any) error {
	if s == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	_ = s.recordGatewayToolAuditLog(ctx, principal, input, tool, result, summary, relatedIDs, meta, redactionSummary, usageSummary)
	_ = s.recordToolOperation(ctx, principal, input, tool, result, summary, relatedIDs, redactionSummary, usageSummary)
	if s.audit == nil {
		return nil
	}
	metadata := map[string]any{
		"aiClientId":       strings.TrimSpace(input.AIClientID),
		"aiClientName":     strings.TrimSpace(input.AIClientName),
		"skillId":          strings.TrimSpace(input.SkillID),
		"toolName":         tool.Name,
		"mcpAdapterId":     tool.MCPAdapterID,
		"mcpToolName":      tool.MCPToolName,
		"riskLevel":        tool.RiskLevel,
		"requiresApproval": tool.RequiresApproval,
		"relatedIds":       relatedIDs,
	}
	addGatewayRedactionAuditMetadata(metadata, redactionSummary)
	addGatewayUsageAuditMetadata(metadata, usageSummary)
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  "AIGatewayTool",
		ResourceName:  tool.Name,
		Action:        "ai_gateway.tool.invoke",
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     firstNonEmpty(input.RequestID, meta.RequestID),
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
	})
}

func (s *Service) recordToolOperation(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest, tool domainaigateway.ToolCapability, result, summary string, relatedIDs map[string]any, redactionSummary gatewayRedactionAuditSummary, usageSummary map[string]any) error {
	if s == nil || s.operations == nil || !shouldRecordGatewayToolOperation(tool, result) {
		return nil
	}
	targetScope := map[string]any{
		"module":       "ai-gateway",
		"resourceKind": "AIGatewayTool",
		"resourceName": tool.Name,
		"riskLevel":    tool.RiskLevel,
	}
	for key, value := range gatewayAuditScope(input.Input, relatedIDs) {
		targetScope[key] = value
	}
	metadata := map[string]any{
		"aiClientId":       strings.TrimSpace(input.AIClientID),
		"aiClientName":     strings.TrimSpace(input.AIClientName),
		"skillId":          strings.TrimSpace(input.SkillID),
		"toolName":         tool.Name,
		"riskLevel":        tool.RiskLevel,
		"requiresApproval": tool.RequiresApproval,
		"relatedIds":       relatedIDs,
	}
	addGatewayRedactionAuditMetadata(metadata, redactionSummary)
	addGatewayUsageAuditMetadata(metadata, usageSummary)
	return s.operations.Record(ctx, operationentry.New(
		ctx,
		principal,
		"ai_gateway.tool.invoke",
		targetScope,
		result,
		summary,
		metadata,
	))
}

func shouldRecordGatewayToolOperation(tool domainaigateway.ToolCapability, result string) bool {
	result = strings.TrimSpace(result)
	if result != "" && result != "success" {
		return true
	}
	switch tool.RiskLevel {
	case domainaigateway.RiskLevelMutate, domainaigateway.RiskLevelExecute, domainaigateway.RiskLevelHigh:
		return true
	default:
		return false
	}
}

func (s *Service) recordGatewayToolAuditLog(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest, tool domainaigateway.ToolCapability, result, summary string, relatedIDs map[string]any, meta requestctx.Metadata, redactionSummary gatewayRedactionAuditSummary, usageSummary map[string]any) error {
	if s == nil || s.repo == nil {
		return nil
	}
	actorType, actorID := gatewaySubject(principal)
	metadata := map[string]any{
		"mcpAdapterId":     tool.MCPAdapterID,
		"mcpToolName":      tool.MCPToolName,
		"requiresApproval": tool.RequiresApproval,
		"relatedIds":       relatedIDs,
	}
	addGatewayRedactionAuditMetadata(metadata, redactionSummary)
	addGatewayUsageAuditMetadata(metadata, usageSummary)
	item := domainaigateway.AuditLog{
		ID:            uuid.NewString(),
		ActorType:     actorType,
		ActorID:       actorID,
		ActorName:     principal.UserName,
		AIClientID:    strings.TrimSpace(input.AIClientID),
		AIClientName:  strings.TrimSpace(input.AIClientName),
		SkillID:       strings.TrimSpace(input.SkillID),
		ToolName:      tool.Name,
		RiskLevel:     tool.RiskLevel,
		ResourceScope: gatewayAuditScope(input.Input, relatedIDs),
		Action:        "ai_gateway.tool.invoke",
		Result:        result,
		Summary:       summary,
		RequestID:     firstNonEmpty(input.RequestID, meta.RequestID),
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
		CreatedAt:     time.Now().UTC(),
	}
	return s.repo.CreateAuditLog(ctx, item)
}

func addGatewayRedactionAuditMetadata(metadata map[string]any, summary gatewayRedactionAuditSummary) {
	if metadata == nil || summary.empty() {
		return
	}
	metadata["redaction"] = summary.toMap()
}

func addGatewayUsageAuditMetadata(metadata map[string]any, usage map[string]any) {
	if metadata == nil || len(usage) == 0 {
		return
	}
	metadata["providerUsage"] = usage
	metadata["usage"] = usage
}

func gatewayAuditScope(input map[string]any, relatedIDs map[string]any) map[string]any {
	scope := map[string]any{}
	for key, value := range standardGatewayScope(input, relatedIDs) {
		scope[key] = value
	}
	for _, key := range []string{"podName", "deploymentName", "serviceName"} {
		if value := stringInput(input, key); value != "" {
			scope[key] = value
			continue
		}
		if value, ok := relatedIDs[key]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			scope[key] = strings.TrimSpace(fmt.Sprint(value))
		}
	}
	return scope
}

func standardGatewayScope(input map[string]any, relatedIDs map[string]any) map[string]string {
	scope := map[string]string{}
	for _, item := range gatewayScopeAliases() {
		if value := firstMapString(input, item.aliases...); value != "" {
			scope[item.key] = value
			continue
		}
		if value := firstMapString(relatedIDs, item.aliases...); value != "" {
			scope[item.key] = value
		}
	}
	return scope
}

func mergeAnyMaps(left, right map[string]any) map[string]any {
	out := make(map[string]any, len(left)+len(right))
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func gatewayApprovalReplayInput(request domainaigateway.ApprovalRequest) map[string]any {
	out := mergeAnyMaps(request.ToolInput, nil)
	if request.ToolName != "delivery.actions.trigger" {
		return out
	}
	variables := mergeAnyMaps(mapValue(out["variables"]), nil)
	if strings.TrimSpace(request.ID) != "" {
		variables["aiGatewayApprovalRequestId"] = request.ID
	}
	if strings.TrimSpace(request.ApprovalPolicyRef) != "" {
		variables["aiGatewayApprovalPolicyRef"] = request.ApprovalPolicyRef
	}
	if strings.TrimSpace(request.PolicyID) != "" {
		variables["aiGatewayPolicyId"] = request.PolicyID
	}
	if strings.TrimSpace(request.ToolName) != "" {
		variables["aiGatewayToolName"] = request.ToolName
	}
	if strings.TrimSpace(request.SkillID) != "" {
		variables["aiGatewaySkillId"] = request.SkillID
	}
	if strings.TrimSpace(request.AIClientID) != "" {
		variables["aiGatewayAIClientId"] = request.AIClientID
	}
	if len(variables) > 0 {
		out["variables"] = variables
	}
	return out
}

func gatewayResourceScopeMatches(resourceScopes map[string]any, invocationScope map[string]string) bool {
	if len(resourceScopes) == 0 {
		return true
	}
	hasConstraint := false
	for _, item := range gatewayScopeAliases() {
		allowedValues := gatewayResourceScopeValues(resourceScopes, item.aliases...)
		if len(allowedValues) == 0 {
			continue
		}
		hasConstraint = true
		requestedValue := strings.TrimSpace(invocationScope[item.key])
		if requestedValue == "" {
			return false
		}
		if !gatewayScopeValueAllowed(allowedValues, requestedValue) {
			return false
		}
	}
	return hasConstraint
}

func gatewayResourceScopesConstrained(resourceScopes map[string]any) bool {
	if len(resourceScopes) == 0 {
		return false
	}
	for _, item := range gatewayScopeAliases() {
		for _, value := range gatewayResourceScopeValues(resourceScopes, item.aliases...) {
			value = strings.TrimSpace(value)
			if value != "" && value != "*" {
				return true
			}
		}
	}
	return false
}

func gatewayScopeValueAllowed(allowedValues []string, requestedValue string) bool {
	requestedValue = strings.TrimSpace(requestedValue)
	for _, allowed := range allowedValues {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || allowed == requestedValue {
			return true
		}
	}
	return false
}

func gatewayResourceScopeValues(values map[string]any, aliases ...string) []string {
	out := make([]string, 0)
	for _, alias := range aliases {
		if values == nil {
			continue
		}
		if raw, ok := values[alias]; ok {
			out = append(out, stringsFromAny(raw)...)
		}
	}
	return normalizeStringSlice(out)
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return out
	default:
		text := strings.TrimSpace(fmt.Sprint(typed))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func firstMapString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if values == nil {
			continue
		}
		if raw, ok := values[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(raw)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

type gatewayScopeAlias struct {
	key     string
	aliases []string
}

func gatewayScopeAliases() []gatewayScopeAlias {
	return []gatewayScopeAlias{
		{key: "businessLineId", aliases: []string{"businessLineId", "businessLineID", "businessLine", "businessLineIds", "businessLineIDs"}},
		{key: "applicationId", aliases: []string{"applicationId", "applicationID", "application", "applicationIds", "applicationIDs"}},
		{key: "applicationEnvironmentId", aliases: []string{"applicationEnvironmentId", "applicationEnvironmentID", "applicationEnvironment", "applicationEnvironmentIds", "applicationEnvironmentIDs", "bindingId", "bindingID"}},
		{key: "environmentId", aliases: []string{"environmentId", "environmentID", "environment", "environmentIds", "environmentIDs"}},
		{key: "clusterId", aliases: []string{"clusterId", "clusterID", "cluster", "clusterIds", "clusterIDs"}},
		{key: "namespace", aliases: []string{"namespace", "namespaces"}},
		{key: "releaseBundleId", aliases: []string{"releaseBundleId", "releaseBundleID", "releaseBundle", "releaseBundleIds", "releaseBundleIDs", "bundleId", "bundleID"}},
		{key: "executionTaskId", aliases: []string{"executionTaskId", "executionTaskID", "executionTask", "executionTaskIds", "executionTaskIDs", "taskId", "taskID"}},
	}
}

func mapInput(input map[string]any, out any) error {
	if input == nil {
		input = map[string]any{}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("%w: invalid tool input", apperrors.ErrInvalidArgument)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: invalid tool input: %v", apperrors.ErrInvalidArgument, err)
	}
	return nil
}

func stringInput(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, ok := input[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mapValue(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return map[string]any{}
	}
}

func stringSliceValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

func sliceLen(value any) int {
	switch typed := value.(type) {
	case []domainresource.PodView:
		return len(typed)
	case []domainresource.DeploymentView:
		return len(typed)
	case []domainresource.ServiceView:
		return len(typed)
	case []domainresource.ClusterEventView:
		return len(typed)
	case []domaindelivery.ExecutionLog:
		return len(typed)
	case []domaindelivery.ExecutionTask:
		return len(typed)
	case []domaindelivery.ExecutionArtifact:
		return len(typed)
	case []any:
		return len(typed)
	default:
		return 0
	}
}

func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func normalizeGatewayResourceURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const legacyPrefix = "soha://resource/"
	if strings.HasPrefix(value, legacyPrefix) {
		value = strings.TrimPrefix(value, legacyPrefix)
	}
	if strings.HasPrefix(value, "resource/") {
		value = strings.TrimPrefix(value, "resource/")
	}
	if strings.HasPrefix(value, "soha://") {
		return value
	}
	if strings.Contains(value, "/") {
		return "soha://" + strings.TrimPrefix(value, "/")
	}
	return value
}

func marshalGatewayDocument(value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("%w: failed to render AI Gateway manifest document", apperrors.ErrInvalidArgument)
	}
	return string(raw), nil
}

func gatewayResourceDocument(resource domainaigateway.ResourceCapability, input domainaigateway.ResourceReadRequest, bindings []domainaigateway.SkillBinding, skillID string) map[string]any {
	return gatewayResourceDocumentWithCapabilities(resource, input, bindings, skillID, resourceToolRefs(resource.Name), resourcePromptRefs(resource.Name), resourceSkillRefs(resource.Name), defaultTools(), defaultPrompts(), defaultSkills())
}

func gatewayResourceDocumentWithCapabilities(resource domainaigateway.ResourceCapability, input domainaigateway.ResourceReadRequest, bindings []domainaigateway.SkillBinding, skillID string, toolRefs []string, promptRefs []string, skillRefs []string, tools []domainaigateway.ToolCapability, prompts []domainaigateway.PromptCapability, skills []domainaigateway.SkillCapability) map[string]any {
	toolRefs = filterToolRefsBySkillBindingsWithSkills(toolRefs, bindings, skillID, skills)
	resourceRefs := ResourceCapabilityRefs{Resource: resource.Name, Tools: toolRefs, Prompts: promptRefs, Skills: skillRefs}
	promptRefs = filterPromptRefsBySkillBindingsWithResourceRefs(promptRefs, bindings, skillID, resourceRefs, skills)
	skillRefs = filterSkillRefsByBindingsWithSkills(skillRefs, bindings, skillID, skills)
	return map[string]any{
		"uri":                    resource.Name,
		"name":                   resource.Name,
		"description":            resource.Description,
		"manifestVersion":        manifestVersion,
		"requiredPermissionKeys": append([]string(nil), resource.PermissionKeys...),
		"requiredScopes":         append([]string(nil), resource.RequiredScopes...),
		"requestedContext":       sanitizeGatewayMap(input.Context),
		"relatedTools":           compactToolCapabilitiesFrom(toolRefs, tools),
		"relatedPrompts":         compactPromptCapabilitiesFrom(promptRefs, prompts),
		"relatedSkills":          compactSkillCapabilitiesForBindingsWithSkills(skillRefs, bindings, skillID, skills),
		"recommendedPromptNames": promptRefs,
		"governance": map[string]any{
			"readAction":     "ai_gateway.resource.read",
			"riskLevel":      domainaigateway.RiskLevelRead,
			"permissionGate": appaccess.PermAIGatewayInvoke,
			"auditBoundary":  "backend",
		},
	}
}

func gatewayPromptMessages(prompt domainaigateway.PromptCapability, input domainaigateway.PromptGetRequest, skill *domainaigateway.SkillCapability) []domainaigateway.PromptMessage {
	context := sanitizeGatewayMap(mergeAnyMaps(input.Context, input.Arguments))
	contextText, _ := marshalGatewayDocument(context)
	content := strings.Join(compactStrings(
		"You are operating through soha AI Gateway. Use only capabilities visible to the current identity and keep all mutations behind the owning soha service, risk policy, approval, and durable task boundaries.",
		"Prompt: "+prompt.Name,
		"Purpose: "+prompt.Description,
		gatewayPromptSkillSection(skill),
		gatewayPromptContextSection(contextText),
		gatewayPromptInstruction(prompt.Name),
	), "\n\n")
	return []domainaigateway.PromptMessage{{Role: "user", Content: content}}
}

func gatewayPromptSkillSection(skill *domainaigateway.SkillCapability) string {
	if skill == nil {
		return "Skill context: none supplied. Infer the workflow from the requested prompt and visible Gateway manifest only."
	}
	refs := append([]string(nil), skill.CapabilityRefs...)
	sort.Strings(refs)
	return "Skill context:\n" +
		"- id: " + skill.ID + "\n" +
		"- name: " + skill.Name + "\n" +
		"- category: " + skill.Category + "\n" +
		"- description: " + skill.Description + "\n" +
		"- capabilityRefs: " + strings.Join(refs, ", ")
}

func gatewayPromptContextSection(contextText string) string {
	if strings.TrimSpace(contextText) == "" || strings.TrimSpace(contextText) == "{}" {
		return "Current context: {}"
	}
	return "Current context, redacted by Gateway:\n" + contextText
}

func gatewayPromptInstruction(name string) string {
	switch strings.TrimSpace(name) {
	case "soha.delivery.plan_release":
		return strings.Join([]string{
			"Release planning workflow:",
			"1. Read application detail, environment bindings, build sources, release targets, workflow templates, approval policies, recent bundles, and execution tasks before proposing an action.",
			"2. Prefer delivery.release_context.diff for candidate promotion evidence and delivery.rollback.context for rollback evidence.",
			"3. Summarize readiness, blockers, blast radius, required approvals, rollback criteria, and exact next tool call inputs.",
			"4. Do not trigger build, deploy, workflow, verify, or rollback actions unless the user explicitly asks and the Gateway response permits it.",
		}, "\n")
	case "soha.k8s.diagnose_workload":
		return strings.Join([]string{
			"Kubernetes diagnosis workflow:",
			"1. Stay read-only. Use scoped cluster and namespace context and do not request raw kubeconfig or raw Kubernetes objects.",
			"2. Collect pod detail, pod logs, deployment rollout status, deployment events, service backends, route context, storage context, node detail, and cluster events as needed.",
			"3. Separate observed evidence from hypotheses and include capabilityWarnings when Gateway API or agent-backed data is unavailable.",
			"4. Return likely causes, confidence, immediate checks, and safe remediation options, but leave mutations to explicit soha tools and approval policy.",
		}, "\n")
	default:
		return "Use the visible Gateway manifest to gather evidence first, keep outputs redacted, and state any missing scope or permission before suggesting a next action."
	}
}

func compactToolCapabilities(names []string) []map[string]any {
	return compactToolCapabilitiesFrom(names, defaultTools())
}

func compactToolCapabilitiesFrom(names []string, tools []domainaigateway.ToolCapability) []map[string]any {
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		tool, ok := toolByNameFrom(name, tools)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"name":             tool.Name,
			"description":      tool.Description,
			"riskLevel":        tool.RiskLevel,
			"requiredScopes":   tool.RequiredScopes,
			"permissionKeys":   tool.PermissionKeys,
			"requiresApproval": tool.RequiresApproval,
		})
	}
	return out
}

func compactPromptCapabilities(names []string) []map[string]any {
	return compactPromptCapabilitiesFrom(names, defaultPrompts())
}

func compactPromptCapabilitiesFrom(names []string, prompts []domainaigateway.PromptCapability) []map[string]any {
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		prompt, ok := promptByNameFrom(name, prompts)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"name":           prompt.Name,
			"description":    prompt.Description,
			"requiredScopes": prompt.RequiredScopes,
			"permissionKeys": prompt.PermissionKeys,
		})
	}
	return out
}

func compactSkillCapabilities(ids []string) []map[string]any {
	return compactSkillCapabilitiesFrom(ids, defaultSkills())
}

func compactSkillCapabilitiesFrom(ids []string, skills []domainaigateway.SkillCapability) []map[string]any {
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		skill, ok := skillByIDFrom(id, skills)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"id":             skill.ID,
			"name":           skill.Name,
			"category":       skill.Category,
			"description":    skill.Description,
			"capabilityRefs": skill.CapabilityRefs,
			"requiredScopes": skill.RequiredScopes,
			"permissionKeys": skill.PermissionKeys,
		})
	}
	return out
}

func compactSkillCapabilitiesForBindings(ids []string, bindings []domainaigateway.SkillBinding, skillID string) []map[string]any {
	return compactSkillCapabilitiesForBindingsWithSkills(ids, bindings, skillID, defaultSkills())
}

func compactSkillCapabilitiesForBindingsWithSkills(ids []string, bindings []domainaigateway.SkillBinding, skillID string, skills []domainaigateway.SkillCapability) []map[string]any {
	if len(bindings) == 0 {
		return compactSkillCapabilitiesFrom(ids, skills)
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		skill, ok := skillByIDFrom(id, skills)
		if !ok {
			continue
		}
		skill = narrowSkillCapabilityByBindingsWithSkills(skill, bindings, skillID, skills)
		if len(skill.CapabilityRefs) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"id":             skill.ID,
			"name":           skill.Name,
			"category":       skill.Category,
			"description":    skill.Description,
			"capabilityRefs": skill.CapabilityRefs,
			"requiredScopes": skill.RequiredScopes,
			"permissionKeys": skill.PermissionKeys,
		})
	}
	return out
}

func narrowSkillCapabilityByBindings(skill domainaigateway.SkillCapability, bindings []domainaigateway.SkillBinding, skillID string) domainaigateway.SkillCapability {
	return narrowSkillCapabilityByBindingsWithSkills(skill, bindings, skillID, defaultSkills())
}

func narrowSkillCapabilityByBindingsWithSkills(skill domainaigateway.SkillCapability, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) domainaigateway.SkillCapability {
	if len(bindings) == 0 {
		return skill
	}
	refs, ok := skillBindingRefsWithSkills(bindings, skillID, knownSkills)[skill.ID]
	if !ok {
		skill.CapabilityRefs = nil
		return skill
	}
	if len(refs) > 0 {
		skill.CapabilityRefs = intersectStringSlices(skill.CapabilityRefs, refs)
	}
	return skill
}

func toolByNameFrom(name string, tools []domainaigateway.ToolCapability) (domainaigateway.ToolCapability, bool) {
	name = strings.TrimSpace(name)
	for _, item := range tools {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.ToolCapability{}, false
}

func promptByNameFrom(name string, prompts []domainaigateway.PromptCapability) (domainaigateway.PromptCapability, bool) {
	name = strings.TrimSpace(name)
	for _, item := range prompts {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.PromptCapability{}, false
}

func resourceToolRefs(name string) []string {
	return resourceCapabilityRefsFrom(defaultResourceCapabilityRefs(), name).Tools
}

func resourcePromptRefs(name string) []string {
	return resourceCapabilityRefsFrom(defaultResourceCapabilityRefs(), name).Prompts
}

func resourceSkillRefs(name string) []string {
	return resourceCapabilityRefsFrom(defaultResourceCapabilityRefs(), name).Skills
}

func sortedGatewayMapKeys(values map[string]any) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func buildGovernanceStatus(now time.Time, windowHours int, personalTokens []domainaigateway.PersonalAccessToken, serviceTokens []domainaigateway.ServiceAccountToken, clients []domainaigateway.AIClient, accessPolicies []domainaigateway.AccessPolicy, toolGrants []domainaigateway.ToolGrant, skillBindings []domainaigateway.SkillBinding, audits []domainaigateway.AuditLog, pendingApprovals []domainaigateway.ApprovalRequest) domainaigateway.GovernanceStatus {
	return buildGovernanceStatusWithCapabilities(now, windowHours, personalTokens, serviceTokens, clients, accessPolicies, toolGrants, skillBindings, audits, pendingApprovals, defaultTools(), defaultSkills())
}

func (s *Service) buildGovernanceStatus(now time.Time, windowHours int, personalTokens []domainaigateway.PersonalAccessToken, serviceTokens []domainaigateway.ServiceAccountToken, clients []domainaigateway.AIClient, accessPolicies []domainaigateway.AccessPolicy, toolGrants []domainaigateway.ToolGrant, skillBindings []domainaigateway.SkillBinding, audits []domainaigateway.AuditLog, pendingApprovals []domainaigateway.ApprovalRequest) domainaigateway.GovernanceStatus {
	return buildGovernanceStatusWithCapabilities(now, windowHours, personalTokens, serviceTokens, clients, accessPolicies, toolGrants, skillBindings, audits, pendingApprovals, s.gatewayTools(), s.gatewaySkills())
}

func buildGovernanceStatusWithCapabilities(now time.Time, windowHours int, personalTokens []domainaigateway.PersonalAccessToken, serviceTokens []domainaigateway.ServiceAccountToken, clients []domainaigateway.AIClient, accessPolicies []domainaigateway.AccessPolicy, toolGrants []domainaigateway.ToolGrant, skillBindings []domainaigateway.SkillBinding, audits []domainaigateway.AuditLog, pendingApprovals []domainaigateway.ApprovalRequest, tools []domainaigateway.ToolCapability, skills []domainaigateway.SkillCapability) domainaigateway.GovernanceStatus {
	tokens := governanceTokenSummary(now, personalTokens, serviceTokens)
	clientsSummary := governanceClientSummary(clients)
	approvals := governanceApprovalSummary(now, pendingApprovals)
	policyCoverage := governancePolicyCoverage(now, accessPolicies, toolGrants, skillBindings)
	metrics := governanceMetrics(audits, pendingApprovals)
	redaction := governanceRedactionSummary(audits)
	anomalies := governanceAnomalies(now, metrics, audits, tokens, approvals, pendingApprovals, accessPolicies, toolGrants, tools, skills)
	recommendationActions := governanceRecommendationActions(tokens, clientsSummary, approvals, policyCoverage, anomalies)
	recommendations := governanceRecommendationSummaries(recommendationActions)
	health := governanceHealth(tokens, metrics, clientsSummary, approvals, policyCoverage, anomalies, pendingApprovals)
	return domainaigateway.GovernanceStatus{
		GeneratedAt:           now,
		WindowHours:           windowHours,
		Health:                health,
		Metrics:               metrics,
		Tokens:                tokens,
		Clients:               clientsSummary,
		Approvals:             approvals,
		PolicyCoverage:        policyCoverage,
		Redaction:             redaction,
		Anomalies:             anomalies,
		Recommendations:       recommendations,
		RecommendationActions: recommendationActions,
		Metadata: map[string]any{
			"auditSampleLimit":       500,
			"expiringSoonDays":       14,
			"staleTokenDays":         90,
			"approvalDueSoonMinutes": 60,
			"staleApprovalHours":     24,
			"lastUsedUpdatedByAuth":  true,
			"registrationApproval":   clientsSummary.RegistrationApproval,
			"governanceDataBoundary": "redacted_summary",
		},
	}
}

func governanceTokenSummary(now time.Time, personalTokens []domainaigateway.PersonalAccessToken, serviceTokens []domainaigateway.ServiceAccountToken) domainaigateway.GovernanceTokenSummary {
	const expiringSoonDays = 14
	const staleTokenDays = 90
	expiringSoonCutoff := now.Add(expiringSoonDays * 24 * time.Hour)
	staleCutoff := now.Add(-staleTokenDays * 24 * time.Hour)
	out := domainaigateway.GovernanceTokenSummary{LastUsedTrackingState: "enabled"}
	for _, item := range personalTokens {
		finding := governanceTokenFinding("personal_access_token", item.ID, item.Name, item.UserID, item.TokenPrefix, item.ExpiresAt, item.LastUsedAt, "warning", "")
		updateGovernanceTokenCounts(now, expiringSoonCutoff, staleCutoff, item.CreatedAt, item.ExpiresAt, item.LastUsedAt, item.RevokedAt, &out.PersonalAccessTokens, finding, &out)
	}
	for _, item := range serviceTokens {
		finding := governanceTokenFinding("service_account_token", item.ID, item.Name, item.ServiceAccountID, item.TokenPrefix, item.ExpiresAt, item.LastUsedAt, "warning", "")
		updateGovernanceTokenCounts(now, expiringSoonCutoff, staleCutoff, item.CreatedAt, item.ExpiresAt, item.LastUsedAt, item.RevokedAt, &out.ServiceAccountTokens, finding, &out)
	}
	return out
}

func updateGovernanceTokenCounts(now, expiringSoonCutoff, staleCutoff time.Time, createdAt time.Time, expiresAt, lastUsedAt, revokedAt *time.Time, counts *domainaigateway.GovernanceTokenCounts, finding domainaigateway.GovernanceTokenFinding, summary *domainaigateway.GovernanceTokenSummary) {
	counts.Total++
	if revokedAt != nil {
		counts.Revoked++
		return
	}
	counts.Active++
	if expiresAt != nil && expiresAt.Before(now) {
		counts.Expired++
		finding.Severity = "critical"
		finding.Message = "active token is expired and should be revoked or rotated"
		finding.DaysUntilDue = int(expiresAt.Sub(now).Hours() / 24)
		summary.ExpiredActive = appendGovernanceTokenFinding(summary.ExpiredActive, finding)
		return
	}
	if expiresAt != nil && !expiresAt.After(expiringSoonCutoff) {
		counts.ExpiringSoon++
		finding.Severity = "warning"
		finding.Message = "token expires soon; rotate or extend intentionally"
		finding.DaysUntilDue = int(expiresAt.Sub(now).Hours() / 24)
		summary.ExpiringSoon = appendGovernanceTokenFinding(summary.ExpiringSoon, finding)
	}
	staleReference := createdAt
	if lastUsedAt != nil {
		staleReference = *lastUsedAt
	}
	if staleReference.Before(staleCutoff) {
		counts.Stale++
		finding.Severity = "warning"
		finding.Message = "token has not been used recently; review whether it is still needed"
		finding.StaleDays = int(now.Sub(staleReference).Hours() / 24)
		summary.Stale = appendGovernanceTokenFinding(summary.Stale, finding)
	}
	if lastUsedAt == nil {
		counts.NeverUsed++
		finding.Severity = "info"
		finding.Message = "token has never been used"
		summary.NeverUsed = appendGovernanceTokenFinding(summary.NeverUsed, finding)
	}
}

func governanceTokenFinding(kind, id, name, ownerID, prefix string, expiresAt, lastUsedAt *time.Time, severity, message string) domainaigateway.GovernanceTokenFinding {
	return domainaigateway.GovernanceTokenFinding{
		Kind:        kind,
		ID:          id,
		Name:        name,
		OwnerID:     ownerID,
		TokenPrefix: prefix,
		Severity:    severity,
		Message:     message,
		ExpiresAt:   expiresAt,
		LastUsedAt:  lastUsedAt,
	}
}

func appendGovernanceTokenFinding(items []domainaigateway.GovernanceTokenFinding, item domainaigateway.GovernanceTokenFinding) []domainaigateway.GovernanceTokenFinding {
	if len(items) >= 20 {
		return items
	}
	return append(items, item)
}

func governanceClientSummary(clients []domainaigateway.AIClient) domainaigateway.GovernanceClientSummary {
	out := domainaigateway.GovernanceClientSummary{RegistrationApproval: "not_configured"}
	for _, client := range clients {
		out.Total++
		status := strings.ToLower(strings.TrimSpace(client.Status))
		switch status {
		case "active", "":
			out.Active++
		case "disabled", "inactive", "blocked":
			out.Disabled++
		case "pending", "pending_approval", "approval_required":
			out.PendingApproval++
			out.PendingApprovalClientIDs = append(out.PendingApprovalClientIDs, client.ID)
			out.RegistrationApproval = "pending_clients"
		default:
			out.Disabled++
		}
		if boolFromAny(client.Metadata["registrationApprovalRequired"]) || boolFromAny(client.Metadata["requiresApproval"]) {
			out.RegistrationApproval = "configured"
		}
	}
	sort.Strings(out.PendingApprovalClientIDs)
	return out
}

func governanceApprovalSummary(now time.Time, pendingApprovals []domainaigateway.ApprovalRequest) domainaigateway.GovernanceApprovalSummary {
	const dueSoonWindow = time.Hour
	const stalePendingHours = 24
	out := domainaigateway.GovernanceApprovalSummary{}
	dueSoonCutoff := now.Add(dueSoonWindow)
	var oldestCreatedAt time.Time
	oldestRequestID := ""
	for index := range pendingApprovals {
		item := pendingApprovals[index]
		if item.Status != "" && normalizeApprovalRequestStatus(item.Status) != "pending" {
			continue
		}
		out.Pending++
		if !item.CreatedAt.IsZero() && !item.CreatedAt.After(now) {
			if oldestRequestID == "" || item.CreatedAt.Before(oldestCreatedAt) {
				oldestCreatedAt = item.CreatedAt
				oldestRequestID = item.ID
			}
			if item.CreatedAt.Before(now.Add(-stalePendingHours * time.Hour)) {
				out.StalePending++
				out.StalePendingRequestIDs = append(out.StalePendingRequestIDs, item.ID)
			}
		}
		if item.ExpiresAt == nil {
			continue
		}
		if !item.ExpiresAt.After(now) {
			out.Overdue++
			out.OverdueRequestIDs = append(out.OverdueRequestIDs, item.ID)
			continue
		}
		if item.ExpiresAt.After(dueSoonCutoff) {
			continue
		}
		out.DueSoon++
		out.DueSoonRequestIDs = append(out.DueSoonRequestIDs, item.ID)
		if out.NextDueAt == nil || item.ExpiresAt.Before(*out.NextDueAt) {
			nextDueAt := *item.ExpiresAt
			out.NextDueAt = &nextDueAt
			out.NextDueRequestID = item.ID
		}
	}
	if oldestRequestID != "" {
		out.OldestPendingRequestID = oldestRequestID
		out.OldestPendingHours = int(now.Sub(oldestCreatedAt).Hours())
		if out.OldestPendingHours < 0 {
			out.OldestPendingHours = 0
		}
	}
	sort.Strings(out.DueSoonRequestIDs)
	sort.Strings(out.StalePendingRequestIDs)
	sort.Strings(out.OverdueRequestIDs)
	return out
}

func governancePolicyCoverage(now time.Time, accessPolicies []domainaigateway.AccessPolicy, toolGrants []domainaigateway.ToolGrant, skillBindings []domainaigateway.SkillBinding) domainaigateway.GovernancePolicyCoverage {
	out := domainaigateway.GovernancePolicyCoverage{
		AccessPolicies:       len(accessPolicies),
		ToolGrants:           len(toolGrants),
		SkillBindings:        len(skillBindings),
		BudgetState:          "not_configured",
		RateLimitState:       "not_configured",
		RedactionPolicyState: "built_in",
		ResourceScopeState:   "not_configured",
	}
	for _, policy := range accessPolicies {
		if !policy.Enabled {
			continue
		}
		out.ActiveAccessPolicies++
		if governanceConditionConfigured(policy.Conditions, "budget", "budgets", "budgetPolicy", "dailyBudget", "monthlyBudget", "maxCost", "maxTokens") {
			out.BudgetPolicies++
		}
		if governanceConditionConfigured(policy.Conditions, "rateLimit", "rate_limit", "rateLimits", "qps", "maxCallsPerMinute", "maxInvocationsPerMinute", "maxCallsPerHour") {
			out.RateLimitPolicies++
		}
		if governanceConditionConfigured(policy.Conditions, "redactionPolicy", "redaction", "sensitiveDataRedaction", "redact", "redactionMode", "outputRedactionPolicy", "outputRedaction", "responseRedactionPolicy", "responseRedaction") {
			out.RedactionPolicies++
		}
		if gatewayResourceScopesConstrained(policy.ResourceScopes) {
			out.ResourceScopedAccessPolicies++
		}
	}
	for _, grant := range toolGrants {
		if grant.ExpiresAt != nil && !grant.ExpiresAt.After(now) {
			continue
		}
		out.ActiveToolGrants++
		if gatewayResourceScopesConstrained(grant.ResourceScopes) {
			out.ResourceScopedToolGrants++
		}
	}
	for _, binding := range skillBindings {
		if binding.Enabled {
			out.ActiveSkillBindings++
		}
	}
	if out.BudgetPolicies > 0 {
		out.BudgetState = "configured"
	}
	if out.RateLimitPolicies > 0 {
		out.RateLimitState = "configured"
	}
	if out.RedactionPolicies > 0 {
		out.RedactionPolicyState = "configured"
	}
	if out.ResourceScopedAccessPolicies > 0 || out.ResourceScopedToolGrants > 0 {
		out.ResourceScopeState = "configured"
	}
	return out
}

func governanceConditionConfigured(values map[string]any, keys ...string) bool {
	if len(values) == 0 {
		return false
	}
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	for key, value := range values {
		nested := mapValue(value)
		if len(nested) == 0 {
			continue
		}
		if governanceConditionConfigured(nested, keys...) {
			_ = key
			return true
		}
	}
	return false
}

func governanceMetrics(audits []domainaigateway.AuditLog, pendingApprovals []domainaigateway.ApprovalRequest) domainaigateway.GovernanceMetrics {
	out := domainaigateway.GovernanceMetrics{
		PendingApprovalCount:  len(pendingApprovals),
		RiskCounts:            map[domainaigateway.RiskLevel]int{},
		RecentResultBreakdown: map[string]int{},
		RecentActionBreakdown: map[string]int{},
	}
	toolCounts := map[string]int{}
	clientCounts := map[string]int{}
	actorCounts := map[string]int{}
	for _, item := range audits {
		out.TotalCalls++
		result := strings.ToLower(strings.TrimSpace(item.Result))
		action := strings.TrimSpace(item.Action)
		out.RecentResultBreakdown[result]++
		out.RecentActionBreakdown[action]++
		switch result {
		case "success", "approved", "executed":
			out.SuccessCount++
		case "deny", "rejected", "canceled", "timeout":
			out.DenyCount++
		case "failure", "failed":
			out.FailureCount++
		case "pending_approval", "pending_human_confirm", "pending":
			out.PendingApprovalCount++
		case "dry_run":
			out.DryRunCount++
		}
		if item.RiskLevel != "" {
			out.RiskCounts[item.RiskLevel]++
		}
		incrementStringCount(toolCounts, firstNonEmpty(item.ToolName, action))
		incrementStringCount(clientCounts, firstNonEmpty(item.AIClientID, "(none)"))
		incrementStringCount(actorCounts, firstNonEmpty(item.ActorType+":"+item.ActorID, item.ActorID, "(unknown)"))
	}
	out.TopTools = topGovernanceCounts(toolCounts, 10)
	out.TopAIClients = topGovernanceCounts(clientCounts, 10)
	out.TopActors = topGovernanceCounts(actorCounts, 10)
	return out
}

func governanceRedactionSummary(audits []domainaigateway.AuditLog) domainaigateway.GovernanceRedactionSummary {
	out := domainaigateway.GovernanceRedactionSummary{}
	targetCounts := map[string]int{}
	fieldPathCounts := map[string]int{}
	matchTypeCounts := map[string]int{}
	classifierCounts := map[string]int{}
	policyCounts := map[string]int{}
	toolCounts := map[string]int{}
	for _, item := range audits {
		redaction := mapValue(item.Metadata["redaction"])
		totalMatches := intFromAny(redaction["totalMatches"])
		if len(redaction) == 0 || totalMatches <= 0 {
			continue
		}
		out.AuditsWithRedaction++
		out.TotalMatches += totalMatches
		out.FieldMatches += intFromAny(redaction["fieldMatches"])
		out.SensitiveKeyMatches += intFromAny(redaction["sensitiveKeyMatches"])
		out.SensitiveTextMatches += intFromAny(redaction["sensitiveTextMatches"])
		out.ValuePatternMatches += intFromAny(redaction["valuePatternMatches"])
		out.SecretClassifierMatches += intFromAny(redaction["secretClassifierMatches"])
		out.StructuredSecretMatches += intFromAny(redaction["structuredSecretMatches"])
		targets := gatewayAppendUniqueStrings(nil, stringsFromAny(redaction["targets"])...)
		for _, target := range targets {
			target = normalizeGatewayRedactionTarget(target)
			if target == "" {
				target = "(unknown)"
			}
			incrementStringCount(targetCounts, target)
			switch target {
			case "input":
				out.InputAudits++
			case "output":
				out.OutputAudits++
			case "both":
				out.InputAudits++
				out.OutputAudits++
			}
		}
		for _, fieldPath := range gatewayAppendUniqueStrings(nil, stringsFromAny(redaction["fieldPaths"])...) {
			incrementStringCount(fieldPathCounts, gatewayAuditFieldPath(fieldPath))
		}
		for _, matchType := range gatewayAppendUniqueStrings(nil, stringsFromAny(redaction["matchTypes"])...) {
			incrementStringCount(matchTypeCounts, matchType)
		}
		for _, classifier := range gatewayAppendUniqueStrings(nil, stringsFromAny(redaction["classifiers"])...) {
			incrementStringCount(classifierCounts, classifier)
		}
		for _, policyID := range gatewayAppendUniqueStrings(nil, stringsFromAny(redaction["policyIds"])...) {
			incrementStringCount(policyCounts, policyID)
		}
		incrementStringCount(toolCounts, firstNonEmpty(item.ToolName, item.Action))
	}
	out.TopTargets = topGovernanceCounts(targetCounts, 10)
	out.TopFieldPaths = topGovernanceCounts(fieldPathCounts, 10)
	out.TopMatchTypes = topGovernanceCounts(matchTypeCounts, 10)
	out.TopClassifiers = topGovernanceCounts(classifierCounts, 10)
	out.TopPolicies = topGovernanceCounts(policyCounts, 10)
	out.TopTools = topGovernanceCounts(toolCounts, 10)
	return out
}

func governanceAnomalies(now time.Time, metrics domainaigateway.GovernanceMetrics, audits []domainaigateway.AuditLog, tokens domainaigateway.GovernanceTokenSummary, approvals domainaigateway.GovernanceApprovalSummary, pendingApprovals []domainaigateway.ApprovalRequest, accessPolicies []domainaigateway.AccessPolicy, toolGrants []domainaigateway.ToolGrant, tools []domainaigateway.ToolCapability, skills []domainaigateway.SkillCapability) []domainaigateway.GovernanceFinding {
	findings := make([]domainaigateway.GovernanceFinding, 0)
	if len(tokens.ExpiredActive) > 0 {
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:     "expired_active_tokens",
			Severity: "critical",
			Summary:  "active Gateway tokens have passed their expiration time",
			Count:    len(tokens.ExpiredActive),
		})
	}
	if len(pendingApprovals) > 0 {
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:              "pending_gateway_approvals",
			Severity:          "info",
			Summary:           "Gateway approval requests are waiting for decision",
			Count:             len(pendingApprovals),
			ApprovalRequestID: approvals.OldestPendingRequestID,
		})
	}
	if approvals.Overdue > 0 {
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:              "overdue_gateway_approvals",
			Severity:          "critical",
			Summary:           "Gateway approval requests are past their SLA expiration",
			Count:             approvals.Overdue,
			ApprovalRequestID: firstNonEmpty(approvals.OverdueRequestIDs...),
		})
	}
	if approvals.DueSoon > 0 {
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:              "approval_sla_due_soon",
			Severity:          "warning",
			Summary:           "Gateway approval requests are approaching SLA expiration",
			Count:             approvals.DueSoon,
			ApprovalRequestID: approvals.NextDueRequestID,
		})
	}
	if approvals.StalePending > 0 {
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:              "stale_gateway_approvals",
			Severity:          "warning",
			Summary:           "Gateway approval requests have been pending for more than 24 hours",
			Count:             approvals.StalePending,
			ApprovalRequestID: approvals.OldestPendingRequestID,
		})
	}
	findings = append(findings, governanceHighRiskResourceScopeFindings(now, accessPolicies, toolGrants, tools, skills)...)
	findings = append(findings, governanceHighRiskAllowFindings(now, accessPolicies, toolGrants, tools, skills)...)
	actorFailures := map[string]int{}
	clientFailures := map[string]int{}
	toolFailures := map[string]int{}
	for _, item := range audits {
		result := strings.ToLower(strings.TrimSpace(item.Result))
		if result != "deny" && result != "failure" && result != "failed" && result != "timeout" {
			continue
		}
		incrementStringCount(actorFailures, firstNonEmpty(item.ActorType+":"+item.ActorID, item.ActorID, "(unknown)"))
		incrementStringCount(clientFailures, firstNonEmpty(item.AIClientID, "(none)"))
		incrementStringCount(toolFailures, firstNonEmpty(item.ToolName, item.Action))
	}
	findings = append(findings, governanceThresholdFindings("actor_error_burst", "actor had repeated denied or failed Gateway calls", actorFailures, 3, "actor")...)
	findings = append(findings, governanceThresholdFindings("client_error_burst", "AI client had repeated denied or failed Gateway calls", clientFailures, 3, "client")...)
	findings = append(findings, governanceThresholdFindings("tool_error_burst", "Gateway tool had repeated denied or failed calls", toolFailures, 3, "tool")...)
	if metrics.FailureCount >= 10 {
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:     "high_failure_volume",
			Severity: "warning",
			Summary:  "recent Gateway failures are elevated",
			Count:    metrics.FailureCount,
		})
	}
	return limitGovernanceFindings(findings, 30)
}

func governanceHighRiskResourceScopeFindings(now time.Time, accessPolicies []domainaigateway.AccessPolicy, toolGrants []domainaigateway.ToolGrant, tools []domainaigateway.ToolCapability, skills []domainaigateway.SkillCapability) []domainaigateway.GovernanceFinding {
	findings := make([]domainaigateway.GovernanceFinding, 0)
	for _, policy := range accessPolicies {
		if !policy.Enabled || grantEffect(policy.Effect) != "allow" || gatewayResourceScopesConstrained(policy.ResourceScopes) {
			continue
		}
		decision := accessPolicyRiskDecision(policy)
		if decision.Strategy == gatewayRiskDeny || decision.Strategy == gatewayRiskDryRunOnly {
			continue
		}
		riskLevels := governanceAccessPolicyHighRiskLevels(policy, tools, skills)
		if len(riskLevels) == 0 {
			continue
		}
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:        "high_risk_allow_without_resource_scope",
			Severity:    "warning",
			Summary:     "AI access policy allows high-risk Gateway tools without a concrete resource scope constraint",
			Count:       len(riskLevels),
			SubjectType: policy.SubjectType,
			SubjectID:   policy.SubjectID,
			AIClientID:  policy.AIClientID,
			PolicyID:    policy.ID,
			RiskLevel:   governanceHighestRiskLevel(riskLevels),
		})
	}
	for _, grant := range toolGrants {
		if grantEffect(grant.Effect) != "allow" || gatewayResourceScopesConstrained(grant.ResourceScopes) {
			continue
		}
		if grant.ExpiresAt != nil && !grant.ExpiresAt.After(now) {
			continue
		}
		riskLevels := governanceToolGrantHighRiskLevels(grant, tools)
		if len(riskLevels) == 0 {
			continue
		}
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:        "high_risk_grant_without_resource_scope",
			Severity:    "warning",
			Summary:     "MCP tool grant allows high-risk Gateway tools without a concrete resource scope constraint",
			Count:       len(riskLevels),
			SubjectType: grant.SubjectType,
			SubjectID:   grant.SubjectID,
			AIClientID:  grant.AIClientID,
			GrantID:     grant.ID,
			ToolName:    grant.ToolName,
			RiskLevel:   governanceHighestRiskLevel(riskLevels),
		})
	}
	return findings
}

func governanceHighRiskAllowFindings(now time.Time, accessPolicies []domainaigateway.AccessPolicy, toolGrants []domainaigateway.ToolGrant, tools []domainaigateway.ToolCapability, skills []domainaigateway.SkillCapability) []domainaigateway.GovernanceFinding {
	findings := make([]domainaigateway.GovernanceFinding, 0)
	for _, policy := range accessPolicies {
		if !policy.Enabled || grantEffect(policy.Effect) != "allow" || governanceAccessPolicyHasRiskGuard(policy) {
			continue
		}
		riskLevels := governanceAccessPolicyUnguardedRiskLevels(policy, tools, skills)
		if len(riskLevels) == 0 {
			continue
		}
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:        "high_risk_allow_without_approval",
			Severity:    "warning",
			Summary:     "AI access policy allows high-risk Gateway tools without approval, human confirmation, or dry-run guard",
			Count:       len(riskLevels),
			SubjectType: policy.SubjectType,
			SubjectID:   policy.SubjectID,
			AIClientID:  policy.AIClientID,
			PolicyID:    policy.ID,
			RiskLevel:   governanceHighestRiskLevel(riskLevels),
		})
	}
	for _, grant := range toolGrants {
		if grantEffect(grant.Effect) != "allow" || grant.RequiresApproval {
			continue
		}
		if grant.ExpiresAt != nil && !grant.ExpiresAt.After(now) {
			continue
		}
		riskLevels := governanceToolGrantUnguardedRiskLevels(grant, tools)
		if len(riskLevels) == 0 {
			continue
		}
		findings = append(findings, domainaigateway.GovernanceFinding{
			Type:        "high_risk_grant_without_approval",
			Severity:    "warning",
			Summary:     "MCP tool grant allows high-risk Gateway tools without approval",
			Count:       len(riskLevels),
			SubjectType: grant.SubjectType,
			SubjectID:   grant.SubjectID,
			AIClientID:  grant.AIClientID,
			GrantID:     grant.ID,
			ToolName:    grant.ToolName,
			RiskLevel:   governanceHighestRiskLevel(riskLevels),
		})
	}
	return findings
}

func governanceAccessPolicyHasRiskGuard(policy domainaigateway.AccessPolicy) bool {
	decision := accessPolicyRiskDecision(policy)
	return governanceRiskDecisionHasGuard(decision.Strategy)
}

func governanceRiskDecisionHasGuard(strategy gatewayRiskStrategy) bool {
	switch strategy {
	case gatewayRiskDeny, gatewayRiskRequireApproval, gatewayRiskRequireHumanConfirm, gatewayRiskDryRunOnly:
		return true
	default:
		return false
	}
}

func governanceAccessPolicyUnguardedRiskLevels(policy domainaigateway.AccessPolicy, tools []domainaigateway.ToolCapability, skills []domainaigateway.SkillCapability) []domainaigateway.RiskLevel {
	decision := accessPolicyRiskDecision(policy)
	explicitAllowOverride := decision.Strategy == gatewayRiskAllow
	knownHighRiskMatch := false
	riskLevels := map[domainaigateway.RiskLevel]struct{}{}
	for _, tool := range tools {
		if !governanceRiskLevelNeedsGuard(tool.RiskLevel) || !accessPolicyToolSelectorsMatchWithSkills(policy, tool, "", skills) {
			continue
		}
		knownHighRiskMatch = true
		if explicitAllowOverride || !tool.RequiresApproval {
			riskLevels[tool.RiskLevel] = struct{}{}
		}
	}
	if !knownHighRiskMatch {
		for _, riskLevel := range policy.RiskLevels {
			if governanceRiskLevelNeedsGuard(riskLevel) {
				riskLevels[riskLevel] = struct{}{}
			}
		}
	}
	return governanceSortedRiskLevels(riskLevels)
}

func governanceAccessPolicyHighRiskLevels(policy domainaigateway.AccessPolicy, tools []domainaigateway.ToolCapability, skills []domainaigateway.SkillCapability) []domainaigateway.RiskLevel {
	knownHighRiskMatch := false
	riskLevels := map[domainaigateway.RiskLevel]struct{}{}
	for _, tool := range tools {
		if !governanceRiskLevelNeedsGuard(tool.RiskLevel) || !accessPolicyToolSelectorsMatchWithSkills(policy, tool, "", skills) {
			continue
		}
		knownHighRiskMatch = true
		riskLevels[tool.RiskLevel] = struct{}{}
	}
	if !knownHighRiskMatch {
		for _, riskLevel := range policy.RiskLevels {
			if governanceRiskLevelNeedsGuard(riskLevel) {
				riskLevels[riskLevel] = struct{}{}
			}
		}
	}
	return governanceSortedRiskLevels(riskLevels)
}

func governanceToolGrantHighRiskLevels(grant domainaigateway.ToolGrant, tools []domainaigateway.ToolCapability) []domainaigateway.RiskLevel {
	knownHighRiskMatch := false
	riskLevels := map[domainaigateway.RiskLevel]struct{}{}
	for _, tool := range tools {
		if !governanceRiskLevelNeedsGuard(tool.RiskLevel) || !grantMatchesTool(grant.ToolName, tool.Name) {
			continue
		}
		knownHighRiskMatch = true
		riskLevels[tool.RiskLevel] = struct{}{}
	}
	if !knownHighRiskMatch && governanceRiskLevelNeedsGuard(grant.RiskLevel) {
		riskLevels[grant.RiskLevel] = struct{}{}
	}
	return governanceSortedRiskLevels(riskLevels)
}

func governanceToolGrantUnguardedRiskLevels(grant domainaigateway.ToolGrant, tools []domainaigateway.ToolCapability) []domainaigateway.RiskLevel {
	knownHighRiskMatch := false
	riskLevels := map[domainaigateway.RiskLevel]struct{}{}
	for _, tool := range tools {
		if !governanceRiskLevelNeedsGuard(tool.RiskLevel) || !grantMatchesTool(grant.ToolName, tool.Name) {
			continue
		}
		knownHighRiskMatch = true
		if !tool.RequiresApproval {
			riskLevels[tool.RiskLevel] = struct{}{}
		}
	}
	if !knownHighRiskMatch && governanceRiskLevelNeedsGuard(grant.RiskLevel) {
		riskLevels[grant.RiskLevel] = struct{}{}
	}
	return governanceSortedRiskLevels(riskLevels)
}

func governanceRiskLevelNeedsGuard(riskLevel domainaigateway.RiskLevel) bool {
	switch riskLevel {
	case domainaigateway.RiskLevelMutate, domainaigateway.RiskLevelExecute, domainaigateway.RiskLevelHigh:
		return true
	default:
		return false
	}
}

func governanceSortedRiskLevels(items map[domainaigateway.RiskLevel]struct{}) []domainaigateway.RiskLevel {
	out := make([]domainaigateway.RiskLevel, 0, len(items))
	for _, riskLevel := range []domainaigateway.RiskLevel{domainaigateway.RiskLevelMutate, domainaigateway.RiskLevelExecute, domainaigateway.RiskLevelHigh} {
		if _, ok := items[riskLevel]; ok {
			out = append(out, riskLevel)
		}
	}
	return out
}

func governanceHighestRiskLevel(items []domainaigateway.RiskLevel) domainaigateway.RiskLevel {
	if len(items) == 0 {
		return ""
	}
	return items[len(items)-1]
}

func governanceThresholdFindings(kind, summary string, counts map[string]int, threshold int, target string) []domainaigateway.GovernanceFinding {
	items := make([]domainaigateway.GovernanceFinding, 0)
	for key, count := range counts {
		if count < threshold {
			continue
		}
		finding := domainaigateway.GovernanceFinding{
			Type:     kind,
			Severity: "warning",
			Summary:  summary,
			Count:    count,
		}
		switch target {
		case "actor":
			parts := strings.SplitN(key, ":", 2)
			if len(parts) == 2 {
				finding.ActorType = parts[0]
				finding.ActorID = parts[1]
			} else {
				finding.ActorID = key
			}
		case "client":
			finding.AIClientID = key
		case "tool":
			finding.ToolName = key
		}
		items = append(items, finding)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Summary < items[j].Summary
		}
		return items[i].Count > items[j].Count
	})
	return items
}

func limitGovernanceFindings(items []domainaigateway.GovernanceFinding, limit int) []domainaigateway.GovernanceFinding {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func governanceRecommendationActions(tokens domainaigateway.GovernanceTokenSummary, clients domainaigateway.GovernanceClientSummary, approvals domainaigateway.GovernanceApprovalSummary, policies domainaigateway.GovernancePolicyCoverage, anomalies []domainaigateway.GovernanceFinding) []domainaigateway.GovernanceRecommendationAction {
	out := make([]domainaigateway.GovernanceRecommendationAction, 0)
	if len(tokens.ExpiredActive) > 0 || len(tokens.ExpiringSoon) > 0 {
		findings := append(tokens.ExpiredActive, tokens.ExpiringSoon...)
		out = append(out, domainaigateway.GovernanceRecommendationAction{
			Type:       "token_rotation",
			Severity:   governanceRecommendationSeverity(len(tokens.ExpiredActive) > 0, len(tokens.ExpiringSoon) > 0),
			Summary:    "rotate or revoke expired and soon-expiring Gateway tokens",
			Action:     "rotate_or_revoke_tokens",
			TargetKind: "tokens",
			Refs:       governanceTokenFindingIDs(findings),
			Count:      len(tokens.ExpiredActive) + len(tokens.ExpiringSoon),
			Metadata:   map[string]any{"tokenRefs": governanceTokenFindingActionRefs(findings)},
		})
	}
	if len(tokens.Stale) > 0 || len(tokens.NeverUsed) > 0 {
		findings := append(tokens.Stale, tokens.NeverUsed...)
		out = append(out, domainaigateway.GovernanceRecommendationAction{
			Type:       "token_hygiene",
			Severity:   "warning",
			Summary:    "review stale or never-used Gateway tokens and remove unused automation credentials",
			Action:     "review_and_revoke_unused_tokens",
			TargetKind: "tokens",
			Refs:       governanceTokenFindingIDs(findings),
			Count:      len(tokens.Stale) + len(tokens.NeverUsed),
			Metadata:   map[string]any{"tokenRefs": governanceTokenFindingActionRefs(findings)},
		})
	}
	if clients.RegistrationApproval == "not_configured" {
		out = append(out, domainaigateway.GovernanceRecommendationAction{
			Type:       "client_registration_approval",
			Severity:   "warning",
			Summary:    "define AI client registration approval metadata or workflow before allowing broad external client onboarding",
			Action:     "configure_client_registration_approval",
			TargetKind: "ai_clients",
		})
	}
	if approvals.Overdue > 0 || approvals.DueSoon > 0 || approvals.StalePending > 0 {
		out = append(out, domainaigateway.GovernanceRecommendationAction{
			Type:       "approval_sla",
			Severity:   governanceRecommendationSeverity(approvals.Overdue > 0, approvals.DueSoon > 0 || approvals.StalePending > 0),
			Summary:    "resolve Gateway approval requests that are stale or approaching SLA expiration",
			Action:     "resolve_gateway_approvals",
			TargetKind: "approval_requests",
			Refs:       governanceApprovalRecommendationRefs(approvals),
			Count:      approvals.Overdue + approvals.DueSoon + approvals.StalePending,
		})
	}
	if policies.BudgetPolicies == 0 {
		out = append(out, governancePolicyCoverageRecommendation("budget_coverage", "add AI access policy budget conditions for high-volume users or clients", "create_budget_guardrail_policy", "budget", policies.ActiveAccessPolicies))
	}
	if policies.RateLimitPolicies == 0 {
		out = append(out, governancePolicyCoverageRecommendation("rate_limit_coverage", "add AI access policy rateLimit conditions for shared clients and service accounts", "create_rate_limit_guardrail_policy", "rate_limit", policies.ActiveAccessPolicies))
	}
	if policies.RedactionPolicies == 0 {
		out = append(out, governancePolicyCoverageRecommendation("redaction_coverage", "configure explicit redactionPolicy conditions where built-in Gateway redaction is not enough", "create_redaction_guardrail_policy", "redaction", policies.ActiveAccessPolicies))
	}
	if policies.ResourceScopedAccessPolicies == 0 && policies.ResourceScopedToolGrants == 0 {
		out = append(out, governancePolicyCoverageRecommendation("resource_scope_coverage", "add concrete resourceScopes to AI access policies or tool grants before expanding cross-environment Gateway access", "create_resource_scope_guardrail_policy", "resource_scopes", policies.ActiveAccessPolicies+policies.ActiveToolGrants))
	}
	if governanceHasFindingType(anomalies, "high_risk_allow_without_approval", "high_risk_grant_without_approval") {
		out = append(out, domainaigateway.GovernanceRecommendationAction{
			Type:       "high_risk_guardrails",
			Severity:   "warning",
			Summary:    "require approval, human confirmation, or dry-run policies for high-risk Gateway allow rules",
			Action:     "create_high_risk_approval_guardrail",
			TargetKind: "access_policies",
			Refs:       governanceFindingRefs(anomalies, "high_risk_allow_without_approval", "high_risk_grant_without_approval"),
			Count:      governanceFindingTypeCount(anomalies, "high_risk_allow_without_approval", "high_risk_grant_without_approval"),
			Metadata:   map[string]any{"policyTemplate": "approval_guardrail"},
		})
	}
	if governanceHasFindingType(anomalies, "high_risk_allow_without_resource_scope", "high_risk_grant_without_resource_scope") {
		out = append(out, domainaigateway.GovernanceRecommendationAction{
			Type:       "high_risk_resource_scopes",
			Severity:   "warning",
			Summary:    "add concrete resourceScopes to high-risk Gateway allow policies and tool grants",
			Action:     "create_resource_scope_guardrail",
			TargetKind: "access_policies",
			Refs:       governanceFindingRefs(anomalies, "high_risk_allow_without_resource_scope", "high_risk_grant_without_resource_scope"),
			Count:      governanceFindingTypeCount(anomalies, "high_risk_allow_without_resource_scope", "high_risk_grant_without_resource_scope"),
			Metadata:   map[string]any{"policyTemplate": "resource_scope_guardrail"},
		})
	}
	if len(anomalies) > 0 {
		out = append(out, domainaigateway.GovernanceRecommendationAction{
			Type:       "anomaly_review",
			Severity:   "warning",
			Summary:    "investigate recent Gateway anomaly findings before expanding tool access",
			Action:     "review_governance_anomalies",
			TargetKind: "anomalies",
			Count:      len(anomalies),
		})
	}
	return out
}

func governanceRecommendationSummaries(actions []domainaigateway.GovernanceRecommendationAction) []string {
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.Summary) != "" {
			out = append(out, action.Summary)
		}
	}
	return out
}

func governanceRecommendationSeverity(critical, warning bool) string {
	if critical {
		return "critical"
	}
	if warning {
		return "warning"
	}
	return "info"
}

func governancePolicyCoverageRecommendation(kind, summary, action, template string, count int) domainaigateway.GovernanceRecommendationAction {
	return domainaigateway.GovernanceRecommendationAction{
		Type:       kind,
		Severity:   "warning",
		Summary:    summary,
		Action:     action,
		TargetKind: "access_policies",
		Count:      count,
		Metadata:   map[string]any{"policyTemplate": template},
	}
}

func governanceTokenFindingIDs(items []domainaigateway.GovernanceTokenFinding) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		ref := strings.TrimSpace(item.ID)
		if ref == "" {
			ref = strings.TrimSpace(item.TokenPrefix)
		}
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func governanceTokenFindingActionRefs(items []domainaigateway.GovernanceTokenFinding) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		prefix := strings.TrimSpace(item.TokenPrefix)
		ref := id
		if ref == "" {
			ref = prefix
		}
		if ref == "" {
			continue
		}
		key := strings.TrimSpace(item.Kind) + ":" + ref
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tokenRef := map[string]any{
			"kind":        strings.TrimSpace(item.Kind),
			"id":          id,
			"tokenPrefix": prefix,
		}
		if ownerID := strings.TrimSpace(item.OwnerID); ownerID != "" {
			tokenRef["ownerId"] = ownerID
		}
		out = append(out, tokenRef)
	}
	return out
}

func governanceApprovalRecommendationRefs(approvals domainaigateway.GovernanceApprovalSummary) []string {
	refs := append([]string{}, approvals.OverdueRequestIDs...)
	refs = append(refs, approvals.DueSoonRequestIDs...)
	refs = append(refs, approvals.StalePendingRequestIDs...)
	return compactUniqueStrings(refs)
}

func governanceFindingRefs(findings []domainaigateway.GovernanceFinding, types ...string) []string {
	out := make([]string, 0)
	for _, finding := range findings {
		if !governanceFindingTypeMatches(finding.Type, types...) {
			continue
		}
		ref := firstNonEmpty(finding.PolicyID, finding.GrantID, finding.ApprovalRequestID, finding.AIClientID, finding.ToolName, finding.ActorID, finding.SubjectID)
		if ref != "" {
			out = append(out, ref)
		}
	}
	return compactUniqueStrings(out)
}

func governanceFindingTypeMatches(value string, types ...string) bool {
	for _, kind := range types {
		if value == kind {
			return true
		}
	}
	return false
}

func compactUniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func governanceHasFindingType(findings []domainaigateway.GovernanceFinding, types ...string) bool {
	for _, finding := range findings {
		for _, kind := range types {
			if finding.Type == kind {
				return true
			}
		}
	}
	return false
}

func governanceHealth(tokens domainaigateway.GovernanceTokenSummary, metrics domainaigateway.GovernanceMetrics, clients domainaigateway.GovernanceClientSummary, approvals domainaigateway.GovernanceApprovalSummary, policies domainaigateway.GovernancePolicyCoverage, anomalies []domainaigateway.GovernanceFinding, pendingApprovals []domainaigateway.ApprovalRequest) domainaigateway.GovernanceHealth {
	unguardedHighRiskAllows := governanceFindingTypeCount(anomalies, "high_risk_allow_without_approval", "high_risk_grant_without_approval")
	unscopedHighRiskAllows := governanceFindingTypeCount(anomalies, "high_risk_allow_without_resource_scope", "high_risk_grant_without_resource_scope")
	resourceScopedControls := policies.ResourceScopedAccessPolicies + policies.ResourceScopedToolGrants
	checks := []domainaigateway.GovernanceHealthCheck{
		{
			Name:    "token_expiration",
			Status:  governanceCheckStatus(len(tokens.ExpiredActive) == 0, len(tokens.ExpiringSoon) == 0),
			Message: "tracks active expired and soon-expiring personal/service account tokens",
			Count:   len(tokens.ExpiredActive) + len(tokens.ExpiringSoon),
		},
		{
			Name:    "token_last_used",
			Status:  governanceCheckStatus(true, len(tokens.Stale) == 0 && len(tokens.NeverUsed) == 0),
			Message: "last_used_at is updated during Gateway token authentication and surfaced for stale-token review",
			Count:   len(tokens.Stale) + len(tokens.NeverUsed),
		},
		{
			Name:    "recent_invocations",
			Status:  governanceCheckStatus(metrics.FailureCount < 10, metrics.DenyCount+metrics.FailureCount < 3),
			Message: "summarizes recent Gateway audit results and denial/failure pressure",
			Count:   metrics.TotalCalls,
		},
		{
			Name:    "pending_approvals",
			Status:  governanceCheckStatus(true, len(pendingApprovals) == 0),
			Message: "tracks Gateway approval requests still waiting for decision",
			Count:   len(pendingApprovals),
		},
		{
			Name:    "approval_sla",
			Status:  governanceCheckStatus(approvals.Overdue == 0, approvals.DueSoon == 0 && approvals.StalePending == 0),
			Message: "tracks pending Gateway approvals that are overdue, approaching SLA expiration, or stale",
			Count:   approvals.Overdue + approvals.DueSoon + approvals.StalePending,
		},
		{
			Name:    "client_registration_approval",
			Status:  governanceCheckStatus(true, clients.RegistrationApproval != "not_configured"),
			Message: "reports whether AI client registration approval is configured or pending",
			Count:   clients.PendingApproval,
		},
		{
			Name:    "policy_coverage",
			Status:  governanceCheckStatus(true, policies.BudgetPolicies > 0 && policies.RateLimitPolicies > 0 && policies.RedactionPolicies > 0),
			Message: "reports active budget, rate-limit, and redaction policy coverage from enabled AI access policy conditions",
			Count:   policies.BudgetPolicies + policies.RateLimitPolicies + policies.RedactionPolicies,
		},
		{
			Name:    "resource_scope_coverage",
			Status:  governanceCheckStatus(true, resourceScopedControls > 0),
			Message: "reports active resourceScopes coverage from enabled AI access policies and unexpired tool grants",
			Count:   resourceScopedControls,
		},
		{
			Name:    "high_risk_guardrails",
			Status:  governanceCheckStatus(true, unguardedHighRiskAllows == 0),
			Message: "flags high-risk Gateway allow policies or grants that can run without approval, human confirmation, or dry-run guard",
			Count:   unguardedHighRiskAllows,
		},
		{
			Name:    "high_risk_resource_scopes",
			Status:  governanceCheckStatus(true, unscopedHighRiskAllows == 0),
			Message: "flags high-risk Gateway allow policies or grants that lack concrete resource scope constraints",
			Count:   unscopedHighRiskAllows,
		},
	}
	status := "healthy"
	for _, check := range checks {
		status = worseGovernanceStatus(status, check.Status)
	}
	for _, finding := range anomalies {
		if finding.Severity == "critical" {
			status = "critical"
			break
		}
		if finding.Severity == "warning" {
			status = worseGovernanceStatus(status, "degraded")
		}
	}
	message := "AI Gateway governance controls are healthy"
	if status == "degraded" {
		message = "AI Gateway governance has warnings to review"
	}
	if status == "critical" {
		message = "AI Gateway governance has critical findings"
	}
	return domainaigateway.GovernanceHealth{Status: status, Message: message, Checks: checks}
}

func governanceFindingTypeCount(findings []domainaigateway.GovernanceFinding, types ...string) int {
	out := 0
	for _, finding := range findings {
		for _, kind := range types {
			if finding.Type == kind {
				out++
				break
			}
		}
	}
	return out
}

func governanceCheckStatus(criticalOK, warningOK bool) string {
	if !criticalOK {
		return "critical"
	}
	if !warningOK {
		return "degraded"
	}
	return "healthy"
}

func worseGovernanceStatus(left, right string) string {
	rank := map[string]int{"healthy": 1, "degraded": 2, "critical": 3}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func incrementStringCount(values map[string]int, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "(unknown)"
	}
	values[key]++
}

func topGovernanceCounts(values map[string]int, limit int) []domainaigateway.GovernanceMetricCount {
	out := make([]domainaigateway.GovernanceMetricCount, 0, len(values))
	for key, count := range values {
		out = append(out, domainaigateway.GovernanceMetricCount{Key: key, Count: count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

func sanitizeGatewayMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	out, ok := sanitizeGatewayValue(values).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return out
}

func sanitizeGatewayValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if gatewaySensitiveKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = sanitizeGatewayValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = sanitizeGatewayValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for index, item := range typed {
			out[index] = sanitizeGatewayMap(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	case string:
		return redactSensitiveText(typed)
	default:
		return typed
	}
}

func cloneGatewayValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneGatewayValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = cloneGatewayValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for index, item := range typed {
			out[index] = cloneGatewayMap(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func cloneGatewayMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = cloneGatewayValue(value)
	}
	return out
}

func gatewaySensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, needle := range []string{"token", "password", "passwd", "secret", "credential", "apikey", "api_key", "authorization", "kubeconfig", "envvar", "environmentvariable"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return normalized == "env" || strings.HasPrefix(normalized, "env_")
}

func redactExecutionLogs(items []domaindelivery.ExecutionLog) []domaindelivery.ExecutionLog {
	out := make([]domaindelivery.ExecutionLog, len(items))
	copy(out, items)
	for index := range out {
		out[index].Message = redactSensitiveText(out[index].Message)
		out[index].Metadata = redactMap(out[index].Metadata)
	}
	return out
}

func redactPodLogs(item domainresource.PodLogsView) domainresource.PodLogsView {
	item.Content = redactSensitiveText(item.Content)
	item.ContentBytes = int64(len(item.Content))
	return item
}

func redactSensitiveText(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return gatewaySensitiveValuePattern.ReplaceAllString(value, "$1$2[REDACTED]")
}

func redactMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "password") || strings.Contains(lowerKey, "secret") || strings.Contains(lowerKey, "credential") || strings.Contains(lowerKey, "api_key") || strings.Contains(lowerKey, "apikey") {
			out[key] = "[REDACTED]"
			continue
		}
		switch typed := value.(type) {
		case string:
			out[key] = redactSensitiveText(typed)
		case map[string]any:
			out[key] = redactMap(typed)
		case []any:
			out[key] = sanitizeGatewayValue(typed)
		case []map[string]any:
			out[key] = sanitizeGatewayValue(typed)
		default:
			out[key] = value
		}
	}
	return out
}

func filterPodsForDiagnosis(items []domainresource.PodView, podName, workloadName string) []domainresource.PodView {
	podName = strings.TrimSpace(podName)
	workloadName = strings.TrimSpace(workloadName)
	if podName == "" && workloadName == "" {
		return items
	}
	out := make([]domainresource.PodView, 0, len(items))
	for _, item := range items {
		if podName != "" && item.Name == podName {
			out = append(out, item)
			continue
		}
		if workloadName != "" && strings.Contains(item.Name, workloadName) {
			out = append(out, item)
		}
	}
	return out
}

func filterDeploymentsForDiagnosis(items []domainresource.DeploymentView, workloadName string) []domainresource.DeploymentView {
	workloadName = strings.TrimSpace(workloadName)
	if workloadName == "" {
		return items
	}
	out := make([]domainresource.DeploymentView, 0, len(items))
	for _, item := range items {
		if item.Name == workloadName || strings.Contains(item.Name, workloadName) {
			out = append(out, item)
		}
	}
	return out
}

func filterEventsForDiagnosis(items []domainresource.ClusterEventView, podName, workloadName string) []domainresource.ClusterEventView {
	podName = strings.TrimSpace(podName)
	workloadName = strings.TrimSpace(workloadName)
	if podName == "" && workloadName == "" {
		return items
	}
	out := make([]domainresource.ClusterEventView, 0, len(items))
	for _, item := range items {
		if podName != "" && (item.InvolvedName == podName || strings.Contains(item.Message, podName)) {
			out = append(out, item)
			continue
		}
		if workloadName != "" && (item.InvolvedName == workloadName || strings.Contains(item.InvolvedName, workloadName) || strings.Contains(item.Message, workloadName)) {
			out = append(out, item)
		}
	}
	return out
}
