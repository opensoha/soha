package manifest

import (
	"errors"
	"testing"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestQueuedMigrationFirstClaimRetainsProvenHealthyBaseline(t *testing.T) {
	for _, scenario := range []string{"valid_pending", "valid_drifted", "valid_reconciling", "retry", "degraded", "unknown_health", "missing_evidence", "wrong_digest", "wrong_template", "replaced_uid", "wrong_receipt_digest", "wrong_receipt_scope", "wrong_receipt_generation", "wrong_generation", "other_plan", "current_unhealthy", "missing_inventory", "wrong_receipt_action"} {
		t.Run(scenario, func(t *testing.T) {
			s, repo, target, selectVersion := newMigrationSnapshotFixture()
			inventory := []domainmanifest.ResourceInventory{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "payments", Name: "settings", UID: "original", DesiredObjectDigest: "resource-v1", Health: "healthy"}}
			oldPayload := domainmanifest.TaskPayload{Action: "observe", DeploymentID: "deployment", Generation: 1, Revision: 1, RenderedDigest: "healthy-one", ClusterID: repo.binding.ClusterID, Namespace: repo.binding.Namespace, Documents: []domainmanifest.RenderedDocument{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "payments", Name: "settings", ContentDigest: "resource-v1"}}}
			oldResult := domainmanifest.TaskResult{Action: "observe", DeploymentID: "deployment", Generation: 1, RenderedDigest: "healthy-one", Inventory: inventory}
			healthy := domaindelivery.ExecutionTask{ID: "healthy-old", ApplicationID: "payments", ApplicationEnvironmentID: "payments-dev", Status: "completed", Payload: structMap(oldPayload), Result: structMap(oldResult)}
			repo.tasks[healthy.ID] = healthy
			repo.deployment.Status.AppliedDigest = "healthy-one"
			repo.deployment.Status.LastKnownGoodRevision = 1
			repo.deployment.Status.Inventory = append([]domainmanifest.ResourceInventory(nil), inventory...)
			repo.deployment.Status.Conditions = []domainmanifest.Condition{{Type: "Healthy", Status: "true", ObservedGeneration: 1, EvidenceRefs: []string{healthy.ID}}}
			selectVersion(2)
			snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "migration", domainmanifest.DeliveryArtifacts{})
			requireSnapshotTestNoError(t, err)
			approveSnapshotPreflight(repo, snapshot)
			_, task, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snapshot)
			requireSnapshotTestNoError(t, err)
			fixture := &rolloutTaskFixture{snapshotRepository: repo, plan: domaindelivery.DeliveryPlan{ID: snapshot.DeliveryPlanID, ApplicationID: "payments", ApplicationEnvironmentID: "payments-dev", Status: "confirmed", RequiresApproval: true, ManifestSnapshots: []domainmanifest.DeliverySnapshot{snapshot}}}
			s.repository, s.delivery = fixture, fixture
			s.executionPrincipals = &rootExecutionPrincipal{principal: testPrincipal()}
			task.AttemptCount = 1
			repo.deployment.Status.Phase = domainmanifest.DeploymentPhasePending
			mutateQueuedMigrationClaim(scenario, repo, fixture, &task, healthy)
			err = s.ValidateRootCRExecutionTask(t.Context(), task)
			valid := scenario == "valid_pending" || scenario == "valid_drifted" || scenario == "valid_reconciling"
			if valid && err != nil || !valid && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("claim result: %v", err)
			}
			if repo.deployment.Status.Phase == domainmanifest.DeploymentPhaseConverged || repo.deployment.Status.AppliedTemplate.TemplateVersion != 1 {
				t.Fatal("claim fabricated a healthy target baseline")
			}
			if err := s.ValidateDeliverySnapshot(t.Context(), testPrincipal(), snapshot); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("ordinary validation reused pending baseline: %v", err)
			}
		})
	}
}

func mutateQueuedMigrationClaim(scenario string, repo *snapshotRepository, fixture *rolloutTaskFixture, task *domaindelivery.ExecutionTask, healthy domaindelivery.ExecutionTask) {
	switch scenario {
	case "valid_drifted":
		repo.deployment.Status.Phase = domainmanifest.DeploymentPhaseDrifted
	case "valid_reconciling":
		repo.deployment.Status.Phase = domainmanifest.DeploymentPhaseReconciling
	case "retry":
		task.AttemptCount = 2
	case "degraded":
		repo.deployment.Status.Phase = domainmanifest.DeploymentPhaseDegraded
	case "unknown_health":
		repo.deployment.Status.Conditions[0].Status = "false"
	case "missing_evidence":
		repo.deployment.Status.Conditions[0].EvidenceRefs = nil
	case "current_unhealthy":
		repo.deployment.Status.Inventory[0].Health = "degraded"
	case "missing_inventory":
		repo.deployment.Status.Inventory = nil
	case "wrong_digest":
		repo.deployment.Status.AppliedDigest = "different"
	case "wrong_template":
		repo.deployment.Status.AppliedTemplate.TemplateDigest = "different"
	case "replaced_uid":
		repo.deployment.Status.Inventory[0].UID = "replacement"
	case "wrong_receipt_action":
		healthy.Result["action"] = "preflight"
		repo.tasks[healthy.ID] = healthy
	case "wrong_receipt_digest":
		healthy.Result["renderedDigest"] = "different"
		repo.tasks[healthy.ID] = healthy
	case "wrong_receipt_scope":
		healthy.ApplicationID = "other"
		repo.tasks[healthy.ID] = healthy
	case "wrong_receipt_generation":
		healthy.Result["generation"] = float64(9)
		repo.tasks[healthy.ID] = healthy
	case "wrong_generation":
		repo.deployment.Generation++
	case "other_plan":
		fixture.plan.ManifestSnapshots = nil
	}
}
