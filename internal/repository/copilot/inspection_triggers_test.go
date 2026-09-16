package copilot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"go.uber.org/zap"
)

func TestInspectionTriggerPersistenceWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	dbConfig := config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4}
	admin, err := dbstore.New(dbConfig, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	dbConfig.Name = "inspection_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.DB().Exec(`CREATE DATABASE "` + dbConfig.Name + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.DB().Exec(`DROP DATABASE "` + dbConfig.Name + `" WITH (FORCE)`).Error; err != nil {
			t.Error(err)
		}
	})
	store, err := dbstore.New(dbConfig, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	migrations := filepath.Join("..", "..", "..", "migrations", "postgres")
	if err := store.MigrateFromFile(ctx, migrations); err != nil {
		t.Fatal(err)
	}
	repo := New(store.DB())
	actor := "inspection-test-" + uuid.NewString()
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM workflow_runs WHERE metadata->>'capabilityActorId' = ?`, actor).Error
		_ = store.DB().Exec(`DELETE FROM ai_inspection_tasks WHERE created_by = ?`, actor).Error
	})
	registration := func(kind string) domain.InspectionTask {
		now := time.Now().UTC().Add(-time.Second)
		task := domain.InspectionTask{ID: uuid.NewString(), Title: "Registered goal", ScopeType: "platform", Enabled: true, IntervalMinutes: 1, CreatedBy: actor, CreatedAt: now, UpdatedAt: now, InspectionCapability: domain.InspectionCapability{CapabilityPlan: &sohaapi.CapabilityPlan{Goal: "Check", Steps: []sohaapi.CapabilityPlanStep{}, VerificationSteps: []string{}}, Trigger: &sohaapi.WorkbenchInspectionTrigger{Kind: sohaapi.WorkbenchInspectionTriggerKind(kind)}}}
		if kind == "alert" {
			task.Trigger.AlertRuleID = uuid.NewString()
			task.Trigger.MaxEventAgeSeconds = 60
		}
		saved, err := repo.CreateInspectionTask(ctx, task)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	task := registration("schedule")
	if _, err := repo.CreateInspectionTask(ctx, task); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate registration ID must be a recoverable conflict: %v", err)
	}
	runInspectionConcurrently(t, func() error { return repo.QueueDueInspectionRuns(ctx, time.Now().UTC(), 20) })
	runs, err := repo.ListInspectionRuns(ctx, actor, domain.InspectionRunFilter{TaskID: task.ID})
	if err != nil || len(runs) != 1 {
		t.Fatalf("due slots not atomic: %v %v", runs, err)
	}
	runID := runs[0].ID
	if err := repo.DeleteInspectionTask(ctx, actor, task.ID); err == nil {
		t.Fatal("deleted capability history")
	}
	verifyInspectionScheduleHandoff(t, ctx, store, repo, actor, task, runID, registration)

	verifyInspectionRegistrationFence(t, ctx, repo, actor, registration("schedule"))

	verifyInspectionAlertOccurrences(t, ctx, store, repo, actor, registration("alert"))
}

func runInspectionConcurrently(t *testing.T, apply func() error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- apply() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func verifyInspectionScheduleHandoff(t *testing.T, ctx context.Context, store *dbstore.Store, repo *Repository, actor string, task domain.InspectionTask, runID string, registration func(string) domain.InspectionTask) {
	t.Helper()
	workflows := repoworkflow.New(store.DB())
	createWorkflow := func(ctx context.Context, run domain.InspectionRun) (string, error) {
		intent := domainworkflow.CapabilityIntent{ActorID: actor, Digest: run.ID, PlanVersion: 1, Deadline: time.Now().Add(time.Hour), Input: domainai.CapabilityTaskInput{IdempotencyKey: run.ID}}
		result, err := workflows.CreateCapabilityRun(ctx, domainworkflow.Run{ID: uuid.NewString(), Scope: domainworkflow.ScopeCapabilityTask, WorkflowName: "inspection", Status: "queued", Metadata: map[string]any{"capabilityIntent": intent}, Steps: []domainworkflow.Step{}})
		return result.ID, err
	}
	countWorkflows := func() int64 {
		var count int64
		if err := store.DB().Raw(`SELECT COUNT(*) FROM workflow_runs WHERE metadata->>'capabilityActorId' = ?`, actor).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	temporary := errors.New("visibility transport interrupted")
	_, err := repo.WithInspectionRun(ctx, runID, func(ctx context.Context, _ domain.InspectionTask, run domain.InspectionRun) (domain.InspectionRun, error) {
		if _, err := createWorkflow(ctx, run); err != nil {
			return run, err
		}
		return run, temporary
	})
	if !errors.Is(err, temporary) || countWorkflows() != 0 {
		t.Fatalf("handoff escaped rollback: %v", err)
	}
	var calls atomic.Int64
	runInspectionConcurrently(t, func() error {
		_, err := repo.WithInspectionRun(ctx, runID, func(ctx context.Context, _ domain.InspectionTask, run domain.InspectionRun) (domain.InspectionRun, error) {
			calls.Add(1)
			id, err := createWorkflow(ctx, run)
			run.Status = "handed_off"
			run.Report["capabilityTaskId"] = id
			return run, err
		})
		return err
	})
	if calls.Load() != 1 || countWorkflows() != 1 {
		t.Fatalf("duplicate handoff: %d/%d", calls.Load(), countWorkflows())
	}
	if err := repo.QueueDueInspectionRuns(ctx, time.Now().Add(time.Hour), 20); err != nil {
		t.Fatal(err)
	}
	runs, _ := repo.ListInspectionRuns(ctx, actor, domain.InspectionRunFilter{TaskID: task.ID})
	if len(runs) != 1 {
		t.Fatal("active goal did not suppress next schedule")
	}
	if err := store.DB().Exec(`UPDATE workflow_runs SET status='blocked' WHERE metadata->>'capabilityActorId'=?`, actor).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.QueueDueInspectionRuns(ctx, time.Now().Add(time.Hour), 20); err != nil {
		t.Fatal(err)
	}
	runs, _ = repo.ListInspectionRuns(ctx, actor, domain.InspectionRunFilter{TaskID: task.ID})
	if len(runs) != 1 {
		t.Fatal("blocked goal with a potentially unknown effect allowed another occurrence")
	}

	verifyDeniedInspectionHandoff(t, ctx, repo, registration("schedule"), createWorkflow, countWorkflows)
}

func verifyDeniedInspectionHandoff(t *testing.T, ctx context.Context, repo *Repository, denied domain.InspectionTask, createWorkflow func(context.Context, domain.InspectionRun) (string, error), countWorkflows func() int64) {
	t.Helper()
	actor := denied.CreatedBy
	// A denial after creating Workflow also rolls it back before storing blocked.
	queued, err := repo.QueueManualInspectionRun(ctx, actor, denied.ID, "manual", denied.Revision)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := repo.WithInspectionRun(ctx, queued.ID, func(ctx context.Context, _ domain.InspectionTask, run domain.InspectionRun) (domain.InspectionRun, error) {
		if _, err := createWorkflow(ctx, run); err != nil {
			return run, err
		}
		return run, apperrors.ErrAccessDenied
	})
	if err != nil || blocked.Status != "blocked" || countWorkflows() != 1 {
		t.Fatalf("denied handoff persisted effects: %+v %v", blocked, err)
	}
	replay, err := repo.QueueManualInspectionRun(ctx, actor, denied.ID, "manual", denied.Revision)
	if err != nil || replay.ID != queued.ID {
		t.Fatalf("manual replay lost original receipt: %+v %v", replay, err)
	}
	if _, err := repo.QueueManualInspectionRun(ctx, actor, denied.ID, "manual", denied.Revision+1); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("revision mismatch accepted")
	}

}

func verifyInspectionRegistrationFence(t *testing.T, ctx context.Context, repo *Repository, actor string, paused domain.InspectionTask) {
	t.Helper()
	// A registration change fences receipts captured before that change.
	queued, err := repo.QueueManualInspectionRun(ctx, actor, paused.ID, "pause", paused.Revision)
	if err != nil {
		t.Fatal(err)
	}
	input := domain.InspectionTaskInput{Title: paused.Title, ScopeType: "platform", InspectionCapability: paused.InspectionCapability, ExpectedRevision: paused.Revision, IntervalMinutes: 1, Enabled: false}
	if _, err := repo.UpdateInspectionTask(ctx, actor, paused.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateInspectionTask(ctx, actor, paused.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale revision update: %v", err)
	}
	blocked, err := repo.WithInspectionRun(ctx, queued.ID, func(context.Context, domain.InspectionTask, domain.InspectionRun) (domain.InspectionRun, error) {
		t.Error("changed registration dispatched")
		return domain.InspectionRun{}, nil
	})
	if err != nil || blocked.Status != "blocked" {
		t.Fatalf("registration fence: %+v %v", blocked, err)
	}

}

func verifyInspectionAlertOccurrences(t *testing.T, ctx context.Context, store *dbstore.Store, repo *Repository, actor string, alertTask domain.InspectionTask) {
	t.Helper()
	temporary := errors.New("event transaction interrupted")
	eventID := uuid.NewString()
	t.Cleanup(func() { _ = store.DB().Exec(`DELETE FROM alert_events WHERE id=?`, eventID).Error })
	fire := func(ctx context.Context) error {
		return dbtx.DB(ctx, store.DB()).Exec(`INSERT INTO alert_events(id,rule_id,source_type,fingerprint,title,summary,severity,status,current_state,starts_at) VALUES(?,?,'internal_rule',?,'test','test','warning','firing','firing',NOW())`, eventID, alertTask.Trigger.AlertRuleID, eventID).Error
	}
	err := dbtx.Within(ctx, store.DB(), func(ctx context.Context) error {
		if err := fire(ctx); err != nil {
			return err
		}
		return temporary
	})
	if !errors.Is(err, temporary) {
		t.Fatal(err)
	}
	runs, _ := repo.ListInspectionRuns(ctx, actor, domain.InspectionRunFilter{TaskID: alertTask.ID})
	if len(runs) != 0 {
		t.Fatal("alert receipt survived rolled-back event")
	}
	if err := fire(ctx); err != nil {
		t.Fatal(err)
	}
	// Duplicate callbacks / acknowledge do not open another occurrence.
	for range 3 {
		if err := store.DB().Exec(`UPDATE alert_events SET summary='updated' WHERE id=?`, eventID).Error; err != nil {
			t.Fatal(err)
		}
	}
	runs, _ = repo.ListInspectionRuns(ctx, actor, domain.InspectionRunFilter{TaskID: alertTask.ID})
	if len(runs) != 1 {
		t.Fatalf("alert receipt count: %d", len(runs))
	}
	first := runs[0]
	if err := store.DB().Exec(`UPDATE alert_events SET status='resolved' WHERE id=?`, eventID).Error; err != nil {
		t.Fatal(err)
	}
	blocked, err := repo.WithInspectionRun(ctx, first.ID, func(context.Context, domain.InspectionTask, domain.InspectionRun) (domain.InspectionRun, error) {
		t.Error("resolved alert dispatched")
		return domain.InspectionRun{}, nil
	})
	if err != nil || blocked.Status != "blocked" {
		t.Fatalf("resolved occurrence fence: %+v %v", blocked, err)
	}
	verifyInspectionAlertReopen(t, ctx, store, repo, actor, alertTask, eventID, first)
}

func verifyInspectionAlertReopen(t *testing.T, ctx context.Context, store *dbstore.Store, repo *Repository, actor string, alertTask domain.InspectionTask, eventID string, first domain.InspectionRun) {
	t.Helper()
	// Cooldown gates a reopen even when its original starts_at is unchanged.
	if err := store.DB().Exec(`UPDATE alert_events SET status='firing' WHERE id=?`, eventID).Error; err != nil {
		t.Fatal(err)
	}
	runs, _ := repo.ListInspectionRuns(ctx, actor, domain.InspectionRunFilter{TaskID: alertTask.ID})
	if len(runs) != 1 {
		t.Fatal("cooldown bypassed")
	}
	_ = store.DB().Exec(`UPDATE ai_inspection_tasks SET last_run_at=NOW()-INTERVAL '2 minutes' WHERE id=?`, alertTask.ID).Error
	_ = store.DB().Exec(`UPDATE alert_events SET status='resolved' WHERE id=?`, eventID).Error
	if err := store.DB().Exec(`UPDATE alert_events SET status='firing' WHERE id=?`, eventID).Error; err != nil {
		t.Fatal(err)
	}
	runs, _ = repo.ListInspectionRuns(ctx, actor, domain.InspectionRunFilter{TaskID: alertTask.ID})
	if len(runs) != 2 || fmt.Sprint(runs[0].Report["eventOccurrence"]) == fmt.Sprint(first.Report["eventOccurrence"]) {
		t.Fatalf("reopen identity missing: %+v", runs)
	}
	// Disabling stops pending occurrences without canceling existing Workflow goals.
	_ = store.DB().Exec(`UPDATE ai_inspection_tasks SET enabled=false WHERE id=?`, alertTask.ID).Error
	blocked, err := repo.WithInspectionRun(ctx, runs[0].ID, func(context.Context, domain.InspectionTask, domain.InspectionRun) (domain.InspectionRun, error) {
		t.Error("disabled alert dispatched")
		return domain.InspectionRun{}, nil
	})
	if err != nil || blocked.Status != "blocked" {
		t.Fatalf("disable gate: %+v %v", blocked, err)
	}
}
