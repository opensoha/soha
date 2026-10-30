package manifest_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	dm "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	mr "github.com/opensoha/soha/internal/repository/manifest"
	"gorm.io/gorm"
)

func publicationOperationFixture(item dm.Package, binding dm.EnvironmentBinding) (dm.OperationRun, dd.ExecutionTask) {
	control := &dm.DeliveryContext{Version: 1, PlanID: uuid.NewString(), TargetID: uuid.NewString(), TargetRevision: strings.Repeat("a", 64), ScopeRevision: strings.Repeat("b", 64)}
	run := dm.OperationRun{ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID, Generation: 1, Action: dm.TaskActionPreflight, IdempotencyKey: uuid.NewString(), CreatedAt: time.Now().UTC(), DeliveryContext: control}
	task := dd.ExecutionTask{ID: uuid.NewString(), ApplicationID: item.ApplicationID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID, TaskKind: "manifest_preflight", ProviderKind: dd.ManifestAdmissionProviderPrefix + "manifest_direct", TargetKind: "manifest_deployment", Status: "queued", Payload: map[string]any{"bindingId": binding.ID}}
	return run, task
}

func verifyPublicationOperationContext(t *testing.T, ctx context.Context, tx *gorm.DB, repository *mr.Repository, item dm.Package, binding dm.EnvironmentBinding) {
	t.Helper()
	t.Run("roundtrip_and_idempotency", func(t *testing.T) {
		verifyPublicationContextRoundtrip(t, ctx, tx, repository, item, binding)
	})
	t.Run("atomic_rollback", func(t *testing.T) {
		verifyPublicationContextRollback(t, ctx, tx, repository, item, binding)
	})
	t.Run("schema_and_task_uniqueness", func(t *testing.T) {
		verifyPublicationContextSchema(t, ctx, tx, repository, item, binding)
	})
	t.Run("legacy_standalone", func(t *testing.T) {
		verifyLegacyPublicationContext(t, ctx, tx, repository, item, binding)
	})
}

func verifyPublicationContextRoundtrip(t *testing.T, ctx context.Context, tx *gorm.DB, repository *mr.Repository, item dm.Package, binding dm.EnvironmentBinding) {
	t.Helper()
	run, task := publicationOperationFixture(item, binding)
	id, created, err := repository.CreateOperationTask(ctx, run, task)
	requireManifestRepositoryNoError(t, err)
	if !created || id != task.ID {
		t.Fatal("operation and task were not atomically created")
	}
	// A newly constructed repository recovers the private context from SQL.
	stored, err := mr.New(tx).GetOperationTask(ctx, id)
	requireManifestRepositoryNoError(t, err)
	if !reflect.DeepEqual(stored.DeliveryContext, run.DeliveryContext) {
		t.Fatal("publication context was not restored")
	}
	data, err := json.Marshal(stored)
	requireManifestRepositoryNoError(t, err)
	if strings.Contains(string(data), run.DeliveryContext.PlanID) {
		t.Fatal("private context leaked into JSON")
	}
	if repeated, created, err := repository.CreateOperationTask(ctx, run, task); err != nil || created || repeated != id {
		t.Fatalf("idempotent recovery: %s %v %v", repeated, created, err)
	}
	changed := *run.DeliveryContext
	changed.ScopeRevision = strings.Repeat("c", 64)
	for _, control := range []*dm.DeliveryContext{&changed, nil} {
		run.DeliveryContext = control
		task.ProviderKind = dd.ManifestAdmissionProviderPrefix + "manifest_direct"
		if control == nil {
			task.ProviderKind = "manifest_direct"
		}
		if _, _, err := repository.CreateOperationTask(ctx, run, task); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("idempotency lost its context: %v", err)
		}
	}
}

func verifyPublicationContextRollback(t *testing.T, ctx context.Context, tx *gorm.DB, repository *mr.Repository, item dm.Package, binding dm.EnvironmentBinding) {
	t.Helper()
	run, task := publicationOperationFixture(item, binding)
	run.PackageID = uuid.NewString() // Task insert succeeds; the operation FK must roll it back.
	if _, _, err := repository.CreateOperationTask(ctx, run, task); err == nil {
		t.Fatal("missing package accepted")
	}
	var count int64
	requireManifestRepositoryNoError(t, tx.Raw(`SELECT count(*) FROM execution_tasks WHERE id=?`, task.ID).Row().Scan(&count))
	if count != 0 {
		t.Fatal("failed operation left a claimable task")
	}
	run, task = publicationOperationFixture(item, binding)
	run.DeliveryContext.Version = 0
	if _, _, err := repository.CreateOperationTask(ctx, run, task); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("invalid context accepted: %v", err)
	}
	requireManifestRepositoryNoError(t, tx.Raw(`SELECT count(*) FROM execution_tasks WHERE id=?`, task.ID).Row().Scan(&count))
	if count != 0 {
		t.Fatal("invalid context touched task storage")
	}
}

func verifyPublicationContextSchema(t *testing.T, ctx context.Context, tx *gorm.DB, repository *mr.Repository, item dm.Package, binding dm.EnvironmentBinding) {
	t.Helper()
	run, task := publicationOperationFixture(item, binding)
	_, _, err := repository.CreateOperationTask(ctx, run, task)
	requireManifestRepositoryNoError(t, err)
	for _, invalid := range []string{`null`, `{}`, `{"version":null}`, `{"version":"1"}`, `{"version":1,"planId":"p","targetId":"t","targetRevision":"bad","scopeRevision":"bad"}`} {
		requireManifestRepositoryNoError(t, tx.SavePoint("invalid_admission").Error)
		err := tx.Exec(`UPDATE manifest_operation_runs SET delivery_context=?::jsonb WHERE id=?`, invalid, run.ID).Error
		requireManifestRepositoryNoError(t, tx.RollbackTo("invalid_admission").Error)
		if err == nil {
			t.Fatalf("schema accepted %s", invalid)
		}
	}
	requireManifestRepositoryNoError(t, tx.SavePoint("duplicate_admission").Error)
	err = tx.Exec(`INSERT INTO manifest_operation_runs (id,package_id,binding_id,generation,action,idempotency_key,execution_task_id,delivery_context) SELECT ?,package_id,binding_id,generation,action,?,execution_task_id,delivery_context FROM manifest_operation_runs WHERE id=?`, uuid.NewString(), uuid.NewString(), run.ID).Error
	requireManifestRepositoryNoError(t, tx.RollbackTo("duplicate_admission").Error)
	if err == nil {
		t.Fatal("duplicate guarded task admission accepted")
	}
	// The existing NOT NULL UNIQUE task constraint also protects legacy rows.
	requireManifestRepositoryNoError(t, tx.SavePoint("legacy_duplicate").Error)
	err = tx.Exec(`INSERT INTO manifest_operation_runs (id,package_id,binding_id,generation,action,idempotency_key,execution_task_id) SELECT ?,package_id,binding_id,generation,action,?,execution_task_id FROM manifest_operation_runs WHERE id=?`, uuid.NewString(), uuid.NewString(), run.ID).Error
	requireManifestRepositoryNoError(t, tx.RollbackTo("legacy_duplicate").Error)
	if err == nil {
		t.Fatal("legacy duplicate task admission accepted")
	}
	stored, err := repository.GetOperationTask(ctx, task.ID)
	requireManifestRepositoryNoError(t, err)
	if !reflect.DeepEqual(stored.DeliveryContext, run.DeliveryContext) {
		t.Fatal("duplicate rejection changed original admission")
	}
}

func verifyLegacyPublicationContext(t *testing.T, ctx context.Context, tx *gorm.DB, repository *mr.Repository, item dm.Package, binding dm.EnvironmentBinding) {
	t.Helper()
	requireManifestRepositoryNoError(t, tx.SavePoint("legacy_admission").Error)
	defer func() { requireManifestRepositoryNoError(t, tx.RollbackTo("legacy_admission").Error) }()
	run, task := publicationOperationFixture(item, binding)
	run.DeliveryContext = nil
	task.ProviderKind = "manifest_direct"
	_, _, err := repository.CreateOperationTask(ctx, run, task)
	requireManifestRepositoryNoError(t, err)
	stored, err := repository.GetOperationTask(ctx, task.ID)
	requireManifestRepositoryNoError(t, err)
	if stored.DeliveryContext != nil {
		t.Fatal("legacy context was fabricated")
	}
	payload := dm.TaskPayload{BindingID: binding.ID, Action: dm.TaskActionApply}
	requireManifestRepositoryNoError(t, repository.ValidateLegacyManifestDelivery(ctx, payload))
	targetID := uuid.NewString()
	requireManifestRepositoryNoError(t, tx.Exec(`INSERT INTO release_targets (id,application_environment_id,cluster_id,namespace,workload_kind,workload_name,target_kind,executor_kind,config_ref,enabled) VALUES (?,?,?,?,'Deployment','api','kubernetes_workload','manifest_ssa',?,FALSE)`, targetID, binding.ApplicationEnvironmentID, binding.ClusterID, binding.Namespace, binding.ID).Error)
	for _, action := range []string{dm.TaskActionApply, dm.TaskActionRepair, dm.TaskActionRollback, dm.TaskActionAdopt, dm.TaskActionRolloutControl} {
		payload.Action = action
		if err := repository.ValidateLegacyManifestDelivery(ctx, payload); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("associated %s escaped Plan: %v", action, err)
		}
	}
	for _, action := range []string{dm.TaskActionPreflight, dm.TaskActionObserve, dm.TaskActionSync} {
		payload.Action = action
		requireManifestRepositoryNoError(t, repository.ValidateLegacyManifestDelivery(ctx, payload))
	}
}
