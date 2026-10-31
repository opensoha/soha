package networkruntime

import (
	"context"
	"database/sql"
	"errors"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"time"
)

func (r *Repository) RestrictManagedContentSubject(ctx context.Context, record d.Record) (string, error) {
	tx := dbtx.DB(ctx, r.db)
	var id string
	err := tx.Raw(`SELECT id FROM network_security_holds WHERE subject_id=? AND destination_id=? AND source_type='managed_content' AND active FOR UPDATE`, record.ActorID, record.DestinationID).Row().Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err := tx.Exec(`INSERT INTO network_security_holds(id,source_transfer_id,source_type,destination_id,subject_id,active,revision,created_at) VALUES(?,?,'managed_content',?,?,true,1,?)`, record.ID, record.ID, record.DestinationID, record.ActorID, record.CreatedAt).Error; err != nil {
		return "", err
	}
	cause := dlp.NetworkCause{ID: record.ID, ActorID: record.ActorID, CreatedAt: record.CreatedAt}
	if err := revokeContentVPN(tx, cause); err != nil {
		return "", err
	}
	if err := restrictContentNAS(tx, cause); err != nil {
		return "", err
	}
	if err := restrictContentProxy(tx, cause); err != nil {
		return "", err
	}
	return record.ID, nil
}
func (r *Repository) ReleaseManagedContentSubject(ctx context.Context, id, reviewer string) error {
	tx := dbtx.DB(ctx, r.db)
	now := time.Now().UTC()
	var actor string
	err := tx.Raw(`UPDATE network_security_holds h SET active=false,revision=h.revision+1,released_at=?,released_by=? WHERE h.id=? AND h.source_type='managed_content' AND h.active AND NOT EXISTS(SELECT 1 FROM content_transfer_records r WHERE r.metadata->>'networkHoldId'=h.id AND COALESCE(r.metadata->>'appealStatus','pending')<>'approved') RETURNING h.subject_id`, now, reviewer, id).Row().Scan(&actor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return releaseContentEffects(tx, id, actor, now)
}
func (r *Repository) ManagedContentState(ctx context.Context, id string) (d.NetworkHold, error) {
	tx := dbtx.DB(ctx, r.db)
	var source, destination string
	err := tx.Raw(`SELECT source_transfer_id,destination_id FROM network_security_holds WHERE id=? AND source_type='managed_content'`, id).Row().Scan(&source, &destination)
	if errors.Is(err, sql.ErrNoRows) {
		return d.NetworkHold{}, apperrors.ErrNotFound
	}
	if err != nil {
		return d.NetworkHold{}, err
	}
	h, err := r.ContentSubjectState(ctx, id)
	if err != nil {
		return d.NetworkHold{}, err
	}
	return d.NetworkHold{ID: h.ID, SourceRecordID: source, DestinationID: destination, SubjectID: h.SubjectID, Active: h.Active, Revision: h.Revision, CreatedAt: h.CreatedAt, ReleasedAt: h.ReleasedAt, Targets: h.Targets}, nil
}
