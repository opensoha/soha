package operation

import (
	"context"

	domainoperation "github.com/kubecrux/kubecrux/internal/domain/operation"
)

type Service struct {
	repo domainoperation.Repository
}

func New(repo domainoperation.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, limit int) ([]domainoperation.Entry, error) {
	return s.repo.List(ctx, limit)
}
