package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/repository/deliverysource"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type templateHead struct {
	Revision         int64
	PublishedVersion int64
	PublicationState string
	CreatedAt        time.Time
}

func lockTemplateHead(tx *gorm.DB, table, id string, expected *int64, create bool) (templateHead, error) {
	head := templateHead{Revision: 1, CreatedAt: time.Now().UTC()}
	if expected != nil && (*expected < 1 || create) {
		return head, fmt.Errorf("%w: expectedRevision requires an existing template revision", apperrors.ErrInvalidArgument)
	}
	if create {
		return head, nil
	}
	err := tx.Table(table).Select("revision, published_version, publication_state, created_at").Where("id = ?", id).
		Clauses(clause.Locking{Strength: "UPDATE"}).Row().Scan(&head.Revision, &head.PublishedVersion, &head.PublicationState, &head.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return head, ErrNotFound
	}
	if err != nil {
		return head, err
	}
	if head.PublicationState == "deprecated" || (expected != nil && head.Revision != *expected) {
		return head, fmt.Errorf("%w: template was modified or deprecated; reload before saving", apperrors.ErrConflict)
	}
	kind := map[string]string{"build_templates": "BuildTemplate", "workflow_templates": "WorkflowTemplate", "deployment_templates": "DeploymentTemplate"}[table]
	if err := deliverysource.CheckWrite(tx, kind, id); err != nil {
		return head, err
	}
	head.Revision++
	return head, nil
}

func templateSaveState(head templateHead, publish *bool) (int64, string) {
	if publish != nil && !*publish {
		return head.PublishedVersion, "draft"
	}
	return head.PublishedVersion + 1, "published"
}

func insertTemplateSnapshot(tx *gorm.DB, kind, id string, version int64, item any) (string, error) {
	data, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("marshal template snapshot: %w", err)
	}
	var digest string
	err = tx.Raw(`INSERT INTO catalog_template_versions (kind, template_id, version, snapshot, content_digest)
		VALUES (?, ?, ?, ?::jsonb, catalog_template_content_digest(?::jsonb)) RETURNING content_digest`,
		kind, id, version, string(data), string(data)).Row().Scan(&digest)
	if err == nil {
		documentKind := map[string]string{"build": "BuildTemplate", "workflow": "WorkflowTemplate", "deployment": "DeploymentTemplate"}[kind]
		err = deliverysource.RecordVersion(tx, documentKind, id, version)
	}
	return digest, err
}

func (r *Repository) deprecateTemplate(ctx context.Context, table, id string) error {
	result := r.db.WithContext(ctx).Table(table).Where("id = ?", strings.TrimSpace(id)).Updates(map[string]any{
		"publication_state": "deprecated", "enabled": false, "revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) saveBuildTemplate(ctx context.Context, id string, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	item := normalizeBuildTemplateInput(input)
	create := id == ""
	if !create {
		item.ID = id
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		head, err := lockTemplateHead(tx, "build_templates", item.ID, input.ExpectedRevision, create)
		if err != nil {
			return err
		}
		item.Revision, item.CreatedAt = head.Revision, head.CreatedAt
		item.PublishedVersion, item.PublicationState = templateSaveState(head, input.Publish)
		commands, err := json.Marshal(item.BuildCommands)
		if err != nil {
			return err
		}
		schema, err := json.Marshal(item.VariableSchema)
		if err != nil {
			return err
		}
		variables, err := json.Marshal(item.DefaultVariables)
		if err != nil {
			return err
		}
		if item.PublicationState == "published" {
			item.ContentDigest, err = insertTemplateSnapshot(tx, "build", item.ID, item.PublishedVersion, item)
			if err != nil {
				return err
			}
		}
		values := map[string]any{
			"template_key": item.Key, "name": item.Name, "description": nullableString(item.Description),
			"builder_kind": item.BuilderKind, "dockerfile_template": nullableString(item.DockerfileTemplate),
			"build_commands": string(commands), "variable_schema": string(schema), "default_variables": string(variables),
			"enabled": item.Enabled, "updated_at": item.UpdatedAt, "revision": item.Revision,
			"published_version": item.PublishedVersion, "publication_state": item.PublicationState, "content_digest": item.ContentDigest,
		}
		return writeTemplateHead(tx, "build_templates", item.ID, item.CreatedAt, values, create)
	})
	return item, err
}

func (r *Repository) saveWorkflowTemplate(ctx context.Context, id string, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	item := normalizeWorkflowTemplateInput(input)
	create := id == ""
	if !create {
		item.ID = id
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		head, err := lockTemplateHead(tx, "workflow_templates", item.ID, input.ExpectedRevision, create)
		if err != nil {
			return err
		}
		item.Revision, item.CreatedAt = head.Revision, head.CreatedAt
		item.PublishedVersion, item.PublicationState = templateSaveState(head, input.Publish)
		definition, err := json.Marshal(item.Definition)
		if err != nil {
			return err
		}
		if item.PublicationState == "published" {
			item.ContentDigest, err = insertTemplateSnapshot(tx, "workflow", item.ID, item.PublishedVersion, item)
			if err != nil {
				return err
			}
		}
		values := map[string]any{
			"template_key": item.Key, "name": item.Name, "description": nullableString(item.Description),
			"category": nullableString(item.Category), "definition": string(definition), "enabled": item.Enabled,
			"updated_at": item.UpdatedAt, "revision": item.Revision, "published_version": item.PublishedVersion,
			"publication_state": item.PublicationState, "content_digest": item.ContentDigest,
		}
		return writeTemplateHead(tx, "workflow_templates", item.ID, item.CreatedAt, values, create)
	})
	return item, err
}

func writeTemplateHead(tx *gorm.DB, table, id string, createdAt time.Time, values map[string]any, create bool) error {
	if create {
		values["id"], values["created_at"] = id, createdAt
		return tx.Table(table).Create(values).Error
	}
	return tx.Table(table).Where("id = ?", id).Updates(values).Error
}

func readTemplateVersions[T any](ctx context.Context, db *gorm.DB, kind, id string, version int64) ([]T, error) {
	query := db.WithContext(ctx).Table("catalog_template_versions").Select("snapshot, content_digest").Where("kind = ? AND template_id = ?", kind, strings.TrimSpace(id))
	if version > 0 {
		query = query.Where("version = ?", version)
	}
	rows, err := query.Order("version DESC").Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]T, 0)
	for rows.Next() {
		var data []byte
		var digest string
		if err := rows.Scan(&data, &digest); err != nil {
			return nil, err
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, fmt.Errorf("decode template snapshot: %w", err)
		}
		switch typed := any(&item).(type) {
		case *domaincatalog.BuildTemplate:
			typed.ContentDigest = digest
		case *domaincatalog.WorkflowTemplate:
			typed.ContentDigest = digest
		case *domaincatalog.DeploymentTemplate:
			typed.ContentDigest = digest
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListBuildTemplateVersions(ctx context.Context, id string) ([]domaincatalog.BuildTemplate, error) {
	return readTemplateVersions[domaincatalog.BuildTemplate](ctx, r.db, "build", id, 0)
}

func (r *Repository) GetBuildTemplateVersion(ctx context.Context, id string, version int64) (domaincatalog.BuildTemplate, error) {
	if version < 1 {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("%w: published template version is required", apperrors.ErrInvalidArgument)
	}
	items, err := readTemplateVersions[domaincatalog.BuildTemplate](ctx, r.db, "build", id, version)
	if err != nil {
		return domaincatalog.BuildTemplate{}, err
	}
	if len(items) == 0 {
		return domaincatalog.BuildTemplate{}, ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) ListWorkflowTemplateVersions(ctx context.Context, id string) ([]domaincatalog.WorkflowTemplate, error) {
	return readTemplateVersions[domaincatalog.WorkflowTemplate](ctx, r.db, "workflow", id, 0)
}

func (r *Repository) GetWorkflowTemplateVersion(ctx context.Context, id string, version int64) (domaincatalog.WorkflowTemplate, error) {
	if version < 1 {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("%w: published template version is required", apperrors.ErrInvalidArgument)
	}
	items, err := readTemplateVersions[domaincatalog.WorkflowTemplate](ctx, r.db, "workflow", id, version)
	if err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	if len(items) == 0 {
		return domaincatalog.WorkflowTemplate{}, ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) PublishBuildTemplate(ctx context.Context, id string, expected int64) (domaincatalog.BuildTemplate, error) {
	item, err := r.GetBuildTemplate(ctx, id)
	if err != nil {
		return item, err
	}
	if item.Revision != expected {
		return item, fmt.Errorf("%w: template revision changed", apperrors.ErrConflict)
	}
	return r.UpdateBuildTemplate(deliverysource.ForPublication(ctx, "BuildTemplate", id), id, domaincatalog.BuildTemplateInput{
		ExpectedRevision: &expected, Key: item.Key, Name: item.Name, Description: item.Description,
		BuilderKind: item.BuilderKind, DockerfileTemplate: item.DockerfileTemplate, BuildCommands: item.BuildCommands,
		VariableSchema: item.VariableSchema, DefaultVariables: item.DefaultVariables, Enabled: item.Enabled,
	})
}

func (r *Repository) PublishWorkflowTemplate(ctx context.Context, id string, expected int64) (domaincatalog.WorkflowTemplate, error) {
	item, err := r.GetWorkflowTemplate(ctx, id)
	if err != nil {
		return item, err
	}
	if item.Revision != expected {
		return item, fmt.Errorf("%w: template revision changed", apperrors.ErrConflict)
	}
	return r.UpdateWorkflowTemplate(deliverysource.ForPublication(ctx, "WorkflowTemplate", id), id, domaincatalog.WorkflowTemplateInput{
		ExpectedRevision: &expected, Key: item.Key, Name: item.Name, Description: item.Description,
		Category: item.Category, Definition: item.Definition, Enabled: item.Enabled,
	})
}

func resolveWorkflowBindingVersion(tx *gorm.DB, item *domaincatalog.ApplicationEnvironment, create bool) error {
	if item.WorkflowTemplateID == "" {
		if item.WorkflowTemplateVersion != 0 {
			return fmt.Errorf("%w: workflowTemplateVersion requires workflowTemplateId", apperrors.ErrInvalidArgument)
		}
		return nil
	}
	var currentID sql.NullString
	var currentVersion int64
	if !create {
		err := tx.Raw(`SELECT workflow_template_id, workflow_template_version FROM application_environments WHERE id = ? FOR UPDATE`, item.ID).Row().Scan(&currentID, &currentVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	unchanged := currentID.String == item.WorkflowTemplateID
	if unchanged && item.WorkflowTemplateVersion == 0 {
		item.WorkflowTemplateVersion = currentVersion
	}
	template, err := New(tx).GetWorkflowTemplate(tx.Statement.Context, item.WorkflowTemplateID)
	if err != nil {
		return err
	}
	if (!unchanged || item.WorkflowTemplateVersion != currentVersion) && (!template.Enabled || template.PublicationState == "deprecated") {
		return fmt.Errorf("%w: workflow template is disabled or deprecated", apperrors.ErrInvalidArgument)
	}
	if item.WorkflowTemplateVersion == 0 {
		item.WorkflowTemplateVersion = template.PublishedVersion
	}
	_, err = New(tx).GetWorkflowTemplateVersion(tx.Statement.Context, item.WorkflowTemplateID, item.WorkflowTemplateVersion)
	return err
}
