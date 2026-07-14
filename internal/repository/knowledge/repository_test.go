package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAccessClauseFiltersDocumentACLBeforeContentRead(t *testing.T) {
	t.Parallel()
	clause, args := accessClause("d", domainknowledge.PrincipalScope{UserID: "user-1", Roles: []string{"reader"}, Teams: []string{"sre"}})
	if strings.Contains(clause, "owner_id") || strings.Contains(clause, ".scope") {
		t.Fatalf("document clause uses wrong columns: %s", clause)
	}
	for _, expected := range []string{"d.acl->>'visibility'", "jsonb_exists(d.acl->'users'", "jsonb_exists(d.acl->'roles'", "jsonb_exists(d.acl->'teams'"} {
		if !strings.Contains(clause, expected) {
			t.Fatalf("clause %q missing %q", clause, expected)
		}
	}
	if len(args) != 3 {
		t.Fatalf("args=%v, want principal plus role/team", args)
	}
}

func TestAdvanceIngestionJobRejectsStaleStageCallback(t *testing.T) {
	repository, mock := newKnowledgeRepository(t)
	now := time.Date(2026, 7, 15, 1, 2, 3, 0, time.UTC)
	job := domainknowledge.IngestionJob{
		ID:        "job-1",
		Stage:     domainknowledge.IngestionStageParsing,
		Status:    domainknowledge.IngestionJobRunning,
		Attempt:   1,
		UpdatedAt: now,
		Checkpoint: domainknowledge.IngestionCheckpoint{
			Stage:      domainknowledge.IngestionStageParsing,
			RecordedAt: now,
		},
	}
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE ai_knowledge_ingestion_jobs.*WHERE id=\$12 AND status=\$13 AND stage=\$14 AND lease_token=\$15`).
		WithArgs(
			job.Stage,
			job.Status,
			job.Attempt,
			job.CancelRequested,
			sqlmock.AnyArg(),
			job.ErrorCode,
			job.Error,
			job.NextAttemptAt,
			job.LeaseExpiresAt,
			job.UpdatedAt,
			job.CompletedAt,
			job.ID,
			domainknowledge.IngestionJobRunning,
			domainknowledge.IngestionStageFetching,
			job.LeaseToken,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repository.AdvanceIngestionJob(
		context.Background(),
		job,
		domainknowledge.IngestionJobRunning,
		domainknowledge.IngestionStageFetching,
	)
	if !errors.Is(err, domainknowledge.ErrIngestionConflict) {
		t.Fatalf("AdvanceIngestionJob() error = %v, want conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newKnowledgeRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
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
