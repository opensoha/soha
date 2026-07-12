package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) StreamMessage(ctx context.Context, principal domainidentity.Principal, sessionID string, input domaincopilot.WorkbenchSendMessageInput, locale string) (domaincopilot.WorkbenchStreamResult, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	session, err := s.sessions.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
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
	effectiveToolset := narrowWorkbenchToolset(metadata.Toolset, input.Toolset, input.ScopeOverrides, metadata.Scope)
	effectiveMetadata := metadata
	effectiveMetadata.Mode = mode
	effectiveMetadata.AgentProviderID = providerID
	effectiveMetadata.Toolset = effectiveToolset
	effectiveSession := session
	effectiveSession.Metadata = sessionMetadataMap(effectiveMetadata)

	if mode == "general" {
		return s.streamGeneralWorkbenchMessage(ctx, principal, effectiveSession, effectiveMetadata, input, locale)
	}
	if !isRunnableSessionAnalysisMode(mode) {
		return domaincopilot.WorkbenchStreamResult{}, fmt.Errorf("%w: stream mode must be general, root_cause, performance, trace, or inspection_review", apperrors.ErrInvalidArgument)
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

	envelope, finalStatus, err := s.runInternalStreamAnalysisMessage(ctx, principal, effectiveSession, effectiveMetadata, userMessage, analysisInput, locale, input.EventSink)
	if err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	s.recordGlobalAssistantAudit(ctx, principal, domaincopilot.WorkbenchGlobalAssistantEventInput{
		Action:           "send",
		SessionID:        effectiveSession.ID,
		Source:           effectiveMetadata.Source,
		LaunchContext:    input.LaunchContext,
		SelectionContext: input.SelectionContext,
		Prompt:           input.Content,
	}, effectiveMetadata, "success")
	if input.EventSink != nil {
		return domaincopilot.WorkbenchStreamResult{Envelope: envelope}, nil
	}
	return domaincopilot.WorkbenchStreamResult{
		Envelope: envelope,
		Events:   streamEventsFromEnvelope(effectiveSession.ID, envelope, time.Now().UTC(), finalStatus),
	}, nil
}

func (s *Service) streamGeneralWorkbenchMessage(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, metadata domaincopilot.SessionMetadata, input domaincopilot.WorkbenchSendMessageInput, locale string) (domaincopilot.WorkbenchStreamResult, error) {
	if input.EventSink == nil {
		envelope, err := s.sendMessageWithSessionConfig(ctx, principal, session, metadata, input.Content, locale)
		if err != nil {
			return domaincopilot.WorkbenchStreamResult{}, err
		}
		return domaincopilot.WorkbenchStreamResult{Envelope: envelope, Events: streamEventsFromEnvelope(session.ID, envelope, time.Now().UTC(), "succeeded")}, nil
	}
	envelope, err := s.streamGeneralMessageWithSessionConfig(ctx, principal, session, metadata, input.Content, locale, input.EventSink)
	if err != nil {
		return domaincopilot.WorkbenchStreamResult{}, err
	}
	auditResult := "success"
	if len(envelope.Messages) == 1 && envelope.Messages[0].Role == "user" {
		auditResult = "cancelled"
	}
	s.recordGlobalAssistantAudit(ctx, principal, domaincopilot.WorkbenchGlobalAssistantEventInput{
		Action: "send", SessionID: session.ID, Source: metadata.Source,
		LaunchContext: input.LaunchContext, SelectionContext: input.SelectionContext, Prompt: input.Content,
	}, metadata, auditResult)
	return domaincopilot.WorkbenchStreamResult{Envelope: envelope}, nil
}

func (s *Service) streamGeneralMessageWithSessionConfig(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, sessionMeta domaincopilot.SessionMetadata, content, locale string, eventSink domaincopilot.WorkbenchStreamEventSink) (domaincopilot.SessionMessageEnvelope, error) {
	locale = detectMessageLocale(content, locale)
	userMessage, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "user",
		Content:   strings.TrimSpace(content),
		Metadata:  map[string]any{"userId": principal.UserID, "locale": locale, "mode": sessionMeta.Mode},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	priorMessages, _ := s.listRecentMessages(ctx, session.ID, 20)
	emitWorkbenchModelRunning(ctx, s, eventSink)
	emittedDelta := false
	reply := s.generateReplyStream(ctx, principal, session.ID, sessionMeta.Mode, buildProviderChatMessages(priorMessages, userMessage, locale), locale, func(delta string) bool {
		if delta == "" {
			return true
		}
		emittedDelta = true
		return eventSink(domaincopilot.WorkbenchStreamEvent{
			Type:         "message.delta",
			Role:         "assistant",
			ContentDelta: delta,
		})
	})
	if reply.Source == "model-cancelled" {
		eventSink(domaincopilot.WorkbenchStreamEvent{
			Type:         "agent.status",
			ProviderID:   firstNonEmpty(reply.ProviderID, agentProviderInternal),
			ProviderKind: firstNonEmpty(reply.ProviderKind, "internal"),
			Status:       replyAgentStatus(reply),
		})
		return domaincopilot.SessionMessageEnvelope{
			Messages: []domaincopilot.Message{userMessage},
		}, nil
	}
	if !emittedDelta && strings.TrimSpace(reply.Content) != "" {
		eventSink(domaincopilot.WorkbenchStreamEvent{
			Type:         "message.delta",
			Role:         "assistant",
			ContentDelta: reply.Content,
		})
	}
	assistantMetadata := map[string]any{
		"mode":              sessionMeta.Mode,
		"source":            reply.Source,
		"locale":            locale,
		"analysisArtifacts": []domaincopilot.AnalysisArtifact{},
	}
	if reply.Model != "" {
		assistantMetadata["model"] = reply.Model
	}
	if reply.Error != "" {
		assistantMetadata["error"] = reply.Error
	}
	agentStatus := map[string]any{
		"status":       replyAgentStatus(reply),
		"providerId":   firstNonEmpty(reply.ProviderID, agentProviderInternal),
		"providerKind": firstNonEmpty(reply.ProviderKind, "internal"),
	}
	assistantMetadata = finalWorkbenchMessageMetadata(assistantMetadata, nil, nil, agentStatus)
	assistantMessage, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "assistant",
		Content:   reply.Content,
		Metadata:  assistantMetadata,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	eventSink(domaincopilot.WorkbenchStreamEvent{
		Type:      "message.done",
		MessageID: assistantMessage.ID,
		Role:      "assistant",
		Content:   assistantMessage.Content,
		Metadata:  assistantMessage.Metadata,
	})
	eventSink(domaincopilot.WorkbenchStreamEvent{
		Type:         "agent.status",
		ProviderID:   firstNonEmpty(reply.ProviderID, agentProviderInternal),
		ProviderKind: firstNonEmpty(reply.ProviderKind, "internal"),
		Status:       replyAgentStatus(reply),
	})
	return domaincopilot.SessionMessageEnvelope{
		Messages: []domaincopilot.Message{userMessage, assistantMessage},
	}, nil
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
}

func (s *Service) runInternalStreamAnalysisMessage(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, metadata domaincopilot.SessionMetadata, userMessage domaincopilot.Message, input domaincopilot.RootCauseRunInput, locale string, eventSink domaincopilot.WorkbenchStreamEventSink) (domaincopilot.SessionMessageEnvelope, string, error) {
	mode := normalizeSessionMode(input.Kind)
	scope := mergeSessionScope(metadata.Scope, metadata.Toolset.ScopeOverrides)
	runCreatedBy, runTriggerType, runDedupKey := sessionAnalysisRunPersistence(session.ID, metadata)
	toolCalls := make([]domaincopilot.ToolExecution, 0)
	artifacts := make([]domaincopilot.AnalysisArtifact, 0)
	refs := append([]domaincopilot.AnalysisRunRef{}, metadata.AnalysisRunRefs...)
	var runErr error
	liveTool := liveAnalysisToolExecution(mode, scope, metadata.Toolset, input.Question)
	emitInternalAnalysisStarted(eventSink, mode, liveTool)

	switch mode {
	case "root_cause":
		var run domaincopilot.RootCauseRun
		var artifact domaincopilot.AnalysisArtifact
		run, toolCalls, artifact, runErr = s.runSessionRootCause(ctx, principal, session.ID, scope, metadata.Toolset, input.Question, locale)
		if runErr == nil {
			artifacts = append(artifacts, artifact)
			refs = append(refs, domaincopilot.AnalysisRunRef{ID: run.ID, Kind: run.Kind, Status: run.Status, CreatedAt: run.CreatedAt.Format(time.RFC3339)})
		}
	case "performance":
		var artifact domaincopilot.AnalysisArtifact
		toolCalls, artifact, runErr = s.runSessionPerformance(ctx, session.ID, scope, metadata.Toolset, input.Question, runCreatedBy, runTriggerType, runDedupKey)
		if runErr == nil {
			artifacts = append(artifacts, artifact)
			refs = append(refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
		}
	case "trace":
		var artifact domaincopilot.AnalysisArtifact
		toolCalls, artifact, runErr = s.runSessionTrace(ctx, session.ID, scope, metadata.Toolset, input.Question, runCreatedBy, runTriggerType, runDedupKey)
		if runErr == nil {
			artifacts = append(artifacts, artifact)
			refs = append(refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
		}
	case "inspection_review":
		var artifact domaincopilot.AnalysisArtifact
		toolCalls, artifact = s.runSessionInspectionReview(session.ID, scope, metadata.Toolset, input.Question, locale)
		artifacts = append(artifacts, artifact)
		refs = append(refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	}

	finalStatus := "succeeded"
	reply := localize(locale, "已执行显式分析，但当前没有生成新的分析工件。", "Explicit analysis completed, but no new analysis artifact was produced.")
	if runErr != nil {
		finalStatus = "failed"
		failedTool := failedAnalysisToolExecution(mode, scope, metadata.Toolset, input.Question, runErr)
		failedTool.ID = liveTool.ID
		failedTool.StartedAt = liveTool.StartedAt
		toolCalls = append(toolCalls, failedTool)
		reply = localize(locale, "分析执行失败：", "Analysis failed: ") + runErr.Error()
	} else if len(artifacts) > 0 {
		reply = artifacts[0].Summary
	}

	sessionPatch := map[string]any{"mode": mode}
	if len(artifacts) > 0 {
		sessionPatch["summary"] = artifacts[0].Summary
		sessionPatch["analysisRunRefs"] = refs
	}
	assistantMetadata := finalWorkbenchMessageMetadata(map[string]any{
		"mode":              mode,
		"source":            "stream-analysis",
		"locale":            locale,
		"analysisArtifacts": artifacts,
	}, toolCalls, artifacts, map[string]any{
		"status":       finalStatus,
		"providerId":   agentProviderInternal,
		"providerKind": "internal",
		"runId":        firstStreamArtifactRunID(artifacts),
	})
	assistantMessage, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "assistant",
		Content:   reply,
		Metadata:  assistantMetadata,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, finalStatus, err
	}
	if len(sessionPatch) > 0 {
		merged := applySessionAnalysisPatch(parseSessionMetadata(session.Metadata), sessionPatch)
		session.Metadata = sessionMetadataMap(merged)
		session.UpdatedAt = time.Now().UTC()
		_, _ = s.sessions.UpdateSession(ctx, principal.UserID, session.ID, session)
	}
	envelope := domaincopilot.SessionMessageEnvelope{
		Messages:          []domaincopilot.Message{userMessage, assistantMessage},
		ToolCalls:         toolCalls,
		AnalysisArtifacts: artifacts,
		SessionPatch:      sessionPatch,
	}
	emitInternalAnalysisFinal(eventSink, mode, envelope, liveTool, finalStatus)
	return envelope, finalStatus, nil
}

func liveAnalysisToolExecution(mode string, scope domaincopilot.SessionScope, toolset domaincopilot.SessionToolset, prompt string) domaincopilot.ToolExecution {
	now := time.Now().UTC()
	adapterID, toolName := analysisToolIdentity(mode)
	return domaincopilot.ToolExecution{
		ID:        "tool:" + uuid.NewString(),
		AdapterID: adapterID,
		ToolName:  toolName,
		Status:    "running",
		Input: map[string]any{
			"prompt":          prompt,
			"scope":           scope,
			"budgetOverrides": toolset.BudgetOverrides,
		},
		StartedAt: now,
	}
}

func emitInternalAnalysisStarted(sink domaincopilot.WorkbenchStreamEventSink, mode string, tool domaincopilot.ToolExecution) {
	if sink == nil {
		return
	}
	sink(domaincopilot.WorkbenchStreamEvent{
		Type:         "agent.status",
		ProviderID:   agentProviderInternal,
		ProviderKind: "internal",
		Status:       "running",
	})
	sink(domaincopilot.WorkbenchStreamEvent{
		Type:         "thinking.delta",
		RunID:        "",
		ProviderID:   agentProviderInternal,
		ProviderKind: "internal",
		TextDelta:    streamAnalysisThinkingDelta(mode),
	})
	toolCall := streamToolCallFromExecution(tool)
	toolCall.Status = "running"
	sink(domaincopilot.WorkbenchStreamEvent{
		Type:         "tool.started",
		ProviderID:   agentProviderInternal,
		ProviderKind: "internal",
		ToolCall:     &toolCall,
	})
}

func emitInternalAnalysisFinal(sink domaincopilot.WorkbenchStreamEventSink, mode string, envelope domaincopilot.SessionMessageEnvelope, liveTool domaincopilot.ToolExecution, finalStatus string) {
	if sink == nil {
		return
	}
	now := time.Now().UTC()
	assistant := latestEnvelopeAssistantMessage(envelope.Messages)
	artifacts := envelope.AnalysisArtifacts
	runID := firstStreamArtifactRunID(artifacts)
	toolCalls := collectStreamToolCalls(envelope)
	if normalizeSessionMode(mode) == "root_cause" {
		liveTool.Status = finalStatus
		if finalStatus == "succeeded" {
			liveTool.Status = "success"
		}
		if len(artifacts) > 0 {
			liveTool.Summary = artifacts[0].Summary
			liveTool.Output = map[string]any{
				"evidenceCount":   len(artifacts[0].Evidence),
				"hypothesisCount": len(artifacts[0].Hypotheses),
				"artifactRunId":   artifacts[0].RunID,
			}
		}
		liveTool.CompletedAt = &now
		coarseTool := streamToolCallFromExecution(liveTool, runID)
		toolCalls = []domaincopilot.WorkbenchToolCall{coarseTool}
	} else {
		for index := range toolCalls {
			if index == 0 && strings.TrimSpace(liveTool.ID) != "" {
				toolCalls[index].ID = liveTool.ID
			}
		}
	}
	if len(toolCalls) > 0 {
		completed := domaincopilot.WorkbenchStreamEvent{
			Type:         "tool.completed",
			RunID:        firstNonEmpty(firstNonEmpty(toolCalls[0].ArtifactRefs...), runID),
			MessageID:    assistant.ID,
			CreatedAt:    now,
			ProviderID:   agentProviderInternal,
			ProviderKind: "internal",
			ToolCall:     &toolCalls[0],
		}
		sink(completed)
	}
	emitInternalAnalysisArtifacts(sink, assistant.ID, artifacts, now)
	summary := streamThinkingSummary(toolCalls, artifacts)
	sink(domaincopilot.WorkbenchStreamEvent{
		Type:         "thinking.done",
		RunID:        runID,
		MessageID:    assistant.ID,
		CreatedAt:    now,
		ProviderID:   agentProviderInternal,
		ProviderKind: "internal",
		Summary:      summary,
		Collapsed:    true,
	})
	if strings.TrimSpace(assistant.Content) != "" {
		sink(domaincopilot.WorkbenchStreamEvent{
			Type:         "message.done",
			RunID:        runID,
			MessageID:    assistant.ID,
			CreatedAt:    now,
			Role:         "assistant",
			Content:      assistant.Content,
			Metadata:     assistant.Metadata,
			ProviderID:   agentProviderInternal,
			ProviderKind: "internal",
		})
	}
	sink(domaincopilot.WorkbenchStreamEvent{
		Type:         "agent.status",
		RunID:        runID,
		MessageID:    assistant.ID,
		CreatedAt:    now,
		ProviderID:   agentProviderInternal,
		ProviderKind: "internal",
		Status:       firstNonEmpty(finalStatus, "succeeded"),
	})
}

func emitInternalAnalysisArtifacts(
	sink domaincopilot.WorkbenchStreamEventSink,
	messageID string,
	artifacts []domaincopilot.AnalysisArtifact,
	createdAt time.Time,
) {
	for _, artifact := range artifacts {
		for _, evidence := range artifact.Evidence {
			sink(domaincopilot.WorkbenchStreamEvent{
				Type:         "source.updated",
				RunID:        artifact.RunID,
				MessageID:    messageID,
				CreatedAt:    createdAt,
				ProviderID:   agentProviderInternal,
				ProviderKind: "internal",
				Source: &domaincopilot.WorkbenchSource{
					ID:      evidence.ID,
					Kind:    streamSourceKindFromEvidence(evidence.Kind),
					Title:   evidence.Title,
					Summary: evidence.Summary,
				},
			})
		}
		sink(domaincopilot.WorkbenchStreamEvent{
			Type:         "artifact.updated",
			RunID:        artifact.RunID,
			MessageID:    messageID,
			CreatedAt:    createdAt,
			ProviderID:   agentProviderInternal,
			ProviderKind: "internal",
			Artifact:     artifact,
		})
	}
}

func streamAnalysisThinkingDelta(mode string) string {
	switch normalizeSessionMode(mode) {
	case "performance":
		return "Preparing performance analysis."
	case "trace":
		return "Preparing trace analysis."
	case "inspection_review":
		return "Preparing inspection review."
	default:
		return "Preparing root cause analysis."
	}
}

func failedAnalysisToolExecution(mode string, scope domaincopilot.SessionScope, toolset domaincopilot.SessionToolset, prompt string, err error) domaincopilot.ToolExecution {
	now := time.Now().UTC()
	adapterID, toolName := analysisToolIdentity(mode)
	return domaincopilot.ToolExecution{
		ID:        "tool:" + uuid.NewString(),
		AdapterID: adapterID,
		ToolName:  toolName,
		Status:    "failed",
		Summary:   err.Error(),
		Input: map[string]any{
			"prompt":          prompt,
			"scope":           scope,
			"budgetOverrides": toolset.BudgetOverrides,
		},
		Output:      map[string]any{"error": err.Error()},
		StartedAt:   now,
		CompletedAt: &now,
	}
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
