package copilot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) chatProvider(providerID string) (domaincopilot.AgentProvider, error) {
	provider := s.resolveAgentProvider(providerID)
	if !provider.Enabled || stringValue(provider.Config["pluginId"]) == "" || !slices.Contains(provider.Capabilities, "general") {
		return domaincopilot.AgentProvider{}, fmt.Errorf("%w: selected agent does not support chat", apperrors.ErrUnsupportedOperation)
	}
	if provider.RuntimeStatus == nil || provider.RuntimeStatus.State != "ready" {
		return domaincopilot.AgentProvider{}, fmt.Errorf("%w: selected agent runner is unavailable", apperrors.ErrClusterUnready)
	}
	return provider, nil
}

func (s *Service) queueAgentChatMessage(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, metadata domaincopilot.SessionMetadata, content, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	provider, err := s.chatProvider(metadata.AgentProviderID)
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > 16384 {
		return domaincopilot.SessionMessageEnvelope{}, fmt.Errorf("%w: chat content must contain 1 to 16384 characters", apperrors.ErrInvalidArgument)
	}
	history, err := s.listRecentMessages(ctx, session.ID, 21)
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	chat := agentChatInput(history, content, normalizeLocale(locale))
	envelope, err := s.buildChatContext(ctx, principal, session.ID, metadata, content)
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	var snapshot map[string]any
	var evidence []domaincopilot.ContextEvidence
	if envelope != nil {
		evidence = envelope.Evidence
		snapshot = contextSnapshot(*envelope)
		if len(envelope.Evidence) > 0 || len(envelope.Truncations) > 0 {
			chat.Context = contextEvidenceSystemMessage(*envelope).Content
		}
		if metadata.KnowledgeContext.Enabled {
			snapshot["knowledgeBaseIds"] = metadata.KnowledgeContext.KnowledgeBaseIDs
			snapshot["knowledgeMode"] = "on_demand"
		}
		snapshot["historyTruncated"] = chat.HistoryTruncated
		snapshot["historyMessageCount"] = len(chat.History)
		if utf8.RuneCountInString(chat.Context) > 32768 {
			return domaincopilot.SessionMessageEnvelope{}, fmt.Errorf("%w: knowledge context exceeds runtime limit", apperrors.ErrInvalidArgument)
		}
	}
	userMessage, err := s.messages.CreateMessage(ctx, domaincopilot.Message{ID: uuid.NewString(), SessionID: session.ID, Role: "user", Content: content, Metadata: map[string]any{"mode": "general", "userId": principal.UserID, "contextSnapshot": snapshot}, CreatedAt: time.Now().UTC()})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	run, err := s.createAgentRun(ctx, principal, domaincopilot.AgentRunInput{
		ProviderID: provider.ID, CapabilityID: "general", SessionID: session.ID, CreatedBy: principal.UserID,
		Scope: metadata.Scope, Toolset: metadata.Toolset, SkillIDs: metadata.Toolset.EnabledSkillIDs,
		ToolBindings:   chatAgentToolBindings(metadata),
		TimeoutSeconds: budgetInt(metadata.Toolset, "timeoutSeconds", 600),
		Input:          map[string]any{"question": content, "mode": "general", "locale": normalizeLocale(locale), "chat": chat, "requestMessageId": userMessage.ID, "contextSnapshot": snapshot, "_sohaKnowledgeBaseIds": chatKnowledgeBaseIDs(metadata), "_sohaEvidence": evidence},
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	queued := domaincopilot.Message{ID: run.ID + ":queued", SessionID: session.ID, Role: "assistant", Content: localize(locale, "已提交给助手。", "Submitted to the assistant."), CreatedAt: time.Now().UTC(),
		Metadata: finalWorkbenchMessageMetadata(map[string]any{"mode": "general", "source": "agent-runtime-queued", "agentRunId": run.ID, "agentProviderId": run.ProviderID}, nil, nil, map[string]any{"status": "queued", "providerId": run.ProviderID, "providerKind": run.ProviderKind, "runId": run.ID, "agentRunId": run.ID})}
	s.recordGlobalAssistantAudit(ctx, principal, domaincopilot.WorkbenchGlobalAssistantEventInput{Action: "send", SessionID: session.ID, Source: metadata.Source, Prompt: content}, metadata, "success")
	return domaincopilot.SessionMessageEnvelope{Messages: []domaincopilot.Message{userMessage, queued}, SessionPatch: map[string]any{"agentRunId": run.ID, "agentProviderId": provider.ID, "mode": "general"}}, nil
}

func agentChatInput(messages []domaincopilot.Message, question, locale string) sohaapi.AgentChatInput {
	history := make([]sohaapi.AgentChatMessage, 0, len(messages))
	used, truncated := 0, false
	failedRequests := failedChatRequestIndexes(messages)
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		source := stringValue(message.Metadata["source"])
		if failedRequests[index] || failedChatMessage(message) {
			continue
		}
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		if slices.Contains([]string{"model-error", "model-unconfigured", "model-empty", "agent-runtime-error", "agent-runtime-queued"}, source) {
			continue
		}
		size := utf8.RuneCountInString(message.Content)
		if size > 16384 || used+size > 32768 || len(history) == 20 {
			truncated = true
			continue
		}
		used += size
		history = append(history, sohaapi.AgentChatMessage{Role: sohaapi.AgentChatMessageRole(message.Role), Content: message.Content})
	}
	slices.Reverse(history)
	return sohaapi.AgentChatInput{Question: question, History: history, HistoryTruncated: truncated, Locale: locale}
}

func failedChatMessage(message domaincopilot.Message) bool {
	status := mapValue(message.Metadata["agentStatus"])
	return slices.Contains([]string{"failed", "error", "canceled", "cancelled", "callback_timeout", "timeout"}, stringValue(status["status"])) ||
		slices.Contains([]string{"model-error", "model-unconfigured", "model-empty", "agent-runtime-error"}, stringValue(message.Metadata["source"]))
}

func failedChatRequestIndexes(messages []domaincopilot.Message) map[int]bool {
	failed := make(map[int]bool)
	for index, message := range messages {
		if !failedChatMessage(message) {
			continue
		}
		requestID := stringValue(message.Metadata["requestMessageId"])
		for previous := index - 1; previous >= 0; previous-- {
			candidate := messages[previous]
			if candidate.Role == "user" && (requestID == "" || candidate.ID == requestID) {
				failed[previous] = true
				break
			}
			// Legacy results have no request ID; only pair within the current turn.
			if requestID == "" && candidate.Role == "assistant" && !failedChatMessage(candidate) {
				break
			}
		}
	}
	return failed
}

func (s *Service) persistAgentChatMessage(ctx context.Context, run domaincopilot.AgentRun) error {
	content := firstNonEmpty(stringValue(run.Output["answer"]), stringValue(run.Output["summary"]), stringValue(run.Output["rawOutput"]))
	role, source := "assistant", "agent-runtime"
	if run.Status != domaincopilot.AgentRunStatusCompleted || content == "" {
		role, source = "system", "agent-runtime-error"
		content = "助手暂时无法完成回复，请检查运行状态后重试。"
		if run.Status == domaincopilot.AgentRunStatusCanceled {
			content = "已取消本次回复。"
		}
	}
	metadata := finalWorkbenchMessageMetadata(map[string]any{
		"mode": "general", "source": source, "agentRunId": run.ID, "agentProviderId": run.ProviderID,
		"contextSnapshot":  run.Input["contextSnapshot"],
		"requestMessageId": run.Input["requestMessageId"],
		"workbenchEvents":  workbenchEventsFromValue(run.Output["workbenchEvents"]),
	}, run.ToolExecutions, run.AnalysisArtifacts, map[string]any{"status": run.Status, "providerId": run.ProviderID, "providerKind": run.ProviderKind, "runId": run.ID, "agentRunId": run.ID, "externalRunId": run.ExternalRunID})
	sources := chatRunSources(run)
	metadata["sources"] = sources
	metadata["contextSnapshot"] = chatUsedContextSnapshot(run, sources)
	if model := stringValue(run.Output["model"]); model != "" {
		metadata["model"] = model
	}
	if usage := agentProviderUsageSummaryFromPayload(run.Output); len(usage) > 0 {
		metadata["usage"] = usage
		metadata["usageProvenance"] = "provider_reported"
	}
	_, err := s.messages.CreateMessage(ctx, domaincopilot.Message{ID: run.ID + ":reply", SessionID: run.SessionID, Role: role, Content: content, Metadata: metadata, CreatedAt: time.Now().UTC()})
	return err
}
