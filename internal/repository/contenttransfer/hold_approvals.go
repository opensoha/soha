package contenttransfer

import (
	"context"
	"fmt"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// ContentHoldReleasable verifies the source record and every record linked
// to the same hold using the record owner's approval representation.
func (r *Repository) ContentHoldReleasable(ctx context.Context, holdID, sourceID, destinationID, subjectID string) (bool, error) {
	if r == nil {
		return false, apperrors.ErrServiceUnavailable
	}
	tx, ok := dbtx.Bound(ctx, r.db)
	if !ok {
		return false, apperrors.ErrServiceUnavailable
	}
	var allowed bool
	err := tx.WithContext(ctx).Raw(`WITH linked AS MATERIALIZED (
 SELECT id,actor_id,destination_id,metadata
 FROM content_transfer_records
 WHERE id=? OR metadata->>'networkHoldId'=?
 ORDER BY id FOR SHARE
)
SELECT COALESCE(bool_or(id=?),false) AND COALESCE(bool_and((
 metadata->>'networkHoldId'=? AND destination_id=? AND actor_id=?
 AND metadata->>'appealStatus'='approved'
 AND jsonb_typeof(metadata->'reviewerId')='string'
 AND metadata->>'reviewerId'<>'' AND metadata->>'reviewerId'<>actor_id
) IS TRUE),false)
FROM linked`, sourceID, holdID, sourceID, holdID, destinationID, subjectID).Row().Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("read linked managed content approvals: %w", err)
	}
	return allowed, nil
}
