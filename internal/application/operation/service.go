package operation

import (
	"context"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
)

type Service struct {
	repo        domainoperation.Repository
	permissions *appaccess.PermissionResolver
}

func New(repo domainoperation.Repository, permissions *appaccess.PermissionResolver) *Service {
	return &Service{repo: repo, permissions: permissions}
}

func (s *Service) Record(ctx context.Context, entry domainoperation.Entry) error {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.TargetScope == nil {
		entry.TargetScope = map[string]any{}
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	return s.repo.Create(ctx, entry)
}

func (s *Service) List(ctx context.Context, filter domainoperation.Filter) ([]domainoperation.Entry, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	return s.repo.List(ctx, filter)
}

func (s *Service) ListAuthorized(ctx context.Context, principal domainidentity.Principal, filter domainoperation.Filter) ([]domainoperation.Entry, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSystemOperationsView); err != nil {
		return nil, err
	}
	return s.List(ctx, filter)
}
