package aigateway

import (
	"fmt"
	"slices"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func workbenchStreamCompletion(endpoint string, payload map[string]any) (bool, error) {
	kind := workbenchString(payload["type"])
	if kind == "error" || payload["error"] != nil {
		return false, fmt.Errorf("%w: model stream reported an error", apperrors.ErrClusterUnready)
	}
	var reason string
	var accepted []string
	switch endpoint {
	case "chat/completions":
		choices, _ := payload["choices"].([]any)
		if len(choices) > 0 {
			reason = workbenchString(workbenchMap(choices[0])["finish_reason"])
		}
		accepted = []string{"stop", "tool_calls", "function_call"}
	case "responses":
		switch kind {
		case "response.completed":
			return true, nil
		case "response.incomplete", "response.failed":
			return false, fmt.Errorf("%w: model response did not complete", apperrors.ErrClusterUnready)
		}
	case "messages":
		if kind == "message_stop" {
			return true, nil
		}
		reason = workbenchString(workbenchMap(payload["delta"])["stop_reason"])
		accepted = []string{"end_turn", "stop_sequence", "tool_use"}
	}
	if reason == "" {
		return false, nil
	}
	if !slices.Contains(accepted, reason) {
		return false, fmt.Errorf("%w: model stream stopped without complete output", apperrors.ErrClusterUnready)
	}
	return true, nil
}
