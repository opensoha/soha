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
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

func TestCapabilityTaskPersistenceWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 8, MaxIdleConns: 4}, zap.NewNop())
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
	intent := domainworkflow.CapabilityIntent{ActorID: "capability-test", Digest: "same-plan", PlanVersion: 1, Deadline: time.Now().Add(time.Hour), Input: domainaigateway.CapabilityTaskInput{IdempotencyKey: uuid.NewString()}}
	run := domainworkflow.Run{ID: uuid.NewString(), Scope: domainworkflow.ScopeCapabilityTask, WorkflowName: "check goal", Status: "queued", Metadata: map[string]any{"capabilityIntent": intent}, NodeRuns: []domainworkflow.NodeRun{{NodeID: "check", Name: "check", Type: "capability", Status: "pending"}}, Steps: []domainworkflow.Step{}}
	t.Cleanup(func() { _ = store.DB().Exec(`DELETE FROM workflow_runs WHERE id = ?`, run.ID).Error })
	createCapabilityRunConcurrently(t, ctx, repo, run, intent)
	changed := intent
	changed.Digest = "different-plan"
	run.Metadata = map[string]any{"capabilityIntent": changed}
	if _, err := repo.CreateCapabilityRun(ctx, run); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed idempotent plan accepted: %v", err)
	}
	frozen := freezeCapabilityRun(t, ctx, store, repo, run.ID)
	stopped, err := repo.StopManagedRun(ctx, run.ID, "user", "stop")
	if err != nil || stopped.Version <= frozen.Version {
		t.Fatalf("stop did not invalidate worker: %+v %v", stopped, err)
	}
	if err := repo.WithCapabilityApproval(ctx, run.ID, false, func(domainworkflow.Run) error {
		t.Error("approval executed after cancellation")
		return nil
	}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stopped approval guard: %v", err)
	}
	if _, err := repo.SaveManagedRun(ctx, frozen, true); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("old worker overwrote cancellation: %v", err)
	}
	stopped.Status = "blocked"
	stopped, err = repo.SaveManagedRun(ctx, stopped, true)
	if err != nil {
		t.Fatal(err)
	}
	reviseCapabilityRunConcurrently(t, ctx, repo, stopped)
	replayed, err := repo.FindCapabilityRun(ctx, intent.ActorID, intent.Input.IdempotencyKey, intent.Digest)
	if err != nil || replayed.Status != "queued" {
		t.Fatalf("initial idempotency replay lost after revision: %+v %v", replayed, err)
	}
	history, err := domainworkflow.CapabilityRevisionsFrom(replayed)
	if err != nil || len(history) != 1 || history[0].Nodes[0].PreparedCall.RequestID != "fixed-request" {
		t.Fatalf("revision history lost: %+v %v", history, err)
	}
}

func createCapabilityRunConcurrently(t *testing.T, ctx context.Context, repo *Repository, run domainworkflow.Run, intent domainworkflow.CapabilityIntent) {
	t.Helper()
	var wg sync.WaitGroup
	errorsFound := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each caller has its own map, as separate HTTP submissions do.
			candidate := run
			candidate.Metadata = map[string]any{"capabilityIntent": intent}
			created, err := repo.CreateCapabilityRun(ctx, candidate)
			if err == nil && created.ID != run.ID {
				err = errors.New("duplicate logical task")
			}
			errorsFound <- err
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func freezeCapabilityRun(t *testing.T, ctx context.Context, store *dbstore.Store, repo *Repository, runID string) domainworkflow.Run {
	t.Helper()
	claimed, err := repo.ClaimManagedRun(ctx, "first-worker", time.Minute)
	if err != nil || claimed.ID != runID {
		t.Fatalf("claim: %s %v", claimed.ID, err)
	}
	if _, err := repo.ClaimManagedRun(ctx, "second-worker", time.Minute); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("live lease stolen: %v", err)
	}
	frozen, err := repo.UpdateCapabilityNode(ctx, claimed, "check", func(_ domainworkflow.Run, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
		node.PreparedCall = &domainaigateway.ToolInvocationRequest{ToolName: "check", CapabilityVersion: "1", RequestID: "fixed-request", Input: map[string]any{"resourceId": "resource"}}
		node.DispatchAttempted, node.Status = true, "waiting_approval"
		return node, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A new repository instance reads only persisted state, including frozen input.
	restored, err := New(store.DB()).Get(ctx, runID)
	if err != nil || restored.NodeRuns[0].PreparedCall == nil || !restored.NodeRuns[0].DispatchAttempted {
		t.Fatalf("frozen dispatch was not durable: %+v %v", restored.NodeRuns, err)
	}
	if _, err := repo.UpdateCapabilityNode(ctx, claimed, "check", func(_ domainworkflow.Run, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
		t.Error("stale worker executed callback")
		return node, nil
	}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale version accepted: %v", err)
	}
	return frozen
}

func reviseCapabilityRunConcurrently(t *testing.T, ctx context.Context, repo *Repository, stopped domainworkflow.Run) {
	t.Helper()
	var wg sync.WaitGroup
	// Only one revision can replace the same reviewed version. Every older
	// dispatcher loses both its CAS version and fencing token.
	revisions := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ReviseCapabilityRun(ctx, stopped.ID, stopped.Version, func(current domainworkflow.Run) (domainworkflow.Run, error) {
				intent, err := domainworkflow.CapabilityIntentFrom(current)
				if err != nil {
					return current, err
				}
				current.Metadata["capabilityRevisions"] = []domainworkflow.CapabilityRevision{{Intent: intent, Nodes: current.NodeRuns, Version: current.Version, Status: current.Status, ArchivedAt: time.Now().UTC().Format(time.RFC3339)}}
				intent.PlanVersion, intent.Digest = 2, "new-plan"
				current.Metadata["capabilityIntent"] = intent
				return current, nil
			})
			revisions <- err
		}()
	}
	wg.Wait()
	close(revisions)
	succeeded := 0
	for err := range revisions {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, apperrors.ErrConflict) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected one atomic revision, got %d", succeeded)
	}
}
