package contentdlp

import (
	"context"
	"fmt"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// ContentHoldReleasable verifies the source event and every coalesced
// transfer event. An absent appeal cannot authorize release.
func (r *Repository) ContentHoldReleasable(ctx context.Context, holdID, sourceID, projectID, subjectID string) (bool, error) {
	if r == nil {
		return false, apperrors.ErrServiceUnavailable
	}
	tx, ok := dbtx.Bound(ctx, r.db)
	if !ok {
		return false, apperrors.ErrServiceUnavailable
	}
	var allowed bool
	err := tx.WithContext(ctx).Raw(`WITH linked AS MATERIALIZED (
 SELECT id,project_id,actor_id,action,metadata->>'networkHoldId' AS hold_id
 FROM docker_volume_dlp_events
 WHERE id=? OR (action='transfer' AND metadata->>'networkHoldId'=?)
 ORDER BY id FOR SHARE
), approvals AS MATERIALIZED (
 SELECT a.event_id,a.project_id,a.actor_id,a.status,a.reviewer_id
 FROM docker_volume_dlp_appeals a JOIN linked l ON l.id=a.event_id
 ORDER BY a.event_id FOR SHARE OF a
)
SELECT COALESCE(bool_or(l.id=?),false) AND COALESCE(bool_and((
 l.action='transfer' AND l.hold_id=? AND l.project_id=? AND l.actor_id=?
 AND a.status='approved' AND a.project_id=l.project_id AND a.actor_id=l.actor_id
 AND a.reviewer_id<>'' AND a.reviewer_id<>l.actor_id
) IS TRUE),false)
FROM linked l LEFT JOIN approvals a ON a.event_id=l.id`, sourceID, holdID, sourceID, holdID, projectID, subjectID).Row().Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("read linked DLP approvals: %w", err)
	}
	return allowed, nil
}
