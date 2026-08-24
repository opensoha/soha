package alert

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestListEventsUsesDurableAscendingCursor(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	cursor := time.Date(2026, 8, 23, 8, 0, 0, 0, time.UTC)
	columns := []string{
		"id", "rule_id", "source_type", "source_system", "fingerprint", "title", "summary", "severity", "status", "cluster_id", "namespace",
		"labels", "annotations", "query_snapshot", "receiver", "generator_url", "current_state", "last_notification_at", "starts_at", "ends_at", "last_seen_at", "created_at", "updated_at",
	}
	mock.ExpectQuery(`(?s)FROM alert_events\s+WHERE cluster_id = \$1 AND \(updated_at > \$2 OR \(updated_at = \$3 AND id > \$4\)\)\s+ORDER BY updated_at ASC, id ASC LIMIT \$5`).
		WithArgs("cluster-a", cursor, cursor, "evt-1", 200).
		WillReturnRows(sqlmock.NewRows(columns))

	items, err := New(db).ListEvents(context.Background(), domainalert.AlertEventFilter{
		ClusterID: "cluster-a", Limit: 200, UpdatedAfter: cursor, AfterID: "evt-1", Ascending: true,
	})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("ListEvents() items = %#v, want empty", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
