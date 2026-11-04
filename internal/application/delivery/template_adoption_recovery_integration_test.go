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
	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	dr "github.com/opensoha/soha/internal/domain/resource"
	dw "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestTemplateAdoptionRecoveryWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to this run's isolated PostgreSQL instance")
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
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"known_rollback_releases_source_guard", "ambiguous_failure_preserves_source_guard", "committed_receipt_not_rolled_back"} {
		t.Run(scenario, func(t *testing.T) {
			f, repo, owner, plan := adoptionRecoveryFixture(t)
			suffix := uuid.NewString()
			appID := "recovery-app-" + suffix
			sourceID := "recovery-source-" + suffix
			consumerID := "recovery-consumer-" + suffix
			serviceID := "recovery-service-" + suffix
			targetID := "recovery-target-" + suffix
			clusterID := "recovery-cluster-" + suffix
			sourceEnv := "recovery-source-env-" + suffix
			consumerEnv := "recovery-consumer-env-" + suffix
			referenceID := "recovery-reference-" + suffix
			plan.ID = "recovery-plan-" + suffix
			plan.ApplicationID = appID
			plan.ApplicationEnvironmentID = sourceID
			repo.plan = plan
			owner.frozen.Candidate.Target.ApplicationID = appID
			owner.frozen.Candidate.Target.ApplicationEnvironmentID = sourceID
			apps := referenceApplicationsFixture(t, &f)
			apps.app.ID = appID
			apps.services[0].ApplicationID = appID
			apps.services[0].ID = serviceID
			f.service.applications = apps
			source := f.catalog.bindings["test-b"]
			source.ID = sourceID
			source.ApplicationID = appID
			source.EnvironmentID = sourceEnv
			source.Targets[0].ID = targetID
			source.Targets[0].ApplicationEnvironmentID = sourceID
			source.Targets[0].ServiceID = serviceID
			source.Targets[0].ClusterID = clusterID
			source.Targets[0].Metadata["serviceId"] = serviceID
			observed := f.targets.sources["cluster-b/backend/Deployment/api/"]
			observed.Resource.ClusterID = clusterID
			raw, err := json.Marshal(observed)
			if err != nil {
				t.Fatal(err)
			}
			source.Targets[0].Metadata["resourceSource"] = string(raw)
			consumer := f.catalog.bindings["test-a"]
			consumer.ID = consumerID
			consumer.ApplicationID = appID
			consumer.EnvironmentID = consumerEnv
			consumer.Targets = nil
			f.catalog.bindings = map[string]dc.ApplicationEnvironment{sourceID: source, consumerID: consumer}
			f.catalog.environments[0].ID = consumerEnv
			f.catalog.environments[1].ID = sourceEnv
			f.targets.sources = map[string]dr.ResourceSourceSnapshot{clusterID + "/backend/Deployment/api/": observed}
			preview, err := f.service.observeSourceReferenceGroup(ctx, deliveryActionPrincipal(), sourceID, serviceID)
			if err != nil {
				t.Fatal(err)
			}
			preview.Consumer = referenceScope(consumer, f.catalog.environments[0])
			identity, err := dd.ServiceReferenceIdentityRevision(preview)
			if err != nil {
				t.Fatal(err)
			}
			control, err := dd.ServiceReferenceControlRevision(source, serviceID)
			if err != nil {
				t.Fatal(err)
			}
			record := dd.ServiceReferenceRecord{Reference: dd.ServiceReference{ID: referenceID, Revision: 1, SourceIdentityRevision: identity, Preview: preview}, ApplicationID: appID, ConsumerBindingID: consumerID, ServiceID: serviceID, SourceBindingID: sourceID, SourceTargetIDs: []string{targetID}, ControlRevision: control, ConsumerConfigurationRevision: strings.Repeat("c", 64), ConsumerEnvironment: f.catalog.environments[0]}
			repo.records = []dd.ServiceReferenceRecord{record}
			frozen, err := f.service.captureServiceReferenceImpact(ctx, deliveryActionPrincipal(), plan)
			if err != nil {
				t.Fatal(err)
			}
			repo.snapshot = &frozen
			rawPreview, err := json.Marshal(preview)
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := json.Marshal(source.Targets[0].Metadata)
			if err != nil {
				t.Fatal(err)
			}
			approval, err := json.Marshal(plan.Impact)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = store.DB().Exec(`DELETE FROM delivery_plans WHERE id=?`, plan.ID).Error
				_ = store.DB().Exec(`DELETE FROM environment_service_references WHERE id=?`, referenceID).Error
				_ = store.DB().Exec(`DELETE FROM application_environments WHERE application_id=?`, appID).Error
				_ = store.DB().Exec(`DELETE FROM application_services WHERE application_id=?`, appID).Error
				_ = store.DB().Exec(`DELETE FROM applications WHERE id=?`, appID).Error
				_ = store.DB().Exec(`DELETE FROM delivery_environments WHERE id IN (?,?)`, sourceEnv, consumerEnv).Error
				_ = store.DB().Exec(`DELETE FROM clusters WHERE id=?`, clusterID).Error
			})
			statements := []struct {
				query string
				args  []any
			}{
				{`INSERT INTO applications(id,name,app_key,app_group,language) VALUES (?,'Adoption recovery',?,'test','configuration')`, []any{appID, appID}},
				{`INSERT INTO application_services(id,application_id,service_key,service_name) VALUES (?,?,?,'API')`, []any{serviceID, appID, serviceID}},
				{`INSERT INTO clusters(id,name) VALUES (?,'Recovery source')`, []any{clusterID}},
				{`INSERT INTO delivery_environments(id,environment_key,name,tier,stage_level) VALUES (?,?,'Source','test',20),(?,?,'Consumer','test',20)`, []any{sourceEnv, sourceEnv, consumerEnv, consumerEnv}},
				{`INSERT INTO application_environments(id,application_id,environment_id) VALUES (?,?,?),(?,?,?)`, []any{sourceID, appID, sourceEnv, consumerID, appID, consumerEnv}},
				{`INSERT INTO release_targets(id,application_environment_id,cluster_id,namespace,target_kind,executor_kind,workload_kind,workload_name,metadata,enabled) VALUES (?,?,?,'backend','deployment','','Deployment','api',?::jsonb,false)`, []any{targetID, sourceID, clusterID, string(metadata)}},
				{`INSERT INTO environment_service_references(id,application_id,consumer_binding_id,service_id,source_binding_id,revision,preview,source_identity_revision,control_revision,consumer_configuration_revision,created_at,updated_at) VALUES (?,?,?,?,?,1,?::jsonb,?,?,?,now(),now())`, []any{referenceID, appID, consumerID, serviceID, sourceID, string(rawPreview), identity, control, strings.Repeat("c", 64)}},
				{`INSERT INTO delivery_plans(id,source,status,application_id,application_environment_id,action,requires_approval,impact,template_candidate_id,updated_at) VALUES (?,'delivery_batch','draft',?,?,'deploy',true,?::jsonb,?,?)`, []any{plan.ID, appID, sourceID, string(approval), plan.TemplateCandidateID, plan.UpdatedAt}},
			}
			for _, statement := range statements {
				if err := store.DB().Exec(statement.query, statement.args...).Error; err != nil {
					t.Fatal(err)
				}
			}
			// The production AFTER INSERT trigger owns this index; the fixture must
			// verify it rather than inserting the same primary key a second time.
			var indexed int64
			if err := store.DB().Raw(`SELECT count(*) FROM environment_service_reference_targets WHERE reference_id=? AND target_id=?`, referenceID, targetID).Row().Scan(&indexed); err != nil || indexed != 1 {
				t.Fatalf("active reference target index was not built atomically: count=%d err=%v", indexed, err)
			}
			// These first three cases isolate the recovery helper with SQL CAS and
			// the real source-busy/delete triggers. Production worker/CAS wiring is
			// exercised separately by production_adapter_recovery_fence below.
			repo.persist = func(ctx context.Context, previous, next dd.DeliveryPlan) error {
				result := dbtx.DB(ctx, store.DB()).Exec(`UPDATE delivery_plans SET status=?,confirmed_at=?,updated_at=? WHERE id=? AND status=? AND updated_at=?`, next.Status, next.ConfirmedAt, next.UpdatedAt, next.ID, previous.Status, previous.UpdatedAt)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return apperrors.ErrConflict
				}
				return nil
			}
			repo.read = func(ctx context.Context, current dd.DeliveryPlan) (dd.DeliveryPlan, error) {
				if err := dbtx.DB(ctx, store.DB()).Raw(`SELECT status,confirmed_at,updated_at FROM delivery_plans WHERE id=?`, current.ID).Row().Scan(&current.Status, &current.ConfirmedAt, &current.UpdatedAt); err != nil {
					return current, err
				}
				stamp := current.UpdatedAt
				current.ExpectedUpdatedAt = &stamp
				return current, nil
			}
			busy := func() bool {
				var value bool
				if err := store.DB().Raw(`SELECT service_reference_source_busy(?)`, sourceID).Row().Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			assertDeleteBlocked := func() {
				if !busy() {
					t.Fatal("persisted confirming Plan did not hold the SQL source guard")
				}
				err := store.DB().Exec(`DELETE FROM environment_service_references WHERE id=?`, referenceID).Error
				var sqlState interface{ SQLState() string }
				if !errors.As(err, &sqlState) || sqlState.SQLState() != "23514" {
					t.Fatalf("SQL reference mutation escaped busy guard: %v", err)
				}
			}
			owner.apply = func(ctx context.Context, planID string, receipt func(context.Context) error) error {
				assertDeleteBlocked()
				if scenario == "ambiguous_failure_preserves_source_guard" {
					return errors.New("commit outcome unknown")
				}
				err := dbtx.Within(ctx, store.DB(), func(txctx context.Context) error {
					if err := dbtx.DB(txctx, store.DB()).Exec(`UPDATE application_environments SET workflow_template_version=4 WHERE id=?`, sourceID).Error; err != nil {
						return err
					}
					if err := receipt(txctx); err != nil {
						return err
					}
					if scenario == "known_rollback_releases_source_guard" {
						return apperrors.ErrConflict
					}
					return nil
				})
				if err != nil {
					return err
				}
				owner.frozen.AppliedPlanID = planID
				return errors.New("commit acknowledgement lost")
			}
			result, err := f.service.confirmTemplateAdoptionPlan(ctx, deliveryActionPrincipal(), plan)
			if scenario == "ambiguous_failure_preserves_source_guard" {
				if err == nil || result.Plan.Status != dd.DeliveryPlanStatusConfirming {
					t.Fatalf("ambiguous adoption unlocked source: %v", err)
				}
				assertDeleteBlocked()
				return
			}
			expectedStatus := dd.DeliveryPlanStatusDraft
			expectedVersion := int64(0)
			if scenario == "committed_receipt_not_rolled_back" {
				expectedStatus = dd.DeliveryPlanStatusConfirmed
				expectedVersion = 4
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, apperrors.ErrConflict) {
				t.Fatal(err)
			}
			if result.Plan.Status != expectedStatus || busy() || !deliveryPlanApprovalGranted(result.Plan) {
				t.Fatalf("SQL busy claim or approval not recovered: %v %+v", err, result.Plan)
			}
			var version int64
			if err := store.DB().Raw(`SELECT workflow_template_version FROM application_environments WHERE id=?`, sourceID).Row().Scan(&version); err != nil || version != expectedVersion {
				t.Fatalf("configuration transaction outcome incorrect: version=%d err=%v", version, err)
			}
			deleted := store.DB().Exec(`DELETE FROM environment_service_references WHERE id=?`, referenceID)
			if deleted.Error != nil || deleted.RowsAffected != 1 {
				t.Fatalf("reference detach could not retry after settled adoption: %v", deleted.Error)
			}
			var remaining int64
			if err := store.DB().Raw(`SELECT count(*) FROM release_targets WHERE id=?`, targetID).Row().Scan(&remaining); err != nil || remaining != 1 {
				t.Fatal("detaching after recovery changed the source target", err)
			}
		})
	}
	t.Run("production_adapter_recovery_fence", func(t *testing.T) {
		for _, scenario := range []string{"valid_owned_claim", "canceled_worker", "lost_lease", "newer_claim", "confirmed_receipt"} {
			t.Run(scenario, func(t *testing.T) {
				tx := store.DB().WithContext(ctx).Begin()
				if tx.Error != nil {
					t.Fatal(tx.Error)
				}
				defer func() {
					if err := tx.Rollback().Error; err != nil {
						t.Error(err)
					}
				}()
				verifyProductionTemplateRecoveryFence(t, ctx, tx, scenario)
			})
		}
	})

}

// This adapter delegates both reads and writes to the actual delivery repository.
// Only unrelated private-impact fixture capabilities remain supplied by the stub.
type productionRecoveryPlanRepository struct {
	*impactTestRepository
	plans  *repodelivery.Repository
	writes int
}

func (r *productionRecoveryPlanRepository) GetDeliveryPlan(ctx context.Context, id string) (dd.DeliveryPlan, error) {
	return r.plans.GetDeliveryPlan(ctx, id)
}
func (r *productionRecoveryPlanRepository) UpdateDeliveryPlan(ctx context.Context, plan dd.DeliveryPlan) (dd.DeliveryPlan, error) {
	r.writes++
	return r.plans.UpdateDeliveryPlan(ctx, plan)
}

func verifyProductionTemplateRecoveryFence(t *testing.T, ctx context.Context, db *gorm.DB, scenario string) {
	t.Helper()
	f, stub, owner, seed := adoptionRecoveryFixture(t)
	plans, workflows := repodelivery.New(db), repoworkflow.New(db)
	appID, environmentID, bindingID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO applications(id,name,app_key,app_group,language) VALUES (?,'Recovery fence',?,'test','configuration')`, []any{appID, appID}},
		{`INSERT INTO delivery_environments(id,environment_key,name,tier,stage_level) VALUES (?,?,'Recovery fence','test',20)`, []any{environmentID, environmentID}},
		{`INSERT INTO application_environments(id,application_id,environment_id) VALUES (?,?,?)`, []any{bindingID, appID, environmentID}},
	} {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	target := dw.DeliveryTargetInput{ID: "adopt", ApplicationID: appID, ApplicationEnvironmentID: bindingID, Action: "config_update", TemplateCandidateID: seed.TemplateCandidateID}
	batch := dw.DeliveryBatch{ID: uuid.NewString(), RootRunID: "workflow:" + uuid.NewString(), Definition: dw.DeliveryWorkflowDefinition{Name: "Recovery fence", Targets: []dw.DeliveryTargetInput{target}}, Targets: []dw.DeliveryTargetSnapshot{{Target: target}}, CreatedBy: "template-recovery-fence", CreatedAt: now, UpdatedAt: now}
	run := dw.Run{ID: batch.RootRunID, Scope: dw.ScopeDeliveryBatch, DeliveryBatchID: batch.ID, WorkflowName: "Recovery fence", Status: "running", Steps: []dw.Step{}, NodeRuns: []dw.NodeRun{{NodeID: "adopt:plan", TargetID: "adopt", Stage: "plan", Status: "running"}, {NodeID: "adopt:deploy", TargetID: "adopt", Stage: "deploy", Status: "running"}}, CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)}
	if _, _, err := workflows.CreateDeliveryBatch(ctx, batch, run, uuid.NewString(), "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	run, err := workflows.ClaimManagedRun(ctx, "recovery-worker", time.Minute)
	if err != nil || run.ID != batch.RootRunID {
		t.Fatalf("claim exact recovery fixture run: id=%s err=%v", run.ID, err)
	}
	planCtx := dw.WithNodeExecution(ctx, run, run.NodeRuns[0])
	previous, err := plans.CreateDeliveryPlan(planCtx, dd.DeliveryPlanInput{Source: dw.ScopeDeliveryBatch, ApplicationID: appID, ApplicationEnvironmentID: bindingID, Action: dd.ApplicationDeliveryActionDeploy, TemplateCandidateID: seed.TemplateCandidateID, RequiresApproval: true, Impact: seed.Impact}, "template-recovery-fence")
	if err != nil {
		t.Fatal(err)
	}
	worker := dw.WithNodeExecution(ctx, run, run.NodeRuns[1])
	claimed := previous
	claimed.Status = dd.DeliveryPlanStatusConfirming
	claimed, err = plans.UpdateDeliveryPlan(worker, claimed)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ExpectedUpdatedAt == nil || claimed.UpdatedAt.Equal(previous.UpdatedAt) {
		t.Fatal("production claim did not retain its updated CAS")
	}
	owner.frozen.Candidate.Target.ApplicationID = appID
	owner.frozen.Candidate.Target.ApplicationEnvironmentID = bindingID
	adapter := &productionRecoveryPlanRepository{impactTestRepository: stub.impactTestRepository, plans: plans}
	f.service.repository = adapter
	expected := claimed
	rollback := claimed
	rollback.Status = dd.DeliveryPlanStatusDraft
	switch scenario {
	case "canceled_worker":
		if _, err := workflows.StopManagedRun(ctx, run.ID, "user", "cancel before recovery"); err != nil {
			t.Fatal(err)
		}
	case "lost_lease":
		if err := db.Exec(`UPDATE workflow_runs SET lease_until=NOW()-INTERVAL '1 second' WHERE id=?`, run.ID).Error; err != nil {
			t.Fatal(err)
		}
		recovered, err := workflows.ClaimManagedRun(ctx, "replacement-worker", time.Minute)
		if err != nil || recovered.ID != run.ID || recovered.FencingToken <= run.FencingToken {
			t.Fatalf("replacement worker did not change the production lease fence: %+v %v", recovered, err)
		}
	case "newer_claim":
		expected, err = plans.UpdateDeliveryPlan(worker, claimed)
		if err != nil || expected.UpdatedAt.Equal(claimed.UpdatedAt) {
			t.Fatalf("new production claim was not persisted: %v", err)
		}
	case "confirmed_receipt":
		confirmed := claimed
		confirmed.Status = dd.DeliveryPlanStatusConfirmed
		stamp := time.Now().UTC()
		confirmed.ConfirmedAt = &stamp
		expected, err = plans.UpdateDeliveryPlan(worker, confirmed)
		if err != nil {
			t.Fatal(err)
		}
		owner.frozen.AppliedPlanID = claimed.ID
	}
	if scenario == "canceled_worker" || scenario == "lost_lease" || scenario == "newer_claim" {
		// This is the real updateBatchPlan path, including LockDeliveryNode and its
		// persisted Plan timestamp CAS, not the SQL-CAS helper from the other cases.
		if _, err := plans.UpdateDeliveryPlan(worker, rollback); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("production rollback accepted an invalid worker/CAS: %v", err)
		}
	}
	result, recoveryErr := f.service.recoverFailedTemplateAdoption(worker, deliveryActionPrincipal(), previous, claimed, apperrors.ErrConflict)
	actual, err := plans.GetDeliveryPlan(ctx, claimed.ID)
	if err != nil {
		t.Fatal(err)
	}
	switch scenario {
	case "valid_owned_claim":
		if !errors.Is(recoveryErr, apperrors.ErrConflict) || actual.Status != dd.DeliveryPlanStatusDraft || !actual.UpdatedAt.After(claimed.UpdatedAt) || adapter.writes != 1 {
			t.Fatalf("owned production recovery did not use its valid CAS: %+v err=%v writes=%d", actual, recoveryErr, adapter.writes)
		}
	case "confirmed_receipt":
		if recoveryErr != nil || adapter.writes != 0 || !reflect.DeepEqual(actual, expected) || !reflect.DeepEqual(result, expected) {
			t.Fatalf("production confirmed receipt was rewritten: %v writes=%d", recoveryErr, adapter.writes)
		}
	default:
		if !errors.Is(recoveryErr, apperrors.ErrConflict) || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("production worker fence or newer claim was clobbered: %v", recoveryErr)
		}
		wantWrites := 1
		if scenario == "newer_claim" {
			wantWrites = 0
		}
		if adapter.writes != wantWrites {
			t.Fatalf("recovery did not exercise the expected production fence: writes=%d want=%d", adapter.writes, wantWrites)
		}
	}
	if !deliveryPlanApprovalGranted(actual) || !reflect.DeepEqual(actual.Impact, previous.Impact) {
		t.Fatal("production recovery changed approved/frozen provenance")
	}
	var busy bool
	if err := db.Raw(`SELECT service_reference_source_busy(?)`, bindingID).Row().Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if busy != (actual.Status == dd.DeliveryPlanStatusConfirming) {
		t.Fatal("source SQL busy guard disagrees with production repository outcome")
	}
}
