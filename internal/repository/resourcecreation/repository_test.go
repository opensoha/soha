package resourcecreation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestClaimCreatesBatchAndDocumentsAtomically(t *testing.T) {
	repository, mock := newRepository(t)
	documents := createDocuments()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO platform_resource_creation_batches`).
		WithArgs(sqlmock.AnyArg(), "actor-1", "cluster-1", "request-1", strings.Repeat("a", 64), domainresource.ResourceCreateBatchRunning, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO platform_resource_creation_documents`).
		WithArgs(sqlmock.AnyArg(), 0, "v1", "ConfigMap", "settings", "minio", true, "not_started", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO platform_resource_creation_documents`).
		WithArgs(sqlmock.AnyArg(), 1, "rbac.authorization.k8s.io/v1", "ClusterRole", "reader", "", false, "not_started", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	claim, err := repository.Claim(context.Background(), "actor-1", "cluster-1", "request-1", strings.Repeat("a", 64), documents)
	if err != nil {
		t.Fatal(err)
	}
	if !claim.Created || claim.Batch.Status != domainresource.ResourceCreateBatchRunning || len(claim.Batch.Documents) != 2 {
		t.Fatalf("claim = %#v", claim)
	}
	if claim.Batch.Documents[0].Status != "not_started" || claim.Batch.Documents[0].Error != "" {
		t.Fatalf("initial document = %#v", claim.Batch.Documents[0])
	}
	assertExpectations(t, mock)
}

func TestClaimRollsBackBatchWhenDocumentInsertFails(t *testing.T) {
	repository, mock := newRepository(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO platform_resource_creation_batches`).
		WithArgs(sqlmock.AnyArg(), "actor-1", "cluster-1", "request-1", strings.Repeat("a", 64), domainresource.ResourceCreateBatchRunning, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO platform_resource_creation_documents`).
		WithArgs(sqlmock.AnyArg(), 0, "v1", "ConfigMap", "settings", "minio", true, "not_started", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("database unavailable"))
	mock.ExpectRollback()

	_, err := repository.Claim(context.Background(), "actor-1", "cluster-1", "request-1", strings.Repeat("a", 64), createDocuments()[:1])
	if err == nil {
		t.Fatal("Claim() error = nil, want document insert failure")
	}
	assertExpectations(t, mock)
}

func TestClaimReturnsExistingTerminalBatchForSameContent(t *testing.T) {
	repository, mock := newRepository(t)
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO platform_resource_creation_batches`).
		WithArgs(sqlmock.AnyArg(), "actor-1", "cluster-1", "request-1", strings.Repeat("a", 64), domainresource.ResourceCreateBatchRunning, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectBatchByIdentity(mock, now, domainresource.ResourceCreateBatchSucceeded, strings.Repeat("a", 64))
	expectDocuments(mock, "batch-1", "succeeded")
	mock.ExpectCommit()

	claim, err := repository.Claim(context.Background(), "actor-1", "cluster-1", "request-1", strings.Repeat("a", 64), createDocuments()[:1])
	if err != nil {
		t.Fatal(err)
	}
	if claim.Created || claim.Batch.ID != "batch-1" || claim.Batch.Status != domainresource.ResourceCreateBatchSucceeded {
		t.Fatalf("claim = %#v", claim)
	}
	assertExpectations(t, mock)
}

func TestClaimRejectsSameKeyWithDifferentContent(t *testing.T) {
	repository, mock := newRepository(t)
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO platform_resource_creation_batches`).
		WithArgs(sqlmock.AnyArg(), "actor-1", "cluster-1", "request-1", strings.Repeat("b", 64), domainresource.ResourceCreateBatchRunning, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectBatchByIdentity(mock, now, domainresource.ResourceCreateBatchRunning, strings.Repeat("a", 64))
	expectDocuments(mock, "batch-1", "not_started")
	mock.ExpectRollback()

	_, err := repository.Claim(context.Background(), "actor-1", "cluster-1", "request-1", strings.Repeat("b", 64), createDocuments()[:1])
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("Claim() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func TestUpdateDocumentPersistsSanitizedChildResult(t *testing.T) {
	repository, mock := newRepository(t)
	document := createDocuments()[0]
	document.Status = "failed"
	document.ErrorCode = "resource_already_exists"
	document.Error = strings.Repeat("x", maxErrorSummaryBytes+100)
	mock.ExpectExec(`UPDATE platform_resource_creation_documents AS document`).
		WithArgs("v1", "ConfigMap", "settings", "minio", true, "failed", "resource_already_exists", strings.Repeat("x", maxErrorSummaryBytes), sqlmock.AnyArg(), "batch-1", 0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repository.UpdateDocument(context.Background(), "batch-1", document); err != nil {
		t.Fatal(err)
	}
	assertExpectations(t, mock)
}

func TestUpdateDocumentRejectsDifferentSecondTerminalResult(t *testing.T) {
	repository, mock := newRepository(t)
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	document := createDocuments()[0]
	document.Status = "failed"
	document.ErrorCode = "resource_create_failed"
	document.Error = "timeout"
	mock.ExpectExec(`UPDATE platform_resource_creation_documents AS document`).
		WithArgs("v1", "ConfigMap", "settings", "minio", true, "failed", "resource_create_failed", "timeout", sqlmock.AnyArg(), "batch-1", 0).
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectBatchByID(mock, now, domainresource.ResourceCreateBatchRunning)
	expectDocuments(mock, "batch-1", "succeeded")

	err := repository.UpdateDocument(context.Background(), "batch-1", document)
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("UpdateDocument() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func TestCompleteIsIdempotentForSameTerminalStatus(t *testing.T) {
	repository, mock := newRepository(t)
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE platform_resource_creation_batches`).
		WithArgs(domainresource.ResourceCreateBatchSucceeded, sqlmock.AnyArg(), sqlmock.AnyArg(), "batch-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectBatchByID(mock, now, domainresource.ResourceCreateBatchSucceeded)
	expectDocuments(mock, "batch-1", "succeeded")
	mock.ExpectCommit()

	batch, err := repository.Complete(context.Background(), "batch-1", domainresource.ResourceCreateBatchSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != domainresource.ResourceCreateBatchSucceeded {
		t.Fatalf("batch = %#v", batch)
	}
	assertExpectations(t, mock)
}

func TestCompletePersistsTerminalBatchState(t *testing.T) {
	repository, mock := newRepository(t)
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE platform_resource_creation_batches`).
		WithArgs(domainresource.ResourceCreateBatchSucceeded, sqlmock.AnyArg(), sqlmock.AnyArg(), "batch-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectBatchByID(mock, now, domainresource.ResourceCreateBatchSucceeded)
	expectDocuments(mock, "batch-1", "succeeded")
	mock.ExpectCommit()

	batch, err := repository.Complete(context.Background(), "batch-1", domainresource.ResourceCreateBatchSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	if batch.FinishedAt == nil || batch.Status != domainresource.ResourceCreateBatchSucceeded {
		t.Fatalf("batch = %#v", batch)
	}
	assertExpectations(t, mock)
}

func TestCompleteRejectsConflictingTerminalStatus(t *testing.T) {
	repository, mock := newRepository(t)
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE platform_resource_creation_batches`).
		WithArgs(domainresource.ResourceCreateBatchFailed, sqlmock.AnyArg(), sqlmock.AnyArg(), "batch-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectBatchByID(mock, now, domainresource.ResourceCreateBatchSucceeded)
	expectDocuments(mock, "batch-1", "succeeded")
	mock.ExpectRollback()

	_, err := repository.Complete(context.Background(), "batch-1", domainresource.ResourceCreateBatchFailed)
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("Complete() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func createDocuments() []domainresource.ResourceCreateExecutionDocument {
	return []domainresource.ResourceCreateExecutionDocument{
		{Index: 0, Resource: domainresource.ResourceCreateRef{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Namespace: "minio", Namespaced: true}, Status: "ready", Error: "must be cleared"},
		{Index: 1, Resource: domainresource.ResourceCreateRef{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole", Name: "reader"}},
	}
}

func expectBatchByIdentity(mock sqlmock.Sqlmock, now time.Time, status domainresource.ResourceCreateBatchStatus, hash string) {
	mock.ExpectQuery(`SELECT id, actor_id, cluster_id, idempotency_key, content_hash, status,`).
		WithArgs("actor-1", "cluster-1", "request-1").
		WillReturnRows(batchRows(now, status, hash))
}

func expectBatchByID(mock sqlmock.Sqlmock, now time.Time, status domainresource.ResourceCreateBatchStatus) {
	mock.ExpectQuery(`SELECT id, actor_id, cluster_id, idempotency_key, content_hash, status,`).
		WithArgs("batch-1").
		WillReturnRows(batchRows(now, status, strings.Repeat("a", 64)))
}

func batchRows(now time.Time, status domainresource.ResourceCreateBatchStatus, hash string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "actor_id", "cluster_id", "idempotency_key", "content_hash", "status", "created_at", "updated_at", "finished_at"}).
		AddRow("batch-1", "actor-1", "cluster-1", "request-1", hash, status, now, now, now)
}

func expectDocuments(mock sqlmock.Sqlmock, batchID, status string) {
	mock.ExpectQuery(`SELECT document_index, api_version, kind, resource_name, namespace, namespaced,`).
		WithArgs(batchID).
		WillReturnRows(sqlmock.NewRows([]string{"document_index", "api_version", "kind", "resource_name", "namespace", "namespaced", "status", "error_code", "error_summary"}).
			AddRow(0, "v1", "ConfigMap", "settings", "minio", true, status, "", ""))
}

func newRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return New(db), mock
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
