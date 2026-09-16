package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type helmRuntimeCheck struct {
	denied bool
	calls  int
}

func (r *helmRuntimeCheck) AuthorizeHelmDelivery(context.Context, domainidentity.Principal, sohaapi.HelmDeliverySnapshot, bool) (string, error) {
	if r.denied {
		return "", apperrors.ErrAccessDenied
	}
	return "helm_direct", nil
}
func (*helmRuntimeCheck) PrepareHelmDelivery(_ context.Context, _ domainidentity.Principal, payload sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	payload.Snapshot.Operation = sohaapi.HelmDeliverySnapshotOperationInstall
	if payload.Snapshot.RollbackRevision > 0 {
		payload.Snapshot.Operation, payload.Snapshot.ExpectedRevision = sohaapi.HelmDeliverySnapshotOperationRollback, 2
	}
	payload.Snapshot.Resources = []sohaapi.HelmDeliveryResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: payload.Snapshot.Namespace, Name: "app"}}
	payload.Prepared.Manifest = "confidential-render"
	payload.Snapshot.RenderedDigest = helmrelease.SHA256([]byte(payload.Prepared.Manifest))
	values, _ := json.Marshal(payload.Prepared.Values)
	payload.Snapshot.ValuesDigest = helmrelease.SHA256(values)
	return payload, nil
}
func (r *helmRuntimeCheck) ExecuteHelmDelivery(context.Context, domainidentity.Principal, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	r.calls++
	return sohaapi.HelmExecutionTaskResult{Stopped: true}, nil
}

type helmChartCheck struct{}

func (helmChartCheck) LoadChart(context.Context, sohaapi.DeploymentTemplateHelmSource, map[string]string) (sohaapi.HelmChartInspection, []byte, error) {
	var defaults sohaapi.TemplateParameterValues
	_ = json.Unmarshal([]byte(`{"worker":{"enabled":true,"count":1}}`), &defaults)
	return sohaapi.HelmChartInspection{Name: "app", Version: "1.0.0", Digest: helmrelease.SHA256([]byte("private-chart")), DefaultValues: defaults}, []byte("private-chart"), nil
}

type helmSecretCheck struct{ revoked bool }

func (*helmSecretCheck) PinReferences(_ context.Context, _ domainidentity.Principal, refs map[string]string, target domainsecret.Target) ([]domainsecret.Reference, error) {
	if target.Type != "project" || target.Ref != "app" {
		return nil, apperrors.ErrAccessDenied
	}
	var result []domainsecret.Reference
	for alias, uri := range refs {
		result = append(result, domainsecret.Reference{Alias: alias, URI: uri, SecretID: "password", Version: 1})
	}
	return result, nil
}
func (r *helmSecretCheck) ResolvePinnedReferences(_ context.Context, _ domainidentity.Principal, refs []domainsecret.Reference, _ domainsecret.Target) (map[string]string, error) {
	if r.revoked {
		return nil, apperrors.ErrAccessDenied
	}
	values := map[string]string{}
	for _, ref := range refs {
		values[ref.Alias] = "resolved-private-value"
	}
	return values, nil
}

func TestHelmPreparationHydratesOnlyApprovedTasksAndRechecksSecrets(t *testing.T) {
	s, repo, _, _, catalog, run, _ := batchRuntimeCheck(t)
	s.applications = stubApplicationReader{app: domainapp.App{ID: "app"}, services: []domainapp.Service{{ID: "svc", ApplicationID: "app", Version: 1, Enabled: true}}}
	var values sohaapi.TemplateParameterValues
	if err := json.Unmarshal([]byte(`{"worker":{"count":2,"password":"soha://secrets/password"}}`), &values); err != nil {
		t.Fatal(err)
	}
	target := domaincatalog.ReleaseTarget{ID: "target", Enabled: true, ExecutorKind: "helm_sdk", TargetKind: "helm_release", ClusterID: "cluster", Namespace: "test", Metadata: map[string]any{"serviceId": "svc"}, Helm: &sohaapi.HelmDeliveryConfiguration{ReleaseName: "app", Source: sohaapi.DeploymentTemplateHelmSource{RepositoryURL: "https://charts.example", Chart: "app", Version: "1.0.0"}, Values: &values}}
	catalog.bindings[0].Targets = []domaincatalog.ReleaseTarget{target}
	runtime, secrets := &helmRuntimeCheck{}, &helmSecretCheck{}
	key, err := keyring.NewKey("test", strings.Repeat("k", 32), time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetHelmDelivery(HelmDeliveryDependencies{Runtime: runtime, Charts: helmChartCheck{}, Secrets: secrets, Keys: ring})
	snapshots, ciphertext, err := s.prepareHelmDelivery(context.Background(), deliveryActionPrincipal(), catalog.bindings[0], []domaincatalog.ReleaseTarget{target}, domaindelivery.DeliveryPlanInput{ID: "plan", Action: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	historicalPlan := verifyHelmHydration(t, s, repo, runtime, secrets, snapshots, ciphertext)
	runtime.denied = false
	verifyFrozenHelmBatch(t, s, repo, &run)
	verifyHistoricalHelmRollback(t, s, repo, catalog, secrets, run, historicalPlan)
}

func verifyHelmHydration(t *testing.T, s *Service, repo *batchRuntimeRepository, runtime *helmRuntimeCheck, secrets *helmSecretCheck, snapshots []sohaapi.HelmDeliverySnapshot, ciphertext string) domaindelivery.DeliveryPlan {
	t.Helper()
	if len(snapshots) != 1 || ciphertext == "" || strings.Contains(ciphertext, "resolved-private-value") {
		t.Fatal("preparation was not encrypted")
	}
	repo.plan = domaindelivery.DeliveryPlan{ID: "plan", ApplicationID: "app", ApplicationEnvironmentID: "env", Status: domaindelivery.DeliveryPlanStatusDraft, HelmSnapshots: snapshots, HelmPreparedCiphertext: ciphertext}
	snapshot := snapshots[0]
	payload := sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Preflight, Snapshot: snapshot}
	task := domaindelivery.ExecutionTask{ID: snapshot.PreflightTaskID, ApplicationID: "app", ApplicationEnvironmentID: "env", ProviderKind: "helm_direct", TaskKind: "helm_preflight", SecretPrincipal: deliveryActionPrincipal(), Payload: map[string]any{"helm": payload}}
	hydrated, err := s.HydrateExecutionTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := decodeHelmPayload(hydrated.Payload)
	if err != nil || prepared.Prepared == nil {
		t.Fatal("claimed task lost private preparation", err)
	}
	encoded, _ := json.Marshal(prepared.Prepared.Values)
	if !strings.Contains(string(encoded), `"enabled":true`) || !strings.Contains(string(encoded), `"count":2`) || !strings.Contains(string(encoded), "resolved-private-value") {
		t.Fatal("defaults, overlay or secret resolution was lost")
	}
	for _, public := range []any{repo.plan, task} {
		encoded, _ := json.Marshal(public)
		if strings.Contains(string(encoded), "confidential-render") || strings.Contains(string(encoded), "resolved-private-value") || strings.Contains(string(encoded), ciphertext) {
			t.Fatal("private preparation reached public plan/task")
		}
	}
	secrets.revoked = true
	if _, err := s.HydrateExecutionTask(context.Background(), task); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("revoked secret remained claimable", err)
	}
	secrets.revoked = false
	task.Status, task.Result = "completed", map[string]any{"helm": sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "preflighted", RenderedDigest: snapshot.RenderedDigest}}
	repo.tasks[task.ID] = task
	payload.Action = sohaapi.Apply
	apply := task
	apply.TaskKind, apply.Payload = "helm_apply", map[string]any{"helm": payload}
	if _, err := s.HydrateExecutionTask(context.Background(), apply); err == nil {
		t.Fatal("unconfirmed plan allowed apply")
	}
	repo.plan.Status = domaindelivery.DeliveryPlanStatusConfirmed
	if _, err := s.HydrateExecutionTask(context.Background(), apply); err != nil {
		t.Fatal(err)
	}
	badPreflight := task
	badPreflight.Result = map[string]any{"helm": sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "preflighted", RenderedDigest: "wrong"}}
	repo.tasks[task.ID] = badPreflight
	if _, err := s.HydrateExecutionTask(context.Background(), apply); err == nil {
		t.Fatal("mismatched preflight allowed apply")
	}
	repo.tasks[task.ID] = task
	runtime.denied = true
	if _, err := s.HydrateExecutionTask(context.Background(), apply); err == nil {
		t.Fatal("revoked runtime permission allowed apply")
	}
	if runtime.calls != 0 {
		t.Fatal("preparation or hydration performed a deployment")
	}

	return repo.plan
}

func verifyFrozenHelmBatch(t *testing.T, s *Service, repo *batchRuntimeRepository, run *domainworkflow.Run) {
	t.Helper()

	frozen, err := s.FreezeDeliveryTarget(context.Background(), deliveryActionPrincipal(), domainworkflow.DeliveryTargetInput{ID: "web", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", Action: "deploy_existing"})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.FrozenHelm == nil || frozen.FrozenHelmCiphertext == "" {
		t.Fatal("batch did not freeze the Helm intent")
	}
	s.helm.Charts = nil // Final plans must consume the frozen archive without fetching again.
	batch := domainworkflow.DeliveryBatch{ID: "batch", RootRunID: run.ID, Targets: []domainworkflow.DeliveryTargetSnapshot{frozen}}
	repo.plan = domaindelivery.DeliveryPlan{}
	batchRuntimeStep(t, s, run, batch, 1, "waiting_execution")
	batchRuntimeStep(t, s, run, batch, 1, "waiting_execution")
	preflight := repo.tasks[run.NodeRuns[1].ExecutionTaskID]
	preflight.Status = "completed"
	preflight.Result = map[string]any{"helm": sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "preflighted", RenderedDigest: repo.plan.HelmSnapshots[0].RenderedDigest}}
	repo.tasks[preflight.ID] = preflight
	batchRuntimeStep(t, s, run, batch, 1, "waiting_approval")
	if _, err := s.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve"}); err != nil {
		t.Fatal(err)
	}
	batchRuntimeStep(t, s, run, batch, 1, "completed")
	batchRuntimeStep(t, s, run, batch, 2, "waiting_execution")
	batchRuntimeStep(t, s, run, batch, 2, "waiting_execution")
	repo.status(run.NodeRuns[2].ExecutionTaskID, "completed")
	batchRuntimeStep(t, s, run, batch, 2, "completed")
	batchRuntimeStep(t, s, run, batch, 3, "waiting_execution")
	repo.status(run.NodeRuns[3].ExecutionTaskID, "completed")
	batchRuntimeStep(t, s, run, batch, 3, "completed")
	if repo.createCount != 1 || repo.tasks[run.NodeRuns[3].ExecutionTaskID].TaskKind != "helm_observe" {
		t.Fatal("batch recovery duplicated its plan or lost native health observation")
	}

}

func verifyHistoricalHelmRollback(t *testing.T, s *Service, repo *batchRuntimeRepository, catalog *stubCatalogReader, secrets *helmSecretCheck, run domainworkflow.Run, historicalPlan domaindelivery.DeliveryPlan) {
	t.Helper()
	snapshot := historicalPlan.HelmSnapshots[0]

	// Rollback must use history even if today's chart/image configuration differs.
	repo.plan = historicalPlan
	repo.tasks["historical-apply"] = domaindelivery.ExecutionTask{TaskKind: "helm_apply", Status: "completed",
		Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: snapshot}},
		Result:  map[string]any{"helm": sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "deployed", Revision: 1, RenderedDigest: snapshot.RenderedDigest}}}
	catalog.bindings[0].Targets[0].Helm.ImageMappings = []sohaapi.HelmImageMapping{{ContainerName: "new-container", Path: "/new-image", Value: sohaapi.HelmImageMappingValueImage}}
	rollback := domainworkflow.DeliveryTargetInput{ID: "rollback", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", Action: "config_update", HelmRevision: 1}
	secrets.revoked = true
	if _, err := s.FreezeDeliveryTarget(context.Background(), deliveryActionPrincipal(), rollback); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("rollback ignored revoked historical secret", err)
	}
	secrets.revoked = false
	frozenRollback, err := s.FreezeDeliveryTarget(context.Background(), deliveryActionPrincipal(), rollback)
	if err != nil {
		t.Fatal(err)
	}
	if frozenRollback.FrozenHelm == nil || frozenRollback.FrozenHelm.Operation != sohaapi.HelmDeliverySnapshotOperationRollback || frozenRollback.FrozenHelm.RollbackRevision != 1 || frozenRollback.FrozenHelm.ValuesDigest != snapshot.ValuesDigest {
		t.Fatal("rollback did not freeze the selected historical revision")
	}
	rollbackBatch := domainworkflow.DeliveryBatch{ID: "rollback", RootRunID: run.ID, Targets: []domainworkflow.DeliveryTargetSnapshot{frozenRollback}}
	rollbackBatch.Targets[0].Target.ID = run.NodeRuns[1].TargetID
	delete(repo.tasks, run.NodeRuns[1].ExecutionTaskID)
	repo.plan = domaindelivery.DeliveryPlan{}
	batchRuntimeStep(t, s, &run, rollbackBatch, 1, "waiting_execution")
	if repo.plan.HelmSnapshots[0].RollbackRevision != 1 || repo.plan.HelmSnapshots[0].Operation != sohaapi.HelmDeliverySnapshotOperationRollback || repo.plan.HelmSnapshots[0].ValuesDigest != snapshot.ValuesDigest {
		t.Fatal("batch lost rollback or replaced historical images at final planning")
	}
	rollback.HelmRevision = 99
	if _, err := s.FreezeDeliveryTarget(context.Background(), deliveryActionPrincipal(), rollback); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("rollback accepted a revision with no delivery history", err)
	}
}

type helmStartCallbackCheck struct {
	ExecutionController
	task   domaindelivery.ExecutionTask
	result domaindelivery.ExecutionCallbackInput
}

func (e *helmStartCallbackCheck) RecordCallback(_ context.Context, input domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error) {
	if input.Status != "running" {
		e.result = input
		e.task.Status = input.Status
	}
	return e.task, nil
}
func TestHelmWorkerAcknowledgesCancellationBeforeMutation(t *testing.T) {
	task := domaindelivery.ExecutionTask{CallbackToken: "token", Status: "canceling", Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: sohaapi.HelmDeliverySnapshot{DeliveryPlanID: "plan"}}}}
	controller, runtime := &helmStartCallbackCheck{task: task}, &helmRuntimeCheck{}
	s := &Service{execution: controller, helm: HelmDeliveryDependencies{Runtime: runtime}}
	s.executeLocalHelmTask(context.Background(), task)
	result, ok := controller.result.Payload["helm"].(sohaapi.HelmExecutionTaskResult)
	if runtime.calls != 0 || controller.result.Status != "canceled" || !ok || !result.Stopped {
		t.Fatal("cancellation before start was not acknowledged")
	}
}

type helmConfirmFailureRepository struct {
	*batchRuntimeRepository
	failConfirmed bool
}

func (r *helmConfirmFailureRepository) UpdateDeliveryPlan(ctx context.Context, plan domaindelivery.DeliveryPlan) (domaindelivery.DeliveryPlan, error) {
	if r.failConfirmed && plan.Status == domaindelivery.DeliveryPlanStatusConfirmed {
		r.failConfirmed = false
		return domaindelivery.DeliveryPlan{}, errors.New("confirmed persistence failed")
	}
	return r.batchRuntimeRepository.UpdateDeliveryPlan(ctx, plan)
}

func TestHelmConfirmRecoversPersistedDispatchAndReturnsOriginalTask(t *testing.T) {
	for _, status := range []string{domaindelivery.DeliveryPlanStatusDraft, domaindelivery.DeliveryPlanStatusConfirming} {
		t.Run(status, func(t *testing.T) {
			s, repo, _, _, catalog, _, _ := batchRuntimeCheck(t)
			s.applications = stubApplicationReader{app: domainapp.App{ID: "app"}, services: []domainapp.Service{{ID: "svc", ApplicationID: "app", Version: 1, Enabled: true}}}
			target := domaincatalog.ReleaseTarget{ID: "target", Enabled: true, ExecutorKind: "helm_sdk", TargetKind: "helm_release", ClusterID: "cluster", Namespace: "test", Metadata: map[string]any{"serviceId": "svc"}, Helm: &sohaapi.HelmDeliveryConfiguration{ReleaseName: "app", Source: sohaapi.DeploymentTemplateHelmSource{RepositoryURL: "https://charts.example", Chart: "app", Version: "1.0.0"}}}
			catalog.bindings[0].Targets, catalog.bindings[0].ReleasePolicy.RequiresApproval = []domaincatalog.ReleaseTarget{target}, false
			key, err := keyring.NewKey("test", strings.Repeat("k", 32), time.Now(), nil)
			if err != nil {
				t.Fatal(err)
			}
			ring, err := keyring.New(key, nil)
			if err != nil {
				t.Fatal(err)
			}
			s.SetHelmDelivery(HelmDeliveryDependencies{Runtime: &helmRuntimeCheck{}, Charts: helmChartCheck{}, Keys: ring})
			snapshots, ciphertext, err := s.prepareHelmDelivery(context.Background(), deliveryActionPrincipal(), catalog.bindings[0], []domaincatalog.ReleaseTarget{target}, domaindelivery.DeliveryPlanInput{ID: "plan", Action: "deploy"})
			if err != nil {
				t.Fatal(err)
			}
			repo.plan = domaindelivery.DeliveryPlan{ID: "plan", Action: "deploy", ApplicationID: "app", ApplicationEnvironmentID: "env", TargetID: "target", TargetIDs: []string{"target"}, Status: status, HelmSnapshots: snapshots, HelmPreparedCiphertext: ciphertext}
			snapshot := snapshots[0]
			repo.tasks[snapshot.PreflightTaskID] = domaindelivery.ExecutionTask{ID: snapshot.PreflightTaskID, Status: "completed", Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Preflight, Snapshot: snapshot}}, Result: map[string]any{"helm": sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "preflighted", RenderedDigest: snapshot.RenderedDigest}}}
			s.repository = &helmConfirmFailureRepository{batchRuntimeRepository: repo, failConfirmed: true}
			if _, err := s.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), "plan"); err == nil || !strings.Contains(err.Error(), "confirmed persistence failed") {
				t.Fatalf("final confirmation failure was not injected: %v", err)
			}
			taskID := helmTaskID(context.Background(), snapshot, sohaapi.Apply)
			if repo.plan.Status != domaindelivery.DeliveryPlanStatusConfirming || repo.tasks[taskID].TaskKind != "helm_apply" || len(repo.tasks) != 2 {
				t.Fatal("failure lost the persisted dispatch")
			}
			// An acknowledgement retry must not use today's changed source or create another task.
			catalog.bindings[0].Targets[0].Helm.Source.Version = "2.0.0"
			for range 2 {
				result, err := s.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), "plan")
				if err != nil || result.Plan.Status != domaindelivery.DeliveryPlanStatusConfirmed || result.Plan.ConfirmedAt == nil || result.Result.RelatedIDs.ExecutionTaskID != taskID || len(repo.tasks) != 2 {
					t.Fatalf("confirmation recovery duplicated or lost the original task: %+v %v", result, err)
				}
			}
			task := repo.tasks[taskID]
			task.ApplicationID = "another-app"
			repo.tasks[taskID] = task
			if _, err := s.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), "plan"); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatal("recovery accepted an unrelated task", err)
			}
		})
	}
}
