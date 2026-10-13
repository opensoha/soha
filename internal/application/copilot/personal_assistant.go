package copilot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func (s *Service) EnsurePersonalAssistant(ctx context.Context, principal domainidentity.Principal, locale string) (domain.PersonalAssistant, error) {
	if err := s.requirePersonalAgent(ctx, principal); err != nil {
		return domain.PersonalAssistant{}, err
	}
	main, err := s.ensurePersonalMainSession(ctx, principal, locale)
	if err != nil {
		return domain.PersonalAssistant{}, err
	}
	now := time.Now().UTC()
	return s.sessions.GetOrCreatePersonalAssistant(ctx, domain.PersonalAssistant{ID: uuid.NewString(), CreatedBy: principal.UserID, Name: "heya", MainSessionID: main.ID, CreatedAt: now, UpdatedAt: now})
}

func (s *Service) ListPersonalAssistantArtifacts(ctx context.Context, principal domainidentity.Principal) ([]domain.PersonalArtifact, error) {
	if strings.TrimSpace(principal.UserID) == "" {
		return nil, apperrors.ErrAccessDenied
	}
	if err := s.requirePersonalAgent(ctx, principal); err != nil {
		return nil, err
	}
	items, err := s.messages.ListPersonalArtifacts(ctx, principal.UserID)
	if err != nil {
		return nil, err
	}
	runs, err := s.ListPersonalAssistantTasks(ctx, principal)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(items))
	deduplicated := items[:0]
	for _, item := range items {
		if _, err := s.validateMessageKnowledge(ctx, principal, domain.Message{Metadata: item.SourceMetadata}); err != nil {
			continue
		}
		if !seen[item.ID] {
			seen[item.ID] = true
			deduplicated = append(deduplicated, item)
		}
	}
	items = deduplicated
	for _, run := range runs {
		if run.ParentRunID != "" {
			continue
		}
		for index, artifact := range run.AnalysisArtifacts {
			id := fmt.Sprintf("task:%s:%d", run.ID, index)
			if seen[id] {
				continue
			}
			seen[id] = true
			createdAt := run.CreatedAt
			if run.CompletedAt != nil {
				createdAt = *run.CompletedAt
			}
			items = append(items, domain.PersonalArtifact{ID: id, TaskID: run.ID, SessionID: run.SessionID, Artifact: artifact, CreatedAt: createdAt})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > 50 {
		items = items[:50]
	}
	if items == nil {
		items = []domain.PersonalArtifact{}
	}
	return items, nil
}

func (s *Service) RenamePersonalAssistant(ctx context.Context, principal domainidentity.Principal, name, locale string) (domain.PersonalAssistant, error) {
	if err := s.requirePersonalAgent(ctx, principal); err != nil {
		return domain.PersonalAssistant{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 60 || strings.ContainsFunc(name, unicode.IsControl) {
		return domain.PersonalAssistant{}, apperrors.ErrInvalidArgument
	}
	if _, err := s.EnsurePersonalAssistant(ctx, principal, locale); err != nil {
		return domain.PersonalAssistant{}, err
	}
	item, err := s.sessions.RenamePersonalAssistant(ctx, principal.UserID, name)
	if err == nil && s.audits != nil {
		meta := requestctx.FromContext(ctx)
		_ = s.audits.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, ResourceKind: "PersonalAssistant", ResourceName: item.ID, Action: "personal_assistant.rename", Result: "success", Summary: "Updated personal assistant name", RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP})
	}
	return item, err
}

func (s *Service) ListPersonalAssistantTasks(ctx context.Context, principal domainidentity.Principal) ([]domain.AgentRun, error) {
	if strings.TrimSpace(principal.UserID) == "" {
		return nil, apperrors.ErrAccessDenied
	}
	if err := s.requirePersonalAgent(ctx, principal); err != nil {
		return nil, err
	}
	items, err := s.agentRuns.ListAgentRuns(ctx, domain.AgentRunFilter{CreatedBy: principal.UserID, Limit: 50, ExcludeArchivedSessions: true, PersonalSessionsOnly: true})
	if err != nil {
		return nil, err
	}
	filtered := items[:0]
	for _, run := range items {
		session, err := s.sessions.GetSession(ctx, principal.UserID, run.SessionID)
		if err == nil && isPersonalSession(session) {
			filtered = append(filtered, run)
		}
	}
	items = filtered
	for index := range items {
		items[index].CallbackToken = ""
		items[index] = s.visibleKnowledgeAgentRun(ctx, principal, items[index])
	}
	return domain.WithOperationStates(items, time.Now().UTC()), nil
}
