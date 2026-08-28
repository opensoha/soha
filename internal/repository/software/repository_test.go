package software

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	appsoftware "github.com/opensoha/soha/internal/application/software"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSoftwarePackageMigrationUsesPostgreSQLMetadataAndNoBlobColumn(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0053_software_object_storage.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS public.software_packages",
		"tenant_id text NOT NULL DEFAULT 'default'",
		"workspace_id text NOT NULL DEFAULT 'default'",
		"visibility text NOT NULL DEFAULT 'workspace'",
		"status text NOT NULL DEFAULT 'ready'",
		"object_key text NOT NULL",
		"UNIQUE (tenant_id, workspace_id, software_id, version, platform, arch)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
	for _, forbidden := range []string{" bytea", " blob"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("migration stores package body in PostgreSQL: %q", forbidden)
		}
	}
}

func TestSoftwarePackageDownloadCountMigration(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0055_software_package_download_count.sql")
	if err != nil || !strings.Contains(string(raw), "download_count bigint NOT NULL DEFAULT 0") {
		t.Fatalf("download count migration: %v", err)
	}
}

func TestMultipleObjectStorageMigrationRemovesSingletonIndex(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0056_multiple_software_object_storages.sql")
	if err != nil || !strings.Contains(string(raw), "DROP INDEX IF EXISTS public.idx_system_integrations_one_active_storage") {
		t.Fatalf("multiple object storage migration: %v", err)
	}
}

type memoryBlobStore struct {
	payload       []byte
	deleted       bool
	integrationID string
}

func (s *memoryBlobStore) Active(_ context.Context, integrationID string) (appsoftware.StorageBackend, error) {
	if integrationID == "" {
		integrationID = "storage-1"
	}
	s.integrationID = integrationID
	return appsoftware.StorageBackend{IntegrationID: integrationID, ProviderType: "s3", Bucket: "packages", Region: "us-east-1"}, nil
}

func (s *memoryBlobStore) Put(_ context.Context, integrationID, key string, content io.Reader) (int64, string, error) {
	if integrationID != s.integrationID || !strings.HasPrefix(key, "software/packages/") {
		return 0, "", io.ErrUnexpectedEOF
	}
	s.payload, _ = io.ReadAll(content)
	digest := sha256.Sum256(s.payload)
	return int64(len(s.payload)), hex.EncodeToString(digest[:]), nil
}

func (s *memoryBlobStore) Open(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.payload)), nil
}

func (s *memoryBlobStore) Delete(context.Context, string, string) error {
	s.deleted = true
	s.payload = nil
	return nil
}

func TestStoreCreatesMetadataAfterObjectUpload(t *testing.T) {
	db, mock := newRepositoryDB(t)
	blobs := &memoryBlobStore{}
	store, err := New(db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC) }
	mock.ExpectExec(`INSERT INTO software_packages`).WillReturnResult(sqlmock.NewResult(0, 1))

	item, err := store.Create(t.Context(), appsoftware.UploadInput{
		TenantID: "default", WorkspaceID: "default", Visibility: "workspace",
		SoftwareID: "demo", Name: "Demo", Publisher: "OpenSoha", Version: "1.0.0",
		Platform: "darwin", Arch: "arm64", FileName: "demo.pkg",
	}, bytes.NewBufferString("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "ready" || item.StorageIntegrationID != "storage-1" || string(blobs.payload) != "payload" {
		t.Fatalf("created item=%#v payload=%q", item, blobs.payload)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreDeleteTombstonesBeforeRemovingObjectAndMetadata(t *testing.T) {
	db, mock := newRepositoryDB(t)
	blobs := &memoryBlobStore{payload: []byte("payload")}
	store, err := New(db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	mock.ExpectQuery(`SELECT .* FROM software_packages WHERE id=`).WithArgs("pkg-1").
		WillReturnRows(packageRow(now, "ready"))
	mock.ExpectExec(`UPDATE software_packages SET status='deleted'`).WithArgs(now, "pkg-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM software_packages WHERE id=`).WithArgs("pkg-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.Delete(t.Context(), "pkg-1"); err != nil {
		t.Fatal(err)
	}
	if !blobs.deleted {
		t.Fatal("object was not deleted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreListsAndSummarizesReadyObjects(t *testing.T) {
	db, mock := newRepositoryDB(t)
	blobs := &memoryBlobStore{}
	store, err := New(db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(SUM\(size_bytes\), 0\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count", "sum"}).AddRow(int64(1), int64(7)))
	mock.ExpectQuery(`SELECT .* FROM software_packages WHERE .*status='ready'`).
		WillReturnRows(packageRow(now, "ready"))

	storage, err := store.Storage(t.Context(), "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if storage.Backend != "s3" || storage.ObjectCount != 1 || storage.TotalBytes != 7 || len(storage.Items) != 1 {
		t.Fatalf("storage=%#v", storage)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreOpensReadyObjectAndRejectsUnknownCursor(t *testing.T) {
	db, mock := newRepositoryDB(t)
	blobs := &memoryBlobStore{payload: []byte("payload")}
	store, err := New(db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .* FROM software_packages WHERE id=`).WithArgs("pkg-1").
		WillReturnRows(packageRow(now, "ready"))
	item, reader, err := store.Open(t.Context(), "pkg-1")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(reader)
	_ = reader.Close()
	if item.ID != "pkg-1" || string(got) != "payload" {
		t.Fatalf("item=%#v payload=%q", item, got)
	}

	mock.ExpectQuery(`SELECT created_at FROM software_packages WHERE id=`).WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}))
	if _, _, err := store.List(t.Context(), appsoftware.Filter{Cursor: "missing", Limit: 20}); err == nil {
		t.Fatal("unknown cursor should fail")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreListFiltersAndReturnsCursor(t *testing.T) {
	db, mock := newRepositoryDB(t)
	store, err := New(db, &memoryBlobStore{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	rows := packageRow(now, "ready")
	rows.AddRow(
		"pkg-2", "default", "default", "demo", "Demo", "", "OpenSoha", "", "2.0.0", "darwin", "arm64",
		"demo-2.pkg", "storage-1", "software/packages/pkg-2", int64(8), strings.Repeat("b", 64), "workspace", "ready", int64(0), now.Add(-time.Minute), now.Add(-time.Minute),
	)
	mock.ExpectQuery(`SELECT .* FROM software_packages WHERE .*status='ready' AND platform=\$1 AND arch=\$2`).
		WithArgs("darwin", "arm64", 2).WillReturnRows(rows)

	items, next, err := store.List(t.Context(), appsoftware.Filter{Platform: "darwin", Arch: "arm64", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || next != "pkg-1" {
		t.Fatalf("items=%#v next=%q", items, next)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreTargetsAndFiltersStorageIntegration(t *testing.T) {
	db, mock := newRepositoryDB(t)
	blobs := &memoryBlobStore{}
	store, err := New(db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`INSERT INTO software_packages`).WillReturnResult(sqlmock.NewResult(0, 1))
	item, err := store.Create(t.Context(), appsoftware.UploadInput{
		StorageIntegrationID: "storage-2", TenantID: "default", WorkspaceID: "default", Visibility: "workspace",
		SoftwareID: "demo", Name: "Demo", Publisher: "OpenSoha", Version: "2.0.0",
		Platform: "darwin", Arch: "arm64", FileName: "demo.pkg",
	}, bytes.NewBufferString("payload"))
	if err != nil || item.StorageIntegrationID != "storage-2" {
		t.Fatalf("item=%#v err=%v", item, err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(SUM\(size_bytes\), 0\).*storage_integration_id=\$1`).
		WithArgs("storage-2").WillReturnRows(sqlmock.NewRows([]string{"count", "sum"}).AddRow(int64(1), int64(7)))
	mock.ExpectQuery(`SELECT .*storage_integration_id=\$1`).WithArgs("storage-2", 51).WillReturnRows(packageRow(now, "ready"))
	storage, err := store.Storage(t.Context(), "storage-2", "", 50)
	if err != nil || storage.IntegrationID != "storage-2" || len(storage.Items) != 1 {
		t.Fatalf("storage=%#v err=%v", storage, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsMissingDependenciesAndQuarantinedOpen(t *testing.T) {
	if _, err := New(nil, &memoryBlobStore{}); err == nil {
		t.Fatal("nil database should fail")
	}
	db, mock := newRepositoryDB(t)
	store, err := New(db, &memoryBlobStore{payload: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .* FROM software_packages WHERE id=`).WithArgs("pkg-1").
		WillReturnRows(packageRow(now, "quarantined"))
	if _, _, err := store.Open(t.Context(), "pkg-1"); err == nil {
		t.Fatal("quarantined package should not open")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreCleansUploadedObjectWhenMetadataConflicts(t *testing.T) {
	db, mock := newRepositoryDB(t)
	blobs := &memoryBlobStore{}
	store, err := New(db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`INSERT INTO software_packages`).
		WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate"})
	_, err = store.Create(t.Context(), appsoftware.UploadInput{
		TenantID: "default", WorkspaceID: "default", Visibility: "workspace",
		SoftwareID: "demo", Name: "Demo", Publisher: "OpenSoha", Version: "1.0.0",
		Platform: "darwin", Arch: "arm64", FileName: "demo.pkg",
	}, bytes.NewBufferString("payload"))
	if !errors.Is(err, apperrors.ErrConflict) || !blobs.deleted {
		t.Fatalf("conflict error=%v object deleted=%v", err, blobs.deleted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRetriesObjectRemovalForTombstonedMetadata(t *testing.T) {
	db, mock := newRepositoryDB(t)
	blobs := &memoryBlobStore{payload: []byte("payload")}
	store, err := New(db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .* FROM software_packages WHERE id=`).WithArgs("pkg-1").
		WillReturnRows(packageRow(now, "deleted"))
	mock.ExpectExec(`DELETE FROM software_packages WHERE id=`).WithArgs("pkg-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.Delete(t.Context(), "pkg-1"); err != nil || !blobs.deleted {
		t.Fatalf("retry delete error=%v object deleted=%v", err, blobs.deleted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreContinuesFromValidCursor(t *testing.T) {
	db, mock := newRepositoryDB(t)
	store, err := New(db, &memoryBlobStore{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT created_at FROM software_packages WHERE id=`).WithArgs("pkg-1").
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	mock.ExpectQuery(`SELECT .* FROM software_packages WHERE .*status='ready' AND \(created_at <`).
		WithArgs(now, now, "pkg-1", 21).WillReturnRows(packageRow(now.Add(-time.Minute), "ready"))
	items, _, err := store.List(t.Context(), appsoftware.Filter{Cursor: "pkg-1", Limit: 20})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreDetectsTamperedObjectDuringDownload(t *testing.T) {
	db, mock := newRepositoryDB(t)
	store, err := New(db, &memoryBlobStore{payload: []byte("tamper!")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .* FROM software_packages WHERE id=`).WithArgs("pkg-1").
		WillReturnRows(packageRow(now, "ready"))
	_, reader, err := store.Open(t.Context(), "pkg-1")
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr == nil {
		t.Fatal("tampered object should fail integrity verification")
	}
}

func TestStoreIncrementsDownloadCount(t *testing.T) {
	db, mock := newRepositoryDB(t)
	store, err := New(db, &memoryBlobStore{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`UPDATE software_packages SET download_count=download_count\+1`).WithArgs("pkg-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.IncrementDownloadCount(t.Context(), "pkg-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newRepositoryDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func packageRow(now time.Time, status string) *sqlmock.Rows {
	digest := sha256.Sum256([]byte("payload"))
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "workspace_id", "software_id", "name", "description", "publisher", "category",
		"version", "platform", "arch", "file_name", "storage_integration_id", "object_key", "size_bytes", "sha256",
		"visibility", "status", "download_count", "created_at", "updated_at",
	}).AddRow(
		"pkg-1", "default", "default", "demo", "Demo", "", "OpenSoha", "", "1.0.0", "darwin", "arm64",
		"demo.pkg", "storage-1", "software/packages/pkg-1", int64(7), hex.EncodeToString(digest[:]), "workspace", status, int64(0), now, now,
	)
}
