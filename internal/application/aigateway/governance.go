package aigateway

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

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
