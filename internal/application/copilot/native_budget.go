package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func reserveNativeWorkCall(ctx context.Context, store NativeRunStore, lease domaincopilot.NativeLease, cp *nativeCheckpoint) error {
	usage, err := store.ReserveNativeToolCall(ctx, lease, cp.Budget.MaxToolCalls)
	if err != nil {
		return err
	}
	cp.Usage.ToolCalls, cp.CleanupToolCalls = usage.WorkCalls, usage.CleanupCalls
	cp.Budget.MaxToolCalls = min(cp.Budget.MaxToolCalls, usage.WorkLimit)
	return nil
}

func reserveNativeModelBudget(cp *nativeCheckpoint, request *appaigateway.WorkbenchRelayRequest) error {
	data, err := json.Marshal(struct {
		Messages []appaigateway.WorkbenchRelayMessage
		Tools    []appaigateway.WorkbenchTool
	}{request.Messages, request.Tools})
	if err != nil {
		return err
	}
	// ponytail: reserve serialized bytes plus per-message overhead, not a model
	// tokenizer estimate. Add a route tokenizer when measured usage needs precision.
	input := int64(len(data)) + int64(len(request.Messages))*64
	remaining := cp.Budget.MaxTokens - max(cp.BudgetTokens, cp.Usage.Tokens) - input
	if remaining <= 0 {
		return fmt.Errorf("%w: Native input leaves no model output budget", apperrors.ErrInvalidArgument)
	}
	request.MaxOutputTokens = int(min(int64(4096), remaining))
	cp.ModelReservation = input + int64(request.MaxOutputTokens)
	cp.BudgetTokens = max(cp.BudgetTokens, cp.Usage.Tokens) + cp.ModelReservation
	return nil
}

func settleNativeModelBudget(cp *nativeCheckpoint, response appaigateway.WorkbenchRelayResponse) error {
	tokens, known := response.Usage["totalTokens"]
	if !known || tokens == 0 {
		cp.UsageKnown = false
		cp.Usage.Tokens += cp.ModelReservation
		cp.ModelReservation = 0
		return nil
	}
	if math.IsNaN(tokens) || math.IsInf(tokens, 0) || tokens < 0 || tokens >= float64(math.MaxInt64) {
		cp.UsageKnown = false
		return fmt.Errorf("%w: invalid model usage measurement", apperrors.ErrInvalidArgument)
	}
	consumed := int64(math.Ceil(tokens))
	charge := consumed
	if response.UsageEstimated {
		cp.UsageKnown = false
		charge = max(charge, cp.ModelReservation)
	}
	previous := cp.BudgetTokens - cp.ModelReservation
	if previous < 0 || cp.Usage.Tokens < 0 || charge > math.MaxInt64-previous || consumed > math.MaxInt64-cp.Usage.Tokens {
		cp.UsageKnown = false
		return fmt.Errorf("%w: model usage exceeds the accounting range", apperrors.ErrInvalidArgument)
	}
	cp.BudgetTokens = previous + charge
	cp.Usage.Tokens += consumed
	cp.ModelReservation = 0
	return nil
}

func nativeUsageProjection(cp nativeCheckpoint) map[string]any {
	return map[string]any{"taskTokenLimit": cp.Budget.MaxTokens, "totalTokens": cp.Usage.Tokens, "budgetTokens": cp.BudgetTokens, "estimatedTokens": !cp.UsageKnown, "steps": cp.Usage.Steps, "toolCalls": cp.Usage.ToolCalls, "cleanupToolCalls": cp.CleanupToolCalls, "totalToolCalls": cp.Usage.ToolCalls + cp.CleanupToolCalls, "workToolCallLimit": cp.Budget.MaxToolCalls, "cleanupToolCallLimit": domaincopilot.NativeCleanupToolCallLimit, "toolCallAccounting": "reserved_attempts", "costKnown": false}
}
