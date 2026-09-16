package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

func TestDeliveryBatchPersistenceWithPostgres(t *testing.T) {
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
	repo := New(store.DB())
	definition := domainworkflow.DeliveryWorkflowDefinition{Name: "release", Targets: []domainworkflow.DeliveryTargetInput{{ID: "web", ApplicationID: "app", ServiceID: "web", Action: "build"}}}
	saved, err := repo.SaveDeliveryWorkflow(ctx, domainworkflow.DeliveryWorkflow{ID: uuid.NewString(), Definition: definition, CreatedBy: "delivery-test"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DB().Exec(`DELETE FROM delivery_workflows WHERE id = ?`, saved.ID).Error })
	if saved.Version != 1 || saved.Definition.Name != "release" {
		t.Fatalf("save lost definition: %+v", saved)
	}
	saved.Definition.Name = "updated"
	updated, err := repo.SaveDeliveryWorkflow(ctx, saved, saved.Version)
	if err != nil || updated.Version != 2 || updated.Definition.Name != "updated" {
		t.Fatalf("conditional update: %+v %v", updated, err)
	}
	if _, err := repo.SaveDeliveryWorkflow(ctx, saved, 1); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale workflow update: %v", err)
	}

	verifyDeliveryWorkflowCreation(t, ctx, repo, definition)
	verifyDeliveryBatchHistory(t, ctx, repo, definition)
}

func verifyDeliveryBatchHistory(t *testing.T, ctx context.Context, repo *Repository, definition domainworkflow.DeliveryWorkflowDefinition) {
	t.Helper()
	now := time.Now().UTC()
	batch := domainworkflow.DeliveryBatch{ID: uuid.NewString(), RootRunID: "workflow:" + uuid.NewString(), Definition: definition, CreatedBy: "delivery-test", CreatedAt: now, UpdatedAt: now, Targets: []domainworkflow.DeliveryTargetSnapshot{{Target: definition.Targets[0]}}}
	run := domainworkflow.Run{GatewayAuthorization: "private-gateway-proof", ID: batch.RootRunID, Scope: domainworkflow.ScopeDeliveryBatch, DeliveryBatchID: batch.ID, WorkflowName: definition.Name, Status: "queued", Steps: []domainworkflow.Step{}, NodeRuns: []domainworkflow.NodeRun{{NodeID: "web:build", TargetID: "web", Stage: "build", Status: "pending"}}, CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)}
	key, digest := uuid.NewString(), "sha256:"+strings.Repeat("a", 64)
	created, createdRun, err := repo.CreateDeliveryBatch(ctx, batch, run, key, digest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = repo.db.Exec(`DELETE FROM delivery_batches WHERE id = ?`, batch.ID).Error
		_ = repo.db.Exec(`DELETE FROM workflow_runs WHERE id = ?`, run.ID).Error
	})
	if created.ID != batch.ID || createdRun.ApplicationID != "" || createdRun.Scope != domainworkflow.ScopeDeliveryBatch {
		t.Fatalf("batch scope: %+v", createdRun)
	}
	duplicate, duplicateRun, err := repo.CreateDeliveryBatch(ctx, batch, run, key, digest)
	if err != nil || duplicate.ID != batch.ID || duplicateRun.ID != run.ID {
		t.Fatalf("idempotent creation: %+v %v", duplicate, err)
	}
	raw, _ := json.Marshal(duplicateRun)
	if duplicateRun.GatewayAuthorization != run.GatewayAuthorization || strings.Contains(string(raw), run.GatewayAuthorization) {
		t.Fatal("private gateway evidence lost or exposed on restart")
	}
	if _, _, err := repo.CreateDeliveryBatch(ctx, batch, run, key, "sha256:"+strings.Repeat("b", 64)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("reused key accepted changed content: %v", err)
	}
	ids, err := repo.ListDeliveryBatchIDs(ctx, "app", "web", "", 10)
	if err != nil || len(ids) != 1 || ids[0] != batch.ID {
		t.Fatalf("target history: %v %v", ids, err)
	}
	legacy, err := repo.List(ctx, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range legacy {
		if item.ID == run.ID {
			t.Fatal("batch leaked into legacy application list")
		}
	}
	if _, err := repo.Update(ctx, createdRun); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy update bypassed lease: %v", err)
	}
	if err := repo.DeleteByIDs(ctx, []string{run.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.GetDeliveryBatch(ctx, batch.ID); err != nil {
		t.Fatalf("legacy pruning removed batch history: %v", err)
	}
	verifyDeliveryLease(t, ctx, repo, run.ID)
}

func verifyDeliveryLease(t *testing.T, ctx context.Context, repo *Repository, runID string) {
	t.Helper()
	claimed, err := repo.ClaimManagedRun(ctx, "worker-a", time.Minute)
	if err != nil || claimed.ID != runID || claimed.FencingToken != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if _, err := repo.ClaimManagedRun(ctx, "worker-b", time.Minute); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("double claim: %v", err)
	}
	claimed.NodeRuns[0].Status = "waiting_execution"
	claimed.NodeRuns[0].ExecutionTaskID = "task-one"
	claimed.Status = "waiting_execution"
	updated, err := repo.SaveManagedRun(ctx, claimed, false)
	if err != nil || updated.NodeRuns[0].TargetID != "web" || updated.NodeRuns[0].ExecutionTaskID != "task-one" {
		t.Fatalf("durable node state: %+v %v", updated, err)
	}
	if _, err := repo.SaveManagedRun(ctx, claimed, false); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale state update: %v", err)
	}
	if err := repo.db.Exec(`UPDATE workflow_runs SET lease_until = NOW() - INTERVAL '1 second' WHERE id = ?`, runID).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := repo.ClaimManagedRun(ctx, "worker-b", time.Minute)
	if err != nil || recovered.FencingToken != 2 || recovered.NodeRuns[0].ExecutionTaskID != "task-one" {
		t.Fatalf("restart lost task identity: %+v %v", recovered, err)
	}
	if _, err := repo.SaveManagedRun(ctx, updated, false); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expired worker wrote state: %v", err)
	}
	if recovered.GatewayAuthorization != "private-gateway-proof" {
		t.Fatal("leased updates dropped private gateway evidence")
	}
	verifyDeliveryLeaseStop(t, ctx, repo, runID, recovered)
}

func verifyDeliveryLeaseStop(t *testing.T, ctx context.Context, repo *Repository, runID string, recovered domainworkflow.Run) {
	t.Helper()
	stopping, err := repo.StopManagedRun(ctx, runID, "failure", "build failed")
	if err != nil || stopping.Status != "canceling" || stopping.NodeRuns[0].Status != "waiting_execution" {
		t.Fatalf("stop fabricated executor cancellation: %+v %v", stopping, err)
	}
	if _, err := repo.SaveManagedRun(ctx, recovered, false); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stop failed to invalidate dispatch: %v", err)
	}
	stopping, err = repo.StopManagedRun(ctx, runID, "user", "manual cancel")
	if err != nil || stopping.StopReason != "failure" || stopping.StopSummary != "build failed" {
		t.Fatalf("stop cause was overwritten: %+v %v", stopping, err)
	}
	stopping.Status = "failed"
	stopping.NodeRuns[0].Status = "canceled"
	finished, err := repo.SaveManagedRun(ctx, stopping, true)
	if err != nil || finished.LeaseOwner != "" || finished.LeaseUntil != nil {
		t.Fatalf("lease release: %+v %v", finished, err)
	}
	if _, err := repo.ClaimManagedRun(ctx, "worker-c", time.Minute); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("terminal run restarted: %v", err)
	}
}

func verifyDeliveryWorkflowCreation(t *testing.T, ctx context.Context, repo *Repository, definition domainworkflow.DeliveryWorkflowDefinition) {
	t.Helper()
	key, actor, digest := uuid.NewString(), "receipt-test", "sha256:"+strings.Repeat("a", 64)
	t.Cleanup(func() {
		_ = repo.db.Exec(`DELETE FROM delivery_workflows WHERE created_by = ? AND creation_key = ?`, actor, key).Error
	})
	results := make(chan domainworkflow.DeliveryWorkflow, 8)
	errorsSeen := make(chan error, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			item, err := repo.CreateDeliveryWorkflowIdempotent(ctx, domainworkflow.DeliveryWorkflow{ID: uuid.NewString(), CreatedBy: actor, Definition: definition}, key, digest)
			results <- item
			errorsSeen <- err
		})
	}
	group.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for result := range results {
		if id == "" {
			id = result.ID
		}
		if result.ID != id || result.Version != 1 {
			t.Fatal("concurrent retries created different workflows")
		}
	}
	var count int
	if err := repo.db.Raw(`SELECT COUNT(*) FROM delivery_workflows WHERE created_by = ? AND creation_key = ?`, actor, key).Row().Scan(&count); err != nil || count != 1 {
		t.Fatalf("creation count=%d err=%v", count, err)
	}
	if _, err := repo.FindDeliveryWorkflowCreation(ctx, actor, key, "changed"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("different intent accepted: %v", err)
	}
	if _, err := repo.FindDeliveryWorkflowCreation(ctx, "other-actor", key, digest); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("actor isolation failed: %v", err)
	}
	item, err := repo.GetDeliveryWorkflow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	item.Definition.Name = "edited later"
	if _, err := repo.SaveDeliveryWorkflow(ctx, item, item.Version); err != nil {
		t.Fatal(err)
	}
	receipt, err := repo.CreateDeliveryWorkflowIdempotent(ctx, item, key, digest)
	if err != nil || receipt.Version != 1 || receipt.Definition.Name != definition.Name {
		t.Fatalf("replay used a newer definition: %+v %v", receipt, err)
	}
}
