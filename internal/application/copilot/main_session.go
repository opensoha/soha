package copilot

import (
	"context"
	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"time"
)

func (s *Service) EnsureMainSession(ctx context.Context, principal domainidentity.Principal, agentProviderID, locale string) (domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.Session{}, err
	}
	if len(strings.TrimSpace(agentProviderID)) > 200 {
		return domaincopilot.Session{}, apperrors.ErrInvalidArgument
	}
	metadata := sessionMetadataMap(domaincopilot.SessionMetadata{
		ConversationKind: "main", Mode: "general", Status: "active",
		AgentProviderID: normalizeAgentProviderID(agentProviderID), Source: "workbench",
	})
	metadata["locale"] = normalizeLocale(locale)
	now := time.Now().UTC()
	session, created, err := s.sessions.GetOrCreateMainSession(ctx, domaincopilot.Session{
		ID: uuid.NewString(), Title: localize(locale, "主聊天", "Main chat"), CreatedBy: principal.UserID,
		Metadata: metadata, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return domaincopilot.Session{}, err
	}
	if created {
		s.recordGlobalAssistantAudit(ctx, principal, domaincopilot.WorkbenchGlobalAssistantEventInput{
			Action: "open", SessionID: session.ID, Source: "workbench",
		}, parseSessionMetadata(session.Metadata), "success")
	}
	return s.visibleSessionSummary(ctx, principal, session), nil
}

func mainChatProtected() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "main_chat_protected", "The main chat cannot be archived or deleted.", "主聊天不能归档或删除。")
}

func (s *Service) ensurePersonalMainSession(ctx context.Context, principal domainidentity.Principal, locale string) (domaincopilot.Session, error) {
	now := time.Now().UTC()
	metadata := sessionMetadataMap(domaincopilot.SessionMetadata{ConversationKind: "personal_main", Mode: "general", Status: "active", AgentProviderID: "internal", Source: "workbench"})
	metadata["locale"] = normalizeLocale(locale)
	session, _, err := s.sessions.GetOrCreateMainSession(ctx, domaincopilot.Session{ID: uuid.NewString(), Title: localize(locale, "主要聊天", "Main chat"), CreatedBy: principal.UserID, Metadata: metadata, CreatedAt: now, UpdatedAt: now})
	return session, err
}

func (s *Service) ListPersonalSessions(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.Session, error) {
	if err := s.requirePersonalAgent(ctx, principal); err != nil {
		return nil, err
	}
	store, ok := s.sessions.(interface {
		ListPersonalSessions(context.Context, string, int) ([]domaincopilot.Session, error)
	})
	if !ok {
		return nil, apperrors.ErrServiceUnavailable
	}
	items, err := store.ListPersonalSessions(ctx, principal.UserID, 20)
	if err != nil {
		return nil, err
	}
	items = s.sessionActivities(ctx, principal, items)
	for i := range items {
		items[i] = s.visibleSessionSummary(ctx, principal, items[i])
	}
	return items, nil
}
