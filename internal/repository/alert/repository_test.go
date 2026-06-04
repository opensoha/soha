package alert

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainalert "github.com/soha/soha/internal/domain/alert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryUpsertWritesAlertEvents(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer sqlDB.Close()

	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	startsAt := time.Date(2026, 6, 4, 8, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(5 * time.Minute)
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT INTO alert_events.*ON CONFLICT \(id\) DO UPDATE SET.*annotations = \(\s*EXCLUDED\.annotations::jsonb.*current_state = CASE`).
		WithArgs(
			"prometheus:cpu-high",
			nil,
			"external_webhook",
			"prometheus",
			"cpu-high",
			"CPUHigh",
			"cpu high",
			"critical",
			"firing",
			"cluster-a",
			"default",
			`{"alertname":"CPUHigh"}`,
			`{"description":"cpu high"}`,
			"oncall",
			"http://generator",
			"firing",
			nil,
			startsAt,
			endsAt,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := New(db)
	instances, err := repo.Upsert(context.Background(), "prometheus", []domainalert.IngestAlert{
		{
			Fingerprint:  "cpu-high",
			Title:        "CPUHigh",
			Summary:      "cpu high",
			Severity:     "critical",
			Status:       "firing",
			ClusterID:    "cluster-a",
			Namespace:    "default",
			Labels:       map[string]string{"alertname": "CPUHigh"},
			Annotations:  map[string]string{"description": "cpu high"},
			Receiver:     "oncall",
			GeneratorURL: "http://generator",
			StartsAt:     startsAt,
			EndsAt:       endsAt,
		},
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}
	if instances[0].ID != "prometheus:cpu-high" {
		t.Fatalf("instance id = %q, want prometheus:cpu-high", instances[0].ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryAcknowledgePreservesAlertStatus(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer sqlDB.Close()

	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	now := time.Date(2026, 6, 4, 8, 0, 0, 0, time.UTC)
	mock.ExpectExec(`(?s)UPDATE alert_events\s+SET current_state = 'acknowledged',\s+annotations = .*updated_at = .*WHERE id = .*`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "prometheus:cpu-high").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT id, COALESCE\(source_system, source_type, ''\), fingerprint, title, summary, severity, status`).
		WithArgs("prometheus:cpu-high").
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"source",
			"fingerprint",
			"title",
			"summary",
			"severity",
			"status",
			"cluster_id",
			"namespace",
			"labels",
			"annotations",
			"receiver",
			"generator_url",
			"starts_at",
			"ends_at",
			"last_seen_at",
			"current_state",
			"created_at",
			"updated_at",
		}).AddRow(
			"prometheus:cpu-high",
			"prometheus",
			"cpu-high",
			"CPUHigh",
			"cpu high",
			"critical",
			"firing",
			"cluster-a",
			"default",
			[]byte(`{"alertname":"CPUHigh"}`),
			[]byte(`{"acknowledgedAt":"2026-06-04T08:00:00Z","acknowledgedBy":"user-1","acknowledgedByName":"User One"}`),
			"oncall",
			"http://generator",
			nil,
			nil,
			now,
			"acknowledged",
			now,
			now,
		))

	repo := New(db)
	instance, err := repo.Acknowledge(context.Background(), "prometheus:cpu-high", "user-1", "User One")
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}
	if instance.Status != "acknowledged" {
		t.Fatalf("instance status = %q, want acknowledged", instance.Status)
	}
	if instance.AcknowledgedBy != "user-1" {
		t.Fatalf("acknowledgedBy = %q, want user-1", instance.AcknowledgedBy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
