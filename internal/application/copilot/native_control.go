package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validateNativeTaskIntent(intent *domaincopilot.TaskIntent) error {
	if intent == nil {
		return nil
	}
	if len(intent.Capabilities) > 4 || len(intent.InspectionRunIDs) > 20 {
		return fmt.Errorf("%w: too many task selections", apperrors.ErrInvalidArgument)
	}
	seen := map[string]bool{}
	for _, kind := range intent.Capabilities {
		key := string(kind)
		if !isRunnableSessionAnalysisMode(key) || seen[key] {
			return fmt.Errorf("%w: invalid task capability", apperrors.ErrInvalidArgument)
		}
		seen[key] = true
	}
	for _, value := range []string{intent.DataSourceID, intent.PerformanceDataSourceID, intent.TraceDataSourceID, intent.RootCauseProfileID, intent.MetricKey, intent.TraceID} {
		if utf8.RuneCountInString(value) > 256 {
			return fmt.Errorf("%w: task selection exceeds limit", apperrors.ErrInvalidArgument)
		}
	}
	if intent.MinDurationMs != nil && (*intent.MinDurationMs < 0 || *intent.MinDurationMs > 86400000) {
		return fmt.Errorf("%w: invalid trace duration", apperrors.ErrInvalidArgument)
	}
	for index, id := range intent.InspectionRunIDs {
		if strings.TrimSpace(id) == "" || utf8.RuneCountInString(id) > 128 || slices.Contains(intent.InspectionRunIDs[:index], id) {
			return fmt.Errorf("%w: invalid inspection selection", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func professionalChatArguments(run domaincopilot.AgentRun, toolName string, input map[string]any) (map[string]any, error) {
	if sessionID := stringValue(input["sessionId"]); sessionID != "" && sessionID != run.SessionID {
		return nil, fmt.Errorf("%w: professional call exceeds run session", apperrors.ErrAccessDenied)
	}
	arguments := maps.Clone(input)
	arguments["sessionId"] = run.SessionID
	arguments["toolset"] = run.Toolset
	arguments["locale"] = normalizeLocale(stringValue(run.Input["locale"]))
	raw, err := json.Marshal(run.Scope)
	if err != nil {
		return nil, err
	}
	var pinned map[string]any
	if err = json.Unmarshal(raw, &pinned); err != nil {
		return nil, err
	}
	requested := maps.Clone(mapValue(input["scope"]))
	if requested == nil {
		requested = map[string]any{}
	}
	for _, key := range []string{"clusterId", "namespace", "workload", "service", "pod", "node", "alertId"} {
		value := stringValue(pinned[key])
		if value == "" {
			continue
		}
		if wanted := stringValue(requested[key]); wanted != "" && wanted != value {
			return nil, fmt.Errorf("%w: professional query exceeds run scope", apperrors.ErrAccessDenied)
		}
		requested[key] = value
	}
	if run.Scope.TimeRangeMinutes > 0 {
		if intCondition(requested["timeRangeMinutes"]) > run.Scope.TimeRangeMinutes {
			return nil, fmt.Errorf("%w: query window exceeds run scope", apperrors.ErrAccessDenied)
		}
		if intCondition(requested["timeRangeMinutes"]) <= 0 {
			requested["timeRangeMinutes"] = run.Scope.TimeRangeMinutes
		}
	}
	arguments["scope"] = requested
	intent, err := nativeProfessionalIntent(run, toolName, input)
	if err != nil {
		return nil, err
	}
	arguments["intent"] = intent
	return arguments, nil
}

func nativeProfessionalBindings() []domaincopilot.AgentToolBinding {
	bindings := []domaincopilot.AgentToolBinding{}
	for _, kind := range []string{"root_cause", "performance", "trace", "inspection_review"} {
		permission := appaccess.PermObserveMonitoringView
		if kind == "root_cause" {
			permission = appaccess.PermObserveAIRootCauseRun
		}
		if kind == "inspection_review" {
			permission = appaccess.PermObserveAIView
		}
		name := "copilot." + kind + ".analyze"
		bindings = append(bindings, domaincopilot.AgentToolBinding{ID: name, ToolName: name, ToolKind: "mcp", AdapterID: "professional.v1", PermissionKey: permission})
	}
	return bindings
}
func nativeStreamStatus(status string) string {
	switch status {
	case "completed":
		return "succeeded"
	case "canceled":
		return "cancelled"
	default:
		return status
	}
}
func nativeUserInputStep(ctx context.Context, store NativeRunStore, lease domaincopilot.NativeLease, cp *nativeCheckpoint) (domaincopilot.NativeLease, error) {
	var input struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(cp.Pending[0].Call.Arguments, &input); err != nil || strings.TrimSpace(input.Question) == "" || len([]rune(input.Question)) > 2048 {
		return lease, fmt.Errorf("%w: invalid input question", apperrors.ErrInvalidArgument)
	}
	if cp.UserInput == "" {
		cp.StopReason = "waiting_input"
		lease.Run.Output["pendingQuestion"] = input.Question
		return saveNativeCheckpoint(ctx, store, lease, *cp, "paused", []domaincopilot.WorkbenchStreamEvent{{Type: "message.delta", Role: "assistant", MessageID: lease.Run.ID + ":reply", ContentDelta: input.Question}, {Type: "agent.status", ProviderID: lease.Run.ProviderID, ProviderKind: "native", Status: "running"}})
	}
	for index, pending := range cp.Pending {
		content := "This call was not executed; user input changed. Re-evaluate its arguments before requesting another call."
		if index == 0 {
			content = cp.UserInput
		}
		cp.Messages = append(cp.Messages, appaigateway.WorkbenchRelayMessage{Role: "tool", ToolCallID: pending.Call.ID, Content: content})
	}
	cp.Pending = nil
	cp.UserInput = ""
	cp.StopReason = ""
	delete(lease.Run.Output, "pendingQuestion")
	return saveNativeCheckpoint(ctx, store, lease, *cp, "preparing", nil)
}

func nativeProfessionalIntent(run domaincopilot.AgentRun, toolName string, input map[string]any) (map[string]any, error) {
	intent := maps.Clone(mapValue(input["intent"]))
	if intent == nil {
		intent = map[string]any{}
	}
	raw, err := json.Marshal(run.Input["taskIntent"])
	if err != nil {
		return nil, err
	}
	var selected map[string]any
	if err = json.Unmarshal(raw, &selected); err != nil {
		return nil, err
	}
	keys := []string{}
	capabilitySourceKey := ""
	switch toolName {
	case "copilot.performance.analyze":
		keys = []string{"metricKey"}
		capabilitySourceKey = "performanceDataSourceId"
	case "copilot.trace.analyze":
		keys = []string{"traceId"}
		capabilitySourceKey = "traceDataSourceId"
	case "copilot.root_cause.analyze":
		keys = []string{"rootCauseProfileId"}
	}
	sourceKey := "dataSourceId"
	if stringValue(selected[capabilitySourceKey]) != "" {
		sourceKey = capabilitySourceKey
	}
	if value := stringValue(selected[sourceKey]); value != "" {
		for _, key := range []string{"dataSourceId", capabilitySourceKey} {
			if key == "" {
				continue
			}
			if wanted := stringValue(intent[key]); wanted != "" && wanted != value {
				return nil, fmt.Errorf("%w: professional query changes selected data source", apperrors.ErrAccessDenied)
			}
			intent[key] = value
		}
	}
	if err := pinProfessionalStrings(intent, selected, keys); err != nil {
		return nil, err
	}
	pinnedKeys := []string{}
	switch toolName {
	case "copilot.trace.analyze":
		pinnedKeys = []string{"minDurationMs"}
	case "copilot.inspection_review.analyze":
		pinnedKeys = []string{"inspectionRunIds"}
	}
	for _, key := range pinnedKeys {
		if value, ok := selected[key]; ok {
			intent[key] = value
		}
	}
	return intent, nil
}

func pinProfessionalStrings(intent, selected map[string]any, keys []string) error {
	for _, key := range keys {
		if value := stringValue(selected[key]); value != "" {
			if wanted := stringValue(intent[key]); wanted != "" && wanted != value {
				return fmt.Errorf("%w: professional query changes selected input", apperrors.ErrAccessDenied)
			}
			intent[key] = value
		}
	}
	return nil
}
