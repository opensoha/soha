package execution

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func normalizeHelmCallback(task domaindelivery.ExecutionTask, input domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionCallbackInput, error) {
	if !strings.HasPrefix(task.TaskKind, "helm_") {
		return input, nil
	}
	var result sohaapi.HelmExecutionTaskResult
	encoded, err := json.Marshal(input.Payload["helm"])
	if err == nil {
		err = json.Unmarshal(encoded, &result)
	}
	if err != nil || isStrictTerminalTaskStatus(input.Status) && !result.Stopped {
		return input, fmt.Errorf("%w: Helm terminal callbacks require confirmed executor stop", apperrors.ErrInvalidArgument)
	}
	if input.Status == "completed" {
		var payload sohaapi.HelmExecutionTaskPayload
		encoded, err := json.Marshal(task.Payload["helm"])
		if err == nil {
			err = json.Unmarshal(encoded, &payload)
		}
		if err != nil || task.TaskKind != "helm_"+string(payload.Action) || helmrelease.ValidateCompletedResult(payload, result) != nil {
			return input, fmt.Errorf("%w: Helm completion does not match the frozen plan", apperrors.ErrConflict)
		}
	}
	// Only the typed, non-secret observation crosses into public task history.
	result.Diagnostics = nil
	input.Payload = map[string]any{"helm": result}
	if input.Status == "failed" || input.Status == "callback_timeout" || input.Status == "canceled" {
		input.Payload["error"] = "Helm execution ended; inspect the recorded revision and resource state"
	}
	return input, nil
}
