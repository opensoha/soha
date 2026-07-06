package copilot

import (
	"fmt"
	"strings"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

const maxAgentRunWorkbenchEvents = 200

func narrowWorkbenchToolset(sessionToolset, requestToolset domaincopilot.SessionToolset, requestScopeOverrides map[string]any, sessionScope domaincopilot.SessionScope) domaincopilot.SessionToolset {
	out := domaincopilot.SessionToolset{
		EnabledAdapterIDs: narrowStringAllowlist(sessionToolset.EnabledAdapterIDs, requestToolset.EnabledAdapterIDs, adapterSelectionMatches),
		EnabledSkillIDs:   narrowStringAllowlist(sessionToolset.EnabledSkillIDs, requestToolset.EnabledSkillIDs, exactSelectionMatches),
		DisabledToolNames: unionStringList(sessionToolset.DisabledToolNames, requestToolset.DisabledToolNames),
		BudgetOverrides:   conservativeBudgetOverrides(sessionToolset.BudgetOverrides, requestToolset.BudgetOverrides),
		ScopeOverrides:    conservativeScopeOverrides(sessionScope, sessionToolset.ScopeOverrides, requestToolset.ScopeOverrides, requestScopeOverrides),
	}
	return out
}

func narrowStringAllowlist(base, requested []string, match func(string, string) bool) []string {
	base = normalizeStringList(base)
	requested = normalizeStringList(requested)
	if len(requested) == 0 {
		return base
	}
	if len(base) == 0 {
		return requested
	}
	out := make([]string, 0, len(requested))
	for _, request := range requested {
		for _, allowed := range base {
			if exactSelectionMatches(allowed, request) {
				out = append(out, request)
				break
			}
			if match(allowed, request) {
				out = append(out, request)
				break
			}
			if match(request, allowed) {
				out = append(out, allowed)
				break
			}
		}
	}
	return normalizeStringList(out)
}

func exactSelectionMatches(selection, value string) bool {
	return strings.TrimSpace(selection) != "" && strings.TrimSpace(selection) == strings.TrimSpace(value)
}

func unionStringList(left, right []string) []string {
	return normalizeStringList(append(append([]string{}, left...), right...))
}

func conservativeBudgetOverrides(base, requested map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, requestValue := range requested {
		requestNumber, requestOK := positiveFloat(requestValue)
		baseValue, hasBase := out[key]
		if !hasBase {
			if value, ok := cappedKnownBudgetValue(key, requestValue, requestNumber, requestOK); ok {
				out[key] = value
			}
			continue
		}
		baseNumber, baseOK := positiveFloat(baseValue)
		if baseOK && requestOK {
			if requestNumber < baseNumber {
				out[key] = requestValue
			}
			continue
		}
		if !baseOK && requestOK {
			if value, ok := cappedKnownBudgetValue(key, requestValue, requestNumber, requestOK); ok {
				out[key] = value
			}
		}
	}
	return out
}

func cappedKnownBudgetValue(key string, requestValue any, requestNumber float64, requestOK bool) (any, bool) {
	if !requestOK {
		return nil, false
	}
	capValue, ok := defaultWorkbenchBudgetCap(key)
	if !ok {
		return nil, false
	}
	if requestNumber > capValue {
		return capValue, true
	}
	return requestValue, true
}

func defaultWorkbenchBudgetCap(key string) (float64, bool) {
	switch strings.TrimSpace(key) {
	case "timeoutSeconds":
		return 600, true
	case "maxEvidenceItems":
		return 100, true
	default:
		return 0, false
	}
}

func conservativeScopeOverrides(sessionScope domaincopilot.SessionScope, maps ...map[string]any) map[string]any {
	out := map[string]any{}
	anchors := map[string]string{
		"clusterId": sessionScope.ClusterID,
		"namespace": sessionScope.Namespace,
		"workload":  sessionScope.Workload,
		"service":   sessionScope.Service,
		"alertId":   sessionScope.AlertID,
	}
	if sessionScope.TimeRangeMinutes > 0 {
		out["timeRangeMinutes"] = sessionScope.TimeRangeMinutes
	}
	for _, values := range maps {
		for _, key := range []string{"clusterId", "namespace", "workload", "service", "alertId"} {
			value := strings.TrimSpace(stringValue(values[key]))
			if value == "" {
				continue
			}
			if existing := strings.TrimSpace(stringValue(out[key])); existing != "" && existing != value {
				continue
			}
			if anchor := strings.TrimSpace(anchors[key]); anchor != "" && anchor != value {
				continue
			}
			out[key] = value
		}
		if value := intValue(values["timeRangeMinutes"], 0); value > 0 {
			current := intValue(out["timeRangeMinutes"], 0)
			if current == 0 || value < current {
				out["timeRangeMinutes"] = value
			}
		}
	}
	return out
}

func finalWorkbenchMessageMetadata(base map[string]any, tools []domaincopilot.ToolExecution, artifacts []domaincopilot.AnalysisArtifact, agentStatus map[string]any) map[string]any {
	metadata := copyMessageMetadata(base)
	toolSnapshot := mergeFinalToolExecutions(tools, artifacts)
	artifactSnapshot := append([]domaincopilot.AnalysisArtifact(nil), artifacts...)
	sources := workbenchSourcesFromArtifacts(artifactSnapshot)
	metadata["thinkingSummary"] = finalThinkingSummary(toolSnapshot, artifactSnapshot, agentStatus)
	metadata["toolExecutions"] = toolSnapshot
	metadata["sources"] = sources
	metadata["analysisArtifacts"] = artifactSnapshot
	if len(toolSnapshot) > 0 {
		metadata["toolCalls"] = toolSnapshot
	}
	if len(agentStatus) == 0 {
		agentStatus = map[string]any{"status": "succeeded"}
	}
	agentStatus = normalizeFinalAgentStatus(agentStatus)
	metadata["agentStatus"] = agentStatus
	return metadata
}

func normalizeFinalAgentStatus(agentStatus map[string]any) map[string]any {
	out := copyMessageMetadata(agentStatus)
	out["status"] = domaincopilot.AgentRunStatusToWorkbenchStatus(stringValue(out["status"]))
	return out
}

func replyAgentStatus(reply chatReply) string {
	switch strings.TrimSpace(reply.Source) {
	case "model-cancelled":
		return "cancelled"
	case "model-error", "model-unconfigured":
		return "failed"
	default:
		if strings.TrimSpace(reply.Error) != "" {
			return "failed"
		}
		return "succeeded"
	}
}

func mergeFinalToolExecutions(tools []domaincopilot.ToolExecution, artifacts []domaincopilot.AnalysisArtifact) []domaincopilot.ToolExecution {
	out := make([]domaincopilot.ToolExecution, 0, len(tools))
	seen := map[string]int{}
	add := func(tool domaincopilot.ToolExecution) {
		id := strings.TrimSpace(tool.ID)
		if id != "" {
			if index, ok := seen[id]; ok {
				out[index] = tool
				return
			}
			seen[id] = len(out)
		}
		out = append(out, tool)
	}
	for _, tool := range tools {
		add(tool)
	}
	for _, artifact := range artifacts {
		for _, tool := range artifact.ToolExecutions {
			add(tool)
		}
	}
	return out
}

func workbenchSourcesFromArtifacts(artifacts []domaincopilot.AnalysisArtifact) []domaincopilot.WorkbenchSource {
	out := make([]domaincopilot.WorkbenchSource, 0)
	seen := map[string]struct{}{}
	for _, artifact := range artifacts {
		for _, evidence := range artifact.Evidence {
			id := strings.TrimSpace(evidence.ID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, domaincopilot.WorkbenchSource{
				ID:      id,
				Kind:    streamSourceKindFromEvidence(evidence.Kind),
				Title:   evidence.Title,
				Summary: evidence.Summary,
			})
		}
	}
	return out
}

func finalThinkingSummary(tools []domaincopilot.ToolExecution, artifacts []domaincopilot.AnalysisArtifact, agentStatus map[string]any) string {
	if status := strings.TrimSpace(stringValue(agentStatus["status"])); status == "queued" || status == "running" {
		return "Agent analysis is " + status + "."
	}
	if len(tools) == 0 {
		if len(artifacts) == 0 {
			return "No tool execution snapshot was produced."
		}
		return fmt.Sprintf("Generated %d analysis artifact(s).", len(artifacts))
	}
	success := 0
	failed := 0
	for _, tool := range tools {
		switch normalizeStreamToolStatus(tool.Status) {
		case "success":
			success++
		case "error":
			failed++
		}
	}
	return fmt.Sprintf("Completed %d analysis step(s): %d succeeded, %d failed.", len(tools), success, failed)
}

func streamEventsFromEnvelope(sessionID string, envelope domaincopilot.SessionMessageEnvelope, now time.Time, finalStatus string) []domaincopilot.WorkbenchStreamEvent {
	assistant := latestEnvelopeAssistantMessage(envelope.Messages)
	messageID := assistant.ID
	agentStatus := mapValue(assistant.Metadata["agentStatus"])
	providerID := firstNonEmpty(stringValueFromMap(assistant.Metadata, "providerId", "agentProviderId"), stringValue(agentStatus["providerId"]))
	providerKind := providerKindOrInternalApp(firstNonEmpty(stringValueFromMap(assistant.Metadata, "providerKind"), stringValue(agentStatus["providerKind"]), providerID))
	events := make([]domaincopilot.WorkbenchStreamEvent, 0, 4+len(envelope.ToolCalls)*2+len(envelope.AnalysisArtifacts)*3)
	base := func(eventType, runID string) domaincopilot.WorkbenchStreamEvent {
		return domaincopilot.WorkbenchStreamEvent{
			Type:         eventType,
			SessionID:    sessionID,
			RunID:        runID,
			MessageID:    messageID,
			CreatedAt:    now,
			ProviderID:   providerID,
			ProviderKind: providerKind,
		}
	}

	toolCalls := collectStreamToolCalls(envelope)
	if len(toolCalls) > 0 || len(envelope.AnalysisArtifacts) > 0 {
		summary := streamThinkingSummary(toolCalls, envelope.AnalysisArtifacts)
		thinking := base("thinking.delta", firstStreamArtifactRunID(envelope.AnalysisArtifacts))
		thinking.TextDelta = summary
		events = append(events, thinking)
		for _, tool := range toolCalls {
			started := base("tool.started", firstNonEmpty(tool.ArtifactRefs...))
			running := tool
			running.Status = "running"
			started.ToolCall = &running
			events = append(events, started)

			completed := base("tool.completed", firstNonEmpty(tool.ArtifactRefs...))
			completed.ToolCall = &tool
			events = append(events, completed)
		}
		for _, artifact := range envelope.AnalysisArtifacts {
			for _, evidence := range artifact.Evidence {
				source := base("source.updated", artifact.RunID)
				source.Source = &domaincopilot.WorkbenchSource{
					ID:      evidence.ID,
					Kind:    streamSourceKindFromEvidence(evidence.Kind),
					Title:   evidence.Title,
					Summary: evidence.Summary,
				}
				events = append(events, source)
			}
			updated := base("artifact.updated", artifact.RunID)
			updated.Artifact = artifact
			events = append(events, updated)
		}
		done := base("thinking.done", firstStreamArtifactRunID(envelope.AnalysisArtifacts))
		done.Summary = summary
		done.Collapsed = true
		events = append(events, done)
	}

	if strings.TrimSpace(assistant.Content) != "" {
		delta := base("message.delta", firstStreamArtifactRunID(envelope.AnalysisArtifacts))
		delta.Role = "assistant"
		delta.ContentDelta = assistant.Content
		events = append(events, delta)

		done := base("message.done", firstStreamArtifactRunID(envelope.AnalysisArtifacts))
		done.Role = "assistant"
		done.Content = assistant.Content
		done.Metadata = assistant.Metadata
		events = append(events, done)
	}

	status := base("agent.status", firstStreamArtifactRunID(envelope.AnalysisArtifacts))
	status.Status = firstNonEmpty(finalStatus, "succeeded")
	if providerID == "" {
		status.ProviderID = agentProviderInternal
	}
	events = append(events, status)
	return events
}

func latestEnvelopeAssistantMessage(messages []domaincopilot.Message) domaincopilot.Message {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "assistant" {
			return messages[index]
		}
	}
	if len(messages) > 0 {
		return messages[len(messages)-1]
	}
	return domaincopilot.Message{}
}

func collectStreamToolCalls(envelope domaincopilot.SessionMessageEnvelope) []domaincopilot.WorkbenchToolCall {
	seen := map[string]struct{}{}
	out := make([]domaincopilot.WorkbenchToolCall, 0, len(envelope.ToolCalls))
	add := func(tool domaincopilot.ToolExecution, artifactRefs ...string) {
		if strings.TrimSpace(tool.ID) == "" {
			return
		}
		if _, ok := seen[tool.ID]; ok {
			return
		}
		seen[tool.ID] = struct{}{}
		out = append(out, streamToolCallFromExecution(tool, artifactRefs...))
	}
	for _, tool := range envelope.ToolCalls {
		add(tool)
	}
	for _, artifact := range envelope.AnalysisArtifacts {
		for _, tool := range artifact.ToolExecutions {
			add(tool, artifact.RunID)
		}
	}
	return out
}

func streamToolCallFromExecution(tool domaincopilot.ToolExecution, artifactRefs ...string) domaincopilot.WorkbenchToolCall {
	startedAt := tool.StartedAt
	var startedAtRef *time.Time
	if !startedAt.IsZero() {
		startedAtRef = &startedAt
	}
	var durationMs int64
	if startedAtRef != nil && tool.CompletedAt != nil {
		durationMs = tool.CompletedAt.Sub(startedAt).Milliseconds()
	}
	return domaincopilot.WorkbenchToolCall{
		ID:            tool.ID,
		AdapterID:     tool.AdapterID,
		ToolName:      tool.ToolName,
		Status:        normalizeStreamToolStatus(tool.Status),
		InputPreview:  tool.Input,
		OutputPreview: tool.Output,
		Summary:       tool.Summary,
		ArtifactRefs:  normalizeStringList(artifactRefs),
		StartedAt:     startedAtRef,
		CompletedAt:   tool.CompletedAt,
		DurationMs:    durationMs,
	}
}

func normalizeStreamToolStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "completed", "complete":
		return "success"
	case "error", "failed", "failure":
		return "error"
	case "skipped", "skip":
		return "skipped"
	case "running", "in_progress":
		return "running"
	default:
		return "pending"
	}
}

func streamThinkingSummary(tools []domaincopilot.WorkbenchToolCall, artifacts []domaincopilot.AnalysisArtifact) string {
	if len(tools) == 0 {
		if len(artifacts) == 0 {
			return "正在分析当前会话。"
		}
		return fmt.Sprintf("已生成 %d 个分析工件。", len(artifacts))
	}
	success := 0
	failed := 0
	for _, tool := range tools {
		switch tool.Status {
		case "success":
			success++
		case "error":
			failed++
		}
	}
	return fmt.Sprintf("已完成 %d 步分析：%d 个成功，%d 个失败。", len(tools), success, failed)
}

func firstStreamArtifactRunID(items []domaincopilot.AnalysisArtifact) string {
	for _, item := range items {
		if strings.TrimSpace(item.RunID) != "" {
			return item.RunID
		}
	}
	return ""
}

func streamSourceKindFromEvidence(kind string) string {
	lower := strings.ToLower(kind)
	switch {
	case strings.Contains(lower, "log"):
		return "log"
	case strings.Contains(lower, "metric"):
		return "metric"
	case strings.Contains(lower, "trace"), strings.Contains(lower, "span"):
		return "trace"
	case strings.Contains(lower, "audit"):
		return "audit"
	case strings.Contains(lower, "delivery"), strings.Contains(lower, "release"), strings.Contains(lower, "build"):
		return "delivery"
	case strings.Contains(lower, "document"), strings.Contains(lower, "doc"):
		return "document"
	default:
		return "event"
	}
}

func stringValueFromMap(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func providerKindOrInternalApp(value string) string {
	kind := strings.ToLower(strings.TrimSpace(value))
	switch kind {
	case "azure_openai":
		return "azure-openai"
	case "hermes", "openclaw", "general", "openai", "anthropic", "deepseek", "qwen", "openrouter", "azure-openai", "gemini", "cohere":
		return kind
	default:
		return "internal"
	}
}

func workbenchEventsFromValue(value any) []domaincopilot.WorkbenchStreamEvent {
	var events []domaincopilot.WorkbenchStreamEvent
	if decodeStructuredValue(value, &events) {
		return events
	}
	return nil
}
