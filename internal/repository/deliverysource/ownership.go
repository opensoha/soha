// Package deliverysource shares the durable single-writer check between catalog
// templates and saved workflows. Call it while holding the object's row lock.
package deliverysource

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type writeContextKey struct{}
type writeContext struct{ source, kind, id string }

// ForImport is used only by the source repository's atomic candidate apply.
func ForImport(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, writeContextKey{}, writeContext{source: source})
}

// ForPublication permits publishing the current, CAS-protected stored payload.
func ForPublication(ctx context.Context, kind, id string) context.Context {
	return context.WithValue(ctx, writeContextKey{}, writeContext{kind: kind, id: id})
}

func CheckWrite(tx *gorm.DB, kind, id string) error {
	var source string
	err := tx.Raw(`SELECT source_id FROM delivery_template_source_objects WHERE kind = ? AND object_id = ?`, kind, id).Row().Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	allowed, _ := tx.Statement.Context.Value(writeContextKey{}).(writeContext)
	if allowed.source == source || allowed.kind == kind && allowed.id == id {
		return nil
	}
	return fmt.Errorf("%w: Git-managed definition is read-only; copy it or detach its source before editing", apperrors.ErrConflict)
}

func RecordVersion(tx *gorm.DB, kind, id string, version int64) error {
	return tx.Exec(`INSERT INTO delivery_document_provenance (kind, object_id, version, provenance)
		SELECT o.kind, o.object_id, ?, jsonb_build_object('kind', o.kind, 'objectId', o.object_id, 'version', ?::bigint,
		'sourceId', o.source_id, 'repositoryId', r.repository_id, 'resolvedCommit', o.association->>'resolvedCommit',
		'treeDigest', r.definition->>'treeDigest', 'path', o.association->>'path',
		'sourceDigest', o.association->>'sourceDigest', 'normalizedSpecDigest', o.association->>'normalizedSpecDigest',
		'syncRunId', o.association->>'syncRunId')
		FROM delivery_template_source_objects o JOIN delivery_template_sync_runs r ON r.id = o.association->>'syncRunId'
		WHERE o.kind = ? AND o.object_id = ?`, version, version, kind, id).Error
}
