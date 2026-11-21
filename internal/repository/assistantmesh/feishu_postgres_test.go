package assistantmesh_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	mesh "github.com/opensoha/soha/internal/repository/assistantmesh"
)

func TestFeishuJobLeaseDedupeAndCleanupWithPostgres(t *testing.T) {
	store := feishuPostgres(t)
	repo := mesh.New(store.DB())
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := strings.Repeat("a", 64)
	first := domain.FeishuJob{
		Scope: scope, ID: strings.Repeat("b", 64), Stage: domain.FeishuReceipt,
		GroupID: "chat", SenderID: "ou_sender", TargetID: "ou_owner", Question: "bounded question",
		NextAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.EnqueueFeishuJob(ctx, first); err != nil {
		t.Fatal(err)
	}
	duplicateReceipt := first
	duplicateReceipt.Question = "must not replace the receipt"
	if err := repo.EnqueueFeishuJob(ctx, duplicateReceipt); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimFeishuJob(ctx, scope, now)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ClaimID == "" || claimed.Question != first.Question {
		t.Fatalf("claim = %+v", claimed)
	}
	if _, err := repo.ClaimFeishuJob(ctx, scope, now); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("second claim error = %v, want not found", err)
	}

	wrong := claimed
	wrong.ClaimID = uuid.NewString()
	if err := repo.SaveFeishuJob(ctx, wrong); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale claim save error = %v, want conflict", err)
	}
	attempt := claimed
	attempt.StartedAt = now
	if err := repo.SaveFeishuJob(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	row := readFeishuJob(t, store, scope, first.ID)
	if row.ClaimID != "" || !row.StartedAt.Equal(now) {
		t.Fatalf("receipt attempt was not persisted with its lease cleared: %+v", row)
	}

	afterLease := now.Add(61 * time.Second)
	reclaimed, err := repo.ClaimFeishuJob(ctx, scope, afterLease)
	if err != nil {
		t.Fatal(err)
	}
	if !reclaimed.StartedAt.Equal(now) {
		t.Fatalf("reclaimed start = %v, want %v", reclaimed.StartedAt, now)
	}
	reclaimed.Stage = domain.FeishuQuestion
	reclaimed.NextAt = afterLease
	reclaimed.StartedAt = time.Time{}
	if err := repo.SaveFeishuJob(ctx, reclaimed); err != nil {
		t.Fatal(err)
	}
	row = readFeishuJob(t, store, scope, first.ID)
	if row.Stage != domain.FeishuQuestion || row.GroupID != "" || row.Question != first.Question {
		t.Fatalf("question stage scrub = %+v", row)
	}

	question, err := repo.ClaimFeishuJob(ctx, scope, afterLease)
	if err != nil {
		t.Fatal(err)
	}
	inquiryID := uuid.NewString()
	question.Stage = domain.FeishuAnswer
	question.InquiryID = inquiryID
	question.CallerID, question.OwnerID = uuid.NewString(), uuid.NewString()
	question.NextAt = afterLease.Add(time.Second)
	if err := repo.SaveFeishuJob(ctx, question); err != nil {
		t.Fatal(err)
	}
	row = readFeishuJob(t, store, scope, first.ID)
	if row.Stage != domain.FeishuAnswer || row.InquiryID != inquiryID || row.Question != "" || row.GroupID != "" {
		t.Fatalf("answer stage = %+v", row)
	}

	second := first
	second.ID = strings.Repeat("c", 64)
	second.NextAt = now
	if err := repo.EnqueueFeishuJob(ctx, second); err != nil {
		t.Fatal(err)
	}
	contender, err := repo.ClaimFeishuJob(ctx, scope, afterLease)
	if err != nil {
		t.Fatal(err)
	}
	contender.Stage = domain.FeishuAnswer
	contender.InquiryID = inquiryID
	if err := repo.SaveFeishuJob(ctx, contender); err != nil {
		t.Fatal(err)
	}
	row = readFeishuJob(t, store, scope, second.ID)
	if row.Stage != domain.FeishuDone || row.Outcome != "duplicate" || row.InquiryID != "" {
		t.Fatalf("duplicate answer = %+v", row)
	}
	if row.GroupID != "" || row.SenderID != "" || row.TargetID != "" || row.CallerID != "" ||
		row.OwnerID != "" || row.Question != "" {
		t.Fatalf("duplicate retained private input: %+v", row)
	}

	retryAt := afterLease.Add(61 * time.Second)
	retry, err := repo.ClaimFeishuJob(ctx, scope, retryAt)
	if err != nil {
		t.Fatal(err)
	}
	retry.Stage = domain.FeishuAnswer
	if err := repo.SaveFeishuJob(ctx, retry); err != nil {
		t.Fatal(err)
	}
	row = readFeishuJob(t, store, scope, first.ID)
	if row.Stage != domain.FeishuAnswer || row.InquiryID != inquiryID {
		t.Fatalf("same owner retry = %+v", row)
	}

	done, err := repo.ClaimFeishuJob(ctx, scope, retryAt.Add(61*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	done.Stage = domain.FeishuDone
	done.Outcome = "answer_sent"
	if err := repo.SaveFeishuJob(ctx, done); err != nil {
		t.Fatal(err)
	}
	row = readFeishuJob(t, store, scope, first.ID)
	if row.Stage != domain.FeishuDone || row.Outcome != "answer_sent" || row.InquiryID != inquiryID {
		t.Fatalf("done dedupe identity = %+v", row)
	}
	if row.GroupID != "" || row.SenderID != "" || row.TargetID != "" || row.CallerID != "" ||
		row.OwnerID != "" || row.Question != "" || row.ClaimID != "" || !row.StartedAt.IsZero() {
		t.Fatalf("done retained private input: %+v", row)
	}

	expired := first
	expired.ID = strings.Repeat("d", 64)
	expired.NextAt, expired.ExpiresAt = now, now.Add(-time.Second)
	if err := repo.EnqueueFeishuJob(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimFeishuJob(ctx, scope, now); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("expired claim error = %v, want not found", err)
	}
	if err := repo.CleanupFeishuJobs(ctx, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.SQLDB().QueryRowContext(ctx,
		`SELECT count(*) FROM assistant_mesh_feishu_jobs WHERE scope = $1 AND id = $2`,
		scope, expired.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired job retained %d rows", count)
	}
}

func feishuPostgres(t *testing.T) *dbstore.Store {
	t.Helper()
	store := receptionPostgres(t)
	dir := t.TempDir()
	name := "0158_assistant_mesh_feishu.sql"
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateFromFile(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	return store
}

func readFeishuJob(t *testing.T, store *dbstore.Store, scope, id string) domain.FeishuJob {
	t.Helper()
	var job domain.FeishuJob
	var startedAt sql.NullTime
	err := store.SQLDB().QueryRowContext(t.Context(), `
		SELECT scope, id, stage, group_id, sender_id, target_id, question, caller_id, owner_id,
		       inquiry_id, COALESCE(claim_id::text, ''), outcome, attempts, next_at, started_at, expires_at
		FROM assistant_mesh_feishu_jobs WHERE scope = $1 AND id = $2
	`, scope, id).Scan(&job.Scope, &job.ID, &job.Stage, &job.GroupID, &job.SenderID, &job.TargetID,
		&job.Question, &job.CallerID, &job.OwnerID, &job.InquiryID, &job.ClaimID, &job.Outcome,
		&job.Attempts, &job.NextAt, &startedAt, &job.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if startedAt.Valid {
		job.StartedAt = startedAt.Time
	}
	return job
}
