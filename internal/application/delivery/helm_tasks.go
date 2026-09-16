package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

func decodeHelmPayload(payload map[string]any) (sohaapi.HelmExecutionTaskPayload, error) {
	var result sohaapi.HelmExecutionTaskPayload
	encoded, err := json.Marshal(payload["helm"])
	if err == nil {
		err = json.Unmarshal(encoded, &result)
	}
	if err != nil || result.Snapshot.DeliveryPlanID == "" || !result.Action.Valid() {
		return result, fmt.Errorf("%w: invalid Helm task payload", apperrors.ErrInvalidArgument)
	}
	return result, nil
}

func helmTaskID(ctx context.Context, snapshot sohaapi.HelmDeliverySnapshot, action sohaapi.HelmExecutionTaskPayloadAction) string {
	taskID := "task:" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(snapshot.DeliveryPlanID+":"+snapshot.TargetID+":"+string(action))).String()
	if action == sohaapi.Preflight {
		taskID = snapshot.PreflightTaskID
	}
	if node, ok := domainworkflow.NodeExecutionFrom(ctx); ok {
		taskID = node.ResourceID("task")
	}
	return taskID
}

func (s *Service) queueHelmTask(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.HelmDeliverySnapshot, action sohaapi.HelmExecutionTaskPayloadAction, timeoutSeconds int) (domaindelivery.ExecutionTask, error) {
	provider, err := s.helm.Runtime.AuthorizeHelmDelivery(ctx, principal, snapshot, action == sohaapi.Apply)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	taskID := helmTaskID(ctx, snapshot, action)
	if task, err := s.repository.GetExecutionTask(ctx, taskID); err == nil {
		return task, nil
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return domaindelivery.ExecutionTask{}, err
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = snapshot.TimeoutSeconds + 30
	}
	now := time.Now().UTC()
	task := domaindelivery.ExecutionTask{ID: taskID, ApplicationID: snapshot.ApplicationID, ApplicationEnvironmentID: snapshot.ApplicationEnvironmentID,
		ReleaseBundleID: snapshot.ReleaseBundleID,
		TaskKind:        "helm_" + string(action), ProviderKind: provider, TargetKind: "helm_release", Status: "queued",
		QueueKey: "helm:" + snapshot.ClusterID + ":" + snapshot.Namespace + ":" + snapshot.ReleaseName, LockKey: snapshot.DeliveryPlanID + ":" + snapshot.TargetID + ":" + string(action),
		TimeoutSeconds: timeoutSeconds, CallbackToken: uuid.NewString(), SecretPrincipal: principal, SecretTarget: domainsecret.Target{Type: "project", Ref: snapshot.ApplicationID},
		Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: action, Snapshot: snapshot}}, Result: map[string]any{}, CreatedAt: now, UpdatedAt: now}
	return s.repository.CreateExecutionTask(ctx, task)
}

func standaloneHelmDeployPlan(plan domaindelivery.DeliveryPlan) bool {
	return plan.Source != domainworkflow.ScopeDeliveryBatch && plan.Action == domaindelivery.ApplicationDeliveryActionDeploy &&
		len(plan.HelmSnapshots) > 0 && len(plan.HelmSnapshots) == len(plan.TargetIDs) && len(plan.ManifestSnapshots) == 0
}

// A persisted apply task means confirmation already dispatched the frozen intent.
// Recover its acknowledgement without revalidating or reapplying today's configuration.
func (s *Service) recoverHelmPlanConfirmation(ctx context.Context, plan domaindelivery.DeliveryPlan) (domaindelivery.DeliveryPlanConfirmResult, bool, error) {
	result := domaindelivery.DeliveryPlanConfirmResult{Plan: plan}
	if !standaloneHelmDeployPlan(plan) || (plan.Status != domaindelivery.DeliveryPlanStatusConfirming && plan.Status != domaindelivery.DeliveryPlanStatusConfirmed) {
		return result, false, nil
	}
	result.Result = domaindelivery.ApplicationDeliveryActionResult{Action: plan.Action, ApplicationID: plan.ApplicationID, ApplicationEnvironmentID: plan.ApplicationEnvironmentID}
	for _, snapshot := range plan.HelmSnapshots {
		task, err := s.repository.GetExecutionTask(ctx, helmTaskID(ctx, snapshot, sohaapi.Apply))
		if errors.Is(err, apperrors.ErrNotFound) && plan.Status == domaindelivery.DeliveryPlanStatusConfirming {
			return result, false, nil
		}
		if err != nil {
			return result, true, err
		}
		payload, err := decodeHelmPayload(task.Payload)
		if err != nil || task.TaskKind != "helm_apply" || task.ApplicationID != plan.ApplicationID || task.ApplicationEnvironmentID != plan.ApplicationEnvironmentID || payload.Action != sohaapi.Apply || !reflect.DeepEqual(payload.Snapshot, snapshot) {
			return result, true, apperrors.ErrConflict
		}
		result.Result.RelatedIDs.ExecutionTaskIDs = append(result.Result.RelatedIDs.ExecutionTaskIDs, task.ID)
	}
	result.Result.RelatedIDs.ExecutionTaskID = result.Result.RelatedIDs.ExecutionTaskIDs[0]
	if plan.Status == domaindelivery.DeliveryPlanStatusConfirming {
		now := time.Now().UTC()
		plan.Status, plan.ConfirmedAt, plan.UpdatedAt = domaindelivery.DeliveryPlanStatusConfirmed, &now, now
		updated, err := s.repository.UpdateDeliveryPlan(ctx, plan)
		if err != nil {
			return result, true, err
		}
		result.Plan = updated
	}
	return result, true, nil
}

func (s *Service) ensureHelmPreflights(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan) error {
	for _, snapshot := range plan.HelmSnapshots {
		if _, err := s.queueHelmTask(ctx, principal, snapshot, sohaapi.Preflight, 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validateHelmSnapshot(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.HelmDeliverySnapshot, requirePreflight bool) error {
	if s.helm.Runtime == nil {
		return fmt.Errorf("%w: Helm delivery is unavailable", apperrors.ErrClusterUnready)
	}
	if err := s.authorizeRuntimeScope(ctx, principal, snapshot.ApplicationID, snapshot.ApplicationEnvironmentID); err != nil {
		return err
	}
	if err := s.authorizeApplicationDeliveryAction(ctx, principal, snapshot.ApplicationEnvironmentID, domaindelivery.ApplicationDeliveryActionDeploy); err != nil {
		return err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, snapshot.ApplicationEnvironmentID)
	if err != nil {
		return err
	}
	service, err := s.helmTargetService(ctx, principal, snapshot.ApplicationID, snapshot.ServiceID)
	if err != nil {
		return err
	}
	var selected *domaincatalog.ReleaseTarget
	for _, target := range binding.Targets {
		if target.ID == snapshot.TargetID && target.Enabled && target.Helm != nil {
			selected = &target
			break
		}
	}
	if selected == nil || selected.ClusterID != snapshot.ClusterID || selected.Namespace != snapshot.Namespace || selected.Helm.ReleaseName != snapshot.ReleaseName || service.Version != snapshot.ServiceVersion {
		return fmt.Errorf("%w: Helm target changed; create a new plan", apperrors.ErrConflict)
	}
	digest, err := s.deliveryConfigurationDigest(ctx, principal, service, binding, nil, selected)
	if err != nil {
		return err
	}
	if digest != snapshot.ConfigurationDigest {
		return fmt.Errorf("%w: Helm configuration changed; create a new plan", apperrors.ErrConflict)
	}
	if _, err := s.manifestArtifacts(ctx, principal, snapshot.ApplicationID, snapshot.ServiceID, snapshot.ReleaseBundleID); err != nil {
		return err
	}
	if _, err := s.helm.Runtime.AuthorizeHelmDelivery(ctx, principal, snapshot, true); err != nil {
		return err
	}
	if requirePreflight {
		return s.validateHelmPreflight(ctx, snapshot)
	}
	return nil
}

func (s *Service) validateHelmPreflight(ctx context.Context, snapshot sohaapi.HelmDeliverySnapshot) error {
	task, err := s.repository.GetExecutionTask(ctx, snapshot.PreflightTaskID)
	if err != nil {
		return err
	}
	payload, err := decodeHelmPayload(task.Payload)
	var result sohaapi.HelmExecutionTaskResult
	encoded, resultErr := json.Marshal(task.Result["helm"])
	if resultErr == nil {
		resultErr = json.Unmarshal(encoded, &result)
	}
	if err != nil || task.Status != "completed" || payload.Action != sohaapi.Preflight || !reflect.DeepEqual(payload.Snapshot, snapshot) || resultErr != nil || helmrelease.ValidateCompletedResult(payload, result) != nil {
		return fmt.Errorf("%w: Helm preflight is incomplete or belongs to another plan", apperrors.ErrConflict)
	}
	return nil
}

// HydrateExecutionTask is called only after a runner claims a persisted task.
// Public task reads and callbacks never load confidential chart/values contents.
func (s *Service) HydrateExecutionTask(ctx context.Context, task domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	if !strings.HasPrefix(task.TaskKind, "helm_") {
		return task, nil
	}
	payload, err := decodeHelmPayload(task.Payload)
	if err != nil {
		return task, err
	}
	if task.TaskKind != "helm_"+string(payload.Action) || task.ApplicationID != payload.Snapshot.ApplicationID || task.ApplicationEnvironmentID != payload.Snapshot.ApplicationEnvironmentID {
		return task, apperrors.ErrConflict
	}
	plan, err := s.repository.GetDeliveryPlan(ctx, payload.Snapshot.DeliveryPlanID)
	if err != nil {
		return task, err
	}
	selected := slices.ContainsFunc(plan.HelmSnapshots, func(snapshot sohaapi.HelmDeliverySnapshot) bool {
		return reflect.DeepEqual(snapshot, payload.Snapshot)
	})
	if !selected || plan.ApplicationID != task.ApplicationID || plan.ApplicationEnvironmentID != task.ApplicationEnvironmentID {
		return task, fmt.Errorf("%w: Helm task is not part of this plan", apperrors.ErrConflict)
	}
	if payload.Action == sohaapi.Apply && (plan.Status != domaindelivery.DeliveryPlanStatusConfirming && plan.Status != domaindelivery.DeliveryPlanStatusConfirmed || plan.RequiresApproval && !deliveryPlanApprovalGranted(plan)) {
		return task, fmt.Errorf("%w: Helm deployment requires a confirmed approved plan", apperrors.ErrConflict)
	}
	if err := s.validateHelmSnapshot(ctx, task.SecretPrincipal, payload.Snapshot, payload.Action != sohaapi.Preflight); err != nil {
		return task, err
	}
	provider, err := s.helm.Runtime.AuthorizeHelmDelivery(ctx, task.SecretPrincipal, payload.Snapshot, payload.Action == sohaapi.Apply)
	if err != nil || provider != task.ProviderKind {
		return task, fmt.Errorf("%w: Helm runtime changed", apperrors.ErrConflict)
	}
	prepared, err := s.helmPlanPreparation(ctx, task.SecretPrincipal, plan, payload.Snapshot)
	if err != nil {
		return task, err
	}
	if payload.Action != sohaapi.Observe {
		payload.Prepared = prepared.Payload.Prepared
	}
	task.Payload = maps.Clone(task.Payload)
	task.Payload["helm"] = payload
	return task, nil
}

func (s *Service) helmPlanPreparation(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan, snapshot sohaapi.HelmDeliverySnapshot) (helmPreparation, error) {
	prepared, err := s.storedHelmPreparation(plan, snapshot)
	if err != nil {
		return prepared, err
	}
	if len(prepared.References) > 0 {
		if s.helm.Secrets == nil {
			return prepared, apperrors.ErrAccessDenied
		}
		_, err = s.helm.Secrets.ResolvePinnedReferences(ctx, principal, prepared.References, domainsecret.Target{Type: "project", Ref: snapshot.ApplicationID})
	}
	return prepared, err
}

func (s *Service) storedHelmPreparation(plan domaindelivery.DeliveryPlan, snapshot sohaapi.HelmDeliverySnapshot) (helmPreparation, error) {
	var prepared helmPreparation
	if plan.ID != snapshot.DeliveryPlanID || plan.ApplicationID != snapshot.ApplicationID || plan.ApplicationEnvironmentID != snapshot.ApplicationEnvironmentID {
		return prepared, apperrors.ErrConflict
	}
	plaintext, err := secretcrypto.DecryptStringWithKeyring(s.helm.Keys, plan.HelmPreparedCiphertext)
	if err != nil {
		return prepared, fmt.Errorf("%w: Helm plan preparation is unavailable", apperrors.ErrConflict)
	}
	var entries map[string]helmPreparation
	if err := json.Unmarshal([]byte(plaintext), &entries); err != nil {
		return prepared, apperrors.ErrConflict
	}
	prepared, exists := entries[snapshot.TargetID]
	if !exists || prepared.Payload.Prepared == nil || !reflect.DeepEqual(prepared.Payload.Snapshot, snapshot) {
		return prepared, fmt.Errorf("%w: Helm task differs from the frozen plan", apperrors.ErrConflict)
	}
	return prepared, nil
}

func (s *Service) validateHelmDelivery(ctx context.Context, principal domainidentity.Principal, targets []domaincatalog.ReleaseTarget, input domaindelivery.ApplicationDeliveryActionInput) error {
	if input.Action == domaindelivery.ApplicationDeliveryActionBuild {
		return nil
	}
	count := 0
	for _, target := range targets {
		if target.Helm == nil {
			continue
		}
		found := false
		for _, snapshot := range input.HelmSnapshots {
			if snapshot.TargetID == target.ID {
				if input.Action != domaindelivery.ApplicationDeliveryActionDeploy {
					return fmt.Errorf("%w: Helm targets require a deploy plan", apperrors.ErrInvalidArgument)
				}
				if err := s.validateHelmSnapshot(ctx, principal, snapshot, true); err != nil {
					return err
				}
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%w: Helm target requires a preflighted plan", apperrors.ErrInvalidArgument)
		}
		count++
	}
	if count != len(input.HelmSnapshots) {
		return fmt.Errorf("%w: Helm target selection changed", apperrors.ErrConflict)
	}
	return nil
}

func (s *Service) applyHelmDelivery(ctx context.Context, principal domainidentity.Principal, targetID string, input domaindelivery.ApplicationDeliveryActionInput, result *domaindelivery.ApplicationDeliveryActionResult) error {
	for _, snapshot := range input.HelmSnapshots {
		if snapshot.TargetID != targetID {
			continue
		}
		task, err := s.queueHelmTask(ctx, principal, snapshot, sohaapi.Apply, 0)
		if err != nil {
			return err
		}
		result.RelatedIDs.ExecutionTaskIDs = append(result.RelatedIDs.ExecutionTaskIDs, task.ID)
		if result.RelatedIDs.ExecutionTaskID == "" {
			result.RelatedIDs.ExecutionTaskID = task.ID
		}
		return nil
	}
	return apperrors.ErrConflict
}
