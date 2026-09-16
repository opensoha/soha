package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
)

type chatToolInvoker interface {
	InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
}

type chatChangeRequester interface {
	RequestToolApproval(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
}

func chatToolEvents(run domaincopilot.AgentRun, execution domaincopilot.ToolExecution, eventType string) []domaincopilot.WorkbenchStreamEvent {
	if run.CapabilityID != "general" {
		return nil
	}
	call := streamToolCallFromExecution(execution)
	if citationID := stringValue(execution.Output["citationId"]); citationID != "" {
		call.EvidenceRefs = []string{citationID}
	}
	events := []domaincopilot.WorkbenchStreamEvent{{Type: eventType, ID: execution.ID + ":" + eventType, RunID: run.ID, MessageID: run.ID + ":reply", ToolCall: &call}}
	if eventType == "tool.completed" {
		run.ToolExecutions = []domaincopilot.ToolExecution{execution}
		for _, source := range chatRunSources(run) {
			events = append(events, domaincopilot.WorkbenchStreamEvent{Type: "source.updated", ID: source.ID + ":source", RunID: run.ID, MessageID: run.ID + ":reply", Source: &source})
		}
	}
	return events
}

func chatKnowledgeBaseIDs(metadata domaincopilot.SessionMetadata) []string {
	if !metadata.KnowledgeContext.Enabled {
		return nil
	}
	return normalizeStringList(metadata.KnowledgeContext.KnowledgeBaseIDs)
}

func chatAgentToolBindings(metadata domaincopilot.SessionMetadata) []domaincopilot.AgentToolBinding {
	bindings := []domaincopilot.AgentToolBinding{
		{ID: "agent.delegate", ToolKind: "internal_api", AdapterID: "conversation", ToolName: "agent.delegate", PermissionKey: appaccess.PermObserveAIChatUse},
		{ID: "artifact.preview", ToolKind: "internal_api", AdapterID: "conversation", ToolName: "artifact.preview", PermissionKey: appaccess.PermObserveAIChatUse},
		{ID: "change.request", ToolKind: "internal_api", AdapterID: "conversation", ToolName: "change.request", PermissionKey: appaccess.PermAIGatewayInvoke},
		{ID: "k8s.workloads.overview", ToolKind: "mcp", AdapterID: "platform-native.v1", ToolName: "k8s.workloads.overview", PermissionKey: appaccess.PermPlatformWorkloadsOverviewView},
		{ID: "k8s.nodes.detail", ToolKind: "mcp", AdapterID: "platform-native.v1", ToolName: "k8s.nodes.detail", PermissionKey: appaccess.PermPlatformNodesView},
		{ID: "k8s.services.backends", ToolKind: "mcp", AdapterID: "platform-native.v1", ToolName: "k8s.services.backends", PermissionKey: appaccess.PlatformActionPermission("network", "Service", "view")},
	}
	if len(chatKnowledgeBaseIDs(metadata)) > 0 {
		bindings = append(bindings, domaincopilot.AgentToolBinding{ID: "knowledge.search", ToolKind: "mcp", AdapterID: "knowledge.v1", ToolName: "knowledge.search", PermissionKey: appaccess.PermAIKnowledgeView})
	}
	return bindings
}

func (s *Service) executeChatAgentTool(ctx context.Context, run domaincopilot.AgentRun, binding domaincopilot.AgentToolBinding, input map[string]any) (map[string]any, error) {
	if binding.ToolName == "agent.delegate" {
		return s.delegateChatSpecialist(ctx, run, input)
	}
	if binding.ToolName == "artifact.preview" {
		return chatArtifactPreview(run, input)
	}
	if binding.ToolName == "change.request" {
		return s.requestChatChange(ctx, run, input)
	}
	gateway, ok := s.workbenchInvoker.(chatToolInvoker)
	if !ok {
		return nil, fmt.Errorf("%w: AI Gateway tools are unavailable", apperrors.ErrUnsupportedOperation)
	}
	principal, err := agentToolPrincipal(run)
	if err != nil {
		return nil, err
	}
	remaining := remainingChatToolEvidenceTokens(run)
	if remaining <= 0 {
		return nil, fmt.Errorf("%w: this turn's evidence budget is exhausted", apperrors.ErrInvalidArgument)
	}
	var arguments map[string]any
	switch binding.ToolName {
	case "knowledge.search":
		ids := agentPrincipalStringList(run.Input["_sohaKnowledgeBaseIds"])
		query := strings.TrimSpace(stringValue(input["query"]))
		if len(ids) == 0 || query == "" || len([]rune(query)) > 2048 {
			return nil, fmt.Errorf("%w: knowledge search requires selected bases and a bounded query", apperrors.ErrInvalidArgument)
		}
		arguments = map[string]any{"knowledgeBaseIds": ids, "query": query, "topK": minPositive(intCondition(input["limit"]), 5)}
	case "k8s.workloads.overview", "k8s.nodes.detail", "k8s.services.backends":
		arguments, err = chatResourceToolArguments(run, binding.ToolName, input)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: tool is not available to chat", apperrors.ErrAccessDenied)
	}
	result, err := gateway.InvokeTool(ctx, principal, domainaigateway.ToolInvocationRequest{ToolName: binding.ToolName, SessionID: run.SessionID, Input: arguments})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result.Output)
	if err != nil {
		return nil, err
	}
	content := []rune(redaction.Text(string(data)))
	maxCharacters := min(8000, remaining*4)
	truncated := len(content) > maxCharacters
	if truncated {
		content = content[:maxCharacters]
	}
	hash := sha256.Sum256([]byte(string(content)))
	return map[string]any{"result": result.Result, "content": string(content), "citationId": "tool:" + hex.EncodeToString(hash[:12]), "truncated": truncated, "estimatedTokens": (len(content) + 3) / 4, "relatedIds": result.RelatedIDs}, nil
}

func chatResourceToolArguments(run domaincopilot.AgentRun, toolName string, input map[string]any) (map[string]any, error) {
	keys := []string{"clusterId", "namespace"}
	if toolName == "k8s.nodes.detail" {
		keys = []string{"clusterId", "nodeName"}
	}
	if toolName == "k8s.services.backends" {
		keys = append(keys, "serviceName")
	}
	pinned := map[string]string{"clusterId": run.Scope.ClusterID, "namespace": run.Scope.Namespace, "serviceName": run.Scope.Service}
	arguments := make(map[string]any, len(keys))
	for _, key := range keys {
		requested := stringValue(input[key])
		if pinned[key] != "" && requested != "" && pinned[key] != requested {
			return nil, fmt.Errorf("%w: resource query exceeds the run scope", apperrors.ErrAccessDenied)
		}
		value := firstNonEmpty(pinned[key], requested)
		if len(value) > 256 {
			return nil, fmt.Errorf("%w: resource identifier is too long", apperrors.ErrInvalidArgument)
		}
		arguments[key] = value
	}
	if arguments["clusterId"] == "" {
		return nil, fmt.Errorf("%w: cluster is required", apperrors.ErrInvalidArgument)
	}
	return arguments, nil
}

func remainingChatToolEvidenceTokens(run domaincopilot.AgentRun) int {
	// Parent and its single specialist reserve disjoint portions of the same 6000-token evidence allowance.
	remaining := 6000
	for _, binding := range run.ToolBindings {
		if binding.ToolName == "agent.delegate" {
			remaining = 4000
		}
	}
	if run.ParentRunID != "" {
		remaining = 2000
	}
	usage := mapValue(run.Input["contextSnapshot"])["budgetUsage"]
	switch value := usage.(type) {
	case domaincopilot.ContextBudgetUsage:
		remaining -= value.EvidenceTokens
	case map[string]any:
		remaining -= intCondition(value["evidenceTokens"])
	}
	for _, execution := range run.ToolExecutions {
		remaining -= intCondition(execution.Output["estimatedTokens"])
	}
	return max(0, remaining)
}
