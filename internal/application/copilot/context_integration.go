package copilot

import (
	"context"
	"strings"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func (s *Service) groundedProviderMessages(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, metadata domaincopilot.SessionMetadata, history []domaincopilot.Message, current domaincopilot.Message, locale string) ([]chatProviderMessage, map[string]any, error) {
	messages := buildProviderChatMessages(history, current, locale)
	config := metadata.KnowledgeContext
	if !config.Enabled || len(config.KnowledgeBaseIDs) == 0 || s.contextBuilder == nil {
		return messages, nil, nil
	}
	envelope, err := s.contextBuilder.BuildForCopilot(ctx, principal, domaincopilot.ContextBuildInput{
		SessionID: session.ID,
		Task:      domaincopilot.ContextTask{Mode: metadata.Mode, Goal: strings.TrimSpace(current.Content)},
		Session:   domaincopilot.ContextSession{Summary: metadata.Summary},
		Knowledge: domaincopilot.ContextKnowledgeInput{Enabled: true, KnowledgeBaseIDs: config.KnowledgeBaseIDs, Query: current.Content, TopK: config.TopK},
		Budgets:   domaincopilot.ContextBudgets{MaxInputTokens: 16000, MaxEvidenceTokens: 6000, MaxSteps: 8},
	})
	if err != nil {
		return nil, nil, err
	}
	if len(envelope.Evidence) == 0 {
		return messages, contextSnapshot(envelope), nil
	}
	messages = append([]chatProviderMessage{contextEvidenceSystemMessage(envelope)}, messages...)
	return messages, contextSnapshot(envelope), nil
}
