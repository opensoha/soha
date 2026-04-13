package scopegrant

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domainscopegrant "github.com/kubecrux/kubecrux/internal/domain/scopegrant"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	scopegrantrepo "github.com/kubecrux/kubecrux/internal/repository/scopegrant"
)

type Service struct {
	repo domainscopegrant.Repository
}

func New(repo domainscopegrant.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context) ([]domainscopegrant.Record, error) {
	return s.repo.List(ctx)
}

func (s *Service) Create(ctx context.Context, input domainscopegrant.Input) (domainscopegrant.Record, error) {
	if err := validateInput(input); err != nil {
		return domainscopegrant.Record{}, err
	}
	return s.repo.Create(ctx, input)
}

func (s *Service) Update(ctx context.Context, id string, input domainscopegrant.Input) (domainscopegrant.Record, error) {
	if err := validateInput(input); err != nil {
		return domainscopegrant.Record{}, err
	}
	item, err := s.repo.Update(ctx, id, input)
	return item, normalizeRepoError(err)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return normalizeRepoError(s.repo.Delete(ctx, id))
}

func validateInput(input domainscopegrant.Input) error {
	if strings.TrimSpace(input.SubjectType) == "" || strings.TrimSpace(input.SubjectID) == "" {
		return fmt.Errorf("%w: subjectType and subjectId are required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.BusinessLineID) == "" {
		return fmt.Errorf("%w: businessLineId is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Role) == "" {
		return fmt.Errorf("%w: role is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func normalizeRepoError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, scopegrantrepo.ErrNotFound) {
		return fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
	}
	return err
}
