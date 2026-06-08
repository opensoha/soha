package aigateway

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

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
