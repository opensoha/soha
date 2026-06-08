package event

import (
	"context"

	domainevent "github.com/opensoha/soha/internal/domain/event"
)

type Repository interface {
	List(context.Context, int) ([]domainevent.Envelope, error)
	Get(context.Context, string) (domainevent.Envelope, error)
}

type Service struct {
	repo Repository
}

func New(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, limit int) ([]domainevent.Envelope, error) {
	if s.repo == nil {
		return []domainevent.Envelope{}, nil
	}
	return s.repo.List(ctx, limit)
}

func (s *Service) Get(ctx context.Context, eventID string) (domainevent.Envelope, error) {
	if s.repo == nil {
		return domainevent.Envelope{}, nil
	}
	return s.repo.Get(ctx, eventID)
}
