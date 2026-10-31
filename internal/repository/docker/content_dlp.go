package docker

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func normalizeDLPDeletionError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "docker_project_active_dlp_hold" {
		return apperrors.NewBusiness(apperrors.ErrConflict, "content_dlp_hold_active", "Resolve the project's active DLP network restrictions before deleting it.", "请先处理该项目的活动 DLP 网络限制，再删除项目。")
	}
	return err
}
