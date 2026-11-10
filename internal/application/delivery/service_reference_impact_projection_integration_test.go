package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	repomanifest "github.com/opensoha/soha/internal/repository/manifest"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestServiceReferencePlanImpactProjectionPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to this run's isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable"}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}

	x := newImpactProjectionPGFixture(t, ctx, store.DB())
	empty := x.newPlan(t, ctx, "empty")
	snapshot, err := x.repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, empty.ID)
	if err != nil || snapshot.Consumers == nil || len(snapshot.Consumers) != 0 || snapshot.Version != dd.ServiceReferencePlanImpactVersion {
		t.Fatalf("missing durable empty evidence: %+v, %v", snapshot, err)
	}
	x.exec(t, ctx, `UPDATE manifest_bindings SET overlay='{"image":"irrelevant"}' WHERE id=?`, x.draftBinding)
	x.assertCurrent(t, ctx, empty)
	x.attachReference(t, ctx)
	x.assertBlocked(t, ctx, empty)

	frozen := x.newPlan(t, ctx, "referenced")
	x.exec(t, ctx, `UPDATE manifest_packages SET description='Reworded', files='[{"path":"draft.yaml","content":"synthetic draft"}]' WHERE id IN (?,?)`, x.pkg, x.draftPkg)
	x.exec(t, ctx, `UPDATE manifest_bindings SET overlay='{"image":"other-env"}' WHERE id=?`, x.crossEnvBinding)
	x.assertCurrent(t, ctx, frozen)
	x.exec(t, ctx, `UPDATE manifest_bindings SET overlay='{"image":"new-render"}' WHERE id=?`, x.bindingID)
	x.assertBlocked(t, ctx, frozen)

	legacy := x.newPlan(t, ctx, "legacy")
	x.exec(t, ctx, `UPDATE delivery_plan_service_reference_impacts SET snapshot=jsonb_set(snapshot,'{version}','1'::jsonb) WHERE plan_id=?`, legacy.ID)
	snapshot, err = x.repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, legacy.ID)
	if err != nil || snapshot.Version != 1 {
		t.Fatalf("historical evidence was not retained: %d, %v", snapshot.Version, err)
	}
	x.assertBlocked(t, ctx, legacy)

	t.Run("legacy-confirming-recovery", func(t *testing.T) {
		verifyLegacyHelmRecoveryPostgres(t, ctx, store.DB(), x.f, x.appID, x.sourceID)
	})
	t.Run("deleted-target-keeps-frozen-impact", func(t *testing.T) {
		x.verifyDeletedTarget(t, ctx)
	})
}

type impactProjectionPGFixture struct {
	f               referenceFixture
	db              *gorm.DB
	repo            *repodelivery.Repository
	id              string
	appID           string
	serviceID       string
	clusterID       string
	sourceID        string
	consumerID      string
	helmTargetID    string
	pkg             string
	bindingID       string
	draftPkg        string
	draftBinding    string
	crossEnvBinding string
}

func newImpactProjectionPGFixture(t *testing.T, ctx context.Context, db *gorm.DB) impactProjectionPGFixture {
	t.Helper()
	f := newReferenceFixture(t)
	id := "impact-projection-" + uuid.NewString()
	appID, serviceID, clusterID := id+"-app", id+"-service", id+"-cluster"
	sourceID, consumerID := id+"-source", id+"-consumer"
	sourceEnv, consumerEnv := id+"-source-env", id+"-consumer-env"
	targetID, helmTargetID := id+"-manifest-target", id+"-helm-target"
	pkg, bindingID := id+"-manifest", id+"-manifest-binding"
	draftPkg, draftBinding := id+"-draft", id+"-draft-binding"
	crossEnvBinding := id + "-other-binding"
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.WithContext(ctx).Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, query := range []string{
			`DELETE FROM execution_tasks WHERE application_id=?`,
			`DELETE FROM delivery_plans WHERE application_id=?`,
			`DELETE FROM environment_service_references WHERE application_id=?`,
			`DELETE FROM manifest_packages WHERE application_id=?`,
			`DELETE FROM application_environments WHERE application_id=?`,
			`DELETE FROM application_services WHERE application_id=?`,
			`DELETE FROM applications WHERE id=?`,
		} {
			if err := db.Exec(query, appID).Error; err != nil {
				t.Error(err)
			}
		}
		if err := db.Exec(`DELETE FROM delivery_environments WHERE id IN (?, ?)`, sourceEnv, consumerEnv).Error; err != nil {
			t.Error(err)
		}
		if err := db.Exec(`DELETE FROM clusters WHERE id=?`, clusterID).Error; err != nil {
			t.Error(err)
		}
	})

	apps := referenceApplicationsFixture(t, &f)
	apps.app.ID, apps.services[0].ApplicationID, apps.services[0].ID = appID, appID, serviceID
	f.service.applications = apps
	source, consumer := f.catalog.bindings["test-b"], f.catalog.bindings["test-a"]
	source.ID, source.ApplicationID, source.EnvironmentID = sourceID, appID, sourceEnv
	consumer.ID, consumer.ApplicationID, consumer.EnvironmentID, consumer.Targets = consumerID, appID, consumerEnv, nil
	target := &source.Targets[0]
	target.ID, target.ServiceID, target.ApplicationEnvironmentID, target.ClusterID = targetID, serviceID, sourceID, clusterID
	target.Enabled, target.ExecutorKind, target.ConfigRef = true, "manifest_ssa", bindingID
	observed := f.targets.sources["cluster-b/backend/Deployment/api/"]
	observed.Resource.ClusterID = clusterID
	raw, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	target.Metadata["serviceId"], target.Metadata["ownershipMode"], target.Metadata["resourceSource"] = serviceID, "managed", string(raw)
	targetMetadata, err := json.Marshal(target.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	source.Targets = append(source.Targets, dc.ReleaseTarget{ID: helmTargetID, ApplicationEnvironmentID: sourceID, ClusterID: clusterID, Namespace: "backend", TargetKind: "helm_release", ExecutorKind: "helm_sdk", WorkloadKind: "HelmRelease", WorkloadName: "demo", Enabled: true})
	f.catalog.bindings = map[string]dc.ApplicationEnvironment{sourceID: source, consumerID: consumer}
	f.catalog.environments[0].ID, f.catalog.environments[1].ID = consumerEnv, sourceEnv
	f.targets.sources = map[string]dr.ResourceSourceSnapshot{clusterID + "/backend/Deployment/api/": observed}
	f.service.permissions = deliveryActionPermissions(appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationEnvView, appaccess.PermDeliveryReleasesTrigger)
	repo := repodelivery.New(db)
	f.service.repository = repo
	f.service.SetServiceReferenceManifestConfigurationReader(repomanifest.New(db))

	exec(`INSERT INTO applications(id,name,app_key,app_group,language) VALUES (?,'Impact test',?,'test','configuration')`, appID, appID)
	exec(`INSERT INTO application_services(id,application_id,service_key,service_name) VALUES (?,?,?,'API')`, serviceID, appID, serviceID)
	exec(`INSERT INTO clusters(id,name) VALUES (?,'Impact test')`, clusterID)
	exec(`INSERT INTO delivery_environments(id,environment_key,name,tier,stage_level) VALUES (?,?,?,'test',20),(?,?,?,'test',20)`, sourceEnv, sourceEnv, "Source", consumerEnv, consumerEnv, "Consumer")
	exec(`INSERT INTO application_environments(id,application_id,environment_id) VALUES (?,?,?),(?,?,?)`, sourceID, appID, sourceEnv, consumerID, appID, consumerEnv)
	exec(`INSERT INTO release_targets(id,application_environment_id,cluster_id,namespace,target_kind,executor_kind,config_ref,workload_kind,workload_name,metadata,enabled) VALUES (?,?,?,'backend','deployment','manifest_ssa',?,'Deployment','api',?::jsonb,true)`, targetID, sourceID, clusterID, bindingID, string(targetMetadata))
	exec(`INSERT INTO release_targets(id,application_environment_id,cluster_id,namespace,target_kind,executor_kind,workload_kind,workload_name,enabled) VALUES (?,?,?,'backend','helm_release','helm_sdk','HelmRelease','demo',true)`, helmTargetID, sourceID, clusterID)
	exec(`INSERT INTO manifest_packages(id,name,application_id) VALUES (?,'Referenced',?),(?,'Unrelated draft',?)`, pkg, appID, draftPkg, appID)
	exec(`INSERT INTO manifest_bindings(id,package_id,application_environment_id,environment_key,cluster_id,namespace) VALUES (?,?,?, 'source',?,'backend'),(?,?,?,'source',?,'draft'),(?,?,?,'other',?,'backend')`, bindingID, pkg, sourceID, clusterID, draftBinding, draftPkg, sourceID, clusterID, crossEnvBinding, pkg, consumerID, clusterID)

	return impactProjectionPGFixture{
		f: f, db: db, repo: repo,
		id:              id,
		clusterID:       clusterID,
		appID:           appID,
		serviceID:       serviceID,
		sourceID:        sourceID,
		consumerID:      consumerID,
		helmTargetID:    helmTargetID,
		pkg:             pkg,
		bindingID:       bindingID,
		draftPkg:        draftPkg,
		draftBinding:    draftBinding,
		crossEnvBinding: crossEnvBinding,
	}
}

func (x impactProjectionPGFixture) exec(t *testing.T, ctx context.Context, query string, args ...any) {
	t.Helper()
	if err := x.db.WithContext(ctx).Exec(query, args...).Error; err != nil {
		t.Fatal(err)
	}
}

func (x impactProjectionPGFixture) newPlan(t *testing.T, ctx context.Context, suffix string) dd.DeliveryPlan {
	t.Helper()
	planID := x.id + "-" + suffix
	input := dd.DeliveryPlanInput{ID: planID, Source: "workbench", ApplicationID: x.appID, ApplicationEnvironmentID: x.sourceID, Action: dd.ApplicationDeliveryActionDeploy, TargetID: x.helmTargetID, TargetIDs: []string{x.helmTargetID}, RequiresApproval: true,
		HelmSnapshots: []sohaapi.HelmDeliverySnapshot{{DeliveryPlanID: planID, TargetID: x.helmTargetID, ClusterID: x.clusterID, Namespace: "backend", ReleaseName: "demo"}},
		Impact:        map[string]any{"approval": []any{map[string]any{"status": "approved"}}}}
	plan, err := x.f.service.createPlanWithServiceReferenceImpact(ctx, deliveryActionPrincipal(), input)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func (x impactProjectionPGFixture) assertCurrent(t *testing.T, ctx context.Context, plan dd.DeliveryPlan) {
	t.Helper()
	if err := x.f.service.validateServiceReferenceImpact(ctx, deliveryActionPrincipal(), plan); err != nil {
		t.Fatal(err)
	}
}

func (x impactProjectionPGFixture) assertBlocked(t *testing.T, ctx context.Context, plan dd.DeliveryPlan) {
	t.Helper()
	if _, err := x.f.service.confirmRuntimeDeliveryPlan(ctx, deliveryActionPrincipal(), plan); !errors.Is(err, apperrors.ErrConflict) || !strings.Contains(err.Error(), "new Plan preflight") {
		t.Fatalf("stale approved Plan was not blocked before execution: %v", err)
	}
	stored, err := x.repo.GetDeliveryPlan(ctx, plan.ID)
	if err != nil || stored.Status != dd.DeliveryPlanStatusDraft {
		t.Fatalf("stale Plan status changed: %q, %v", stored.Status, err)
	}
	var tasks int64
	if err := x.db.Raw(`SELECT count(*) FROM execution_tasks WHERE application_id=?`, x.appID).Row().Scan(&tasks); err != nil || tasks != 0 {
		t.Fatalf("stale confirmation created execution tasks: %d, %v", tasks, err)
	}
}

func (x impactProjectionPGFixture) attachReference(t *testing.T, ctx context.Context) {
	t.Helper()
	preview, err := x.f.service.observeSourceReferenceGroup(ctx, deliveryActionPrincipal(), x.sourceID, x.serviceID)
	if err != nil {
		t.Fatal(err)
	}
	preview.Consumer = referenceScope(x.f.catalog.bindings[x.consumerID], x.f.catalog.environments[0])
	identity, err := dd.ServiceReferenceIdentityRevision(preview)
	if err != nil {
		t.Fatal(err)
	}
	control, err := dd.ServiceReferenceControlRevision(x.f.catalog.bindings[x.sourceID], x.serviceID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	x.exec(t, ctx, `INSERT INTO environment_service_references(id,application_id,consumer_binding_id,service_id,source_binding_id,revision,preview,source_identity_revision,control_revision,consumer_configuration_revision,created_at,updated_at) VALUES (?,?,?,?,?,1,?::jsonb,?,?,?,now(),now())`, x.id, x.appID, x.consumerID, x.serviceID, x.sourceID, string(encoded), identity, control, strings.Repeat("c", 64))
}

func (x impactProjectionPGFixture) verifyDeletedTarget(t *testing.T, ctx context.Context) {
	t.Helper()
	p := deliveryActionPrincipal()
	plan := x.newPlan(t, ctx, "removed-target")
	before, err := x.f.service.GetDeliveryPlanServiceReferenceImpact(ctx, p, plan.ID, "", 10)
	if err != nil || !before.IsCurrent || len(before.Revision) != 64 || len(before.Items) != 1 {
		t.Fatalf("current impact not readable: %+v, %v", before, err)
	}
	x.exec(t, ctx, `DELETE FROM release_targets WHERE id=?`, x.helmTargetID)
	binding := x.f.catalog.bindings[x.sourceID]
	binding.Targets = binding.Targets[:1]
	x.f.catalog.bindings[x.sourceID] = binding
	after, err := x.f.service.GetDeliveryPlanServiceReferenceImpact(ctx, p, plan.ID, "", 10)
	if err != nil || after.IsCurrent || after.Revision != before.Revision || len(after.CurrentRevision) != 64 ||
		after.CurrentRevision == after.Revision || !reflect.DeepEqual(after.Items, before.Items) {
		t.Fatalf("frozen impact lost after target deletion: %+v, %v", after, err)
	}
	x.f.catalog.denied[x.sourceID] = apperrors.ErrAccessDenied
	if _, err := x.f.service.GetDeliveryPlanServiceReferenceImpact(ctx, p, plan.ID, "", 10); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("stale impact bypassed source authorization: %v", err)
	}
	delete(x.f.catalog.denied, x.sourceID)
	x.assertBlocked(t, ctx, plan)
}

func verifyLegacyHelmRecoveryPostgres(t *testing.T, ctx context.Context, db *gorm.DB, f referenceFixture, appID, sourceID string) {
	for _, scenario := range []struct {
		name, firstTask, secondTask, finalPlan string
	}{
		{"zero-tasks", "", "", dd.DeliveryPlanStatusDraft},
		{"partial-terminal", "failed", "", dd.DeliveryPlanStatusDraft},
		{"partial-queued", "queued", "", dd.DeliveryPlanStatusConfirming},
		{"partial-active", "running", "", dd.DeliveryPlanStatusConfirming},
		{"partial-ambiguous", "ambiguous", "", dd.DeliveryPlanStatusConfirming},
		{"receipt-complete", "failed", "canceled", dd.DeliveryPlanStatusConfirmed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tx := db.WithContext(ctx).Begin()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(tx.Error)
			defer func() { must(tx.Rollback().Error) }()
			repo := repodelivery.New(tx)
			f.service.repository = repo
			defer func() { f.service.repository = repodelivery.New(db) }()
			f.service.SetExecutionPrincipalReader(fixtureExecutionPrincipalReader{})
			p := deliveryActionPrincipal()
			planID := uuid.NewString()
			snapshots := []sohaapi.HelmDeliverySnapshot{
				{DeliveryPlanID: planID, TargetID: "first", ApplicationID: appID, ApplicationEnvironmentID: sourceID},
				{DeliveryPlanID: planID, TargetID: "second", ApplicationID: appID, ApplicationEnvironmentID: sourceID},
			}
			input := dd.DeliveryPlanInput{ID: planID, ApplicationID: appID, ApplicationEnvironmentID: sourceID,
				Action: dd.ApplicationDeliveryActionDeploy, TargetID: "first", TargetIDs: []string{"first", "second"},
				HelmSnapshots: snapshots, RequiresApproval: true, Impact: map[string]any{"approval": []any{map[string]any{"status": "approved"}}}}
			_, err := repo.CreateDeliveryPlan(ctx, input, p.UserID)
			must(err)
			must(tx.Exec(`UPDATE delivery_plans SET status='confirming' WHERE id=?`, planID).Error)
			frozen := dd.ServiceReferencePlanImpact{Version: 1, PlanID: planID, ApplicationID: appID, SourceBindingID: sourceID,
				Revision: strings.Repeat("a", 64), DatabaseRevision: strings.Repeat("b", 64),
				Consumers: []dd.ServiceReferenceRecord{}, SourceRevisions: map[string]string{}}
			raw, err := json.Marshal(frozen)
			must(err)
			must(tx.Exec(`INSERT INTO delivery_plan_service_reference_impacts(plan_id,snapshot) VALUES (?,?::jsonb)`, planID, string(raw)).Error)

			originalTasks := make([]dd.ExecutionTask, 0, len(snapshots))
			for index, status := range []string{scenario.firstTask, scenario.secondTask} {
				if status != "" {
					originalTasks = append(originalTasks, insertLegacyHelmRecoveryTask(t, ctx, tx, repo, snapshots[index], status))
				}
			}
			plan, err := repo.GetDeliveryPlan(ctx, planID)
			must(err)
			result, err := f.service.ConfirmDeliveryPlan(ctx, p, planID)
			assertLegacyHelmConfirmationResponse(t, result, err, scenario.finalPlan, len(snapshots))
			after, err := repo.GetDeliveryPlan(ctx, planID)
			must(err)
			afterEvidence, err := repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, planID)
			must(err)
			if after.Status != scenario.finalPlan || !reflect.DeepEqual(after.Impact, plan.Impact) || !reflect.DeepEqual(afterEvidence, frozen) {
				t.Fatalf("incorrect Plan state or rewritten evidence: status=%s", after.Status)
			}
			var busy bool
			must(tx.Raw(`SELECT service_reference_source_busy(?)`, sourceID).Row().Scan(&busy))
			if busy != (scenario.finalPlan == dd.DeliveryPlanStatusConfirming) {
				t.Fatalf("service-reference source busy=%v after %s", busy, after.Status)
			}
			var taskCount, auditCount int
			must(tx.Raw(`SELECT count(*) FROM execution_tasks WHERE payload::jsonb #>> '{helm,snapshot,deliveryPlanId}' = ?`, planID).Row().Scan(&taskCount))
			must(tx.Raw(`SELECT count(*) FROM operation_logs WHERE operation_type='delivery.plan.legacy_helm.recover' AND target_scope::jsonb->>'planId'=?`, planID).Row().Scan(&auditCount))
			wantAudit := 1
			if scenario.finalPlan == dd.DeliveryPlanStatusConfirming {
				wantAudit = 0
			}
			if taskCount != len(originalTasks) || auditCount != wantAudit {
				t.Fatalf("unexpected tasks or audit entries: tasks=%d audit=%d", taskCount, auditCount)
			}
			for _, original := range originalTasks {
				task, err := repo.GetExecutionTask(ctx, original.ID)
				must(err)
				if !reflect.DeepEqual(task, original) {
					t.Fatal("recovery rewrote a task receipt")
				}
				if err := f.service.ValidateExecutionTaskRetry(ctx, task); !errors.Is(err, apperrors.ErrConflict) {
					t.Fatalf("legacy task became retryable: %v", err)
				}
			}
		})
	}
}

func assertLegacyHelmConfirmationResponse(t *testing.T, result dd.DeliveryPlanConfirmResult, err error, finalPlan string, expectedTasks int) {
	t.Helper()
	if finalPlan == dd.DeliveryPlanStatusConfirmed {
		if err != nil || len(result.Result.RelatedIDs.ExecutionTaskIDs) != expectedTasks {
			t.Fatalf("complete Helm receipts were not returned: %+v %v", result, err)
		}
	} else if !errors.Is(err, apperrors.ErrConflict) || finalPlan == dd.DeliveryPlanStatusDraft && !strings.Contains(err.Error(), "new Plan preflight") {
		t.Fatalf("unexpected legacy confirmation response: %v", err)
	}
}

func insertLegacyHelmRecoveryTask(t *testing.T, ctx context.Context, tx *gorm.DB, repo *repodelivery.Repository, snapshot sohaapi.HelmDeliverySnapshot, status string) dd.ExecutionTask {
	t.Helper()
	id := helmTaskID(ctx, snapshot, sohaapi.Apply)
	payload, err := json.Marshal(map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: snapshot}})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := json.Marshal(deliveryActionPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	var finished *time.Time
	attempts := 0
	receipt := `{}`
	switch status {
	case "ambiguous", "failed":
		now := time.Now().UTC()
		finished, attempts = &now, 1
		if status == "failed" {
			receipt = `{"helm":{"stopped":true}}`
		}
		status = "failed"
	case "canceled":
		now := time.Now().UTC()
		finished = &now
	case "running":
		attempts = 1
	}
	if err := tx.Exec(`
		INSERT INTO execution_tasks (id,application_id,application_environment_id,task_kind,provider_kind,target_kind,status,attempt_count,payload,result,finished_at,secret_principal)
		VALUES (?,?,?,'helm_apply','helm_direct','helm_release',?,?,?,?,?,?)`,
		id, snapshot.ApplicationID, snapshot.ApplicationEnvironmentID, status, attempts, string(payload), receipt, finished, string(principal)).Error; err != nil {
		t.Fatal(err)
	}
	task, err := repo.GetExecutionTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}
