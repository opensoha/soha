package application

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatalf("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}

func TestListBuildSourcesMigratesLegacyBuildSource(t *testing.T) {
	repo, mock := newApplicationRepository(t)
	app := domainapp.App{
		ID:              "app-1",
		Name:            "Demo",
		Key:             "demo",
		Enabled:         true,
		BuildImage:      "registry.local/demo",
		BuildContextDir: "./src",
		DockerfilePath:  "Dockerfile",
	}

	mock.ExpectQuery(legacyBuildSourcesSelectPattern()).
		WithArgs("app-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_name", "source_type", "enabled", "is_default", "build_image", "default_tag", "config", "created_at", "updated_at",
		}))
	mock.ExpectExec(legacyBuildSourcesInsertPattern()).
		WithArgs(
			"default:app-1",
			"app-1",
			"Repository Dockerfile",
			string(domainapp.BuildSourceTypeRepoDockerfile),
			true,
			true,
			"registry.local/demo",
			nil,
			jsonContains{
				"builderKind":    "docker",
				"contextDir":     "./src",
				"dockerfilePath": "Dockerfile",
			},
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	items, err := repo.listBuildSources(context.Background(), app.ID, app)
	if err != nil {
		t.Fatalf("listBuildSources returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ID != "default:app-1" || items[0].Name != "Repository Dockerfile" || items[0].Type != domainapp.BuildSourceTypeRepoDockerfile {
		t.Fatalf("unexpected legacy build source: %#v", items[0])
	}
	if items[0].BuildImage != "registry.local/demo" || items[0].Config["contextDir"] != "./src" || items[0].Config["dockerfilePath"] != "Dockerfile" {
		t.Fatalf("unexpected legacy build source config: %#v", items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListBuildSourcesUsesExistingRowsWithoutMigration(t *testing.T) {
	repo, mock := newApplicationRepository(t)

	rows := sqlmock.NewRows([]string{
		"id", "source_name", "source_type", "enabled", "is_default", "build_image", "default_tag", "config", "created_at", "updated_at",
	}).AddRow(
		"source-1",
		"Repo Dockerfile",
		string(domainapp.BuildSourceTypeRepoDockerfile),
		true,
		true,
		"registry.local/demo",
		"stable",
		`{"builderKind":"docker","contextDir":".","dockerfilePath":"Dockerfile"}`,
		time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
	)
	mock.ExpectQuery(legacyBuildSourcesSelectPattern()).
		WithArgs("app-1").
		WillReturnRows(rows)

	items, err := repo.listBuildSources(context.Background(), "app-1", domainapp.App{ID: "app-1"})
	if err != nil {
		t.Fatalf("listBuildSources returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ID != "source-1" || items[0].DefaultTag != "stable" {
		t.Fatalf("unexpected build source item: %#v", items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func legacyBuildSourcesSelectPattern() string {
	return `(?s)^SELECT\s+id,\s+source_name,\s+source_type,\s+enabled,\s+is_default,\s+build_image,\s+default_tag,\s+config,\s+created_at,\s+updated_at\s+FROM\s+application_build_sources\s+WHERE\s+application_id\s+=\s+\$1\s+ORDER\s+BY\s+is_default\s+DESC,\s+created_at\s+ASC$`
}

func legacyBuildSourcesInsertPattern() string {
	return `(?s)^INSERT\s+INTO\s+application_build_sources\s*\(\s*id,\s*application_id,\s*source_name,\s*source_type,\s*enabled,\s*is_default,\s*build_image,\s*default_tag,\s*config,\s*created_at,\s*updated_at\s*\)\s*VALUES\s*\(\s*\$1,\s*\$2,\s*\$3,\s*\$4,\s*\$5,\s*\$6,\s*\$7,\s*\$8,\s*\$9,\s*\$10,\s*\$11\s*\)\s*ON\s+CONFLICT\s*\(id\)\s+DO\s+NOTHING$`
}

type jsonContains map[string]any

func (j jsonContains) Match(v driver.Value) bool {
	var raw []byte
	switch typed := v.(type) {
	case string:
		raw = []byte(typed)
	case []byte:
		raw = typed
	default:
		return false
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		return false
	}
	for key, want := range j {
		if got[key] != want {
			return false
		}
	}
	return true
}

func newApplicationRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
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
