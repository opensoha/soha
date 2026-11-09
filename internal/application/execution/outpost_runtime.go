package execution

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// ClaimOutpostRuntimeExecutionTask is reachable only through the dedicated
// installation credential adapter, never generic delivery runner claims.
func (s *Service) ClaimOutpostRuntimeExecutionTask(ctx context.Context, executorID string) (delivery.ExecutionTask, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(executorID) {
		return delivery.ExecutionTask{}, apperrors.ErrAccessDenied
	}
	return s.ClaimExecutionTask(delivery.WithOutpostRuntimeClaim(ctx, executorID), []string{"outpost_runtime." + executorID}, executorID, "")
}

// StartOutpostRuntime maintains only installation-bound private execution tasks.
// It does not dispatch Delivery jobs or invoke Kubernetes/remote cancel APIs.
// Bootstrap invokes this after registering result sinks when Identity has slots.
func (s *Service) StartOutpostRuntime(ctx context.Context, executorIDs []string) {
	if s == nil || len(executorIDs) == 0 {
		return
	}
	ids := slices.Clone(executorIDs)
	s.outpostMonitorOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(executionMonitorInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					_ = s.maintainOutpostRuntime(sweepCtx, ids, time.Now().UTC())
					cancel()
				}
			}
		}()
	})
}

func (s *Service) maintainOutpostRuntime(ctx context.Context, executorIDs []string, now time.Time) error {
	for _, id := range executorIDs {
		provider := "outpost_runtime." + id
		for _, status := range []string{"queued", "dispatching", "running"} {
			tasks, err := s.repo.ListExecutionTasks(ctx, delivery.ExecutionTaskFilter{ProviderKind: provider, TaskKinds: []string{domain.TaskKind}, Status: status, Limit: 200})
			if err != nil {
				return err
			}
			for _, task := range tasks {
				if task.ProviderKind != provider || task.TaskKind != domain.TaskKind || task.Status != status {
					continue
				}
				if outpostRuntimeExpired(task, now) {
					if err := s.markTaskTimedOut(ctx, task, now); err != nil {
						continue
					}
				}
			}
		}
	}
	return nil
}

func outpostRuntimeExpired(task delivery.ExecutionTask, now time.Time) bool {
	var command domain.Command
	encoded, err := json.Marshal(task.Payload["outpostRuntime"])
	if err == nil && json.Unmarshal(encoded, &command) == nil && !command.Deadline.IsZero() && !now.Before(command.Deadline) {
		return true
	}
	if task.Status == "queued" {
		return !now.Before(task.CreatedAt.Add(time.Duration(effectiveTimeoutSeconds(task)) * time.Second))
	}
	return taskHeartbeatExpired(task, now)
}

func (s *Service) GetOutpostRuntimeExecutionTask(ctx context.Context, executorID, taskID string) (delivery.ExecutionTask, error) {
	task, err := s.GetExecutionTaskInternal(ctx, taskID)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	if !delivery.IsOutpostRuntimeTask(task) {
		return delivery.ExecutionTask{}, apperrors.ErrAccessDenied
	}
	if err = delivery.AuthorizeOutpostRuntimeTask(delivery.WithOutpostRuntimeClaim(ctx, executorID), task); err != nil {
		return delivery.ExecutionTask{}, err
	}
	return task, nil
}

func (s *Service) RecordOutpostRuntimeCallback(ctx context.Context, executorID string, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	return s.RecordCallback(delivery.WithOutpostRuntimeClaim(ctx, executorID), input)
}
