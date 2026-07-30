package manifest

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
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

func TestUpdateSourceReturnsConflictForStaleGeneration(t *testing.T) {
	repository, mock := newManifestRepository(t)
	now := time.Now().UTC()
	mock.ExpectExec(`UPDATE manifest_sources`).
		WithArgs(domainmanifest.SourceModeSohaManaged, "", "", "", "", `[]`, `[]`, domainmanifest.SyncPolicyManual, 0, false, false, "manifest-1", int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM manifest_sources source`).WithArgs("manifest-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "package_id", "mode", "repository_id", "ref_type", "ref_value", "source_path",
			"include_patterns", "exclude_patterns", "sync_policy", "poll_interval_seconds", "auto_publish", "auto_deploy",
			"last_resolved_commit", "last_tree_digest", "last_canonical_digest", "last_successful_sync_at", "last_error_code", "last_error_message",
			"generation", "created_at", "updated_at",
		}).AddRow("source-1", "manifest-1", domainmanifest.SourceModeSohaManaged, "", "", "", "", `[]`, `[]`, domainmanifest.SyncPolicyManual, 0, false, false, "", "", "", nil, "", "", int64(2), now, now))

	_, err := repository.UpdateSource(context.Background(), "manifest-1", domainmanifest.SourceInput{
		Mode: domainmanifest.SourceModeSohaManaged, IncludePatterns: []string{}, ExcludePatterns: []string{},
		SyncPolicy: domainmanifest.SyncPolicyManual, ExpectedGeneration: 1,
	})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("UpdateSource() error = %v, want conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUpdateSourceMapsUnknownRepositoryToInvalidArgument(t *testing.T) {
	repository, mock := newManifestRepository(t)
	mock.ExpectExec(`UPDATE manifest_sources`).WillReturnError(&pgconn.PgError{Code: "23503"})

	_, err := repository.UpdateSource(context.Background(), "manifest-1", domainmanifest.SourceInput{
		Mode: domainmanifest.SourceModeGitSynced, RepositoryID: "missing", RefType: domainmanifest.SourceRefBranch,
		RefValue: "main", Path: ".", IncludePatterns: []string{}, ExcludePatterns: []string{},
		SyncPolicy: domainmanifest.SyncPolicyManual, ExpectedGeneration: 1,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("UpdateSource() error = %v, want invalid argument", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUpdateSyncTaskIgnoresStaleSourceGeneration(t *testing.T) {
	repository, mock := newManifestRepository(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT run.source_id, run.package_id, run.source_generation, source.generation`).
		WithArgs("task-1").
		WillReturnRows(sqlmock.NewRows([]string{"source_id", "package_id", "run_generation", "source_generation", "actor", "auto_publish", "canonical_digest"}).
			AddRow("source-1", "manifest-1", int64(1), int64(2), "admin", true, "sha256:old"))
	mock.ExpectExec(`UPDATE manifest_sync_runs SET status='ignored', error_code='stale_source_generation'`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "task-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repository.UpdateSyncTask(context.Background(), domaindelivery.ExecutionTask{ID: "task-1", Status: "completed"}, domainmanifest.TaskPayload{Generation: 1}, domainmanifest.TaskResult{CanonicalDigest: "sha256:new"})
	if err != nil {
		t.Fatalf("UpdateSyncTask() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestDecideDeliveryIntentRejectsAcceptedProposalForGitSynchronizedSource(t *testing.T) {
	repository, mock := newManifestRepository(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	item := domainmanifest.DeliveryIntent{
		ID: "intent-1", PackageID: "manifest-1", Status: domainmanifest.IntentStatusAccepted,
		Files:     []domainmanifest.File{{Path: "deployment.yaml", Content: "apiVersion: v1\nkind: ConfigMap\n"}},
		DecidedBy: "admin", DecidedAt: &now, UpdatedAt: now,
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM manifest_delivery_intents WHERE id=\$1 FOR UPDATE`).
		WithArgs(item.ID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(domainmanifest.IntentStatusDraft))
	mock.ExpectQuery(`SELECT package.current_revision, package.updated_at, source.mode`).
		WithArgs(item.PackageID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision", "updated_at", "mode"}).
			AddRow(0, now, domainmanifest.SourceModeGitSynced))
	mock.ExpectRollback()

	_, err := repository.DecideDeliveryIntent(context.Background(), item, domainmanifest.DeliveryIntentDecisionInput{
		ExpectedCurrentRevision: 0, ExpectedPackageUpdatedAt: now,
	})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("DecideDeliveryIntent() error = %v, want conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestCreateBindingMaintainsLegacyProjection(t *testing.T) {
	repository, mock := newManifestRepository(t)
	now := time.Now().UTC()
	item := domainmanifest.EnvironmentBinding{
		ID: "binding-1", PackageID: "manifest-1", ApplicationEnvironmentID: "payments-dev",
		EnvironmentKey: "dev", ClusterID: "dev-1", Namespace: "payments", Overlay: map[string]string{"image": "v2"},
		DriftPolicy: domainmanifest.DriftPolicyReport, DeletionPolicy: domainmanifest.DeletionPolicyOrphan,
		Enabled: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO manifest_bindings`).
		WithArgs(item.ID, item.PackageID, item.ApplicationEnvironmentID, item.EnvironmentKey, item.ClusterID,
			item.Namespace, `{"image":"v2"}`, "", "", item.DriftPolicy, item.DeletionPolicy,
			item.Enabled, item.Version, item.CreatedAt, item.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE manifest_packages package`).WithArgs(item.PackageID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`FROM manifest_bindings`).WithArgs(item.ID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "package_id", "application_environment_id", "environment_key", "cluster_id", "namespace",
			"overlay", "rollout_strategy_id", "verification_policy_id", "drift_policy", "deletion_policy",
			"enabled", "version", "created_at", "updated_at",
		}).AddRow(item.ID, item.PackageID, item.ApplicationEnvironmentID, item.EnvironmentKey, item.ClusterID,
			item.Namespace, `{"image":"v2"}`, "", "", item.DriftPolicy, item.DeletionPolicy,
			item.Enabled, item.Version, item.CreatedAt, item.UpdatedAt))

	created, err := repository.CreateBinding(context.Background(), item)
	if err != nil {
		t.Fatalf("CreateBinding() error = %v", err)
	}
	if created.ID != item.ID || created.Overlay["image"] != "v2" {
		t.Fatalf("CreateBinding() = %#v, want persisted binding", created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestCreatePackageMaintainsRelationalBindings(t *testing.T) {
	repository, mock := newManifestRepository(t)
	now := time.Now().UTC()
	item := domainmanifest.Package{
		ID: "manifest-1", Name: "Payments", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
		Status: domainmanifest.StatusDraft, Files: []domainmanifest.File{},
		Bindings: []domainmanifest.Binding{{
			ID: "binding-1", ApplicationEnvironmentID: "payments-dev", EnvironmentKey: "dev",
			ClusterID: "dev-1", Namespace: "payments", Overlay: map[string]string{"image": "v2"}, Status: "not_deployed",
		}},
		CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now,
	}
	files, bindings, err := encodePackageJSON(item)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO manifest_packages`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO manifest_bindings`).
		WithArgs("binding-1", "manifest-1", "payments-dev", "dev", "dev-1", "payments", `{"image":"v2"}`, now, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM manifest_bindings WHERE package_id = \$1 AND id NOT IN \(\$2\)`).
		WithArgs("manifest-1", "binding-1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE manifest_packages package`).WithArgs("manifest-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT id, name, description.*FROM manifest_packages`).WithArgs("manifest-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "description", "application_id", "business_line_id", "renderer", "status",
			"current_revision", "files", "bindings", "created_by", "updated_by", "created_at", "updated_at",
		}).AddRow(item.ID, item.Name, item.Description, item.ApplicationID, item.BusinessLineID, item.Renderer,
			item.Status, item.CurrentRevision, files, bindings, item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt))

	created, err := repository.Create(context.Background(), item)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(created.Bindings) != 1 || created.Bindings[0].ID != "binding-1" {
		t.Fatalf("Create() bindings = %#v, want relationally projected binding", created.Bindings)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestDeclarativeManifestMigrationPreservesCompatibilityAndIntegrity(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0036_declarative_manifest_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"CREATE TRIGGER manifest_packages_ensure_source",
		"VALUES ('manifest-source-' || NEW.id",
		"CASE WHEN jsonb_typeof(package.bindings) = 'array'",
		"jsonb_array_elements(",
		"manifest_binding_migration_issues",
		"duplicate binding id or package target",
		"FOREIGN KEY (binding_id, package_id)",
		"manifest_deployment_conditions",
		"manifest_resource_inventory",
		"manifest_revisions_overlay_snapshot_shape_check",
		"manifest_revisions_validation_summary_shape_check",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
}

func TestManifestReconciliationMigrationIncludesDurableState(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0037_manifest_reconciliation_and_proposals.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"ADD COLUMN IF NOT EXISTS auto_deploy",
		"ADD COLUMN IF NOT EXISTS last_canonical_digest",
		"ADD COLUMN IF NOT EXISTS last_execution_task_id",
		"ADD COLUMN IF NOT EXISTS drift",
		"CREATE TABLE manifest_operation_runs",
		"CREATE TABLE manifest_sync_runs",
		"CREATE TABLE manifest_delivery_intents",
		"idempotency_key TEXT NOT NULL UNIQUE",
		"UNIQUE (idempotency_key)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing %q", required)
		}
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
