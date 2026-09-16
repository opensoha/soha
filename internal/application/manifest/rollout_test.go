package manifest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type rolloutTaskFixture struct {
	*snapshotRepository
	plan             domaindelivery.DeliveryPlan
	task             domaindelivery.ExecutionTask
	readErr, planErr error
	calls            []domainmanifest.TaskPayload
}

func (f *rolloutTaskFixture) GetDeployment(context.Context, string) (domainmanifest.Deployment, error) {
	return f.deployment, nil
}
func (f *rolloutTaskFixture) GetExecutionTask(context.Context, domainidentity.Principal, string) (domaindelivery.ExecutionTask, error) {
	return f.task, f.readErr
}
func (f *rolloutTaskFixture) GetConfirmedDeliveryPlan(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryPlan, error) {
	return f.plan, f.planErr
}
func (f *rolloutTaskFixture) Execute(_ context.Context, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	f.calls = append(f.calls, payload)
	return domainmanifest.TaskResult{Rollout: &sohaapi.ProgressiveRolloutStatus{UID: "native-uid", ResourceVersion: "42", OperationID: payload.IdempotencyKey}}, nil
}

func TestRolloutControlsRequireCurrentApprovedFrozenTask(t *testing.T) {
	for _, scenario := range []string{"promote", "pause", "abort", "finished observation", "new template edit", "task scope denied", "plan not approved", "old generation", "frozen content changed", "plan snapshot changed", "wrong application", "finished control", "wrong permission", "scope denied", "missing identity"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, _, _ := newSnapshotTestService()
			payload, _, fixture, principal, input := rolloutScenarioFixture(repo, scenario)
			service.repository, service.delivery, service.direct = fixture, fixture, fixture
			if scenario == "scope denied" {
				service.base.authorizer = testAuthorizer{deny: true}
			}
			var err error
			if scenario == "finished observation" {
				_, err = service.GetTaskRollout(t.Context(), principal, "task")
			} else {
				_, err = service.ControlTaskRollout(t.Context(), principal, "task", input)
			}
			assertRolloutScenario(t, scenario, err, fixture, payload, input)
		})
	}
}

func rolloutScenarioFixture(repo *snapshotRepository, scenario string) (domainmanifest.TaskPayload, domainmanifest.DeliverySnapshot, *rolloutTaskFixture, domainidentity.Principal, sohaapi.ProgressiveRolloutControlInput) {
	payload := domainmanifest.TaskPayload{Action: domainmanifest.TaskActionApply, PackageID: "package", BindingID: "manifest-binding", DeploymentID: "deployment", Generation: 3, ClusterID: "dev-1", Namespace: "payments", RenderedDigest: "frozen", IdempotencyKey: "operation", Documents: []domainmanifest.RenderedDocument{{APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout", Namespace: "payments", Name: "web", Content: "frozen", ContentDigest: "digest"}}}
	snapshot := domainmanifest.DeliverySnapshot{DeliveryPlanID: "plan", TargetID: "target", PackageID: payload.PackageID, BindingID: payload.BindingID, ApplicationEnvironmentID: "payments-dev", ClusterID: payload.ClusterID, Namespace: payload.Namespace, RenderedDigest: payload.RenderedDigest, Documents: append([]domainmanifest.RenderedDocument(nil), payload.Documents...)}
	fixture := &rolloutTaskFixture{snapshotRepository: repo, task: domaindelivery.ExecutionTask{ID: "task", TaskKind: domainmanifest.TaskKindApply, Status: "running", ApplicationID: "payments", ApplicationEnvironmentID: "payments-dev", Payload: structMap(payload)}, plan: domaindelivery.DeliveryPlan{ID: "plan", Status: "confirmed", ApplicationID: "payments", ApplicationEnvironmentID: "payments-dev", ManifestSnapshots: []domainmanifest.DeliverySnapshot{snapshot}}}
	snapshot.Documents = append([]domainmanifest.RenderedDocument(nil), snapshot.Documents...)
	repo.deployment = domainmanifest.Deployment{ID: payload.DeploymentID, Generation: payload.Generation, PackageID: payload.PackageID, BindingID: payload.BindingID, Spec: domainmanifest.DeploymentSpec{DeliverySnapshot: &snapshot}}
	principal := testPrincipal()
	input := sohaapi.ProgressiveRolloutControlInput{Action: "promote", UID: "native-uid", ResourceVersion: "42"}
	switch scenario {
	case "pause", "abort":
		input.Action = sohaapi.ProgressiveRolloutControlInputAction(scenario)
	case "finished observation", "finished control":
		fixture.task.Status = "completed"
	case "new template edit":
		repo.base.item.UpdatedAt = time.Now().Add(time.Hour)
	case "task scope denied":
		fixture.readErr = apperrors.ErrAccessDenied
	case "plan not approved":
		fixture.planErr = apperrors.ErrConflict
	case "old generation":
		repo.deployment.Generation++
	case "frozen content changed":
		snapshot.Documents[0].Content = "new"
	case "plan snapshot changed":
		fixture.plan.ManifestSnapshots = nil
	case "wrong application":
		fixture.task.ApplicationID = "outside"
	case "wrong permission":
		principal.Roles = []string{"editor"}
	case "missing identity":
		input.UID = ""
	}
	return payload, snapshot, fixture, principal, input
}

func assertRolloutScenario(t *testing.T, scenario string, err error, fixture *rolloutTaskFixture, payload domainmanifest.TaskPayload, input sohaapi.ProgressiveRolloutControlInput) {
	t.Helper()
	accepted := scenario == "promote" || scenario == "pause" || scenario == "abort" || scenario == "finished observation" || scenario == "new template edit"
	if accepted {
		if err != nil || len(fixture.calls) != 1 {
			t.Fatalf("control failed: calls=%d err=%v", len(fixture.calls), err)
		}
		call := fixture.calls[0]
		if !reflect.DeepEqual(call.Documents, payload.Documents) || call.Generation != 3 || call.IdempotencyKey != "operation" || scenario != "finished observation" && !reflect.DeepEqual(call.RolloutControl, &input) {
			t.Fatalf("control changed frozen task: %+v", call)
		}
	} else if err == nil || len(fixture.calls) != 0 {
		t.Fatalf("unsafe control reached native runtime: calls=%d err=%v", len(fixture.calls), err)
	}
	if scenario == "wrong permission" && !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("permission failure: %v", err)
	}
}
