package manifest_test

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
	manifestrepo "github.com/opensoha/soha/internal/repository/manifest"
	"go.uber.org/zap"
)

func TestDeliverySnapshotWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_MANIFEST_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_MANIFEST_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	requireManifestRepositoryNoError(t, err)
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable",
		MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	}, zap.NewNop())
	requireManifestRepositoryNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	requireManifestRepositoryNoError(t, store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")))
	t.Run("binding-edit-and-operation-package-lock-order", func(t *testing.T) {
		verifyManifestBindingOperationOrder(t, ctx, store.DB())
	})

	// The fixture lives in a transaction, so repeated runs leave no business data.
	tx := store.DB().WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	applicationID, environmentID, clusterID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO applications (id, name, app_key, app_group, language) VALUES (?, 'Manifest acceptance', ?, 'test', 'configuration')`, []any{applicationID, applicationID}},
		{`INSERT INTO application_environments (id, application_id, environment_id) VALUES (?, ?, ?)`, []any{environmentID, applicationID, "test"}},
		{`INSERT INTO clusters (id, name) VALUES (?, 'Manifest acceptance')`, []any{clusterID}},
	} {
		requireManifestRepositoryNoError(t, tx.Exec(statement.query, statement.args...).Error)
	}
	repository := manifestrepo.New(tx)
	verifyConfigurationRevisionCAS(t, ctx, repository, applicationID)
	t.Run("root-cr-publication-control", func(t *testing.T) {
		verifyRootCRPublicationControl(t, ctx, tx, repository, applicationID, environmentID, clusterID)
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	item, err := repository.Create(ctx, domainmanifest.Package{
		ID: uuid.NewString(), Name: "API", ApplicationID: applicationID, Renderer: domainmanifest.RendererKustomize,
		Status: domainmanifest.StatusPublished, CurrentRevision: 1, Files: []domainmanifest.File{}, Bindings: []domainmanifest.Binding{}, CreatedAt: now, UpdatedAt: now,
	})
	requireManifestRepositoryNoError(t, err)
	binding, err := repository.CreateBinding(ctx, domainmanifest.EnvironmentBinding{
		ID: uuid.NewString(), PackageID: item.ID, ApplicationEnvironmentID: environmentID, EnvironmentKey: "test", ClusterID: clusterID, Namespace: "test",
		Overlay: map[string]string{}, Kustomize: &domainmanifest.KustomizeOptions{EntryPath: "overlays/test"},
		Enabled: true, Version: 1, DriftPolicy: "report", DeletionPolicy: "orphan", CreatedAt: now, UpdatedAt: now,
	})
	requireManifestRepositoryNoError(t, err)
	if binding.Kustomize == nil || binding.Kustomize.EntryPath != "overlays/test" {
		t.Fatalf("binding: %#v", binding)
	}
	item, err = repository.Get(ctx, item.ID)
	requireManifestRepositoryNoError(t, err)
	if len(item.Bindings) != 1 || !reflect.DeepEqual(item.Bindings[0].Kustomize, binding.Kustomize) {
		t.Fatalf("binding projection: %#v", item.Bindings)
	}
	snapshot := domainmanifest.DeliverySnapshot{
		DeliveryPlanID: uuid.NewString(), TargetID: "target-1", PackageID: item.ID, BindingID: binding.ID, BindingVersion: binding.Version,
		ApplicationEnvironmentID: environmentID, ClusterID: clusterID, Namespace: binding.Namespace, Revision: 1,
		PackageUpdatedAt: item.UpdatedAt.UTC(), RevisionDigest: "sha256:revision", RendererVersion: "kustomize/test", InputDigest: "sha256:input", RenderedDigest: "sha256:rendered",
		Documents: []domainmanifest.RenderedDocument{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "test", Name: "api", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: api\n  namespace: test\n"}}, PreflightTaskID: "preflight-1",
	}
	verifyDeliveryPlanRoundTrip(t, ctx, deliveryrepo.New(tx), applicationID, snapshot)
	t.Run("publication-operation-context", func(t *testing.T) { verifyPublicationOperationContext(t, ctx, tx, repository, item, binding) })

	next := domainmanifest.Deployment{ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID, CreatedAt: now, UpdatedAt: now,
		Spec: domainmanifest.DeploymentSpec{DesiredRevision: 1, DesiredDigest: snapshot.RenderedDigest, ReconcilePolicy: "continuous", DriftPolicy: "report", DeletionPolicy: "orphan", DeliverySnapshot: &snapshot}}
	deployment, err := repository.SetDesiredRevision(ctx, next, 0)
	requireManifestRepositoryNoError(t, err)
	if deployment.Generation != 1 || !reflect.DeepEqual(deployment.Spec.DeliverySnapshot, &snapshot) {
		t.Fatalf("deployment snapshot: %#v", deployment)
	}
	if _, err := repository.SetDesiredRevision(ctx, next, 0); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale generation: %v", err)
	}
	deployment, err = repository.SetDesiredRevision(ctx, next, 1)
	if err != nil || deployment.Generation != 2 {
		t.Fatalf("update deployment: %#v, %v", deployment, err)
	}
	t.Run("migration-baseline-is-success-only", func(t *testing.T) { verifyMigrationBaseline(t, ctx, tx, repository, deployment) })
	verifyManifestObservationCadence(t, ctx, tx, repository, deployment.ID)
	verifyResourceObservationRoundTrip(t, ctx, repository, deployment)
	t.Run("pending-plan-retains-observed-identities", func(t *testing.T) {
		verifyPendingPlanInventory(t, ctx, tx, repository, deployment)
	})

	verifyManifestResourceOwnership(t, ctx, tx, repository, next, binding, item)
	verifyHelmResourceOwnership(t, ctx, tx, repository, next, binding, item)
	verifyGitOpsResourceOwnership(t, ctx, tx, repository, next, binding, item)

	verifyManifestChangedInputConflict(t, ctx, tx, repository, next, binding, item)
}

func verifyManifestBindingOperationOrder(t *testing.T, ctx context.Context, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	appID, envID, clusterID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	pkgID, bindingID := uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO applications(id,name,app_key,app_group,language) VALUES (?,'Lock order',?,'test','configuration')`, []any{appID, appID}},
		{`INSERT INTO application_environments(id,application_id,environment_id) VALUES (?,?,'test')`, []any{envID, appID}},
		{`INSERT INTO clusters(id,name) VALUES (?,'Lock order')`, []any{clusterID}},
		{`INSERT INTO manifest_packages(id,name,application_id) VALUES (?,'Lock order',?)`, []any{pkgID, appID}},
		{`INSERT INTO manifest_bindings(id,package_id,application_environment_id,environment_key,cluster_id,namespace) VALUES (?,?,?,'test',?,'test')`, []any{bindingID, pkgID, envID, clusterID}},
	} {
		requireManifestRepositoryNoError(t, db.WithContext(ctx).Exec(statement.query, statement.args...).Error)
	}
	t.Cleanup(func() {
		for _, row := range []struct{ table, id string }{
			{"manifest_packages", pkgID}, {"applications", appID}, {"clusters", clusterID},
		} {
			if err := db.Exec("DELETE FROM "+row.table+" WHERE id=?", row.id).Error; err != nil {
				t.Error(err)
			}
		}
	})
	repo := manifestrepo.New(db)
	binding, err := repo.GetBinding(ctx, bindingID)
	requireManifestRepositoryNoError(t, err)
	holder := db.WithContext(ctx).Begin()
	requireManifestRepositoryNoError(t, holder.Error)
	defer func() { _ = holder.Rollback().Error }()
	requireManifestRepositoryNoError(t, holder.Exec(`SELECT id FROM manifest_packages WHERE id=? FOR SHARE`, pkgID).Error)
	var pid int
	requireManifestRepositoryNoError(t, holder.Raw(`SELECT pg_backend_pid()`).Row().Scan(&pid))
	done := make(chan error, 1)
	go func() {
		binding.Overlay = map[string]string{"image": "changed"}
		_, err := repo.UpdateBinding(ctx, binding, binding.Version)
		done <- err
	}()
	waitForManifestPackageLock(t, ctx, db, pid, done)
	requireManifestRepositoryNoError(t, holder.Exec(`SELECT id FROM manifest_bindings WHERE id=? FOR SHARE NOWAIT`, bindingID).Error)
	now := time.Now().UTC()
	_, err = manifestrepo.New(holder).SetDesiredRevision(ctx, domainmanifest.Deployment{
		ID: uuid.NewString(), PackageID: pkgID, BindingID: bindingID, CreatedAt: now, UpdatedAt: now,
		Spec: domainmanifest.DeploymentSpec{DesiredRevision: 1, ReconcilePolicy: "continuous", DriftPolicy: "report", DeletionPolicy: "orphan"},
	}, 0)
	requireManifestRepositoryNoError(t, err)
	requireManifestRepositoryNoError(t, holder.Commit().Error)
	select {
	case err := <-done:
		requireManifestRepositoryNoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stored, err := repo.GetBinding(ctx, bindingID)
	requireManifestRepositoryNoError(t, err)
	if stored.Overlay["image"] != "changed" {
		t.Fatal("binding edit did not complete after ordinary operation")
	}
}

func waitForManifestPackageLock(t *testing.T, ctx context.Context, db *gorm.DB, pid int, done <-chan error) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		requireManifestRepositoryNoError(t, db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ?=ANY(pg_blocking_pids(pid)))`, pid).Row().Scan(&waiting))
		if waiting {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("binding edit completed before reaching the held lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func verifyPendingPlanInventory(t *testing.T, ctx context.Context, tx *gorm.DB, repository *manifestrepo.Repository, deployment domainmanifest.Deployment) {
	t.Helper()
	requireManifestRepositoryNoError(t, tx.SavePoint("pending_inventory").Error)
	defer func() { requireManifestRepositoryNoError(t, tx.RollbackTo("pending_inventory").Error) }()
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.AppliedDigest = deployment.Spec.DesiredDigest
	deployment.Status.Phase = "converged"
	deployment.Status.Inventory = []domainmanifest.ResourceInventory{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "test", Name: "api", UID: "known-uid", Health: "healthy", LastObservedAt: time.Now().UTC().Truncate(time.Microsecond)}}
	requireManifestRepositoryNoError(t, repository.UpdateDeploymentStatus(ctx, deployment.ID, deployment.Generation, deployment.Status))
	next, err := repository.SetDesiredRevision(ctx, deployment, deployment.Generation)
	requireManifestRepositoryNoError(t, err)
	if next.Generation != deployment.Generation+1 || next.Status.ObservedGeneration != deployment.Generation || next.Status.Phase != "pending" || len(next.Status.Inventory) != 1 || next.Status.Inventory[0].UID != "known-uid" || next.Status.Inventory[0].Generation != deployment.Generation {
		t.Fatalf("pending Plan lost observed identities or fabricated health: %#v", next)
	}
	// A new observation, including partial/unhealthy inventory, is authoritative.
	next.Status.ObservedGeneration = next.Generation
	next.Status.Inventory = []domainmanifest.ResourceInventory{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "test", Name: "api", UID: "new-uid", Health: "degraded", LastObservedAt: time.Now().UTC().Truncate(time.Microsecond)}}
	requireManifestRepositoryNoError(t, repository.UpdateDeploymentStatus(ctx, next.ID, next.Generation, next.Status))
	loaded, err := repository.GetDeployment(ctx, next.ID)
	requireManifestRepositoryNoError(t, err)
	if len(loaded.Status.Inventory) != 1 || loaded.Status.Inventory[0].UID != "new-uid" || loaded.Status.Inventory[0].Health != "degraded" {
		t.Fatalf("old inventory replaced current observation: %#v", loaded.Status.Inventory)
	}
	next.Status.Inventory = nil
	requireManifestRepositoryNoError(t, repository.UpdateDeploymentStatus(ctx, next.ID, next.Generation, next.Status))
	loaded, err = repository.GetDeployment(ctx, next.ID)
	requireManifestRepositoryNoError(t, err)
	if len(loaded.Status.Inventory) != 0 {
		t.Fatal("empty current observation resurrected an old UID")
	}
}

func verifyResourceObservationRoundTrip(t *testing.T, ctx context.Context, repository *manifestrepo.Repository, deployment domainmanifest.Deployment) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	observed := int64(0)
	deployment.Status.Inventory = []domainmanifest.ResourceInventory{{
		DeploymentID: deployment.ID, Generation: deployment.Generation, APIVersion: "workloads.soha.io/v1alpha1",
		Kind: "WorkloadCronJob", Namespace: "test", Name: "periodic", UID: "observed-uid", ResourceVersion: "42",
		ResourceGeneration: 3, ObservedResourceGeneration: &observed, DeletingAt: &now,
		Finalizers: []string{"test.soha.io/cleanup"}, Health: "degraded", LastObservedAt: now,
	}}
	requireManifestRepositoryNoError(t, repository.UpdateDeploymentStatus(ctx, deployment.ID, deployment.Generation, deployment.Status))
	loaded, err := repository.GetDeploymentByBinding(ctx, deployment.BindingID)
	requireManifestRepositoryNoError(t, err)
	if len(loaded.Status.Inventory) == 1 {
		loaded.Status.Inventory[0].LastObservedAt = loaded.Status.Inventory[0].LastObservedAt.UTC()
		if value := loaded.Status.Inventory[0].DeletingAt; value != nil {
			utc := value.UTC()
			loaded.Status.Inventory[0].DeletingAt = &utc
		}
	}
	if !reflect.DeepEqual(loaded.Status.Inventory, deployment.Status.Inventory) {
		t.Fatalf("resource observations did not persist: %#v", loaded.Status.Inventory)
	}
	// A legacy executor's missing observation remains unknown, rather than zero.
	deployment.Status.Inventory[0].ObservedResourceGeneration = nil
	deployment.Status.Inventory[0].DeletingAt = nil
	deployment.Status.Inventory[0].Finalizers = []string{}
	requireManifestRepositoryNoError(t, repository.UpdateDeploymentStatus(ctx, deployment.ID, deployment.Generation, deployment.Status))
	loaded, err = repository.GetDeploymentByBinding(ctx, deployment.BindingID)
	requireManifestRepositoryNoError(t, err)
	if len(loaded.Status.Inventory) == 1 {
		loaded.Status.Inventory[0].LastObservedAt = loaded.Status.Inventory[0].LastObservedAt.UTC()
	}
	if !reflect.DeepEqual(loaded.Status.Inventory, deployment.Status.Inventory) {
		t.Fatal("missing observations acquired a fabricated value")
	}
}

func verifyManifestChangedInputConflict(t *testing.T, ctx context.Context, tx *gorm.DB, repository *manifestrepo.Repository, next domainmanifest.Deployment, binding domainmanifest.EnvironmentBinding, item domainmanifest.Package) {
	t.Helper()
	// The final database write must reject changes made after plan validation.
	for _, change := range []struct {
		name, query string
		args        []any
	}{
		{"binding version", `UPDATE manifest_bindings SET version=version+1 WHERE id=?`, []any{binding.ID}},
		{"binding scope", `UPDATE manifest_bindings SET namespace='other' WHERE id=?`, []any{binding.ID}},
		{"package revision", `UPDATE manifest_packages SET updated_at=updated_at+interval '1 second' WHERE id=?`, []any{item.ID}},
	} {
		t.Run(change.name, func(t *testing.T) {
			requireManifestRepositoryNoError(t, tx.SavePoint("changed_input").Error)
			requireManifestRepositoryNoError(t, tx.Exec(change.query, change.args...).Error)
			if _, err := repository.SetDesiredRevision(ctx, next, 2); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("changed input accepted: %v", err)
			}
			requireManifestRepositoryNoError(t, tx.RollbackTo("changed_input").Error)
		})
	}
	deployment, err := repository.GetDeploymentByBinding(ctx, binding.ID)
	if err != nil || deployment.Generation != 2 || !reflect.DeepEqual(deployment.Spec.DeliverySnapshot, next.Spec.DeliverySnapshot) {
		t.Fatalf("conflict changed desired state: %#v, %v", deployment, err)
	}
}

func verifyManifestObservationCadence(t *testing.T, ctx context.Context, tx *gorm.DB, repository *manifestrepo.Repository, deploymentID string) {
	t.Helper()
	requireManifestRepositoryNoError(t, tx.SavePoint("observation_cadence").Error)
	defer func() { requireManifestRepositoryNoError(t, tx.RollbackTo("observation_cadence").Error) }()
	for _, test := range []struct {
		phase string
		age   int
		due   bool
	}{
		{"reconciling", 5, false},
		{"reconciling", 11, true},
		{"converged", 11, false},
		{"converged", 61, true},
		{"degraded", 11, false},
	} {
		requireManifestRepositoryNoError(t, tx.Exec(`UPDATE manifest_deployment_status SET phase=?, last_reconciled_at=NOW() - (? * INTERVAL '1 second') WHERE deployment_id=?`, test.phase, test.age, deploymentID).Error)
		items, err := repository.ListContinuousDeployments(ctx, 100)
		requireManifestRepositoryNoError(t, err)
		found := slices.ContainsFunc(items, func(item domainmanifest.Deployment) bool { return item.ID == deploymentID })
		if found != test.due {
			t.Fatalf("%s observation after %ds: due=%v, want %v", test.phase, test.age, found, test.due)
		}
	}
}

func verifyHelmResourceOwnership(t *testing.T, ctx context.Context, tx *gorm.DB, manifests *manifestrepo.Repository, next domainmanifest.Deployment, binding domainmanifest.EnvironmentBinding, item domainmanifest.Package) {
	t.Helper()
	requireManifestRepositoryNoError(t, tx.SavePoint("helm_ownership").Error)
	defer func() { requireManifestRepositoryNoError(t, tx.RollbackTo("helm_ownership").Error) }()
	repo := deliveryrepo.New(tx)
	snapshot := sohaapi.HelmDeliverySnapshot{ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID, ServiceID: "service", TargetID: "helm-target", DeliveryPlanID: uuid.NewString(), ClusterID: binding.ClusterID, Namespace: "test", ReleaseName: "app", Resources: []sohaapi.HelmDeliveryResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "test", Name: "api"}}}
	makeTask := func(snapshot sohaapi.HelmDeliverySnapshot) domaindelivery.ExecutionTask {
		return domaindelivery.ExecutionTask{ID: uuid.NewString(), ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID, TaskKind: "helm_apply", ProviderKind: "helm_direct", Status: "queued", CallbackToken: uuid.NewString(), QueueKey: "helm:" + snapshot.ClusterID + ":" + snapshot.Namespace + ":" + snapshot.ReleaseName, Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: snapshot}}}
	}
	if _, err := repo.CreateExecutionTask(ctx, makeTask(snapshot)); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Helm took a Manifest resource: %v", err)
	}
	snapshot.Resources[0].Name = "helm-owned"
	task, err := repo.CreateExecutionTask(ctx, makeTask(snapshot))
	requireManifestRepositoryNoError(t, err)
	plan, err := repo.CreateDeliveryPlan(ctx, domaindelivery.DeliveryPlanInput{ID: snapshot.DeliveryPlanID, ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID, Action: "deploy", HelmSnapshots: []sohaapi.HelmDeliverySnapshot{snapshot}, HelmPreparedCiphertext: "encrypted-test-payload"}, "tester")
	requireManifestRepositoryNoError(t, err)
	plan, err = repo.GetDeliveryPlan(ctx, plan.ID)
	requireManifestRepositoryNoError(t, err)
	if len(plan.HelmSnapshots) != 1 || !reflect.DeepEqual(plan.HelmSnapshots[0], snapshot) || plan.HelmPreparedCiphertext != "encrypted-test-payload" {
		t.Fatal("Helm plan persistence lost its private preparation")
	}
	other := snapshot
	other.TargetID = "another-owner"
	other.Resources = []sohaapi.HelmDeliveryResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "test", Name: "different"}}
	if _, err := repo.CreateExecutionTask(ctx, makeTask(other)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("same release was concurrently admitted: %v", err)
	}
	changed := next
	manifestSnapshot := *next.Spec.DeliverySnapshot
	manifestSnapshot.Documents = slices.Clone(manifestSnapshot.Documents)
	manifestSnapshot.Documents[0].Name = "helm-owned"
	changed.Spec.DeliverySnapshot = &manifestSnapshot
	if _, err := manifests.SetDesiredRevision(ctx, changed, 2); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("Manifest took queued Helm resources: %v", err)
	}
	stopped, err := repo.RequestDeliveryTaskStop(ctx, task.ID, "cancel before dispatch")
	requireManifestRepositoryNoError(t, err)
	if stopped.Status != "canceled" {
		t.Fatal("unclaimed Helm cancellation was not acknowledged")
	}
	if _, err := manifests.SetDesiredRevision(ctx, changed, 2); err != nil {
		t.Fatalf("never-started Helm cancellation retained resources: %v", err)
	}
	// A claimed task remains canceling until its executor confirms stopping.
	requireManifestRepositoryNoError(t, tx.Exec(`UPDATE execution_tasks SET status='running',attempt_count=1 WHERE id=?`, task.ID).Error)
	stopped, err = repo.RequestDeliveryTaskStop(ctx, task.ID, "cancel after claim")
	requireManifestRepositoryNoError(t, err)
	if stopped.Status != "canceling" {
		t.Fatal("running Helm cancellation claimed an unconfirmed stop")
	}
	if _, err := repo.CreateExecutionTask(ctx, makeTask(other)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("canceling Helm task released its native release: %v", err)
	}
}

func requireManifestRepositoryNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func verifyDeliveryPlanRoundTrip(t *testing.T, ctx context.Context, plans *deliveryrepo.Repository, applicationID string, snapshot domainmanifest.DeliverySnapshot) {
	t.Helper()
	plan, err := plans.CreateDeliveryPlan(ctx, domaindelivery.DeliveryPlanInput{ID: snapshot.DeliveryPlanID, ApplicationID: applicationID, ApplicationEnvironmentID: snapshot.ApplicationEnvironmentID, Action: domaindelivery.ApplicationDeliveryActionDeploy, ManifestSnapshots: []domainmanifest.DeliverySnapshot{snapshot}}, "tester")
	requireManifestRepositoryNoError(t, err)
	plan, err = plans.GetDeliveryPlan(ctx, plan.ID)
	requireManifestRepositoryNoError(t, err)
	if !reflect.DeepEqual(plan.ManifestSnapshots, []domainmanifest.DeliverySnapshot{snapshot}) {
		t.Fatalf("plan snapshot round trip: got %#v, want %#v", plan.ManifestSnapshots, []domainmanifest.DeliverySnapshot{snapshot})
	}
	plan.Status = domaindelivery.DeliveryPlanStatusConfirmed
	if _, err := plans.UpdateDeliveryPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	plan, err = plans.GetDeliveryPlan(ctx, plan.ID)
	if err != nil || plan.Status != domaindelivery.DeliveryPlanStatusConfirmed || !reflect.DeepEqual(plan.ManifestSnapshots, []domainmanifest.DeliverySnapshot{snapshot}) {
		t.Fatalf("updated plan: %#v, %v", plan, err)
	}

}

func verifyManifestResourceOwnership(t *testing.T, ctx context.Context, tx *gorm.DB, repository *manifestrepo.Repository, next domainmanifest.Deployment, binding domainmanifest.EnvironmentBinding, item domainmanifest.Package) {
	t.Helper()
	requireManifestRepositoryNoError(t, tx.SavePoint("ownership").Error)
	defer func() { requireManifestRepositoryNoError(t, tx.RollbackTo("ownership").Error) }()
	otherPackage := item
	otherPackage.ID, otherPackage.Bindings = uuid.NewString(), nil
	otherPackage, err := repository.Create(ctx, otherPackage)
	requireManifestRepositoryNoError(t, err)
	otherBinding := binding
	otherBinding.ID, otherBinding.PackageID = uuid.NewString(), otherPackage.ID
	otherBinding, err = repository.CreateBinding(ctx, otherBinding)
	requireManifestRepositoryNoError(t, err)
	otherPackage, err = repository.Get(ctx, otherPackage.ID)
	requireManifestRepositoryNoError(t, err)
	other := next
	other.ID, other.PackageID, other.BindingID = uuid.NewString(), otherPackage.ID, otherBinding.ID
	snapshot := *next.Spec.DeliverySnapshot
	snapshot.PackageID, snapshot.BindingID, snapshot.PackageUpdatedAt = otherPackage.ID, otherBinding.ID, otherPackage.UpdatedAt
	snapshot.Documents = slices.Clone(snapshot.Documents)
	// An API version is not a distinct Kubernetes object identity.
	snapshot.Documents[0].APIVersion = "v2"
	other.Spec.DeliverySnapshot = &snapshot
	if _, err := repository.SetDesiredRevision(ctx, other, 0); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("overlapping binding admitted: %v", err)
	}
	snapshot.Documents[0].Name = "independent"
	if _, err := repository.SetDesiredRevision(ctx, other, 0); err != nil {
		t.Fatalf("independent resource rejected: %v", err)
	}

	payload := domainmanifest.TaskPayload{Action: "apply", BindingID: binding.ID, ClusterID: binding.ClusterID, Documents: next.Spec.DeliverySnapshot.Documents}
	data, err := json.Marshal(payload)
	requireManifestRepositoryNoError(t, err)
	taskPayload := map[string]any{}
	requireManifestRepositoryNoError(t, json.Unmarshal(data, &taskPayload))
	task := domaindelivery.ExecutionTask{ID: uuid.NewString(), ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID, TaskKind: "manifest_apply", ProviderKind: "manifest_direct", TargetKind: "k8s", Status: "queued", Payload: taskPayload}
	run := domainmanifest.OperationRun{ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID, DeploymentID: next.ID, Generation: 2, Action: "apply", IdempotencyKey: uuid.NewString(), CreatedAt: time.Now().UTC()}
	taskID, created, err := repository.CreateOperationTask(ctx, run, task)
	requireManifestRepositoryNoError(t, err)
	if !created {
		t.Fatal("operation was not admitted")
	}
	if repeated, created, err := repository.CreateOperationTask(ctx, run, task); err != nil || created || repeated != taskID {
		t.Fatalf("operation not idempotent: %s %v %v", repeated, created, err)
	}
	for _, status := range []string{"queued", "running", "canceling"} {
		requireManifestRepositoryNoError(t, tx.Exec(`UPDATE execution_tasks SET status = ? WHERE id = ?`, status, taskID).Error)
		if _, err := repository.SetDesiredRevision(ctx, next, 2); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("%s operation did not retain ownership: %v", status, err)
		}
	}
	requireManifestRepositoryNoError(t, tx.Exec(`UPDATE execution_tasks SET status = 'canceled' WHERE id = ?`, taskID).Error)
	if _, err := repository.SetDesiredRevision(ctx, next, 2); err != nil {
		t.Fatalf("acknowledged cancellation did not release target: %v", err)
	}
	run.ID, run.IdempotencyKey, task.ID = uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, _, err := repository.CreateOperationTask(ctx, run, task); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("superseded generation dispatched: %v", err)
	}
}

func verifyConfigurationRevisionCAS(t *testing.T, ctx context.Context, repository *manifestrepo.Repository, applicationID string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	item, err := repository.Create(ctx, domainmanifest.Package{
		ID: uuid.NewString(), Name: "Configuration revision", ApplicationID: applicationID, Renderer: domainmanifest.RendererRaw,
		Status: domainmanifest.StatusDraft, Files: []domainmanifest.File{{Path: "config.yaml", Content: "original"}}, Bindings: []domainmanifest.Binding{}, CreatedAt: now, UpdatedAt: now,
	})
	requireManifestRepositoryNoError(t, err)
	now = item.UpdatedAt
	stale := item
	stale.ExpectedUpdatedAt = &now
	stale.CurrentRevision, stale.Status, stale.UpdatedAt = 1, domainmanifest.StatusPublished, now.Add(2*time.Second)
	revision := domainmanifest.Revision{ID: uuid.NewString(), PackageID: item.ID, Version: 1, Digest: "original", Files: item.Files, Bindings: item.Bindings, CreatedAt: stale.UpdatedAt}
	item.ExpectedUpdatedAt = &now
	item.Files = []domainmanifest.File{{Path: "config.yaml", Content: "updated"}}
	item.UpdatedAt = now.Add(time.Second)
	item, err = repository.Update(ctx, item.ID, item)
	requireManifestRepositoryNoError(t, err)
	if _, err := repository.Publish(ctx, stale, revision); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale configuration snapshot: %v", err)
	}
	revisions, err := repository.ListRevisions(ctx, item.ID)
	requireManifestRepositoryNoError(t, err)
	if len(revisions) != 0 {
		t.Fatal("conflicting snapshot left a revision")
	}
	expected := item.UpdatedAt
	item.ExpectedUpdatedAt = &expected
	item.CurrentRevision, item.Status, item.UpdatedAt = 1, domainmanifest.StatusPublished, now.Add(3*time.Second)
	revision.Files, revision.Digest, revision.CreatedAt = item.Files, "updated", item.UpdatedAt
	saved, err := repository.Publish(ctx, item, revision)
	requireManifestRepositoryNoError(t, err)
	if saved.CurrentRevision != 1 || saved.Files[0].Content != "updated" {
		t.Fatalf("saved configuration: %#v", saved)
	}
	if _, err := repository.Publish(ctx, item, revision); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate snapshot: %v", err)
	}
	revisions, err = repository.ListRevisions(ctx, item.ID)
	requireManifestRepositoryNoError(t, err)
	if len(revisions) != 1 || revisions[0].Files[0].Content != "updated" {
		t.Fatalf("immutable revisions: %#v", revisions)
	}
}

func verifyGitOpsResourceOwnership(t *testing.T, ctx context.Context, tx *gorm.DB, repository *manifestrepo.Repository, next domainmanifest.Deployment, binding domainmanifest.EnvironmentBinding, item domainmanifest.Package) {
	t.Helper()
	requireManifestRepositoryNoError(t, tx.SavePoint("gitops_ownership").Error)
	defer func() { requireManifestRepositoryNoError(t, tx.RollbackTo("gitops_ownership").Error) }()
	child := domainmanifest.RenderedDocument{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "test", Name: "gitops-child"}
	root := domainmanifest.RenderedDocument{APIVersion: "argoproj.io/v1alpha1", Kind: "Application", Namespace: "test", Name: "gitops-root"}
	frozen := *next.Spec.DeliverySnapshot
	frozen.DeliveryPlanID = uuid.NewString()
	frozen.Documents, frozen.GitOpsDocuments = []domainmanifest.RenderedDocument{root}, []domainmanifest.RenderedDocument{child}
	next.Spec.DeliverySnapshot = &frozen
	owned, err := repository.SetDesiredRevision(ctx, next, 2)
	requireManifestRepositoryNoError(t, err)
	if !reflect.DeepEqual(owned.Spec.DeliverySnapshot.GitOpsDocuments, frozen.GitOpsDocuments) {
		t.Fatal("GitOps snapshot persistence lost children")
	}
	verifyDeliveryPlanRoundTrip(t, ctx, deliveryrepo.New(tx), item.ApplicationID, frozen)
	keys := domainmanifest.ResourceKeys(binding.ClusterID, []domainmanifest.RenderedDocument{child})
	if err := deliveryrepo.CheckManifestResourceOwners(tx, binding.ClusterID, "other-binding", keys); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("GitOps snapshot did not reserve child: %v", err)
	}
	helm := sohaapi.HelmDeliverySnapshot{DeliveryPlanID: uuid.NewString(), ClusterID: binding.ClusterID, Namespace: "test", ReleaseName: "other", ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID, ServiceID: "other", TargetID: "other", Resources: []sohaapi.HelmDeliveryResource{{APIVersion: child.APIVersion, Kind: child.Kind, Namespace: child.Namespace, Name: child.Name}}}
	if _, err := deliveryrepo.New(tx).CreateExecutionTask(ctx, domaindelivery.ExecutionTask{ID: uuid.NewString(), ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID, TaskKind: "helm_apply", ProviderKind: "helm_direct", Status: "queued", CallbackToken: uuid.NewString(), Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: helm}}}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Helm took a GitOps child: %v", err)
	}
	// Only the task retains this different child, proving the second SQL branch.
	taskChild := child
	taskChild.Name = "gitops-task-only"
	payload := domainmanifest.TaskPayload{Action: "apply", BindingID: binding.ID, ClusterID: binding.ClusterID, Documents: []domainmanifest.RenderedDocument{root}, GitOpsDocuments: []domainmanifest.RenderedDocument{taskChild}}
	data, err := json.Marshal(payload)
	requireManifestRepositoryNoError(t, err)
	body := map[string]any{}
	requireManifestRepositoryNoError(t, json.Unmarshal(data, &body))
	task := domaindelivery.ExecutionTask{ID: uuid.NewString(), ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID, TaskKind: "manifest_apply", ProviderKind: "manifest_direct", Status: "queued", Payload: body}
	run := domainmanifest.OperationRun{ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID, DeploymentID: next.ID, Generation: owned.Generation, Action: "apply", IdempotencyKey: uuid.NewString(), CreatedAt: time.Now().UTC()}
	taskID, created, err := repository.CreateOperationTask(ctx, run, task)
	requireManifestRepositoryNoError(t, err)
	if !created {
		t.Fatal("GitOps operation was not created")
	}
	for _, status := range []string{"queued", "running", "canceling"} {
		requireManifestRepositoryNoError(t, tx.Exec(`UPDATE execution_tasks SET status=? WHERE id=?`, status, taskID).Error)
		if err := deliveryrepo.CheckManifestResourceOwners(tx, binding.ClusterID, "other-binding", domainmanifest.ResourceKeys(binding.ClusterID, []domainmanifest.RenderedDocument{taskChild})); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("%s GitOps task did not reserve child: %v", status, err)
		}
	}
	// Incoming GitOps children must also respect another binding's ownership.
	requireManifestRepositoryNoError(t, tx.Exec(`UPDATE execution_tasks SET status='canceled' WHERE id=?`, taskID).Error)
	otherPackage := item
	otherPackage.ID, otherPackage.Bindings = uuid.NewString(), nil
	otherPackage, err = repository.Create(ctx, otherPackage)
	requireManifestRepositoryNoError(t, err)
	otherBinding := binding
	otherBinding.ID, otherBinding.PackageID = uuid.NewString(), otherPackage.ID
	otherBinding, err = repository.CreateBinding(ctx, otherBinding)
	requireManifestRepositoryNoError(t, err)
	otherPackage, err = repository.Get(ctx, otherPackage.ID)
	requireManifestRepositoryNoError(t, err)
	incoming := *next.Spec.DeliverySnapshot
	incoming.PackageID, incoming.BindingID, incoming.PackageUpdatedAt = otherPackage.ID, otherBinding.ID, otherPackage.UpdatedAt
	incoming.Documents = []domainmanifest.RenderedDocument{{APIVersion: root.APIVersion, Kind: root.Kind, Namespace: root.Namespace, Name: "other-root"}}
	other := next
	other.ID, other.PackageID, other.BindingID = uuid.NewString(), otherPackage.ID, otherBinding.ID
	other.Spec.DeliverySnapshot = &incoming
	if _, err := repository.SetDesiredRevision(ctx, other, 0); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("incoming GitOps child ignored another binding: %v", err)
	}
}

func verifyRootCRPublicationControl(t *testing.T, ctx context.Context, tx *gorm.DB, repository *manifestrepo.Repository, applicationID, environmentID, clusterID string) {
	t.Helper()
	for _, mode := range []string{"valid", "withdrawn", "uid-replaced", "baseline-changed", "binding-changed"} {
		t.Run(mode, func(t *testing.T) {
			requireManifestRepositoryNoError(t, tx.SavePoint("root_cr").Error)
			defer func() { requireManifestRepositoryNoError(t, tx.RollbackTo("root_cr").Error) }()
			now := time.Now().UTC().Truncate(time.Microsecond)
			item, err := repository.Create(ctx, domainmanifest.Package{ID: uuid.NewString(), Name: "Generic CR", ApplicationID: applicationID, Renderer: "raw_yaml", Status: "published", CurrentRevision: 1, Files: []domainmanifest.File{}, Bindings: []domainmanifest.Binding{}, CreatedAt: now, UpdatedAt: now})
			requireManifestRepositoryNoError(t, err)
			binding, err := repository.CreateBinding(ctx, domainmanifest.EnvironmentBinding{ID: uuid.NewString(), PackageID: item.ID, ApplicationEnvironmentID: environmentID, ClusterID: clusterID, Namespace: "test", Enabled: true, Version: 1, DriftPolicy: "report", DeletionPolicy: "orphan", Overlay: map[string]string{}, CreatedAt: now, UpdatedAt: now})
			requireManifestRepositoryNoError(t, err)
			item, err = repository.Get(ctx, item.ID)
			requireManifestRepositoryNoError(t, err)
			targetID := uuid.NewString()
			metadata := map[string]any{"importSource": "kubernetes", "ownershipMode": "managed", "rootCRGeneration": 2, "resourceSource": `{"resource":{"apiVersion":"unknown.example/v1","kind":"Database","uid":"root-uid"}}`}
			switch mode {
			case "withdrawn":
				metadata["ownershipMode"] = "observe_only"
			case "uid-replaced":
				metadata["resourceSource"] = `{"resource":{"apiVersion":"unknown.example/v1","kind":"Database","uid":"new-uid"}}`
			case "baseline-changed":
				metadata["rootCRGeneration"] = 3
			}
			data, err := json.Marshal(metadata)
			requireManifestRepositoryNoError(t, err)
			configRef := binding.ID
			if mode == "binding-changed" {
				configRef = "different"
			}
			requireManifestRepositoryNoError(t, tx.Exec(`INSERT INTO release_targets (id,application_environment_id,cluster_id,namespace,target_kind,executor_kind,config_ref,workload_kind,workload_name,metadata,enabled) VALUES (?,?,?,'test','kubernetes_workload','manifest_ssa',?,'CustomResource','root',?::jsonb,TRUE)`, targetID, environmentID, clusterID, configRef, string(data)).Error)
			root := &sohaapi.ManifestRootResource{APIVersion: "unknown.example/v1", Kind: "Database", Namespace: "test", Name: "root", UID: "root-uid", Generation: 2, CrdName: "databases.unknown.example", TargetID: targetID, DeliveryPlanID: uuid.NewString()}
			snapshot := domainmanifest.DeliverySnapshot{RootResource: root, DeliveryPlanID: root.DeliveryPlanID, TargetID: targetID, PackageID: item.ID, BindingID: binding.ID, BindingVersion: binding.Version, ApplicationEnvironmentID: environmentID, ClusterID: clusterID, Namespace: "test", Revision: 1, PackageUpdatedAt: item.UpdatedAt, Documents: []domainmanifest.RenderedDocument{{APIVersion: root.APIVersion, Kind: root.Kind, Namespace: "test", Name: "root"}}}
			next := domainmanifest.Deployment{ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID, CreatedAt: now, UpdatedAt: now, Spec: domainmanifest.DeploymentSpec{DesiredRevision: 1, ReconcilePolicy: "continuous", DriftPolicy: "report", DeletionPolicy: "orphan", DeliverySnapshot: &snapshot}}
			admission := domainmanifest.TaskPayload{Action: "apply", BindingID: binding.ID, ClusterID: clusterID, Namespace: "test", Documents: snapshot.Documents}
			rejectLegacyRootAdmission(t, ctx, repository, admission)
			admission.RootResource = root
			if err := repository.ValidateImportedManifestTargets(ctx, admission); mode == "valid" && err != nil || mode != "valid" && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("root ownership admission: %v", err)
			}
			stored, err := repository.SetDesiredRevision(ctx, next, 0)
			if mode != "valid" {
				if !errors.Is(err, apperrors.ErrConflict) {
					t.Fatalf("changed control accepted: %v", err)
				}
				return
			}
			requireManifestRepositoryNoError(t, err)
			if stored.Spec.DeliverySnapshot.RootResource.UID != "root-uid" {
				t.Fatal("root fence not persisted")
			}
			data, err = json.Marshal(admission)
			requireManifestRepositoryNoError(t, err)
			wire := map[string]any{}
			requireManifestRepositoryNoError(t, json.Unmarshal(data, &wire))
			task := domaindelivery.ExecutionTask{ID: uuid.NewString(), ApplicationID: applicationID, ApplicationEnvironmentID: environmentID, TaskKind: "manifest_apply", ProviderKind: "manifest_direct", TargetKind: "manifest_deployment", Status: "queued", Payload: wire, SecretPrincipal: domainidentity.Principal{UserID: "actor", AccessTokenID: "private-token-id"}}
			run := domainmanifest.OperationRun{ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID, DeploymentID: stored.ID, Generation: stored.Generation, Action: "apply", IdempotencyKey: uuid.NewString(), CreatedAt: now}
			taskID, _, err := repository.CreateOperationTask(ctx, run, task)
			requireManifestRepositoryNoError(t, err)
			restored, err := deliveryrepo.New(tx).GetExecutionTask(ctx, taskID)
			requireManifestRepositoryNoError(t, err)
			if restored.SecretPrincipal.AccessTokenID != "private-token-id" {
				t.Fatal("execution token identity lost")
			}
			public, err := json.Marshal(restored)
			requireManifestRepositoryNoError(t, err)
			if strings.Contains(string(public), "private-token-id") {
				t.Fatal("private token identity leaked")
			}

		})
	}
}

func rejectLegacyRootAdmission(t *testing.T, ctx context.Context, repository *manifestrepo.Repository, payload domainmanifest.TaskPayload) {
	t.Helper()
	for _, namespace := range []string{payload.Namespace, "different-binding-namespace"} {
		payload.Namespace = namespace
		if err := repository.ValidateImportedManifestTargets(ctx, payload); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("legacy publication bypass accepted from binding namespace %s: %v", namespace, err)
		}
	}
}
