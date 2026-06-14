package execution

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Repository interface {
	domaindelivery.Repository
}

type Service struct {
	repo          Repository
	builds        BuildRecordRepository
	releases      ReleaseRecordRepository
	clusters      *k8sinfra.Manager
	jobClusterID  string
	jobNamespace  string
	jobImage      string
	jobGitImage   string
	jobTTLSeconds int
	runnerToken   string
	permissions   *appaccess.PermissionResolver
}

type BuildRecordRepository interface {
	GetByExecutionTaskID(context.Context, string) (domainbuild.Record, error)
	Update(context.Context, domainbuild.Record) (domainbuild.Record, error)
}

type ReleaseRecordRepository interface {
	GetByExecutionTaskID(context.Context, string) (domainrelease.Record, error)
	Update(context.Context, domainrelease.Record) (domainrelease.Record, error)
}

const executionMonitorInterval = 15 * time.Second

type artifactBundleGetter interface {
	GetReleaseBundle(context.Context, string) (domaindelivery.ReleaseBundle, error)
}

func New(repo Repository, builds BuildRecordRepository, releases ReleaseRecordRepository, clusters *k8sinfra.Manager, jobClusterID, jobNamespace, jobImage, jobGitImage string, jobTTLSeconds int, runnerToken string, permissions *appaccess.PermissionResolver) *Service {
	if strings.TrimSpace(jobNamespace) == "" {
		jobNamespace = "soha-system"
	}
	if strings.TrimSpace(jobImage) == "" {
		jobImage = "alpine:3.20"
	}
	if strings.TrimSpace(jobGitImage) == "" {
		jobGitImage = "alpine/git:2.47.0"
	}
	if jobTTLSeconds <= 0 {
		jobTTLSeconds = 3600
	}
	return &Service{
		repo:          repo,
		builds:        builds,
		releases:      releases,
		clusters:      clusters,
		jobClusterID:  strings.TrimSpace(jobClusterID),
		jobNamespace:  strings.TrimSpace(jobNamespace),
		jobImage:      strings.TrimSpace(jobImage),
		jobGitImage:   strings.TrimSpace(jobGitImage),
		jobTTLSeconds: jobTTLSeconds,
		runnerToken:   strings.TrimSpace(runnerToken),
		permissions:   permissions,
	}
}

func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	go s.monitorLoop(ctx)
}

type BuildPlan struct {
	ApplicationID            string
	ApplicationEnvironmentID string
	Version                  string
	SourceType               string
	ProviderKind             string
	TargetKind               string
	ArtifactRef              string
	Metadata                 map[string]any
}

type ReleasePlan struct {
	ApplicationID            string
	ApplicationEnvironmentID string
	ReleaseBundleID          string
	Version                  string
	SourceType               string
	ProviderKind             string
	TargetKind               string
	ArtifactRef              string
	Metadata                 map[string]any
}

func (s *Service) ClaimExecutionTask(ctx context.Context, providerKinds []string, agentID, runtimeEndpoint string) (domaindelivery.ExecutionTask, error) {
	if strings.TrimSpace(agentID) == "" {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: agentID is required", apperrors.ErrInvalidArgument)
	}
	task, err := s.repo.ClaimExecutionTask(ctx, providerKinds, strings.TrimSpace(agentID), strings.TrimSpace(runtimeEndpoint))
	return domaindelivery.WithOperationState(task, time.Now().UTC()), err
}

func (s *Service) ListReleaseBundles(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleaseBoardView); err != nil {
		return nil, err
	}
	return s.repo.ListReleaseBundles(ctx, filter)
}

func (s *Service) GetReleaseBundle(ctx context.Context, principal domainidentity.Principal, bundleID string) (domaindelivery.ReleaseBundle, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleaseBoardView); err != nil {
		return domaindelivery.ReleaseBundle{}, err
	}
	return s.repo.GetReleaseBundle(ctx, strings.TrimSpace(bundleID))
}

func (s *Service) ListExecutionTasks(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return nil, err
	}
	items, err := s.repo.ListExecutionTasks(ctx, filter)
	return domaindelivery.WithOperationStates(items, time.Now().UTC()), err
}

func (s *Service) GetExecutionTask(ctx context.Context, principal domainidentity.Principal, taskID string) (domaindelivery.ExecutionTask, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	task, err := s.repo.GetExecutionTask(ctx, strings.TrimSpace(taskID))
	return domaindelivery.WithOperationState(task, time.Now().UTC()), err
}

func (s *Service) ListExecutionLogs(ctx context.Context, principal domainidentity.Principal, taskID string, limit int) ([]domaindelivery.ExecutionLog, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return nil, err
	}
	return s.repo.ListExecutionLogs(ctx, strings.TrimSpace(taskID), limit)
}

func (s *Service) ListExecutionArtifacts(ctx context.Context, principal domainidentity.Principal, taskID string) ([]domaindelivery.ExecutionArtifact, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return nil, err
	}
	return s.repo.ListExecutionArtifacts(ctx, strings.TrimSpace(taskID))
}

func (s *Service) ListReleaseBundleArtifacts(ctx context.Context, principal domainidentity.Principal, bundleID string) ([]domaindelivery.ExecutionArtifact, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleaseBoardView); err != nil {
		return nil, err
	}
	return s.repo.ListExecutionArtifactsByBundle(ctx, strings.TrimSpace(bundleID))
}

func (s *Service) CancelExecutionTask(ctx context.Context, taskID string, input domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	task, err := s.repo.GetExecutionTask(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if !isCancelableTaskStatus(task.Status) {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: task %s cannot be canceled from status %s", apperrors.ErrInvalidArgument, task.ID, task.Status)
	}
	now := time.Now().UTC()
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "canceled from control plane"
	}
	payload := map[string]any{
		"executionTaskStatus": "canceled",
		"canceledAt":          now.Format(time.RFC3339),
		"cancelReason":        reason,
	}
	task.Status = "canceled"
	task.AttemptCount = maxInt(task.AttemptCount, 1)
	task.Result = mergeMaps(task.Result, payload)
	task.LastHeartbeatAt = &now
	task.FinishedAt = &now
	task.UpdatedAt = now
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
		ID:              uuid.NewString(),
		ExecutionTaskID: updated.ID,
		LogLevel:        "warn",
		Message:         fmt.Sprintf("execution task canceled: %s", reason),
		Metadata:        payload,
		CreatedAt:       now,
	})
	_ = s.stopK8sJobExecution(ctx, updated, reason)
	_ = s.stopRemoteRuntimeTask(ctx, updated.ID, updated.Result, reason)
	if strings.TrimSpace(updated.ReleaseBundleID) != "" {
		bundle, bundleErr := s.repo.GetReleaseBundle(ctx, updated.ReleaseBundleID)
		if bundleErr == nil {
			applyTaskResultToBundle(&bundle, updated, now)
			_, _ = s.repo.UpdateReleaseBundle(ctx, bundle)
		}
	}
	_ = s.persistArtifacts(ctx, updated)
	switch updated.TaskKind {
	case "build":
		_ = s.syncBuildRecord(ctx, updated)
	case "release":
		_ = s.syncReleaseRecord(ctx, updated)
	}
	return domaindelivery.WithOperationState(updated, time.Now().UTC()), nil
}

func (s *Service) RetryExecutionTask(ctx context.Context, taskID string, input domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	task, err := s.repo.GetExecutionTask(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if !isRetryableTaskStatus(task.Status) {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: task %s cannot be retried from status %s", apperrors.ErrInvalidArgument, task.ID, task.Status)
	}
	now := time.Now().UTC()
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "retry requested from control plane"
	}
	previousStatus := task.Status
	previousResult := task.Result
	task.Status = "queued"
	task.MaxRetries = maxInt(task.MaxRetries, task.AttemptCount+1)
	task.CallbackToken = uuid.NewString()
	task.StartedAt = nil
	task.LastHeartbeatAt = nil
	task.FinishedAt = nil
	task.Result = map[string]any{
		"previousStatus":      previousStatus,
		"previousResult":      previousResult,
		"retryRequestedAt":    now.Format(time.RFC3339),
		"retryReason":         reason,
		"executionTaskStatus": "queued",
	}
	task.UpdatedAt = now
	_ = s.stopK8sJobExecution(ctx, task, "retry")
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
		ID:              uuid.NewString(),
		ExecutionTaskID: updated.ID,
		LogLevel:        "info",
		Message:         fmt.Sprintf("execution task re-queued: %s", reason),
		Metadata: map[string]any{
			"previousStatus": previousStatus,
			"retryReason":    reason,
		},
		CreatedAt: now,
	})
	if strings.TrimSpace(updated.ReleaseBundleID) != "" {
		bundle, bundleErr := s.repo.GetReleaseBundle(ctx, updated.ReleaseBundleID)
		if bundleErr == nil {
			prepareBundleForRetry(&bundle, updated, reason, now)
			_, _ = s.repo.UpdateReleaseBundle(ctx, bundle)
		}
	}
	dispatched, dispatchErr := s.dispatchExecutionTask(ctx, updated)
	if dispatchErr == nil {
		updated = dispatched
	}
	switch updated.TaskKind {
	case "build":
		_ = s.syncBuildRecord(ctx, updated)
	case "release":
		_ = s.syncReleaseRecord(ctx, updated)
	}
	return domaindelivery.WithOperationState(updated, time.Now().UTC()), nil
}

func (s *Service) StartBuildExecution(ctx context.Context, plan BuildPlan) (domaindelivery.ReleaseBundle, domaindelivery.ExecutionTask, error) {
	bundle, err := s.repo.CreateReleaseBundle(ctx, domaindelivery.ReleaseBundle{
		ID:                       "bundle:" + uuid.NewString(),
		ApplicationID:            strings.TrimSpace(plan.ApplicationID),
		ApplicationEnvironmentID: strings.TrimSpace(plan.ApplicationEnvironmentID),
		Version:                  resolveVersion(plan.Version),
		SourceType:               firstNonEmpty(plan.SourceType, "build"),
		Status:                   "building",
		ArtifactRef:              strings.TrimSpace(plan.ArtifactRef),
		Metadata:                 ensureMap(plan.Metadata),
		CreatedAt:                time.Now().UTC(),
		UpdatedAt:                time.Now().UTC(),
	})
	if err != nil {
		return domaindelivery.ReleaseBundle{}, domaindelivery.ExecutionTask{}, err
	}
	task, err := s.repo.CreateExecutionTask(ctx, domaindelivery.ExecutionTask{
		ID:                       "task:" + uuid.NewString(),
		ReleaseBundleID:          bundle.ID,
		ApplicationID:            bundle.ApplicationID,
		ApplicationEnvironmentID: bundle.ApplicationEnvironmentID,
		TaskKind:                 "build",
		ProviderKind:             firstNonEmpty(plan.ProviderKind, "k8s_job_runner"),
		TargetKind:               firstNonEmpty(plan.TargetKind, "k8s_workload"),
		Status:                   "queued",
		QueueKey:                 bundle.ApplicationID,
		LockKey:                  bundle.ApplicationID + ":build",
		MaxRetries:               1,
		AttemptCount:             0,
		TimeoutSeconds:           300,
		CallbackToken:            uuid.NewString(),
		Payload:                  ensureMap(plan.Metadata),
		Result:                   map[string]any{},
		CreatedAt:                time.Now().UTC(),
		UpdatedAt:                time.Now().UTC(),
	})
	if err != nil {
		return domaindelivery.ReleaseBundle{}, domaindelivery.ExecutionTask{}, err
	}
	dispatched, dispatchErr := s.dispatchExecutionTask(ctx, task)
	if dispatchErr == nil {
		task = dispatched
	}
	return bundle, domaindelivery.WithOperationState(task, time.Now().UTC()), nil
}

func (s *Service) CompleteBuildExecution(ctx context.Context, bundleID, taskID, artifactRef, artifactDigest string, result map[string]any) error {
	bundle, err := s.repo.GetReleaseBundle(ctx, bundleID)
	if err != nil {
		return err
	}
	bundle.Status = "ready"
	bundle.ArtifactRef = strings.TrimSpace(artifactRef)
	bundle.ArtifactDigest = strings.TrimSpace(artifactDigest)
	bundle.Metadata = mergeMaps(bundle.Metadata, result)
	bundle.UpdatedAt = time.Now().UTC()
	if _, err := s.repo.UpdateReleaseBundle(ctx, bundle); err != nil {
		return err
	}
	task, err := s.repo.GetExecutionTask(ctx, taskID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	task.Status = "completed"
	task.AttemptCount = maxInt(task.AttemptCount, 1)
	task.StartedAt = &now
	task.FinishedAt = &now
	task.Result = mergeMaps(task.Result, result)
	task.UpdatedAt = now
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return err
	}
	return s.syncBuildRecord(ctx, updated)
}

func (s *Service) StartReleaseExecution(ctx context.Context, plan ReleasePlan) (domaindelivery.ReleaseBundle, domaindelivery.ExecutionTask, error) {
	var (
		bundle domaindelivery.ReleaseBundle
		err    error
	)
	if strings.TrimSpace(plan.ReleaseBundleID) != "" {
		bundle, err = s.repo.GetReleaseBundle(ctx, plan.ReleaseBundleID)
		if err != nil {
			return domaindelivery.ReleaseBundle{}, domaindelivery.ExecutionTask{}, err
		}
	} else {
		bundle, err = s.repo.CreateReleaseBundle(ctx, domaindelivery.ReleaseBundle{
			ID:                       "bundle:" + uuid.NewString(),
			ApplicationID:            strings.TrimSpace(plan.ApplicationID),
			ApplicationEnvironmentID: strings.TrimSpace(plan.ApplicationEnvironmentID),
			Version:                  resolveVersion(plan.Version),
			SourceType:               firstNonEmpty(plan.SourceType, "release"),
			Status:                   "ready",
			ArtifactRef:              strings.TrimSpace(plan.ArtifactRef),
			Metadata:                 ensureMap(plan.Metadata),
			CreatedAt:                time.Now().UTC(),
			UpdatedAt:                time.Now().UTC(),
		})
		if err != nil {
			return domaindelivery.ReleaseBundle{}, domaindelivery.ExecutionTask{}, err
		}
	}
	task, err := s.repo.CreateExecutionTask(ctx, domaindelivery.ExecutionTask{
		ID:                       "task:" + uuid.NewString(),
		ReleaseBundleID:          bundle.ID,
		ApplicationID:            bundle.ApplicationID,
		ApplicationEnvironmentID: bundle.ApplicationEnvironmentID,
		TaskKind:                 "release",
		ProviderKind:             firstNonEmpty(plan.ProviderKind, "k8s_job_runner"),
		TargetKind:               firstNonEmpty(plan.TargetKind, "k8s_workload"),
		Status:                   "queued",
		QueueKey:                 bundle.ApplicationID,
		LockKey:                  bundle.ApplicationID + ":release",
		MaxRetries:               1,
		AttemptCount:             0,
		TimeoutSeconds:           300,
		CallbackToken:            uuid.NewString(),
		Payload:                  ensureMap(plan.Metadata),
		Result:                   map[string]any{},
		CreatedAt:                time.Now().UTC(),
		UpdatedAt:                time.Now().UTC(),
	})
	if err != nil {
		return domaindelivery.ReleaseBundle{}, domaindelivery.ExecutionTask{}, err
	}
	dispatched, dispatchErr := s.dispatchExecutionTask(ctx, task)
	if dispatchErr == nil {
		task = dispatched
	}
	return bundle, domaindelivery.WithOperationState(task, time.Now().UTC()), nil
}

func (s *Service) CompleteReleaseExecution(ctx context.Context, bundleID, taskID, status string, result map[string]any) error {
	task, err := s.repo.GetExecutionTask(ctx, taskID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	task.Status = firstNonEmpty(status, "completed")
	task.AttemptCount = maxInt(task.AttemptCount, 1)
	task.StartedAt = &now
	task.FinishedAt = &now
	task.Result = mergeMaps(task.Result, result)
	task.UpdatedAt = now
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return err
	}
	bundle, err := s.repo.GetReleaseBundle(ctx, bundleID)
	if err != nil {
		return err
	}
	bundle.Status = task.Status
	bundle.Metadata = mergeMaps(bundle.Metadata, result)
	bundle.UpdatedAt = now
	_, err = s.repo.UpdateReleaseBundle(ctx, bundle)
	if err != nil {
		return err
	}
	return s.syncReleaseRecord(ctx, updated)
}

func (s *Service) RecordCallback(ctx context.Context, input domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error) {
	task, err := s.repo.GetExecutionTaskByCallbackToken(ctx, strings.TrimSpace(input.CallbackToken))
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	callback := domaindelivery.ExecutionCallback{
		ID:              uuid.NewString(),
		ExecutionTaskID: task.ID,
		ProviderKind:    task.ProviderKind,
		Status:          strings.TrimSpace(input.Status),
		Payload:         ensureMap(input.Payload),
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.repo.CreateExecutionCallback(ctx, callback); err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if isStrictTerminalTaskStatus(task.Status) {
		now := time.Now().UTC()
		_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
			ID:              uuid.NewString(),
			ExecutionTaskID: task.ID,
			LogLevel:        "warn",
			Message:         fmt.Sprintf("ignored late callback for terminal task status %s", task.Status),
			Metadata: map[string]any{
				"callbackStatus": strings.TrimSpace(input.Status),
			},
			CreatedAt: now,
		})
		return domaindelivery.WithOperationState(task, time.Now().UTC()), nil
	}
	if logs, ok := input.Payload["logs"].([]any); ok {
		for _, item := range logs {
			message := strings.TrimSpace(fmt.Sprint(item))
			if message == "" {
				continue
			}
			_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
				ID:              uuid.NewString(),
				ExecutionTaskID: task.ID,
				LogLevel:        "info",
				Message:         message,
				Metadata:        map[string]any{},
				CreatedAt:       time.Now().UTC(),
			})
		}
	}
	now := time.Now().UTC()
	if strings.TrimSpace(task.Status) == "queued" || strings.TrimSpace(task.Status) == "dispatching" {
		task.StartedAt = &now
	}
	task.Status = firstNonEmpty(input.Status, task.Status)
	task.AttemptCount = maxInt(task.AttemptCount, 1)
	task.Result = mergeMaps(task.Result, ensureMap(input.Payload))
	task.LastHeartbeatAt = &now
	if task.Status == "completed" || task.Status == "failed" {
		task.FinishedAt = &now
	}
	task.UpdatedAt = now
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if strings.TrimSpace(updated.ReleaseBundleID) != "" && (updated.Status == "completed" || updated.Status == "failed") {
		bundle, bundleErr := s.repo.GetReleaseBundle(ctx, updated.ReleaseBundleID)
		if bundleErr == nil {
			applyTaskResultToBundle(&bundle, updated, now)
			_, _ = s.repo.UpdateReleaseBundle(ctx, bundle)
		}
	}
	_ = s.persistArtifacts(ctx, updated)
	switch updated.TaskKind {
	case "build":
		_ = s.syncBuildRecord(ctx, updated)
	case "release":
		_ = s.syncReleaseRecord(ctx, updated)
	}
	return domaindelivery.WithOperationState(updated, time.Now().UTC()), nil
}

func (s *Service) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(executionMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = s.recoverStaleTasks(sweepCtx, time.Now().UTC())
			cancel()
		}
	}
}

func (s *Service) recoverStaleTasks(ctx context.Context, now time.Time) error {
	for _, status := range []string{"dispatching", "running"} {
		tasks, err := s.repo.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{
			Status: status,
			Limit:  200,
		})
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if handled, reconcileErr := s.reconcileK8sJobExecution(ctx, task, now); reconcileErr == nil && handled {
				continue
			}
			if !taskHeartbeatExpired(task, now) {
				continue
			}
			if err := s.markTaskTimedOut(ctx, task, now); err != nil {
				continue
			}
		}
	}
	return nil
}

func taskHeartbeatExpired(task domaindelivery.ExecutionTask, now time.Time) bool {
	if strings.TrimSpace(task.Status) != "dispatching" && strings.TrimSpace(task.Status) != "running" {
		return false
	}
	timeoutSeconds := task.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}
	reference := task.CreatedAt
	if task.LastHeartbeatAt != nil && !task.LastHeartbeatAt.IsZero() {
		reference = task.LastHeartbeatAt.UTC()
	} else if task.StartedAt != nil && !task.StartedAt.IsZero() {
		reference = task.StartedAt.UTC()
	}
	return now.After(reference.Add(time.Duration(timeoutSeconds) * time.Second))
}

func isCancelableTaskStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "queued", "dispatching", "running":
		return true
	default:
		return false
	}
}

func isRetryableTaskStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "failed", "callback_timeout", "canceled":
		return true
	default:
		return false
	}
}

func isStrictTerminalTaskStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "canceled", "callback_timeout":
		return true
	default:
		return false
	}
}

func (s *Service) markTaskTimedOut(ctx context.Context, task domaindelivery.ExecutionTask, now time.Time) error {
	timeoutPayload := map[string]any{
		"error":               fmt.Sprintf("execution task timed out after %d seconds without heartbeat", effectiveTimeoutSeconds(task)),
		"timeoutSeconds":      effectiveTimeoutSeconds(task),
		"timedOutAt":          now.Format(time.RFC3339),
		"executionTaskStatus": "callback_timeout",
	}
	if task.LastHeartbeatAt != nil {
		timeoutPayload["lastHeartbeatAt"] = task.LastHeartbeatAt.UTC().Format(time.RFC3339)
	}
	_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
		ID:              uuid.NewString(),
		ExecutionTaskID: task.ID,
		LogLevel:        "warn",
		Message:         fmt.Sprintf("execution task timed out after %d seconds without heartbeat", effectiveTimeoutSeconds(task)),
		Metadata:        timeoutPayload,
		CreatedAt:       now,
	})
	_ = s.stopK8sJobExecution(ctx, task, "callback_timeout")
	_ = s.stopRemoteRuntimeTask(ctx, task.ID, task.Result, "callback_timeout")
	task.Status = "callback_timeout"
	task.AttemptCount = maxInt(task.AttemptCount, 1)
	task.Result = mergeMaps(task.Result, timeoutPayload)
	task.FinishedAt = &now
	task.UpdatedAt = now
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return err
	}
	if strings.TrimSpace(updated.ReleaseBundleID) != "" {
		bundle, bundleErr := s.repo.GetReleaseBundle(ctx, updated.ReleaseBundleID)
		if bundleErr == nil {
			applyTaskResultToBundle(&bundle, updated, now)
			_, _ = s.repo.UpdateReleaseBundle(ctx, bundle)
		}
	}
	_ = s.persistArtifacts(ctx, updated)
	switch updated.TaskKind {
	case "build":
		_ = s.syncBuildRecord(ctx, updated)
	case "release":
		_ = s.syncReleaseRecord(ctx, updated)
	}
	return nil
}

func (s *Service) stopRemoteRuntimeTask(ctx context.Context, taskID string, result map[string]any, reason string) error {
	if strings.TrimSpace(s.runnerToken) == "" {
		return nil
	}
	endpoint := strings.TrimSpace(fmt.Sprint(result["runtimeEndpoint"]))
	if endpoint == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/api/v1/runtime/execution-tasks/"+strings.TrimSpace(taskID)+"/cancel", strings.NewReader(fmt.Sprintf(`{"reason":%q}`, reason)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.runnerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("runtime cancel failed with status %d", resp.StatusCode)
	}
	return nil
}

func (s *Service) persistArtifacts(ctx context.Context, task domaindelivery.ExecutionTask) error {
	if len(task.Artifacts) == 0 {
		return nil
	}
	for _, item := range task.Artifacts {
		artifact := item
		artifact.ExecutionTaskID = task.ID
		artifact.ReleaseBundleID = task.ReleaseBundleID
		artifact.ApplicationID = task.ApplicationID
		artifact.ApplicationEnvironmentID = task.ApplicationEnvironmentID
		if strings.TrimSpace(artifact.Status) == "" {
			artifact.Status = task.Status
		}
		if _, err := s.repo.UpsertExecutionArtifact(ctx, artifact); err != nil {
			return err
		}
	}
	return nil
}

func resolveVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC().Format("20060102-150405")
	}
	return value
}

func ensureMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	next := ensureMap(base)
	for key, value := range ensureMap(overlay) {
		next[key] = value
	}
	return next
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt(values ...int) int {
	result := 0
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func (s *Service) dispatchExecutionTask(ctx context.Context, task domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	now := time.Now().UTC()
	switch strings.TrimSpace(task.ProviderKind) {
	case "ci_agent_runner", "external_pipeline_adapter":
		_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
			ID:              uuid.NewString(),
			ExecutionTaskID: task.ID,
			LogLevel:        "info",
			Message:         fmt.Sprintf("task queued for %s and is waiting for runner claim", task.ProviderKind),
			Metadata:        map[string]any{"providerKind": task.ProviderKind},
			CreatedAt:       now,
		})
		return task, nil
	case "k8s_job_runner":
		if reason := s.k8sJobRunnerDisabledReason(task); reason != "" {
			return s.markTaskProviderDisabled(ctx, task, now, reason)
		}
		if handled, updated, err := s.dispatchK8sJobExecution(ctx, task, now); handled {
			return updated, err
		}
		return s.markTaskProviderDisabled(ctx, task, now, "k8s_job_runner provider is disabled: no executable Kubernetes Job could be created")
	default:
		providerKind := strings.TrimSpace(task.ProviderKind)
		if providerKind == "" {
			return s.markTaskProviderDisabled(ctx, task, now, "execution provider is disabled: providerKind is required")
		}
		return s.markTaskProviderDisabled(ctx, task, now, fmt.Sprintf("execution provider %q is not configured", providerKind))
	}
}

func (s *Service) k8sJobRunnerDisabledReason(task domaindelivery.ExecutionTask) string {
	if s.clusters == nil {
		return "k8s_job_runner provider is disabled: Kubernetes cluster manager is not configured"
	}
	if len(valueAsStringSlice(task.Payload["commands"])) == 0 {
		return "k8s_job_runner provider is disabled: execution commands are required"
	}
	if s.resolveExecutionJobClusterID(task) == "" {
		return "k8s_job_runner provider is disabled: execution job cluster is not configured"
	}
	return ""
}

func (s *Service) markTaskProviderDisabled(ctx context.Context, task domaindelivery.ExecutionTask, now time.Time, message string) (domaindelivery.ExecutionTask, error) {
	task.Status = "failed"
	task.AttemptCount = maxInt(task.AttemptCount, 1)
	task.LastHeartbeatAt = &now
	task.FinishedAt = &now
	task.UpdatedAt = now
	task.Result = mergeMaps(task.Result, map[string]any{
		"executionTaskStatus": "failed",
		"failureReason":       "provider_disabled",
		"providerDisabled":    true,
		"providerKind":        strings.TrimSpace(task.ProviderKind),
		"error":               message,
	})
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if strings.TrimSpace(updated.ReleaseBundleID) != "" {
		bundleItem, bundleErr := s.repo.GetReleaseBundle(ctx, updated.ReleaseBundleID)
		if bundleErr == nil {
			applyTaskResultToBundle(&bundleItem, updated, now)
			_, _ = s.repo.UpdateReleaseBundle(ctx, bundleItem)
		}
	}
	_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
		ID:              uuid.NewString(),
		ExecutionTaskID: updated.ID,
		LogLevel:        "error",
		Message:         message,
		Metadata: map[string]any{
			"providerKind":  updated.ProviderKind,
			"failureReason": "provider_disabled",
		},
		CreatedAt: now,
	})
	switch updated.TaskKind {
	case "build":
		_ = s.syncBuildRecord(ctx, updated)
	case "release":
		_ = s.syncReleaseRecord(ctx, updated)
	}
	return updated, nil
}

func (s *Service) dispatchK8sJobExecution(ctx context.Context, task domaindelivery.ExecutionTask, now time.Time) (bool, domaindelivery.ExecutionTask, error) {
	if s.clusters == nil {
		return false, domaindelivery.ExecutionTask{}, nil
	}
	commands := valueAsStringSlice(task.Payload["commands"])
	if len(commands) == 0 {
		return false, domaindelivery.ExecutionTask{}, nil
	}
	clusterID := s.resolveExecutionJobClusterID(task)
	if clusterID == "" {
		return false, domaindelivery.ExecutionTask{}, nil
	}
	namespace := firstNonEmpty(strings.TrimSpace(fmt.Sprint(task.Payload["jobNamespace"])), s.jobNamespace)
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return false, domaindelivery.ExecutionTask{}, err
	}
	if err := ensureNamespaceExists(ctx, bundle, namespace); err != nil {
		return false, domaindelivery.ExecutionTask{}, err
	}
	job, err := s.buildExecutionJob(task, namespace)
	if err != nil {
		return false, domaindelivery.ExecutionTask{}, err
	}
	created, err := bundle.Typed.BatchV1().Jobs(namespace).Create(ctx, &job, metav1.CreateOptions{})
	if err != nil {
		return false, domaindelivery.ExecutionTask{}, err
	}
	task.Status = "running"
	task.StartedAt = &now
	task.LastHeartbeatAt = &now
	task.UpdatedAt = now
	task.Result = mergeMaps(task.Result, map[string]any{
		"k8sJobClusterId": clusterID,
		"k8sJobNamespace": namespace,
		"k8sJobName":      created.Name,
		"k8sJobStatus":    "running",
	})
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return true, domaindelivery.ExecutionTask{}, err
	}
	_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
		ID:              uuid.NewString(),
		ExecutionTaskID: updated.ID,
		LogLevel:        "info",
		Message:         fmt.Sprintf("k8s_job_runner created Job %s/%s on cluster %s", namespace, created.Name, clusterID),
		Metadata: map[string]any{
			"clusterId": clusterID,
			"namespace": namespace,
			"jobName":   created.Name,
		},
		CreatedAt: now,
	})
	return true, updated, nil
}

func (s *Service) reconcileK8sJobExecution(ctx context.Context, task domaindelivery.ExecutionTask, now time.Time) (bool, error) {
	if strings.TrimSpace(task.ProviderKind) != "k8s_job_runner" || s.clusters == nil {
		return false, nil
	}
	clusterID, namespace, jobName := executionJobRef(task)
	if clusterID == "" || namespace == "" || jobName == "" {
		return false, nil
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return false, err
	}
	job, err := bundle.Typed.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return true, s.markTaskFailed(ctx, task, now, fmt.Sprintf("execution Job %s/%s was not found", namespace, jobName))
		}
		return true, err
	}
	if job.Status.Succeeded > 0 {
		task.Status = "completed"
		task.LastHeartbeatAt = &now
		task.FinishedAt = &now
		task.UpdatedAt = now
		task.Result = mergeMaps(task.Result, map[string]any{
			"k8sJobStatus":   "completed",
			"jobCompletedAt": now.Format(time.RFC3339),
		})
		updated, updateErr := s.repo.UpdateExecutionTask(ctx, task)
		if updateErr != nil {
			return true, updateErr
		}
		if strings.TrimSpace(updated.ReleaseBundleID) != "" {
			bundleItem, bundleErr := s.repo.GetReleaseBundle(ctx, updated.ReleaseBundleID)
			if bundleErr == nil {
				applyTaskResultToBundle(&bundleItem, updated, now)
				_, _ = s.repo.UpdateReleaseBundle(ctx, bundleItem)
			}
		}
		_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
			ID:              uuid.NewString(),
			ExecutionTaskID: updated.ID,
			LogLevel:        "info",
			Message:         fmt.Sprintf("k8s_job_runner Job %s/%s completed", namespace, jobName),
			Metadata:        map[string]any{"clusterId": clusterID, "namespace": namespace, "jobName": jobName},
			CreatedAt:       now,
		})
		_ = s.captureK8sJobLogs(ctx, bundle, namespace, jobName, updated.ID, "info", now)
		_ = s.syncBuildRecord(ctx, updated)
		return true, nil
	}
	if job.Status.Failed > 0 {
		_ = s.captureK8sJobLogs(ctx, bundle, namespace, jobName, task.ID, "error", now)
		return true, s.markTaskFailed(ctx, task, now, fmt.Sprintf("k8s_job_runner Job %s/%s failed", namespace, jobName))
	}
	task.LastHeartbeatAt = &now
	task.UpdatedAt = now
	task.Result = mergeMaps(task.Result, map[string]any{
		"k8sJobStatus": "running",
	})
	_, _ = s.repo.UpdateExecutionTask(ctx, task)
	return true, nil
}

func (s *Service) buildExecutionJob(task domaindelivery.ExecutionTask, namespace string) (batchv1.Job, error) {
	commands := valueAsStringSlice(task.Payload["commands"])
	if len(commands) == 0 {
		return batchv1.Job{}, fmt.Errorf("execution task %s does not contain commands", task.ID)
	}
	runtime := valueAsMap(task.Payload["runtime"])
	workspace := valueAsMap(task.Payload["workspace"])
	checkout := valueAsMap(workspace["checkout"])
	jobName := buildExecutionJobName(task)
	shell := firstNonEmpty(strings.TrimSpace(fmt.Sprint(runtime["shell"])), "/bin/sh")
	script := "set -e\n" + strings.Join(commands, "\n")
	workingDir := "/workspace"
	if commandDir := strings.TrimSpace(fmt.Sprint(runtime["commandDir"])); commandDir != "" && commandDir != "." {
		workingDir = "/workspace/" + trimRelativePath(commandDir)
	}
	container := corev1.Container{
		Name:            "runner",
		Image:           firstNonEmpty(strings.TrimSpace(fmt.Sprint(runtime["image"])), s.jobImage),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{shell, "-lc", script},
		WorkingDir:      workingDir,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace", MountPath: "/workspace"},
		},
	}
	spec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Volumes: []corev1.Volume{
			{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		},
		Containers: []corev1.Container{container},
	}
	if repositoryURL := firstNonEmpty(strings.TrimSpace(fmt.Sprint(checkout["repositoryURL"])), strings.TrimSpace(fmt.Sprint(checkout["repositoryUrl"]))); repositoryURL != "" {
		spec.InitContainers = []corev1.Container{
			{
				Name:            "checkout",
				Image:           firstNonEmpty(strings.TrimSpace(fmt.Sprint(runtime["checkoutImage"])), s.jobGitImage),
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"/bin/sh", "-lc", buildCheckoutScript(checkout, repositoryURL)},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "workspace", MountPath: "/workspace"},
				},
			},
		}
	}
	ttl := int32(s.jobTTLSeconds)
	backoff := int32(0)
	return batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "soha",
				"soha.io/execution-task":       task.ID,
				"soha.io/task-kind":            task.TaskKind,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "soha",
						"soha.io/execution-task":       task.ID,
					},
				},
				Spec: spec,
			},
		},
	}, nil
}

func buildExecutionJobName(task domaindelivery.ExecutionTask) string {
	base := strings.NewReplacer(":", "-", "_", "-", "/", "-").Replace(strings.TrimSpace(task.ID))
	if len(base) > 38 {
		base = base[len(base)-38:]
	}
	return fmt.Sprintf("soha-exec-%s-%d", base, time.Now().UTC().Unix()%100000)
}

func buildCheckoutScript(checkout map[string]any, repositoryURL string) string {
	refType := firstNonEmpty(strings.TrimSpace(fmt.Sprint(checkout["refType"])), "branch")
	refName := strings.TrimSpace(fmt.Sprint(checkout["refName"]))
	defaultBranch := strings.TrimSpace(fmt.Sprint(checkout["defaultBranch"]))
	if refName == "" && refType == "branch" {
		refName = defaultBranch
	}
	lines := []string{"set -e", "git clone " + shellQuote(repositoryURL) + " /workspace", "cd /workspace"}
	if refName == "" {
		return strings.Join(lines, "\n")
	}
	switch refType {
	case "tag":
		lines = append(lines, "git checkout tags/"+shellQuote(refName))
	case "commit":
		lines = append(lines, "git checkout "+shellQuote(refName))
	default:
		lines = append(lines, "git checkout "+shellQuote(refName))
	}
	return strings.Join(lines, "\n")
}

func (s *Service) resolveExecutionJobClusterID(task domaindelivery.ExecutionTask) string {
	if value := strings.TrimSpace(s.jobClusterID); value != "" {
		return value
	}
	if value := strings.TrimSpace(fmt.Sprint(task.Payload["jobClusterId"])); value != "" {
		return value
	}
	if value := strings.TrimSpace(fmt.Sprint(task.Payload["clusterId"])); value != "" {
		return value
	}
	if s.clusters != nil {
		ids := s.clusters.ClusterIDs()
		if len(ids) > 0 {
			return strings.TrimSpace(ids[0])
		}
	}
	return ""
}

func executionJobRef(task domaindelivery.ExecutionTask) (string, string, string) {
	return strings.TrimSpace(fmt.Sprint(task.Result["k8sJobClusterId"])),
		strings.TrimSpace(fmt.Sprint(task.Result["k8sJobNamespace"])),
		strings.TrimSpace(fmt.Sprint(task.Result["k8sJobName"]))
}

func ensureNamespaceExists(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) error {
	if bundle == nil {
		return fmt.Errorf("kubernetes bundle is not available")
	}
	if _, err := bundle.Typed.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err == nil {
		return nil
	} else if !k8serrors.IsNotFound(err) {
		return err
	}
	_, err := bundle.Typed.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	return err
}

func (s *Service) markTaskFailed(ctx context.Context, task domaindelivery.ExecutionTask, now time.Time, message string) error {
	task.Status = "failed"
	task.AttemptCount = maxInt(task.AttemptCount, 1)
	task.LastHeartbeatAt = &now
	task.FinishedAt = &now
	task.UpdatedAt = now
	task.Result = mergeMaps(task.Result, map[string]any{
		"executionTaskStatus": "failed",
		"error":               message,
	})
	updated, err := s.repo.UpdateExecutionTask(ctx, task)
	if err != nil {
		return err
	}
	if strings.TrimSpace(updated.ReleaseBundleID) != "" {
		bundleItem, bundleErr := s.repo.GetReleaseBundle(ctx, updated.ReleaseBundleID)
		if bundleErr == nil {
			applyTaskResultToBundle(&bundleItem, updated, now)
			_, _ = s.repo.UpdateReleaseBundle(ctx, bundleItem)
		}
	}
	_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
		ID:              uuid.NewString(),
		ExecutionTaskID: updated.ID,
		LogLevel:        "error",
		Message:         message,
		Metadata:        map[string]any{},
		CreatedAt:       now,
	})
	switch updated.TaskKind {
	case "build":
		_ = s.syncBuildRecord(ctx, updated)
	case "release":
		_ = s.syncReleaseRecord(ctx, updated)
	}
	return nil
}

func (s *Service) stopK8sJobExecution(ctx context.Context, task domaindelivery.ExecutionTask, reason string) error {
	if strings.TrimSpace(task.ProviderKind) != "k8s_job_runner" || s.clusters == nil {
		return nil
	}
	clusterID, namespace, jobName := executionJobRef(task)
	if clusterID == "" || namespace == "" || jobName == "" {
		return nil
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return err
	}
	propagation := metav1.DeletePropagationBackground
	if err := bundle.Typed.BatchV1().Jobs(namespace).Delete(ctx, jobName, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
		ID:              uuid.NewString(),
		ExecutionTaskID: task.ID,
		LogLevel:        "warn",
		Message:         fmt.Sprintf("k8s_job_runner Job %s/%s deleted due to %s", namespace, jobName, reason),
		Metadata: map[string]any{
			"clusterId": clusterID,
			"namespace": namespace,
			"jobName":   jobName,
			"reason":    reason,
		},
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (s *Service) captureK8sJobLogs(ctx context.Context, bundle *k8sinfra.Bundle, namespace, jobName, taskID, level string, now time.Time) error {
	if bundle == nil || strings.TrimSpace(namespace) == "" || strings.TrimSpace(jobName) == "" || strings.TrimSpace(taskID) == "" {
		return nil
	}
	pods, err := bundle.Typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			request := bundle.Typed.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
				TailLines: int64ptr(200),
			})
			raw, readErr := request.DoRaw(ctx)
			if readErr != nil {
				continue
			}
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				_ = s.repo.CreateExecutionLog(ctx, domaindelivery.ExecutionLog{
					ID:              uuid.NewString(),
					ExecutionTaskID: taskID,
					LogLevel:        firstNonEmpty(strings.TrimSpace(level), "info"),
					Message:         line,
					Metadata: map[string]any{
						"podName":       pod.Name,
						"containerName": container.Name,
					},
					CreatedAt: now,
				})
			}
		}
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func trimRelativePath(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	for strings.HasPrefix(value, "../") {
		value = strings.TrimPrefix(value, "../")
	}
	if value == "." {
		return ""
	}
	return value
}

func int64ptr(value int64) *int64 {
	return &value
}

func valueAsStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	default:
		return nil
	}
}

func valueAsMap(raw any) map[string]any {
	value, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return value
}

func (s *Service) syncBuildRecord(ctx context.Context, task domaindelivery.ExecutionTask) error {
	if s.builds == nil {
		return nil
	}
	record, err := s.builds.GetByExecutionTaskID(ctx, task.ID)
	if err != nil {
		return nil
	}
	record.Status = mapTaskStatusToBuildStatus(task.Status)
	record.Metadata = mergeMaps(record.Metadata, task.Result)
	record.Metadata["executionTaskStatus"] = task.Status
	if strings.TrimSpace(task.ReleaseBundleID) != "" {
		record.Metadata["releaseBundleId"] = task.ReleaseBundleID
	}
	if task.LastHeartbeatAt != nil {
		record.Metadata["lastHeartbeatAt"] = task.LastHeartbeatAt.UTC().Format(time.RFC3339)
	}
	if task.StartedAt != nil {
		record.StartedAt = task.StartedAt
	}
	if (task.Status == "completed" || task.Status == "failed") && task.FinishedAt != nil {
		record.FinishedAt = task.FinishedAt
	}
	_, err = s.builds.Update(ctx, record)
	return err
}

func (s *Service) syncReleaseRecord(ctx context.Context, task domaindelivery.ExecutionTask) error {
	if s.releases == nil {
		return nil
	}
	record, err := s.releases.GetByExecutionTaskID(ctx, task.ID)
	if err != nil {
		return nil
	}
	record.Status = mapTaskStatusToReleaseStatus(task.Status)
	record.Metadata = mergeMaps(record.Metadata, task.Result)
	record.Metadata["executionTaskStatus"] = task.Status
	if strings.TrimSpace(task.ReleaseBundleID) != "" {
		record.Metadata["releaseBundleId"] = task.ReleaseBundleID
	}
	if task.LastHeartbeatAt != nil {
		record.Metadata["lastHeartbeatAt"] = task.LastHeartbeatAt.UTC().Format(time.RFC3339)
	}
	if task.Status == "completed" {
		if task.FinishedAt != nil {
			record.DeployedAt = task.FinishedAt
		} else {
			now := time.Now().UTC()
			record.DeployedAt = &now
		}
	}
	_, err = s.releases.Update(ctx, record)
	return err
}

func mapTaskStatusToBuildStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "running", "dispatching":
		return "running"
	case "completed":
		return "completed"
	case "failed", "callback_timeout", "canceled":
		return "failed"
	default:
		return "queued"
	}
}

func mapTaskStatusToReleaseStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "running", "dispatching":
		return "running"
	case "completed":
		return "deployed"
	case "failed", "callback_timeout", "canceled":
		return "failed"
	default:
		return "queued"
	}
}

func applyTaskResultToBundle(bundle *domaindelivery.ReleaseBundle, task domaindelivery.ExecutionTask, now time.Time) {
	if bundle == nil {
		return
	}
	bundle.Metadata = mergeMaps(bundle.Metadata, task.Result)
	switch strings.TrimSpace(task.TaskKind) {
	case "build":
		switch strings.TrimSpace(task.Status) {
		case "completed":
			bundle.Status = "ready"
		case "failed", "callback_timeout", "canceled":
			bundle.Status = "failed"
		default:
			bundle.Status = "building"
		}
		if artifactRef := resolveArtifactRef(task.Result, task.Payload); artifactRef != "" {
			bundle.ArtifactRef = artifactRef
		}
		if artifactDigest := resolveArtifactDigest(task.Result, task.Payload); artifactDigest != "" {
			bundle.ArtifactDigest = artifactDigest
		}
	default:
		switch strings.TrimSpace(task.Status) {
		case "failed", "callback_timeout", "canceled":
			bundle.Status = "failed"
		default:
			bundle.Status = firstNonEmpty(task.Status, bundle.Status)
		}
	}
	bundle.UpdatedAt = now
}

func prepareBundleForRetry(bundle *domaindelivery.ReleaseBundle, task domaindelivery.ExecutionTask, reason string, now time.Time) {
	if bundle == nil {
		return
	}
	bundle.Metadata = mergeMaps(bundle.Metadata, map[string]any{
		"retryRequestedAt": now.Format(time.RFC3339),
		"retryReason":      reason,
		"retryTaskId":      task.ID,
	})
	switch strings.TrimSpace(task.TaskKind) {
	case "build":
		bundle.Status = "building"
	case "release":
		bundle.Status = "releasing"
	default:
		bundle.Status = "queued"
	}
	bundle.UpdatedAt = now
}

func effectiveTimeoutSeconds(task domaindelivery.ExecutionTask) int {
	if task.TimeoutSeconds > 0 {
		return task.TimeoutSeconds
	}
	return 300
}

func resolveArtifactRef(result, payload map[string]any) string {
	if ref := strings.TrimSpace(fmt.Sprint(result["image"])); ref != "" {
		return ref
	}
	if artifact, ok := result["artifact"].(map[string]any); ok {
		if ref := strings.TrimSpace(fmt.Sprint(artifact["ref"])); ref != "" {
			return ref
		}
	}
	if artifacts := valueAsMapSlice(result["artifacts"]); len(artifacts) > 0 {
		for _, artifact := range artifacts {
			if ref := strings.TrimSpace(fmt.Sprint(artifact["ref"])); ref != "" {
				return ref
			}
		}
	}
	if ref := strings.TrimSpace(fmt.Sprint(payload["image"])); ref != "" {
		return ref
	}
	return ""
}

func resolveArtifactDigest(result, payload map[string]any) string {
	if digest := strings.TrimSpace(fmt.Sprint(result["imageDigest"])); digest != "" && digest != "pending" {
		return digest
	}
	if artifact, ok := result["artifact"].(map[string]any); ok {
		if digest := strings.TrimSpace(fmt.Sprint(artifact["digest"])); digest != "" && digest != "pending" {
			return digest
		}
	}
	if artifacts := valueAsMapSlice(result["artifacts"]); len(artifacts) > 0 {
		for _, artifact := range artifacts {
			if digest := strings.TrimSpace(fmt.Sprint(artifact["digest"])); digest != "" && digest != "pending" {
				return digest
			}
		}
	}
	if digest := strings.TrimSpace(fmt.Sprint(payload["imageDigest"])); digest != "" && digest != "pending" {
		return digest
	}
	return ""
}

func valueAsMapSlice(raw any) []map[string]any {
	switch value := raw.(type) {
	case []map[string]any:
		return value
	case []any:
		items := make([]map[string]any, 0, len(value))
		for _, item := range value {
			mapped, ok := item.(map[string]any)
			if ok {
				items = append(items, mapped)
			}
		}
		return items
	default:
		return nil
	}
}
