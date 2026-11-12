package execution

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	outpostdomain "github.com/opensoha/soha/internal/domain/outpostruntime"
	proxydomain "github.com/opensoha/soha/internal/domain/proxydeployment"
	radiusdomain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Credentials, claims and task authorization stay in the dedicated adapters.
func (s *Service) startPrivateMaintenance(ctx context.Context, executorIDs []string, once *sync.Once, maintain func(context.Context, []string, time.Time) error) {
	if len(executorIDs) == 0 {
		return
	}
	ids := slices.Clone(executorIDs)
	once.Do(func() {
		go func() {
			ticker := time.NewTicker(executionMonitorInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					_ = maintain(sweepCtx, ids, time.Now().UTC())
					cancel()
				}
			}
		}()
	})
}
func (s *Service) maintainPrivateExecutions(ctx context.Context, executorIDs []string, prefix, kind string, expired func(delivery.ExecutionTask, time.Time) bool, now time.Time) error {
	for _, id := range executorIDs {
		provider := prefix + id
		for _, status := range []string{"queued", "dispatching", "running"} {
			tasks, err := s.repo.ListExecutionTasks(ctx, delivery.ExecutionTaskFilter{ProviderKind: provider, TaskKinds: []string{kind}, Status: status, Limit: 200})
			if err != nil {
				return err
			}
			for _, task := range tasks {
				if task.ProviderKind != provider || task.TaskKind != kind || task.Status != status {
					continue
				}
				if expired(task, now) {
					if err := s.markTaskTimedOut(ctx, task, now); err != nil {
						continue
					}
				}
			}
		}
	}
	return nil
}

func privateExecutionExpired(task delivery.ExecutionTask, deadline time.Time, decoded bool, now time.Time) bool {
	if decoded && !deadline.IsZero() && !now.Before(deadline) {
		return true
	}
	if task.Status == "queued" {
		return !now.Before(task.CreatedAt.Add(time.Duration(effectiveTimeoutSeconds(task)) * time.Second))
	}
	return taskHeartbeatExpired(task, now)
}
func (s *Service) getPrivateExecutionTask(ctx context.Context, taskID string, matches func(delivery.ExecutionTask) bool, authorize func(context.Context, delivery.ExecutionTask) error) (delivery.ExecutionTask, error) {
	task, err := s.GetExecutionTaskInternal(ctx, taskID)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	if !matches(task) {
		return delivery.ExecutionTask{}, apperrors.ErrAccessDenied
	}
	if err := authorize(ctx, task); err != nil {
		return delivery.ExecutionTask{}, err
	}
	return task, nil
}

func (s *Service) claimPrivateExecutionTask(ctx context.Context, executorID, providerPrefix string) (delivery.ExecutionTask, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(executorID) {
		return delivery.ExecutionTask{}, apperrors.ErrAccessDenied
	}
	return s.ClaimExecutionTask(ctx, []string{providerPrefix + executorID}, executorID, "")
}
func decodePrivateExecutionPayload(payload, command any) bool {
	encoded, err := json.Marshal(payload)
	return err == nil && json.Unmarshal(encoded, command) == nil
}

// StartOutpostRuntime maintains only installation-bound private execution tasks.
// It does not dispatch Delivery jobs or invoke Kubernetes/remote cancel APIs.
// Bootstrap invokes this after registering result sinks when Identity has slots.
func (s *Service) StartOutpostRuntime(ctx context.Context, executorIDs []string) {
	if s != nil {
		s.startPrivateMaintenance(ctx, executorIDs, &s.outpostMonitorOnce, s.maintainOutpostRuntime)
	}
}

// StartProxyDeployment maintains only installation-bound private execution tasks.
// It does not dispatch Delivery jobs or invoke Kubernetes/remote cancel APIs.
// Bootstrap invokes this after registering result sinks when Identity has slots.
func (s *Service) StartProxyDeployment(ctx context.Context, executorIDs []string) {
	if s != nil {
		s.startPrivateMaintenance(ctx, executorIDs, &s.proxyMonitorOnce, s.maintainProxyDeployment)
	}
}

// StartRadiusDeployment maintains only installation-bound private execution tasks.
// It does not dispatch Delivery jobs or invoke Kubernetes/remote cancel APIs.
// Bootstrap invokes this after registering result sinks when Identity has slots.
func (s *Service) StartRadiusDeployment(ctx context.Context, executorIDs []string) {
	if s != nil {
		s.startPrivateMaintenance(ctx, executorIDs, &s.radiusMonitorOnce, s.maintainRadiusDeployment)
	}
}

func (s *Service) maintainOutpostRuntime(ctx context.Context, executorIDs []string, now time.Time) error {
	return s.maintainPrivateExecutions(ctx, executorIDs, "outpost_runtime.", outpostdomain.TaskKind, outpostRuntimeExpired, now)
}

func (s *Service) maintainProxyDeployment(ctx context.Context, executorIDs []string, now time.Time) error {
	return s.maintainPrivateExecutions(ctx, executorIDs, "proxy_deployment.", proxydomain.TaskKind, proxyDeploymentExpired, now)
}

func (s *Service) maintainRadiusDeployment(ctx context.Context, executorIDs []string, now time.Time) error {
	return s.maintainPrivateExecutions(ctx, executorIDs, "radius_deployment.", radiusdomain.TaskKind, radiusDeploymentExpired, now)
}

func outpostRuntimeExpired(task delivery.ExecutionTask, now time.Time) bool {
	var command outpostdomain.Command
	valid := decodePrivateExecutionPayload(task.Payload["outpostRuntime"], &command)
	return privateExecutionExpired(task, command.Deadline, valid, now)
}

func proxyDeploymentExpired(task delivery.ExecutionTask, now time.Time) bool {
	var command proxydomain.Command
	valid := decodePrivateExecutionPayload(task.Payload["proxyDeployment"], &command)
	return privateExecutionExpired(task, command.Deadline, valid, now)
}

func radiusDeploymentExpired(task delivery.ExecutionTask, now time.Time) bool {
	var command radiusdomain.Command
	valid := decodePrivateExecutionPayload(task.Payload["radiusDeployment"], &command)
	return privateExecutionExpired(task, command.Deadline, valid, now)
}
