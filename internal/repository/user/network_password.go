package user

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// PasswordUpdatedAt is a non-secret credential version, not a password hash.
func (r *Repository) PasswordUpdatedAt(ctx context.Context, subjectID string) (time.Time, error) {
	var version time.Time
	err := dbtx.DB(ctx, r.db).Raw(`SELECT password_updated_at FROM user_password_credentials WHERE user_id=?`, subjectID).Row().Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, apperrors.ErrNotFound
	}
	return version.UTC(), err
}
