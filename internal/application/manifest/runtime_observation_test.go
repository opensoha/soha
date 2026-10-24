package manifest

import (
	"context"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"testing"
)

type continuousSnapshotRepository struct {
	*snapshotRepository
	items []domainmanifest.Deployment
}

func (r *continuousSnapshotRepository) ListContinuousDeployments(context.Context, int) ([]domainmanifest.Deployment, error) {
	return r.items, nil
}

func TestAutomaticObservationWaitsForPlanApply(t *testing.T) {
	for _, scenario := range []string{"pending_plan", "failed_plan", "applied_plan", "legacy_pending", "same_digest_failed_plan", "transient_observe_failure"} {
		t.Run(scenario, func(t *testing.T) {
			s, repo, renderer, _ := newSnapshotTestService()
			snapshot := &domainmanifest.DeliverySnapshot{BindingID: repo.binding.ID, ClusterID: repo.binding.ClusterID, Namespace: repo.binding.Namespace, Revision: 1, RenderedDigest: "next", Documents: []domainmanifest.RenderedDocument{{Kind: "ConfigMap", APIVersion: "v1", Namespace: repo.binding.Namespace, Name: "settings"}}}
			deployment := domainmanifest.Deployment{ID: "deployment", PackageID: repo.base.item.ID, BindingID: repo.binding.ID, Generation: 2, Spec: domainmanifest.DeploymentSpec{DesiredRevision: 1, DesiredDigest: "next", DeliverySnapshot: snapshot}, Status: domainmanifest.DeploymentStatus{AppliedDigest: "old", Phase: domainmanifest.DeploymentPhasePending}}
			if scenario == "failed_plan" || scenario == "same_digest_failed_plan" {
				deployment.Status.Phase = domainmanifest.DeploymentPhaseDegraded
				deployment.Status.Conditions = []domainmanifest.Condition{{Type: "Synced", Status: "false", Reason: "TaskFailed", ObservedGeneration: 2}}
			}
			if scenario == "same_digest_failed_plan" {
				deployment.Status.AppliedDigest = "next"
			}
			if scenario == "transient_observe_failure" {
				deployment.Status.AppliedDigest = "next"
				deployment.Status.Phase = domainmanifest.DeploymentPhaseDegraded
				deployment.Status.Conditions = []domainmanifest.Condition{{Type: "Drifted", Status: "false", Reason: "TaskFailed", ObservedGeneration: 2}}
			}
			if scenario == "applied_plan" {
				deployment.Status.AppliedDigest = "next"
			}
			if scenario == "legacy_pending" {
				deployment.Spec.DeliverySnapshot = nil
			}
			s.repository = &continuousSnapshotRepository{snapshotRepository: repo, items: []domainmanifest.Deployment{deployment}}
			s.observeContinuousDeployments(t.Context())
			expected := 0
			if scenario == "applied_plan" || scenario == "legacy_pending" || scenario == "transient_observe_failure" {
				expected = 1
			}
			if len(repo.tasks) != expected {
				t.Fatalf("queued %d observations, want %d", len(repo.tasks), expected)
			}
			if expected == 0 && renderer.calls != 0 {
				t.Fatal("pending plan rendered unexecuted desired state")
			}
		})
	}
}

func TestQueuedObservationCannotClearFailedPlanWrite(t *testing.T) {
	for _, taskStatus := range []string{"running", "completed", "failed"} {
		t.Run(taskStatus, func(t *testing.T) {
			s, repo, _, _ := newSnapshotTestService()
			repo.deployment = domainmanifest.Deployment{ID: "deployment", Generation: 2, Spec: domainmanifest.DeploymentSpec{DeliverySnapshot: &domainmanifest.DeliverySnapshot{}, DesiredDigest: "same"}, Status: domainmanifest.DeploymentStatus{AppliedDigest: "same", Phase: domainmanifest.DeploymentPhaseDegraded, LastErrorCode: "manifest_task_failed", LastErrorMessage: "original failure", LastExecutionTaskID: "failed-write", Conditions: []domainmanifest.Condition{{Type: "Synced", Status: "false", Reason: "TaskFailed", ObservedGeneration: 2}}}}
			s.repository = &migrationStatusRepository{snapshotRepository: repo}
			payload := domainmanifest.TaskPayload{Action: "observe", DeploymentID: "deployment", Generation: 2, RenderedDigest: "same"}
			task := domaindelivery.ExecutionTask{ID: "late-observation", Status: taskStatus, Result: structMap(domainmanifest.TaskResult{Action: "observe", DeploymentID: "deployment", Generation: 2, RenderedDigest: "same"})}
			requireSnapshotTestNoError(t, s.recordDeploymentTaskResult(t.Context(), task, payload))
			status := repo.deployment.Status
			if status.Phase != domainmanifest.DeploymentPhaseDegraded || status.LastErrorCode != "manifest_task_failed" || status.LastErrorMessage != "original failure" || status.LastExecutionTaskID != "failed-write" {
				t.Fatalf("observation erased write failure: %+v", status)
			}
		})
	}
}
