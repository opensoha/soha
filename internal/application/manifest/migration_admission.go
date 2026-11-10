package manifest

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// A selected Plan changes phase to pending before its first task is claimed.
// Reuse the previous generation's completed healthy receipt, never a desired
// version or a guessed baseline. Ordinary Plan creation/confirmation cannot use it.
func (s *DeclarativeService) pendingMigrationBaseline(ctx context.Context, applicationID string, current domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot) (domainmanifest.Deployment, error) {
	if !pendingBaselineSelected(current, snapshot) {
		return current, nil
	}
	switch current.Status.Phase {
	case domainmanifest.DeploymentPhasePending, domainmanifest.DeploymentPhaseReconciling, domainmanifest.DeploymentPhaseDrifted:
	default:
		return current, nil
	}
	for _, condition := range current.Status.Conditions {
		if condition.Type != "Healthy" || condition.Status != "true" || condition.ObservedGeneration != snapshot.ExpectedGeneration || len(condition.EvidenceRefs) != 1 {
			continue
		}
		if s.tasks == nil {
			return current, apperrors.ErrUnsupportedOperation
		}
		task, err := s.tasks.GetExecutionTaskInternal(ctx, condition.EvidenceRefs[0])
		if err != nil {
			return current, err
		}
		payload, err := decodeTaskPayload(task.Payload)
		if err != nil {
			return current, err
		}
		result := decodeTaskResult(task.Result)
		if !migrationReceiptMatches(task, payload, result, applicationID, current, snapshot) {
			return current, fmt.Errorf("%w: previous healthy execution receipt no longer matches", apperrors.ErrConflict)
		}
		if !inventoryHealthy(result.Inventory, payload.Documents, payload.GitOpsDocuments) || !inventoryHealthy(current.Status.Inventory, payload.Documents, payload.GitOpsDocuments) || result.Drift != nil && result.Drift.Drifted || !sameMigrationResourceUIDs(result.Inventory, current.Status.Inventory) {
			return current, fmt.Errorf("%w: previous healthy resource identity no longer matches", apperrors.ErrConflict)
		}
		// This is an assessment view only. Never persist a fabricated phase or advance
		// AppliedTemplate/last-known-good; the new rollout must prove its own health.
		current.Status.Phase = domainmanifest.DeploymentPhaseConverged
		return current, nil
	}
	return current, nil
}

func sameMigrationResourceUIDs(left, right []domainmanifest.ResourceInventory) bool {
	identities := func(items []domainmanifest.ResourceInventory) []string {
		keys := make([]string, 0, len(items))
		for _, item := range items {
			if item.UID == "" {
				return nil
			}
			group, _, _ := strings.Cut(item.APIVersion, "/")
			if !strings.Contains(item.APIVersion, "/") {
				group = ""
			}
			keys = append(keys, strings.Join([]string{group, item.Kind, item.Namespace, item.Name, item.UID}, "/"))
		}
		slices.Sort(keys)
		return keys
	}
	a, b := identities(left), identities(right)
	return len(a) > 0 && slices.Equal(a, b)
}

func pendingBaselineSelected(current domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot) bool {
	return deliveryAlreadySelected(current, snapshot) && reflect.DeepEqual(current.Spec.DeliverySnapshot, &snapshot) && snapshot.Migration != nil && snapshot.Migration.FromVersion > 0 && current.Status.AppliedDigest != "" && current.Status.AppliedTemplate != nil && current.Status.AppliedTemplate.TemplateVersion == snapshot.Migration.FromVersion
}

func migrationReceiptMatches(task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload, result domainmanifest.TaskResult, applicationID string, current domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot) bool {
	return task.Status == "completed" && task.ApplicationID == applicationID && task.ApplicationEnvironmentID == snapshot.ApplicationEnvironmentID && payload.DeploymentID == current.ID && payload.Generation == snapshot.ExpectedGeneration && payload.RenderedDigest == current.Status.AppliedDigest && payload.ClusterID == snapshot.ClusterID && payload.Namespace == snapshot.Namespace && slices.Contains([]string{"apply", "observe", "repair", "rollback"}, payload.Action) && result.Action == payload.Action && result.Generation == payload.Generation && result.DeploymentID == current.ID && result.RenderedDigest == payload.RenderedDigest && !result.Stale && len(result.Diagnostics) == 0 && current.Status.LastKnownGoodRevision == payload.Revision
}
