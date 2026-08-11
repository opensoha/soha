package virtualization

import (
	"context"
	"errors"
	"slices"
	"strings"
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

func TestImageExtraClausesGroupsCatalogAndStorageKinds(t *testing.T) {
	tests := []struct {
		name     string
		category string
		wantArgs []any
	}{
		{name: "catalog", category: "catalog", wantArgs: []any{"iso", "template", "lxc_template", "datasource", "pvc"}},
		{name: "storage", category: "storage", wantArgs: []any{"storage", "storage_content", "image", "images", "rootdir", "datavolume", "persistentvolumeclaim"}},
		{name: "all", category: "", wantArgs: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clauses, args := imageExtraClauses(domainvirtualization.ImageFilter{Category: tt.category})
			if !slices.Equal(args, tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tt.wantArgs)
			}
			if tt.category == "" {
				if len(clauses) != 0 {
					t.Fatalf("clauses = %#v, want none", clauses)
				}
				return
			}
			if len(clauses) != 1 || !strings.Contains(clauses[0], "config->>'sourceKind'") {
				t.Fatalf("clauses = %#v, want sourceKind JSON filter", clauses)
			}
		})
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

func TestUpdateTaskFencesClaimedWorkerAttempt(t *testing.T) {
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
	mock.ExpectExec(`(?s)UPDATE virtualization_tasks.*WHERE id = \$12 AND claimed_by_worker_id = \$13 AND attempt_count = \$14 AND status = 'running'`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	_, err = repo.UpdateTask(context.Background(), domainvirtualization.Task{
		ID: "task-1", Status: "completed", ClaimedByWorkerID: "worker-1", AttemptCount: 2,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateTask() error = %v, want ErrNotFound for stale attempt", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUpdateTaskResultPreservesTaskState(t *testing.T) {
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
	mock.ExpectExec(`(?s)UPDATE virtualization_tasks\s+SET result = \$1, updated_at = \$2\s+WHERE id = \$3`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "task-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.UpdateTaskResult(context.Background(), "task-1", map[string]any{"connectionDeletedAt": "now"})
	if err != nil {
		t.Fatalf("UpdateTaskResult() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
