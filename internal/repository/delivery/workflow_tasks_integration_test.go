package delivery_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoapp "github.com/opensoha/soha/internal/repository/application"
	repobuild "github.com/opensoha/soha/internal/repository/build"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	repodocker "github.com/opensoha/soha/internal/repository/docker"
	repomanifest "github.com/opensoha/soha/internal/repository/manifest"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"go.uber.org/zap"
)

func TestDeliveryNodeTaskIdentityFencingAndLateCallbackWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 6, MaxIdleConns: 4}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	repo, workflows, builds := repodelivery.New(store.DB()), repoworkflow.New(store.DB()), repobuild.New(store.DB())
	now := time.Now().UTC()
	applicationID := uuid.NewString()
	if _, err := repoapp.New(store.DB()).Create(ctx, domainapp.UpsertInput{ID: applicationID, Key: applicationID, Name: "Delivery task check", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DB().Exec(`DELETE FROM applications WHERE id = ?`, applicationID).Error })
	target := domainworkflow.DeliveryTargetInput{ID: "web", ApplicationID: applicationID, ServiceID: "web", Action: "build"}
	batch := domainworkflow.DeliveryBatch{ID: uuid.NewString(), RootRunID: "workflow:" + uuid.NewString(), CreatedBy: "delivery-task-check", CreatedAt: now, UpdatedAt: now, Definition: domainworkflow.DeliveryWorkflowDefinition{Name: "task check", Targets: []domainworkflow.DeliveryTargetInput{target}}, Targets: []domainworkflow.DeliveryTargetSnapshot{{Target: target}}}
	run := domainworkflow.Run{ID: batch.RootRunID, Scope: domainworkflow.ScopeDeliveryBatch, DeliveryBatchID: batch.ID, WorkflowName: "task check", Status: "running", Steps: []domainworkflow.Step{}, NodeRuns: []domainworkflow.NodeRun{{NodeID: "web:build", TargetID: "web", Stage: "build", Status: "running"}, {NodeID: "web:plan", TargetID: "web", Stage: "plan", Status: "running"}, {NodeID: "web:deploy", TargetID: "web", Stage: "deploy", Status: "running"}}, CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)}
	_, _, err = workflows.CreateDeliveryBatch(ctx, batch, run, uuid.NewString(), "sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM execution_tasks WHERE payload->>'workflowRunId' = ? OR id = ?`, run.ID, "legacy:"+run.ID).Error
		_ = store.DB().Exec(`DELETE FROM delivery_plans WHERE impact->>'workflowRunId' = ?`, run.ID).Error
		_ = store.DB().Exec(`DELETE FROM release_bundles WHERE metadata->>'workflowRunId' = ?`, run.ID).Error
		_ = store.DB().Exec(`DELETE FROM build_records WHERE metadata->>'workflowRunId' = ?`, run.ID).Error
		_ = store.DB().Exec(`DELETE FROM delivery_batches WHERE id = ?`, batch.ID).Error
		_ = store.DB().Exec(`DELETE FROM workflow_runs WHERE id = ?`, run.ID).Error
	})
	run, err = workflows.ClaimManagedRun(ctx, "worker-a", time.Minute)
	if err != nil || run.ID != batch.RootRunID {
		t.Fatalf("claim isolated run: %v", err)
	}
	t.Run("final plan identity and approval CAS", func(t *testing.T) { verifyBatchPlanPersistence(t, ctx, repo, run, target) })
	t.Run("Docker domain queue fencing and claim", func(t *testing.T) { verifyDockerBatchQueue(t, ctx, store, run, target) })
	input := verifyDeliveryTaskIdentity(t, ctx, repo, builds, run, target, now)
	recovered, task := verifyDeliveryTaskFencing(t, ctx, store, repo, workflows, run, input)
	legacy := verifyDeliveryTaskStopping(t, ctx, repo, workflows, run, recovered, input, task)
	verifyManifestOperationReplay(t, ctx, store, repo, applicationID, legacy, now)
}

func verifyDockerBatchQueue(t *testing.T, ctx context.Context, store *dbstore.Store, run domainworkflow.Run, target domainworkflow.DeliveryTargetInput) {
	t.Helper()
	repo := repodocker.New(store.DB())
	host, err := repo.CreateHost(ctx, domaindocker.HostInput{Name: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	project, err := repo.CreateProject(ctx, domaindocker.ProjectInput{HostID: host.ID, Name: "queue-check", Slug: uuid.NewString(), ComposeContent: "services: {}"})
	if err != nil {
		t.Fatal(err)
	}
	verifyDockerProjectCreation(t, ctx, store, repo, host.ID)
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM docker_operations WHERE host_id = ?`, host.ID).Error
		_ = store.DB().Exec(`DELETE FROM docker_projects WHERE id = ?`, project.ID).Error
		_ = store.DB().Exec(`DELETE FROM docker_hosts WHERE id = ?`, host.ID).Error
	})
	nodeCtx := domainworkflow.WithNodeExecution(ctx, run, run.NodeRuns[1])
	node, _ := domainworkflow.NodeExecutionFrom(nodeCtx)
	input := domaindocker.OperationInput{ID: uuid.NewString(), HostID: host.ID, ProjectID: project.ID, OperationKind: "project_deploy", Status: "queued", MaxRetries: 5, Payload: node.Metadata(map[string]any{"applicationId": target.ApplicationID, "applicationEnvironmentId": target.ApplicationEnvironmentID, "deliveryPlanId": node.ResourceID("plan"), "action": "validate", "deliveryCiphertext": "sealed"})}
	if _, err := repo.CreateOperation(ctx, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unscoped metadata accepted: %v", err)
	}
	created, err := repo.CreateOperation(nodeCtx, input)
	if err != nil || created.MaxRetries != 0 {
		t.Fatalf("frozen queue: %+v %v", created, err)
	}
	if _, err := repo.ClaimOperation(ctx, "host-worker", "agent", []string{host.ID}, []string{"project_deploy"}, "", time.Now()); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("legacy runner claimed governed deployment: %v", err)
	}
	claimed, err := repo.ClaimOperation(ctx, "host-worker", "agent", []string{host.ID}, []string{"project_deploy"}, "claim-token", time.Now())
	if err != nil || claimed.ID != created.ID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	input.ID = uuid.NewString()
	second, err := repo.CreateOperation(nodeCtx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimOperation(ctx, "second-worker", "agent", []string{host.ID}, []string{"project_deploy"}, "next-token", time.Now()); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("concurrent Compose mutation claimed: %v", err)
	}
	claimed.Status = "completed"
	if _, err := repo.UpdateOperation(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimOperation(ctx, "second-worker", "agent", []string{host.ID}, []string{"project_deploy"}, "next-token", time.Now())
	if err != nil || claimed.ID != second.ID {
		t.Fatalf("project queue did not resume: %+v %v", claimed, err)
	}
	claimed.Status = "completed"
	if _, err := repo.UpdateOperation(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	verifyConcurrentDockerClaims(t, ctx, nodeCtx, repo, input, host.ID)
	old := run
	old.FencingToken++
	input.ID = uuid.NewString()
	if _, err := repo.CreateOperation(domainworkflow.WithNodeExecution(ctx, old, old.NodeRuns[1]), input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale workflow created Docker work: %v", err)
	}
}

func verifyConcurrentDockerClaims(t *testing.T, ctx, nodeCtx context.Context, repo *repodocker.Repository, input domaindocker.OperationInput, hostID string) {
	t.Helper()
	for range 4 {
		input.ID = uuid.NewString()
		if _, err := repo.CreateOperation(nodeCtx, input); err != nil {
			t.Fatal(err)
		}
	}
	start, results := make(chan struct{}), make(chan error, 8)
	claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for range 8 {
		go func() {
			<-start
			_, err := repo.ClaimOperation(claimCtx, uuid.NewString(), "agent", []string{hostID}, []string{"project_deploy"}, uuid.NewString(), time.Now())
			results <- err
		}()
	}
	close(start)
	successes := 0
	for range 8 {
		if err := <-results; err == nil {
			successes++
		} else if !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("parallel claim: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("parallel project claim winners=%d", successes)
	}
}

func verifyDockerProjectCreation(t *testing.T, ctx context.Context, store *dbstore.Store, repo *repodocker.Repository, hostID string) {
	t.Helper()
	input := domaindocker.ProjectInput{ID: uuid.NewString(), HostID: hostID, Name: "fixed-name", Slug: uuid.NewString(), ComposeContent: "services:\n  api:\n    image: test\n", EnvContent: "DO_NOT_COPY=secret"}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM docker_services WHERE project_id = ?`, input.ID).Error
		_ = store.DB().Exec(`DELETE FROM docker_projects WHERE id = ?`, input.ID).Error
		_ = store.DB().Exec(`DELETE FROM docker_project_creations WHERE id = ?`, input.ID).Error
	})
	type result struct {
		receipt domaindocker.Project
		err     error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			item, err := repo.CreateProjectIdempotent(ctx, input, "same-digest", []string{"api", "worker"})
			results <- result{item, err}
		}()
	}
	var first domaindocker.Project
	for range 8 {
		got := <-results
		if got.err != nil || got.receipt.ID != input.ID || got.receipt.ComposeContent != "" || got.receipt.EnvContent != "" {
			t.Fatalf("creation: %+v %v", got.receipt, got.err)
		}
		if first.ID != "" && !first.CreatedAt.Equal(got.receipt.CreatedAt) {
			t.Fatal("creation receipt changed")
		}
		first = got.receipt
	}
	var count int64
	if err := store.DB().Raw(`SELECT COUNT(*) FROM docker_services WHERE project_id = ?`, input.ID).Scan(&count).Error; err != nil || count != 2 {
		t.Fatalf("services count=%d %v", count, err)
	}
	changed := input
	changed.Name = "edited"
	if _, err := repo.UpdateProject(ctx, input.ID, changed); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.FindProjectCreation(ctx, input.ID, "same-digest"); err != nil || got.Name != first.Name {
		t.Fatalf("receipt changed after edit: %+v %v", got, err)
	}
	if _, err := repo.CreateProjectIdempotent(ctx, input, "different-digest", nil); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("different input replayed: %v", err)
	}
	if err := repo.DeleteProject(ctx, input.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.CreateProjectIdempotent(ctx, input, "same-digest", nil); err != nil || got.ID != first.ID {
		t.Fatalf("deleted receipt lost: %+v %v", got, err)
	}
	if _, err := repo.GetProject(ctx, input.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted project recreated: %v", err)
	}
	verifyDockerProjectCreationRollback(t, ctx, repo, input)
}

func verifyDockerProjectCreationRollback(t *testing.T, ctx context.Context, repo *repodocker.Repository, input domaindocker.ProjectInput) {
	t.Helper()
	failed := input
	failed.ID = uuid.NewString()
	failed.Slug = uuid.NewString()
	if _, err := repo.CreateProjectIdempotent(ctx, failed, "rollback-digest", []string{"first-service", "invalid\x00name"}); err == nil {
		t.Fatal("expected service persistence failure")
	}
	if _, err := repo.GetProject(ctx, failed.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("failed creation retained project: %v", err)
	}
	if _, err := repo.FindProjectCreation(ctx, failed.ID, "rollback-digest"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("failed creation retained receipt: %v", err)
	}
}

func verifyDeliveryTaskIdentity(t *testing.T, ctx context.Context, repo *repodelivery.Repository, builds *repobuild.Repository, run domainworkflow.Run, target domainworkflow.DeliveryTargetInput, now time.Time) domaindelivery.ExecutionTask {
	t.Helper()
	nodeCtx := domainworkflow.WithNodeExecution(ctx, run, run.NodeRuns[0])
	node, _ := domainworkflow.NodeExecutionFrom(nodeCtx)
	for range 2 {
		record, err := builds.Create(nodeCtx, domainbuild.TriggerInput{ApplicationID: target.ApplicationID}, map[string]any{"source": "frozen"})
		if err != nil || record.ID != node.ResourceID("build") {
			t.Fatalf("build record was not recovered: %+v %v", record, err)
		}
		bundle, err := repo.CreateReleaseBundle(nodeCtx, domaindelivery.ReleaseBundle{ID: uuid.NewString(), ApplicationID: target.ApplicationID, Version: "fixed", SourceType: "build", Status: "building", CreatedAt: now, UpdatedAt: now})
		if err != nil || bundle.ID != node.ResourceID("bundle") {
			t.Fatalf("bundle was not recovered: %+v %v", bundle, err)
		}
	}
	input := domaindelivery.ExecutionTask{ID: uuid.NewString(), ApplicationID: target.ApplicationID, ReleaseBundleID: node.ResourceID("bundle"), TaskKind: "build", ProviderKind: "k8s_job_runner", TargetKind: "k8s_workload", Status: "queued", CallbackToken: uuid.NewString(), Payload: map[string]any{}, Result: map[string]any{}, CreatedAt: now, UpdatedAt: now}
	results := make(chan error, 4)
	for range 4 {
		go func() {
			task, err := repo.CreateExecutionTask(nodeCtx, input)
			if err == nil && (task.ID != node.ResourceID("task") || task.MaxRetries != 0) {
				err = errors.New("duplicate identity or automatic retry enabled")
			}
			results <- err
		}()
	}
	for range 4 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	wrong := input
	wrong.ApplicationID = "foreign-app"
	if _, err := repo.CreateExecutionTask(nodeCtx, wrong); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("cross-application task accepted: %v", err)
	}
	forged := input
	forged.Payload = node.Metadata(nil)
	if _, err := repo.CreateExecutionTask(ctx, forged); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("client metadata forged node execution: %v", err)
	}
	forged.Payload = map[string]any{"workflowRunId": run.ID}
	if _, err := repo.CreateExecutionTask(ctx, forged); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy path bypassed batch scope: %v", err)
	}
	return input
}

func verifyDeliveryTaskFencing(t *testing.T, ctx context.Context, store *dbstore.Store, repo *repodelivery.Repository, workflows *repoworkflow.Repository, run domainworkflow.Run, input domaindelivery.ExecutionTask) (domainworkflow.Run, domaindelivery.ExecutionTask) {
	t.Helper()
	nodeCtx := domainworkflow.WithNodeExecution(ctx, run, run.NodeRuns[0])
	node, _ := domainworkflow.NodeExecutionFrom(nodeCtx)
	if err := store.DB().Exec(`UPDATE workflow_runs SET lease_until = NOW() - INTERVAL '1 second' WHERE id = ?`, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := workflows.ClaimManagedRun(ctx, "worker-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExecutionTask(nodeCtx, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("old worker created task after takeover: %v", err)
	}
	recoveredCtx := domainworkflow.WithNodeExecution(ctx, recovered, recovered.NodeRuns[0])
	task, err := repo.CreateExecutionTask(recoveredCtx, input)
	if err != nil || task.ID != node.ResourceID("task") || recovered.NodeRuns[0].ExecutionTaskID != "" {
		t.Fatalf("crash before node-reference write created a second task: %+v %v", task, err)
	}
	task, err = repo.PrepareDeliveryJob(ctx, task.ID, map[string]any{"k8sJobName": "stable-job", "k8sJobNamespace": "original"})
	if err != nil || task.Status != "dispatching" || task.AttemptCount != 1 {
		t.Fatalf("persist before external dispatch: %+v %v", task, err)
	}
	replayed, err := repo.PrepareDeliveryJob(ctx, task.ID, map[string]any{"k8sJobNamespace": "changed"})
	if err != nil || replayed.Result["k8sJobNamespace"] != "original" || replayed.Result["k8sJobName"] != "stable-job" || replayed.AttemptCount != 1 {
		t.Fatalf("recovery changed frozen Job or attempt: %+v %v", replayed, err)
	}
	return recovered, task
}

func verifyDeliveryTaskStopping(t *testing.T, ctx context.Context, repo *repodelivery.Repository, workflows *repoworkflow.Repository, run, recovered domainworkflow.Run, input, task domaindelivery.ExecutionTask) domaindelivery.ExecutionTask {
	t.Helper()
	recoveredCtx := domainworkflow.WithNodeExecution(ctx, recovered, recovered.NodeRuns[0])
	_, err := workflows.StopManagedRun(ctx, run.ID, "user", "cancel test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExecutionTask(recoveredCtx, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stopped delivery created task: %v", err)
	}
	if _, err := repo.PrepareDeliveryJob(ctx, task.ID, nil); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stopped batch permitted Direct Job dispatch: %v", err)
	}
	stopping, err := repo.RequestDeliveryTaskStop(ctx, task.ID, "stop test")
	if err != nil || stopping.Status != "canceling" || stopping.FinishedAt != nil {
		t.Fatalf("unconfirmed cancellation became terminal: %+v %v", stopping, err)
	}
	staleDispatch := task
	staleDispatch.Status = "running"
	stopping, err = repo.UpdateExecutionTask(ctx, staleDispatch)
	if err != nil || stopping.Status != "canceling" {
		t.Fatalf("late dispatch resurrected stopping task: %+v %v", stopping, err)
	}
	legacy := input
	legacy.ProviderKind = "delivery_test_runner"
	legacy.ID, legacy.ReleaseBundleID, legacy.CallbackToken, legacy.CreatedAt = "legacy:"+run.ID, "", uuid.NewString(), input.CreatedAt.Add(time.Second)
	if _, err := repo.CreateExecutionTask(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimExecutionTask(ctx, []string{"delivery_test_runner"}, "agent", "local")
	if err != nil || claimed.ID != legacy.ID {
		t.Fatalf("stopped batch was dispatched or blocked independent work: %+v %v", claimed, err)
	}
	verifyLateDeliveryCallback(t, ctx, repo, task)
	return legacy
}

func verifyLateDeliveryCallback(t *testing.T, ctx context.Context, repo *repodelivery.Repository, task domaindelivery.ExecutionTask) {
	t.Helper()
	late := task
	task.Status = "canceled"
	if _, err := repo.UpdateExecutionTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	late.Status = "completed"
	final, err := repo.UpdateExecutionTask(ctx, late)
	if err != nil || final.Status != "canceled" {
		t.Fatalf("late callback resurrected canceled task: %+v %v", final, err)
	}
	late.Payload = map[string]any{}
	if _, err := repo.UpdateExecutionTask(ctx, late); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("stripped metadata bypassed protected update: %v", err)
	}
}

func verifyManifestOperationReplay(t *testing.T, ctx context.Context, store *dbstore.Store, repo *repodelivery.Repository, applicationID string, legacy domaindelivery.ExecutionTask, now time.Time) {
	t.Helper()
	// Manifest operations now use this same task creation path. Replaying an
	// operation must retain its existing task, including the original token.
	manifests := repomanifest.New(store.DB())
	environmentID, clusterID := uuid.NewString(), uuid.NewString()
	if err := store.DB().Exec(`INSERT INTO application_environments (id, application_id, environment_id) VALUES (?, ?, 'dev')`, environmentID, applicationID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`INSERT INTO clusters (id, name) VALUES (?, 'Operation check')`, clusterID).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM application_environments WHERE id = ?`, environmentID).Error
		_ = store.DB().Exec(`DELETE FROM clusters WHERE id = ?`, clusterID).Error
	})
	pkg, err := manifests.Create(ctx, domainmanifest.Package{ID: uuid.NewString(), ApplicationID: applicationID, Name: "Operation check", Renderer: domainmanifest.RendererRaw, Status: "draft", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DB().Exec(`DELETE FROM manifest_packages WHERE id = ?`, pkg.ID).Error })
	binding, err := manifests.CreateBinding(ctx, domainmanifest.EnvironmentBinding{ID: uuid.NewString(), PackageID: pkg.ID, ApplicationEnvironmentID: environmentID, EnvironmentKey: "dev", ClusterID: clusterID, Namespace: "default", Overlay: map[string]string{}, Version: 1, DriftPolicy: "report", DeletionPolicy: "orphan", Enabled: true, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	manifestTask := legacy
	manifestTask.ID, manifestTask.CallbackToken, manifestTask.TaskKind = uuid.NewString(), uuid.NewString(), "manifest.apply"
	manifestTask.Payload = map[string]any{"clusterId": binding.ClusterID, "bindingId": binding.ID}
	operation := domainmanifest.OperationRun{ID: uuid.NewString(), PackageID: pkg.ID, BindingID: binding.ID, Generation: 1, Action: "apply", IdempotencyKey: uuid.NewString(), ExecutionTaskID: manifestTask.ID, CreatedAt: now}
	id, created, err := manifests.CreateOperationTask(ctx, operation, manifestTask)
	if err != nil || !created || id != manifestTask.ID {
		t.Fatalf("create manifest operation: %s %t %v", id, created, err)
	}
	t.Cleanup(func() { _ = store.DB().Exec(`DELETE FROM execution_tasks WHERE id = ?`, id).Error })
	manifestTask.ID, manifestTask.CallbackToken = uuid.NewString(), uuid.NewString()
	duplicateID, created, err := manifests.CreateOperationTask(ctx, operation, manifestTask)
	if err != nil || created || duplicateID != id {
		t.Fatalf("manifest operation replay: %s %t %v", duplicateID, created, err)
	}
	if _, err := repo.GetExecutionTask(ctx, id); err != nil {
		t.Fatalf("manifest replay deleted original task: %v", err)
	}
}

func verifyBatchPlanPersistence(t *testing.T, ctx context.Context, repo *repodelivery.Repository, run domainworkflow.Run, target domainworkflow.DeliveryTargetInput) {
	t.Helper()
	planCtx := domainworkflow.WithNodeExecution(ctx, run, run.NodeRuns[1])
	deployCtx := domainworkflow.WithNodeExecution(ctx, run, run.NodeRuns[2])
	input := domaindelivery.DeliveryPlanInput{ApplicationID: target.ApplicationID, ApplicationEnvironmentID: target.ApplicationEnvironmentID, Source: domainworkflow.ScopeDeliveryBatch, Action: domaindelivery.ApplicationDeliveryActionDeploy, TargetID: "immutable-target", RequiresApproval: true}
	input.DockerSnapshots = []sohaapi.DockerDeliverySnapshot{{TargetID: "immutable-target", ProjectID: "project", RenderedDigest: "frozen-render"}}
	input.DockerPrepared = map[string]string{"immutable-target": "encrypted-value"}
	first, err := repo.CreateDeliveryPlan(planCtx, input, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.DockerSnapshots) != 1 || first.DockerSnapshots[0].RenderedDigest != "frozen-render" || first.DockerPrepared["immutable-target"] != "encrypted-value" {
		t.Fatal("Docker plan snapshot did not persist")
	}
	input.TargetID = "changed-target"
	again, err := repo.CreateDeliveryPlan(planCtx, input, "actor")
	if err != nil || again.ID != first.ID || again.TargetID != "immutable-target" {
		t.Fatalf("plan was recreated: %+v %v", again, err)
	}
	if _, err = repo.CreateDeliveryPlan(ctx, input, "actor"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("unscoped batch plan accepted", err)
	}
	stripped := first
	stripped.Source = "manual"
	if _, err = repo.UpdateDeliveryPlan(ctx, stripped); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("batch plan scope stripped", err)
	}
	first.Status = domaindelivery.DeliveryPlanStatusWaitingApproval
	first.Impact["approval"] = []any{map[string]any{"status": "requested"}}
	waiting, err := repo.UpdateDeliveryPlan(planCtx, first)
	if err != nil {
		t.Fatal(err)
	}
	approved := waiting
	approved.Status = domaindelivery.DeliveryPlanStatusDraft
	approved.Impact = map[string]any{"approval": []any{map[string]any{"status": "requested"}, map[string]any{"status": "approved", "actorId": "approver"}}}
	approved, err = repo.UpdateDeliveryPlan(ctx, approved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.UpdateDeliveryPlan(ctx, waiting); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("stale approval write accepted", err)
	}
	approved.Status = domaindelivery.DeliveryPlanStatusConfirming
	approved.TargetID = "changed-target"
	confirming, err := repo.UpdateDeliveryPlan(deployCtx, approved)
	if err != nil || confirming.TargetID != "immutable-target" || confirming.Impact["workflowRunId"] != run.ID {
		t.Fatalf("approved contents changed: %+v %v", confirming, err)
	}
	confirming.Status = domaindelivery.DeliveryPlanStatusConfirmed
	confirmed, err := repo.UpdateDeliveryPlan(deployCtx, confirming)
	if err != nil {
		t.Fatal(err)
	}
	confirmed.Status = domaindelivery.DeliveryPlanStatusDraft
	if _, err = repo.UpdateDeliveryPlan(deployCtx, confirmed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("confirmed plan reopened", err)
	}
}
