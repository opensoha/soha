package copilot

import (
	"context"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	audit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type PersonalAgentAccessStore interface {
	GetPersonalAgentAccess(context.Context, string) (domain.PersonalAgentAccess, error)
	RequestPersonalAgentAccess(context.Context, string) (domain.PersonalAgentAccess, error)
	ListPersonalAgentAccess(context.Context) ([]domain.PersonalAgentAccess, error)
	DecidePersonalAgentAccess(context.Context, string, string, string, int64) (domain.PersonalAgentAccess, error)
}

func (s *Service) GetPersonalAgentAccess(ctx context.Context, p identity.Principal) (domain.PersonalAgentAccess, error) {
	if strings.TrimSpace(p.UserID) == "" {
		return domain.PersonalAgentAccess{}, apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, p, appaccess.PermObserveAIChatUse); err != nil {
		return domain.PersonalAgentAccess{}, err
	}
	store, ok := s.sessions.(PersonalAgentAccessStore)
	if !ok {
		return domain.PersonalAgentAccess{}, apperrors.ErrServiceUnavailable
	}
	return store.GetPersonalAgentAccess(ctx, p.UserID)
}

func (s *Service) requirePersonalAgent(ctx context.Context, p identity.Principal) error {
	item, err := s.GetPersonalAgentAccess(ctx, p)
	if err != nil {
		return err
	}
	if item.Status != "approved" {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (s *Service) RequestPersonalAgentAccess(ctx context.Context, p identity.Principal) (domain.PersonalAgentAccess, error) {
	if _, err := s.GetPersonalAgentAccess(ctx, p); err != nil {
		return domain.PersonalAgentAccess{}, err
	}
	store, ok := s.sessions.(PersonalAgentAccessStore)
	if !ok {
		return domain.PersonalAgentAccess{}, apperrors.ErrServiceUnavailable
	}
	item, err := store.RequestPersonalAgentAccess(ctx, p.UserID)
	if err == nil {
		s.auditPersonalAgentAccess(ctx, p, item, "request")
	}
	return item, err
}

func (s *Service) ListPersonalAgentAccessRequests(ctx context.Context, p identity.Principal) ([]domain.PersonalAgentAccess, error) {
	if err := s.authorizePrincipal(ctx, p, appaccess.PermAIEnvironmentsUpdate); err != nil {
		return nil, err
	}
	store, ok := s.sessions.(PersonalAgentAccessStore)
	if !ok {
		return nil, apperrors.ErrServiceUnavailable
	}
	return store.ListPersonalAgentAccess(ctx)
}

func (s *Service) DecidePersonalAgentAccess(ctx context.Context, p identity.Principal, owner, status string, version int64) (domain.PersonalAgentAccess, error) {
	if p.UserID == "" {
		return domain.PersonalAgentAccess{}, apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, p, appaccess.PermAIEnvironmentsUpdate); err != nil {
		return domain.PersonalAgentAccess{}, err
	}
	if owner == "" || len(owner) > 128 || strings.TrimSpace(owner) != owner || version < 1 || (status != "approved" && status != "rejected" && status != "revoked") {
		return domain.PersonalAgentAccess{}, apperrors.ErrInvalidArgument
	}
	store, ok := s.sessions.(PersonalAgentAccessStore)
	if !ok {
		return domain.PersonalAgentAccess{}, apperrors.ErrServiceUnavailable
	}
	item, err := store.DecidePersonalAgentAccess(ctx, owner, p.UserID, status, version)
	if err == nil {
		s.auditPersonalAgentAccess(ctx, p, item, "decide")
	}
	return item, err
}

func (s *Service) auditPersonalAgentAccess(ctx context.Context, p identity.Principal, item domain.PersonalAgentAccess, action string) {
	if s.audits == nil {
		return
	}
	meta := requestctx.FromContext(ctx)
	_ = s.audits.Record(ctx, audit.Entry{ActorID: p.UserID, ActorName: p.UserName, ResourceKind: "PersonalAgentAccess", ResourceName: item.UserID, Action: "personal_agent." + action, Result: item.Status, Summary: "Personal Agent activation " + action, RequestID: meta.RequestID, RequestPath: meta.Path, RequestMethod: meta.Method, SourceIP: meta.SourceIP, Metadata: map[string]any{"version": item.Version}})
}

func isPersonalSession(session domain.Session) bool {
	kind := parseSessionMetadata(session.Metadata).ConversationKind
	return kind == "personal_main" || kind == "personal_side"
}

func isMainSessionKind(kind string) bool { return kind == "main" || kind == "personal_main" }

func (s *Service) ownedSession(ctx context.Context, p identity.Principal, id string) (domain.Session, error) {
	item, err := s.sessions.GetSession(ctx, p.UserID, id)
	if err == nil && isPersonalSession(item) {
		err = s.requirePersonalAgent(ctx, p)
	}
	return item, err
}
