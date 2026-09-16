package manifest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func (s *DeclarativeService) GetTaskRollout(ctx context.Context, principal domainidentity.Principal, taskID string) (sohaapi.ProgressiveRolloutStatus, error) {
	_, payload, _, err := s.rolloutTask(ctx, principal, taskID, false)
	if err != nil {
		return sohaapi.ProgressiveRolloutStatus{}, err
	}
	payload.Action = domainmanifest.TaskActionObserve
	return s.executeTaskRollout(ctx, payload)
}

func (s *DeclarativeService) ControlTaskRollout(ctx context.Context, principal domainidentity.Principal, taskID string, input sohaapi.ProgressiveRolloutControlInput) (sohaapi.ProgressiveRolloutStatus, error) {
	if !slices.Contains([]string{"pause", "promote", "abort"}, string(input.Action)) || strings.TrimSpace(input.UID) == "" || strings.TrimSpace(input.ResourceVersion) == "" {
		return sohaapi.ProgressiveRolloutStatus{}, fmt.Errorf("%w: rollout action and current resource identity are required", apperrors.ErrInvalidArgument)
	}
	if err := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger")); err != nil {
		return sohaapi.ProgressiveRolloutStatus{}, err
	}
	task, payload, item, err := s.rolloutTask(ctx, principal, taskID, true)
	if err != nil {
		return sohaapi.ProgressiveRolloutStatus{}, err
	}
	payload.Action, payload.RolloutControl = domainmanifest.TaskActionRolloutControl, &input
	state, err := s.executeTaskRollout(ctx, payload)
	if err == nil {
		s.base.record(ctx, principal, "delivery.manifest.rollout."+string(input.Action), item, fmt.Sprintf("task %s deployment %s generation %d operation %s resource %s/%s uid %s resourceVersion %s", task.ID, payload.DeploymentID, payload.Generation, payload.IdempotencyKey, state.Namespace, state.Name, input.UID, input.ResourceVersion))
	}
	return state, err
}

func (s *DeclarativeService) rolloutTask(ctx context.Context, principal domainidentity.Principal, taskID string, control bool) (domaindelivery.ExecutionTask, domainmanifest.TaskPayload, domainmanifest.Package, error) {
	var payload domainmanifest.TaskPayload
	var item domainmanifest.Package
	if s.delivery == nil {
		return domaindelivery.ExecutionTask{}, payload, item, fmt.Errorf("%w: delivery task reader is unavailable", apperrors.ErrUnsupportedOperation)
	}
	task, err := s.delivery.GetExecutionTask(ctx, principal, strings.TrimSpace(taskID))
	if err != nil {
		return task, payload, item, err
	}
	payload, err = progressiveTaskPayload(task)
	if err != nil {
		return task, payload, item, err
	}
	if control && task.Status != "running" && task.Status != "dispatching" {
		return task, payload, item, fmt.Errorf("%w: rollout controls require a running execution", apperrors.ErrConflict)
	}
	deployment, err := s.repository.GetDeployment(ctx, payload.DeploymentID)
	if err != nil {
		return task, payload, item, err
	}
	snapshot := deployment.Spec.DeliverySnapshot
	if !rolloutDeploymentMatchesTask(deployment, task, payload) {
		return task, payload, item, fmt.Errorf("%w: task no longer matches the current frozen deployment", apperrors.ErrConflict)
	}
	plan, err := s.delivery.GetConfirmedDeliveryPlan(ctx, principal, snapshot.DeliveryPlanID)
	if err != nil {
		return task, payload, item, err
	}
	if plan.ApplicationID != task.ApplicationID || plan.ApplicationEnvironmentID != task.ApplicationEnvironmentID || !slices.ContainsFunc(plan.ManifestSnapshots, func(candidate domainmanifest.DeliverySnapshot) bool { return reflect.DeepEqual(candidate, *snapshot) }) {
		return task, payload, item, fmt.Errorf("%w: rollout is outside its approved plan", apperrors.ErrConflict)
	}
	action := domainaccess.ActionView
	if control {
		action = domainaccess.ActionTrigger
	}
	item, app, err := s.bindingPackage(ctx, principal, payload.PackageID, action)
	if err != nil {
		return task, payload, item, err
	}
	if item.ApplicationID != task.ApplicationID {
		return task, payload, item, fmt.Errorf("%w: rollout application changed", apperrors.ErrConflict)
	}
	// Authorize the frozen scope, even if an unrelated current template edit or
	// disabled target would prevent starting another deployment.
	environment, err := s.base.environments.GetApplicationEnvironment(ctx, principal, task.ApplicationEnvironmentID)
	if err != nil {
		return task, payload, item, err
	}
	if environment.ApplicationID != task.ApplicationID {
		return task, payload, item, fmt.Errorf("%w: rollout environment changed", apperrors.ErrConflict)
	}
	connection, err := s.base.loadCluster(ctx, payload.ClusterID)
	if err != nil {
		return task, payload, item, err
	}
	if err := s.base.authorizeManifest(ctx, principal, action, item, app, connection, firstNonEmptyString(environment.EnvironmentKey, environment.EnvironmentID), payload.Namespace); err != nil {
		return task, payload, item, err
	}
	return task, payload, item, nil
}

func progressiveTaskPayload(task domaindelivery.ExecutionTask) (domainmanifest.TaskPayload, error) {
	payload, err := decodeTaskPayload(task.Payload)
	if err != nil || task.TaskKind != domainmanifest.TaskKindApply || payload.Action != domainmanifest.TaskActionApply || payload.DeploymentID == "" || payload.RolloutControl != nil || !hasRolloutDocuments(payload.Documents) {
		return payload, fmt.Errorf("%w: task is not a progressive deployment", apperrors.ErrInvalidArgument)
	}
	return payload, nil
}

func rolloutDeploymentMatchesTask(deployment domainmanifest.Deployment, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) bool {
	snapshot := deployment.Spec.DeliverySnapshot
	return deployment.Generation == payload.Generation && deployment.PackageID == payload.PackageID && deployment.BindingID == payload.BindingID && snapshot != nil && snapshot.DeliveryPlanID != "" && snapshot.PackageID == payload.PackageID && snapshot.BindingID == payload.BindingID && snapshot.ApplicationEnvironmentID == task.ApplicationEnvironmentID && snapshot.ClusterID == payload.ClusterID && snapshot.Namespace == payload.Namespace && snapshot.RenderedDigest == payload.RenderedDigest && reflect.DeepEqual(snapshot.Documents, payload.Documents) && len(payload.GitOpsDocuments) == 0
}

func (s *DeclarativeService) executeTaskRollout(ctx context.Context, payload domainmanifest.TaskPayload) (sohaapi.ProgressiveRolloutStatus, error) {
	connection, err := s.base.loadCluster(ctx, payload.ClusterID)
	if err != nil {
		return sohaapi.ProgressiveRolloutStatus{}, err
	}
	runtime := s.direct
	if connection != nil && connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		if s.agents == nil || !slices.Contains(connection.Summary.Capabilities, resourceruntime.ManifestAgentCapability) {
			return sohaapi.ProgressiveRolloutStatus{}, fmt.Errorf("%w: upgrade the Agent for native rollout controls", apperrors.ErrUnsupportedOperation)
		}
		runtime, err = s.agents(*connection)
		if err != nil {
			return sohaapi.ProgressiveRolloutStatus{}, err
		}
	}
	if runtime == nil {
		return sohaapi.ProgressiveRolloutStatus{}, fmt.Errorf("%w: rollout runtime is unavailable", apperrors.ErrUnsupportedOperation)
	}
	query, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, err := runtime.Execute(query, payload)
	if apierrors.IsConflict(err) || errors.Is(err, resourceruntime.ErrResourceOwnership) {
		err = fmt.Errorf("%w: rollout changed; refresh before controlling it", apperrors.ErrConflict)
	}
	if err != nil {
		return sohaapi.ProgressiveRolloutStatus{}, err
	}
	if result.Rollout == nil || result.Rollout.OperationID != payload.IdempotencyKey {
		return sohaapi.ProgressiveRolloutStatus{}, fmt.Errorf("%w: native rollout no longer belongs to this execution", apperrors.ErrConflict)
	}
	return *result.Rollout, nil
}

func hasRolloutDocuments(documents []domainmanifest.RenderedDocument) bool {
	return slices.ContainsFunc(documents, func(document domainmanifest.RenderedDocument) bool {
		return document.APIVersion == "argoproj.io/v1alpha1" && document.Kind == "Rollout"
	})
}
