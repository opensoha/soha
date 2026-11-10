package aigateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

const protectedResourceUpdateTool = domainai.ProtectedResourceUpdateTool
const protectedPodExecTool = domainai.ProtectedPodExecTool

// Public approval records contain a digest of the YAML, never the YAML itself.
// The exact replay input uses the existing credential keyring and is bound to
// the immutable approval identity inside the authenticated encrypted envelope.
type protectedApprovalInput struct {
	ApprovalID             string         `json:"approvalId"`
	ToolName               string         `json:"toolName"`
	ActorType              string         `json:"actorType"`
	ActorID                string         `json:"actorId"`
	AIClientID             string         `json:"aiClientId"`
	SkillID                string         `json:"skillId"`
	Input                  map[string]any `json:"input"`
	ActorTokenID           string         `json:"actorTokenId,omitempty"`
	ActorPermissionCeiling []string       `json:"actorPermissionCeiling,omitempty"`
}

func hasProtectedApprovalInput(tool string) bool {
	return domainai.RequiresProtectedToolInput(tool)
}

func (s *Service) authorizeProtectedWorkbenchInput(ctx context.Context, tool string) error {
	native, _ := ctx.Value(nativeWorkbenchInvocationKey{}).(bool)
	if native && hasProtectedApprovalInput(tool) && (!s.protectedDurableStorage || nativeUnownedApproval(ctx)) {
		return fmt.Errorf("%w: protected input requires encrypted durable storage and an owned governed capability task", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func approvalDisplayInput(tool string, input map[string]any) map[string]any {
	if tool == protectedPodExecTool {
		return podExecutionApprovalDisplay(input)
	}
	if tool != protectedResourceUpdateTool {
		return sanitizeGatewayMap(input)
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		if key != "content" {
			out[key] = value
		}
	}
	content, _ := input["content"].(string)
	out["content"] = fmt.Sprintf("[PROTECTED sha256:%x]", sha256.Sum256([]byte(content)))
	return sanitizeGatewayMap(out)
}

func (s *Service) protectApprovalInput(request *domainai.ApprovalRequest, input map[string]any) error {
	if !hasProtectedApprovalInput(request.ToolName) {
		return nil
	}
	if request.ToolName == protectedPodExecTool && request.ActorTokenID == "" {
		return fmt.Errorf("%w: credential-bound Pod execution is required", apperrors.ErrUnsupportedOperation)
	}
	envelope := protectedApprovalInput{ApprovalID: request.ID, ToolName: request.ToolName, ActorType: request.ActorType, ActorID: request.ActorID, AIClientID: request.AIClientID, SkillID: request.SkillID, Input: input}
	if request.ToolName == protectedPodExecTool {
		envelope.ActorTokenID = request.ActorTokenID
		envelope.ActorPermissionCeiling = request.ActorPermissionCeiling
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return apperrors.ErrInvalidArgument
	}
	ciphertext, err := s.relayCredentialCodec().encrypt(string(raw))
	if err != nil || !secretcrypto.Encrypted(ciphertext) {
		return fmt.Errorf("%w: protected approval storage requires a persistent encryption key", apperrors.ErrServiceUnavailable)
	}
	request.ProtectedToolInput = ciphertext
	request.ToolInput = approvalDisplayInput(request.ToolName, input)
	return nil
}

func (s *Service) approvalExecutionInput(request domainai.ApprovalRequest) (map[string]any, error) {
	if !hasProtectedApprovalInput(request.ToolName) {
		if request.ProtectedToolInput != "" {
			return nil, apperrors.ErrAccessDenied
		}
		return request.ToolInput, nil
	}
	if !secretcrypto.Encrypted(request.ProtectedToolInput) {
		return nil, fmt.Errorf("%w: protected approval payload is unavailable", apperrors.ErrAccessDenied)
	}
	raw, err := s.relayCredentialCodec().decrypt(request.ProtectedToolInput)
	var envelope protectedApprovalInput
	if err != nil || json.Unmarshal([]byte(raw), &envelope) != nil || envelope.ApprovalID != request.ID || envelope.ToolName != request.ToolName || envelope.ActorType != request.ActorType || envelope.ActorID != request.ActorID || envelope.AIClientID != request.AIClientID || envelope.SkillID != request.SkillID || !executionJSONEqual(request.ToolInput, approvalDisplayInput(request.ToolName, envelope.Input)) {
		return nil, fmt.Errorf("%w: protected approval identity or input changed", apperrors.ErrAccessDenied)
	}
	if request.ToolName == protectedPodExecTool && (request.ActorTokenID == "" || envelope.ActorTokenID != request.ActorTokenID || !slices.Equal(envelope.ActorPermissionCeiling, request.ActorPermissionCeiling)) {
		return nil, fmt.Errorf("%w: protected execution credential or ceiling changed", apperrors.ErrAccessDenied)
	}
	return envelope.Input, nil
}

func approvalInputMatchesCall(request domainai.ApprovalRequest, input map[string]any) bool {
	if hasProtectedApprovalInput(request.ToolName) {
		return secretcrypto.Encrypted(request.ProtectedToolInput) && executionJSONEqual(request.ToolInput, approvalDisplayInput(request.ToolName, input))
	}
	// The approval repository and sealed tasks restore JSON numbers using
	// different Go types. Compare the exact serialized input, not those types.
	return executionJSONEqual(request.ToolInput, input)
}
