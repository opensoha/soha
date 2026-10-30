package delivery

import (
	"context"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
)

func (s *Service) hydrateManifestExecutionTask(ctx context.Context, task dd.ExecutionTask) (dd.ExecutionTask, error) {
	task.ManifestAdmissionValidated = false
	protected := dd.RequiresManifestAdmission(task)
	if protected && !strings.HasPrefix(task.TaskKind, "manifest_") {
		return task, apperrors.ErrUnsupportedOperation
	}
	var err error
	if task.Payload["rootResource"] != nil {
		task, err = s.hydrateRootCRTask(ctx, task)
	} else if validator, ok := s.manifestDelivery.(rootCRTaskValidator); ok {
		err = validator.ValidateRootCRExecutionTask(ctx, task)
	} else if protected {
		err = apperrors.ErrUnsupportedOperation
	}
	if err == nil && protected {
		task.ManifestAdmissionValidated = true
	}
	return task, err
}
