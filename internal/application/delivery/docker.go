package delivery

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DockerDelivery interface {
	DeliveryProjectAccess(context.Context, domainidentity.Principal, sohaapi.DockerDeliverySnapshot, string) ([]domaindelivery.AccessCandidate, error)
	FreezeDeliveryProject(context.Context, domainidentity.Principal, string, string) (domaindocker.PreparedDeliveryProject, error)
	PrepareDeliveryProject(context.Context, domainidentity.Principal, string, map[string]string, map[string]string) (domaindocker.PreparedDeliveryProject, error)
	ValidateDeliveryProject(context.Context, domainidentity.Principal, sohaapi.DockerDeliverySnapshot, string) error
	QueueDeliveryProject(context.Context, domainidentity.Principal, sohaapi.DockerDeliverySnapshot, string, string) (domaindocker.Operation, error)
	GetOperation(context.Context, domainidentity.Principal, string) (domaindocker.Operation, error)
	CancelDeliveryProject(context.Context, string, string, string) (domaindocker.Operation, error)
	AssessProject(context.Context, domainidentity.Principal, appdocker.ProjectAssessmentInput) (domainaigateway.CapabilityAssessment, error)
}

func (s *Service) SetDockerDelivery(runtime DockerDelivery) { s.docker = runtime }

func frozenDockerCiphertext(snapshot domainworkflow.DeliveryTargetSnapshot) string {
	if snapshot.FrozenDocker == nil {
		return ""
	}
	return snapshot.FrozenDocker.Ciphertext
}

func (s *Service) freezeDockerDeployment(ctx context.Context, principal domainidentity.Principal, service domainapp.Service, binding domaincatalog.ApplicationEnvironment, target domaincatalog.ReleaseTarget, snapshot *domainworkflow.DeliveryTargetSnapshot) error {
	if s.docker == nil {
		return apperrors.ErrClusterUnready
	}
	if snapshot.Target.HelmRevision > 0 {
		return apperrors.ErrInvalidArgument
	}
	if err := s.authorizeApplicationDeliveryAction(ctx, principal, binding.ID, domaindelivery.ApplicationDeliveryActionDeploy); err != nil {
		return err
	}
	frozen, err := s.docker.FreezeDeliveryProject(ctx, principal, target.Docker.HostID, target.Docker.ProjectID)
	if err != nil {
		return err
	}
	if snapshot.Target.Action != "build_deploy" {
		if snapshot.Target.ReleaseBundleID == "" {
			return fmt.Errorf("%w: Docker deployment requires a verified release bundle", apperrors.ErrInvalidArgument)
		}
		artifacts, err := s.manifestArtifacts(ctx, principal, service.ApplicationID, service.ID, snapshot.Target.ReleaseBundleID)
		if err != nil {
			return err
		}
		if _, err := s.docker.PrepareDeliveryProject(ctx, principal, frozen.Ciphertext, target.Docker.ImageMappings, artifacts.ContainerImages); err != nil {
			return err
		}
	}
	snapshot.FrozenDocker, snapshot.FrozenReleaseTarget, snapshot.Target.ReleaseTargetID = &frozen, &target, target.ID
	return nil
}

func (s *Service) prepareDockerDelivery(ctx context.Context, principal domainidentity.Principal, binding domaincatalog.ApplicationEnvironment, targets []domaincatalog.ReleaseTarget, input domaindelivery.DeliveryPlanInput) ([]sohaapi.DockerDeliverySnapshot, map[string]string, error) {
	var snapshots []sohaapi.DockerDeliverySnapshot
	prepared := map[string]string{}
	for _, target := range targets {
		if target.Docker == nil {
			continue
		}
		if s.docker == nil || input.Action != domaindelivery.ApplicationDeliveryActionDeploy || input.Source != domainworkflow.ScopeDeliveryBatch || input.FrozenDockerCiphertext == "" || len(targets) != 1 {
			return nil, nil, fmt.Errorf("%w: create a delivery batch to deploy this Docker target", apperrors.ErrInvalidArgument)
		}
		serviceID, _ := target.Metadata["serviceId"].(string)
		artifacts, err := s.manifestArtifacts(ctx, principal, binding.ApplicationID, serviceID, input.ReleaseBundleID)
		if err != nil {
			return nil, nil, err
		}
		project, err := s.docker.PrepareDeliveryProject(ctx, principal, input.FrozenDockerCiphertext, target.Docker.ImageMappings, artifacts.ContainerImages)
		if err != nil {
			return nil, nil, err
		}
		if project.HostID != target.Docker.HostID || project.ProjectID != target.Docker.ProjectID {
			return nil, nil, apperrors.ErrConflict
		}
		snapshot := sohaapi.DockerDeliverySnapshot{DeliveryPlanID: input.ID, ApplicationID: binding.ApplicationID, ApplicationEnvironmentID: binding.ID, ServiceID: serviceID, TargetID: target.ID, HostID: project.HostID, ProjectID: project.ProjectID, ProjectDigest: project.ProjectDigest, RenderedDigest: project.RenderedDigest, ReleaseBundleID: input.ReleaseBundleID, ExpectedServices: project.ExpectedServices, Images: project.Images}
		snapshot.PreflightOperationID, err = appdocker.DeliveryProjectOperationID(principal, snapshot, "validate")
		if err != nil {
			return nil, nil, err
		}
		snapshot.DeployOperationID, err = appdocker.DeliveryProjectOperationID(principal, snapshot, "delivery_deploy")
		if err != nil {
			return nil, nil, err
		}
		snapshots = append(snapshots, snapshot)
		prepared[target.ID] = project.Ciphertext
	}
	return snapshots, prepared, nil
}

func (s *Service) ensureDockerPreflights(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan) error {
	for _, snapshot := range plan.DockerSnapshots {
		if _, err := s.docker.QueueDeliveryProject(ctx, principal, snapshot, plan.DockerPrepared[snapshot.TargetID], "validate"); err != nil {
			return err
		}
	}
	return nil
}

func validDockerOperation(operation domaindocker.Operation, snapshot sohaapi.DockerDeliverySnapshot, action string) bool {
	id := snapshot.PreflightOperationID
	if action == "delivery_deploy" {
		id = snapshot.DeployOperationID
	}
	return operation.ID == id && operation.HostID == snapshot.HostID && operation.ProjectID == snapshot.ProjectID && operation.OperationKind == "project_deploy" && operation.Payload["deliveryPlanId"] == snapshot.DeliveryPlanID && operation.Payload["renderedDigest"] == snapshot.RenderedDigest && operation.Payload["releaseTargetId"] == snapshot.TargetID && operation.Payload["action"] == action
}

func (s *Service) validateDockerSnapshot(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.DockerDeliverySnapshot, ciphertext string, requirePreflight bool) error {
	if s.docker == nil {
		return apperrors.ErrClusterUnready
	}
	if err := s.docker.ValidateDeliveryProject(ctx, principal, snapshot, ciphertext); err != nil {
		return err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, snapshot.ApplicationEnvironmentID)
	if err != nil {
		return err
	}
	if binding.ApplicationID != snapshot.ApplicationID {
		return apperrors.ErrConflict
	}
	for _, target := range binding.Targets {
		if target.ID != snapshot.TargetID || !target.Enabled {
			continue
		}
		if target.Docker == nil || target.ExecutorKind != "docker_compose" || target.Metadata["serviceId"] != snapshot.ServiceID || target.Docker.HostID != snapshot.HostID || target.Docker.ProjectID != snapshot.ProjectID {
			return apperrors.ErrConflict
		}
		artifacts, err := s.manifestArtifacts(ctx, principal, snapshot.ApplicationID, snapshot.ServiceID, snapshot.ReleaseBundleID)
		if err != nil {
			return err
		}
		for service, container := range target.Docker.ImageMappings {
			if artifacts.ContainerImages[container] == "" || snapshot.Images[service] != artifacts.ContainerImages[container] {
				return apperrors.ErrConflict
			}
		}
		if !requirePreflight {
			return nil
		}
		return s.validateDockerPreflight(ctx, principal, snapshot)
	}
	return apperrors.ErrConflict
}

func (s *Service) validateDockerPreflight(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.DockerDeliverySnapshot) error {
	operation, err := s.docker.GetOperation(ctx, principal, snapshot.PreflightOperationID)
	if err != nil {
		return err
	}
	if !validDockerOperation(operation, snapshot, "validate") || operation.Status != "completed" || operation.Result["validatedRenderedDigest"] != snapshot.RenderedDigest {
		return fmt.Errorf("%w: frozen Docker preflight has not passed", apperrors.ErrConflict)
	}
	return nil
}

func (s *Service) validateDockerDelivery(ctx context.Context, principal domainidentity.Principal, targets []domaincatalog.ReleaseTarget, input domaindelivery.ApplicationDeliveryActionInput) error {
	count := 0
	for _, target := range targets {
		if target.Docker == nil && target.ExecutorKind != "docker_compose" {
			continue
		}
		count++
		found := false
		for _, snapshot := range input.DockerSnapshots {
			if snapshot.TargetID != target.ID {
				continue
			}
			found = true
			if err := s.validateDockerSnapshot(ctx, principal, snapshot, input.DockerPrepared[target.ID], true); err != nil {
				return err
			}
		}
		if !found {
			return fmt.Errorf("%w: Docker deployments require a confirmed delivery plan", apperrors.ErrConflict)
		}
	}
	if count != len(input.DockerSnapshots) {
		return apperrors.ErrConflict
	}
	return nil
}

func (s *Service) applyDockerDelivery(ctx context.Context, principal domainidentity.Principal, targetID string, input domaindelivery.ApplicationDeliveryActionInput, result *domaindelivery.ApplicationDeliveryActionResult) error {
	for _, snapshot := range input.DockerSnapshots {
		if snapshot.TargetID != targetID {
			continue
		}
		operation, err := s.docker.QueueDeliveryProject(ctx, principal, snapshot, input.DockerPrepared[targetID], "delivery_deploy")
		if err != nil {
			return err
		}
		result.DockerOperationIDs = append(result.DockerOperationIDs, operation.ID)
		return nil
	}
	return apperrors.ErrConflict
}

func projectDockerOperation(node domainworkflow.NodeRun, operation domaindocker.Operation) domainworkflow.NodeRun {
	node.DockerOperationID = operation.ID
	switch operation.Status {
	case "completed":
		node.Status, node.Summary = "completed", "Docker operation completed"
	case "failed", "callback_timeout":
		node.Status, node.Summary = "failed", "Docker operation failed"
	case "canceled":
		if operation.Result["cancellationAcknowledged"] == true {
			node.Status, node.Summary = "canceled", "Docker executor stop confirmed"
		} else {
			node.Status, node.Summary = "canceling", "Docker stop requires executor confirmation"
		}
	case "canceling":
		node.Status, node.Summary = "canceling", "waiting for Docker executor stop"
	default:
		node.Status, node.Summary = "waiting_execution", "waiting for Docker operation"
	}
	return node
}

func (s *Service) batchDockerPreflight(ctx context.Context, principal domainidentity.Principal, frozen domainworkflow.DeliveryTargetSnapshot, plan domaindelivery.DeliveryPlan, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	if len(plan.DockerSnapshots) != 1 || len(plan.HelmSnapshots)+len(plan.ManifestSnapshots) != 0 {
		return node, apperrors.ErrConflict
	}
	snapshot := plan.DockerSnapshots[0]
	if frozen.FrozenDocker.ProjectDigest != snapshot.ProjectDigest || !reflect.DeepEqual(frozen.ResourceKeys(), []string{"docker:" + snapshot.HostID + ":" + snapshot.ProjectID}) {
		return node, apperrors.ErrConflict
	}
	operation, err := s.docker.GetOperation(ctx, principal, snapshot.PreflightOperationID)
	if errors.Is(err, apperrors.ErrNotFound) {
		if err := s.ensureDockerPreflights(ctx, principal, plan); err != nil {
			return node, err
		}
		operation, err = s.docker.GetOperation(ctx, principal, snapshot.PreflightOperationID)
	}
	if err != nil {
		return node, err
	}
	if !validDockerOperation(operation, snapshot, "validate") {
		return node, apperrors.ErrConflict
	}
	return projectDockerOperation(node, operation), nil
}

func (s *Service) executeBatchDockerDeploy(ctx context.Context, principal domainidentity.Principal, frozen domainworkflow.DeliveryTargetSnapshot, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	plan, err := s.repository.GetDeliveryPlan(ctx, node.DeliveryPlanID)
	if err != nil {
		return node, err
	}
	if len(plan.DockerSnapshots) != 1 {
		return node, apperrors.ErrConflict
	}
	snapshot := plan.DockerSnapshots[0]
	operation, err := s.docker.GetOperation(ctx, principal, snapshot.DeployOperationID)
	if errors.Is(err, apperrors.ErrNotFound) {
		if err := s.validateBatchConfiguration(ctx, principal, frozen); err != nil {
			return node, err
		}
		if _, err := s.ConfirmDeliveryPlan(ctx, principal, plan.ID); err != nil {
			return node, err
		}
		operation, err = s.docker.GetOperation(ctx, principal, snapshot.DeployOperationID)
	}
	if err != nil {
		return node, err
	}
	if !validDockerOperation(operation, snapshot, "delivery_deploy") {
		return node, apperrors.ErrConflict
	}
	if plan.Status == domaindelivery.DeliveryPlanStatusConfirming {
		if _, err := s.ConfirmDeliveryPlan(ctx, principal, plan.ID); err != nil {
			return node, err
		}
	}
	return projectDockerOperation(node, operation), nil
}

func (s *Service) recoverDockerPlanConfirmation(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan) (domaindelivery.DeliveryPlanConfirmResult, bool, error) {
	result := domaindelivery.DeliveryPlanConfirmResult{Plan: plan}
	if plan.Status != domaindelivery.DeliveryPlanStatusConfirming || len(plan.DockerSnapshots) == 0 {
		return result, false, nil
	}
	for _, snapshot := range plan.DockerSnapshots {
		operation, err := s.docker.GetOperation(ctx, principal, snapshot.DeployOperationID)
		if errors.Is(err, apperrors.ErrNotFound) {
			return result, false, nil
		}
		if err != nil {
			return result, false, err
		}
		if !validDockerOperation(operation, snapshot, "delivery_deploy") {
			return result, false, apperrors.ErrConflict
		}
		result.Result.DockerOperationIDs = append(result.Result.DockerOperationIDs, operation.ID)
	}
	now := time.Now().UTC()
	plan.Status, plan.ConfirmedAt, plan.UpdatedAt = domaindelivery.DeliveryPlanStatusConfirmed, &now, now
	var err error
	result.Plan, err = s.repository.UpdateDeliveryPlan(ctx, plan)
	return result, true, err
}

func (s *Service) observeBatchDockerRuntime(ctx context.Context, principal domainidentity.Principal, frozen domainworkflow.DeliveryTargetSnapshot, previous, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	plan, err := s.repository.GetDeliveryPlan(ctx, previous.DeliveryPlanID)
	if err != nil {
		return node, err
	}
	if len(plan.DockerSnapshots) != 1 {
		return node, apperrors.ErrConflict
	}
	snapshot := plan.DockerSnapshots[0]
	assessment, err := s.docker.AssessProject(ctx, principal, appdocker.ProjectAssessmentInput{ProjectID: snapshot.ProjectID, AfterOperationID: snapshot.DeployOperationID, ExpectedServices: snapshot.ExpectedServices, ExpectedImages: snapshot.Images})
	if err != nil {
		return node, err
	}
	node.DockerOperationID = snapshot.DeployOperationID
	node.Status, node.Summary = "waiting_execution", assessment.Summary
	if assessment.Verdict == "satisfied" {
		node.Status = "completed"
	}
	timeout := frozen.HealthTimeoutSeconds
	if timeout <= 0 {
		timeout = 300
	}
	if started, err := time.Parse(time.RFC3339, node.StartedAt); err == nil && node.Status != "completed" && time.Since(started) > time.Duration(timeout)*time.Second {
		node.Status, node.Summary = "failed", "fresh Docker runtime evidence did not satisfy the deployment before timeout"
	}
	return node, nil
}

func (s *Service) cancelBatchDockerStage(ctx context.Context, run domainworkflow.Run, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	if node.Stage == "health" {
		node.Status, node.Summary = "canceled", "runtime observation stopped; deployed containers retained"
		return node, nil
	}
	ctx = domainworkflow.WithNodeExecution(ctx, run, node)
	execution, _ := domainworkflow.NodeExecutionFrom(ctx)
	planID := node.DeliveryPlanID
	if node.Stage == "plan" {
		planID = execution.ResourceID("plan")
	}
	if node.Stage == "deploy" && planID == "" {
		planID = batchStageNode(run, node.TargetID, "plan").DeliveryPlanID
	}
	if planID == "" {
		return node, apperrors.ErrConflict
	}
	plan, err := s.repository.GetDeliveryPlan(ctx, planID)
	if errors.Is(err, apperrors.ErrNotFound) {
		node.Status, node.Summary = "canceled", "Docker stage stopped before plan creation"
		return node, nil
	}
	if err != nil {
		return node, err
	}
	if len(plan.DockerSnapshots) != 1 {
		return node, apperrors.ErrConflict
	}
	operationID := plan.DockerSnapshots[0].PreflightOperationID
	if node.Stage == "deploy" {
		operationID = plan.DockerSnapshots[0].DeployOperationID
	}
	operation, err := s.docker.CancelDeliveryProject(ctx, operationID, plan.ID, run.StopSummary)
	if errors.Is(err, apperrors.ErrNotFound) {
		node.Status, node.Summary = "canceled", "Docker stage stopped before dispatch"
		return node, nil
	}
	if err != nil {
		return node, err
	}
	if node.Stage == "plan" && operation.Status == "completed" {
		node.Status, node.Summary = "canceled", "delivery stopped after Docker preflight"
		return node, nil
	}
	return projectDockerOperation(node, operation), nil
}
