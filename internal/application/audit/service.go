package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainaudit "github.com/kubecrux/kubecrux/internal/domain/audit"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
)

type Service struct {
	repo        domainaudit.Repository
	permissions *appaccess.PermissionResolver
}

func New(repo domainaudit.Repository, permissions *appaccess.PermissionResolver) *Service {
	return &Service{repo: repo, permissions: permissions}
}

func (s *Service) Record(ctx context.Context, entry domainaudit.Entry) error {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.Roles == nil {
		entry.Roles = []string{}
	}
	if entry.Teams == nil {
		entry.Teams = []string{}
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	return s.repo.Create(ctx, entry)
}

func (s *Service) List(ctx context.Context, filter domainaudit.Filter) ([]domainaudit.Entry, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	return s.repo.List(ctx, filter)
}

func (s *Service) ListAuthorized(ctx context.Context, principal domainidentity.Principal, filter domainaudit.Filter) ([]domainaudit.Entry, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSystemAuditView); err != nil {
		return nil, err
	}
	return s.List(ctx, filter)
}
