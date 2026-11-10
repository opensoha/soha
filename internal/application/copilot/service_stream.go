package copilot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// ErrWorkbenchRequestNotAccepted identifies failures before durable submission.
// A transport error or storage commit failure cannot provide this guarantee.
var ErrWorkbenchRequestNotAccepted = errors.New("workbench request not accepted")

func rejectBeforeWorkbenchSubmission(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrWorkbenchRequestNotAccepted, err)
}

func (s *Service) StreamMessage(ctx context.Context, principal domainidentity.Principal, sessionID string, input domaincopilot.WorkbenchSendMessageInput, locale string) (result domaincopilot.WorkbenchStreamResult, err error) {
	beforeSubmission := true
	defer func() {
		if beforeSubmission {
			err = rejectBeforeWorkbenchSubmission(err)
		}
	}()
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	session, err := s.ownedSession(ctx, principal, strings.TrimSpace(sessionID))
	if err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	if err := s.validateChatImageInput(input, parseSessionMetadata(session.Metadata)); err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	session, err = s.persistWorkbenchModelPreferences(ctx, principal, session, input)
	if err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	metadata := parseSessionMetadata(session.Metadata)

	if nextMetadata, changed := applyGlobalAssistantInput(metadata, input); changed {
		metadata = nextMetadata
		session.Metadata = sessionMetadataMap(metadata)
		session.UpdatedAt = time.Now().UTC()
		if updated, updateErr := s.sessions.UpdateSession(ctx, principal.UserID, session.ID, session); updateErr == nil {
			session = updated
			metadata = parseSessionMetadata(session.Metadata)
		}
	}
	mode := normalizeSessionMode(firstNonEmpty(input.Mode, metadata.Mode))
	providerID := normalizeAgentProviderID(firstNonEmpty(input.AgentProviderID, metadata.AgentProviderID, agentProviderInternal))
	if input.TaskIntent != nil {
		mode = "general"
	}
	if mode != "general" {
		if err := s.authorizeProfessionalCapability(ctx, principal, mode); err != nil {
			return domaincopilot.WorkbenchStreamResult{}, err
		}
	}
	useNative := !s.shouldUseExternalAgent(providerID)
	effectiveToolset := narrowWorkbenchToolset(metadata.Toolset, input.Toolset, input.ScopeOverrides, metadata.Scope)
	effectiveMetadata := metadata
	effectiveMetadata.RequestContext = input.ContextSelection
	effectiveMetadata.Mode = mode
	effectiveMetadata.AgentProviderID = providerID
	effectiveMetadata.Toolset = effectiveToolset
	applyStreamKnowledgeContext(&effectiveMetadata, input.KnowledgeContext)
	effectiveSession := session
	effectiveSession.Metadata = sessionMetadataMap(effectiveMetadata)
	if result, handled, err := s.assistantProfileFromChat(ctx, principal, effectiveSession, input, mode, locale); handled {
		beforeSubmission = false
		return result, err
	}
	if err := validateWorkbenchExecutionInput(input, mode, useNative); err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	beforeSubmission = false
	if useNative {
		return s.queueNativeMessage(ctx, principal, effectiveSession, effectiveMetadata, input, locale)
	}

	if mode == "general" {
		return s.streamGeneralWorkbenchMessage(ctx, principal, effectiveSession, effectiveMetadata, input, locale)
	}
	return s.streamAnalysisWorkbenchMessage(ctx, principal, effectiveSession, metadata, effectiveMetadata, input, locale)
}

func (s *Service) streamAnalysisWorkbenchMessage(ctx context.Context, principal domainidentity.Principal, effectiveSession domaincopilot.Session, metadata, effectiveMetadata domaincopilot.SessionMetadata, input domaincopilot.WorkbenchSendMessageInput, locale string) (domaincopilot.WorkbenchStreamResult, error) {
	mode, providerID := effectiveMetadata.Mode, effectiveMetadata.AgentProviderID
	effectiveToolset := effectiveMetadata.Toolset
	// Analysis choices belong to this task; preserve the conversation defaults.
	effectiveMetadata.Mode = metadata.Mode
	effectiveMetadata.AgentProviderID = metadata.AgentProviderID
	effectiveSession.Metadata = sessionMetadataMap(effectiveMetadata)
	if !isRunnableSessionAnalysisMode(mode) {
		return domaincopilot.WorkbenchStreamResult{}, fmt.Errorf("%w: stream mode must be general, root_cause, performance, trace, or inspection_review", apperrors.ErrInvalidArgument)
	}
	if err := s.authorizeProfessionalCapability(ctx, principal, mode); err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}

	locale = detectMessageLocale(input.Content, locale)
	userMessage, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: effectiveSession.ID,
		Role:      "user",
		Content:   strings.TrimSpace(input.Content),
		Metadata:  map[string]any{"userId": principal.UserID, "locale": locale, "mode": mode},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	scope := mergeSessionScope(metadata.Scope, effectiveToolset.ScopeOverrides)
	analysisInput := domaincopilot.RootCauseRunInput{
		Kind:             mode,
		SessionID:        effectiveSession.ID,
		AgentProviderID:  providerID,
		Question:         input.Content,
		ClusterID:        scope.ClusterID,
		Namespace:        scope.Namespace,
		WorkloadName:     scope.Workload,
		AlertID:          scope.AlertID,
		TimeRangeMinutes: scope.TimeRangeMinutes,
	}
	if s.shouldUseExternalAgent(providerID) {
		envelope, err := s.queueSessionAgentAnalysis(ctx, principal, effectiveSession, mode, providerID, scope, effectiveToolset, analysisInput, locale)
		if err != nil {
			return domaincopilot.WorkbenchStreamResult{}, err
		}
		envelope.Messages = append([]domaincopilot.Message{userMessage}, envelope.Messages...)
		emitExternalAgentQueued(input.EventSink, envelope)
		s.recordGlobalAssistantAudit(ctx, principal, domaincopilot.WorkbenchGlobalAssistantEventInput{
			Action:           "send",
			SessionID:        effectiveSession.ID,
			Source:           effectiveMetadata.Source,
			LaunchContext:    input.LaunchContext,
			SelectionContext: input.SelectionContext,
			Prompt:           input.Content,
		}, effectiveMetadata, "success")
		return domaincopilot.WorkbenchStreamResult{
			Envelope: envelope,
			Events:   streamEventsFromEnvelope(effectiveSession.ID, envelope, time.Now().UTC(), "queued"),
		}, nil
	}

	return domaincopilot.WorkbenchStreamResult{}, fmt.Errorf("%w: internal chat analysis requires dsh", apperrors.ErrUnsupportedOperation)
}

func validateWorkbenchExecutionInput(input domaincopilot.WorkbenchSendMessageInput, mode string, useNative bool) error {
	if input.ContextSelection != nil && len(input.ContextSelection.Images) > 0 && mode != "general" && !useNative {
		return fmt.Errorf("%w: chat images are supported in general chat", apperrors.ErrUnsupportedOperation)
	}
	if input.TaskIntent != nil && !useNative {
		return fmt.Errorf("%w: this execution provider does not support per-turn task intent", apperrors.ErrUnsupportedOperation)
	}
	if mode != "general" && input.ContextSelection != nil && !useNative {
		return fmt.Errorf("%w: context references are supported in general chat", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func (s *Service) streamGeneralWorkbenchMessage(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, metadata domaincopilot.SessionMetadata, input domaincopilot.WorkbenchSendMessageInput, locale string) (domaincopilot.WorkbenchStreamResult, error) {
	if s.shouldUseExternalAgent(metadata.AgentProviderID) {
		envelope, err := s.queueAgentChatMessage(ctx, principal, session, metadata, input.Content, locale)
		if err != nil {
			return domaincopilot.WorkbenchStreamResult{}, err
		}
		emitExternalAgentQueued(input.EventSink, envelope)
		if input.EventSink != nil {
			return domaincopilot.WorkbenchStreamResult{Envelope: envelope}, nil
		}
		return domaincopilot.WorkbenchStreamResult{Envelope: envelope, Events: streamEventsFromEnvelope(session.ID, envelope, time.Now().UTC(), "queued")}, nil
	}
	result, err := s.queueNativeMessage(ctx, principal, session, metadata, input, locale)
	return result, err
}

func applyChatReplyMetadata(metadata map[string]any, reply chatReply, userMessageID string) {
	replyReactionMetadata(metadata, reply, userMessageID)
	if reply.ContextUsage != nil {
		metadata["contextUsage"] = reply.ContextUsage
	}
	if reply.Model != "" {
		metadata["model"] = reply.Model
	}
	if reply.Error != "" {
		metadata["error"] = reply.Error
	}
}

func (s *Service) updateSessionAnalysisMetadata(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, patch map[string]any, messageID string) {
	merged := bindChatSummarySource(parseSessionMetadata(session.Metadata), patch, messageID)
	merged = applySessionAnalysisPatch(merged, patch)
	patch["mode"] = merged.Mode
	session.Metadata = sessionMetadataMap(merged)
	session.UpdatedAt = time.Now().UTC()
	_, _ = s.sessions.UpdateSession(ctx, principal.UserID, session.ID, session)
}

func emitWorkbenchModelRunning(ctx context.Context, s *Service, sink domaincopilot.WorkbenchStreamEventSink) {
	if sink == nil {
		return
	}
	sink(domaincopilot.WorkbenchStreamEvent{
		Type:         "agent.status",
		ProviderID:   agentProviderInternal,
		ProviderKind: s.workbenchModelInitialProviderKind(ctx),
		Status:       "running",
	})
}

func (s *Service) workbenchModelInitialProviderKind(ctx context.Context) string {
	if s == nil {
		return "internal"
	}
	settings, err := s.resolveAIWorkbenchSettings(ctx)
	if err != nil || !settings.Enabled {
		return "internal"
	}
	if strings.TrimSpace(settings.DefaultPublicModel) == "" && strings.TrimSpace(settings.DefaultRouteID) == "" {
		return "internal"
	}
	return workbenchModelProviderKindFromEndpoint(settings.DefaultEndpoint)
}

func workbenchModelProviderKindFromEndpoint(endpoint string) string {
	switch strings.Trim(strings.TrimSpace(endpoint), "/") {
	case "messages":
		return "anthropic"
	case "chat/completions", "responses", "":
		return "openai"
	default:
		return "openai"
	}
}

func emitExternalAgentQueued(sink domaincopilot.WorkbenchStreamEventSink, envelope domaincopilot.SessionMessageEnvelope) {
	if sink == nil {
		return
	}
	assistant := latestEnvelopeAssistantMessage(envelope.Messages)
	status := mapValue(assistant.Metadata["agentStatus"])
	sink(domaincopilot.WorkbenchStreamEvent{
		Type:         "agent.status",
		RunID:        firstNonEmpty(stringValue(status["runId"]), stringValue(status["agentRunId"])),
		MessageID:    assistant.ID,
		ProviderID:   firstNonEmpty(stringValue(status["providerId"]), stringValue(assistant.Metadata["agentProviderId"]), agentProviderInternal),
		ProviderKind: firstNonEmpty(stringValue(status["providerKind"]), providerKindOrInternalApp(stringValue(status["providerId"]))),
		Status:       firstNonEmpty(stringValue(status["status"]), domaincopilot.AgentRunStatusQueued),
	})
	sink(domaincopilot.WorkbenchStreamEvent{
		Type:      "message.done",
		MessageID: assistant.ID,
		Role:      "assistant",
		Content:   assistant.Content,
		Metadata:  assistant.Metadata,
	})
}

func analysisToolIdentity(mode string) (string, string) {
	switch normalizeSessionMode(mode) {
	case "performance":
		return "metrics.v1", "metrics.anomaly_summary"
	case "trace":
		return "traces.v1", "traces.find_slow_spans"
	case "inspection_review":
		return "platform-native.v1", "inspection.review"
	default:
		return "platform-native.v1", "root_cause.analysis"
	}
}

func validWorkbenchReasoningEffort(value string) bool {
	switch value {
	case "", "auto", "low", "medium", "high":
		return true
	default:
		return false
	}
}

func (s *Service) persistWorkbenchModelPreferences(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, input domaincopilot.WorkbenchSendMessageInput) (domaincopilot.Session, error) {
	if input.ModelPreferences == nil {
		return session, nil
	}
	metadata := parseSessionMetadata(session.Metadata)
	preferences := *input.ModelPreferences
	preferences.PublicModel = strings.TrimSpace(preferences.PublicModel)
	if len(preferences.PublicModel) > 200 || !validWorkbenchReasoningEffort(preferences.ReasoningEffort) {
		return domaincopilot.Session{}, fmt.Errorf("%w: invalid model preferences", apperrors.ErrInvalidArgument)
	}
	if normalizeAgentProviderID(firstNonEmpty(input.AgentProviderID, metadata.AgentProviderID)) != agentProviderInternal {
		return domaincopilot.Session{}, fmt.Errorf("%w: model preferences require internal chat", apperrors.ErrInvalidArgument)
	}
	metadata.ModelPreferences = preferences
	session.Metadata = sessionMetadataMap(metadata)
	session.UpdatedAt = time.Now().UTC()
	return s.sessions.UpdateSession(ctx, principal.UserID, session.ID, session)
}

func applyStreamKnowledgeContext(metadata *domaincopilot.SessionMetadata, input *domaincopilot.KnowledgeContextConfig) {
	if input == nil {
		return
	}
	metadata.KnowledgeContext = *input
	metadata.KnowledgeContext.KnowledgeBaseIDs = normalizeStringList(metadata.KnowledgeContext.KnowledgeBaseIDs)
	metadata.KnowledgeContext.TopK = min(max(metadata.KnowledgeContext.TopK, 5), 50)
}
