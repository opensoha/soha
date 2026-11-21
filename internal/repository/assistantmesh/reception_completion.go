package assistantmesh

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *Repository) CompleteInquiry(ctx context.Context, item domain.Inquiry) error {
	if item.Status != domain.InquiryAnswered || item.Answer == "" || len(item.Citations) != 1 ||
		item.ClosedAt == nil || item.PurgeAfter == nil {
		return apperrors.ErrInvalidArgument
	}
	citations, err := json.Marshal(item.Citations)
	if err != nil {
		return err
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		open, err := lockInquiryOwner(ctx, db, item.OwnerID)
		if err != nil {
			return err
		}
		if !open {
			return apperrors.ErrConflict
		}
		result := db.Exec(`
			UPDATE assistant_mesh_inquiries i
			SET status = ?, answer = ?, citations = ?::jsonb, closed_at = ?, purge_after = ?
			WHERE i.id = ? AND i.caller_id = ? AND i.owner_id = ? AND i.status = ? AND i.due_at > ?
			  AND EXISTS (SELECT 1 FROM users u WHERE u.id = i.owner_id AND u.status = 'active')
			  AND EXISTS (SELECT 1 FROM users u WHERE u.id = i.caller_id AND u.status = 'active')
		`, item.Status, item.Answer, string(citations), item.ClosedAt, item.PurgeAfter, item.ID, item.CallerID,
			item.OwnerID, domain.InquiryProcessing, item.ClosedAt)
		if result.Error != nil {
			return fmt.Errorf("complete assistant mesh inquiry: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return nil
	})
}

func (r *Repository) Cleanup(ctx context.Context) error {
	return r.CleanupWithInquiryPolicy(ctx, domain.InquiryLimits{OwnerPending: domain.DefaultInquiryPendingLimit, Retention: receptionRetention})
}

func (r *Repository) CleanupWithInquiryPolicy(ctx context.Context, policy domain.InquiryLimits) error {
	now := time.Now().UTC()
	return errors.Join(r.cleanupInquiries(ctx, policy), r.CleanupDelegations(ctx, now, receptionRetention), r.CleanupFeishuJobs(ctx, now))
}

func (r *Repository) cleanupInquiries(ctx context.Context, policy domain.InquiryLimits) error {
	if err := r.recoverReception(ctx, policy); err != nil {
		return err
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if err := db.Exec(`
			WITH due AS (
				SELECT id FROM assistant_mesh_inquiries WHERE status IN ('pending', 'processing') AND due_at <= now()
				ORDER BY due_at LIMIT 200 FOR UPDATE SKIP LOCKED
			)
			UPDATE assistant_mesh_inquiries i
			SET status = 'expired', closed_at = i.due_at, purge_after = i.due_at + make_interval(secs => assistant_mesh_effective_inquiry_retention_seconds(?))
			FROM due WHERE i.id = due.id
		`, int(policy.Retention.Seconds())).Error; err != nil {
			return fmt.Errorf("expire assistant mesh inquiries: %w", err)
		}
		if err := db.Exec(`
			WITH ready AS (
				SELECT id FROM assistant_mesh_inquiries
				WHERE purge_after <= now() AND (question <> '' OR answer <> '' OR citations <> '[]'::jsonb)
				ORDER BY purge_after LIMIT 200 FOR UPDATE SKIP LOCKED
			)
			UPDATE assistant_mesh_inquiries i SET question = '', answer = '',
			    citation_versions = CASE WHEN citations <> '[]'::jsonb
			        THEN assistant_mesh_retained_citation_versions(citations) ELSE citation_versions END,
			    citations = '[]'::jsonb
			FROM ready WHERE i.id = ready.id
		`).Error; err != nil {
			return fmt.Errorf("purge assistant mesh inquiries: %w", err)
		}
		return nil
	})
}

func (r *Repository) FallbackInquiry(ctx context.Context, item domain.Inquiry, limit int, now time.Time, retention time.Duration) (domain.Inquiry, error) {
	var current domain.Inquiry
	var full bool
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		open, err := lockInquiryOwner(ctx, db, item.OwnerID)
		if err != nil {
			return err
		}
		counts, err := inquiryAdmissionCounts(ctx, db, item.CallerID, item.OwnerID, now)
		if err != nil {
			return err
		}
		full = limit > 0 && counts.ownerOpen >= limit
		current, err = fallbackReception(db, item, open && !full, now, retention)
		return err
	})
	if err == nil && full && current.Status == domain.InquiryInvalidated {
		return current, domain.ErrInquiryInboxFull
	}
	return current, err
}

func fallbackReception(db *gorm.DB, item domain.Inquiry, promote bool, now time.Time, retention time.Duration) (domain.Inquiry, error) {
	status := domain.InquiryInvalidated
	var closedAt, purgeAfter *time.Time
	if promote {
		status = domain.InquiryPending
	} else {
		purge := now.Add(retention)
		closedAt, purgeAfter = &now, &purge
	}
	next, err := scanInquiry(db.Raw(`
		UPDATE assistant_mesh_inquiries
		SET status = ?, closed_at = ?, purge_after = ?, escalated = ?
		WHERE id = ? AND caller_id = ? AND owner_id = ? AND status = ? AND due_at > ?
		RETURNING `+inquiryColumns, status, closedAt, purgeAfter, promote,
		item.ID, item.CallerID, item.OwnerID, domain.InquiryProcessing, now).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return scanInquiry(db.Raw(`SELECT `+inquiryColumns+`
			FROM assistant_mesh_inquiries WHERE id = ? AND caller_id = ? AND owner_id = ?`,
			item.ID, item.CallerID, item.OwnerID).Row())
	}
	return next, err
}

func (r *Repository) recoverReception(ctx context.Context, policy domain.InquiryLimits) error {
	now := time.Now().UTC()
	var rows []struct{ ID, OwnerID, CallerID string }
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, owner_id, caller_id FROM assistant_mesh_inquiries
		WHERE status = ? AND created_at <= ? AND due_at > ?
		ORDER BY created_at LIMIT 50
	`, domain.InquiryProcessing, now.Add(-time.Minute), now).Scan(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		item := domain.Inquiry{ID: row.ID, OwnerID: row.OwnerID, CallerID: row.CallerID}
		_, err := r.FallbackInquiry(ctx, item, policy.OwnerPending, now, policy.Retention)
		if err != nil && !errors.Is(err, domain.ErrInquiryInboxFull) && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}
