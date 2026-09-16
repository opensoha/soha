package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) groundedProviderMessages(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, metadata domaincopilot.SessionMetadata, history []domaincopilot.Message, current domaincopilot.Message, locale string) ([]chatProviderMessage, map[string]any, error) {
	messages := buildProviderChatMessages(history, current, locale)
	envelope, err := s.buildChatContext(ctx, principal, session.ID, metadata, current.Content)
	if err != nil || envelope == nil {
		return messages, nil, err
	}
	if len(envelope.Evidence) > 0 {
		messages = append([]chatProviderMessage{contextEvidenceSystemMessage(*envelope)}, messages...)
	}
	return messages, contextSnapshot(*envelope), nil
}

func (s *Service) buildChatContext(ctx context.Context, principal domainidentity.Principal, sessionID string, metadata domaincopilot.SessionMetadata, content string) (*domaincopilot.ContextEnvelope, error) {
	metadata, err := withChatBranchReference(metadata)
	if err != nil {
		return nil, err
	}
	config := metadata.KnowledgeContext
	if !config.Enabled && metadata.RequestContext == nil {
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
	if err := s.appendChatReferences(ctx, principal, &envelope, metadata.RequestContext); err != nil {
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
