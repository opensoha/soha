package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	"github.com/opensoha/soha/internal/application/deliverygovernance"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type batchRuntimeRepository struct {
	planRepository
	tasks      map[string]domaindelivery.ExecutionTask
	bundle     domaindelivery.ReleaseBundle
	promotions map[string]domaindelivery.ReleaseBundle
}

func (r *batchRuntimeRepository) CreateExecutionTask(ctx context.Context, task domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	if node, ok := domainworkflow.NodeExecutionFrom(ctx); ok {
		task.ID, task.Payload = node.ResourceID("task"), node.Metadata(task.Payload)
	}
	if current, exists := r.tasks[task.ID]; exists {
		return current, nil
	}
	r.tasks[task.ID] = task
	return task, nil
}

func (r *batchRuntimeRepository) GetExecutionTask(_ context.Context, id string) (domaindelivery.ExecutionTask, error) {
	task, ok := r.tasks[id]
	if !ok {
		return task, apperrors.ErrNotFound
	}
	return task, nil
}
func (r *batchRuntimeRepository) ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	var tasks []domaindelivery.ExecutionTask
	for _, task := range r.tasks {
		tasks = append(tasks, task)
	}
	return tasks, nil
}
func (r *batchRuntimeRepository) GetReleaseBundle(_ context.Context, id string) (domaindelivery.ReleaseBundle, error) {
	if id == r.bundle.ID {
		return r.bundle, nil
	}
	if bundle, ok := r.promotions[id]; ok {
		return bundle, nil
	}
	return domaindelivery.ReleaseBundle{}, apperrors.ErrNotFound
}
func (r *batchRuntimeRepository) CreateReleaseBundle(ctx context.Context, bundle domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	bundle.ID, bundle.Metadata = node.ResourceID("bundle"), node.Metadata(bundle.Metadata)
	if r.promotions == nil {
		r.promotions = map[string]domaindelivery.ReleaseBundle{}
	}
	if existing, ok := r.promotions[bundle.ID]; ok {
		return existing, nil
	}
	r.promotions[bundle.ID] = bundle
	return bundle, nil
}
func (r *batchRuntimeRepository) GetDeliveryPlan(_ context.Context, id string) (domaindelivery.DeliveryPlan, error) {
	if r.plan.ID != id || id == "" {
		return domaindelivery.DeliveryPlan{}, apperrors.ErrNotFound
	}
	return r.plan, nil
}
func (r *batchRuntimeRepository) CreateDeliveryPlan(ctx context.Context, input domaindelivery.DeliveryPlanInput, user string) (domaindelivery.DeliveryPlan, error) {
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	input.Impact = node.Metadata(input.Impact)
	input.Impact["workflowTargetId"] = node.TargetID
	plan, err := r.planRepository.CreateDeliveryPlan(ctx, input, user)
	plan.ReleaseBundleID = input.ReleaseBundleID
	r.plan = plan
	return plan, err
}
func (r *batchRuntimeRepository) newTask(ctx context.Context, kind, bundle string, payload map[string]any) domaindelivery.ExecutionTask {
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	task := domaindelivery.ExecutionTask{ID: node.ResourceID("task"), ApplicationID: "app", ApplicationEnvironmentID: "env", ReleaseBundleID: bundle, TaskKind: kind, Status: "queued", Payload: node.Metadata(payload)}
	if kind == "build" {
		task.ApplicationEnvironmentID = r.bundle.ApplicationEnvironmentID
	}
	r.tasks[task.ID] = task
	return task
}
func (r *batchRuntimeRepository) status(id, status string) {
	task := r.tasks[id]
	task.Status = status
	if task.TaskKind == "build" && status == "completed" {
		task.Result = map[string]any{"image": r.bundle.ArtifactRef, "imageDigest": r.bundle.ArtifactDigest}
	}
	r.tasks[id] = task
}

type batchRuntimeBuild struct {
	stubBuildReader
	repo     *batchRuntimeRepository
	created  int
	prepared int
}

func (b *batchRuntimeBuild) PrepareDeliveryBuild(_ context.Context, _ domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Prepared, error) {
	b.prepared++
	return domainbuild.Prepared{Input: input, Fingerprint: "fixed-build"}, nil
}

func TestValidateDeliveryTargetOnlyReadsOwnedReferences(t *testing.T) {
	for _, scenario := range []string{"valid", "service", "source", "environment", "release-target", "bundle"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, builder, manifest, catalog, _, _ := batchRuntimeCheck(t)
			apps, ok := service.applications.(stubApplicationReader)
			if !ok {
				t.Fatal("unexpected application reader")
			}
			apps.app.BuildSources = []domainapp.BuildSource{{ID: "source", Enabled: true}}
			target := domainworkflow.DeliveryTargetInput{ID: "web", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", Action: "build_deploy", ReleaseTargetID: "target"}
			switch scenario {
			case "service":
				target.ServiceID = "foreign-service"
			case "source":
				apps.app.BuildSources = nil
			case "environment":
				catalog.bindings[0].ApplicationID = "foreign-app"
			case "release-target":
				target.ReleaseTargetID = "foreign-target"
			case "bundle":
				target.ReleaseBundleID = "missing-bundle"
			}
			service.applications = apps
			prepared := builder.prepared
			err := service.ValidateDeliveryTarget(context.Background(), deliveryActionPrincipal(), target)
			if (scenario == "valid") != (err == nil) {
				t.Fatalf("reference validation: %v", err)
			}
			if builder.prepared != prepared || builder.created != 0 || manifest.created != 0 || len(repo.tasks) != 0 || repo.createCount != 0 {
				t.Fatal("reference validation prepared or created execution")
			}
		})
	}
}
func (b *batchRuntimeBuild) TriggerFrozen(ctx context.Context, _ domainidentity.Principal, _ domainbuild.Prepared) (domainbuild.Record, error) {
	b.created++
	b.repo.newTask(ctx, "build", "bundle", map[string]any{"image": "registry.example/api:v1", "serviceId": "svc", "buildSourceId": "source", "buildRecordId": "build"})
	return domainbuild.Record{ID: "build"}, nil
}

type batchRuntimeManifest struct {
	manifestDeliveryStub
	repo    *batchRuntimeRepository
	created int
}

func (m *batchRuntimeManifest) FreezeDeliveryConfiguration(context.Context, domainidentity.Principal, string, string, string, domaincatalog.ReleaseTarget, int) (domainmanifest.DeliveryConfiguration, error) {
	return domainmanifest.DeliveryConfiguration{PackageID: "package", BindingID: "manifest-binding", Revision: 1, ConfigurationDigest: "fixed-config"}, nil
}
func (m *batchRuntimeManifest) CreateDeliverySnapshot(ctx context.Context, p domainidentity.Principal, app, env string, target domaincatalog.ReleaseTarget, revision int, planID string, artifacts domainmanifest.DeliveryArtifacts) (domainmanifest.DeliverySnapshot, error) {
	m.created++
	snapshot, _ := m.manifestDeliveryStub.CreateDeliverySnapshot(ctx, p, app, env, target, revision, planID, artifacts)
	task := m.repo.newTask(ctx, "manifest_preflight", "", nil)
	snapshot.PreflightTaskID, snapshot.ServiceID = task.ID, artifacts.ServiceID
	snapshot.TemplateInputs = &domainmanifest.ServiceTemplateInputs{ReleaseBundleID: artifacts.ReleaseBundleID, ArtifactImages: artifacts.ContainerImages}
	m.snapshot = snapshot
	return snapshot, nil
}
func (m *batchRuntimeManifest) ApplyDeliverySnapshot(ctx context.Context, _ domainidentity.Principal, snapshot domainmanifest.DeliverySnapshot) (domainmanifest.Deployment, domaindelivery.ExecutionTask, error) {
	m.applied = append(m.applied, snapshot)
	task := m.repo.newTask(ctx, "manifest_apply", "", map[string]any{"deploymentId": "deployment"})
	m.deployment = domainmanifest.Deployment{ID: "deployment", Generation: 2, Spec: domainmanifest.DeploymentSpec{DeliverySnapshot: &snapshot}}
	return m.deployment, task, nil
}

type batchRuntimeExecution struct {
	ExecutionController
	repo *batchRuntimeRepository
}

type batchGovernanceAudit struct{}

func (batchGovernanceAudit) Record(context.Context, domainaudit.Entry) error { return nil }

func (e batchRuntimeExecution) CancelExecutionTask(_ context.Context, id string, _ domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	task := e.repo.tasks[id]
	if task.Status != "completed" && task.Status != "canceled" {
		e.repo.status(id, "canceling")
	}
	return e.repo.tasks[id], nil
}

func batchRuntimeCheck(t *testing.T) (*Service, *batchRuntimeRepository, *batchRuntimeBuild, *batchRuntimeManifest, *stubCatalogReader, domainworkflow.Run, domainworkflow.DeliveryBatch) {
	t.Helper()
	repo := &batchRuntimeRepository{tasks: map[string]domaindelivery.ExecutionTask{}, bundle: domaindelivery.ReleaseBundle{ID: "bundle", ApplicationID: "app", ApplicationEnvironmentID: "env", Status: "ready", ArtifactRef: "registry.example/api:v1", ArtifactDigest: "sha256:" + strings.Repeat("a", 64)}}
	builder := &batchRuntimeBuild{repo: repo}
	manifest := &batchRuntimeManifest{repo: repo}
	catalog := &stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: "env", ApplicationID: "app", EnvironmentID: "global-env", ReleasePolicy: domaincatalog.ReleasePolicy{RequiresApproval: true}, BuildPolicy: domaincatalog.BuildPolicy{RefValue: "release", BuildArgs: map[string]any{"MODE": "release"}}, Targets: []domaincatalog.ReleaseTarget{{ID: "target", ExecutorKind: "manifest_ssa", ConfigRef: "manifest-binding", ClusterID: "cluster", Namespace: "test", Enabled: true, Metadata: map[string]any{"serviceId": "svc"}}}}}, envs: []domaincatalog.Environment{{ID: "global-env", Enabled: true}}}
	service := New(stubApplicationReader{app: domainapp.App{ID: "app", Name: "App"}, services: []domainapp.Service{{ID: "svc", ApplicationID: "app", Name: "API", Version: 1, BuildSourceID: "source", Containers: []domainapp.ServiceContainer{{Name: "api", ImageRepository: "registry.example/api"}}}}}, catalog, builder, stubWorkflowReader{}, stubReleaseReader{}, repo, batchRuntimeExecution{repo: repo}, nil, deliveryActionPermissions(appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryReleaseBundlesView, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger, appaccess.PermDeliveryApplicationEnvApprove))
	service.SetManifestDelivery(manifest)
	governance, err := deliverygovernance.New(repo, batchGovernanceAudit{})
	if err != nil {
		t.Fatal(err)
	}
	service.SetGovernance(governance)
	snapshot, err := service.FreezeDeliveryTarget(context.Background(), deliveryActionPrincipal(), domainworkflow.DeliveryTargetInput{ID: "web", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", Action: "build_deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if builder.created != 0 || manifest.created != 0 || snapshot.FrozenBuild.Input.RefName != "release" || snapshot.FrozenBuild.Input.BuildArgs["MODE"] != "release" {
		t.Fatal("freeze started work or dropped environment defaults")
	}
	snapshot.BuildNodeID = "web:build"
	run := domainworkflow.Run{ID: "run", Scope: domainworkflow.ScopeDeliveryBatch, Version: 1, FencingToken: 1, LeaseOwner: "worker"}
	for _, stage := range []string{"build", "plan", "deploy", "health"} {
		run.NodeRuns = append(run.NodeRuns, domainworkflow.NodeRun{NodeID: "web:" + stage, TargetID: "web", Stage: stage, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339)})
	}
	return service, repo, builder, manifest, catalog, run, domainworkflow.DeliveryBatch{ID: "batch", RootRunID: "run", Targets: []domainworkflow.DeliveryTargetSnapshot{snapshot}}
}
func batchRuntimeStep(t *testing.T, service *Service, run *domainworkflow.Run, batch domainworkflow.DeliveryBatch, index int, expected string) {
	t.Helper()
	node := run.NodeRuns[index]
	ctx := domainworkflow.WithNodeExecution(context.Background(), *run, node)
	actual, err := service.ExecuteDeliveryStage(ctx, deliveryActionPrincipal(), *run, batch, node)
	if err != nil || actual.Status != expected {
		t.Fatalf("%s status = %s (%s), err = %v; want %s", node.Stage, actual.Status, actual.Summary, err, expected)
	}
	run.NodeRuns[index] = actual
}

func TestBatchRuntimeBuildPlanApprovalDeployHealthRecovery(t *testing.T) {
	service, repo, builder, manifest, _, run, batch := batchRuntimeCheck(t)
	batchRuntimeStep(t, service, &run, batch, 0, "waiting_execution")
	batchRuntimeStep(t, service, &run, batch, 0, "waiting_execution")
	repo.status(run.NodeRuns[0].ExecutionTaskID, "completed")
	batchRuntimeStep(t, service, &run, batch, 0, "completed")
	batchRuntimeStep(t, service, &run, batch, 1, "waiting_execution")
	batchRuntimeStep(t, service, &run, batch, 1, "waiting_execution")
	if repo.plan.RequiresApproval != true || len(manifest.applied) != 0 {
		t.Fatal("approval policy lost or preflight applied resources")
	}
	repo.status(run.NodeRuns[1].ExecutionTaskID, "completed")
	batchRuntimeStep(t, service, &run, batch, 1, "waiting_approval")
	if _, err := service.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), repo.plan.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("public confirmation bypassed run: %v", err)
	}
	if _, err := service.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve"}); err != nil {
		t.Fatal(err)
	}
	batchRuntimeStep(t, service, &run, batch, 1, "completed")
	batchRuntimeStep(t, service, &run, batch, 2, "waiting_execution")
	batchRuntimeStep(t, service, &run, batch, 2, "waiting_execution")
	repo.status(run.NodeRuns[2].ExecutionTaskID, "completed")
	batchRuntimeStep(t, service, &run, batch, 2, "completed")
	manifest.deployment.Status = domainmanifest.DeploymentStatus{Phase: domainmanifest.DeploymentPhaseConverged, ObservedGeneration: 1, AppliedDigest: manifest.snapshot.RenderedDigest, Conditions: []domainmanifest.Condition{{Type: "Healthy", Status: "true", ObservedGeneration: 1}}}
	batchRuntimeStep(t, service, &run, batch, 3, "waiting_execution")
	timeoutSnapshot, timeoutNode := batch.Targets[0], run.NodeRuns[3]
	timeoutSnapshot.HealthTimeoutSeconds = 1
	timeoutNode.StartedAt = time.Now().Add(-2 * time.Second).UTC().Format(time.RFC3339)
	timedOut, err := service.observeBatchHealth(context.Background(), deliveryActionPrincipal(), run, timeoutSnapshot, timeoutNode)
	if err != nil || timedOut.Status != "failed" || !strings.Contains(timedOut.Summary, "1 seconds") {
		t.Fatalf("environment health timeout ignored: %+v %v", timedOut, err)
	}
	manifest.deployment.Status.ObservedGeneration = 2
	manifest.deployment.Status.Conditions[0].ObservedGeneration = 2
	batchRuntimeStep(t, service, &run, batch, 3, "completed")
	if builder.created != 1 || manifest.created != 1 || len(manifest.applied) != 1 || repo.createCount != 1 {
		t.Fatalf("resume created duplicate work: build=%d preflight=%d apply=%d plan=%d", builder.created, manifest.created, len(manifest.applied), repo.createCount)
	}
	// A configuration-only run recovers the current verified bundle without a build.
	target := batch.Targets[0].Target
	target.Action = "config_update"
	frozen, err := service.FreezeDeliveryTarget(context.Background(), deliveryActionPrincipal(), target)
	if err != nil || frozen.Target.ReleaseBundleID != "bundle" || frozen.FrozenBuild != nil || builder.created != 1 {
		t.Fatalf("configuration update lost existing artifact: %#v %v", frozen, err)
	}
}

func TestBatchRuntimeRejectDriftAndWaitForCancellation(t *testing.T) {
	for _, scenario := range []string{"rejected", "policy", "global-policy", "cancel", "missing-digest"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, _, manifest, catalog, run, batch := batchRuntimeCheck(t)
			batchRuntimeStep(t, service, &run, batch, 0, "waiting_execution")
			if scenario == "cancel" {
				node, err := service.CancelDeliveryStage(context.Background(), run, batch, run.NodeRuns[0])
				if err != nil || node.Status != "canceling" {
					t.Fatalf("cancel treated request as acknowledgement: %#v %v", node, err)
				}
				repo.status(node.ExecutionTaskID, "canceled")
				node, err = service.CancelDeliveryStage(context.Background(), run, batch, node)
				if err != nil || node.Status != "canceled" {
					t.Fatal("confirmed stop not reflected")
				}
				return
			}
			repo.status(run.NodeRuns[0].ExecutionTaskID, "completed")
			if scenario == "missing-digest" {
				task := repo.tasks[run.NodeRuns[0].ExecutionTaskID]
				task.Result = nil
				repo.tasks[task.ID] = task
				batchRuntimeStep(t, service, &run, batch, 0, "failed")
				return
			}
			batchRuntimeStep(t, service, &run, batch, 0, "completed")
			if scenario == "policy" {
				catalog.bindings[0].ReleasePolicy.RequiresApproval = false
				batchRuntimeStep(t, service, &run, batch, 1, "failed")
				return
			}
			if scenario == "global-policy" {
				catalog.envs[0].RequiresApproval = true
				batchRuntimeStep(t, service, &run, batch, 1, "failed")
				return
			}
			batchRuntimeStep(t, service, &run, batch, 1, "waiting_execution")
			repo.status(run.NodeRuns[1].ExecutionTaskID, "completed")
			batchRuntimeStep(t, service, &run, batch, 1, "waiting_approval")
			if _, err := service.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "reject"}); err != nil {
				t.Fatal(err)
			}
			batchRuntimeStep(t, service, &run, batch, 1, "failed")
			if len(manifest.applied) != 0 {
				t.Fatal("rejected plan applied")
			}
		})
	}
}

func TestGovernancePreflightEvidenceComesOnlyFromRevalidatedBatchSnapshots(t *testing.T) {
	service, _, _, manifest, _, run, _ := batchRuntimeCheck(t)
	ctx := domainworkflow.WithNodeExecution(context.Background(), run, run.NodeRuns[2])
	plan := domaindelivery.DeliveryPlan{ID: "plan", Source: domainworkflow.ScopeDeliveryBatch, ApplicationID: "app", ApplicationEnvironmentID: "env", Impact: map[string]any{"workflowRunId": run.ID, "workflowTargetId": "web"}, ManifestSnapshots: []domainmanifest.DeliverySnapshot{{DeliveryPlanID: "plan", ApplicationEnvironmentID: "env", PreflightTaskID: "preflight"}}}
	request, err := service.deliveryGovernanceRequest(ctx, deliveryActionPrincipal(), plan)
	if err != nil || len(request.ValidatedPreflightTaskIDs) != 1 || request.ValidatedPreflightTaskIDs[0] != "preflight" {
		t.Fatalf("validated snapshot evidence missing: %+v %v", request, err)
	}
	manifest.validationError = apperrors.ErrConflict
	request, err = service.deliveryGovernanceRequest(ctx, deliveryActionPrincipal(), plan)
	if !errors.Is(err, apperrors.ErrConflict) || len(request.ValidatedPreflightTaskIDs) != 0 {
		t.Fatal("stale or failed preflight became validation evidence")
	}
	manifest.validationError = nil
	if _, err := service.deliveryGovernanceRequest(context.Background(), deliveryActionPrincipal(), plan); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("public request supplied batch evidence")
	}
	plan.Source = "manual"
	plan.Impact["validatedPreflightTaskIds"] = []string{"preflight"}
	request, err = service.deliveryGovernanceRequest(ctx, deliveryActionPrincipal(), plan)
	if err != nil || len(request.ValidatedPreflightTaskIDs) != 0 {
		t.Fatal("legacy plan accepted caller-supplied validation evidence")
	}
}
