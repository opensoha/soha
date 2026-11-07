package networkruntime

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// ContentHoldApprovalReader supplies the complete release decision from the
// content owner using the transaction carried by ctx.
type ContentHoldApprovalReader interface {
	ContentHoldReleasable(ctx context.Context, holdID, sourceID, scopeID, subjectID string) (bool, error)
}

// WithinContentGovernance serializes restriction and review with network
// authorization and response policy changes before taking hold or source locks.
func (r *Repository) WithinContentGovernance(ctx context.Context, fn func(context.Context) error) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if err := lockWireGuardMutations(tx); err != nil {
			return err
		}
		for _, key := range []string{"network-endpoint-response-policies:default:default", "network-access-policy-snapshot:default:default"} {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, key).Error; err != nil {
				return err
			}
		}
		return fn(ctx)
	})
}

func (r *Repository) releaseContentHold(ctx context.Context, id, reviewer, sourceType string, approvals ContentHoldApprovalReader) error {
	return r.WithinContentGovernance(ctx, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		var source, scope, subject string
		err := tx.Raw(`SELECT COALESCE(source_event_id,source_transfer_id),COALESCE(project_id,destination_id),subject_id
 FROM network_security_holds WHERE id=? AND source_type=? AND active FOR UPDATE`, id, sourceType).Row().Scan(&source, &scope, &subject)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if approvals == nil {
			return apperrors.ErrServiceUnavailable
		}
		allowed, err := approvals.ContentHoldReleasable(ctx, id, source, scope, subject)
		if err != nil || !allowed {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Exec(`UPDATE network_security_holds SET active=false,revision=revision+1,released_at=?,released_by=? WHERE id=?`, now, reviewer, id).Error; err != nil {
			return err
		}
		return releaseContentEffects(tx, id, subject, now)
	})
}
