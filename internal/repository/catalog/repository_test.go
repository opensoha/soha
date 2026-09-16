package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatalf("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}

func TestNormalizeApplicationEnvironmentInputPreservesEnvironmentDefaults(t *testing.T) {
	item := normalizeApplicationEnvironmentInput(domaincatalog.ApplicationEnvironmentInput{
		ApplicationID: " app-1 ",
		EnvironmentID: " env-dev ",
		Alias:         " 开发环境 ",
		ClusterID:     " cluster-a ",
		Namespace:     " app-dev ",
		RegistryID:    " registry-a ",
		ResourceSelector: domaincatalog.ResourceSelector{
			MatchLabels: map[string]string{"app": "api"},
		},
	})

	if item.ApplicationID != "app-1" || item.EnvironmentID != "env-dev" {
		t.Fatalf("application/environment = %q/%q", item.ApplicationID, item.EnvironmentID)
	}
	if item.Alias != "开发环境" || item.ClusterID != "cluster-a" || item.Namespace != "app-dev" || item.RegistryID != "registry-a" {
		t.Fatalf("environment defaults = %#v", item)
	}
	if item.ResourceSelector.MatchLabels["app"] != "api" {
		t.Fatalf("resource selector = %#v", item.ResourceSelector)
	}
}

func TestSaveApplicationWorkflowCommitsTemplateAndBindingTogether(t *testing.T) {
	repo, mock := newCatalogRepository(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT workflow_template_id, workflow_template_version FROM application_environments`).
		WithArgs("binding-1", "app-1").
		WillReturnRows(sqlmock.NewRows([]string{"workflow_template_id", "workflow_template_version"}).AddRow(nil, int64(0)))
	mock.ExpectExec(`SAVEPOINT`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`INSERT INTO catalog_template_versions`).WillReturnRows(sqlmock.NewRows([]string{"content_digest"}).AddRow("sha256:test"))
	mock.ExpectExec(`INSERT INTO delivery_document_provenance`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO "workflow_templates"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE application_environments`).
		WithArgs(sqlmock.AnyArg(), int64(1), sqlmock.AnyArg(), "binding-1", "app-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	item, err := repo.SaveApplicationWorkflow(context.Background(), "app-1", "binding-1", domaincatalog.WorkflowTemplateInput{
		Key:        "app-binding-1",
		Name:       "Release",
		Definition: map[string]any{"mode": "release_dag"},
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("SaveApplicationWorkflow returned error: %v", err)
	}
	if item.ID == "" || item.Category != "application:app-1" {
		t.Fatalf("workflow = %#v", item)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSaveApplicationWorkflowRollsBackWhenBindingFails(t *testing.T) {
	repo, mock := newCatalogRepository(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT workflow_template_id, workflow_template_version FROM application_environments`).
		WithArgs("binding-1", "app-1").
		WillReturnRows(sqlmock.NewRows([]string{"workflow_template_id", "workflow_template_version"}).AddRow(nil, int64(0)))
	mock.ExpectExec(`SAVEPOINT`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`INSERT INTO catalog_template_versions`).WillReturnRows(sqlmock.NewRows([]string{"content_digest"}).AddRow("sha256:test"))
	mock.ExpectExec(`INSERT INTO delivery_document_provenance`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO "workflow_templates"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE application_environments`).
		WillReturnError(errors.New("binding write failed"))
	mock.ExpectRollback()

	_, err := repo.SaveApplicationWorkflow(context.Background(), "app-1", "binding-1", domaincatalog.WorkflowTemplateInput{
		Key:        "app-binding-1",
		Name:       "Release",
		Definition: map[string]any{"mode": "release_dag"},
		Enabled:    true,
	})
	if err == nil {
		t.Fatal("SaveApplicationWorkflow returned nil error")
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("expectations: %v", expectationErr)
	}
}

func newCatalogRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm postgres mock: %v", err)
	}
	return New(db), mock
}
