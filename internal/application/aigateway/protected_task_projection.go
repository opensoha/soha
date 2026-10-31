package aigateway

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func approvedCapabilityOutput(ctx context.Context, tool string, output any) (any, error) {
	execution, bound := capabilityExecutionFrom(ctx)
	if bound && execution.Scope == domainworkflow.ScopeCapabilityTask {
		return protectedCapabilityOutput(tool, output)
	}
	return output, nil
}

// Protected input is restored only inside the encrypted execution boundary.
// Task and Native projections retain the original receipt, never process text.
func protectedCapabilityOutput(tool string, output any) (any, error) {
	if output == nil {
		return nil, nil
	}
	if tool == protectedResourceUpdateTool {
		raw, err := json.Marshal(output)
		var result struct {
			MutationRequestID string                                         `json:"mutationRequestId,omitempty"`
			MutationReceiptID string                                         `json:"mutationReceiptId,omitempty"`
			ClusterID         string                                         `json:"clusterId,omitempty"`
			Action            string                                         `json:"action,omitempty"`
			UID               string                                         `json:"uid,omitempty"`
			ResourceVersion   string                                         `json:"resourceVersion,omitempty"`
			Applied           bool                                           `json:"applied"`
			Kind              string                                         `json:"kind"`
			Name              string                                         `json:"name"`
			Namespace         string                                         `json:"namespace"`
			Preconditions     contractresource.ResourceMutationPreconditions `json:"preconditions"`
		}
		if err != nil || json.Unmarshal(raw, &result) != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		return result, nil
	}
	if tool != protectedPodExecTool && tool != "k8s.pods.exec.receipt" {
		return output, nil
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	var result contractresource.PodGatewayResult
	if json.Unmarshal(raw, &result) != nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	result.Output = nil
	result.OutputState = "unavailable"
	result.Receipt.OutputAvailable = false
	return result, nil
}

func ProtectedWorkbenchTool(tool string) bool {
	return hasProtectedApprovalInput(tool) || tool == "k8s.pods.exec.receipt"
}

// This is a public observation copy. The exact arguments and model transcript
// remain in the sealed checkpoint; recovery must never read these previews.
func ProtectedWorkbenchExecution(execution domaincopilot.ToolExecution) (domaincopilot.ToolExecution, error) {
	if !ProtectedWorkbenchTool(execution.ToolName) {
		return execution, nil
	}
	if hasProtectedApprovalInput(execution.ToolName) {
		if !protectedWorkbenchInputProjected(execution.Input) {
			execution.Input = map[string]any{"protectedInput": true, "inputDigest": protectedPodInputDigest(execution.Input)}
		}
		execution.Summary = "Protected capability; inspect the original authorized receipt"
	}
	output, err := protectedCapabilityOutput(execution.ToolName, execution.Output)
	if err != nil {
		return execution, err
	}
	if output != nil {
		value, err := capabilityJSON(output)
		if err != nil {
			return execution, apperrors.ErrServiceUnavailable
		}
		execution.Output, _ = value.(map[string]any)
	}
	return execution, nil
}

func protectedWorkbenchInputProjected(input map[string]any) bool {
	if len(input) != 2 || input["protectedInput"] != true {
		return false
	}
	digest, ok := input["inputDigest"].(string)
	if !ok || !strings.HasPrefix(digest, "[PROTECTED sha256:") || !strings.HasSuffix(digest, "]") {
		return false
	}
	raw, err := hex.DecodeString(strings.TrimSuffix(strings.TrimPrefix(digest, "[PROTECTED sha256:"), "]"))
	return err == nil && len(raw) == 32
}
