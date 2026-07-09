package providerportal

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryListRecentLaunchesAllowsNullProviderID(t *testing.T) {
	repo, mock := newProviderPortalRepository(t)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)FROM\s+identity_application_launches\s+l`).
		WithArgs("user-1", 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"application_id",
			"name",
			"user_id",
			"provider_id",
			"provider_type",
			"result",
			"reason",
			"launch_url",
			"source_ip",
			"user_agent",
			"created_at",
		}).AddRow(
			"launch-1",
			"app-1",
			"Console",
			"user-1",
			nil,
			"link",
			"success",
			"",
			"https://console.example",
			"127.0.0.1",
			"curl/8",
			createdAt,
		))

	items, err := repo.ListRecentLaunches(context.Background(), "user-1", 0)
	if err != nil {
		t.Fatalf("ListRecentLaunches returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ProviderID != "" {
		t.Fatalf("ProviderID = %q, want empty string for NULL provider_id", items[0].ProviderID)
	}
	if items[0].ID != "launch-1" || items[0].ApplicationName != "Console" || !items[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected item: %#v", items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func newProviderPortalRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
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
