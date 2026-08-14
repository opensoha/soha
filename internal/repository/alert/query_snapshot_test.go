package alert

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAlertQuerySnapshotRoundTrip(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	now := time.Now().UTC()
	snapshot := map[string]any{"version": "v1", "signal": "metrics", "metricKey": "cpu_usage"}
	mock.ExpectExec(`(?s)INSERT INTO alert_rule_runs .*query_snapshot`).
		WithArgs(sqlmock.AnyArg(), "rule-1", "firing", nil, true, 12, nil, `{}`, `{"metricKey":"cpu_usage","signal":"metrics","version":"v1"}`, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	repo := New(db)
	if _, err := repo.CreateRuleRun(context.Background(), domainalert.AlertRuleRunInput{RuleID: "rule-1", Status: "firing", Matched: true, DurationMs: 12, QuerySnapshot: snapshot}); err != nil {
		t.Fatalf("CreateRuleRun() error = %v", err)
	}

	mock.ExpectQuery(`(?s)SELECT id, rule_id, status, summary, matched, duration_ms, error, result, query_snapshot`).
		WithArgs("rule-1", 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "rule_id", "status", "summary", "matched", "duration_ms", "error", "result", "query_snapshot", "created_at", "updated_at"}).
			AddRow("run-1", "rule-1", "firing", nil, true, 12, nil, []byte(`{}`), []byte(`{"version":"v1","signal":"metrics","metricKey":"cpu_usage"}`), now, now))
	runs, err := repo.ListRuleRuns(context.Background(), domainalert.AlertRuleRunFilter{RuleID: "rule-1"})
	if err != nil || len(runs) != 1 || runs[0].QuerySnapshot["metricKey"] != "cpu_usage" {
		t.Fatalf("ListRuleRuns() = %#v, %v", runs, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAlertQuerySnapshotMigration(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0043_observability_alert_query_snapshots.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	migration := string(raw)
	for _, expected := range []string{"ALTER TABLE public.alert_events", "ALTER TABLE public.alert_rule_runs", "query_snapshot jsonb"} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("migration missing %q", expected)
		}
	}
}
