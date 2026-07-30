package manifest

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestListAppliesAuthorizedApplicationsAndPagination(t *testing.T) {
	repository, mock := newManifestRepository(t)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM manifest_packages WHERE archived_at IS NULL AND application_id IN \(\$1,\$2\)`).
		WithArgs("app-1", "app-2").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(`SELECT id, name, description, application_id, business_line_id, renderer, status, current_revision, files, bindings, created_by, updated_by, created_at, updated_at FROM manifest_packages WHERE archived_at IS NULL AND application_id IN \(\$1,\$2\) ORDER BY updated_at DESC LIMIT \$3 OFFSET \$4`).
		WithArgs("app-1", "app-2", 2, 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "description", "application_id", "business_line_id", "renderer", "status", "current_revision", "files", "bindings", "created_by", "updated_by", "created_at", "updated_at",
		}).AddRow("manifest-3", "Ingress", "", "app-2", "line-1", domainmanifest.RendererRaw, domainmanifest.StatusDraft, 0, `[]`, `[]`, "admin", "admin", now, now))

	page, err := repository.List(context.Background(), domainmanifest.Filter{
		ApplicationIDs: []string{"app-1", "app-2"}, Page: 2, PageSize: 2,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 3 || page.Page != 2 || page.PageSize != 2 || len(page.Items) != 1 {
		t.Fatalf("List() = %#v, want second page with one item", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestDeleteArchivesPublishedPackage(t *testing.T) {
	repository, mock := newManifestRepository(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM manifest_packages WHERE id = \$1 AND archived_at IS NULL AND current_revision = 0`).
		WithArgs("manifest-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE manifest_packages SET archived_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id = \$1 AND archived_at IS NULL AND current_revision > 0`).
		WithArgs("manifest-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repository.Delete(context.Background(), "manifest-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func newManifestRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
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
