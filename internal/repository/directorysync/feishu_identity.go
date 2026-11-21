package directorysync

import (
	"context"
	"fmt"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// ResolveActiveFeishuOpenID returns the local user only when the app-scoped
// open ID maps to an active Feishu person whose identity is bound to the same
// verified login provider. Name, email, and user IDs are never used as a
// fallback.
func (r *Repository) ResolveActiveFeishuOpenID(ctx context.Context, connectionID, loginProviderID, openID string) (string, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT dp.local_user_id
		FROM directory_connections c
		JOIN directory_people dp ON dp.connection_id = c.id
		JOIN user_identities ui ON ui.user_id::text = dp.local_user_id
			AND ui.provider_type = c.provider_type
			AND ui.provider_id = c.login_provider_id
			AND ui.provider_user_id = dp.provider_subject
		WHERE c.id = ? AND c.enabled = true AND c.provider_type = ?
			AND c.login_provider_id = ?
			AND dp.external_id = ? AND dp.provider_subject = ?
			AND dp.status = ? AND dp.archived_at IS NULL AND dp.departed_at IS NULL
			AND dp.local_user_id IS NOT NULL AND dp.local_user_id <> ''
			AND ui.provider_type = ? AND ui.provider_id = ? AND ui.provider_user_id = ?
		LIMIT 2`,
		connectionID, domain.ProviderFeishu, loginProviderID, openID, openID,
		domain.ProjectionActive, domain.ProviderFeishu, loginProviderID, openID,
	).Rows()
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return "", err
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(userIDs) == 0 {
		return "", fmt.Errorf("%w: active Feishu identity", apperrors.ErrNotFound)
	}
	if len(userIDs) > 1 || userIDs[0] == "" {
		return "", fmt.Errorf("%w: ambiguous Feishu identity", apperrors.ErrConflict)
	}
	return userIDs[0], nil
}
