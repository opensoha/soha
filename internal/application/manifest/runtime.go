package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const manifestWorkerInterval = time.Second

type DeploymentActionResult struct {
	Deployment domainmanifest.Deployment
	Task       domaindelivery.ExecutionTask
}

func (s *DeclarativeService) Start(ctx context.Context) {
	if s == nil || s.tasks == nil {
		return
	}
	go s.workerLoop(ctx)
}

func (s *DeclarativeService) Render(ctx context.Context, principal domainidentity.Principal, packageID string, input domainmanifest.RenderInput) (domainmanifest.RenderResult, error) {
	if s.renderer == nil {
		return domainmanifest.RenderResult{}, fmt.Errorf("%w: manifest renderer is unavailable", apperrors.ErrInvalidArgument)
	}
	item, err := s.base.Get(ctx, principal, strings.TrimSpace(packageID))
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	binding, err := s.repository.GetBinding(ctx, strings.TrimSpace(input.BindingID))
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	if binding.PackageID != item.ID {
		return domainmanifest.RenderResult{}, fmt.Errorf("%w: binding does not belong to manifest package", apperrors.ErrInvalidArgument)
	}
	files, revision, err := s.filesForRevision(ctx, item, input.Revision)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	return s.renderer.Render(ctx, item, binding, files, revision)
}

func (s *DeclarativeService) Preflight(ctx context.Context, principal domainidentity.Principal, packageID string, input domainmanifest.PreflightInput) (domaindelivery.ExecutionTask, error) {
	if err := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "preflight")); err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if input.ForceConflicts {
		if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryManifestFieldsForce); err != nil {
			return domaindelivery.ExecutionTask{}, err
		}
	}
	rendered, err := s.Render(ctx, principal, packageID, domainmanifest.RenderInput{BindingID: input.BindingID, Revision: input.Revision})
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	binding, err := s.repository.GetBinding(ctx, input.BindingID)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	item, err := s.base.get(ctx, packageID)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	generation := int64(rendered.Revision)
	if generation < 1 {
		generation = 1
	}
	payload := s.taskPayload(domainmanifest.TaskActionPreflight, item, binding, domainmanifest.Deployment{}, rendered, generation, input.ForceConflicts, principal.UserID)
	payload.IdempotencyKey = fmt.Sprintf("manifest:%s:%s:%d:%s:%t", item.ID, binding.ID, generation, rendered.RenderedDigest, input.ForceConflicts)
	return s.queueOperation(ctx, item, binding, domainmanifest.Deployment{}, payload)
}

func (s *DeclarativeService) SetDesiredRevision(ctx context.Context, principal domainidentity.Principal, bindingID string, input domainmanifest.DesiredRevisionInput) (DeploymentActionResult, error) {
	if err := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger")); err != nil {
		return DeploymentActionResult{}, err
	}
	binding, err := s.repository.GetBinding(ctx, strings.TrimSpace(bindingID))
	if err != nil {
		return DeploymentActionResult{}, err
	}
	item, _, err := s.bindingPackage(ctx, principal, binding.PackageID, domainaccess.ActionTrigger)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	files, revision, err := s.filesForRevision(ctx, item, input.DesiredRevision)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	rendered, err := s.renderer.Render(ctx, item, binding, files, revision)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	policy := strings.TrimSpace(input.ReconcilePolicy)
	if policy != domainmanifest.ReconcilePolicyManual && policy != domainmanifest.ReconcilePolicyContinuous {
		return DeploymentActionResult{}, fmt.Errorf("%w: unsupported reconcilePolicy", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	deployment := domainmanifest.Deployment{
		ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID,
		Spec:      domainmanifest.DeploymentSpec{DesiredRevision: revision, DesiredDigest: rendered.RenderedDigest, ReconcilePolicy: policy, DriftPolicy: binding.DriftPolicy, DeletionPolicy: binding.DeletionPolicy},
		Status:    domainmanifest.DeploymentStatus{Phase: domainmanifest.DeploymentPhasePending, Conditions: []domainmanifest.Condition{}, Inventory: []domainmanifest.ResourceInventory{}},
		CreatedAt: now, UpdatedAt: now,
	}
	deployment, err = s.repository.SetDesiredRevision(ctx, deployment, input.ExpectedGeneration)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	payload := s.taskPayload(domainmanifest.TaskActionApply, item, binding, deployment, rendered, deployment.Generation, false, principal.UserID)
	payload.IdempotencyKey = operationKey(deployment, domainmanifest.TaskActionApply)
	task, err := s.queueOperation(ctx, item, binding, deployment, payload)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	s.base.record(ctx, principal, "delivery.manifest.desired-revision.update", item, fmt.Sprintf("selected manifest revision v%d for %s", revision, binding.EnvironmentKey))
	return DeploymentActionResult{Deployment: deployment, Task: task}, nil
}

func (s *DeclarativeService) Reconcile(ctx context.Context, principal domainidentity.Principal, deploymentID, action string, input domainmanifest.ActionInput) (DeploymentActionResult, error) {
	permission := appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger")
	if action == domainmanifest.TaskActionRepair {
		permission = appaccess.PermDeliveryManifestDriftRepair
	} else if action == domainmanifest.TaskActionAdopt {
		permission = appaccess.PermDeliveryManifestDriftAdopt
	}
	if err := s.base.authorize(ctx, principal, permission); err != nil {
		return DeploymentActionResult{}, err
	}
	if input.ForceConflicts {
		if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryManifestFieldsForce); err != nil {
			return DeploymentActionResult{}, err
		}
	}
	deployment, item, binding, rendered, err := s.actionContext(ctx, principal, deploymentID, input.ExpectedGeneration)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	payload := s.taskPayload(action, item, binding, deployment, rendered, deployment.Generation, input.ForceConflicts, principal.UserID)
	payload.IdempotencyKey = operationKey(deployment, action)
	task, err := s.queueOperation(ctx, item, binding, deployment, payload)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	s.base.record(ctx, principal, "delivery.manifest."+action, item, "queued manifest "+action)
	return DeploymentActionResult{Deployment: deployment, Task: task}, nil
}

func (s *DeclarativeService) Rollback(ctx context.Context, principal domainidentity.Principal, deploymentID string, input domainmanifest.RollbackInput) (DeploymentActionResult, error) {
	if err := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger")); err != nil {
		return DeploymentActionResult{}, err
	}
	current, err := s.repository.GetDeployment(ctx, strings.TrimSpace(deploymentID))
	if err != nil {
		return DeploymentActionResult{}, err
	}
	if current.Generation != input.ExpectedGeneration {
		return DeploymentActionResult{}, fmt.Errorf("%w: manifest deployment generation changed", apperrors.ErrConflict)
	}
	target := input.TargetRevision
	if target == 0 && input.UseLastKnownGood {
		target = current.Status.LastKnownGoodRevision
	}
	if target < 1 {
		return DeploymentActionResult{}, fmt.Errorf("%w: rollback target revision is unavailable", apperrors.ErrInvalidArgument)
	}
	binding, err := s.repository.GetBinding(ctx, current.BindingID)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	item, _, err := s.bindingPackage(ctx, principal, current.PackageID, domainaccess.ActionTrigger)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	files, revision, err := s.filesForRevision(ctx, item, target)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	rendered, err := s.renderer.Render(ctx, item, binding, files, revision)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	current.Spec.DesiredRevision = revision
	current.Spec.DesiredDigest = rendered.RenderedDigest
	current.UpdatedAt = time.Now().UTC()
	current, err = s.repository.SetDesiredRevision(ctx, current, input.ExpectedGeneration)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	payload := s.taskPayload(domainmanifest.TaskActionRollback, item, binding, current, rendered, current.Generation, false, principal.UserID)
	payload.IdempotencyKey = operationKey(current, domainmanifest.TaskActionRollback)
	task, err := s.queueOperation(ctx, item, binding, current, payload)
	if err != nil {
		return DeploymentActionResult{}, err
	}
	s.base.record(ctx, principal, "delivery.manifest.rollback", item, fmt.Sprintf("queued rollback to manifest revision v%d", revision))
	return DeploymentActionResult{Deployment: current, Task: task}, nil
}

func (s *DeclarativeService) actionContext(ctx context.Context, principal domainidentity.Principal, deploymentID string, expectedGeneration int64) (domainmanifest.Deployment, domainmanifest.Package, domainmanifest.EnvironmentBinding, domainmanifest.RenderResult, error) {
	deployment, err := s.repository.GetDeployment(ctx, strings.TrimSpace(deploymentID))
	if err != nil {
		return domainmanifest.Deployment{}, domainmanifest.Package{}, domainmanifest.EnvironmentBinding{}, domainmanifest.RenderResult{}, err
	}
	if expectedGeneration < 1 || deployment.Generation != expectedGeneration {
		return domainmanifest.Deployment{}, domainmanifest.Package{}, domainmanifest.EnvironmentBinding{}, domainmanifest.RenderResult{}, fmt.Errorf("%w: manifest deployment generation changed", apperrors.ErrConflict)
	}
	item, _, err := s.bindingPackage(ctx, principal, deployment.PackageID, domainaccess.ActionTrigger)
	if err != nil {
		return domainmanifest.Deployment{}, domainmanifest.Package{}, domainmanifest.EnvironmentBinding{}, domainmanifest.RenderResult{}, err
	}
	binding, err := s.repository.GetBinding(ctx, deployment.BindingID)
	if err != nil {
		return domainmanifest.Deployment{}, domainmanifest.Package{}, domainmanifest.EnvironmentBinding{}, domainmanifest.RenderResult{}, err
	}
	files, revision, err := s.filesForRevision(ctx, item, deployment.Spec.DesiredRevision)
	if err != nil {
		return domainmanifest.Deployment{}, domainmanifest.Package{}, domainmanifest.EnvironmentBinding{}, domainmanifest.RenderResult{}, err
	}
	rendered, err := s.renderer.Render(ctx, item, binding, files, revision)
	return deployment, item, binding, rendered, err
}

func (s *DeclarativeService) filesForRevision(ctx context.Context, item domainmanifest.Package, revision int) ([]domainmanifest.File, int, error) {
	if revision == 0 {
		return item.Files, 0, nil
	}
	revisions, err := s.base.repository.ListRevisions(ctx, item.ID)
	if err != nil {
		return nil, 0, err
	}
	for _, candidate := range revisions {
		if candidate.Version == revision {
			return candidate.Files, candidate.Version, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: manifest revision v%d does not exist", apperrors.ErrNotFound, revision)
}

func (s *DeclarativeService) taskPayload(action string, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, deployment domainmanifest.Deployment, rendered domainmanifest.RenderResult, generation int64, force bool, actor string) domainmanifest.TaskPayload {
	return domainmanifest.TaskPayload{
		Action: action, PackageID: item.ID, BindingID: binding.ID, DeploymentID: deployment.ID,
		Generation: generation, Revision: rendered.Revision, RenderedDigest: rendered.RenderedDigest,
		ClusterID: binding.ClusterID, Namespace: binding.Namespace, FieldManager: "opensoha-delivery/v1",
		ForceConflicts: force, Documents: rendered.Documents, Inventory: deployment.Status.Inventory, RequestedBy: actor,
	}
}

func (s *DeclarativeService) queueOperation(ctx context.Context, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, deployment domainmanifest.Deployment, payload domainmanifest.TaskPayload) (domaindelivery.ExecutionTask, error) {
	if s.tasks == nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: manifest task runtime is unavailable", apperrors.ErrInvalidArgument)
	}
	provider, err := s.taskProvider(ctx, binding.ClusterID)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	now := time.Now().UTC()
	taskID := "task:" + uuid.NewString()
	task := domaindelivery.ExecutionTask{
		ID: taskID, ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID,
		TaskKind: taskKind(payload.Action), ProviderKind: provider, TargetKind: "manifest_deployment",
		Status: "queued", QueueKey: firstNonEmptyString(deployment.ID, binding.ID), LockKey: payload.IdempotencyKey,
		MaxRetries: 1, TimeoutSeconds: 300, CallbackToken: uuid.NewString(), Payload: structMap(payload), Result: map[string]any{},
		CreatedAt: now, UpdatedAt: now,
	}
	run := domainmanifest.OperationRun{ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID, DeploymentID: deployment.ID, Generation: payload.Generation, Action: payload.Action, IdempotencyKey: payload.IdempotencyKey, ExecutionTaskID: task.ID, CreatedAt: now}
	existingID, created, err := s.repository.CreateOperationTask(ctx, run, task)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if created {
		return task, nil
	}
	return s.tasks.GetExecutionTaskInternal(ctx, existingID)
}

func (s *DeclarativeService) taskProvider(ctx context.Context, clusterID string) (string, error) {
	connection, err := s.base.loadCluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	if connection != nil && connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return "manifest_agent." + strings.TrimSpace(clusterID), nil
	}
	return domainmanifest.TaskProviderDirect, nil
}

func (s *DeclarativeService) workerLoop(ctx context.Context) {
	ticker := time.NewTicker(manifestWorkerInterval)
	defer ticker.Stop()
	nextPoll := time.Now().UTC()
	nextObserve := time.Now().UTC()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !time.Now().UTC().Before(nextPoll) {
				s.pollGitSources(ctx)
				nextPoll = time.Now().UTC().Add(30 * time.Second)
			}
			if !time.Now().UTC().Before(nextObserve) {
				s.observeContinuousDeployments(ctx)
				nextObserve = time.Now().UTC().Add(30 * time.Second)
			}
			task, err := s.tasks.ClaimExecutionTask(ctx, []string{domainmanifest.TaskProviderDirect, domainmanifest.TaskProviderGit}, "soha-manifest-controller", "local")
			if err != nil {
				continue
			}
			s.executeLocalTask(ctx, task)
		}
	}
}

func (s *DeclarativeService) executeLocalTask(ctx context.Context, task domaindelivery.ExecutionTask) {
	payload, err := decodeTaskPayload(task.Payload)
	if err != nil {
		_, _ = s.tasks.RecordCallback(context.WithoutCancel(ctx), domaindelivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: "failed", Payload: map[string]any{"error": "invalid manifest task payload"}})
		return
	}
	_, _ = s.tasks.RecordCallback(ctx, domaindelivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: "running", Payload: map[string]any{"action": payload.Action, "generation": payload.Generation}})
	runtime := s.direct
	if task.ProviderKind == domainmanifest.TaskProviderGit {
		runtime = s.git
	}
	if runtime == nil {
		err = fmt.Errorf("manifest runtime provider is unavailable")
	}
	var result domainmanifest.TaskResult
	if err == nil {
		result, err = runtime.Execute(ctx, payload)
	}
	status := "completed"
	resultPayload := structMap(result)
	if err != nil {
		status = "failed"
		resultPayload["error"] = "manifest task execution failed"
	}
	_, _ = s.tasks.RecordCallback(context.WithoutCancel(ctx), domaindelivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: status, Payload: resultPayload})
}

func (s *DeclarativeService) RecordExecutionTaskResult(ctx context.Context, task domaindelivery.ExecutionTask) error {
	if !strings.HasPrefix(task.TaskKind, "manifest_") {
		return nil
	}
	payload, err := decodeTaskPayload(task.Payload)
	if task.TaskKind == domainmanifest.TaskKindSync {
		return s.recordSyncTaskResult(ctx, task, payload, err)
	}
	if err != nil || payload.DeploymentID == "" {
		return nil
	}
	return s.recordDeploymentTaskResult(ctx, task, payload)
}

func (s *DeclarativeService) recordSyncTaskResult(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload, decodeErr error) error {
	if decodeErr != nil {
		return nil
	}
	if err := s.repository.UpdateSyncTask(ctx, task, payload, decodeTaskResult(task.Result)); err != nil {
		return err
	}
	if task.Status == "completed" {
		return s.autoDeploySyncedRevision(ctx, payload, task.ID)
	}
	return nil
}

func (s *DeclarativeService) recordDeploymentTaskResult(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) error {
	deployment, err := s.repository.GetDeployment(ctx, payload.DeploymentID)
	if err != nil {
		return err
	}
	if deployment.Generation != payload.Generation {
		return nil
	}
	status := deployment.Status
	status.LastExecutionTaskID = task.ID
	now := time.Now().UTC()
	status.LastReconciledAt = &now
	if task.Status == "dispatching" || task.Status == "running" {
		status.Phase = domainmanifest.DeploymentPhaseReconciling
		status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: "Progressing", Status: "true", Reason: "TaskRunning", Message: "manifest task is running", ObservedGeneration: payload.Generation, LastTransitionAt: now, EvidenceRefs: []string{task.ID}})
		return s.updateStatusIgnoringStale(ctx, deployment.ID, payload.Generation, status)
	}
	result := decodeTaskResult(task.Result)
	if task.Status != "completed" {
		status.Phase = domainmanifest.DeploymentPhaseDegraded
		status.LastErrorCode = "manifest_task_failed"
		status.LastErrorMessage = publicTaskError(task.Result)
		status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: conditionType(payload.Action), Status: "false", Reason: "TaskFailed", Message: status.LastErrorMessage, ObservedGeneration: payload.Generation, LastTransitionAt: now, EvidenceRefs: []string{task.ID}})
		return s.updateStatusIgnoringStale(ctx, deployment.ID, payload.Generation, status)
	}
	status.ObservedGeneration = payload.Generation
	status.LastErrorCode = ""
	status.LastErrorMessage = ""
	status.Inventory = result.Inventory
	status.Drift = result.Drift
	status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: conditionType(payload.Action), Status: "true", Reason: "TaskCompleted", Message: "manifest task completed", ObservedGeneration: payload.Generation, LastTransitionAt: now, EvidenceRefs: append([]string{task.ID}, result.EvidenceRefs...)})
	switch payload.Action {
	case domainmanifest.TaskActionApply, domainmanifest.TaskActionRepair, domainmanifest.TaskActionRollback:
		status.AppliedRevision = payload.Revision
		status.AppliedDigest = payload.RenderedDigest
		if inventoryHealthy(result.Inventory) {
			status.LastKnownGoodRevision = payload.Revision
			status.Phase = domainmanifest.DeploymentPhaseConverged
			status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: "Healthy", Status: "true", Reason: "ApplyObserved", Message: "managed resources were applied and observed", ObservedGeneration: payload.Generation, LastTransitionAt: now, EvidenceRefs: []string{task.ID}})
		} else {
			status.Phase = domainmanifest.DeploymentPhaseReconciling
			status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: "Healthy", Status: "false", Reason: "AwaitingHealth", Message: "managed resources were applied and are awaiting a healthy observation", ObservedGeneration: payload.Generation, LastTransitionAt: now, EvidenceRefs: []string{task.ID}})
		}
	case domainmanifest.TaskActionObserve:
		if result.Drift != nil && result.Drift.Drifted {
			status = applyObservedDriftState(status, deployment.Spec.DriftPolicy, payload.Generation, task.ID, now)
		} else if !inventoryHealthy(result.Inventory) {
			status.Phase = domainmanifest.DeploymentPhaseReconciling
			status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: "Healthy", Status: "false", Reason: "AwaitingHealth", Message: "managed resources match the desired fields but are not healthy yet", ObservedGeneration: payload.Generation, LastTransitionAt: now, EvidenceRefs: []string{task.ID}})
		} else {
			status.LastKnownGoodRevision = payload.Revision
			status.Phase = domainmanifest.DeploymentPhaseConverged
			status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: "Healthy", Status: "true", Reason: "ObservedHealthy", Message: "managed resources are healthy", ObservedGeneration: payload.Generation, LastTransitionAt: now, EvidenceRefs: []string{task.ID}})
			status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: "Drifted", Status: "false", Reason: "DesiredStateMatched", Message: "managed resource fields match the desired revision", ObservedGeneration: payload.Generation, LastTransitionAt: now, EvidenceRefs: []string{task.ID}})
		}
	case domainmanifest.TaskActionAdopt:
		if len(result.AdoptedFiles) > 0 {
			if err := s.repository.ApplyAdoptedFiles(ctx, deployment.ID, payload.Generation, result.AdoptedFiles, payload.RequestedBy); err != nil {
				return err
			}
		}
		status.Phase = domainmanifest.DeploymentPhaseDrifted
	}
	return s.updateStatusIgnoringStale(ctx, deployment.ID, payload.Generation, status)
}

func inventoryHealthy(items []domainmanifest.ResourceInventory) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item.Health != "healthy" {
			return false
		}
	}
	return true
}

func applyObservedDriftState(status domainmanifest.DeploymentStatus, driftPolicy string, generation int64, taskID string, observedAt time.Time) domainmanifest.DeploymentStatus {
	status.Phase = domainmanifest.DeploymentPhaseDrifted
	reason := "ManagedFieldsChanged"
	message := "managed resource fields differ from the desired revision"
	if driftPolicy == domainmanifest.DriftPolicyRepair {
		status.Phase = domainmanifest.DeploymentPhaseWaitingApproval
		reason = "RepairApprovalRequired"
		message = "managed resource fields differ from the desired revision; an authorized repair must be confirmed"
	}
	status.Conditions = upsertCondition(status.Conditions, domainmanifest.Condition{Type: "Drifted", Status: "true", Reason: reason, Message: message, ObservedGeneration: generation, LastTransitionAt: observedAt, EvidenceRefs: []string{taskID}})
	return status
}

func (s *DeclarativeService) observeContinuousDeployments(ctx context.Context) {
	items, err := s.repository.ListContinuousDeployments(ctx, 20)
	if err != nil {
		return
	}
	for _, deployment := range items {
		_ = s.queueAutomaticAction(ctx, deployment, domainmanifest.TaskActionObserve, time.Now().UTC())
	}
}

func (s *DeclarativeService) queueAutomaticAction(ctx context.Context, deployment domainmanifest.Deployment, action string, observedAt time.Time) error {
	item, err := s.base.get(ctx, deployment.PackageID)
	if err != nil {
		return err
	}
	binding, err := s.repository.GetBinding(ctx, deployment.BindingID)
	if err != nil {
		return err
	}
	files, revision, err := s.filesForRevision(ctx, item, deployment.Spec.DesiredRevision)
	if err != nil {
		return err
	}
	rendered, err := s.renderer.Render(ctx, item, binding, files, revision)
	if err != nil {
		return err
	}
	payload := s.taskPayload(action, item, binding, deployment, rendered, deployment.Generation, false, "system:manifest-controller")
	bucket := observedAt.UTC().Unix() / 60
	payload.IdempotencyKey = fmt.Sprintf("manifest:%s:%d:%s:%d", deployment.ID, deployment.Generation, action, bucket)
	_, err = s.queueOperation(ctx, item, binding, deployment, payload)
	return err
}

func (s *DeclarativeService) updateStatusIgnoringStale(ctx context.Context, deploymentID string, generation int64, status domainmanifest.DeploymentStatus) error {
	err := s.repository.UpdateDeploymentStatus(ctx, deploymentID, generation, status)
	if errors.Is(err, apperrors.ErrConflict) {
		return nil
	}
	return err
}

func operationKey(deployment domainmanifest.Deployment, action string) string {
	return fmt.Sprintf("manifest:%s:%d:%s", deployment.ID, deployment.Generation, action)
}

func taskKind(action string) string {
	switch action {
	case domainmanifest.TaskActionPreflight:
		return domainmanifest.TaskKindPreflight
	case domainmanifest.TaskActionObserve:
		return domainmanifest.TaskKindObserve
	case domainmanifest.TaskActionRepair:
		return domainmanifest.TaskKindRepair
	case domainmanifest.TaskActionAdopt:
		return domainmanifest.TaskKindAdopt
	case domainmanifest.TaskActionRollback:
		return domainmanifest.TaskKindRollback
	case domainmanifest.TaskActionSync:
		return domainmanifest.TaskKindSync
	default:
		return domainmanifest.TaskKindApply
	}
}

func decodeTaskPayload(value map[string]any) (domainmanifest.TaskPayload, error) {
	var result domainmanifest.TaskPayload
	encoded, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(encoded, &result)
	}
	return result, err
}

func decodeTaskResult(value map[string]any) domainmanifest.TaskResult {
	var result domainmanifest.TaskResult
	encoded, _ := json.Marshal(value)
	_ = json.Unmarshal(encoded, &result)
	return result
}

func structMap(value any) map[string]any {
	encoded, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(encoded, &result)
	return result
}

func conditionType(action string) string {
	switch action {
	case domainmanifest.TaskActionPreflight:
		return "PreflightPassed"
	case domainmanifest.TaskActionObserve, domainmanifest.TaskActionAdopt:
		return "Drifted"
	default:
		return "Synced"
	}
}

func upsertCondition(items []domainmanifest.Condition, next domainmanifest.Condition) []domainmanifest.Condition {
	for index := range items {
		if items[index].Type == next.Type {
			if items[index].Status == next.Status && items[index].Reason == next.Reason {
				next.LastTransitionAt = items[index].LastTransitionAt
			}
			items[index] = next
			return items
		}
	}
	return append(items, next)
}

func publicTaskError(result map[string]any) string {
	return "manifest task failed"
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
