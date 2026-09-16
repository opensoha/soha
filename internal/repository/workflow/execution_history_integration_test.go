package workflow

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"go.uber.org/zap"
)

func TestExecutionHistoryPostgresPaginationAndBuildOwnership(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
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
	tx := store.DB().Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	for _, sql := range []string{
		`CREATE TEMP TABLE delivery_batches (id text, root_run_id text, snapshot json, created_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE workflow_runs (id text, scope text, application_id text, metadata json, created_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE build_records (id text, project_id text, metadata json, created_at timestamptz) ON COMMIT DROP`,
		`INSERT INTO build_records SELECT 'standalone-' || n, 'app', '{}', '2026-01-01'::timestamptz FROM generate_series(1,230) n`,
		`INSERT INTO workflow_runs VALUES
   ('legacy','application','app','{"nodeRuns":[{"type":"build","buildRecordId":"legacy-build"}]}','2026-01-01'),
   ('root','delivery_batch','','{"nodeRuns":[{"stage":"build","nodeId":"web:build","buildRecordId":"batch-build"},{"stage":"deploy","buildRecordId":"reused-build"}]}','2026-01-01'),
   ('empty','application','other-app','{"nodeRuns":null}','2026-01-01'),
   ('other','application','other-app','{"nodeRuns":[{"type":"build","buildRecordId":"cross-app-build"}]}','2026-01-01')`,
		`INSERT INTO delivery_batches VALUES ('batch','root','{"workflowId":"definition","targets":[{"target":{"id":"web","applicationId":"app","serviceId":"web"},"buildNodeId":"web:build"}]}','2026-01-01')`,
		`INSERT INTO build_records VALUES
   ('legacy-build','app','{}','2026-01-01'),
   ('batch-build','app','{}','2026-01-01'),
   ('starting-build','app','{"workflowRunId":"root","workflowNodeId":"web:build"}','2026-01-01'),
   ('starting-legacy','app','{"workflowRunId":"legacy"}','2026-01-01'),
   ('reused-build','app','{}','2026-01-01'),
   ('cross-app-build','app','{}','2026-01-01')`,
	} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(tx)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	filter := domainworkflow.ExecutionHistoryFilter{ApplicationID: "app", IncludeWorkflows: true, IncludeBuilds: true}
	seen := collectExecutionHistory(t, ctx, repo, filter)
	if len(seen) != 234 || !seen["batch:batch"] || !seen["application:legacy"] || !seen["build:cross-app-build"] || !seen["build:reused-build"] {
		t.Fatalf("incorrect unified history (%d): %v", len(seen), seen)
	}
	for _, id := range []string{"legacy-build", "batch-build", "starting-build", "starting-legacy"} {
		if seen["build:"+id] {
			t.Fatal("embedded build duplicated", id)
		}
	}
	filter.IncludeWorkflows = false
	rows, err := repo.ListExecutionHistoryCandidates(ctx, filter, nil, 300)
	if err != nil || len(rows) != 236 {
		t.Fatalf("build-only view lost records: %d %v", len(rows), err)
	}
	filter.IncludeWorkflows = true
	filter.WorkflowID = "definition"
	rows, err = repo.ListExecutionHistoryCandidates(ctx, filter, nil, 10)
	if err != nil || len(rows) != 1 || rows[0].ID != "batch" {
		t.Fatalf("definition filter: %+v %v", rows, err)
	}
}

func collectExecutionHistory(t *testing.T, ctx context.Context, repo *Repository, filter domainworkflow.ExecutionHistoryFilter) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	var cursor *domainworkflow.ExecutionHistoryPosition
	for {
		rows, err := repo.ListExecutionHistoryCandidates(ctx, filter, cursor, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			key := row.Kind + ":" + row.ID
			if seen[key] {
				t.Fatal("duplicate cursor result", key)
			}
			seen[key] = true
		}
		cursor = &rows[len(rows)-1]
	}
	return seen
}
