package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appexecution "github.com/opensoha/soha/internal/application/execution"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ExecuteDeliveryStage(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, batch domainworkflow.DeliveryBatch, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	var snapshot *domainworkflow.DeliveryTargetSnapshot
	for i := range batch.Targets {
		if batch.Targets[i].Target.ID == node.TargetID {
			snapshot = &batch.Targets[i]
			break
		}
	}
	if snapshot == nil {
		return node, apperrors.ErrNotFound
	}
	var err error
	switch node.Stage {
	case "build":
		node, err = s.executeBatchBuild(ctx, principal, *snapshot, node)
	case "plan":
		node, err = s.executeBatchPlan(ctx, principal, run, *snapshot, node)
	case "deploy":
		node, err = s.executeBatchDeploy(ctx, principal, run, *snapshot, node)
	case "health":
		node, err = s.observeBatchHealth(ctx, principal, run, *snapshot, node)
	default:
		err = fmt.Errorf("%w: unsupported delivery stage", apperrors.ErrInvalidArgument)
	}
	if errors.Is(err, apperrors.ErrInvalidArgument) || errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) {
		node.Status, node.Summary = "failed", err.Error()
		return node, nil
	}
	return node, err
}

func (s *Service) executeBatchBuild(ctx context.Context, principal domainidentity.Principal, snapshot domainworkflow.DeliveryTargetSnapshot, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	capability, _ := domainworkflow.NodeExecutionFrom(ctx)
	task, err := s.repository.GetExecutionTask(ctx, capability.ResourceID("task"))
	if err == nil {
		return projectBatchTask(node, task), nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return node, err
	}
	if snapshot.FrozenBuild == nil {
		return node, fmt.Errorf("%w: build snapshot is missing", apperrors.ErrInvalidArgument)
	}
	if err := s.validateBatchConfiguration(ctx, principal, snapshot); err != nil {
		return node, err
	}
	builder, ok := s.builds.(frozenBuildRuntime)
	if !ok {
		return node, fmt.Errorf("%w: frozen build runtime is unavailable", apperrors.ErrInvalidArgument)
	}
	record, err := builder.TriggerFrozen(ctx, principal, *snapshot.FrozenBuild)
	if err != nil {
		// An external create may have succeeded before its response was lost.
		if task, readErr := s.repository.GetExecutionTask(ctx, capability.ResourceID("task")); readErr == nil {
			return projectBatchTask(node, task), nil
		}
		return node, err
	}
	node.BuildRecordID = record.ID
	task, err = s.repository.GetExecutionTask(ctx, capability.ResourceID("task"))
	if err != nil {
		return node, err
	}
	return projectBatchTask(node, task), nil
}

func projectBatchTask(node domainworkflow.NodeRun, task domaindelivery.ExecutionTask) domainworkflow.NodeRun {
	node.ExecutionTaskID = task.ID
	if task.ReleaseBundleID != "" {
		node.ReleaseBundleID = task.ReleaseBundleID
	}
	if value, ok := task.Payload["deploymentId"].(string); ok {
		node.ManifestDeploymentID = value
	}
	if value, ok := task.Payload["buildRecordId"].(string); ok {
		node.BuildRecordID = value
	}
	switch task.Status {
	case "completed":
		node.Status, node.Summary = "completed", "execution completed"
		if task.TaskKind == "build" {
			if _, err := appexecution.BuildArtifactImage(task); err != nil {
				node.Status, node.Summary = "failed", "build completed without a verified immutable image"
			}
		}
	case "failed", "callback_timeout":
		node.Status, node.Summary = "failed", "execution failed"
	case "canceled":
		node.Status, node.Summary = "canceled", "executor stop confirmed"
	case "canceling":
		node.Status, node.Summary = "canceling", "waiting for executor stop confirmation"
	default:
		node.Status, node.Summary = "waiting_execution", "waiting for execution result"
	}
	return node
}

func batchStageNode(run domainworkflow.Run, targetID, stage string) domainworkflow.NodeRun {
	for _, node := range run.NodeRuns {
		if node.TargetID == targetID && node.Stage == stage {
			return node
		}
	}
	return domainworkflow.NodeRun{}
}

func (s *Service) validateBatchConfiguration(ctx context.Context, principal domainidentity.Principal, snapshot domainworkflow.DeliveryTargetSnapshot) error {
	_, service, err := s.deliveryTargetService(ctx, principal, snapshot.Target)
	if err != nil {
		return err
	}
	if service.Version != snapshot.ServiceVersion {
		return fmt.Errorf("%w: service configuration changed; start a new delivery batch", apperrors.ErrInvalidArgument)
	}
	var binding domaincatalog.ApplicationEnvironment
	if snapshot.Target.ApplicationEnvironmentID != "" {
		binding, err = s.catalog.GetApplicationEnvironment(ctx, principal, snapshot.Target.ApplicationEnvironmentID)
		if err != nil {
			return err
		}
		if binding.ApplicationID != service.ApplicationID {
			return apperrors.ErrInvalidArgument
		}
	}
	current, selected, err := s.currentBatchManifest(ctx, principal, service, binding, snapshot)
	if err != nil {
		return err
	}
	digest, err := s.deliveryConfigurationDigest(ctx, principal, service, binding, current, selected)
	if err != nil {
		return err
	}
	if digest != snapshot.ConfigurationDigest {
		return fmt.Errorf("%w: delivery configuration changed; start a new delivery batch", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) currentBatchManifest(ctx context.Context, principal domainidentity.Principal, service domainapp.Service, binding domaincatalog.ApplicationEnvironment, snapshot domainworkflow.DeliveryTargetSnapshot) (*domainmanifest.DeliveryConfiguration, *domaincatalog.ReleaseTarget, error) {
	if snapshot.FrozenManifest == nil && snapshot.FrozenHelm == nil && snapshot.FrozenDocker == nil {
		return nil, nil, nil
	}
	var selected = snapshot.FrozenReleaseTarget
	if selected == nil {
		return nil, nil, fmt.Errorf("%w: frozen release target is missing", apperrors.ErrInvalidArgument)
	}
	found := false
	for _, target := range binding.Targets {
		if target.ID == selected.ID && target.Enabled {
			selected, found = &target, true
			break
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("%w: release target is no longer enabled", apperrors.ErrInvalidArgument)
	}
	if snapshot.FrozenDocker != nil {
		if selected.Docker == nil || s.docker == nil {
			return nil, nil, apperrors.ErrConflict
		}
		current, err := s.docker.FreezeDeliveryProject(ctx, principal, selected.Docker.HostID, selected.Docker.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		if current.ProjectDigest != snapshot.FrozenDocker.ProjectDigest {
			return nil, nil, fmt.Errorf("%w: Docker project changed", apperrors.ErrConflict)
		}
		return nil, selected, nil
	}
	if snapshot.FrozenHelm != nil {
		if selected.Helm == nil {
			return nil, nil, apperrors.ErrConflict
		}
		return nil, selected, nil
	}
	runtime, ok := s.manifestDelivery.(manifestConfigurationRuntime)
	if !ok {
		return nil, nil, fmt.Errorf("%w: Manifest configuration runtime is unavailable", apperrors.ErrInvalidArgument)
	}
	current, err := runtime.FreezeDeliveryConfiguration(ctx, principal, service.ApplicationID, service.ID, binding.ID, *selected, snapshot.ManifestRevision)
	if err != nil {
		return nil, nil, err
	}
	return &current, selected, nil
}

func (s *Service) observeBatchHealth(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, snapshot domainworkflow.DeliveryTargetSnapshot, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	previous := batchStageNode(run, node.TargetID, "deploy")
	node.DeliveryPlanID, node.ManifestDeploymentID = previous.DeliveryPlanID, previous.ManifestDeploymentID
	if snapshot.FrozenReleaseTarget == nil {
		return node, apperrors.ErrInvalidArgument
	}
	if snapshot.FrozenDocker != nil {
		return s.observeBatchDockerRuntime(ctx, principal, snapshot, previous, node)
	}
	if snapshot.FrozenHelm != nil {
		return s.observeBatchHelmHealth(ctx, principal, snapshot, node)
	}
	deployment, err := s.manifestDelivery.DeliveryDeployment(ctx, principal, snapshot.Target.ApplicationID, snapshot.Target.ApplicationEnvironmentID, *snapshot.FrozenReleaseTarget)
	if err != nil {
		return node, err
	}
	node.ManifestDeploymentID = deployment.ID
	selected := deployment.Spec.DeliverySnapshot
	if selected == nil || selected.DeliveryPlanID != node.DeliveryPlanID {
		return node, fmt.Errorf("%w: environment now has another delivery plan", apperrors.ErrInvalidArgument)
	}
	for _, condition := range deployment.Status.Conditions {
		if deployment.Status.Phase == domainmanifest.DeploymentPhaseConverged && deployment.Status.ObservedGeneration == deployment.Generation && deployment.Status.AppliedDigest == selected.RenderedDigest && condition.Type == "Healthy" && condition.Status == "true" && condition.ObservedGeneration == deployment.Generation {
			node.Status, node.Summary = "completed", "deployed resources are healthy"
			return node, nil
		}
	}
	node.Status, node.Summary = "waiting_execution", "waiting for this deployment to become healthy"
	timeout := snapshot.HealthTimeoutSeconds
	if timeout <= 0 {
		timeout = 300
	}
	if started, err := time.Parse(time.RFC3339, node.StartedAt); err == nil && time.Since(started) > time.Duration(timeout)*time.Second {
		node.Status, node.Summary = "failed", fmt.Sprintf("deployment did not become healthy within %d seconds", timeout)
	}
	return node, nil
}

func (s *Service) CancelDeliveryStage(ctx context.Context, run domainworkflow.Run, batch domainworkflow.DeliveryBatch, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	for _, snapshot := range batch.Targets {
		if snapshot.Target.ID == node.TargetID && snapshot.FrozenDocker != nil && node.Stage != "build" {
			return s.cancelBatchDockerStage(ctx, run, node)
		}
	}
	capability, _ := domainworkflow.NodeExecutionFrom(domainworkflow.WithNodeExecution(ctx, run, node))
	taskID := firstNonEmpty(node.ExecutionTaskID, capability.ResourceID("task"))
	task, err := s.repository.GetExecutionTask(ctx, taskID)
	if errors.Is(err, apperrors.ErrNotFound) {
		node.Status, node.Summary = "canceled", "delivery stage stopped before dispatch"
		return node, nil
	}
	if err != nil {
		return node, err
	}
	if task.Payload["workflowRunId"] != run.ID || task.Payload["workflowNodeId"] != node.NodeID {
		return node, fmt.Errorf("%w: execution task does not belong to the delivery node", apperrors.ErrConflict)
	}
	if task.Status == "completed" && node.Stage == "plan" {
		node.Status, node.Summary = "canceled", "delivery stopped after preflight"
		return node, nil
	}
	task, err = s.execution.CancelExecutionTask(ctx, task.ID, domaindelivery.ExecutionTaskActionInput{Reason: strings.TrimSpace(run.StopSummary)})
	if err != nil {
		return node, err
	}
	return projectBatchTask(node, task), nil
}
