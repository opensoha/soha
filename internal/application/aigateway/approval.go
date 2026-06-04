package aigateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/requestctx"
)

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
