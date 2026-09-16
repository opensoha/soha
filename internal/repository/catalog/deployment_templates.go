package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/repository/deliverysource"
	"gorm.io/gorm"
)

const deploymentTemplateColumns = "id, spec, enabled, revision, published_version, publication_state, content_digest, created_at, updated_at"

func (r *Repository) ListDeploymentTemplates(ctx context.Context) ([]domaincatalog.DeploymentTemplate, error) {
	rows, err := r.db.WithContext(ctx).Table("deployment_templates").Select(deploymentTemplateColumns).Order("name, id").Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domaincatalog.DeploymentTemplate, 0)
	for rows.Next() {
		item, err := scanDeploymentTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetDeploymentTemplate(ctx context.Context, id string) (domaincatalog.DeploymentTemplate, error) {
	return scanDeploymentTemplate(r.db.WithContext(ctx).Table("deployment_templates").Select(deploymentTemplateColumns).Where("id = ?", strings.TrimSpace(id)).Row())
}

func scanDeploymentTemplate(row interface{ Scan(...any) error }) (domaincatalog.DeploymentTemplate, error) {
	var item domaincatalog.DeploymentTemplate
	var data []byte
	err := row.Scan(&item.ID, &data, &item.Enabled, &item.Revision, &item.PublishedVersion, &item.PublicationState, &item.ContentDigest, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	var spec domaincatalog.DeploymentTemplateSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return item, fmt.Errorf("decode deployment template: %w", err)
	}
	spec.Enabled = item.Enabled // Deprecation changes the head, not the frozen spec.
	item.DeploymentTemplateSpec = spec
	return item, nil
}

func (r *Repository) SaveDeploymentTemplate(ctx context.Context, id string, input domaincatalog.DeploymentTemplateInput) (domaincatalog.DeploymentTemplate, error) {
	return r.writeDeploymentTemplate(ctx, id, input, false)
}

func (r *Repository) writeDeploymentTemplate(ctx context.Context, id string, input domaincatalog.DeploymentTemplateInput, publish bool) (domaincatalog.DeploymentTemplate, error) {
	create := id == ""
	if !create && input.ExpectedRevision == nil {
		return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: expectedRevision is required", apperrors.ErrInvalidArgument)
	}
	if create {
		id = uuid.NewString()
	}
	item := domaincatalog.DeploymentTemplate{DeploymentTemplateSpec: input.DeploymentTemplateSpec, ID: id, UpdatedAt: time.Now().UTC()}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		head, err := lockTemplateHead(tx, "deployment_templates", id, input.ExpectedRevision, create)
		if err != nil {
			return err
		}
		item.Revision, item.CreatedAt = head.Revision, head.CreatedAt
		item.PublishedVersion, item.PublicationState = templateSaveState(head, &publish)
		if publish {
			item.ContentDigest, err = insertTemplateSnapshot(tx, "deployment", id, item.PublishedVersion, item)
			if err != nil {
				return err
			}
		}
		data, err := json.Marshal(item.DeploymentTemplateSpec)
		if err != nil {
			return err
		}
		return writeTemplateHead(tx, "deployment_templates", id, item.CreatedAt, map[string]any{
			"template_key": item.Key, "name": item.Name, "spec": string(data), "enabled": item.Enabled,
			"revision": item.Revision, "published_version": item.PublishedVersion, "publication_state": item.PublicationState,
			"content_digest": item.ContentDigest, "updated_at": item.UpdatedAt,
		}, create)
	})
	return item, err
}

func (r *Repository) PublishDeploymentTemplate(ctx context.Context, id string, expected int64) (domaincatalog.DeploymentTemplate, error) {
	item, err := r.GetDeploymentTemplate(ctx, id)
	if err != nil {
		return item, err
	}
	if expected != item.Revision {
		return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: template revision changed", apperrors.ErrConflict)
	}
	return r.writeDeploymentTemplate(deliverysource.ForPublication(ctx, "DeploymentTemplate", id), id, domaincatalog.DeploymentTemplateInput{DeploymentTemplateSpec: item.DeploymentTemplateSpec, ExpectedRevision: &expected}, true)
}

func (r *Repository) DeprecateDeploymentTemplate(ctx context.Context, id string) error {
	return r.deprecateTemplate(ctx, "deployment_templates", id)
}

func (r *Repository) ListDeploymentTemplateVersions(ctx context.Context, id string) ([]domaincatalog.DeploymentTemplate, error) {
	return readTemplateVersions[domaincatalog.DeploymentTemplate](ctx, r.db, "deployment", id, 0)
}

func (r *Repository) GetDeploymentTemplateVersion(ctx context.Context, id string, version int64) (domaincatalog.DeploymentTemplate, error) {
	if version < 1 {
		return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: published template version is required", apperrors.ErrInvalidArgument)
	}
	items, err := readTemplateVersions[domaincatalog.DeploymentTemplate](ctx, r.db, "deployment", id, version)
	if err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	if len(items) == 0 {
		return domaincatalog.DeploymentTemplate{}, ErrNotFound
	}
	return items[0], nil
}
