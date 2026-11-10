package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func TestWorkflowRequestReceiptsWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("requires this run's disposable PostgreSQL")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 16, MaxIdleConns: 8})
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	repo := New(store.DB())
	actor := "request-test:" + uuid.NewString()
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM workflow_request_receipts WHERE actor_id IN (?,?)`, actor, actor+":other").Error
	})
	now := time.Now().UTC()
	original := domainworkflow.TriggerRequest{ID: uuid.NewString(), ActorID: actor, Key: "original-request-1", InputDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ApplicationID: "request-app", RunID: "workflow:" + uuid.NewString(), CreatedAt: now, UpdatedAt: now}
	t.Run("concurrent_original_claim", func(t *testing.T) { workflowRequestReceiptsWithPostgresPhase1(t, &ctx, &repo, &original) })
	t.Run("changed_input_conflicts", func(t *testing.T) { workflowRequestReceiptsWithPostgresPhase2(t, &ctx, &repo, &original) })
	t.Run("restart_preserves_unknown_and_original_id", func(t *testing.T) { workflowRequestReceiptsWithPostgresPhase3(t, &store, &ctx, &original) })
	t.Run("completion_is_immutable_and_idempotent", func(t *testing.T) { workflowRequestReceiptsWithPostgresPhase4(t, &ctx, &repo, &original) })
	t.Run("actor_isolation", func(t *testing.T) { workflowRequestReceiptsWithPostgresPhase5(t, &ctx, &repo, &original) })
	t.Run("missing_run_keeps_tombstone", func(t *testing.T) { workflowRequestReceiptsWithPostgresPhase6(t, &store, &ctx, &repo, &original) })
}

func workflowRequestReceiptsWithPostgresPhase1(t *testing.T, sharedCtx *context.Context, sharedRepo **Repository, sharedOriginal *domainworkflow.TriggerRequest) {

	var wg sync.WaitGroup
	created := make(chan bool, 16)
	failures := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, newClaim, err := (*sharedRepo).ClaimTriggerRequest((*sharedCtx), (*sharedOriginal))
			if err == nil && (item.ID != (*sharedOriginal).ID || item.RunID != (*sharedOriginal).RunID || item.Status != "unknown") {
				err = errors.New("original claim changed")
			}
			created <- newClaim
			failures <- err
		}()
	}
	wg.Wait()
	close(created)
	close(failures)
	count := 0
	for value := range created {
		if value {
			count++
		}
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatal("claim fence admitted more than one submitter", count)
	}
}

func workflowRequestReceiptsWithPostgresPhase2(t *testing.T, sharedCtx *context.Context, sharedRepo **Repository, sharedOriginal *domainworkflow.TriggerRequest) {

	changed := (*sharedOriginal)
	changed.InputDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, _, err := (*sharedRepo).ClaimTriggerRequest((*sharedCtx), changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("changed input reused receipt", err)
	}
}

func workflowRequestReceiptsWithPostgresPhase3(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedOriginal *domainworkflow.TriggerRequest) {

	item, created, err := New((*sharedStore).DB()).ClaimTriggerRequest((*sharedCtx), (*sharedOriginal))
	if err != nil || created || item.Status != "unknown" || item.RunID != (*sharedOriginal).RunID {
		t.Fatal("restart reopened submission", err)
	}
}

func workflowRequestReceiptsWithPostgresPhase4(t *testing.T, sharedCtx *context.Context, sharedRepo **Repository, sharedOriginal *domainworkflow.TriggerRequest) {

	if err := (*sharedRepo).FinishTriggerRequest((*sharedCtx), (*sharedOriginal).ID, "accepted", ""); err != nil {
		t.Fatal(err)
	}
	if err := (*sharedRepo).FinishTriggerRequest((*sharedCtx), (*sharedOriginal).ID, "accepted", ""); err != nil {
		t.Fatal(err)
	}
	if err := (*sharedRepo).FinishTriggerRequest((*sharedCtx), (*sharedOriginal).ID, "rejected", "invalid_argument"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("completed receipt changed", err)
	}
}

func workflowRequestReceiptsWithPostgresPhase5(t *testing.T, sharedCtx *context.Context, sharedRepo **Repository, sharedOriginal *domainworkflow.TriggerRequest) {

	other := (*sharedOriginal)
	other.ActorID += ":other"
	other.ID = uuid.NewString()
	other.RunID = "workflow:" + uuid.NewString()
	item, created, err := (*sharedRepo).ClaimTriggerRequest((*sharedCtx), other)
	if err != nil || !created || item.ID == (*sharedOriginal).ID {
		t.Fatal("actor namespaces mixed", err)
	}
	if _, err := (*sharedRepo).GetTriggerRequest((*sharedCtx), "unknown-actor", (*sharedOriginal).Key); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("unknown actor read receipt", err)
	}
}

func workflowRequestReceiptsWithPostgresPhase6(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedRepo **Repository, sharedOriginal *domainworkflow.TriggerRequest) {

	if err := (*sharedStore).DB().Exec(`DELETE FROM workflow_runs WHERE id=?`, (*sharedOriginal).RunID).Error; err != nil {
		t.Fatal(err)
	}
	item, created, err := (*sharedRepo).ClaimTriggerRequest((*sharedCtx), (*sharedOriginal))
	if err != nil || created || item.Status != "accepted" || item.ID != (*sharedOriginal).ID {
		t.Fatal("missing run reopened original request", err)
	}
}
