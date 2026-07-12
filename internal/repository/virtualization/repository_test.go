package virtualization

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestVMExtraClausesExcludeDeletedByDefault(t *testing.T) {
	clauses, args := vmExtraClauses(domainvirtualization.VMFilter{Namespace: "apps"})
	if !slices.Contains(clauses, "namespace = ?") {
		t.Fatalf("clauses = %#v, want namespace clause", clauses)
	}
	if !slices.Contains(clauses, "status <> ?") {
		t.Fatalf("clauses = %#v, want deleted exclusion", clauses)
	}
	if len(args) != 2 || args[0] != "apps" || args[1] != "deleted" {
		t.Fatalf("args = %#v", args)
	}
}

func TestVMExtraClausesAllowsExplicitDeletedStatus(t *testing.T) {
	clauses, args := vmExtraClauses(domainvirtualization.VMFilter{Status: "deleted"})
	if slices.Contains(clauses, "status <> ?") {
		t.Fatalf("clauses = %#v, should not add deleted exclusion", clauses)
	}
	if len(args) != 0 {
		t.Fatalf("args = %#v, want none", args)
	}
}

func TestSafeTableNameAllowsKnownVirtualizationTables(t *testing.T) {
	for _, tableName := range []string{
		"virtualization_connections",
		"virtualization_vms",
		"virtualization_images",
		"virtualization_flavors",
		"virtualization_tasks",
	} {
		got, err := safeTableName(tableName)
		if err != nil {
			t.Fatalf("safeTableName(%q) error = %v", tableName, err)
		}
		if got != tableName {
			t.Fatalf("safeTableName(%q) = %q", tableName, got)
		}
	}
}

func TestBuildAssetListQueryRejectsUnknownTable(t *testing.T) {
	if _, _, _, err := buildAssetListQuery(vmSelect(), "virtualization_vms; DROP TABLE virtualization_vms", "", "", "", "", nil, 0, 0, 0); err == nil {
		t.Fatal("buildAssetListQuery accepted unsafe table name")
	}
}

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatalf("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}

func TestRepositoryGetConnectionNormalizesMissingRow(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	repo := New(db)
	mock.ExpectQuery(`(?s)SELECT id, provider, name, endpoint.*FROM virtualization_connections.*WHERE id = \$1`).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "provider", "name", "endpoint", "kubernetes_cluster_id", "default_namespace", "enabled", "verify_tls",
			"encrypted_credential", "config", "health", "last_synced_at", "created_at", "updated_at",
		}))

	_, err = repo.GetConnection(context.Background(), "missing")
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("GetConnection() error = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
