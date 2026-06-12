package registry

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainregistry "github.com/opensoha/soha/internal/domain/registry"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryUpdatePreservesExistingSecretWhenInputSecretEmpty(t *testing.T) {
	repo, mock := newRegistryRepository(t)
	updatedAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	item := domainregistry.Connection{
		Name:         "Docker Hub",
		RegistryType: "docker",
		Endpoint:     "https://registry-1.docker.io",
		Username:     "operator",
		Metadata:     map[string]any{"env": "prod"},
		UpdatedAt:    updatedAt.Format(time.RFC3339),
	}

	mock.ExpectQuery(`SELECT secret FROM registry_connections WHERE id = .*`).
		WithArgs("registry-1").
		WillReturnRows(sqlmock.NewRows([]string{"secret"}).AddRow("soha:v1:existing-secret"))
	mock.ExpectExec(`(?s)UPDATE registry_connections\s+SET name = .*secret = .*WHERE id = .*`).
		WithArgs(
			item.Name,
			item.RegistryType,
			item.Endpoint,
			nil,
			item.Username,
			"soha:v1:existing-secret",
			item.Insecure,
			`{"env":"prod"}`,
			updatedAt,
			"registry-1",
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	updated, err := repo.Update(context.Background(), "registry-1", item)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Secret != "soha:v1:existing-secret" {
		t.Fatalf("updated.Secret = %q, want preserved existing secret", updated.Secret)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func newRegistryRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
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
