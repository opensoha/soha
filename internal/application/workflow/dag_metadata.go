package workflow

import (
	"fmt"
	"strings"
	"time"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

var gatewayApprovalWorkflowMetadataKeys = []string{
	"aiGatewayApprovalRequestId",
	"aiGatewayApprovalPolicyRef",
	"aiGatewayPolicyId",
	"aiGatewayToolName",
	"aiGatewaySkillId",
	"aiGatewayAIClientId",
}

func workflowMetadataMode(metadata map[string]any) string {
	mode := strings.TrimSpace(fmt.Sprint(metadata["mode"]))
	if mode == "delivery_dag" {
		return mode
	}
	return "release_dag"
}

func mapString(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func ensureDAGExecutionMetadata(run *domainworkflow.Run) {
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}
	if _, ok := run.Metadata["events"]; !ok {
		run.Metadata["events"] = []map[string]any{}
	}
	if _, ok := run.Metadata["nodeOutputs"]; !ok {
		run.Metadata["nodeOutputs"] = map[string]any{}
	}
}

func appendDAGRunEvent(run *domainworkflow.Run, event map[string]any) {
	if event == nil {
		return
	}
	ensureDAGExecutionMetadata(run)
	if _, ok := event["occurredAt"]; !ok {
		event["occurredAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	items := metadataMapSlice(run.Metadata["events"])
	items = append(items, event)
	run.Metadata["events"] = items
}

func appendDAGNodeEvent(run *domainworkflow.Run, nodeID, eventType, status, summary string, details map[string]any) {
	if strings.TrimSpace(eventType) == "" {
		eventType = "node_event"
	}
	event := map[string]any{
		"type":   eventType,
		"nodeId": nodeID,
		"status": status,
	}
	if summary != "" {
		event["summary"] = summary
	}
	for key, value := range details {
		if key == "type" || key == "nodeId" || key == "occurredAt" {
			continue
		}
		event[key] = value
	}
	appendDAGRunEvent(run, event)
}

func recordDAGNodeOutputs(run *domainworkflow.Run, nodeID string, values map[string]any) {
	if len(values) == 0 {
		return
	}
	ensureDAGExecutionMetadata(run)
	nodeOutputs := metadataMap(run.Metadata["nodeOutputs"])
	existing := metadataMap(nodeOutputs[nodeID])
	for key, value := range values {
		if value == nil {
			continue
		}
		if mapped, ok := value.(map[string]any); ok && len(mapped) == 0 {
			continue
		}
		existing[key] = value
	}
	nodeOutputs[nodeID] = existing
	run.Metadata["nodeOutputs"] = nodeOutputs
}

func metadataMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func metadataMapSlice(value any) []map[string]any {
	if typed, ok := value.([]map[string]any); ok {
		return typed
	}
	if items, ok := value.([]any); ok {
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	}
	return []map[string]any{}
}

func withNodeRunsMetadata(metadata map[string]any, definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	next := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		next[key] = value
	}
	nodeRunItems := mapNodeRunsToSlice(definition, nodeRuns)
	next["nodeRuns"] = nodeRunItems
	statuses := make(map[string]string, len(nodeRunItems))
	for _, item := range nodeRunItems {
		statuses[item.NodeID] = item.Status
	}
	next["nodeStatus"] = statuses
	return next
}

func withGatewayApprovalWorkflowMetadata(metadata map[string]any, input domainworkflow.Input) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	for _, key := range gatewayApprovalWorkflowMetadataKeys {
		if value := workflowMetadataString(input.Variables, key); value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func workflowApprovalGatewayMetadata(run domainworkflow.Run, nodeID string) map[string]any {
	metadata := map[string]any{
		"workflowRunId": run.ID,
		"nodeId":        nodeID,
	}
	for _, key := range gatewayApprovalWorkflowMetadataKeys {
		if value := workflowMetadataString(run.Metadata, key); value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func workflowMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func cloneRunMetadataForAsyncWorker(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = cloneRunMetadataValueForAsyncWorker(value)
	}
	return out
}

func cloneRunMetadataValueForAsyncWorker(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneRunMetadataForAsyncWorker(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneRunMetadataForAsyncWorker(item))
		}
		return out
	case []domainworkflow.NodeRun:
		return append([]domainworkflow.NodeRun(nil), typed...)
	case []domainworkflow.Step:
		return append([]domainworkflow.Step(nil), typed...)
	case []dagWorkflowNode:
		return append([]dagWorkflowNode(nil), typed...)
	case []dagWorkflowEdge:
		return append([]dagWorkflowEdge(nil), typed...)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneRunMetadataValueForAsyncWorker(item))
		}
		return out
	default:
		return value
	}
}
