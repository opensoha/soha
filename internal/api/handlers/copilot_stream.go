package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

func (h *CopilotHandler) StreamMessage(c *gin.Context) {
	var req dto.WorkbenchSendMessageStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workbench stream payload")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sessionID := c.Param("sessionID")
	seq := 0
	emit := func(event domaincopilot.WorkbenchStreamEvent) bool {
		seq++
		if event.ID == "" {
			event.ID = fmt.Sprintf("evt:%s:%06d", sessionID, seq)
		}
		event.SessionID = sessionID
		event.Sequence = seq
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
			return false
		}
		c.Writer.Flush()
		return true
	}

	if !emit(domaincopilot.WorkbenchStreamEvent{Type: "agent.status", ProviderID: "internal", ProviderKind: "internal", Status: "running"}) {
		return
	}

	principal := apiMiddleware.PrincipalFromContext(c)
	envelope, err := h.service.SendMessage(c.Request.Context(), principal, sessionID, req.Content, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		retryable := false
		emit(domaincopilot.WorkbenchStreamEvent{Type: "error", Message: err.Error(), Retryable: &retryable})
		emit(domaincopilot.WorkbenchStreamEvent{Type: "agent.status", ProviderID: "internal", ProviderKind: "internal", Status: "failed"})
		return
	}

	for _, event := range workbenchStreamEventsFromEnvelope(sessionID, envelope, time.Now().UTC()) {
		if !emit(event) {
			return
		}
	}
}

func workbenchStreamEventsFromEnvelope(sessionID string, envelope domaincopilot.SessionMessageEnvelope, now time.Time) []domaincopilot.WorkbenchStreamEvent {
	assistant := latestAssistantMessage(envelope.Messages)
	messageID := assistant.ID
	providerID := stringMetadata(assistant.Metadata, "providerId", "agentProviderId")
	providerKind := providerKindOrInternal(stringMetadata(assistant.Metadata, "providerKind"))
	events := make([]domaincopilot.WorkbenchStreamEvent, 0, 4+len(envelope.ToolCalls)*2+len(envelope.AnalysisArtifacts)*2)
	base := func(eventType, runID string) domaincopilot.WorkbenchStreamEvent {
		return domaincopilot.WorkbenchStreamEvent{
			ID:           "",
			Type:         eventType,
			SessionID:    sessionID,
			RunID:        runID,
			MessageID:    messageID,
			CreatedAt:    now,
			ProviderID:   providerID,
			ProviderKind: providerKind,
		}
	}

	toolCalls := collectWorkbenchToolCalls(envelope)
	if len(toolCalls) > 0 || len(envelope.AnalysisArtifacts) > 0 {
		summary := workbenchThinkingSummary(toolCalls, envelope.AnalysisArtifacts)
		thinking := base("thinking.delta", firstArtifactRunID(envelope.AnalysisArtifacts))
		thinking.TextDelta = summary
		events = append(events, thinking)
		for _, tool := range toolCalls {
			started := base("tool.started", firstString(tool.ArtifactRefs...))
			running := tool
			running.Status = "running"
			started.ToolCall = &running
			events = append(events, started)

			completed := base("tool.completed", firstString(tool.ArtifactRefs...))
			completed.ToolCall = &tool
			events = append(events, completed)
		}
		for _, artifact := range envelope.AnalysisArtifacts {
			for _, evidence := range artifact.Evidence {
				source := base("source.updated", artifact.RunID)
				source.Source = &domaincopilot.WorkbenchSource{
					ID:      evidence.ID,
					Kind:    sourceKindFromEvidence(evidence.Kind),
					Title:   evidence.Title,
					Summary: evidence.Summary,
				}
				events = append(events, source)
			}
			updated := base("artifact.updated", artifact.RunID)
			updated.Artifact = artifact
			events = append(events, updated)
		}
		done := base("thinking.done", firstArtifactRunID(envelope.AnalysisArtifacts))
		done.Summary = summary
		done.Collapsed = true
		events = append(events, done)
	}

	if strings.TrimSpace(assistant.Content) != "" {
		delta := base("message.delta", firstArtifactRunID(envelope.AnalysisArtifacts))
		delta.Role = "assistant"
		delta.ContentDelta = assistant.Content
		events = append(events, delta)

		done := base("message.done", firstArtifactRunID(envelope.AnalysisArtifacts))
		done.Role = "assistant"
		done.Content = assistant.Content
		done.Metadata = assistant.Metadata
		events = append(events, done)
	}

	status := base("agent.status", firstArtifactRunID(envelope.AnalysisArtifacts))
	status.Status = "succeeded"
	if providerID == "" {
		status.ProviderID = "internal"
	}
	events = append(events, status)
	return events
}

func latestAssistantMessage(messages []domaincopilot.Message) domaincopilot.Message {
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

func collectWorkbenchToolCalls(envelope domaincopilot.SessionMessageEnvelope) []domaincopilot.WorkbenchToolCall {
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
		out = append(out, workbenchToolCallFromExecution(tool, artifactRefs...))
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

func workbenchToolCallFromExecution(tool domaincopilot.ToolExecution, artifactRefs ...string) domaincopilot.WorkbenchToolCall {
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
		Status:        normalizeWorkbenchToolStatus(tool.Status),
		InputPreview:  tool.Input,
		OutputPreview: tool.Output,
		Summary:       tool.Summary,
		ArtifactRefs:  compactStrings(artifactRefs),
		StartedAt:     startedAtRef,
		CompletedAt:   tool.CompletedAt,
		DurationMs:    durationMs,
	}
}

func normalizeWorkbenchToolStatus(status string) string {
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

func workbenchThinkingSummary(tools []domaincopilot.WorkbenchToolCall, artifacts []domaincopilot.AnalysisArtifact) string {
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

func firstArtifactRunID(items []domaincopilot.AnalysisArtifact) string {
	for _, item := range items {
		if strings.TrimSpace(item.RunID) != "" {
			return item.RunID
		}
	}
	return ""
}

func sourceKindFromEvidence(kind string) string {
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

func stringMetadata(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func providerKindOrInternal(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hermes", "openclaw", "general":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "internal"
	}
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}
