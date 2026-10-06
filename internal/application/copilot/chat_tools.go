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
	}
	bindings = append(bindings, chatKubernetesToolBindings()...)
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
	default:
		if !chatKubernetesReadTool(binding.ToolName) {
			return nil, fmt.Errorf("%w: tool is not available to chat", apperrors.ErrAccessDenied)
		}
		arguments, err = chatResourceToolArguments(run, binding.ToolName, input)
		if err != nil {
			return nil, err
		}
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
	keys, options := chatResourceToolKeys(toolName)
	pinned := map[string]string{"clusterId": run.Scope.ClusterID, "namespace": run.Scope.Namespace,
		"serviceName": run.Scope.Service, "podName": run.Scope.Pod, "nodeName": run.Scope.Node, "deploymentName": run.Scope.Workload}
	arguments := make(map[string]any, len(keys)+len(options))
	for _, key := range keys {
		requested := strings.TrimSpace(stringValue(input[key]))
		if pinned[key] != "" && requested != "" && pinned[key] != requested {
			return nil, fmt.Errorf("%w: resource query exceeds the run scope", apperrors.ErrAccessDenied)
		}
		value := firstNonEmpty(pinned[key], requested)
		if len(value) > 256 {
			return nil, fmt.Errorf("%w: resource identifier is too long", apperrors.ErrInvalidArgument)
		}
		arguments[key] = value
	}
	for _, key := range options {
		if value, ok := input[key]; ok {
			arguments[key] = value
		}
	}
	if (toolName == "k8s.pods.metrics" || toolName == "k8s.deployments.metrics") && run.Scope.TimeRangeMinutes > 0 {
		if intCondition(input["rangeMinutes"]) > run.Scope.TimeRangeMinutes {
			return nil, fmt.Errorf("%w: metric query exceeds the run time range", apperrors.ErrAccessDenied)
		}
		if intCondition(input["rangeMinutes"]) == 0 {
			arguments["rangeMinutes"] = min(15, run.Scope.TimeRangeMinutes)
		}
	}
	if arguments["clusterId"] == "" {
		return nil, fmt.Errorf("%w: cluster is required", apperrors.ErrInvalidArgument)
	}
	return arguments, nil
}

func chatResourceToolKeys(toolName string) ([]string, []string) {
	keys := []string{"clusterId", "namespace"}
	options := []string{}
	switch toolName {
	case "k8s.crds.list":
		keys = []string{"clusterId"}
	case "k8s.custom_resources.list":
		keys = append(keys, "crdName")
	case "k8s.nodes.detail":
		keys = []string{"clusterId", "nodeName"}
	case "k8s.pods.describe", "k8s.pods.logs", "k8s.pods.metrics":
		keys = append(keys, "podName")
		if toolName == "k8s.pods.logs" {
			keys = append(keys, "container")
			options = []string{"tailLines", "sinceSeconds", "previous"}
		}
	case "k8s.deployments.rollout_status", "k8s.deployments.events", "k8s.deployments.metrics":
		keys = append(keys, "deploymentName")
	case "k8s.services.backends", "k8s.routes.context":
		keys = append(keys, "serviceName")
	}
	if toolName == "k8s.pods.metrics" || toolName == "k8s.deployments.metrics" {
		options = append(options, "rangeMinutes", "stepSeconds")
	}
	if toolName == "k8s.events.list" || toolName == "k8s.deployments.events" {
		options = append(options, "limit")
	}
	return keys, options
}

func chatKubernetesToolBindings() []domaincopilot.AgentToolBinding {
	tools := []struct{ name, permission string }{
		{"k8s.crds.list", appaccess.PermPlatformExtensionsView},
		{"k8s.custom_resources.list", appaccess.PermPlatformExtensionsView},
		{"k8s.pods.metrics", appaccess.PermPlatformPodsView},
		{"k8s.deployments.metrics", appaccess.PermPlatformDeploymentView},
		{"k8s.namespaces.list", appaccess.PermPlatformNamespacesView},
		{"k8s.workloads.overview", appaccess.PermPlatformWorkloadsOverviewView},
		{"k8s.configmaps.list", appaccess.PlatformActionPermission("configuration", "ConfigMap", "view")},
		{"k8s.secrets.metadata", appaccess.PlatformActionPermission("configuration", "Secret", "view")},
		{"k8s.helm.releases.list", appaccess.PermPlatformHelmView},
		{"k8s.pods.list", appaccess.PermPlatformPodsView},
		{"k8s.pods.logs", appaccess.PermPlatformPodsLogs},
		{"k8s.pods.describe", appaccess.PermPlatformPodsView},
		{"k8s.deployments.list", appaccess.PermPlatformDeploymentView},
		{"k8s.deployments.rollout_status", appaccess.PermPlatformDeploymentView},
		{"k8s.deployments.events", appaccess.PermObserveEventsView},
		{"k8s.services.list", appaccess.PlatformActionPermission("network", "Service", "view")},
		{"k8s.services.backends", appaccess.PlatformActionPermission("network", "Service", "view")},
		{"k8s.routes.context", appaccess.PlatformActionPermission("network", "Ingress", "view")},
		{"k8s.storage.context", appaccess.PlatformActionPermission("storage", "PersistentVolumeClaim", "view")},
		{"k8s.nodes.detail", appaccess.PermPlatformNodesView},
		{"k8s.events.list", appaccess.PermObserveEventsView},
	}
	bindings := make([]domaincopilot.AgentToolBinding, 0, len(tools))
	for _, tool := range tools {
		bindings = append(bindings, domaincopilot.AgentToolBinding{ID: tool.name, ToolKind: "mcp", AdapterID: "platform-native.v1", ToolName: tool.name, PermissionKey: tool.permission})
	}
	return bindings
}

func chatKubernetesReadTool(name string) bool {
	for _, binding := range chatKubernetesToolBindings() {
		if binding.ToolName == name {
			return true
		}
	}
	return false
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
