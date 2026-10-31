package docker

import (
	"context"
	"database/sql"
	"errors"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// Keep callback inventory writes and both immutable row identities under the
// same transaction lock; a separate pre-read would permit delete/recreate ABA.
func (r *Repository) WithinRuntimeIdentity(ctx context.Context, hostID, projectID, hostUID, projectUID string, apply func(context.Context) error) error {
	if apply == nil {
		return apperrors.ErrInvalidArgument
	}
	return dbtx.Within(ctx, r.db, func(txCtx context.Context) error {
		var currentHostUID, currentProjectUID string
		var version int
		err := dbtx.DB(txCtx, r.db).Raw(`SELECT h.resource_uid::text,p.resource_uid::text,p.runtime_identity_version
			FROM docker_hosts h JOIN docker_projects p ON p.host_id=h.id
			WHERE h.id=? AND p.id=? FOR UPDATE OF h,p`, hostID, projectID).Row().Scan(&currentHostUID, &currentProjectUID, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrConflict
		}
		if err != nil {
			return err
		}
		if version != 1 || currentHostUID != hostUID || currentProjectUID != projectUID {
			return apperrors.ErrConflict
		}
		return apply(txCtx)
	})
}

func runtimeIdentityClaimClause(supported bool) string {
	if supported {
		return "(NOT jsonb_exists(payload, 'runtimeBinding') OR payload->'runtimeBinding'->>'protocol' = 'docker-project-identity-v1')"
	}
	return "NOT jsonb_exists(payload, 'runtimeBinding')"
}
