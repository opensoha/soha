package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type chatContextLineage struct {
	RequestMessageID  string                 `json:"requestMessageId,omitempty"`
	HistoryMessageIDs []string               `json:"historyMessageIds,omitempty"`
	ExcludedMessages  []chatContextExclusion `json:"excludedMessages,omitempty"`
	Summary           *chatSummarySelection  `json:"summary,omitempty"`
	HistoryWindow     *chatHistoryWindow     `json:"historyWindow,omitempty"`
}

type chatContextExclusion struct {
	MessageID string `json:"messageId"`
	Reason    string `json:"reason"`
}

type chatSummarySelection struct {
	SourceMessageIDs []string `json:"sourceMessageIds,omitempty"`
	Included         bool     `json:"included"`
	Reason           string   `json:"reason,omitempty"`
}

type chatHistoryWindow struct {
	Scope  string    `json:"scope"`
	Limit  int       `json:"limit"`
	Loaded int       `json:"loaded"`
	Before time.Time `json:"before"`
}

func (s *Service) groundedProviderMessages(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, metadata domaincopilot.SessionMetadata, history []domaincopilot.Message, current domaincopilot.Message, locale string) ([]chatProviderMessage, map[string]any, error) {
	messages, snapshot, _, err := s.groundedProviderContext(ctx, principal, session, metadata, history, current, locale)
	return messages, snapshot, err
}

func (s *Service) groundedProviderContext(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, metadata domaincopilot.SessionMetadata, history []domaincopilot.Message, current domaincopilot.Message, locale string) ([]chatProviderMessage, map[string]any, chatContextLineage, error) {
	history, excluded, sources := s.groundedHistorySelections(ctx, principal, history)
	messages, lineage := buildProviderChatMessages(history, current, locale)
	lineage.ExcludedMessages = append(excluded, lineage.ExcludedMessages...)
	envelope, err := s.buildChatContext(ctx, principal, session.ID, metadata, current.Content)
	if err != nil {
		return messages, nil, lineage, err
	}
	inherited := []domainknowledge.Citation{}
	for _, message := range history {
		if chatHistoryExclusionReason(message, current.ID) == "" {
			inherited = append(inherited, sources[message.ID]...)
		}
	}
	envelope, err = inheritChatHistoryCitations(ctx, principal, session.ID, current.Content, envelope, inherited)
	if err != nil || envelope == nil {
		return messages, nil, lineage, err
	}
	if len(envelope.Evidence) > 0 || envelope.Session.Summary != "" {
		messages = append([]chatProviderMessage{contextEvidenceSystemMessage(*envelope)}, messages...)
	}
	lineage.Summary = chatSummarySources(metadata, *envelope)
	return messages, contextSnapshot(*envelope), lineage, nil
}

// Preserve source withdrawal checks on answers derived from authorized history,
// including turns which do not select new knowledge or supply a summary.
func inheritChatHistoryCitations(ctx context.Context, principal domainidentity.Principal, sessionID, goal string, envelope *domaincopilot.ContextEnvelope, inherited []domainknowledge.Citation) (*domaincopilot.ContextEnvelope, error) {
	if len(inherited) == 0 {
		return envelope, nil
	}
	if envelope == nil {
		seed, err := NewContextBuilder(nil, nil).BuildForCopilot(ctx, principal, domaincopilot.ContextBuildInput{SessionID: sessionID, Task: domaincopilot.ContextTask{Mode: "general", Goal: goal}})
		if err != nil {
			return nil, err
		}
		envelope = &seed
	}
	seen := make(map[string]bool, len(envelope.Citations))
	for _, citation := range envelope.Citations {
		seen[knowledgeCitationKey(citation)] = true
	}
	for _, citation := range inherited {
		key := knowledgeCitationKey(citation)
		if !seen[key] {
			envelope.Citations = append(envelope.Citations, citation)
			seen[key] = true
		}
	}
	if len(envelope.Citations) > 200 {
		return nil, fmt.Errorf("%w: historical citation budget exceeded", apperrors.ErrInvalidArgument)
	}
	if err := sealContextEnvelope(envelope); err != nil {
		return nil, err
	}
	return envelope, nil
}

func chatSummarySources(metadata domaincopilot.SessionMetadata, envelope domaincopilot.ContextEnvelope) *chatSummarySelection {
	if strings.TrimSpace(metadata.Summary) == "" {
		return nil
	}
	selection := &chatSummarySelection{}
	ids := agentPrincipalStringList(metadata.PinnedContext["summarySourceMessageIds"])
	if len(ids) > 0 && len(ids) <= 20 {
		selection.SourceMessageIDs = ids
	}
	uri := "session-summary:" + envelope.SessionID
	for _, citation := range envelope.Citations {
		if citation.URI == uri && slices.ContainsFunc(envelope.Evidence, func(evidence domaincopilot.ContextEvidence) bool { return evidence.CitationID == citation.ID }) {
			selection.Included = true
		}
	}
	for _, reason := range envelope.Truncations {
		if strings.HasPrefix(reason, "session.summary:") || strings.HasPrefix(reason, uri+":") {
			selection.Reason = reason
		}
	}
	if !selection.Included && selection.Reason == "" {
		selection.Reason = "session.summary:not_supplied"
	}
	return selection
}

func (s *Service) buildChatContext(ctx context.Context, principal domainidentity.Principal, sessionID string, metadata domaincopilot.SessionMetadata, content string) (*domaincopilot.ContextEnvelope, error) {
	summary, summaryLineage, summaryReason := s.currentChatSummary(ctx, principal, sessionID, metadata)
	metadata.Summary = summary
	metadata, err := withChatBranchReference(metadata)
	if err != nil {
		return nil, err
	}
	config := metadata.KnowledgeContext
	if !config.Enabled && metadata.RequestContext == nil && summary == "" && summaryReason == "" {
		return nil, nil
	}
	if config.Enabled && (len(config.KnowledgeBaseIDs) == 0 || len(config.KnowledgeBaseIDs) > 20) {
		return nil, fmt.Errorf("%w: select 1 to 20 knowledge bases", apperrors.ErrInvalidArgument)
	}
	builder := s.contextBuilder
	if config.Enabled && builder == nil {
		return nil, fmt.Errorf("%w: knowledge context is unavailable", apperrors.ErrUnsupportedOperation)
	}
	if builder == nil {
		builder = NewContextBuilder(nil, nil)
	}
	knowledgeEnabled := config.Enabled
	if config.Enabled && metadata.Mode == "general" {
		reader, ok := builder.(interface {
			AuthorizeKnowledgeBases(context.Context, domainidentity.Principal, []string) error
		})
		if !ok {
			return nil, fmt.Errorf("%w: knowledge authorization is unavailable", apperrors.ErrUnsupportedOperation)
		}
		if err := reader.AuthorizeKnowledgeBases(ctx, principal, config.KnowledgeBaseIDs); err != nil {
			return nil, err
		}
		knowledgeEnabled = false
	}
	envelope, err := builder.BuildForCopilot(ctx, principal, domaincopilot.ContextBuildInput{
		SessionID: sessionID,
		Task:      domaincopilot.ContextTask{Mode: metadata.Mode, Goal: strings.TrimSpace(content)},
		Session:   domaincopilot.ContextSession{Summary: metadata.Summary},
		Knowledge: domaincopilot.ContextKnowledgeInput{Enabled: knowledgeEnabled, KnowledgeBaseIDs: config.KnowledgeBaseIDs, Query: content, TopK: config.TopK},
		Budgets:   domaincopilot.ContextBudgets{MaxInputTokens: 16000, MaxEvidenceTokens: 6000, MaxSteps: 8},
	})
	if err != nil {
		return nil, err
	}
	envelope.Citations = append(envelope.Citations, summaryLineage...)
	if summaryReason != "" {
		envelope.Truncations = append(envelope.Truncations, summaryReason)
	}
	if err := s.appendChatReferences(ctx, principal, &envelope, metadata.RequestContext); err != nil {
		return nil, err
	}
	if err := sealContextEnvelope(&envelope); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func withChatBranchReference(metadata domaincopilot.SessionMetadata) (domaincopilot.SessionMetadata, error) {
	if value, ok := metadata.PinnedContext["branchReference"]; ok {
		var reference domaincopilot.ContextReference
		data, err := json.Marshal(value)
		if err != nil || json.Unmarshal(data, &reference) != nil || reference.Kind != "session" || reference.SessionID == "" || len(reference.MessageIDs) == 0 || len(reference.MessageIDs) > 20 {
			return metadata, fmt.Errorf("%w: invalid branch reference", apperrors.ErrInvalidArgument)
		}
		selection := domaincopilot.ContextSelection{}
		if metadata.RequestContext != nil {
			selection = *metadata.RequestContext
		}
		selection.References = append(append([]domaincopilot.ContextReference{}, selection.References...), reference)
		metadata.RequestContext = &selection
	}
	return metadata, nil
}
