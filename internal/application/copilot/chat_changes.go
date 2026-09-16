package copilot

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) requestChatChange(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	gateway, ok := s.workbenchInvoker.(chatChangeRequester)
	if !ok {
		return nil, fmt.Errorf("%w: approval requests are unavailable", apperrors.ErrUnsupportedOperation)
	}
	tool := stringValue(input["toolName"])
	if tool != "delivery.applications.create" && tool != "delivery.actions.trigger" {
		return nil, fmt.Errorf("%w: this change is not available in chat", apperrors.ErrAccessDenied)
	}
	principal, err := agentToolPrincipal(run)
	if err != nil {
		return nil, err
	}
	arguments, err := chatChangeArguments(tool, mapValue(input["arguments"]))
	if err != nil {
		return nil, err
	}
	for _, execution := range run.ToolExecutions {
		if execution.ToolName != "change.request" || execution.Status != "success" {
			continue
		}
		previous := mapValue(execution.Input["input"])
		if stringValue(previous["toolName"]) == tool && reflect.DeepEqual(mapValue(previous["arguments"]), arguments) {
			return maps.Clone(execution.Output), nil
		}
		return nil, fmt.Errorf("%w: this turn already submitted a change; review or cancel its saved request before proposing another", apperrors.ErrConflict)
	}
	result, err := gateway.RequestToolApproval(ctx, principal, domainaigateway.ToolInvocationRequest{ToolName: tool, SessionID: run.SessionID, Input: arguments})
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": result.Result, "relatedIds": result.RelatedIDs, "approval": result.Output, "message": "No change was executed. The user must review the saved request and approve it. Do not submit it again or claim execution; approval executes the saved arguments through the owning service."}, nil
}

func chatChangeArguments(tool string, arguments map[string]any) (map[string]any, error) {
	allowed := []string{"name", "key", "description", "enabled"}
	required := []string{"name", "key"}
	if tool == "delivery.actions.trigger" {
		allowed = []string{"applicationId", "applicationEnvironmentId", "action"}
		required = allowed
	}
	for key, value := range arguments {
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("%w: unexpected change field", apperrors.ErrInvalidArgument)
		}
		if key == "enabled" {
			if _, ok := value.(bool); !ok {
				return nil, fmt.Errorf("%w: enabled must be boolean", apperrors.ErrInvalidArgument)
			}
			continue
		}
		text, ok := value.(string)
		limit := 256
		if key == "description" {
			limit = 2048
		}
		if key == "key" {
			limit = 128
		}
		if !ok || utf8.RuneCountInString(text) > limit {
			return nil, fmt.Errorf("%w: invalid change field", apperrors.ErrInvalidArgument)
		}
	}
	for _, key := range required {
		if strings.TrimSpace(stringValue(arguments[key])) == "" {
			return nil, fmt.Errorf("%w: %s is required", apperrors.ErrInvalidArgument, key)
		}
	}
	if tool == "delivery.actions.trigger" && !slices.Contains([]string{"build", "deploy", "rollback"}, stringValue(arguments["action"])) {
		return nil, fmt.Errorf("%w: unsupported delivery action", apperrors.ErrInvalidArgument)
	}
	return arguments, nil
}
