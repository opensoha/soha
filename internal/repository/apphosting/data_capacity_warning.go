package apphosting

import (
	"context"
	"database/sql"
	"errors"
	"time"

	app "github.com/opensoha/soha/internal/application/apphosting"
)

func (r *PostgresRepository) ClaimDataCapacityCheck(ctx context.Context, claimID string, now, expiresAt time.Time) (app.DataCapacityCheck, bool, error) {
	var check app.DataCapacityCheck
	err := r.db.WithContext(ctx).Raw(`
		WITH candidate AS (
			SELECT a.id
			FROM public.hosted_apps a
			JOIN public.hosted_app_revisions rev ON rev.app_id = a.id AND rev.id = a.active_revision_id
			LEFT JOIN public.hosted_app_data_usage_checks c ON c.app_id = a.id
			WHERE a.kind = 'dynamic' AND a.status IN ('online', 'offline') AND rev.status = 'ready'
				AND rev.database_enabled AND (c.next_check_at IS NULL OR c.next_check_at <= ?)
			ORDER BY c.next_check_at NULLS FIRST, a.id
			LIMIT 1
		), claimed AS (
			INSERT INTO public.hosted_app_data_usage_checks (app_id, claim_id, next_check_at, warning_id)
			SELECT id, ?, ?, ? FROM candidate
			ON CONFLICT (app_id) DO UPDATE SET
				claim_id = EXCLUDED.claim_id,
				next_check_at = EXCLUDED.next_check_at,
				warning_id = COALESCE(NULLIF(hosted_app_data_usage_checks.warning_id, ''), EXCLUDED.warning_id)
			WHERE hosted_app_data_usage_checks.next_check_at <= ?
			RETURNING app_id, claim_id, warning_id
		)
		SELECT app_id, claim_id, warning_id FROM claimed
	`, now, claimID, expiresAt, claimID, now).Row().Scan(&check.AppID, &check.ClaimID, &check.WarningID)
	if errors.Is(err, sql.ErrNoRows) {
		return app.DataCapacityCheck{}, false, nil
	}
	return check, err == nil, err
}

func (r *PostgresRepository) RotateDataCapacityWarning(ctx context.Context, check app.DataCapacityCheck, warningID string) error {
	return dynamicRevisionWriteResult(r.db.WithContext(ctx).Exec(`
        UPDATE public.hosted_app_data_usage_checks SET warning_id = ?
        WHERE app_id = ? AND claim_id = ? AND warning_id = ?
    `, warningID, check.AppID, check.ClaimID, check.WarningID))
}

func (r *PostgresRepository) CompleteDataCapacityCheck(ctx context.Context, check app.DataCapacityCheck, above bool, next time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE public.hosted_app_data_usage_checks
		SET claim_id = '', next_check_at = ?,
			warning_id = CASE WHEN ? THEN warning_id ELSE '' END
		WHERE app_id = ? AND claim_id = ?
	`, next, above, check.AppID, check.ClaimID).Error
}
