package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) GetTemplateSource(ctx context.Context, id string) (domaindocument.Source, error) {
	return readTemplateSource(r.db.WithContext(ctx), id, false)
}

func readTemplateSource(db *gorm.DB, id string, lock bool) (domaindocument.Source, error) {
	query := `SELECT definition FROM delivery_template_sources WHERE id = ? AND deleted_at IS NULL`
	if lock {
		query += " FOR UPDATE"
	}
	var data []byte
	var item domaindocument.Source
	err := db.Raw(query, id).Row().Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	err = json.Unmarshal(data, &item)
	return item, err
}

func (r *Repository) ListTemplateSources(ctx context.Context, offset, limit int) ([]domaindocument.Source, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT definition FROM delivery_template_sources WHERE deleted_at IS NULL ORDER BY id OFFSET ? LIMIT ?`, offset, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domaindocument.Source{}
	for rows.Next() {
		var data []byte
		var item domaindocument.Source
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) SaveTemplateSource(ctx context.Context, item domaindocument.Source, expected int) (domaindocument.Source, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item.UpdatedAt = time.Now().UTC()
		if item.ID == "" {
			if expected != 0 {
				return apperrors.ErrInvalidArgument
			}
			item.ID, item.Generation, item.CreatedAt = uuid.NewString(), 1, item.UpdatedAt
			data, err := json.Marshal(item)
			if err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO delivery_template_sources (id, generation, definition) VALUES (?, ?, ?::jsonb)`, item.ID, item.Generation, string(data)).Error
		}
		before, err := lockTemplateSourceGeneration(tx, item.ID, expected)
		if err != nil {
			return err
		}
		item.Generation, item.CreatedAt = before.Generation+1, before.CreatedAt
		item.LastSyncRunID, item.LastAppliedRunID, item.ResolvedCommit = before.LastSyncRunID, before.LastAppliedRunID, before.ResolvedCommit
		return writeTemplateSource(tx, item)
	})
	return item, err
}

func lockTemplateSourceGeneration(tx *gorm.DB, id string, expected int) (domaindocument.Source, error) {
	item, err := readTemplateSource(tx, id, true)
	if err != nil {
		return item, err
	}
	if expected < 1 || item.Generation != expected {
		return item, fmt.Errorf("%w: source changed; preview again", apperrors.ErrConflict)
	}
	return item, nil
}

func writeTemplateSource(tx *gorm.DB, item domaindocument.Source) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return tx.Exec(`UPDATE delivery_template_sources SET generation = ?, definition = ?::jsonb WHERE id = ?`, item.Generation, string(data), item.ID).Error
}

func (r *Repository) ListTemplateSourceObjects(ctx context.Context, id string) ([]domaindocument.StoredAssociation, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT association, imported_document FROM delivery_template_source_objects WHERE source_id = ? ORDER BY kind, object_id`, id).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domaindocument.StoredAssociation{}
	for rows.Next() {
		var data, document []byte
		var item domaindocument.StoredAssociation
		if err := rows.Scan(&data, &document); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &item.Association); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(document, &item.Document); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetDocumentSource(ctx context.Context, kind, id string, version int64) (domaindocument.SourceInfo, error) {
	var result domaindocument.SourceInfo
	var data []byte
	err := r.db.WithContext(ctx).Raw(`SELECT association FROM delivery_template_source_objects WHERE kind = ? AND object_id = ?`, kind, id).Row().Scan(&data)
	if err == nil {
		if err := json.Unmarshal(data, &result.Association); err != nil {
			return result, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if version > 0 {
		err = r.db.WithContext(ctx).Raw(`SELECT provenance FROM delivery_document_provenance WHERE kind = ? AND object_id = ? AND version = ?`, kind, id, version).Row().Scan(&data)
		if err == nil {
			if err := json.Unmarshal(data, &result.Provenance); err != nil {
				return result, err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
	}
	return result, nil
}

func (r *Repository) RemoveTemplateSource(ctx context.Context, id string, expected int, kind, objectID, disposition string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		source, err := lockTemplateSourceGeneration(tx, id, expected)
		if err != nil {
			return err
		}
		objects, err := New(tx).ListTemplateSourceObjects(ctx, id)
		if err != nil {
			return err
		}
		found := objectID == ""
		for _, object := range objects {
			if objectID != "" && (object.ObjectID != objectID || string(object.Kind) != kind) {
				continue
			}
			found = true
			if err := removeTemplateSourceObject(ctx, tx, object, disposition); err != nil {
				return err
			}
		}
		if !found {
			return ErrNotFound
		}
		source.Generation++
		source.UpdatedAt = time.Now().UTC()
		if err := writeTemplateSource(tx, source); err != nil {
			return err
		}
		if objectID == "" {
			return tx.Exec(`UPDATE delivery_template_sources SET deleted_at = NOW() WHERE id = ?`, id).Error
		}
		return nil
	})
}

func removeTemplateSourceObject(ctx context.Context, tx *gorm.DB, object domaindocument.StoredAssociation, disposition string) error {
	if disposition != "keep" && disposition != "deprecate" || disposition == "deprecate" && object.Kind == "Workflow" {
		return apperrors.ErrInvalidArgument
	}
	table, err := documentTable(string(object.Kind))
	if err != nil {
		return err
	}
	var id string
	if err := tx.Raw(`SELECT id FROM `+table+` WHERE id = ? FOR UPDATE`, object.ObjectID).Row().Scan(&id); err != nil {
		return err
	}
	if disposition == "deprecate" {
		if err := New(tx).deprecateTemplate(ctx, table, id); err != nil {
			return err
		}
	}
	return tx.Exec(`DELETE FROM delivery_template_source_objects WHERE source_id = ? AND kind = ? AND object_id = ?`, object.SourceID, object.Kind, id).Error
}
