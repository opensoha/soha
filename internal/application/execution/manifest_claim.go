package execution

import (
	"context"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) hydrateExecutionClaim(ctx context.Context, task dd.ExecutionTask) (dd.ExecutionTask, error) {
	task.ManifestAdmissionValidated = false
	if s.payloadHydrator == nil {
		if dd.RequiresManifestAdmission(task) {
			return task, apperrors.ErrUnsupportedOperation
		}
		return task, nil
	}
	hydrated, err := s.payloadHydrator.HydrateExecutionTask(ctx, task)
	if err == nil && dd.RequiresManifestAdmission(task) && !hydrated.ManifestAdmissionValidated {
		return task, apperrors.ErrUnsupportedOperation
	}
	return hydrated, err
}
