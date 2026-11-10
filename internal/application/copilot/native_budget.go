package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"

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
	messages := slices.Clone(request.Messages)
	var images, imageBytes int64
	for i := range messages {
		for _, image := range messages[i].Images {
			if len(image) == 0 || len(image) > maxChatImageDataURLBytes {
				return fmt.Errorf("%w: Native image exceeds request size limit", apperrors.ErrInvalidArgument)
			}
			images++
			imageBytes += int64(len(image))
		}
		messages[i].Images = nil
	}
	if imageBytes > 8*maxChatImageDataURLBytes {
		return fmt.Errorf("%w: Native image egress exceeds request size limit", apperrors.ErrInvalidArgument)
	}
	data, err := json.Marshal(struct {
		Messages []appaigateway.WorkbenchRelayMessage
		Tools    []appaigateway.WorkbenchTool
	}{messages, request.Tools})
	if err != nil {
		return err
	}
	// shortcut: use a fixed reservation for each validated <=1024px image,
	// until route-specific vision token estimates are available.
	const imageTokenReserve = 4096
	input := int64(len(data)) + int64(len(messages))*64 + images*imageTokenReserve
	remaining := cp.Budget.MaxTokens - max(cp.BudgetTokens, cp.Usage.Tokens) - input
	if remaining <= 0 {
		return fmt.Errorf("%w: Native input leaves no model output budget", apperrors.ErrInvalidArgument)
	}
	outputLimit := int64(4096)
	if request.MaxOutputTokens > 0 {
		outputLimit = min(outputLimit, int64(request.MaxOutputTokens))
	}
	request.MaxOutputTokens = int(min(outputLimit, remaining))
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
