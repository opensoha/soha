package manifest

import (
	"context"
	"errors"
	"fmt"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

func localManifestTaskStopped(current, claimed domaindelivery.ExecutionTask) bool {
	return current.ID != claimed.ID || current.CallbackToken != claimed.CallbackToken ||
		(current.Status != "running" && current.Status != "dispatching")
}

func (s *DeclarativeService) runLocalManifestTask(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload, runtime ManifestRuntime) (domainmanifest.TaskResult, error) {
	runCtx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				current, err := s.tasks.GetExecutionTaskInternal(runCtx, task.ID)
				if err != nil {
					cancel(fmt.Errorf("read manifest task state: %w", err))
					return
				}
				if localManifestTaskStopped(current, task) {
					cancel(context.Canceled)
					return
				}
			}
		}
	}()
	defer func() { cancel(context.Canceled); <-done }()
	result, err := runtime.Execute(runCtx, payload)
	if cause := context.Cause(runCtx); cause != nil && !errors.Is(err, resourceruntime.ErrArgoStopUnconfirmed) && !errors.Is(err, resourceruntime.ErrRolloutStopUnconfirmed) {
		return result, cause
	}
	return result, err
}
