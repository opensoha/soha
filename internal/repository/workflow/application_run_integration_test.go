package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

func TestApplicationRunContinuationWithPostgres(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	repo := New(store.DB())
	now := time.Now().UTC().Format(time.RFC3339)
	run, err := repo.Create(ctx, domainworkflow.Run{
		ID: "workflow:" + uuid.NewString(), ApplicationID: "test-app", WorkflowName: "continuation", Status: "running",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repo.DeleteByIDs(context.Background(), []string{run.ID}); err != nil {
			t.Error(err)
		}
	})
	run.Status = "waiting_execution"
	paused, err := repo.UpdateApplicationRunIfCurrent(ctx, run, "running", nil)
	if err != nil || paused.Version != run.Version+1 {
		t.Fatalf("persist pause: %+v, %v", paused, err)
	}
	verifyConcurrentApplicationContinuations(t, ctx, store, paused)
	verifyApplicationApprovalTransition(t, ctx, repo, run.ID)
}

func verifyConcurrentApplicationContinuations(t *testing.T, ctx context.Context, store *dbstore.Store, paused domainworkflow.Run) {
	t.Helper()
	status := paused.Status
	paused.Status = "queued"
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		repo := New(store.DB())
		go func() {
			<-start
			_, err := repo.UpdateApplicationRunIfCurrent(ctx, paused, status, nil)
			results <- err
		}()
	}
	close(start)
	var winners, conflicts int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			winners++
		case errors.Is(err, apperrors.ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	stored, err := New(store.DB()).Get(ctx, paused.ID)
	if err != nil || winners != 1 || conflicts != 1 || stored.Status != "queued" || stored.Version != paused.Version+1 {
		t.Fatalf("stored = %+v, winners = %d, conflicts = %d, error = %v", stored, winners, conflicts, err)
	}
}

func verifyApplicationApprovalTransition(t *testing.T, ctx context.Context, repo *Repository, id string) {
	t.Helper()
	run, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = "waiting_approval"
	paused, err := repo.Update(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	approval := domainworkflow.Approval{ID: uuid.NewString(), WorkflowRunID: id, NodeID: "approve", Action: "approved", ActorID: "tester", CreatedAt: time.Now().UTC()}
	updated := paused
	updated.Status = "queued"
	if _, err := repo.UpdateApplicationRunIfCurrent(ctx, updated, paused.Status, &approval); err != nil {
		t.Fatalf("apply approval: %v", err)
	}
	if _, err := repo.UpdateApplicationRunIfCurrent(ctx, updated, paused.Status, &approval); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale approval: %v", err)
	}
	paused, err = repo.Update(ctx, paused)
	if err != nil {
		t.Fatal(err)
	}
	updated.Version = paused.Version
	if _, err := repo.UpdateApplicationRunIfCurrent(ctx, updated, paused.Status, &approval); err == nil {
		t.Fatal("duplicate approval record unexpectedly succeeded")
	}
	stored, err := repo.Get(ctx, id)
	if err != nil || stored.Status != paused.Status || stored.Version != paused.Version {
		t.Fatalf("approval insert failure left a committed transition: %+v, %v", stored, err)
	}
}
