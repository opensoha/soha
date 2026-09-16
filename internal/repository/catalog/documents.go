package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	workflowrepo "github.com/opensoha/soha/internal/repository/workflow"
	"gorm.io/gorm"
)

func documentTable(kind string) (string, error) {
	switch kind {
	case "BuildTemplate":
		return "build_templates", nil
	case "DeploymentTemplate":
		return "deployment_templates", nil
	case "WorkflowTemplate":
		return "workflow_templates", nil
	case "Workflow":
		return "delivery_workflows", nil
	default:
		return "", fmt.Errorf("%w: unsupported document kind", apperrors.ErrInvalidArgument)
	}
}

func (r *Repository) FindDocumentKey(ctx context.Context, kind, key string) (string, error) {
	table, err := documentTable(kind)
	if err != nil || kind == "Workflow" {
		return "", fmt.Errorf("%w: template kind required", apperrors.ErrInvalidArgument)
	}
	var id string
	err = r.db.WithContext(ctx).Table(table).Select("id").Where("template_key = ?", key).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func lockDocumentActor(tx *gorm.DB, actor string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "delivery-document:"+actor).Error
}

func (r *Repository) SaveDocumentPreview(ctx context.Context, preview domaindocument.StoredPreview) error {
	data, err := json.Marshal(preview.Candidates)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDocumentActor(tx, preview.ActorID); err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM delivery_document_imports WHERE result IS NULL AND expires_at <= NOW()`).Error; err != nil {
			return err
		}
		var pending int64
		if err := tx.Table("delivery_document_imports").Where("actor_id = ? AND result IS NULL", preview.ActorID).Count(&pending).Error; err != nil {
			return err
		}
		if pending >= 32 {
			return fmt.Errorf("%w: too many pending previews; apply one or wait for expiration", apperrors.ErrConflict)
		}
		return tx.Exec(`INSERT INTO delivery_document_imports (id, actor_id, candidate_digest, candidates, expires_at)
			VALUES (?, ?, ?, ?::jsonb, ?)`, preview.ID, preview.ActorID, preview.CandidateDigest, string(data), preview.ExpiresAt).Error
	})
}

func (r *Repository) GetDocumentPreview(ctx context.Context, id, actor string) (domaindocument.StoredPreview, error) {
	return readDocumentPreview(r.db.WithContext(ctx), id, actor, false)
}

func readDocumentPreview(db *gorm.DB, id, actor string, lock bool) (domaindocument.StoredPreview, error) {
	item := domaindocument.StoredPreview{ActorID: actor}
	query := `SELECT id, candidate_digest, candidates, expires_at, COALESCE(idempotency_key, ''), result
		FROM delivery_document_imports WHERE id = ? AND actor_id = ?`
	if lock {
		query += " FOR UPDATE"
	}
	var candidates, result []byte
	err := db.Raw(query, id, actor).Row().Scan(&item.ID, &item.CandidateDigest, &candidates, &item.ExpiresAt, &item.IdempotencyKey, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal(candidates, &item.Candidates); err != nil {
		return item, err
	}
	if len(result) > 0 {
		if err := json.Unmarshal(result, &item.Result); err != nil {
			return item, err
		}
	}
	item.Valid = true
	return item, nil
}

func (r *Repository) ApplyDocumentImport(ctx context.Context, preview domaindocument.StoredPreview, key string, mutations []domaindocument.Mutation) (domaindocument.Import, error) {
	result := domaindocument.Import{PreviewID: preview.ID, Objects: []domaindocument.ImportedObject{}}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDocumentActor(tx, preview.ActorID); err != nil {
			return err
		}
		stored, err := readDocumentPreview(tx, preview.ID, preview.ActorID, true)
		if err != nil {
			return err
		}
		if err := validateDocumentImport(tx, stored, preview, key, mutations); err != nil {
			return err
		}
		if stored.Result != nil {
			result = *stored.Result
			return nil
		}
		if err := lockDocumentTargets(tx, mutations); err != nil {
			return err
		}
		for _, mutation := range mutations {
			item, err := New(tx).writeDocument(ctx, stored.ActorID, mutation)
			if err != nil {
				return err
			}
			result.Objects = append(result.Objects, item)
		}
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.Exec(`UPDATE delivery_document_imports SET idempotency_key = ?, result = ?::jsonb, applied_at = NOW() WHERE id = ?`, key, string(data), stored.ID).Error
	})
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return domaindocument.Import{}, fmt.Errorf("%w: template key or import key was claimed; preview again", apperrors.ErrConflict)
		}
		return domaindocument.Import{}, err
	}
	return result, nil
}

func validateDocumentImport(tx *gorm.DB, stored, expected domaindocument.StoredPreview, key string, mutations []domaindocument.Mutation) error {
	if stored.CandidateDigest != expected.CandidateDigest || !reflect.DeepEqual(stored.Candidates, expected.Candidates) || len(mutations) != len(stored.Candidates) {
		return fmt.Errorf("%w: preview content changed", apperrors.ErrConflict)
	}
	for index, mutation := range mutations {
		if !reflect.DeepEqual(mutation.Candidate, stored.Candidates[index]) {
			return fmt.Errorf("%w: prepared document differs from preview", apperrors.ErrConflict)
		}
	}
	if stored.Result != nil {
		if stored.IdempotencyKey != key {
			return fmt.Errorf("%w: preview already applied with another key", apperrors.ErrConflict)
		}
		return nil
	}
	if stored.ExpiresAt == nil || !time.Now().Before(*stored.ExpiresAt) {
		return fmt.Errorf("%w: import preview expired", apperrors.ErrConflict)
	}
	var count int64
	if err := tx.Table("delivery_document_imports").Where("actor_id = ? AND idempotency_key = ?", stored.ActorID, key).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("%w: idempotency key belongs to another preview", apperrors.ErrConflict)
	}
	return nil
}

func lockDocumentTargets(tx *gorm.DB, mutations []domaindocument.Mutation) error {
	// Stable ordering prevents two imports with overlapping targets deadlocking.
	ordered := slices.Clone(mutations)
	slices.SortFunc(ordered, func(a, b domaindocument.Mutation) int {
		return strings.Compare(a.Document.Kind+":"+a.TargetID, b.Document.Kind+":"+b.TargetID)
	})
	for _, mutation := range ordered {
		if mutation.TargetID == "" {
			continue
		}
		table, err := documentTable(mutation.Document.Kind)
		if err != nil {
			return err
		}
		if mutation.Document.Kind == "Workflow" {
			var version int64
			err := tx.Raw(`SELECT version FROM delivery_workflows WHERE id = ? FOR UPDATE`, mutation.TargetID).Row().Scan(&version)
			if errors.Is(err, sql.ErrNoRows) || err == nil && version != mutation.ExpectedRevision {
				return fmt.Errorf("%w: workflow changed; preview again", apperrors.ErrConflict)
			}
			if err != nil {
				return err
			}
		} else if _, err := lockTemplateHead(tx, table, mutation.TargetID, &mutation.ExpectedRevision, false); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) writeDocument(ctx context.Context, actor string, mutation domaindocument.Mutation) (domaindocument.ImportedObject, error) {
	result := domaindocument.ImportedObject{Path: mutation.Path, Kind: mutation.Document.Kind, ID: mutation.TargetID, Revision: mutation.ExpectedRevision, Action: mutation.Action}
	if mutation.Action == "unchanged" {
		return result, nil
	}
	switch mutation.Document.Kind {
	case "BuildTemplate":
		publish := false
		mutation.Build.Publish = &publish
		item, err := r.saveBuildTemplate(ctx, mutation.TargetID, mutation.Build)
		result.ID, result.Revision = item.ID, item.Revision
		return result, err
	case "DeploymentTemplate":
		item, err := r.SaveDeploymentTemplate(ctx, mutation.TargetID, mutation.Deployment)
		result.ID, result.Revision = item.ID, item.Revision
		return result, err
	case "WorkflowTemplate":
		publish := false
		mutation.Template.Publish = &publish
		item, err := r.saveWorkflowTemplate(ctx, mutation.TargetID, mutation.Template)
		result.ID, result.Revision = item.ID, item.Revision
		return result, err
	case "Workflow":
		id := mutation.TargetID
		if id == "" {
			id = uuid.NewString()
		}
		item, err := workflowrepo.New(r.db).SaveDeliveryWorkflow(ctx, domainworkflow.DeliveryWorkflow{ID: id, Definition: mutation.Workflow.Definition, CreatedBy: actor}, mutation.ExpectedRevision)
		result.ID, result.Revision = item.ID, item.Version
		return result, err
	default:
		return result, fmt.Errorf("%w: unsupported document kind", apperrors.ErrInvalidArgument)
	}
}
