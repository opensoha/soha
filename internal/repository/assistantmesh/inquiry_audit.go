package assistantmesh

import (
	"context"
	"database/sql"
	"errors"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) ReadInquiryForAudit(ctx context.Context, id string, record func(context.Context, domain.Inquiry) error) error {
	if !canonicalUUID(id) {
		return apperrors.ErrInvalidArgument
	}
	return dbtx.Within(ctx, r.db, func(tx context.Context) error {
		item, err := scanInquiry(dbtx.DB(tx, r.db).Raw(`
			SELECT `+inquiryColumns+` FROM assistant_mesh_inquiries WHERE id = ? FOR SHARE
		`, id).Row())
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return err
		}
		return record(tx, item)
	})
}
