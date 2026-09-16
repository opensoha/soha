package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/repository/deliverysource"
	"gorm.io/gorm"
)

func (r *Repository) ApplyTemplateSync(ctx context.Context, expected domaindocument.StoredSyncRun, input domaindocument.SyncApplyInput, mutations []domaindocument.Mutation) (domaindocument.StoredSyncRun, error) {
	ctx = deliverysource.ForImport(ctx, expected.SourceID)
	var run domaindocument.StoredSyncRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		source, err := readTemplateSource(tx, expected.SourceID, true)
		if err != nil {
			return err
		}
		run, err = readTemplateSyncRun(tx, expected.SourceID, expected.ID)
		if err != nil {
			return err
		}
		if err := validateTemplateSyncApply(tx, source, run, expected, input, mutations); err != nil {
			return err
		}
		if run.Status == "applied" {
			return nil
		}
		if err := lockDocumentTargets(tx, mutations); err != nil {
			return err
		}
		result := domaindocument.Import{PreviewID: run.ID, Objects: []domaindocument.ImportedObject{}}
		for _, mutation := range mutations {
			object, err := New(tx).writeDocument(ctx, run.ActorID, mutation)
			if err != nil {
				return err
			}
			if err := associateTemplateSource(tx, run, mutation, object); err != nil {
				return err
			}
			result.Objects = append(result.Objects, object)
		}
		if err := markRemovedSourceObjects(tx, run); err != nil {
			return err
		}
		run.Status, run.ApplyKey, run.Result = "applied", input.IdempotencyKey, &result
		if err := writeTemplateSyncRun(tx, &run); err != nil {
			return err
		}
		source.Generation++
		source.LastAppliedRunID, source.ResolvedCommit, source.UpdatedAt = run.ID, run.ResolvedCommit, run.UpdatedAt
		return writeTemplateSource(tx, source)
	})
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.Code == "23505" {
		err = fmt.Errorf("%w: object or idempotency key was claimed; preview again", apperrors.ErrConflict)
	}
	return run, err
}

func validateTemplateSyncApply(tx *gorm.DB, source domaindocument.Source, run, expected domaindocument.StoredSyncRun, input domaindocument.SyncApplyInput, mutations []domaindocument.Mutation) error {
	if run.ActorID != expected.ActorID || run.SourceGeneration != input.ExpectedGeneration || run.Preview == nil || run.Preview.CandidateDigest != input.CandidateDigest {
		return fmt.Errorf("%w: sync candidate identity changed", apperrors.ErrConflict)
	}
	if run.Status == "applied" {
		if run.ApplyKey != input.IdempotencyKey {
			return fmt.Errorf("%w: sync was applied with another key", apperrors.ErrConflict)
		}
		return nil
	}
	if run.Status != "ready" || !source.Enabled || source.Generation != run.SourceGeneration || source.LastSyncRunID != run.ID || run.Preview.ExpiresAt == nil || !time.Now().Before(*run.Preview.ExpiresAt) {
		return fmt.Errorf("%w: sync is stale, expired or unavailable; preview again", apperrors.ErrConflict)
	}
	if !reflect.DeepEqual(run.Preview, expected.Preview) || !reflect.DeepEqual(run.Removed, expected.Removed) || len(mutations) != len(run.Preview.Candidates) {
		return fmt.Errorf("%w: stored candidates changed", apperrors.ErrConflict)
	}
	for index, mutation := range mutations {
		if !reflect.DeepEqual(mutation.Candidate, run.Preview.Candidates[index]) {
			return fmt.Errorf("%w: prepared candidate changed", apperrors.ErrConflict)
		}
	}
	var count int64
	if err := tx.Table("delivery_template_sync_runs").Where("source_id = ? AND actor_id = ? AND apply_key = ?", run.SourceID, run.ActorID, input.IdempotencyKey).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("%w: apply key belongs to another sync", apperrors.ErrConflict)
	}
	return nil
}

func associateTemplateSource(tx *gorm.DB, run domaindocument.StoredSyncRun, mutation domaindocument.Mutation, object domaindocument.ImportedObject) error {
	association := domaindocument.Association{SourceID: run.SourceID, Kind: contractsapi.DeliveryDocumentKind(object.Kind), ObjectID: object.ID,
		Key: mutation.Document.Metadata.Name, Path: mutation.Path, LastImportedRevision: int(object.Revision), ResolvedCommit: run.ResolvedCommit,
		SourceDigest: mutation.SourceDigest, NormalizedSpecDigest: mutation.NormalizedSpecDigest, SyncRunID: run.ID}
	data, err := json.Marshal(association)
	if err != nil {
		return err
	}
	document, err := json.Marshal(mutation.Document)
	if err != nil {
		return err
	}
	result := tx.Exec(`INSERT INTO delivery_template_source_objects (source_id, kind, object_id, document_key, association, imported_document)
		VALUES (?, ?, ?, ?, ?::jsonb, ?::jsonb) ON CONFLICT (kind, object_id) DO UPDATE SET document_key = EXCLUDED.document_key,
		association = EXCLUDED.association, imported_document = EXCLUDED.imported_document
		WHERE delivery_template_source_objects.source_id = EXCLUDED.source_id`, run.SourceID, object.Kind, object.ID, association.Key, string(data), string(document))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: object belongs to another source", apperrors.ErrConflict)
	}
	if object.Kind == "Workflow" && object.Action != "unchanged" {
		return deliverysource.RecordVersion(tx, object.Kind, object.ID, object.Revision)
	}
	return nil
}

func markRemovedSourceObjects(tx *gorm.DB, run domaindocument.StoredSyncRun) error {
	for _, object := range run.Removed {
		result := tx.Exec(`UPDATE delivery_template_source_objects SET association = jsonb_set(association, '{removed}', 'true'::jsonb) WHERE source_id = ? AND kind = ? AND object_id = ?`, run.SourceID, object.Kind, object.ObjectID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: source association changed", apperrors.ErrConflict)
		}
	}
	return nil
}
