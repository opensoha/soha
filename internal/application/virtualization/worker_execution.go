package virtualization

import (
	"context"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
)

func isWorkerCreation(task domain.Task) bool {
	return payloadString(task.Payload, "workerPoolId") != ""
}

func (s *Service) resetUnstartedWorkerBootstrap(ctx context.Context, task *domain.Task) error {
	if !isWorkerCreation(*task) || boolValue(task.Payload, "providerDispatchStarted") || payloadString(task.Result, "providerEffect") != "not_started" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.revokeWorkerBootstrap(ctx, *task); err != nil {
		return err
	}
	// Only an explicit retry with a proven absence of VM dispatch can issue a
	// replacement bootstrap. A dispatched or unknown VM keeps its original token.
	task.Payload = cloneMap(task.Payload)
	for _, key := range []string{"workerBootstrapDeadline", "workerBootstrapSecretUid", "workerBootstrapExpiresAt", "cloudInitCredential", "cloudInitConfigured"} {
		delete(task.Payload, key)
	}
	delete(task.Result, "workerBootstrapRevoked")
	return nil
}

func (s *Service) prepareWorkerBootstrapTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	if !isWorkerCreation(task) {
		return task, nil
	}
	pool, err := workerPoolFromTask(task)
	if err != nil {
		return task, err
	}
	if err := s.checkWorkerPoolEnabled(ctx, pool); err != nil {
		return task, err
	}
	if err := s.checkWorkerPoolImage(ctx, pool); err != nil {
		return task, err
	}
	if err := s.authorizeQueuedVMTask(ctx, task); err != nil {
		return task, err
	}
	if payloadString(task.Payload, "workerBootstrapSecretUid") != "" {
		expires, err := time.Parse(time.RFC3339Nano, payloadString(task.Payload, "workerBootstrapExpiresAt"))
		if err != nil || !expires.After(time.Now()) || boolValue(task.Result, "workerBootstrapRevoked") {
			return task, fmt.Errorf("%w: original worker bootstrap expired or was revoked; inspect the retained VM", apperrors.ErrConflict)
		}
		return task, nil
	}
	deadline, err := time.Parse(time.RFC3339Nano, payloadString(task.Payload, "workerBootstrapDeadline"))
	if err != nil {
		deadline = time.Now().UTC().Add(15 * time.Minute)
		if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
			deadline = limit
		}
		task.Payload = cloneMap(task.Payload)
		task.Payload["workerBootstrapDeadline"] = deadline.Format(time.RFC3339Nano)
		task, err = s.tasks.UpdateTask(ctx, task)
		if err != nil {
			return task, err
		}
	}
	bootstrapCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	bootstrap, err := s.workerRuntime.PrepareBootstrap(bootstrapCtx, pool, task.ID, domain.WorkerNodeName(task.ID), deadline)
	if err != nil {
		return task, err
	}
	// Keep the returned UID in the durable task before any VM write. If the
	// checkpoint loses a cancellation race, revoke the exact just-created token.
	saved, err := s.persistWorkerBootstrap(ctx, task, bootstrap, deadline)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = s.workerRuntime.RevokeBootstrap(cleanupCtx, pool, task.ID, bootstrap.SecretUID)
		return task, err
	}
	return saved, nil
}

func (s *Service) persistWorkerBootstrap(ctx context.Context, task domain.Task, bootstrap domain.WorkerBootstrap, deadline time.Time) (domain.Task, error) {
	current, err := s.tasks.GetTask(ctx, task.ID)
	if err != nil {
		return task, err
	}
	if current.Status != TaskStatusRunning || current.ClaimedByWorkerID != task.ClaimedByWorkerID || current.AttemptCount != task.AttemptCount || bootstrap.SecretUID == "" || bootstrap.CloudInit == "" || bootstrap.ExpiresAt.After(deadline.Add(time.Second)) || !bootstrap.ExpiresAt.After(time.Now()) {
		return task, apperrors.ErrConflict
	}
	current.Payload = cloneMap(current.Payload)
	if err := s.sealVMBootstrap(current.Payload, bootstrap.CloudInit); err != nil {
		return task, err
	}
	current.Payload["workerBootstrapSecretUid"] = bootstrap.SecretUID
	current.Payload["workerBootstrapExpiresAt"] = bootstrap.ExpiresAt.UTC().Format(time.RFC3339Nano)
	return s.tasks.UpdateTask(ctx, current)
}

// The original VM queue owns readiness. A retry with a confirmed VM receipt
// continues these observations; it never provisions another VM or bootstrap.
func (s *Service) executeWorkerReadiness(ctx context.Context, task domain.Task) (string, error) {
	pool, err := workerPoolFromTask(task)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	for {
		current, err := s.tasks.GetTask(ctx, task.ID)
		if err == nil && (current.AttemptCount != task.AttemptCount || current.ClaimedByWorkerID != task.ClaimedByWorkerID) {
			return runtimeobs.OutcomeFailed, apperrors.ErrConflict
		}
		if err != nil || ctx.Err() != nil {
			if err == nil {
				err = ctx.Err()
			}
			s.failTask(ctx, task, err)
			return runtimeobs.OutcomeFailed, err
		}
		task = current
		if task.Status == TaskStatusCanceled || task.Status == TaskStatusCanceling {
			return runtimeobs.OutcomeCanceled, nil
		}
		if task.Status != TaskStatusRunning {
			return runtimeobs.OutcomeFailed, apperrors.ErrConflict
		}
		ready, err := s.observeAndClaimWorker(ctx, pool, task)
		if err != nil {
			s.failTask(ctx, task, err)
			return runtimeobs.OutcomeFailed, err
		}
		task.Result = mergeMaps(task.Result, map[string]any{"workerReady": ready.Ready, "workerNodeName": ready.NodeName, "workerNodeUid": ready.NodeUID, "workerObservedAt": ready.ObservedAt, "workerValidUntil": ready.ValidUntil, "message": ready.Reason})
		now := time.Now().UTC()
		task.LastHeartbeatAt = &now
		if ready.Ready {
			if err := s.revokeWorkerBootstrap(ctx, task); err != nil {
				s.failTask(ctx, task, err)
				return runtimeobs.OutcomeFailed, err
			}
			task.Status, task.FinishedAt = TaskStatusSucceeded, &now
			task.Result["workerBootstrapRevoked"] = true
		}
		task, err = s.tasks.UpdateTask(ctx, task)
		if err != nil {
			return runtimeobs.OutcomeFailed, err
		}
		if ready.Ready {
			return runtimeobs.OutcomeSucceeded, nil
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *Service) observeAndClaimWorker(ctx context.Context, pool domain.WorkerPool, task domain.Task) (domain.WorkerObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.authorizeQueuedVMTask(ctx, task); err != nil {
		return domain.WorkerObservation{}, err
	}
	if err := s.checkWorkerPoolEnabled(ctx, pool); err != nil {
		return domain.WorkerObservation{}, err
	}
	observed, err := s.workerRuntime.Observe(ctx, pool, task.ID)
	if err != nil || observed.NodeUID == "" {
		return observed, err
	}
	if err := s.workerRuntime.ClaimNode(ctx, pool, task.ID); err != nil {
		return observed, err
	}
	return s.workerRuntime.Observe(ctx, pool, task.ID)
}

func (s *Service) revokeWorkerBootstrap(ctx context.Context, task domain.Task) error {
	if !isWorkerCreation(task) || boolValue(task.Result, "workerBootstrapRevoked") {
		return nil
	}
	pool, err := workerPoolFromTask(task)
	if err != nil {
		return err
	}
	if s.workerRuntime == nil || s.workerRuntime.RevokeBootstrap == nil {
		return apperrors.ErrUnsupportedOperation
	}
	uid := payloadString(task.Payload, "workerBootstrapSecretUid")
	if uid == "" && payloadString(task.Payload, "workerBootstrapDeadline") == "" {
		return nil
	}
	return s.workerRuntime.RevokeBootstrap(ctx, pool, task.ID, uid)
}

func (s *Service) finishWorkerBootstrap(ctx context.Context, original domain.Task) {
	if !isWorkerCreation(original) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	task, err := s.tasks.GetTask(ctx, original.ID)
	if err != nil || task.AttemptCount != original.AttemptCount || task.ClaimedByWorkerID != original.ClaimedByWorkerID {
		return
	}
	if err := s.revokeWorkerBootstrap(ctx, task); err != nil {
		return
	}
	task.Result = mergeMaps(task.Result, map[string]any{"workerBootstrapRevoked": true})
	if task.Status == TaskStatusCanceling && (!boolValue(task.Payload, "providerDispatchStarted") || payloadString(task.Result, "providerEffect") == "created") {
		now := time.Now().UTC()
		task.Status, task.FinishedAt = TaskStatusCanceled, &now
		task.Result["cancellationConfirmed"] = true
	}
	_, _ = s.tasks.UpdateTask(ctx, task)
}
