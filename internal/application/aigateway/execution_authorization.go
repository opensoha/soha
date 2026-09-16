package aigateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"time"

	domain "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Rates govern acceptance of new calls. A domain queue rechecks current policy
// and budgets without incrementing rate counters or creating another approval.
type queuedAuthorizationKey struct{}

func withGatewayExecutionAuthorization(ctx context.Context, principal domainidentity.Principal, tool domain.ToolCapability, call domain.ToolInvocationRequest, approvalID string, decision gatewayRiskDecision) context.Context {
	call.CapabilityVersion = tool.Version
	return domain.WithExecutionAuthorization(ctx, domain.ExecutionAuthorization{ActorID: principal.UserID, Call: call, ResourceScope: resolvedCapabilityAuditScope(ctx, tool, call.Input, nil), ApprovalID: approvalID, ApprovalPolicy: executionApprovalPolicy(decision)})
}

func executionApprovalPolicy(decision gatewayRiskDecision) string {
	encoded, _ := json.Marshal([]any{decision.PolicyID, decision.ApprovalPolicyRef, decision.ApprovalPolicy})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

// CheckExecutionAuthorization never invokes a provider. The caller supplies a
// freshly resolved principal and its own immutable, decrypted queue record.
func (s *Service) CheckExecutionAuthorization(ctx context.Context, principal domainidentity.Principal, authorization domain.ExecutionAuthorization) error {
	if authorization.ActorID == "" || authorization.ActorID != principal.UserID {
		return fmt.Errorf("%w: queued gateway actor changed", apperrors.ErrAccessDenied)
	}
	call := authorization.Call
	ctx = context.WithValue(ctx, queuedAuthorizationKey{}, true)
	ctx, err := s.authorizeCapabilityCall(ctx, principal, call)
	if err != nil {
		return err
	}
	tool, _ := s.toolByName(call.ToolName)
	if tool.Version != call.CapabilityVersion {
		return fmt.Errorf("%w: queued capability version changed", apperrors.ErrConflict)
	}
	if !matchesApprovedCapabilityScopes(ctx, tool.Name, authorization.ResourceScope) {
		return fmt.Errorf("%w: queued gateway resource scope changed", apperrors.ErrAccessDenied)
	}
	if err := validateCapabilityInput(tool, call.Input); err != nil {
		return err
	}
	scope := capabilityGatewayScope(tool, call.Input)
	requiresApproval, err := s.authorizeToolGrant(ctx, principal, call.AIClientID, tool, scope)
	if err != nil {
		return err
	}
	decision, policyInput, _, err := s.authorizeAccessPolicy(ctx, principal, call.AIClientID, call.SkillID, &tool, scope, call.Input)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(policyInput, call.Input) || decision.Strategy == gatewayRiskDeny || decision.Strategy == gatewayRiskDryRunOnly {
		return fmt.Errorf("%w: current policy blocks or changes queued gateway input", apperrors.ErrAccessDenied)
	}
	if requiresApproval || decision.requiresApproval() || authorization.ApprovalID != "" {
		if err := s.checkExecutionApproval(ctx, principal, tool, authorization, decision); err != nil {
			return err
		}
	}
	originalRefs := call.SecretRefs
	if err := s.pinToolSecretRefs(ctx, principal, tool, &call); err != nil {
		return err
	}
	if !maps.Equal(originalRefs, call.SecretRefs) {
		return fmt.Errorf("%w: queued secret reference version changed", apperrors.ErrAccessDenied)
	}
	return nil
}

func (s *Service) checkExecutionApproval(ctx context.Context, principal domainidentity.Principal, tool domain.ToolCapability, authorization domain.ExecutionAuthorization, decision gatewayRiskDecision) error {
	repo := s.approvalRepository()
	if authorization.ApprovalID == "" || repo == nil || authorization.ApprovalPolicy != executionApprovalPolicy(decision) {
		return fmt.Errorf("%w: queued call requires current approval", apperrors.ErrAccessDenied)
	}
	request, err := repo.GetApprovalRequest(ctx, authorization.ApprovalID)
	if err != nil {
		return err
	}
	call := authorization.Call
	actorType, actorID := gatewaySubject(principal)
	if request.ActorType != actorType || request.ActorID != actorID || request.ToolName != tool.Name || request.AIClientID != call.AIClientID || request.SkillID != call.SkillID || request.Status != "approved" && request.Status != "executed" || request.ExpiresAt != nil && !request.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("%w: queued approval is no longer valid", apperrors.ErrAccessDenied)
	}
	if validateApprovedCapability(tool, request) != nil || !matchesApprovedCapabilityScopes(ctx, tool.Name, request.ResourceScope) || !executionJSONEqual(gatewayApprovalReplayInput(request), call.Input) || !maps.Equal(request.SecretRefs, call.SecretRefs) {
		return fmt.Errorf("%w: queued call differs from approved request", apperrors.ErrAccessDenied)
	}
	return nil
}

func executionJSONEqual(left, right map[string]any) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	a, err := json.Marshal(left)
	b, otherErr := json.Marshal(right)
	return err == nil && otherErr == nil && string(a) == string(b)
}
