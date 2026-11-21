package assistantmesh

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

const (
	feishuLease        = time.Minute
	feishuJobReturning = `j.scope, j.id, j.stage, j.group_id, j.sender_id, j.target_id, j.question,
		j.caller_id, j.owner_id, j.inquiry_id, COALESCE(j.claim_id::text, ''), j.outcome, j.attempts,
		j.next_at, j.started_at, j.expires_at`
)

func (r *Repository) EnqueueFeishuJob(ctx context.Context, job domain.FeishuJob) error {
	if strings.TrimSpace(job.Scope) == "" || strings.TrimSpace(job.ID) == "" ||
		!validFeishuStage(job.Stage) || job.NextAt.IsZero() || job.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: invalid assistant mesh Feishu job", apperrors.ErrInvalidArgument)
	}
	if err := dbtx.DB(ctx, r.db).Exec(`
		INSERT INTO assistant_mesh_feishu_jobs (
			scope, id, stage, group_id, sender_id, target_id, question, caller_id, owner_id,
			inquiry_id, outcome, attempts, next_at, started_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
	`, job.Scope, job.ID, job.Stage, job.GroupID, job.SenderID, job.TargetID, job.Question,
		job.CallerID, job.OwnerID, job.InquiryID, job.Outcome, job.Attempts, job.NextAt,
		nullableFeishuTime(job.StartedAt), job.ExpiresAt).Error; err != nil {
		return fmt.Errorf("enqueue assistant mesh Feishu job: %w", err)
	}
	return nil
}

func (r *Repository) ClaimFeishuJob(ctx context.Context, scope string, now time.Time) (domain.FeishuJob, error) {
	if strings.TrimSpace(scope) == "" || now.IsZero() {
		return domain.FeishuJob{}, fmt.Errorf("%w: Feishu job scope and claim time are required", apperrors.ErrInvalidArgument)
	}
	claimID := uuid.NewString()
	job, err := scanFeishuJob(dbtx.DB(ctx, r.db).Raw(`
		WITH due AS (
			SELECT scope, id FROM assistant_mesh_feishu_jobs
			WHERE scope = ? AND stage <> ? AND next_at <= ? AND expires_at > ?
			  AND (claim_expires_at IS NULL OR claim_expires_at <= ?)
			ORDER BY next_at, id LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE assistant_mesh_feishu_jobs j
		SET claim_id = ?::uuid, claim_expires_at = ?, updated_at = ?
		FROM due
		WHERE j.scope = due.scope AND j.id = due.id
		RETURNING `+feishuJobReturning,
		scope, domain.FeishuDone, now, now, now, claimID, now.Add(feishuLease), now).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FeishuJob{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domain.FeishuJob{}, fmt.Errorf("claim assistant mesh Feishu job: %w", err)
	}
	return job, nil
}

func (r *Repository) SaveFeishuJob(ctx context.Context, job domain.FeishuJob) error {
	if !validFeishuStage(job.Stage) {
		return fmt.Errorf("%w: invalid assistant mesh Feishu job stage", apperrors.ErrInvalidArgument)
	}
	if !canonicalUUID(job.ClaimID) {
		return apperrors.ErrConflict
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		var oldStage, oldInquiry string
		var oldStarted sql.NullTime
		err := db.Raw(`
			SELECT stage, inquiry_id, started_at FROM assistant_mesh_feishu_jobs
			WHERE scope = ? AND id = ? AND claim_id = ?::uuid
			FOR UPDATE
		`, job.Scope, job.ID, job.ClaimID).Row().Scan(&oldStage, &oldInquiry, &oldStarted)
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrConflict
		}
		if err != nil {
			return fmt.Errorf("lock assistant mesh Feishu job: %w", err)
		}

		stage, inquiryID, outcome := job.Stage, job.InquiryID, job.Outcome
		duplicate := false
		if stage == domain.FeishuAnswer && inquiryID != "" {
			duplicate, err = feishuAnswerTaken(ctx, db, job.Scope, job.ID, inquiryID)
			if err != nil {
				return err
			}
			if duplicate {
				stage, inquiryID, outcome = domain.FeishuDone, "", "duplicate"
			}
		}
		if stage == domain.FeishuDone {
			if inquiryID == "" && !duplicate {
				inquiryID = oldInquiry
			}
			job.GroupID, job.SenderID, job.TargetID = "", "", ""
			job.CallerID, job.OwnerID, job.Question = "", "", ""
		}
		if stage != domain.FeishuReceipt {
			job.GroupID = ""
		}
		if stage != domain.FeishuReceipt && stage != domain.FeishuQuestion {
			job.Question = ""
		}

		startedAt := job.StartedAt
		if stage == domain.FeishuDone {
			startedAt = time.Time{}
		} else if stage == oldStage && oldStarted.Valid {
			startedAt = oldStarted.Time
		}
		result := db.Exec(`
			UPDATE assistant_mesh_feishu_jobs
			SET stage = ?, group_id = ?, sender_id = ?, target_id = ?, question = ?, caller_id = ?,
			    owner_id = ?, inquiry_id = ?, outcome = ?, attempts = ?, next_at = ?, started_at = ?,
			    expires_at = ?, claim_id = NULL, claim_expires_at = NULL, updated_at = now()
			WHERE scope = ? AND id = ?
		`, stage, job.GroupID, job.SenderID, job.TargetID, job.Question, job.CallerID, job.OwnerID,
			inquiryID, outcome, job.Attempts, job.NextAt, nullableFeishuTime(startedAt), job.ExpiresAt, job.Scope, job.ID)
		if result.Error != nil {
			return fmt.Errorf("save assistant mesh Feishu job: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return nil
	})
}

func (r *Repository) CleanupFeishuJobs(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := dbtx.DB(ctx, r.db).Exec(
		`DELETE FROM assistant_mesh_feishu_jobs WHERE expires_at <= ?`, now,
	).Error; err != nil {
		return fmt.Errorf("cleanup assistant mesh Feishu jobs: %w", err)
	}
	return nil
}

func feishuAnswerTaken(ctx context.Context, db *gorm.DB, scope, id, inquiryID string) (bool, error) {
	if err := db.WithContext(ctx).Exec(
		`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`,
		"assistant-mesh-feishu-answer:"+scope+":"+inquiryID,
	).Error; err != nil {
		return false, fmt.Errorf("lock assistant mesh Feishu answer owner: %w", err)
	}
	var taken bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM assistant_mesh_feishu_jobs
			WHERE scope = ? AND inquiry_id = ? AND id <> ? AND stage IN ('answer', 'done')
		)
	`, scope, inquiryID, id).Row().Scan(&taken); err != nil {
		return false, fmt.Errorf("check assistant mesh Feishu answer owner: %w", err)
	}
	return taken, nil
}

func validFeishuStage(stage string) bool {
	switch stage {
	case domain.FeishuReceipt, domain.FeishuQuestion, domain.FeishuAnswer, domain.FeishuDone:
		return true
	default:
		return false
	}
}

func nullableFeishuTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func scanFeishuJob(scanner interface{ Scan(...any) error }) (domain.FeishuJob, error) {
	var job domain.FeishuJob
	var startedAt sql.NullTime
	if err := scanner.Scan(&job.Scope, &job.ID, &job.Stage, &job.GroupID, &job.SenderID, &job.TargetID,
		&job.Question, &job.CallerID, &job.OwnerID, &job.InquiryID, &job.ClaimID, &job.Outcome,
		&job.Attempts, &job.NextAt, &startedAt, &job.ExpiresAt); err != nil {
		return domain.FeishuJob{}, err
	}
	if startedAt.Valid {
		job.StartedAt = startedAt.Time
	}
	return job, nil
}
