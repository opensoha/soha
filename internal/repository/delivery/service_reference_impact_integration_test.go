package delivery_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	dm "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	mr "github.com/opensoha/soha/internal/repository/manifest"
)

func TestServiceReferenceImpactsPostgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	setup := func(t *testing.T) (referenceCandidatePGFixture, dd.DeliveryPlanInput, dd.ServiceReferencePlanImpact) {
		f := newReferenceCandidatePGFixture(t, ctx, store)
		id := "impact-plan-" + uuid.NewString()
		input := dd.DeliveryPlanInput{ID: id, ApplicationID: f.source.Application.ID, ApplicationEnvironmentID: f.source.ApplicationEnvironmentID, Action: dd.ApplicationDeliveryActionDeploy}
		snapshot := dd.ServiceReferencePlanImpact{Version: 1, PlanID: id, ApplicationID: input.ApplicationID, SourceBindingID: input.ApplicationEnvironmentID, DatabaseRevision: strings.Repeat("a", 64), Revision: strings.Repeat("b", 64), Consumers: []dd.ServiceReferenceRecord{}, SourceRevisions: map[string]string{}}
		t.Cleanup(func() {
			if err := store.DB().Exec(`DELETE FROM delivery_plans WHERE id=?`, id).Error; err != nil {
				t.Error(err)
			}
		})
		return f, input, snapshot
	}
	t.Run("explicit_empty_snapshot_roundtrip", func(t *testing.T) {
		f, input, snapshot := setup(t)
		plan, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(ctx, input, "actor", snapshot)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := f.repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, plan.ID)
		if err != nil || saved.Consumers == nil || saved.SourceRevisions == nil || saved.Revision != snapshot.Revision {
			t.Fatalf("empty evidence lost: %+v %v", saved, err)
		}
		if plan.Impact["consumers"] != nil || plan.Impact["snapshot"] != nil {
			t.Fatal("private evidence exposed")
		}
	})
	t.Run("missing_snapshot_fails_closed", func(t *testing.T) {
		f, input, _ := setup(t)
		plan, err := f.repo.CreateDeliveryPlan(ctx, input, "actor")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, plan.ID); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("missing legacy snapshot was synthesized: %v", err)
		}
	})
	t.Run("atomic_plan_snapshot_rollback", func(t *testing.T) {
		f, input, snapshot := setup(t)
		snapshot.Version = 0
		if _, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(ctx, input, "actor", snapshot); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
		if _, err := f.repo.GetDeliveryPlan(ctx, input.ID); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("partial Plan persisted: %v", err)
		}
	})
	t.Run("persisted_snapshot_not_replaced", func(t *testing.T) {
		f, input, snapshot := setup(t)
		plan, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(ctx, input, "actor", snapshot)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Revision = strings.Repeat("c", 64)
		if _, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(ctx, input, "actor", snapshot); err == nil {
			t.Fatal("duplicate Plan overwritten")
		}
		saved, err := f.repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, plan.ID)
		if err != nil || saved.Revision == snapshot.Revision {
			t.Fatalf("evidence replaced: %v", err)
		}
	})
	t.Run("application_scope_serializes_capture", func(t *testing.T) {
		f, input, snapshot := setup(t)
		locked := make(chan struct{})
		release := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			finished <- f.repo.WithServiceReferenceScope(ctx, input.ApplicationID, func(lockctx context.Context) error {
				close(locked)
				<-release
				_, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(lockctx, input, "actor", snapshot)
				return err
			})
		}()
		select {
		case <-locked:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		contender := make(chan error, 1)
		entered := make(chan struct{})
		go func() {
			contender <- f.repo.WithServiceReferenceScope(ctx, input.ApplicationID, func(context.Context) error { close(entered); return nil })
		}()
		blocked := false
		select {
		case <-entered:
		case <-time.After(100 * time.Millisecond):
			blocked = true
		}
		close(release)
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		if err := <-contender; err != nil {
			t.Fatal(err)
		}
		if !blocked {
			t.Fatal("same app scope did not serialize capture")
		}
		// Nested dbtx scopes are the same durable transaction: rollback removes both rows.
		id := "nested-" + uuid.NewString()
		input.ID = id
		snapshot.PlanID = id
		sentinel := errors.New("rollback outer transaction")
		err := dbtx.Within(ctx, store.DB(), func(txctx context.Context) error {
			_, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(txctx, input, "actor", snapshot)
			if err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
		if _, err := f.repo.GetDeliveryPlan(ctx, id); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal("nested Plan escaped rollback")
		}
	})
	t.Run("plan_delete_cascades_snapshot", func(t *testing.T) {
		f, input, snapshot := setup(t)
		plan, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(ctx, input, "actor", snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.DB().Exec(`DELETE FROM delivery_plans WHERE id=?`, plan.ID).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, plan.ID); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("snapshot survived Plan delete: %v", err)
		}
	})
	t.Run("prepared_preflight_atomic_claim_barrier", func(t *testing.T) {
		f, input, snapshot := setup(t)
		pkg := "prepared-pkg-" + uuid.NewString()
		if err := store.DB().Exec(`INSERT INTO manifest_packages(id,name,application_id) VALUES (?,'Prepared proof',?)`, pkg, input.ApplicationID).Error; err != nil {
			t.Fatal(err)
		}
		taskID := "prepared-task-" + uuid.NewString()
		provider := dd.ManifestAdmissionProviderPrefix + "prepared-" + uuid.NewString()
		t.Cleanup(func() {
			_ = store.DB().Exec(`DELETE FROM execution_tasks WHERE id=?`, taskID).Error
			_ = store.DB().Exec(`DELETE FROM manifest_packages WHERE id=?`, pkg).Error
		})
		contextProof := &dm.DeliveryContext{Version: 1, PlanID: input.ID, TargetID: "target", TargetRevision: strings.Repeat("a", 64), ScopeRevision: strings.Repeat("b", 64)}
		task := dd.ExecutionTask{ID: taskID, ApplicationID: input.ApplicationID, ApplicationEnvironmentID: input.ApplicationEnvironmentID, TaskKind: "manifest_preflight", ProviderKind: provider, Status: "queued", QueueKey: taskID, LockKey: taskID, CallbackToken: uuid.NewString(), Payload: map[string]any{}, Result: map[string]any{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		operation := dm.OperationRun{ID: uuid.NewString(), PackageID: pkg, Generation: 1, Action: "preflight", IdempotencyKey: taskID, ExecutionTaskID: taskID, DeliveryContext: contextProof, CreatedAt: time.Now().UTC()}
		prepared, _ := dm.WithPreparedOperations(ctx)
		dm.StagePreparedOperation(prepared, func(txctx context.Context) error {
			_, _, err := mr.New(store.DB()).CreateOperationTask(txctx, operation, task)
			return err
		})
		written := make(chan struct{})
		release := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			finished <- f.repo.WithServiceReferenceScope(prepared, input.ApplicationID, func(txctx context.Context) error {
				if _, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(txctx, input, "actor", snapshot); err != nil {
					return err
				}
				if err := dm.FlushPreparedOperations(txctx); err != nil {
					return err
				}
				close(written)
				<-release
				return nil
			})
		}()
		select {
		case <-written:
		case err := <-finished:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		_, claimErr := f.repo.ClaimExecutionTask(ctx, []string{provider}, "proof-worker", "local")
		_, readErr := f.repo.GetDeliveryPlan(ctx, input.ID)
		close(release)
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		if !errors.Is(claimErr, apperrors.ErrNotFound) || !errors.Is(readErr, apperrors.ErrNotFound) {
			t.Fatalf("uncommitted task/Plan was visible: claim=%v plan=%v", claimErr, readErr)
		}
		claimed, err := f.repo.ClaimExecutionTask(ctx, []string{provider}, "proof-worker", "local")
		if err != nil || claimed.ID != taskID {
			t.Fatalf("committed proof was not claimable: %v", err)
		}
		if _, err := f.repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, input.ID); err != nil {
			t.Fatal("claimed task lacks durable private evidence", err)
		}
	})
	t.Run("prepared_preflight_rollback_has_no_orphan", func(t *testing.T) {
		f, input, snapshot := setup(t)
		pkg := "rollback-pkg-" + uuid.NewString()
		taskID := "rollback-task-" + uuid.NewString()
		if err := store.DB().Exec(`INSERT INTO manifest_packages(id,name,application_id) VALUES (?,'Rollback proof',?)`, pkg, input.ApplicationID).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = store.DB().Exec(`DELETE FROM execution_tasks WHERE id=?`, taskID).Error
			_ = store.DB().Exec(`DELETE FROM manifest_packages WHERE id=?`, pkg).Error
		})
		prepared, _ := dm.WithPreparedOperations(ctx)
		operation := dm.OperationRun{ID: uuid.NewString(), PackageID: pkg, Generation: 1, Action: "preflight", IdempotencyKey: taskID, ExecutionTaskID: taskID, CreatedAt: time.Now().UTC()}
		task := dd.ExecutionTask{ID: taskID, ApplicationID: input.ApplicationID, ApplicationEnvironmentID: input.ApplicationEnvironmentID, TaskKind: "manifest_preflight", ProviderKind: "proof-rollback", Status: "queued", CallbackToken: uuid.NewString(), Payload: map[string]any{}, Result: map[string]any{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		dm.StagePreparedOperation(prepared, func(txctx context.Context) error {
			_, _, err := mr.New(store.DB()).CreateOperationTask(txctx, operation, task)
			return err
		})
		sentinel := errors.New("final Plan persistence failed")
		err := f.repo.WithServiceReferenceScope(prepared, input.ApplicationID, func(txctx context.Context) error {
			if _, err := f.repo.CreateDeliveryPlanWithServiceReferenceImpact(txctx, input, "actor", snapshot); err != nil {
				return err
			}
			if err := dm.FlushPreparedOperations(txctx); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
		if _, err := f.repo.GetExecutionTask(ctx, taskID); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal("orphan executable preflight persisted", err)
		}
		if _, err := f.repo.GetDeliveryPlan(ctx, input.ID); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal("partial Plan persisted", err)
		}
	})

}
