package aigateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainresource "github.com/soha/soha/internal/domain/resource"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/requestctx"
)

const manifestVersion = "v1alpha1"

var gatewaySensitiveValuePattern = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key|authorization|credential)(\s*[:=]\s*)([^\s,;]+)`)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Repository interface {
	ListPersonalAccessTokens(context.Context, string) ([]domainaigateway.PersonalAccessToken, error)
	CreatePersonalAccessToken(context.Context, domainaigateway.PersonalAccessToken) (domainaigateway.PersonalAccessToken, error)
	RevokePersonalAccessToken(context.Context, string, string) error
	ListServiceAccounts(context.Context) ([]domainaigateway.ServiceAccount, error)
	CreateServiceAccount(context.Context, domainaigateway.ServiceAccount) (domainaigateway.ServiceAccount, error)
	GetServiceAccount(context.Context, string) (domainaigateway.ServiceAccount, error)
	CreateServiceAccountToken(context.Context, domainaigateway.ServiceAccountToken) (domainaigateway.ServiceAccountToken, error)
	RevokeServiceAccountToken(context.Context, string) error
	ListAIClients(context.Context) ([]domainaigateway.AIClient, error)
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
	CreateAuditLog(context.Context, domainaigateway.AuditLog) error
}

type ApplicationService interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Create(context.Context, domainidentity.Principal, domainapp.UpsertInput) (domainapp.App, error)
}

type DeliveryService interface {
	GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error)
	GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error)
	TriggerApplicationDeliveryAction(context.Context, domainidentity.Principal, string, domaindelivery.ApplicationDeliveryActionInput) (domaindelivery.ApplicationDeliveryActionResult, error)
	ListReleaseBundles(context.Context, domainidentity.Principal, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error)
	ListReleaseBundleArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
	ListExecutionTasks(context.Context, domainidentity.Principal, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error)
	ListExecutionLogs(context.Context, domainidentity.Principal, string, int) ([]domaindelivery.ExecutionLog, error)
}

type ResourceService interface {
	ListPods(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodView, error)
	GetPodLogs(context.Context, domainidentity.Principal, string, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error)
	ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error)
	ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error)
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
}

type Service struct {
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	repo        Repository
	apps        ApplicationService
	delivery    DeliveryService
	resources   ResourceService
}

func New(permissions *appaccess.PermissionResolver, audit AuditRecorder, repos ...Repository) *Service {
	var repo Repository
	if len(repos) > 0 {
		repo = repos[0]
	}
	return &Service{permissions: permissions, audit: audit, repo: repo}
}

func (s *Service) SetDeliveryServices(apps ApplicationService, delivery DeliveryService) {
	s.apps = apps
	s.delivery = delivery
}

func (s *Service) SetResourceService(resources ResourceService) {
	s.resources = resources
}

func (s *Service) Capabilities(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ManifestRequest) (domainaigateway.Manifest, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayView); err != nil {
		_ = s.recordManifestAudit(ctx, principal, input, "deny", err.Error(), 0, len(defaultTools()))
		return domainaigateway.Manifest{}, err
	}
	permissionKeys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}

	tools, deniedTools := filterTools(defaultTools(), permissionKeys)
	resources, deniedResources := filterResources(defaultResources(), permissionKeys)
	prompts, deniedPrompts := filterPrompts(defaultPrompts(), permissionKeys)
	skills, deniedSkills := filterSkills(defaultSkills(), permissionKeys)
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
	skills, bindingDeniedSkills := filterSkillsByBindings(skills, bindings)
	deniedSkills += bindingDeniedSkills
	tools, bindingDeniedTools := filterToolsBySkillBindings(tools, bindings, input.SkillID)
	deniedTools += bindingDeniedTools
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

func (s *Service) InvokeTool(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	toolName := strings.TrimSpace(input.ToolName)
	tool, ok := toolByName(toolName)
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
	if err := s.authorizeToolGrant(ctx, principal, input.AIClientID, tool); err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}
	if err := s.authorizeAccessPolicy(ctx, principal, input.AIClientID, input.SkillID, &tool); err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}
	if err := s.authorizeSkillBinding(ctx, principal, input.AIClientID, input.SkillID, tool); err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}

	output, relatedIDs, err := s.invokeGatewayTool(ctx, principal, tool, input.Input)
	if err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "failure", err.Error(), relatedIDs)
		return domainaigateway.ToolInvocationResult{}, err
	}
	_ = s.recordToolAudit(ctx, principal, input, tool, "success", "invoked AI Gateway tool", relatedIDs)
	return domainaigateway.ToolInvocationResult{
		ToolName:         tool.Name,
		RiskLevel:        tool.RiskLevel,
		RequiresApproval: tool.RequiresApproval,
		Result:           "success",
		Output:           output,
		RelatedIDs:       relatedIDs,
		Audit: map[string]any{
			"riskLevel":        tool.RiskLevel,
			"requiresApproval": tool.RequiresApproval,
		},
	}, nil
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

func (s *Service) authorizeToolGrant(ctx context.Context, principal domainidentity.Principal, aiClientID string, tool domainaigateway.ToolCapability) error {
	grants, err := s.activeToolGrants(ctx, principal, aiClientID)
	if err != nil {
		return err
	}
	if len(grants) == 0 {
		return nil
	}
	permissionKeys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return err
	}
	allowed, reason := toolAllowedByGrants(tool, grants, permissionKeys)
	if allowed {
		return nil
	}
	return fmt.Errorf("%w: AI Gateway tool grant rejected %s: %s", apperrors.ErrAccessDenied, tool.Name, reason)
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
		allowed, _ := toolAllowedByGrants(tool, grants, permissionKeys)
		if allowed {
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
	if aiClientID != "" {
		if err := appendGrants("ai_client", aiClientID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) authorizeAccessPolicy(ctx context.Context, principal domainidentity.Principal, aiClientID, skillID string, tool *domainaigateway.ToolCapability) error {
	policies, err := s.activeAccessPolicies(ctx, principal, aiClientID)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	allowed, requiresApproval, reason := toolAllowedByAccessPolicies(*tool, policies, skillID)
	if !allowed {
		return fmt.Errorf("%w: AI Gateway access policy rejected %s: %s", apperrors.ErrAccessDenied, tool.Name, reason)
	}
	if requiresApproval {
		tool.RequiresApproval = true
	}
	return nil
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
		allowed, requiresApproval, _ := toolAllowedByAccessPolicies(tool, policies, skillID)
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
	allowed, reason := toolAllowedBySkillBindings(tool, bindings, skillID)
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
	default:
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
}

func (s *Service) invokeKubernetesTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if s.resources == nil {
		return nil, nil, fmt.Errorf("%w: Kubernetes resource gateway service is not configured", apperrors.ErrInvalidArgument)
	}
	var req struct {
		ClusterID    string `json:"clusterId"`
		Namespace    string `json:"namespace"`
		PodName      string `json:"podName"`
		Container    string `json:"container"`
		TailLines    int64  `json:"tailLines"`
		SinceSeconds int64  `json:"sinceSeconds"`
		Previous     bool   `json:"previous"`
		Limit        int    `json:"limit"`
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
	case "k8s.deployments.list":
		items, err := s.resources.ListDeployments(ctx, principal, req.ClusterID, req.Namespace)
		related["count"] = len(items)
		return items, related, err
	case "k8s.services.list":
		items, err := s.resources.ListServices(ctx, principal, req.ClusterID, req.Namespace)
		related["count"] = len(items)
		return items, related, err
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
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	related := map[string]any{
		"applicationId":            strings.TrimSpace(req.ApplicationID),
		"applicationEnvironmentId": strings.TrimSpace(req.ApplicationEnvironmentID),
		"releaseBundleId":          strings.TrimSpace(req.ReleaseBundleID),
		"executionTaskId":          strings.TrimSpace(req.ExecutionTaskID),
		"clusterId":                strings.TrimSpace(req.ClusterID),
		"namespace":                strings.TrimSpace(req.Namespace),
	}
	contextView := map[string]any{
		"summary": "collected release failure diagnosis context",
		"scope": map[string]any{
			"applicationId":            strings.TrimSpace(req.ApplicationID),
			"applicationEnvironmentId": strings.TrimSpace(req.ApplicationEnvironmentID),
			"releaseBundleId":          strings.TrimSpace(req.ReleaseBundleID),
			"executionTaskId":          strings.TrimSpace(req.ExecutionTaskID),
			"clusterId":                strings.TrimSpace(req.ClusterID),
			"namespace":                strings.TrimSpace(req.Namespace),
			"workloadKind":             strings.TrimSpace(req.WorkloadKind),
			"workloadName":             strings.TrimSpace(req.WorkloadName),
			"podName":                  strings.TrimSpace(req.PodName),
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
		if taskID := strings.TrimSpace(req.ExecutionTaskID); taskID != "" {
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
		if bundleID := strings.TrimSpace(req.ReleaseBundleID); bundleID != "" {
			artifacts, err := s.delivery.ListReleaseBundleArtifacts(ctx, principal, bundleID)
			if err != nil {
				deliveryEvidence["releaseBundleArtifactsError"] = err.Error()
			} else {
				deliveryEvidence["releaseBundleArtifacts"] = artifacts
				deliveryEvidence["releaseBundleArtifactCount"] = len(artifacts)
				related["releaseBundleArtifactCount"] = len(artifacts)
			}
		}
		if strings.TrimSpace(req.ApplicationID) != "" || strings.TrimSpace(req.ApplicationEnvironmentID) != "" || strings.TrimSpace(req.ReleaseBundleID) != "" {
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

	if s.resources != nil && strings.TrimSpace(req.ClusterID) != "" {
		clusterID := strings.TrimSpace(req.ClusterID)
		namespace := strings.TrimSpace(req.Namespace)
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
		if podName := strings.TrimSpace(req.PodName); podName != "" {
			logs, err := s.resources.GetPodLogs(ctx, principal, clusterID, namespace, podName, req.Container, 200, 0, false)
			if err != nil {
				runtimeEvidence["podLogsError"] = err.Error()
			} else {
				runtimeEvidence["podLogs"] = redactPodLogs(logs)
			}
		}
	} else if strings.TrimSpace(req.ClusterID) == "" {
		nextChecks = append(nextChecks, "Provide clusterId and namespace to collect runtime Kubernetes evidence.")
	} else {
		runtimeEvidence["error"] = "Kubernetes resource gateway service is not configured"
	}

	contextView["nextChecks"] = nextChecks
	return contextView, related, nil
}

func (s *Service) ListPersonalAccessTokens(ctx context.Context, principal domainidentity.Principal) ([]domainaigateway.PersonalAccessToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
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
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return err
	}
	if s.repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.RevokePersonalAccessToken(ctx, principal.UserID, strings.TrimSpace(tokenID)); err != nil {
		return err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.personal_token.revoke", "success", "revoked personal access token", map[string]any{"tokenId": strings.TrimSpace(tokenID)})
	return nil
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
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayAIClient", created.ID, "ai_gateway.ai_client.create", "success", "created AI Gateway client", map[string]any{
		"aiClientId": created.ID,
		"kind":       created.Kind,
		"status":     created.Status,
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
	_ = s.recordConfigAudit(ctx, principal, "AIGatewayAIClient", updated.ID, "ai_gateway.ai_client.update", "success", "updated AI Gateway client", map[string]any{
		"aiClientId": updated.ID,
		"kind":       updated.Kind,
		"status":     updated.Status,
	})
	return updated, nil
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
	item, err := buildToolGrant(principal, input)
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
	item, err := buildSkillBinding(principal, input, "")
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
	item, err := buildSkillBinding(principal, input, strings.TrimSpace(bindingID))
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
	if !validStatus(status, "active", "disabled") {
		return domainaigateway.AIClient{}, fmt.Errorf("%w: AI client status must be active or disabled", apperrors.ErrInvalidArgument)
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

func buildToolGrant(principal domainidentity.Principal, input domainaigateway.ToolGrantInput) (domainaigateway.ToolGrant, error) {
	subjectType := normalizeSubjectType(input.SubjectType)
	if !validStatus(subjectType, "user", "service_account", "role", "ai_client") {
		return domainaigateway.ToolGrant{}, fmt.Errorf("%w: subjectType must be user, service_account, role, or ai_client", apperrors.ErrInvalidArgument)
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
	if exactTool, ok := toolByName(toolName); ok {
		if riskLevel == "" {
			riskLevel = exactTool.RiskLevel
		}
		if exactTool.RequiresApproval {
			input.RequiresApproval = true
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
	if !validStatus(subjectType, "user", "service_account", "role", "ai_client") {
		return domainaigateway.AccessPolicy{}, fmt.Errorf("%w: subjectType must be user, service_account, role, or ai_client", apperrors.ErrInvalidArgument)
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
		ApprovalPolicy: emptyMap(input.ApprovalPolicy),
		Conditions:     emptyMap(input.Conditions),
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func buildSkillBinding(principal domainidentity.Principal, input domainaigateway.SkillBindingInput, forcedID string) (domainaigateway.SkillBinding, error) {
	id := strings.TrimSpace(forcedID)
	if id == "" {
		id = uuid.NewString()
	}
	subjectType := normalizeSubjectType(input.SubjectType)
	if !validStatus(subjectType, "user", "service_account", "role", "ai_client") {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: subjectType must be user, service_account, role, or ai_client", apperrors.ErrInvalidArgument)
	}
	subjectID := strings.TrimSpace(input.SubjectID)
	if subjectID == "" {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: subjectId is required", apperrors.ErrInvalidArgument)
	}
	skillID := strings.TrimSpace(input.SkillID)
	if skillID == "" {
		return domainaigateway.SkillBinding{}, fmt.Errorf("%w: skillId is required", apperrors.ErrInvalidArgument)
	}
	if _, ok := skillByID(skillID); !ok {
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
	return strings.ToLower(strings.TrimSpace(value))
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

func toolAllowedByGrants(tool domainaigateway.ToolCapability, grants []domainaigateway.ToolGrant, permissionKeys []string) (bool, string) {
	hasAllowGrant := false
	matchedAllowGrant := false
	matchedAllowNeedsPermission := false
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
			continue
		}
		matchedAllowNeedsPermission = true
	}
	for _, grant := range grants {
		if grantEffect(grant.Effect) != "deny" || !grantMatchesTool(grant.ToolName, tool.Name) {
			continue
		}
		return false, "matched deny grant " + grant.ID
	}
	if hasAllowGrant && !matchedAllowGrant {
		if matchedAllowNeedsPermission {
			return false, "matching allow grant requires permissions not granted to the subject"
		}
		return false, "no matching allow grant"
	}
	return true, ""
}

func toolAllowedByAccessPolicies(tool domainaigateway.ToolCapability, policies []domainaigateway.AccessPolicy, skillID string) (bool, bool, string) {
	hasAllowPolicy := false
	matchedAllowPolicy := false
	requiresApproval := tool.RequiresApproval
	for _, policy := range policies {
		if grantEffect(policy.Effect) == "allow" {
			hasAllowPolicy = true
		}
		if grantEffect(policy.Effect) != "deny" || !accessPolicyMatchesTool(policy, tool, skillID) {
			continue
		}
		return false, requiresApproval, "matched deny policy " + policy.ID
	}
	for _, policy := range policies {
		if grantEffect(policy.Effect) != "allow" || !accessPolicyMatchesTool(policy, tool, skillID) {
			continue
		}
		matchedAllowPolicy = true
		if accessPolicyRequiresApproval(policy) {
			requiresApproval = true
		}
	}
	if hasAllowPolicy && !matchedAllowPolicy {
		return false, requiresApproval, "no matching allow policy"
	}
	return true, requiresApproval, ""
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
		return toolInAnySkill(policy.SkillIDs, tool.Name)
	}
	return len(policy.ToolPatterns) > 0 || len(policy.RiskLevels) > 0 || len(policy.SkillIDs) > 0 || policyHasNoSelectors(policy)
}

func policyHasNoSelectors(policy domainaigateway.AccessPolicy) bool {
	return len(policy.ToolPatterns) == 0 && len(policy.RiskLevels) == 0 && len(policy.SkillIDs) == 0
}

func accessPolicyRequiresApproval(policy domainaigateway.AccessPolicy) bool {
	if value, ok := policy.ApprovalPolicy["requiresApproval"].(bool); ok && value {
		return true
	}
	for _, key := range []string{"mode", "approval", "state"} {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(policy.ApprovalPolicy[key])), "required") {
			return true
		}
	}
	return false
}

func filterSkillsByBindings(skills []domainaigateway.SkillCapability, bindings []domainaigateway.SkillBinding) ([]domainaigateway.SkillCapability, int) {
	if len(bindings) == 0 {
		return skills, 0
	}
	bindingRefs := skillBindingRefs(bindings, "")
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
	if len(bindings) == 0 {
		return tools, 0
	}
	allowedRefs := allowedCapabilityRefsForBindings(bindings, skillID)
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

func toolAllowedBySkillBindings(tool domainaigateway.ToolCapability, bindings []domainaigateway.SkillBinding, skillID string) (bool, string) {
	allowedRefs := allowedCapabilityRefsForBindings(bindings, skillID)
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

func allowedCapabilityRefsForBindings(bindings []domainaigateway.SkillBinding, skillID string) []string {
	refsBySkill := skillBindingRefs(bindings, skillID)
	out := make([]string, 0)
	for _, refs := range refsBySkill {
		out = append(out, refs...)
	}
	return normalizeStringSlice(out)
}

func skillBindingRefs(bindings []domainaigateway.SkillBinding, skillID string) map[string][]string {
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
			if skill, ok := skillByID(bindingSkillID); ok {
				refs = normalizeStringSlice(skill.CapabilityRefs)
			}
		}
		out[bindingSkillID] = normalizeStringSlice(append(out[bindingSkillID], refs...))
	}
	return out
}

func toolInAnySkill(skillIDs []string, toolName string) bool {
	for _, skillID := range skillIDs {
		skillID = strings.TrimSpace(skillID)
		if skillID == "" {
			continue
		}
		if skillID == "*" {
			return true
		}
		skill, ok := skillByID(skillID)
		if !ok {
			continue
		}
		if matchesToolPatternList(skill.CapabilityRefs, toolName) {
			return true
		}
	}
	return false
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

func (s *Service) recordToolAudit(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest, tool domainaigateway.ToolCapability, result, summary string, relatedIDs map[string]any) error {
	if s == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	_ = s.recordGatewayToolAuditLog(ctx, principal, input, tool, result, summary, relatedIDs, meta)
	if s.audit == nil {
		return nil
	}
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
		Metadata: map[string]any{
			"aiClientId":       strings.TrimSpace(input.AIClientID),
			"aiClientName":     strings.TrimSpace(input.AIClientName),
			"skillId":          strings.TrimSpace(input.SkillID),
			"toolName":         tool.Name,
			"mcpAdapterId":     tool.MCPAdapterID,
			"mcpToolName":      tool.MCPToolName,
			"riskLevel":        tool.RiskLevel,
			"requiresApproval": tool.RequiresApproval,
			"relatedIds":       relatedIDs,
		},
	})
}

func (s *Service) recordGatewayToolAuditLog(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest, tool domainaigateway.ToolCapability, result, summary string, relatedIDs map[string]any, meta requestctx.Metadata) error {
	if s == nil || s.repo == nil {
		return nil
	}
	actorType, actorID := gatewaySubject(principal)
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
		Metadata: map[string]any{
			"mcpAdapterId":     tool.MCPAdapterID,
			"mcpToolName":      tool.MCPToolName,
			"requiresApproval": tool.RequiresApproval,
			"relatedIds":       relatedIDs,
		},
		CreatedAt: time.Now().UTC(),
	}
	return s.repo.CreateAuditLog(ctx, item)
}

func gatewayAuditScope(input map[string]any, relatedIDs map[string]any) map[string]any {
	scope := map[string]any{}
	for _, key := range []string{
		"applicationId",
		"applicationEnvironmentId",
		"businessLineId",
		"environmentId",
		"releaseBundleId",
		"executionTaskId",
		"clusterId",
		"namespace",
		"podName",
		"deploymentName",
		"serviceName",
	} {
		if value := stringInput(input, key); value != "" {
			scope[key] = value
			continue
		}
		if value, ok := relatedIDs[key]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			scope[key] = value
		}
	}
	return scope
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
