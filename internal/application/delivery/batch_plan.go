package delivery

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/opensoha/soha/internal/application/deliverygovernance"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) executeBatchPlan(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, snapshot domainworkflow.DeliveryTargetSnapshot, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	capability, _ := domainworkflow.NodeExecutionFrom(ctx)
	planID := capability.ResourceID("plan")
	plan, err := s.repository.GetDeliveryPlan(ctx, planID)
	if errors.Is(err, apperrors.ErrNotFound) {
		if err := s.validateBatchConfiguration(ctx, principal, snapshot); err != nil {
			return node, err
		}
		bundleID, bundleErr := s.resolveBatchPlanBundle(ctx, principal, run, snapshot)
		if bundleErr != nil {
			return node, bundleErr
		}
		plan, err = s.CreateDeliveryPlan(ctx, principal, domaindelivery.DeliveryPlanInput{ID: planID, Source: domainworkflow.ScopeDeliveryBatch,
			ApplicationID: snapshot.Target.ApplicationID, ApplicationEnvironmentID: snapshot.Target.ApplicationEnvironmentID,
			Action: domaindelivery.ApplicationDeliveryActionDeploy, TargetID: snapshot.Target.ReleaseTargetID,
			ReleaseBundleID: bundleID, ManifestRevision: snapshot.ManifestRevision, HelmRevision: snapshot.Target.HelmRevision, FrozenHelmCiphertext: snapshot.FrozenHelmCiphertext, FrozenDockerCiphertext: frozenDockerCiphertext(snapshot)})
	}
	if err != nil {
		return node, err
	}
	node.DeliveryPlanID, node.ReleaseBundleID = plan.ID, plan.ReleaseBundleID
	if snapshot.FrozenDocker != nil {
		node, err = s.batchDockerPreflight(ctx, principal, snapshot, plan, node)
	} else {
		var task domaindelivery.ExecutionTask
		task, err = s.batchPlanPreflight(ctx, principal, snapshot, plan)
		node = projectBatchTask(node, task)
	}
	if err != nil {
		return node, err
	}
	node.ReleaseBundleID = plan.ReleaseBundleID
	if node.Status != "completed" {
		return node, nil
	}
	if err := s.validateBatchPlanPreflight(ctx, principal, plan); err != nil {
		return node, fmt.Errorf("%w: final preflight is no longer valid: %v", apperrors.ErrInvalidArgument, err)
	}
	if approvalStatus(plan) == "rejected" {
		node.Status, node.Summary = "failed", "delivery plan was rejected"
		return node, nil
	}
	if plan.RequiresApproval && !deliveryPlanApprovalGranted(plan) {
		if plan.Status != domaindelivery.DeliveryPlanStatusWaitingApproval {
			plan.Status = domaindelivery.DeliveryPlanStatusWaitingApproval
			plan.Impact = deliveryPlanApprovalImpact(plan, principal, "requested", "")
			plan, err = s.repository.UpdateDeliveryPlan(ctx, plan)
			if err != nil {
				return node, err
			}
			s.recordDeliveryPlanApproval(ctx, principal, plan, "requested", "")
		}
		node.Status, node.Summary = "waiting_approval", "final deployment plan is waiting for approval"
		return node, nil
	}
	node.Status, node.Summary = "completed", "final deployment plan is ready"
	return node, nil
}

func (s *Service) resolveBatchPlanBundle(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, snapshot domainworkflow.DeliveryTargetSnapshot) (string, error) {
	bundleID := snapshot.Target.ReleaseBundleID
	if snapshot.BuildNodeID != "" {
		for _, build := range run.NodeRuns {
			if build.NodeID == snapshot.BuildNodeID {
				bundleID = build.ReleaseBundleID
				break
			}
		}
	}
	if bundleID == "" && (snapshot.FrozenHelm == nil || snapshot.FrozenReleaseTarget == nil || snapshot.FrozenReleaseTarget.Helm == nil || snapshot.Target.HelmRevision == 0 && len(snapshot.FrozenReleaseTarget.Helm.ImageMappings) > 0) {
		return "", fmt.Errorf("%w: final plan requires a verified release bundle", apperrors.ErrInvalidArgument)
	}
	if bundleID == "" {
		return "", nil
	}
	return s.batchTargetBundle(ctx, principal, snapshot.Target, bundleID)
}

func (s *Service) batchPlanPreflight(ctx context.Context, principal domainidentity.Principal, frozen domainworkflow.DeliveryTargetSnapshot, plan domaindelivery.DeliveryPlan) (domaindelivery.ExecutionTask, error) {
	var task domaindelivery.ExecutionTask
	if len(plan.ManifestSnapshots)+len(plan.HelmSnapshots) != 1 {
		return task, fmt.Errorf("%w: final plan must contain one deployment snapshot", apperrors.ErrInvalidArgument)
	}
	var taskID string
	var keys []string
	if frozen.FrozenHelm != nil && len(plan.HelmSnapshots) == 1 {
		snapshot := plan.HelmSnapshots[0]
		if snapshot.ChartDigest != frozen.FrozenHelm.ChartDigest || snapshot.ExpectedRevision != frozen.FrozenHelm.ExpectedRevision || snapshot.RollbackRevision != frozen.FrozenHelm.RollbackRevision {
			return task, fmt.Errorf("%w: Helm chart or installed revision changed", apperrors.ErrInvalidArgument)
		}
		keys, taskID = domainworkflow.HelmResourceKeys(snapshot), snapshot.PreflightTaskID
		if err := s.ensureHelmPreflights(ctx, principal, plan); err != nil {
			return task, err
		}
	} else if frozen.FrozenManifest != nil && len(plan.ManifestSnapshots) == 1 {
		keys, taskID = domainmanifest.ResourceKeys(plan.ManifestSnapshots[0].ClusterID, plan.ManifestSnapshots[0].Documents, plan.ManifestSnapshots[0].GitOpsDocuments), plan.ManifestSnapshots[0].PreflightTaskID
	} else {
		return task, apperrors.ErrConflict
	}
	if !slices.Equal(frozen.ResourceKeys(), keys) {
		return task, fmt.Errorf("%w: final artifacts changed the frozen resource identities", apperrors.ErrInvalidArgument)
	}
	return s.repository.GetExecutionTask(ctx, taskID)
}

func (s *Service) validateBatchPlanPreflight(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan) error {
	if len(plan.DockerSnapshots) == 1 {
		return s.validateDockerSnapshot(ctx, principal, plan.DockerSnapshots[0], plan.DockerPrepared[plan.DockerSnapshots[0].TargetID], true)
	}
	if len(plan.HelmSnapshots) == 1 {
		return s.validateHelmSnapshot(ctx, principal, plan.HelmSnapshots[0], true)
	}
	if len(plan.ManifestSnapshots) != 1 || s.manifestDelivery == nil {
		return apperrors.ErrConflict
	}
	return s.manifestDelivery.ValidateDeliverySnapshot(ctx, principal, plan.ManifestSnapshots[0])
}

func (s *Service) executeBatchDeploy(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, snapshot domainworkflow.DeliveryTargetSnapshot, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	planNode := batchStageNode(run, node.TargetID, "plan")
	node.DeliveryPlanID, node.ReleaseBundleID = planNode.DeliveryPlanID, planNode.ReleaseBundleID
	if snapshot.FrozenDocker != nil {
		return s.executeBatchDockerDeploy(ctx, principal, snapshot, node)
	}
	capability, _ := domainworkflow.NodeExecutionFrom(ctx)
	task, err := s.repository.GetExecutionTask(ctx, capability.ResourceID("task"))
	if err == nil {
		return projectBatchTask(node, task), nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return node, err
	}
	if err := s.validateBatchConfiguration(ctx, principal, snapshot); err != nil {
		return node, err
	}
	_, err = s.ConfirmDeliveryPlan(ctx, principal, node.DeliveryPlanID)
	if err != nil {
		if task, readErr := s.repository.GetExecutionTask(ctx, capability.ResourceID("task")); readErr == nil {
			return projectBatchTask(node, task), nil
		}
		return node, err
	}
	task, err = s.repository.GetExecutionTask(ctx, capability.ResourceID("task"))
	if err != nil {
		return node, err
	}
	return projectBatchTask(node, task), nil
}

func requireBatchPlanWorker(ctx context.Context, plan domaindelivery.DeliveryPlan) error {
	if plan.Source != domainworkflow.ScopeDeliveryBatch {
		return nil
	}
	node, ok := domainworkflow.NodeExecutionFrom(ctx)
	if !ok || node.Stage != "deploy" || plan.Impact["workflowRunId"] != node.RunID || plan.Impact["workflowTargetId"] != node.TargetID {
		return fmt.Errorf("%w: delivery batch plans are executed by their active Run", apperrors.ErrConflict)
	}
	return nil
}

func (s *Service) deliveryGovernanceRequest(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan) (deliverygovernance.Request, error) {
	request := deliverygovernance.Request{
		PlanID: plan.ID, ApplicationID: plan.ApplicationID, ApplicationEnvironmentID: plan.ApplicationEnvironmentID,
		Action: string(plan.Action), ReleaseBundleID: plan.ReleaseBundleID, RequiresValidation: true,
		RequiresApproval: plan.RequiresApproval, ApprovalStatus: approvalStatus(plan), AIStatus: "available",
	}
	if plan.Source != domainworkflow.ScopeDeliveryBatch && len(plan.HelmSnapshots) == 0 {
		return request, nil
	}
	if err := requireBatchPlanWorker(ctx, plan); err != nil {
		return request, err
	}
	if len(plan.ManifestSnapshots)+len(plan.HelmSnapshots)+len(plan.DockerSnapshots) == 0 || len(plan.ManifestSnapshots) > 0 && s.manifestDelivery == nil {
		return request, fmt.Errorf("%w: final deployment preflight is missing", apperrors.ErrInvalidArgument)
	}
	for _, snapshot := range plan.ManifestSnapshots {
		if snapshot.DeliveryPlanID != plan.ID || snapshot.ApplicationEnvironmentID != plan.ApplicationEnvironmentID || snapshot.PreflightTaskID == "" {
			return request, fmt.Errorf("%w: preflight does not belong to the final plan", apperrors.ErrConflict)
		}
		if err := s.manifestDelivery.ValidateDeliverySnapshot(ctx, principal, snapshot); err != nil {
			return request, err
		}
		request.ValidatedPreflightTaskIDs = append(request.ValidatedPreflightTaskIDs, snapshot.PreflightTaskID)
	}
	for _, snapshot := range plan.HelmSnapshots {
		if snapshot.DeliveryPlanID != plan.ID || snapshot.ApplicationID != plan.ApplicationID || snapshot.ApplicationEnvironmentID != plan.ApplicationEnvironmentID || snapshot.PreflightTaskID == "" {
			return request, apperrors.ErrConflict
		}
		if err := s.validateHelmSnapshot(ctx, principal, snapshot, true); err != nil {
			return request, err
		}
		request.ValidatedPreflightTaskIDs = append(request.ValidatedPreflightTaskIDs, snapshot.PreflightTaskID)
	}
	for _, snapshot := range plan.DockerSnapshots {
		if err := s.validateDockerSnapshot(ctx, principal, snapshot, plan.DockerPrepared[snapshot.TargetID], true); err != nil {
			return request, err
		}
		request.ValidatedPreflightTaskIDs = append(request.ValidatedPreflightTaskIDs, snapshot.PreflightOperationID)
	}
	return request, nil
}
