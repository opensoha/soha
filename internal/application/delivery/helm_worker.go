package delivery

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

func (s *Service) StartHelmDelivery(ctx context.Context) {
	if s.helm.Runtime == nil || s.execution == nil {
		return
	}
	// ponytail: four workers bound native Helm work; use a configured pool if
	// measured queue latency requires more concurrency.
	for range 4 {
		go s.helmWorkerLoop(ctx)
	}
	go s.helmRecoveryLoop(ctx)
}

func (s *Service) helmWorkerLoop(ctx context.Context) {
	owner := "soha-helm-" + uuid.NewString()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			task, err := s.ClaimExecutionTask(ctx, []string{"helm_direct"}, owner, "local")
			if err == nil {
				s.executeLocalHelmTask(ctx, task)
			}
		}
	}
}

func (s *Service) executeLocalHelmTask(ctx context.Context, task domaindelivery.ExecutionTask) {
	payload, err := decodeHelmPayload(task.Payload)
	result := sohaapi.HelmExecutionTaskResult{Stopped: true}
	if err == nil {
		current, callbackErr := s.RecordCallback(ctx, domaindelivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: "running", Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskResult{}}})
		if callbackErr != nil {
			// No native action has started, so a failed start can confirm stop.
			s.recordHelmCompletion(ctx, task, "failed", result)
			return
		}
		if current.CallbackToken != task.CallbackToken {
			return
		}
		if current.Status == "canceling" {
			s.recordHelmCompletion(ctx, task, "canceled", result)
			return
		}
		if current.Status != "running" {
			return
		}
		result, err = s.runLocalHelmTask(ctx, task, payload)
	}
	status := "completed"
	if err != nil {
		status = "failed"
	}
	if errors.Is(err, context.Canceled) {
		status = "canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		status = "callback_timeout"
	}
	if !result.Stopped {
		return
	}
	s.recordHelmCompletion(ctx, task, status, result)
}

func (s *Service) runLocalHelmTask(ctx context.Context, task domaindelivery.ExecutionTask, payload sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSeconds)*time.Second)
	defer cancel()
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				checkCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				current, err := s.repository.GetExecutionTask(checkCtx, task.ID)
				if err != nil || current.CallbackToken != task.CallbackToken || current.Status != "running" {
					cancel()
				} else {
					_, _ = s.RecordCallback(checkCtx, domaindelivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: "running", Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskResult{}}})
				}
				finish()
			}
		}
	}()
	defer func() { close(done); <-stopped }()
	for {
		result, err := s.helm.Runtime.ExecuteHelmDelivery(runCtx, task.SecretPrincipal, payload)
		if err != nil || payload.Action != sohaapi.Observe || result.Ready {
			if runCtx.Err() != nil {
				err = runCtx.Err()
			}
			return result, err
		}
		select {
		case <-runCtx.Done():
			return result, runCtx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (s *Service) recordHelmCompletion(ctx context.Context, task domaindelivery.ExecutionTask, status string, result sohaapi.HelmExecutionTaskResult) {
	for {
		callbackCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_, err := s.RecordCallback(callbackCtx, domaindelivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: status, Payload: map[string]any{"helm": result}})
		finish()
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (s *Service) helmRecoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, status := range []string{"dispatching", "running", "canceling"} {
				tasks, err := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{TaskKinds: []string{"helm_preflight", "helm_apply", "helm_observe"}, Status: status, Limit: 200})
				if err != nil {
					continue
				}
				for _, task := range tasks {
					if task.LastHeartbeatAt != nil && time.Since(*task.LastHeartbeatAt) < 30*time.Second {
						continue
					}
					s.recoverHelmTask(ctx, task)
				}
			}
		}
	}
}

func (s *Service) recoverHelmTask(ctx context.Context, task domaindelivery.ExecutionTask) {
	payload, err := decodeHelmPayload(task.Payload)
	if err != nil {
		return
	}
	if payload.Action != sohaapi.Apply {
		// Preflight and observation are read-only and may be repeated after a lost
		// callback. Hydration rechecks the current principal and frozen plan.
		if task.Status == "canceling" {
			s.recordHelmCompletion(ctx, task, "canceled", sohaapi.HelmExecutionTaskResult{Stopped: true})
			return
		}
		hydrated, err := s.HydrateExecutionTask(ctx, task)
		if err != nil {
			s.recordHelmCompletion(ctx, task, "failed", sohaapi.HelmExecutionTaskResult{Stopped: true})
			return
		}
		s.executeLocalHelmTask(ctx, hydrated)
		return
	}
	payload.Action = sohaapi.Observe
	observeCtx, finish := context.WithTimeout(ctx, 30*time.Second)
	defer finish()
	result, err := s.helm.Runtime.ExecuteHelmDelivery(observeCtx, task.SecretPrincipal, payload)
	// A deployed native revision is durable evidence that its mutation and hooks
	// finished. Pending/absent/unknown revisions remain unconfirmed after a crash.
	if err != nil || !result.Stopped || result.Status != "deployed" || result.Revision != payload.Snapshot.ExpectedRevision+1 {
		return
	}
	status := "completed"
	if task.Status == "canceling" {
		status = "canceled"
	}
	_, _ = s.RecordCallback(ctx, domaindelivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: status, Payload: map[string]any{"helm": result}})
}
