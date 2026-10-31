package copilot

import (
	"encoding/json"
	"maps"
	"slices"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

func nativePublicExecution(execution domaincopilot.ToolExecution) domaincopilot.ToolExecution {
	visible, err := appaigateway.ProtectedWorkbenchExecution(execution)
	if err != nil {
		visible.Input, visible.Output = nil, nil
		visible.Summary = "Protected observation unavailable"
	}
	return visible
}

func nativePublicEvent(event domaincopilot.WorkbenchStreamEvent) domaincopilot.WorkbenchStreamEvent {
	if event.ToolCall == nil || !appaigateway.ProtectedWorkbenchTool(event.ToolCall.ToolName) {
		return event
	}
	call := *event.ToolCall
	input := mapValue(call.InputPreview)
	if input == nil && call.InputPreview != nil {
		input = map[string]any{"input": call.InputPreview}
	}
	output := mapValue(call.OutputPreview)
	execution := nativePublicExecution(domaincopilot.ToolExecution{ToolName: call.ToolName, Input: input, Output: output, Summary: call.Summary})
	call.InputPreview, call.OutputPreview, call.Summary = execution.Input, execution.Output, execution.Summary
	event.ToolCall = &call
	event.ContentDelta, event.Content, event.TextDelta, event.OutputDelta, event.LogDelta = "", "", "", "", ""
	event.Summary, event.Message, event.Metadata = "", "", nil
	return event
}

func nativePublicRun(run domaincopilot.AgentRun) domaincopilot.AgentRun {
	if run.ProviderKind != "native" {
		return run
	}
	run.ToolExecutions = slices.Clone(run.ToolExecutions)
	for i, execution := range run.ToolExecutions {
		run.ToolExecutions[i] = nativePublicExecution(execution)
	}
	run.Output = maps.Clone(run.Output)
	if value, exists := run.Output["workbenchEvents"]; exists {
		raw, err := json.Marshal(value)
		var events []domaincopilot.WorkbenchStreamEvent
		if err != nil || json.Unmarshal(raw, &events) != nil {
			delete(run.Output, "workbenchEvents")
		} else {
			for i, event := range events {
				events[i] = nativePublicEvent(event)
			}
			run.Output["workbenchEvents"] = events
		}
	}
	return run
}
