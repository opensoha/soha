package eventstream

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryCreateUsesProvidedOccurredAtAndIdempotentInsert(t *testing.T) {
	repo, mock := newEventStreamRepository(t)
	occurredAt := time.Date(2026, 6, 11, 8, 0, 0, 0, time.UTC)

	mock.ExpectExec(`INSERT INTO event_stream .* ON CONFLICT \(id\) DO NOTHING`).
		WithArgs("event-1", "connector.feishu", "connector", "info", "cluster-a", "default", "summary", `{"k":"v"}`, "event-1", occurredAt, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Create(context.Background(), domainevent.Envelope{
		ID:         "event-1",
		Source:     "connector.feishu",
		Category:   "connector",
		Severity:   "info",
		ClusterID:  "cluster-a",
		Namespace:  "default",
		Summary:    "summary",
		Payload:    map[string]any{"k": "v"},
		OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestRepositoryListAndGetScanOccurredAt(t *testing.T) {
	repo, mock := newEventStreamRepository(t)
	occurredAt := time.Date(2026, 6, 11, 8, 0, 0, 0, time.UTC)

	listRows := sqlmock.NewRows([]string{"id", "source", "category", "severity", "cluster_id", "namespace", "summary", "payload", "occurred_at"}).
		AddRow("event-1", "connector.feishu", "connector", "info", "cluster-a", "default", "summary", []byte(`{"k":"v"}`), occurredAt)
	mock.ExpectQuery("SELECT id, source, category, severity, cluster_id, namespace, summary, payload, occurred_at").
		WithArgs(1).
		WillReturnRows(listRows)

	items, err := repo.List(context.Background(), 1)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 1 || !items[0].OccurredAt.Equal(occurredAt) {
		t.Fatalf("List items = %#v, want occurredAt %v", items, occurredAt)
	}

	getRows := sqlmock.NewRows([]string{"id", "source", "category", "severity", "cluster_id", "namespace", "summary", "payload", "occurred_at"}).
		AddRow("event-1", "connector.feishu", "connector", "info", "cluster-a", "default", "summary", []byte(`{"k":"v"}`), occurredAt)
	mock.ExpectQuery("SELECT id, source, category, severity, cluster_id, namespace, summary, payload, occurred_at").
		WithArgs("event-1").
		WillReturnRows(getRows)

	item, err := repo.Get(context.Background(), "event-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !item.OccurredAt.Equal(occurredAt) || item.Payload["k"] != "v" {
		t.Fatalf("Get item = %#v, want occurredAt %v and payload", item, occurredAt)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func newEventStreamRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm postgres mock: %v", err)
	}
	return New(db), mock
}
