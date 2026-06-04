package aigateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainresource "github.com/soha/soha/internal/domain/resource"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/operationentry"
	"github.com/soha/soha/internal/platform/requestctx"
)

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
