package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	domainaudit "github.com/kubecrux/kubecrux/internal/domain/audit"
)

type Service struct {
	repo domainaudit.Repository
}

func New(repo domainaudit.Repository) *Service {
	return &Service{repo: repo}
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
