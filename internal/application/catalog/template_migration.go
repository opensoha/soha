package catalog

import (
	"context"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// ServiceTemplateMigrationRequired supports internal source admission only.
// Callers retain their existing user/scope checks; this boolean grants nothing.
func (s *Service) ServiceTemplateMigrationRequired(ctx context.Context, applicationID, serviceID string) (bool, error) {
	reader, ok := s.repo.(interface {
		ServiceTemplateMigrationRequired(context.Context, string, string) (bool, error)
	})
	if !ok {
		return false, apperrors.ErrUnsupportedOperation
	}
	return reader.ServiceTemplateMigrationRequired(ctx, applicationID, serviceID)
}
